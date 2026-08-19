package publish_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/publish"
)

// build writes a small corpus, publishes it, and hands back where it went.
//
// It is a real corpus written through corpus.Save rather than a directory of
// handmade files, so a change to the front matter schema breaks this test where
// it should break it, at the front matter, instead of leaving the site building
// happily out of a shape the corpus no longer has.
func build(t *testing.T) (root, out string, stats publish.Stats) {
	t.Helper()
	root, out = t.TempDir(), t.TempDir()

	page(t, root, 1975, "4", 11, "27", "Начало статьи с формулой $x_1 + x_2 = 3$.\n\n⟦folio 27⟧")
	page(t, root, 1975, "4", 12, "28", "Продолжение.\n\n⟦figure⟧\n\n⟦folio 28⟧")
	page(t, root, 1975, "10", 3, "", "Обложка.\n\n⟦folio none⟧")
	page(t, root, 1976, "1", 5, "3", "Другой год.")

	article(t, root, 1975, "4", "01_nachalo", corpus.ArticleFront{
		Title:     "Начало",
		Authors:   []string{"И. Иванов"},
		Rubric:    "osnovnye-stati",
		PageFirst: 11,
		PageLast:  12,
	}, "Текст статьи с выключенной формулой:\n\n$$\\int_0^1 x\\,dx = \\frac{1}{2}$$\n")

	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	site := &publish.Site{Corpus: c, Out: out}
	stats, err = site.Build()
	if err != nil {
		t.Fatal(err)
	}
	return root, out, stats
}

func page(t *testing.T, root string, year int, number string, index int, label, body string) {
	t.Helper()
	key, err := corpus.NewIssueKey(year, number)
	if err != nil {
		t.Fatal(err)
	}
	front := &corpus.PageFront{
		Issue:     key.String(),
		Year:      year,
		Number:    number,
		PageIndex: index,
		PageLabel: label,
	}
	front.Lang = corpus.DefaultLang
	front.Extraction = corpus.ExtractionVision
	front.ExtractionModel = "gpt-5"
	c := &corpus.Corpus{Root: root}
	path := c.PagePath(corpus.DefaultLang, corpus.PageID{Issue: key, Index: index})
	if err := corpus.Save(path, front, body); err != nil {
		t.Fatal(err)
	}
}

func article(t *testing.T, root string, year int, number, name string, front corpus.ArticleFront, body string) {
	t.Helper()
	key, err := corpus.NewIssueKey(year, number)
	if err != nil {
		t.Fatal(err)
	}
	front.ID = key.String() + "_" + name
	front.Issue = key.String()
	front.Year = year
	front.Number = number
	front.Lang = corpus.DefaultLang
	c := &corpus.Corpus{Root: root}
	path := filepath.Join(c.IssueDir(corpus.DefaultLang, key), "articles", name+".md")
	if err := corpus.Save(path, &front, body); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, out, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The site is a view of the corpus and the corpus is what has been read, so a
// build publishes the issues that have a directory and says how many that was.
func TestASiteIsBuiltOutOfWhatWasRead(t *testing.T) {
	_, out, stats := build(t)

	if stats.Issues != 3 {
		t.Errorf("published %d issues, want 3", stats.Issues)
	}
	if stats.Pages != 4 {
		t.Errorf("published %d pages, want 4", stats.Pages)
	}
	if stats.Articles != 1 {
		t.Errorf("published %d articles, want 1", stats.Articles)
	}

	for _, name := range []string{
		"index.html",
		"1975/index.html",
		"1975/04/index.html",
		"1975/04/pages/0011.html",
		"1975/04/articles/01_nachalo.html",
		"1975/10/pages/0003.html",
		"1976/01/index.html",
		"assets/site.css",
		"assets/katex.min.css",
	} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(name))); err != nil {
			t.Errorf("the build did not write %s", name)
		}
	}
}

// An article is a view of pages rather than a replacement for them, so both are
// on the issue page. Anything no article claimed survives only on the sheet it
// was printed on, and a site that listed articles alone would lose it.
func TestTheIssuePageListsBothTheArticlesAndTheSheets(t *testing.T) {
	_, out, _ := build(t)
	page := read(t, out, "1975/04/index.html")

	for _, want := range []string{
		`href="articles/01_nachalo.html"`, "Начало",
		`href="pages/0011.html"`, "Лист 11",
		`href="pages/0012.html"`, "Лист 12",
		// The file records the slug the assembler gave it. A reader is shown the
		// section the way the magazine printed it.
		"Основные статьи",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the issue page has no %s in it", want)
		}
	}
}

