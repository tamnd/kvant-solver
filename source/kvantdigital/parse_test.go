package kvantdigital

import (
	"os"
	"strings"
	"testing"
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestParseIssuesIndex(t *testing.T) {
	// The fixture is two year boxes lifted out of the real index page. 1975 is
	// an ordinary twelve issue year. 1993 is the year the magazine nearly went
	// under and shipped four double issues and nothing else.
	refs, err := ParseIssuesIndex(open(t, "issues_index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 16 {
		t.Fatalf("got %d issues, want 12 for 1975 and 4 for 1993", len(refs))
	}

	byKey := map[string]IssueRef{}
	for _, r := range refs {
		byKey[r.Key] = r
	}
	first, ok := byKey["kvant_1975_1"]
	if !ok {
		t.Fatal("kvant_1975_1 is missing")
	}
	if first.Year != 1975 || first.Number != "1" {
		t.Errorf("first issue parsed as %d number %q", first.Year, first.Number)
	}
	if first.URL != "https://www.kvant.digital/issues/1975/1/" {
		t.Errorf("url is %q", first.URL)
	}
	// Doubles are the case that breaks a naive integer parse. 1993 has nothing
	// else, so if these were dropped the year would come back empty.
	for _, key := range []string{"kvant_1993_1-2", "kvant_1993_3-4", "kvant_1993_9-10", "kvant_1993_11-12"} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("%s did not come through with its number intact", key)
		}
	}
}

func TestParseIssuesIndexRefusesAPageWithNoIssues(t *testing.T) {
	_, err := ParseIssuesIndex(strings.NewReader("<html><body><p>nothing here</p></body></html>"))
	if err == nil {
		t.Fatal("a page with no issue links should be an error, not an empty list")
	}
}

