// Package publish builds the static site out of the committed Markdown.
//
// The corpus is the source and the site is a view of it, the same way pages are
// the source and articles are a view of them. Nothing here writes to the corpus
// and nothing here reads the scan cache, so a build can be thrown away and done
// again from a clean checkout and come out byte for byte the same.
//
// What the reader is sent is text. The mathematics is typeset here rather than
// in the browser, the stylesheet and the fonts are vendored, and no scan, no
// figure and no PDF is copied into the output. That last one is a guard with a
// test behind it rather than a habit, because it is the difference between
// publishing a transcription and republishing the magazine.
package publish

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/mathtex"
)

// Renderer turns one corpus body into the HTML that goes in a page.
//
// It holds a JavaScript engine with KaTeX in it, which costs about a tenth of a
// second to build and is then good for the whole run, so a Renderer is made
// once and used for all twenty two thousand files.
type Renderer struct {
	math *katex.Renderer
	md   goldmark.Markdown
}

// NewRenderer loads KaTeX and configures the Markdown parser.
func NewRenderer() (*Renderer, error) {
	math, err := katex.New()
	if err != nil {
		return nil, fmt.Errorf("load katex: %w", err)
	}
	return &Renderer{
		math: math,
		// Raw HTML in the source is escaped rather than passed through, which is
		// goldmark's default and is kept deliberately. These bodies were written
		// by a model reading a scan, so an angle bracket in one of them is a
		// character the magazine printed and not markup somebody meant. Letting
		// it through would also be the one hole in the guard below, since an img
		// tag in a body would put a request for a picture on the page without
		// any file ever being written.
		md: goldmark.New(
			goldmark.WithExtensions(extension.Footnote, extension.Typographer),
			goldmark.WithRendererOptions(
				html.WithHardWraps(),
				renderer.WithNodeRenderers(util.Prioritized(unlinked{}, 100)),
			),
		),
	}, nil
}

// BadMath is one formula KaTeX would not typeset.
//
// It is reported rather than raised because the corpus is twenty two thousand
// pages a model read off scans, and a handful of them will always hold TeX that
// is not TeX. A build that stopped at the first one would mean the site could
// not be published until the last reading defect in the archive had been found
// and fixed, which is never. So the file is published, the formula is marked on
// the page where a reader can see that the reading failed there, and the count
// comes back so that whoever is running the build knows how many there were and
// can gate on it.
type BadMath struct {
	Line int
	TeX  string
	Err  error
}

func (b BadMath) Error() string { return fmt.Sprintf("line %d: %v", b.Line, b.Err) }

// Render turns a corpus body into HTML and reports the formulas it could not
// typeset.
//
// The mathematics comes out before the Markdown parser sees it and goes back in
// after. That order is not a convenience, it is the only one that works: a
// Markdown parser reads $x_1 + x_2$ as a subscript, an emphasis and a stray
// dollar, and by the time it has finished there is no TeX left to typeset.
//
// An error means the body could not be rendered at all. A body with bad
// formulas in it renders, and the bad ones come back in the second result.
func (r *Renderer) Render(body string) (string, []BadMath, error) {
	hidden, spans := hideMath(body)

	var buf bytes.Buffer
	if err := r.md.Convert([]byte(escapeMarkup(hidden)), &buf); err != nil {
		return "", nil, fmt.Errorf("markdown: %w", err)
	}

	out, bad, err := r.showMath(buf.String(), spans)
	if err != nil {
		return "", nil, err
	}
	return markers(out), bad, nil
}

// The sentinel a hidden span leaves behind, in the private use area.
//
// Those code points cannot occur in the corpus: they have no glyph, no
// magazine printed one, and the OCR prompt has no way to produce one. They are
// also nothing to Markdown and nothing to HTML, so a sentinel travels through
// the parser as ordinary text and comes out the other side unescaped and
// unwrapped, which is the whole requirement.
const (
	sentinelOpen  = '\ue000'
	sentinelClose = '\ue001'
)

