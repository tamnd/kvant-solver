package publish_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/publish"
)

// The section index is built out of what the corpus has, not out of the table
// of every section the magazine ever ran. A page for a section this corpus has
// not reached yet would be an empty page a reader clicked on for nothing.
func TestTheRubricIndexHoldsTheSectionsThatHaveSomethingInThem(t *testing.T) {
	_, out, _ := build(t)
	index := read(t, out, "ru/rubrics/index.html")

	for _, want := range []string{
		`href="../../ru/rubrics/osnovnye-stati"`, "Основные статьи",
		`href="../../ru/rubrics/matematicheskiy-kruzhok"`, "Математический кружок",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("the rubric index has no %s in it", want)
		}
	}
	if strings.Contains(index, "shahmatnaya-stranichka") {
		t.Error("the rubric index lists a section nothing in this corpus is under")
	}

	section := read(t, out, "ru/rubrics/osnovnye-stati.html")
	if !strings.Contains(section, "Начало") {
		t.Error("the section page does not list the article that ran under it")
	}
	if strings.Contains(section, "Продолжение") {
		t.Error("the section page lists an article from a different section")
	}
}

// An author page is the cut the shelf order cannot give you: everything one
// person wrote, across every year they wrote it.
func TestAnAuthorPageGathersTheirWorkFromEveryIssue(t *testing.T) {
	_, out, _ := build(t)
	page := read(t, out, "ru/authors/i-ivanov.html")

	for _, want := range []string{"Начало", "Продолжение", "1975 · 4", "1976 · 1"} {
		if !strings.Contains(page, want) {
			t.Errorf("the author page has no %s in it", want)
		}
	}

	solo := read(t, out, "ru/authors/p-petrov.html")
	if strings.Contains(solo, "Начало") {
		t.Error("the author page lists an article this person did not write")
	}
}

// Every name over an article is a link to that author's page, because an index
// nothing points at is an index nobody finds.
func TestAnArticleLinksToItsAuthorItsSectionAndItsTag(t *testing.T) {
	_, out, _ := build(t)
	article := read(t, out, "ru/1975/04/01_nachalo.html")

	for _, want := range []string{
		`href="../../../ru/authors/i-ivanov"`,
		`href="../../../ru/rubrics/osnovnye-stati"`,
		`href="../../../tags/Z#ZSAK"`,
		`href="../../../ru/1975/04/pages#p11"`,
	} {
		if !strings.Contains(article, want) {
			t.Errorf("the article does not carry %s", want)
		}
	}
}

// A tag is a permanent address and the path of the thing it names is not: the
// file carries a slug made out of the title, and a retitled article moves. So
// the tag gets a row of its own that points at wherever the object is now.
// The rows are split on the first character, because the corpus already has
// five and a half thousand tags and a file for each is a file budget spent on
// nothing, and one page of them all is not a page.
func TestATagIsAPermanentAddressForWhateverItNames(t *testing.T) {
	_, out, _ := build(t)
	shard := read(t, out, "tags/Z.html")

	if !strings.Contains(shard, `id="ZSAK" data-go="../ru/1975/04/01_nachalo"`) {
		t.Error("the tag row does not point at the article it names")
	}
	if !strings.Contains(shard, "Начало") {
		t.Error("the tag row does not say what it names")
	}

	index := read(t, out, "tags/index.html")
	if !strings.Contains(index, `href="../tags/Z"`) {
		t.Error("the tag index does not reach the tags starting with Z")
	}
	if strings.Contains(shard, "Q7T2") {
		t.Error("the shard lists a tag that starts with something else")
	}
}

// A tag names one thing. Two objects carrying the same one is a fault in the
// corpus and the build says so rather than publishing whichever it saw last.
func TestATagOnTwoThingsStopsTheBuild(t *testing.T) {
	root, out := t.TempDir(), t.TempDir()
	page(t, root, 1975, "4", 11, "27", "Лист.")
	article(t, root, 1975, "4", "01_odna", corpus.ArticleFront{
		Title: "Одна", PageFirst: 11, PageLast: 11, Tag: "ZSAK",
	}, "Текст.")
	article(t, root, 1975, "4", "02_drugaya", corpus.ArticleFront{
		Title: "Другая", PageFirst: 11, PageLast: 11, Tag: "ZSAK",
	}, "Текст.")

	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&publish.Site{Corpus: c, Out: out}).Build()
	if err == nil {
		t.Fatal("the build published one tag on two articles")
	}
	if !strings.Contains(err.Error(), "ZSAK") {
		t.Errorf("the error does not name the tag: %v", err)
	}
}

