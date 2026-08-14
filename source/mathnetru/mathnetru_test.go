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