func TestParseIssue(t *testing.T) {
	iss, err := ParseIssue(open(t, "issue_1975_1.html"), 1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	if iss.Key != "kvant_1975_1" {
		t.Errorf("key is %q", iss.Key)
	}
	if iss.ISSN != "0130-2221" {
		t.Errorf("issn is %q", iss.ISSN)
	}
	// The page count is the number the page sweep has to reach, so it is worth
	// digging out of the bibliographic line.
	if iss.PageCount != 80 {
		t.Errorf("page count is %d, the description says 80 pages", iss.PageCount)
	}
	if !strings.Contains(iss.Title, "1975") {
		t.Errorf("title is %q", iss.Title)
	}
	if len(iss.Rows) == 0 {
		t.Fatal("no contents rows")
	}
	// The scan manifest comes off the same page as the contents, so an issue
	// costs one request rather than two.
	if len(iss.Sheets) != 8 {
		t.Errorf("%d sheets, the fixture has 8 thumbnails", len(iss.Sheets))
	}
}

func TestFirstContentsRow(t *testing.T) {
	iss, err := ParseIssue(open(t, "issue_1975_1.html"), 1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	row := iss.Rows[0]
	if row.Rubric != "Основные статьи" {
		t.Errorf("rubric is %q", row.Rubric)
	}
	if row.Title != "Эллипс" {
		t.Errorf("title is %q, the author name should have been split off", row.Title)
	}
	// The site writes the initials with non breaking spaces, which have to be
	// folded or every author string carries invisible junk.
	if row.Authors != "Бронштейн И. Н." {
		t.Errorf("authors is %q", row.Authors)
	}
	if !strings.HasSuffix(row.Slug, "-d2e5763b") {
		t.Errorf("slug is %q, it should end in the eight hex characters the site appends", row.Slug)
	}
}

func TestPrintedPageAndSheetAreDifferentNumbers(t *testing.T) {
	iss, err := ParseIssue(open(t, "issue_1975_1.html"), 1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	row := iss.Rows[0]
	if row.Page != 2 {
		t.Errorf("printed page is %d, the contents say 2", row.Page)
	}
	if row.Sheet != 3 {
		t.Errorf("sheet is %d, the viewer link says p3", row.Sheet)
	}
	// This is the whole reason both numbers are kept. The covers are scanned
	// but not numbered, so asking for sheet 2 when you want printed page 2
	// gets you the wrong page for the entire issue.
	off, ok := iss.PageOffset()
	if !ok || off != 1 {
		t.Errorf("offset is %d ok %v, want 1", off, ok)
	}
}

func TestTextAvailabilityIsReadFromTheClassNotTheGlyph(t *testing.T) {
	iss, err := ParseIssue(open(t, "issue_1975_1.html"), 1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	// Every row carries the glyph, so a parser keyed on the glyph would report
	// that the site holds text for the whole issue. It does not. In 1975 the
	// only rows with text are the problem sets and their solutions, which is
	// worth knowing on its own: those are the rows the solver cares about most
	// and they do not have to go through OCR.
	withText, withoutText := 0, 0
	for _, r := range iss.Rows {
		if r.HasText {
			withText++
			if !strings.Contains(r.Rubric, "Задачник") && !strings.Contains(r.Rubric, "младших") {
				t.Errorf("row %q in rubric %q claims text, in 1975 only the problem rows have it", r.Title, r.Rubric)
			}
			continue
		}
		withoutText++
	}
	if withText == 0 || withoutText == 0 {
		t.Fatalf("%d rows with text and %d without, the class is not being read", withText, withoutText)
	}
}

func TestInvisibleTypesettingCharactersAreStripped(t *testing.T) {
	iss, err := ParseIssue(open(t, "issue_1975_1.html"), 1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	// The contents write a problem range with zero width joiners around the
	// dash so the browser will not break the line inside it. Left in, they end
	// up in identifiers and in anything anyone tries to search for.
	var found bool
	for _, r := range iss.Rows {
		if strings.ContainsAny(r.Title, "\u200b\u200c\u200d\u2060\ufeff\u00ad") {
			t.Errorf("title %q still has an invisible character in it", r.Title)
		}
		if r.Title == "Задачи М301—М305; Ф313—Ф317" {
			found = true
		}
	}
	if !found {
		t.Error("the problem range row is not in the fixture, so this test proves nothing")
	}
}

func TestURLBuilders(t *testing.T) {
	if got := IssueURL(1976, "5-6"); got != "https://www.kvant.digital/issues/1976/5-6/" {
		t.Errorf("IssueURL: %s", got)
	}
	// There is no builder for a viewer URL. robots.txt disallows /view.
	if got := ScanURL("kvant_1975_1", "0001"); got != "https://www.kvant.digital/data/kvant_1975_1/jpg/0001.jpg" {
		t.Errorf("ScanURL: %s", got)
	}
	if got := ProblemURL("M", 1234); got != "https://www.kvant.digital/problems/m1234/" {
		t.Errorf("ProblemURL: %s", got)
	}
}

func TestSplitIssueKey(t *testing.T) {
	for _, tc := range []struct {
		key    string
		year   int
		number string
		ok     bool
	}{
		{"kvant_1975_1", 1975, "1", true},
		{"kvant_1976_5-6", 1976, "5-6", true},
		{"kvant_197_1", 0, "", false},
		{"not a key", 0, "", false},
	} {
		year, number, ok := SplitIssueKey(tc.key)
		if year != tc.year || number != tc.number || ok != tc.ok {
			t.Errorf("SplitIssueKey(%q) = %d %q %v", tc.key, year, number, ok)
		}
	}
}

func TestSlugExtraction(t *testing.T) {
	href := "https://www.kvant.digital/issues/1975/1/bronshteyn-ellips-d2e5763b/"
	if got := ArticleSlug(href); got != "bronshteyn-ellips-d2e5763b" {
		t.Errorf("ArticleSlug: %q", got)
	}
	if got := ArticleSlug("https://www.kvant.digital/issues/1975/1/"); got != "" {
		t.Errorf("an issue url is not an article, got %q", got)
	}
	if got := PersonSlug("//www.kvant.digital/indices/personalia/smorodinskiy_ya_a/"); got != "smorodinskiy_ya_a" {
		t.Errorf("PersonSlug: %q", got)
	}
}

// TestAnItalicTitleStaysATitle is the row that taught this parser the
// difference between an author and a variable. 1983 no. 10 has an article
// called n^x = x^n, set the way the magazine sets mathematics, and a parser
// that pulls every em out of the link as the author is left with the title "="
// and an author with the maths glued onto the end of it.
func TestAnItalicTitleStaysATitle(t *testing.T) {
	const page = `<html><body><ul class="object--toc">
<li><div class="toc--item toc--lev--1"><div class="toc--title">
<a href="//www.kvant.digital/issues/1983/10/pecherskiy-nx_xn-0b008072/">
<span><em>Печерский&nbsp;Л.&nbsp;Б.</em> <em>n</em><sup><em>x</em></sup> = <em>x</em><sup><em>n</em></sup></span></a>
</div><span class="toc--page"><a href="//www.kvant.digital/view/kvant_1983_10/p32/">31</a></span></div></li>
</ul></body></html>`

	iss, err := ParseIssue(strings.NewReader(page), 1983, "10")
	if err != nil {
		t.Fatal(err)
	}
	row := iss.Rows[0]
	if row.Authors != "Печерский Л. Б." {
		t.Errorf("authors are %q", row.Authors)
	}
	if row.Title != "n^x = x^n" {
		t.Errorf("title is %q", row.Title)
	}
}

// TestATitleThatOpensWithAVariable is the other half of it. Nothing says a row
// has an author, and a row whose title starts in italics must not have the
// first word of the title taken for one.
func TestATitleThatOpensWithAVariable(t *testing.T) {
	const page = `<html><body><ul class="object--toc">
<li><div class="toc--item toc--lev--1"><div class="toc--title">
<a href="//www.kvant.digital/issues/1975/1/x_i_y-0b008072/">
<span><em>x</em> и <em>y</em></span></a>
</div><span class="toc--page"><a href="//www.kvant.digital/view/kvant_1975_1/p10/">9</a></span></div></li>
</ul></body></html>`

	iss, err := ParseIssue(strings.NewReader(page), 1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	row := iss.Rows[0]
	if row.Authors != "" {
		t.Errorf("authors are %q, the row is printed without one", row.Authors)
	}
	if row.Title != "x и y" {
		t.Errorf("title is %q", row.Title)
	}
}
