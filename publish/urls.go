package publish

import (
	"path"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/rubric"
)

// The addresses of the site, every one of them built here and nowhere else.
//
//	/                              the languages
//	/ru/                           the archive in one language
//	/ru/1975/                      a year
//	/ru/1975/01/                   an issue, its contents
//	/ru/1975/01/pages              every sheet of the issue, one under the other
//	/ru/1975/01/06_bronshteyn-…    an article
//	/ru/rubrics/  /ru/rubrics/smes
//	/ru/authors/  /ru/authors/smorodinskiy-ya-a
//	/ru/problems/ /ru/problems/M301
//	/ru/search
//	/tags/        /tags/K#KS54     the permanent address of whatever KS54 names
//
// An address here is a path from the site root, with no leading slash: "x/" for
// a directory and "x" for a page, which both hosts serve out of x/index.html
// and x.html. The pages link to each other relatively, so the same build works
// at the root of kvant.tamnd.com and under /kvant/ on GitHub Pages.
//
// A tag has no file of its own. The corpus holds one tag per article and per
// problem, which is as many files again as there are articles, and Cloudflare
// Pages stops at twenty thousand. So the tags are filed by their first
// character into thirty six pages, and a tag's address is its row on one of
// them. That address is still a function of the tag alone, which is the whole
// of what permanent asks.

// file is where an address is written.
func file(addr string) string {
	switch {
	case addr == "":
		return "index.html"
	case strings.HasSuffix(addr, "/"):
		return addr + "index.html"
	}
	return addr + ".html"
}

// rootOf is the way back to the site root from the file an address is written
// to.
func rootOf(addr string) string {
	return strings.Repeat("../", strings.Count(file(addr), "/"))
}

func langURL(lang string) string { return lang + "/" }

func yearURL(lang string, year int) string { return lang + "/" + strconv.Itoa(year) + "/" }

func issueURL(lang string, key corpus.IssueKey) string {
	return yearURL(lang, key.Year) + pad(key.Number) + "/"
}

func sheetsURL(lang string, key corpus.IssueKey) string { return issueURL(lang, key) + "pages" }

// sheetURL is one sheet, as its place on the page that holds all of them.
func sheetURL(lang string, key corpus.IssueKey, index int) string {
	return sheetsURL(lang, key) + "#" + sheetID(index)
}

func sheetID(index int) string { return "p" + strconv.Itoa(index) }

// articleURL is an article by its file name, which carries the slug the
// assembler gave it and so does not move when the printed title is retyped.
func articleURL(lang string, key corpus.IssueKey, name string) string {
	return issueURL(lang, key) + strings.TrimSuffix(name, ".md")
}

func rubricsURL(lang string) string { return lang + "/rubrics/" }

func rubricURL(lang, slug string) string { return rubricsURL(lang) + slug }

func authorsURL(lang string) string { return lang + "/authors/" }

func authorURL(lang, name string) string { return authorsURL(lang) + rubric.Slug(name) }

func problemsURL(lang string) string { return lang + "/problems/" }

func problemURL(lang string, id corpus.ProblemID) string {
	return problemsURL(lang) + id.String()
}

func searchURL(lang string) string { return lang + "/search" }

func searchIndexURL(lang string) string { return lang + "/search.json" }

func tagsURL() string { return "tags/" }

// tagShard is the page a tag is filed on.
func tagShard(tag string) string {
	if tag == "" {
		return ""
	}
	return tagsURL() + strings.ToUpper(tag[:1])
}

// tagURL is the permanent address of a tag.
func tagURL(tag string) string { return tagShard(tag) + "#" + tag }

// href is an address as a link from the page at another one.
func href(from, to string) string {
	h := rootOf(from) + to
	if h == "" {
		return "./"
	}
	return h
}

// up is the directory an address is in, as an address.
func up(addr string) string {
	addr = strings.TrimSuffix(addr, "/")
	dir := path.Dir(addr)
	if dir == "." {
		return ""
	}
	return dir + "/"
}
