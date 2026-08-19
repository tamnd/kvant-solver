package mathnetru

import (
	"os"
	"strings"
	"testing"
)

func refs(t *testing.T) []IssueRef {
	t.Helper()
	f, err := os.Open("testdata/kvant_contents.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	got, err := ParseContents(f)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func find(t *testing.T, year int, number string) IssueRef {
	t.Helper()
	for _, r := range refs(t) {
		if r.Year == year && r.Number == number {
			return r
		}
	}
	t.Fatalf("%d issue %s is not in the fixture", year, number)
	return IssueRef{}
}

func TestParseContents(t *testing.T) {
	got := refs(t)
	// The fixture holds the 2025 and 2024 blocks. Both years ran ten covers
	// over twelve numbers, and the 2024 block in the fixture stops part way
	// through, so this is a floor rather than an exact count.
	if len(got) < 15 {
		t.Fatalf("got %d issues", len(got))
	}
	if years := Years(got); len(years) != 2 || years[0] != 2025 || years[1] != 2024 {
		t.Errorf("years came out as %v, the page lists newest first", years)
	}
	first := got[0]
	if first.Year != 2025 || first.Number != "1" {
		t.Errorf("first issue is %d number %q", first.Year, first.Number)
	}
	if !strings.HasPrefix(first.CoverURL, "https://www.mathnet.ru/JrnLogos/kvant/") {
		t.Errorf("cover URL is %q, the relative src did not resolve", first.CoverURL)
	}
}

func TestPrintedNumberAndQueryNumberDiffer(t *testing.T) {
	// This is the reason the package exists. The magazine printed 5-6 on one
	// cover and mathnet asks for it as issue=5. Storing only the query number
	// loses the cover, and storing only the cover cannot build a URL.
	r := find(t, 2025, "5-6")
	if r.Query != 5 {
		t.Errorf("the 5-6 issue has query number %d", r.Query)
	}
	if !r.Double() {
		t.Error("5-6 did not come out as a double")
	}
	if !strings.Contains(r.URL, "issue=5&") {
		t.Errorf("URL is %q, it has to carry the query number and not the cover", r.URL)
	}
}

func TestSingleIssueIsNotADouble(t *testing.T) {
	r := find(t, 2025, "4")
	if r.Double() {
		t.Error("issue 4 came out as a double")
	}
	if r.Query != 4 {
		t.Errorf("query number is %d", r.Query)
	}
}

func TestFullTextFlag(t *testing.T) {
	// Every issue in the fixture has full text. The flag still gets a test,
	// because it decides whether this source is worth fetching at all for a
	// given year, and a parser change that silently sets it false everywhere
	// would look like the site had withdrawn the texts.
	for _, r := range refs(t) {
		if !r.FullText {
			t.Errorf("%d number %s came out without full text", r.Year, r.Number)
		}
	}
}

func TestEveryIssueGetsAWorkingURL(t *testing.T) {
	for _, r := range refs(t) {
		if r.Year < FirstYear {
			t.Errorf("%d is earlier than mathnet's own coverage", r.Year)
		}
		if r.Query <= 0 {
			t.Errorf("%d number %s has query number %d", r.Year, r.Number, r.Query)
		}
		if !strings.HasPrefix(r.URL, BaseURL) {
			t.Errorf("%d number %s has URL %q", r.Year, r.Number, r.URL)
		}
	}
}

func TestParseContentsRefusesAPageWithNoIssues(t *testing.T) {
	_, err := ParseContents(strings.NewReader("<html><body>nothing here</body></html>"))
	if err == nil {
		t.Fatal("a page with no issue links should be an error")
	}
}

func TestURLBuilders(t *testing.T) {
	want := "https://www.mathnet.ru/php/archive.phtml?jrnid=kvant&wshow=contents&option_lang=rus"
	if got := ContentsURL(); got != want {
		t.Errorf("ContentsURL: %s", got)
	}
	want = "https://www.mathnet.ru/php/archive.phtml?jrnid=kvant&wshow=issue&year=2024&volume=&issue=11&option_lang=rus"
	if got := IssueURL(2024, 11); got != want {
		t.Errorf("IssueURL: %s", got)
	}
}

func papers(t *testing.T) []PaperRef {
	t.Helper()
	f, err := os.Open("testdata/kvant_issue.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	got, err := ParseIssue(f)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func paper(t *testing.T, id string) PaperRef {
	t.Helper()
	for _, p := range papers(t) {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("%s is not in the fixture", id)
	return PaperRef{}
}

func TestAnIssuePageComesApartIntoItsArticles(t *testing.T) {
	got := papers(t)
	if len(got) != 4 {
		t.Fatalf("%d articles, want 4", len(got))
	}
	first := got[0]
	if first.ID != "kvant2822" || first.Title != "Из истории газовых центрифуг" {
		t.Errorf("%+v", first)
	}
	if first.PageFirst != 2 || first.PageLast != 5 {
		t.Errorf("pages %d to %d", first.PageFirst, first.PageLast)
	}
	if len(first.Authors) != 1 || first.Authors[0] != "С. Романов" {
		t.Errorf("authors %q", first.Authors)
	}
}

// The permanent link is the whole reason this manifest is worth keeping, so it
// has to be built and not guessed at.
func TestEveryArticleGetsItsPermanentLink(t *testing.T) {
	for _, p := range papers(t) {
		want := "https://www.mathnet.ru/rus/" + p.ID
		if p.URL != want {
			t.Errorf("%s: %s", p.ID, p.URL)
		}
	}
}

func TestTwoAuthorsComeApart(t *testing.T) {
	got := paper(t, "kvant1576").Authors
	if len(got) != 2 || got[0] != "А. Иванов" || got[1] != "Б. Петрова" {
		t.Errorf("%q", got)
	}
}

// A one page article prints one number rather than a range.
func TestAOnePageArticleStartsAndEndsOnTheSamePage(t *testing.T) {
	got := paper(t, "kvant2006")
	if got.PageFirst != 17 || got.PageLast != 17 {
		t.Errorf("pages %d to %d", got.PageFirst, got.PageLast)
	}
	if len(got.Authors) != 0 {
		t.Errorf("an unsigned article got a byline: %q", got.Authors)
	}
}

func TestAnArticleWithNoPrintedRangeIsNotGivenOne(t *testing.T) {
	got := paper(t, "kvant2834")
	if got.PageFirst != 0 || got.PageLast != 0 {
		t.Errorf("pages %d to %d, want none", got.PageFirst, got.PageLast)
	}
}

func TestTheFullTextClaimIsReadPerArticle(t *testing.T) {
	if !paper(t, "kvant2822").FullText {
		t.Error("an article with text was read as a scan")
	}
	if paper(t, "kvant1576").FullText {
		t.Error("an article with only a scan was read as having text")
	}
}

// The issue page carries links to the next and previous issue in the same
// class as the article links, and those are not articles.
func TestANavigationLinkIsNotAnArticle(t *testing.T) {
	for _, p := range papers(t) {
		if !strings.HasPrefix(p.ID, JournalID) || p.Title == "4" {
			t.Errorf("navigation link read as an article: %+v", p)
		}
	}
}

func TestParseIssueRefusesAPageWithNoArticles(t *testing.T) {
	if _, err := ParseIssue(strings.NewReader("<html><body>no articles</body></html>")); err == nil {
		t.Fatal("a page with no articles was accepted")
	}
}
