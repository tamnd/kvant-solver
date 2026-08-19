package nsta_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/source/nsta"
)

func index(t *testing.T) []nsta.Entry {
	t.Helper()
	f, err := os.Open("testdata/index.htm")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	entries, err := nsta.ParseIndex(f)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func find(t *testing.T, entries []nsta.Entry, title string) nsta.Entry {
	t.Helper()
	i := slices.IndexFunc(entries, func(e nsta.Entry) bool { return e.Title == title })
	if i < 0 {
		t.Fatalf("the index has no entry titled %q", title)
	}
	return entries[i]
}

func TestAnIndexLineComesApartIntoItsBibliography(t *testing.T) {
	e := find(t, index(t), "Adding Angles in Three Dimensions")
	if got := strings.Join(e.Authors, ", "); got != "A. Shirshov, A. Nikitin" {
		t.Errorf("authors: %q", got)
	}
	if e.Year != 1997 || e.Months != "May/Jun" || e.Page != 46 {
		t.Errorf("issue: %s p%d", e.Issue(), e.Page)
	}
	if e.Department != "At the Blackboard" {
		t.Errorf("department: %q", e.Department)
	}
}

// The description in brackets after the title is the index's own writing and
// the mapping has no use for it, so it is dropped rather than parsed.
func TestTheDescriptionIsNotKept(t *testing.T) {
	for _, e := range index(t) {
		for _, field := range []string{e.Title, e.Department, strings.Join(e.Authors, " ")} {
			if strings.Contains(field, "polyhedrons") || strings.Contains(field, "Fermi problems") {
				t.Errorf("%q carries the description: %q", e.Title, field)
			}
		}
	}
}

// Most departments ran unsigned, and so did the contest and puzzle pages. An
// entry with no byline is still an entry.
func TestAnUnsignedEntryStillCounts(t *testing.T) {
	if e := find(t, index(t), "About the Triangle"); len(e.Authors) != 0 {
		t.Errorf("authors: %q", e.Authors)
	}
}

// Several entries share a paragraph. Reading one per paragraph looks right and
// quietly loses the second of every pair.
func TestTwoEntriesInOneParagraphAreBothRead(t *testing.T) {
	entries := index(t)
	find(t, entries, "Bend This Sheet")
	find(t, entries, "Ballpark Estimates")
}

// The two 1990 pilots came out before the bimonthly run started and carry a
// single month on the cover.
func TestThePilotIssuesOfNineteenNinetyAreRead(t *testing.T) {
	e := find(t, index(t), "Bend This Sheet")
	if e.Year != 1990 || e.Months != "Jan" {
		t.Errorf("issue: %s", e.Issue())
	}
}

// Twelve years of hand keying, so the index is not uniform. None of these is
// worth losing an article over.
func TestTheIndexIsReadThroughItsOwnTypos(t *testing.T) {
	entries := index(t)
	for _, c := range []struct{ title, issue string }{
		{"Constructions with Compass Alone", "May 1990"}, // the p on the page is missing
		{"Do You Get the Drift?", "Jan/Feb 1993"},        // the slash is missing
		{"Matter and Magnetism", "May/Jun 2001"},         // May is spelled Mau
		{"Shapes and Sizes", "Nov/Dec 1990"},             // a space before the year
	} {
		if got := find(t, entries, c.title).Issue(); got != c.issue {
			t.Errorf("%s: issue %q, want %q", c.title, got, c.issue)
		}
	}
	if got := find(t, entries, "Constructions with Compass Alone").Page; got != 47 {
		t.Errorf("page %d", got)
	}
}

// An article that ran under two departments is listed twice. One row is enough
// for a mapping, and two would make the count of what Quantum published wrong.
func TestAnArticleListedTwiceIsOneEntry(t *testing.T) {
	n := 0
	for _, e := range index(t) {
		if e.Title == "Adding Angles in Three Dimensions" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the article is in the index %d times", n)
	}
}

// The index carries a few standing notes about how to read it, laid out the
// same way an entry is. What tells them apart is that they have no issue.
func TestANoteIsNotAnArticle(t *testing.T) {
	for _, e := range index(t) {
		if strings.HasPrefix(e.Title, "How to read") || strings.HasPrefix(e.Title, "Note") {
			t.Errorf("the note %q was read as an article", e.Title)
		}
	}
}

func TestAPageThatIsNotTheIndexIsRefused(t *testing.T) {
	_, err := nsta.ParseIndex(strings.NewReader("<html><body><p>Not found.</p></body></html>"))
	if err == nil {
		t.Fatal("a page with no entries on it was accepted as the index")
	}
}
