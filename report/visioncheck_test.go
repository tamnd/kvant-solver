package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/publisher"
	"github.com/tamnd/kvant-solver/report"
)

// toOpen is the list at the end of the document and nothing above it.
//
// Both tables are rows of pipes that start with an issue key, so a test that
// searched the whole document would pass on a page that only appears in the by
// issue summary, which is every page there is.
func toOpen(t *testing.T, doc string) string {
	t.Helper()
	_, list, ok := strings.Cut(doc, "## The pages to open")
	if !ok {
		t.Fatal("the document has no list of pages to open")
	}
	return list
}

// check is a page that has the archive's words and is missing the share given.
func check(issue string, sheet, words int, missing float64) report.VisionCheck {
	return report.VisionCheck{
		Issue: issue,
		Year:  1975,
		Sheet: sheet,
		Count: publisher.Count{Words: words, Changed: int(float64(words) * missing), Ours: words},
	}
}

func TestASheetWithNoPageIsCountedApartFromTheRate(t *testing.T) {
	// A reading that never happened is not evidence about the ones that did,
	// and letting it into the rate would make an issue look worse the less of
	// it we managed to read.
	t.Parallel()
	got := report.TallyVision([]report.VisionCheck{
		check("kvant_1975_4", 1, 100, 0.10),
		{Issue: "kvant_1975_4", Sheet: 2, Unread: true},
	})
	if got.Pages != 1 || got.Unread != 1 {
		t.Errorf("%d pages and %d unread, want 1 and 1", got.Pages, got.Unread)
	}
	if got.Words != 100 {
		t.Errorf("the unread sheet put %d words into the rate", got.Words-100)
	}
}

func TestTheRateWeighsAPageByWhatIsOnIt(t *testing.T) {
	// Adding rates rather than words would let a cover missing everything
	// outweigh a dense page missing nothing.
	t.Parallel()
	got := report.TallyVision([]report.VisionCheck{
		check("kvant_1975_4", 1, 900, 0),
		check("kvant_1975_4", 2, 100, 1),
	})
	if m := got.Missing(); m < 0.09 || m > 0.11 {
		t.Errorf("missing %.2f, want about 0.10", m)
	}
}

func TestAPageOverTheLineIsListedForSomebodyToOpen(t *testing.T) {
	t.Parallel()
	doc := report.VisionMarkdown([]report.VisionCheck{
		check("kvant_1975_4", 26, 200, 0.95),
		check("kvant_1975_4", 27, 200, 0.05),
	}, time.Unix(0, 0))
	list := toOpen(t, doc)
	if !strings.Contains(list, "| kvant_1975_4 | 26 |") {
		t.Error("the page over the line is not in the list")
	}
	if strings.Contains(list, "| kvant_1975_4 | 27 |") {
		t.Error("a page the two readings agree on was listed anyway")
	}
}

func TestAPageTheArchiveReadNothingOnIsNotRanked(t *testing.T) {
	// Its rate is arithmetic on a handful of words and says nothing about our
	// reading. Covers are the whole of this class and they would otherwise fill
	// the list.
	t.Parallel()
	doc := report.VisionMarkdown([]report.VisionCheck{check("kvant_1975_4", 1, 6, 1)}, time.Unix(0, 0))
	if strings.Contains(toOpen(t, doc), "| kvant_1975_4 | 1 |") {
		t.Error("a cover the archive read six words on was ranked as a bad page")
	}
	if !strings.Contains(doc, "None.") {
		t.Error("with nothing to list the document should say so")
	}
}

func TestTheWorstPageComesFirst(t *testing.T) {
	t.Parallel()
	doc := report.VisionMarkdown([]report.VisionCheck{
		check("kvant_1975_4", 10, 200, 0.70),
		check("kvant_1975_4", 20, 200, 0.99),
	}, time.Unix(0, 0))
	list := toOpen(t, doc)
	first := strings.Index(list, "| kvant_1975_4 | 20 |")
	second := strings.Index(list, "| kvant_1975_4 | 10 |")
	if first < 0 || second < 0 {
		t.Fatal("both pages should be listed")
	}
	if first > second {
		t.Error("the better page is listed above the worse one")
	}
}

func TestEveryIssueGetsARowWhetherOrNotItHasABadPage(t *testing.T) {
	// An issue with nothing wrong still has to appear, because a table that
	// only listed the issues with problems would leave no way to tell an issue
	// that is clean from one that was never checked.
	t.Parallel()
	doc := report.VisionMarkdown([]report.VisionCheck{
		check("kvant_1975_4", 1, 200, 0.95),
		check("kvant_1975_5", 1, 200, 0.05),
	}, time.Unix(0, 0))
	for _, issue := range []string{"| kvant_1975_4 | 1 |", "| kvant_1975_5 | 1 |"} {
		if !strings.Contains(doc, issue) {
			t.Errorf("%s has no row in the by issue table", issue)
		}
	}
}

func TestADocumentWithNothingCheckedSaysSo(t *testing.T) {
	t.Parallel()
	doc := report.VisionMarkdown(nil, time.Unix(0, 0))
	if !strings.Contains(doc, "nothing here") {
		t.Errorf("an empty run produced %d bytes that do not say it is empty", len(doc))
	}
}
