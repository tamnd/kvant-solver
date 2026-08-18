package report

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/publisher"
)

func check(issue string, sheet, words, changed int) Check {
	return Check{
		Issue: issue, Year: 2010, Sheet: sheet,
		Count: publisher.Count{Words: words, Changed: changed, Ours: words, Extra: 0},
	}
}

func TestThePageThatCouldNotBeReadTwiceIsNotCountedEitherWay(t *testing.T) {
	// Counting it as agreement would let a lane that broke the scan report a
	// perfect score, and counting it as disagreement would blame the text layer
	// for a model timing out.
	t.Parallel()
	got := TallyChecks([]Check{
		check("kvant_2010_1", 3, 100, 10),
		{Issue: "kvant_2010_1", Sheet: 4, Unread: true},
	})
	if got.Pages != 1 || got.Unread != 1 {
		t.Fatalf("%d pages compared and %d unread, want 1 and 1", got.Pages, got.Unread)
	}
	if got.Words != 100 || got.Changed != 10 {
		t.Fatalf("the unread page brought %d words and %d changes with it", got.Words-100, got.Changed-10)
	}
}

func TestTheRateIsTakenOverWordsAndNotOverPages(t *testing.T) {
	// A page of dense formulas is fifty words and a page of prose is six
	// hundred. Averaging the two rates would let the short page, which is the
	// one this lane is worst at, count as much as the long one.
	t.Parallel()
	got := TallyChecks([]Check{
		check("kvant_2010_1", 3, 900, 0),
		check("kvant_2010_1", 4, 100, 50),
	})
	if want := 0.05; got.Rate() != want {
		t.Fatalf("the rate is %.3f, want %.3f, which is the words added up", got.Rate(), want)
	}
}

func TestAPageTheModelMisspelledItsWayThroughIsNotAPageToOpen(t *testing.T) {
	// A third of the words differing sounds like a page in trouble. A third of
	// the words differing by one letter each is a page the file has right and
	// the second reading fumbled, and sending somebody to look at it wastes the
	// only expensive thing in this loop, which is their attention.
	t.Parallel()
	noisy := Check{
		Issue: "kvant_2010_1", Sheet: 58,
		Count: publisher.Count{Words: 300, Changed: 100, Near: 96, Ours: 300},
	}
	if got := noisy.Missing(); got > CheckDrift {
		t.Fatalf("missing is %.3f, want it under the line at %.2f", got, CheckDrift)
	}
	if worst := worstChecks([]Check{noisy}, MaxCheckExamples); len(worst) != 0 {
		t.Fatalf("%d pages to open, and this one has four words actually gone", len(worst))
	}
}

func TestTheWorstPagesComeOutWorstFirstAndOnlyOverTheLine(t *testing.T) {
	t.Parallel()
	got := worstChecks([]Check{
		check("kvant_2010_1", 3, 100, 2),
		check("kvant_2010_1", 4, 100, 40),
		check("kvant_2010_1", 5, 100, 20),
		{Issue: "kvant_2010_1", Sheet: 6, Unread: true},
	}, MaxCheckExamples)
	if len(got) != 2 {
		t.Fatalf("%d pages over the line, want the two above %.0f%%", len(got), CheckDrift*100)
	}
	if got[0].Sheet != 4 || got[1].Sheet != 5 {
		t.Fatalf("the list runs %d then %d, want the worst first", got[0].Sheet, got[1].Sheet)
	}
}

func TestTheIssueRowAddsUpToTheSameThingTheSummaryDoes(t *testing.T) {
	// The first version of this dropped the near misses on the way into the row
	// and printed an issue missing 13% under a summary saying 5%, which is worse
	// than either number being wrong on its own.
	t.Parallel()
	checks := []Check{
		{Issue: "kvant_2010_1", Sheet: 3, Count: publisher.Count{Words: 200, Changed: 40, Near: 30, Ours: 200}},
		{Issue: "kvant_2010_1", Sheet: 4, Count: publisher.Count{Words: 300, Changed: 30, Near: 20, Ours: 300}},
	}
	rows := checksByIssue(checks)
	if len(rows) != 1 {
		t.Fatalf("%d rows for one issue", len(rows))
	}
	whole := TallyChecks(checks)
	if rows[0].Missing() != whole.Missing() || rows[0].Rate() != whole.Rate() {
		t.Fatalf("the row says %.3f missing of %.3f differing, the summary says %.3f of %.3f",
			rows[0].Missing(), rows[0].Rate(), whole.Missing(), whole.Rate())
	}
}

func TestTheDocumentSaysWhatItComparedAndWhatItCouldNot(t *testing.T) {
	t.Parallel()
	md := CheckMarkdown([]Check{
		{
			Issue: "kvant_2010_1", Year: 2010, Sheet: 7,
			Count:    publisher.Count{Words: 200, Changed: 60, Ours: 180, Extra: 5},
			Examples: []publisher.Example{{Publisher: "теорема", Vision: "тсорсма"}},
		},
		{Issue: "kvant_2010_1", Year: 2010, Sheet: 9, Unread: true},
	}, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"1 pages compared",
		"30.0%",
		"1 more the vision lane could not read",
		"kvant_2010_1 sheet 7",
		"теорема",
		"the scan would not read",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the document does not say %q", want)
		}
	}
}

func TestADocumentWithNothingComparedSaysSoRatherThanScoringZero(t *testing.T) {
	// A rate of zero per cent reads as perfect agreement, which is the one thing
	// an empty run has not established.
	t.Parallel()
	md := CheckMarkdown([]Check{{Issue: "kvant_2010_1", Sheet: 3, Unread: true}},
		time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(md, "No page was read twice") {
		t.Fatalf("an empty run wrote a report anyway:\n%s", md)
	}
}
