package publish_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/publish"
)

// The two scripts the site carries are its own files under assets, and a page
// may load them. Any other script, in a file or on a page, is refused.
func TestTheGuardLetsThroughTheSitesOwnScriptsAndNothingElse(t *testing.T) {
	for _, test := range []struct {
		name, file, data string
		ok               bool
	}{
		{"the site script", "assets/site.js", "x()", true},
		{"the theme script", "assets/theme.js", "x()", true},
		{"the headers file", "_headers", "/*\n  X: y", true},
		{"a page loading the site script", "ru/index.html", `<script defer src="../assets/site.js"></script>`, true},
		{"the missing page with a rooted path", "404.html", `<script src="/kvant/assets/theme.js"></script>`, true},

		{"a script of somebody else's", "assets/tracker.js", "x()", false},
		{"a script outside assets", "ru/site.js", "x()", false},
		{"a page loading a script from elsewhere", "ru/index.html", `<script src="https://cdn.example/a.js"></script>`, false},
		{"a page with inline code", "ru/index.html", `<script defer src="../assets/site.js"></script><script>x()</script>`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := publish.Guard(test.file, []byte(test.data))
			if test.ok && err != nil {
				t.Errorf("refused %s: %v", test.file, err)
			}
			if !test.ok && err == nil {
				t.Errorf("wrote %s, want it refused", test.file)
			}
		})
	}
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Both hosts serve x.html at x and x/index.html at x/, and the site links that
// way, so the checker resolves a link the way the hosts will.
func TestTheLinkCheckerResolvesAddressesTheWayTheHostsDo(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"index.html":       `<a href="ru/">ok</a> <a href="ru/a">ok</a> <a href="ru/a#x">ok</a> <a href="#top">ok</a>`,
		"ru/index.html":    `<a href="../ru/b">missing</a> <a href="../empty/">no index</a> <a href="../ru/index">ok</a>`,
		"ru/a.html":        `<a href="./">ok</a>`,
		"empty/notes.html": `<a href="../">ok</a>`,
	})
	broken, err := publish.CheckLinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 2 {
		t.Fatalf("found %d broken references, want 2: %v", len(broken), broken)
	}
	all := ""
	for _, b := range broken {
		all += b.Error() + "\n"
	}
	for _, want := range []string{"ru/b", "empty/"} {
		if !strings.Contains(all, want) {
			t.Errorf("the checker did not report %s: %s", want, all)
		}
	}
}

func open(t *testing.T, root string) *corpus.Corpus {
	t.Helper()
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The mirror on GitHub Pages has to stay under a gigabyte, and the sheets are
// half the site. Without them every article is still there and nothing links
// to a page that was not written.
func TestABuildWithoutSheetsLeavesNoLinkToThem(t *testing.T) {
	root, _, full := build(t)
	out := t.TempDir()
	stats, err := (&publish.Site{Corpus: open(t, root), Out: out, NoSheets: true}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files >= full.Files {
		t.Errorf("the build without sheets wrote %d files, the full one %d", stats.Files, full.Files)
	}
	if _, err := os.Stat(filepath.Join(out, "ru", "1975", "04", "pages.html")); err == nil {
		t.Error("the build without sheets wrote the sheets")
	}
	if !strings.Contains(read(t, out, "ru/1975/04/01_nachalo.html"), "Начало") {
		t.Error("the build without sheets lost an article")
	}
	// Build already fails on a broken link; this says which promise it keeps.
	if strings.Contains(read(t, out, "ru/1975/04/index.html"), "/pages") {
		t.Error("the issue page links the sheets it does not have")
	}
}

// A host with a limit on files or bytes rejects a deploy halfway, which is
// worse than not starting. So the build checks first and fails on its own.
func TestABuildOverItsBudgetFails(t *testing.T) {
	root, _, _ := build(t)
	c := open(t, root)
	if _, err := (&publish.Site{Corpus: c, Out: t.TempDir(), MaxFiles: 10}).Build(); err == nil {
		t.Error("a build over its file budget passed")
	}
	if _, err := (&publish.Site{Corpus: c, Out: t.TempDir(), MaxBytes: 1000}).Build(); err == nil {
		t.Error("a build over its byte budget passed")
	}
	if _, err := (&publish.Site{Corpus: c, Out: t.TempDir(), MaxFiles: 1000, MaxBytes: 1 << 30}).Build(); err != nil {
		t.Errorf("a build inside its budget failed: %v", err)
	}
}

// A translation is the same article in another language. It links to the
// original, the switcher moves between the two, and what the translator left
// out of the front matter is taken from the original instead of going missing.
func TestATranslationIsLinkedToItsOriginal(t *testing.T) {
	root, _, _ := build(t)
	key, err := corpus.NewIssueKey(1975, "4")
	if err != nil {
		t.Fatal(err)
	}
	front := corpus.ArticleFront{Title: "The beginning", PageFirst: 11, PageLast: 12}
	front.ID = key.String() + "_01_nachalo"
	front.Issue, front.Year, front.Number, front.Lang = key.String(), 1975, "4", "en"
	c := &corpus.Corpus{Root: root}
	if err := corpus.Save(filepath.Join(c.IssueDir("en", key), "articles", "01_nachalo.md"), &front, "Text.\n"); err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	stats, err := (&publish.Site{Corpus: open(t, root), Out: out}).Build()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Langs != 2 {
		t.Errorf("published %d languages, want 2", stats.Langs)
	}

	en := read(t, out, "en/1975/04/01_nachalo.html")
	for _, want := range []string{
		"The beginning",
		`href="../../../en/authors/i-ivanov"`, // the author, from the original
		`href="../../../ru/1975/04/01_nachalo"`,
		`hreflang="ru"`,
	} {
		if !strings.Contains(en, want) {
			t.Errorf("the translation has no %s in it", want)
		}
	}
	ru := read(t, out, "ru/1975/04/01_nachalo.html")
	if !strings.Contains(ru, `href="../../../en/1975/04/01_nachalo"`) {
		t.Error("the original does not offer its translation")
	}
	// Where there is no translation the switcher goes to that language's home.
	if !strings.Contains(read(t, out, "ru/1976/01/01_prodolzhenie.html"), `href="../../../en/"`) {
		t.Error("an untranslated page does not offer the other language's home")
	}
}

// kvant serve has to answer the way the hosts do or a link that works on the
// laptop breaks after the deploy, or the other way round.
func TestTheLocalServerAnswersTheWayTheHostsDo(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"index.html":    "home",
		"404.html":      "missing",
		"ru/index.html": "ru home",
		"ru/a.html":     "page a",
		"assets/x.css":  "css",
	})
	srv := httptest.NewServer(publish.Handler(dir))
	defer srv.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, test := range []struct {
		path string
		code int
		body string
	}{
		{"/", 200, "home"},
		{"/ru/", 200, "ru home"},
		{"/ru", 301, ""},
		{"/ru/a", 200, "page a"},
		{"/assets/x.css", 200, "css"},
		{"/ru/b", 404, "missing"},
	} {
		resp, err := client.Get(srv.URL + test.path)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != test.code {
			t.Errorf("%s answered %d, want %d", test.path, resp.StatusCode, test.code)
		}
		if test.body != "" && !strings.Contains(b.String(), test.body) {
			t.Errorf("%s answered %q, want %q", test.path, b.String(), test.body)
		}
	}
}