// escapeMarkup turns the three characters that mean something to HTML into the
// entities that print them.
//
// goldmark offers two ways to treat raw HTML in the source and neither is what
// this corpus wants. Passing it through would serve whatever a model wrote as
// markup, which is the one hole in the guard below: an img tag in a body puts a
// request for a picture on the page without any file ever being written.
// Dropping it, which is the default, replaces the characters with a comment and
// loses them, and these bodies are a transcription: an angle bracket in one is a
// character somebody printed and the site's job is to show it.
//
// So they are escaped before the parser runs. goldmark reads an entity, turns
// it back into the character, and escapes it again on the way out, so the three
// survive as themselves and as nothing else.
//
// This runs after the mathematics has been hidden, where the ampersands and the
// angle brackets of TeX are already out of the body and behind a sentinel.
func escapeMarkup(body string) string {
	return markupEscaper.Replace(body)
}

var markupEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

// hideMath replaces every math span, delimiters and all, with a sentinel.
func hideMath(body string) (string, []mathtex.Span) {
	spans, _ := mathtex.Split(body)
	if len(spans) == 0 {
		return body, nil
	}

	// Split counts in runes and reports where the text sits rather than where
	// the delimiters sit, so the delimiters are added back here: one dollar
	// either side of an inline span and two either side of a display.
	rs := []rune(body)
	var out strings.Builder
	at := 0
	for i, span := range spans {
		width := 1
		if span.Display {
			width = 2
		}
		from, to := span.Start-width, span.End+width
		if from < at || to > len(rs) {
			// Overlapping or out of range spans would corrupt the body rather
			// than lose a formula, so the span is left as it was written.
			continue
		}
		out.WriteString(string(rs[at:from]))
		out.WriteRune(sentinelOpen)
		fmt.Fprintf(&out, "%d", i)
		out.WriteRune(sentinelClose)
		at = to
	}
	out.WriteString(string(rs[at:]))
	return out.String(), spans
}

// showMath puts the typeset mathematics back where the sentinels are.
//
// A span KaTeX refuses leaves a mark rather than the raw source. Printing the
// source would put the fault on the page looking deliberate, where nobody would
// ever go back and fix it, and dropping the span would lose the only evidence
// that anything was there. The mark says the reading failed here and carries
// what it read, which is what somebody fixing it needs.
func (r *Renderer) showMath(page string, spans []mathtex.Span) (string, []BadMath, error) {
	if len(spans) == 0 {
		return page, nil, nil
	}
	var bad []BadMath
	var out strings.Builder
	rest := page
	for {
		before, after, found := strings.Cut(rest, string(sentinelOpen))
		if !found {
			out.WriteString(rest)
			return out.String(), bad, nil
		}
		out.WriteString(before)

		digits, tail, closed := strings.Cut(after, string(sentinelClose))
		if !closed {
			return "", nil, fmt.Errorf("a hidden formula was never closed")
		}
		var n int
		if _, err := fmt.Sscanf(digits, "%d", &n); err != nil || n < 0 || n >= len(spans) {
			return "", nil, fmt.Errorf("a hidden formula points at nothing: %q", digits)
		}
		markup, err := r.math.Render(spans[n].Text, spans[n].Display)
		if err != nil {
			bad = append(bad, BadMath{Line: spans[n].Line, TeX: spans[n].Text, Err: err})
			markup = brokenMath(spans[n].Text)
		}
		out.WriteString(markup)
		rest = tail
	}
}

// brokenMath is what a reader sees where KaTeX refused.
//
// The source goes in the page as text and not in an attribute, because it is
// the one thing a reader can act on: it shows what the model read off the sheet
// and therefore what is wrong with it.
func brokenMath(tex string) string {
	return `<code class="tex-failed" title="эту формулу не удалось набрать">` +
		string(util.EscapeHTML([]byte(tex))) + `</code>`
}

