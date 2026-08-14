package kvantmccme

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func contents(t *testing.T) *Contents {
	t.Helper()
	c, err := ParseContents(fixture(t, "issue_1975_01.htm"), 1975, "01")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDecodeWindows1251(t *testing.T) {
	// The fixture is stored in the encoding the mirror actually serves. Read
	// as UTF-8 it is not an error, it is a page of replacement characters,
	// which is the failure mode worth a test of its own.
	raw, err := os.ReadFile("testdata/issue_1975_01.htm")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Квант") {
		t.Fatal("the fixture is not in Windows-1251, so this test proves nothing")
	}
	r, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decoded), "Квант") {
		t.Error("the decoded page does not contain the magazine's own name")
	}
	if bytes.ContainsRune(decoded, '�') {
		t.Error("the decoded page has replacement characters in it")
	}
}

func TestParseContents(t *testing.T) {
	c := contents(t)
	if len(c.Entries) < 15 {
		t.Fatalf("got %d entries, a 1975 issue has more than that", len(c.Entries))
	}
	first := c.Entries[0]
	if first.Title != "Равенства из спичек" {
		t.Errorf("first title is %q", first.Title)
	}
	if len(first.Authors) != 1 {
		t.Fatalf("first entry has %d authors", len(first.Authors))
	}
	if first.Authors[0].Name != "Антонович Н." {
		t.Errorf("author name is %q", first.Authors[0].Name)
	}
	if first.Authors[0].Slug != "antonovich_n" {
		t.Errorf("author slug is %q", first.Authors[0].Slug)
	}
}

func TestTheMastheadIsNotTheFirstArticle(t *testing.T) {
	c := contents(t)
	// The masthead has a stack of breaks in it and the entries are separated by
	// a pair of breaks, so a naive split cuts it up and hands back its bold
	// lines as articles. It also runs straight into the first entry with no
	// separator, so the first article of every issue is what gets lost.
	for _, e := range c.Entries {
		if strings.Contains(e.Title, "Редакция") || strings.Contains(e.Title, "издается") {
			t.Errorf("a line of the masthead came back as the article %q", e.Title)
		}
	}
	if c.Entries[0].Title != "Равенства из спичек" {
		t.Errorf("first entry is %q, the issue opens with Равенства из спичек", c.Entries[0].Title)
	}
}

func TestPagesAreAListNotARange(t *testing.T) {
	c := contents(t)
	var multi *Entry
	for i := range c.Entries {
		if len(c.Entries[i].Pages) > 1 {
			multi = &c.Entries[i]
			break
		}
	}
	if multi == nil {
		t.Fatal("no entry in the fixture spans more than one page")
	}
	// The mirror scanned page by page and lists each one, so the pages an item
	// occupies are known exactly. An article interrupted by a full page advert
	// is not contiguous, so collapsing this to first and last would be wrong.
	if len(multi.GIFURLs) != len(multi.Pages) {
		t.Errorf("%d pages but %d scans", len(multi.Pages), len(multi.GIFURLs))
	}
	for i, u := range multi.GIFURLs {
		if !strings.HasPrefix(u, "https://kvant.mccme.ru/1975/01/p") {
			t.Errorf("scan %d is %q, the relative href did not resolve", i, u)
		}
	}
}

func TestAllPagesLinkIsNotMistakenForAPage(t *testing.T) {
	c := contents(t)
	first := c.Entries[0]
	// Every entry has an "all pages" link with the same URL shape as a page
	// link. Its text is a phrase rather than a number, and that is the only
	// thing that tells them apart.
	if first.AllPagesURL == "" {
		t.Error("the all pages link was not picked up")
	}
	for _, p := range first.Pages {
		if p == 0 {
			t.Error("a page came through as zero, so a non numeric link was counted as a page")
		}
	}
}

func TestParseContentsRefusesAPageWithNoEntries(t *testing.T) {
	_, err := ParseContents(strings.NewReader("<html><body>nothing</body></html>"), 1975, "01")
	if err == nil {
		t.Fatal("a page with no entries should be an error")
	}
}

func TestURLBuilders(t *testing.T) {
	if got := IssueURL(1975, "01"); got != "https://kvant.mccme.ru/1975/01/index.htm" {
		t.Errorf("IssueURL: %s", got)
	}
	// The two orderings are generated separately, so having both is how a row
	// dropped from one of them gets noticed.
	if got := IssueByTitleURL(1975, "01"); got != "https://kvant.mccme.ru/1975/01/index_n.htm" {
		t.Errorf("IssueByTitleURL: %s", got)
	}
	// There is no builder for a PDF on purpose. The mirror names those four
	// different ways and the URL comes off its archive page instead.
	if got := AuthorURL("antonovich_n"); got != "https://kvant.mccme.ru/au/antonovich_n.htm" {
		t.Errorf("AuthorURL: %s", got)
	}
}
