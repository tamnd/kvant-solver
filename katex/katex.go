// Package katex renders TeX to HTML at build time, with no Node and no
// network, by running the real katex.min.js inside a JavaScript engine.
//
// Rendering in the browser was the other option and it is the wrong one for
// this corpus. The Markdown is 4.4 MB with mathematics in most paragraphs, so
// browser rendering means shipping the engine as well as the fonts and then
// showing every reader a flash of raw TeX on a site whose whole content is
// mathematics. Rendering here means the HTML holds the finished markup and the
// browser needs the stylesheet and the fonts and nothing else.
//
// Three ways to reach KaTeX from Go. Shelling out to Node puts a second
// toolchain in CI and on the laptop for a project that is otherwise Go and
// poppler. A Go translator that handles what Bourbaki writes does not exist,
// and writing one is not a trade worth making. Embedding a JavaScript engine
// and running the real file costs one vendored dependency and keeps the build
// pure Go, which is what this does.
//
// The file is vendored rather than fetched, and its SHA-256 is recorded in
// SHA256SUMS and checked by a test, for the same reason every source PDF has
// its hash written down: a dependency that can change under us without saying
// so is not a dependency, it is a risk.
package katex

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// Version is the KaTeX release vendored here. It is written down because the
// error messages below are its error messages and they move between releases.
const Version = "0.18.4"

//go:embed katex.min.js
var script string

//go:embed assets
var assetsFS embed.FS

// Assets is the stylesheet and the fonts a page needs to display what Render
// writes: assets/katex.min.css and assets/fonts/*.woff2.
//
// Only woff2 is vendored, of the three formats KaTeX ships. The stylesheet
// names all three in one src list and every browser that has shipped since 2016
// takes the first it understands, so the other two are bytes nobody would ever
// fetch.
func Assets() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err) // the directory is embedded above; this cannot fail at run time
	}
	return sub
}

// Renderer holds one JavaScript engine with KaTeX loaded in it.
//
// Loading the script costs about a tenth of a second and rendering a span costs
// well under a millisecond, so the engine is built once and kept. A Renderer is
// safe to use from several goroutines: goja is not, and the lock is what makes
// up the difference.
type Renderer struct {
	mu    sync.Mutex
	vm    *goja.Runtime
	call  goja.Callable
	cache map[string]string
}

// New loads KaTeX into a fresh engine.
func New() (*Renderer, error) {
	vm := goja.New()
	if _, err := vm.RunString(script); err != nil {
		return nil, fmt.Errorf("loading katex %s: %w", Version, err)
	}
	fn, ok := goja.AssertFunction(vm.Get("katex").ToObject(vm).Get("renderToString"))
	if !ok {
		return nil, fmt.Errorf("katex %s has no renderToString", Version)
	}
	return &Renderer{vm: vm, call: fn, cache: map[string]string{}}, nil
}

// Render returns the HTML for one span of TeX.
//
// An error is a refusal by KaTeX and it is returned rather than swallowed. A
// span that does not parse is a fault in the extraction, and falling back to
// printing the raw TeX would put the fault on the page in a form that looks
// deliberate. The message is KaTeX's own, which names the character it stopped
// at.
func (r *Renderer) Render(tex string, display bool) (string, error) {
	key := tex
	if display {
		key = "$$" + tex
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if html, ok := r.cache[key]; ok {
		return html, nil
	}

	// set collects the errors from building the options object so the whole
	// thing is checked once at the end. A property set on an object this
	// package just made cannot fail, but writing eighteen ignored returns to
	// say so reads worse than this does.
	var setErr error
	set := func(o *goja.Object, name string, value any) {
		if err := o.Set(name, value); err != nil && setErr == nil {
			setErr = err
		}
	}

	opts := r.vm.NewObject()
	set(opts, "displayMode", display)
	// throwOnError is the default and is set anyway, because the alternative is
	// KaTeX writing the error into the page in red, which is the one behaviour
	// this must not have.
	set(opts, "throwOnError", true)
	// The macros are the Russian function names. KaTeX knows tan and cot and
	// has never heard of tg, ctg, arctg, arcctg, sh, ch or th, which is what
	// this magazine prints and what the prompt asks the model to keep. Defining
	// them here is what lets the corpus hold the Russian spelling and still
	// render, and it is the alternative to Anglicising 34000 pages of formulas
	// so that a renderer written elsewhere is happy.
	//
	// They are written as \mathrm rather than \operatorname because the corpus
	// writes \mathrm{tg} and this is only here for the pages that came back
	// with the bare command anyway.
	macros := r.vm.NewObject()
	for _, name := range []string{"tg", "ctg", "arctg", "arcctg", "sh", "ch", "th", "cth", "arcsh", "arcch", "arcth"} {
		set(macros, `\`+name, `\mathrm{`+name+`}`)
	}
	// And these are the shorthands the model writes for things the magazine
	// printed plainly. Building the whole site turned up 56 spans KaTeX would
	// not take and 51 of them are these two: an epsilon written the way half of
	// mathematics writes it, and a temperature. Neither is a misreading, so
	// defining them is right for the same reason the function names are, and
	// rereading six pages to get a different spelling of the same character
	// would be a waste of the reading lane.
	for name, expansion := range map[string]string{
		`\eps`:     `\varepsilon`,
		`\celsius`: `{}^\circ\mathrm{C}`,
	} {
		set(macros, name, expansion)
	}
	set(opts, "macros", macros)
	set(opts, "strict", false)
	if setErr != nil {
		return "", setErr
	}

	v, err := r.call(goja.Undefined(), r.vm.ToValue(tex), opts)
	if err != nil {
		return "", errors.New(clean(err.Error()))
	}
	html := v.String()
	r.cache[key] = html
	return html, nil
}

// Check parses a span and reports what is wrong with it, throwing the HTML
// away.
//
// It exists because two callers want opposite things from the same work. The
// site wants the markup and holds on to it; the OCR rules want a yes or no on
// every span of a page and would otherwise build a megabyte of HTML per issue
// to look at none of it. The cache is shared either way, so a page whose spans
// were checked here costs nothing to render later.
func (r *Renderer) Check(tex string, display bool) error {
	_, err := r.Render(tex, display)
	return err
}

// clean takes the engine's position off the end of a KaTeX error. The position
// is inside the one line of JavaScript this package evaluates and says nothing
// about the corpus, and the caller knows the file and the line that matter.
func clean(msg string) string {
	if i := strings.Index(msg, " at <eval>:"); i > 0 {
		msg = msg[:i]
	}
	msg = strings.TrimPrefix(msg, "ParseError: ")
	return strings.TrimPrefix(msg, "KaTeX parse error: ")
}