// unlinked keeps the body from pointing anywhere.
//
// Nothing in a corpus body is a link. Every destination in all twenty two
// thousand files is one of two things: a placeholder like (image) or
// (table2.png) that names no file anywhere, written where the prompt asked for
// a figure mark, or a parenthesis after a square bracket that CommonMark read
// as link syntax and the magazine printed as mathematics. Not one is a URL.
//
// So the destination is dropped and the label is kept, because the label is the
// text somebody printed. Leaving it to goldmark would put an img tag or an
// anchor on the page pointing at nothing, which the guard refuses and rightly
// so. The site's own links are written by the templates and are not affected.
type unlinked struct{}

func (unlinked) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, renderFigure)
	reg.Register(ast.KindLink, renderLabel)
	reg.Register(ast.KindAutoLink, renderURL)
}

// An image is the mark it meant. The caption is the part worth keeping, and the
// children that hold it are left to the default renderer rather than pulled out
// as text, so that the escaping is the same escaping every other inline gets.
func renderFigure(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString(`<span class="mark figure">`)
	} else {
		_, _ = w.WriteString(`</span>`)
	}
	return ast.WalkContinue, nil
}

// A link is its label and nothing else.
func renderLabel(_ util.BufWriter, _ []byte, _ ast.Node, _ bool) (ast.WalkStatus, error) {
	return ast.WalkContinue, nil
}

// An autolink has no label, so what it printed is the address itself.
func renderURL(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	link, ok := node.(*ast.AutoLink)
	if !ok {
		return ast.WalkContinue, nil
	}
	_, _ = w.Write(util.EscapeHTML(link.URL(source)))
	return ast.WalkContinue, nil
}

// marker is one of the five structural marks the OCR prompt asks for, and what
// the site does with it.
//
// These are not decoration and they are not text the magazine printed. They are
// the reading's record of the shape of the sheet, and dropping them would lose
// the only evidence the site has that a paragraph refers to a picture nobody
// can see. So each one becomes an element with a class, and what it looks like
// is the stylesheet's business.
//
// A figure is the one that matters. The corpus holds the text of the magazine
// and none of its pictures, and an article that says look at the figure is
// unreadable if the site is silent about where the figure was.
var markerOf = map[string]string{
	"⟦figure⟧": `<span class="mark figure" title="a figure stood here"></span>`,
	"⟦column⟧": `<span class="mark column"></span>`,
	"⟦rubric⟧": `<span class="mark rubric"></span>`,
	// An unnumbered sheet is the normal state of a cover or a full page plate.
	// There is no number to show, so there is nothing to say.
	"⟦folio none⟧": "",
}

func markers(page string) string {
	for mark, markup := range markerOf {
		page = strings.ReplaceAll(page, mark, markup)
	}
	return folios(page)
}

// folios turns a numbered folio mark into the printed page number.
//
// The number is where the sheet broke in the middle of an article, which is the
// one piece of the printed object a reader might want to cite, so it is an
// anchor as well as a mark.
func folios(page string) string {
	const open = "⟦folio "
	var out strings.Builder
	rest := page
	for {
		before, after, found := strings.Cut(rest, open)
		if !found {
			out.WriteString(rest)
			return out.String()
		}
		number, tail, closed := strings.Cut(after, "⟧")
		if !closed {
			out.WriteString(before)
			out.WriteString(open)
			return out.String() + after
		}
		out.WriteString(before)
		if n := strings.TrimSpace(number); n != "" && safeFolio(n) {
			fmt.Fprintf(&out, `<span class="mark folio" id="folio-%s">%s</span>`, n, n)
		}
		rest = tail
	}
}

// safeFolio keeps anything that is not a plain printed number out of an id and
// out of the markup. The model writes what it reads off the foot of the sheet
// and what it reads there is sometimes not a number at all.
func safeFolio(n string) bool {
	if len(n) > 8 {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