// A problem belongs to the issue that posed it and the issue that printed the
// solution, which are usually two to four apart, so it is filed under neither.
// Its number is the name the olympiad literature cites it by and that is the
// path.
func TestAProblemStandsOnItsNumberAndLinksBothOfItsIssues(t *testing.T) {
	_, out, _ := build(t)
	page := read(t, out, "ru/problems/M301.html")

	for _, want := range []string{
		"Задача M301",
		`href="../../ru/1975/04/02_zadachi"`,
		`href="../../ru/1975/10/"`,
		"41-42", "51-53",
		"С. Охитин",
		`href="../../tags/0#0IZG"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the problem page has no %s in it", want)
		}
	}
}

// A Задачник article links every problem its heading names that has a page,
// and only those.
func TestAZadachnikArticleLinksItsProblems(t *testing.T) {
	_, out, _ := build(t)
	page := read(t, out, "ru/1975/04/02_zadachi.html")
	if !strings.Contains(page, `href="../../../ru/problems/M301"`) {
		t.Error("the article does not link the problem it posed")
	}
	if strings.Contains(page, "problems/M302") {
		t.Error("the article links a problem that has no page")
	}
}

// Most of the corpus is problems whose issues have not been read yet. They still
// publish, because the number is what a citation lands on, and the issue is
// named without being linked so that the site never points at a directory that
// is not there.
func TestAProblemFromAnIssueNobodyHasReadNamesItWithoutLinkingIt(t *testing.T) {
	_, out, _ := build(t)
	page := read(t, out, "ru/problems/F372.html")

	if !strings.Contains(page, "Квант 1988, №11-12") {
		t.Error("the problem page does not say which issue posed it")
	}
	if strings.Contains(page, `href="../../ru/1988/`) {
		t.Error("the problem page links an issue the site does not have")
	}
	if strings.Contains(page, "Решение:") {
		t.Error("the problem page claims a solution the magazine never printed")
	}
}

// Every reference on the built site is a sum over the depth of the file it is
// written in, and a sum written in six places will be wrong in one of them. The
// only way to know is to walk the output and open what it points at.
func TestEveryLinkOnTheBuiltSiteLandsOnAFile(t *testing.T) {
	_, out, _ := build(t)

	broken, err := publish.CheckLinks(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range broken {
		t.Errorf("%v", b)
	}
}

// The checker has to actually fail, or the test above is a test of nothing.
func TestTheLinkCheckerFindsALinkThatGoesNowhere(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", `<a href="1975/04/index.html">есть</a>`)
	write("1975/04/index.html", `<a href="pages/0011.html">нет</a> <a href="../../index.html">есть</a>`)
	write("absolute.html", `<a href="/1975/04/index.html">корень</a>`)
	write("away.html", `<a href="https://example.com/">наружу</a>`)

	broken, err := publish.CheckLinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 3 {
		t.Fatalf("found %d broken references, want 3: %v", len(broken), broken)
	}
	for _, want := range []string{"1975/04/pages/0011.html", "is absolute", "leaves the site"} {
		found := false
		for _, b := range broken {
			if strings.Contains(b.Error(), want) {
				found = true
			}
		}
		if !found {
			t.Errorf("the checker did not report %s: %v", want, broken)
		}
	}
}

// Building the index is the hard half of search and it is done once, here,
// where every document is already open. It holds titles, authors, sections and
// tags and no body text, because the bodies were twenty megabytes of download
// for a lookup that is nearly always by title or by author.
func TestTheSearchIndexHoldsEveryDocumentTheSitePublished(t *testing.T) {
	_, out, stats := build(t)

	var index struct {
		Count   int              `json:"count"`
		Records []publish.Record `json:"records"`
	}
	raw := read(t, out, "ru/search.json")
	if err := json.Unmarshal([]byte(raw), &index); err != nil {
		t.Fatal(err)
	}

	want := stats.Articles + stats.Problems
	if index.Count != want || len(index.Records) != want {
		t.Fatalf("the index holds %d records, want %d", index.Count, want)
	}

	byHref := map[string]publish.Record{}
	for _, r := range index.Records {
		byHref[r.H] = r
	}
	got, ok := byHref["ru/1975/04/01_nachalo"]
	if !ok {
		t.Fatalf("the article is not in the index: %v", index.Records)
	}
	if got.K != "a" || got.T != "Начало" || got.I != "1975 · 4" {
		t.Errorf("the article record is %+v", got)
	}
	if got.R != "Основные статьи" || got.G != "ZSAK" || got.A != "И. Иванов" {
		t.Errorf("the article record lost its section, tag or author: %+v", got)
	}
	if p, ok := byHref["ru/problems/M301"]; !ok || p.K != "p" {
		t.Errorf("the problems are not in the index: %+v", p)
	}
	if strings.Contains(raw, "Текст статьи") {
		t.Error("the index carries body text")
	}
}