// A number nobody has assembled into articles yet is most of the corpus. It
// still publishes, because its sheets have been read, and it says so rather
// than showing an empty heading.
func TestAnIssueWithNoArticlesStillPublishesItsSheets(t *testing.T) {
	_, out, _ := build(t)
	page := read(t, out, "1975/10/index.html")

	if !strings.Contains(page, `href="pages/0003.html"`) {
		t.Error("the issue page does not link the sheet that was read")
	}
	if !strings.Contains(page, "номер ещё не разобран на статьи") {
		t.Error("the issue page says nothing about having no articles")
	}
}

// The mathematics is typeset at build time and not in the browser, which is the
// whole reason the site needs no JavaScript.
func TestTheMathematicsIsTypesetIntoTheFile(t *testing.T) {
	_, out, _ := build(t)

	sheet := read(t, out, "1975/04/pages/0011.html")
	if !strings.Contains(sheet, "katex") {
		t.Error("the inline formula was not typeset")
	}
	if strings.Contains(sheet, "$x_1") {
		t.Error("the TeX source is still on the page")
	}

	article := read(t, out, "1975/04/articles/01_nachalo.html")
	if !strings.Contains(article, "katex-display") {
		t.Error("the display formula was not typeset as a display")
	}
}

// How a page was read is part of what it is. A sheet lifted out of a born
// digital PDF and a sheet a model read off a scan are not the same kind of
// evidence, and a reader comparing two readings should not have to go to the
// repository to tell them apart.
func TestASheetSaysHowItWasRead(t *testing.T) {
	_, out, _ := build(t)
	sheet := read(t, out, "1975/04/pages/0011.html")

	for _, want := range []string{"vision", "gpt-5", "27"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the sheet does not say %s", want)
		}
	}
}

// Kvant is still being published. The text was read off the scans and written
// down here and that is ours to show. Nothing else in the build is, and the
// guard is on the write path so that this holds for every view somebody adds
// later and not only for the ones written today.
func TestNothingButTextReachesTheOutput(t *testing.T) {
	_, out, _ := build(t)

	err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		if err := publish.Guard(filepath.ToSlash(name), data); err != nil {
			t.Errorf("the build published %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Nothing here reads the scan cache and nothing here reads the clock, so two
// builds of one checkout are the same site. That is what makes it safe to
// rebuild the whole thing on every push and see the diff as a real change.
func TestTwoBuildsOfOneCorpusAgree(t *testing.T) {
	root, first, _ := build(t)

	second := t.TempDir()
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&publish.Site{Corpus: c, Out: second}).Build(); err != nil {
		t.Fatal(err)
	}

	err = filepath.Walk(first, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		name, err := filepath.Rel(first, path)
		if err != nil {
			return err
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			t.Errorf("%s came out differently the second time", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The corpus is twenty two thousand pages a model read off scans and a few of
// them will always hold TeX that is not TeX. Losing a page of good text over
// one formula is the wrong trade, so the build publishes the page, marks the
// formula, and says how many there were. A run that wants the stricter answer
// asks for it.
func TestABrokenFormulaIsMarkedAndCountedRatherThanFatal(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	page(t, root, 1975, "4", 11, "27", "Формула $\\frac{1}$ и обычный текст.\n")

	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := (&publish.Site{Corpus: c, Out: out}).Build()
	if err != nil {
		t.Fatalf("the build stopped over one formula: %v", err)
	}
	if stats.BadMath != 1 {
		t.Errorf("counted %d broken formulas, want 1", stats.BadMath)
	}

	sheet := read(t, out, "1975/04/pages/0011.html")
	if !strings.Contains(sheet, "обычный текст") {
		t.Error("the rest of the page went missing with the formula")
	}
	if !strings.Contains(sheet, "tex-failed") {
		t.Error("the page does not show that the reading failed there")
	}

	if _, err := (&publish.Site{Corpus: c, Out: t.TempDir(), Strict: true}).Build(); err == nil {
		t.Error("a strict build passed with a formula it could not typeset")
	}
}

// A build of nothing is a mistake somebody made with a path, not an empty site
// worth publishing.
func TestPublishingACorpusWithNothingInItIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "content", "ru"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&publish.Site{Corpus: c, Out: t.TempDir()}).Build(); err == nil {
		t.Error("the build published an empty corpus")
	}
}

// Nothing on the site needs a browser to run anything, and that is a
// requirement of the milestone rather than a preference. A reader with
// scripting off sees the finished article, the typeset mathematics and every
// link.
func TestThereIsNoJavaScriptAnywhereOnTheSite(t *testing.T) {
	_, out, _ := build(t)

	err := filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range []string{"<script", "onclick", "javascript:"} {
			if strings.Contains(strings.ToLower(string(data)), bad) {
				t.Errorf("%s carries %s", path, bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
