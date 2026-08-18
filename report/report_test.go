package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
	"github.com/tamnd/kvant-solver/report"
)

var when = time.Date(1975, 3, 4, 5, 6, 7, 0, time.UTC)

func TestTheCostRollsUpPerYearAndCountsTheWaste(t *testing.T) {
	spends := report.Cost([]ocr.Entry{
		{Year: 1975, Engine: "glm-ocr", Seconds: 2, OK: true, Usage: api.Usage{InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500}},
		{Year: 1975, Engine: "glm-ocr", Seconds: 2, Usage: api.Usage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100}},
		{Year: 1975, Engine: "glm-ocr", Seconds: 4, OK: true, Usage: api.Usage{InputTokens: 1000, OutputTokens: 900, TotalTokens: 1900}},
		{Year: 1976, Engine: "claude-cli", Seconds: 10, OK: true},
	})
	if len(spends) != 2 || spends[0].Year != 1975 || spends[1].Year != 1976 {
		t.Fatalf("spends = %+v, want a row for each year in order", spends)
	}
	first := spends[0]
	switch {
	case first.Attempts != 3 || first.Pages != 2:
		t.Errorf("1975 counted %d attempts and %d pages, want 3 and 2", first.Attempts, first.Pages)
	case first.Waste() != 1:
		t.Errorf("1975 wasted %d attempts, want the one that was rejected", first.Waste())
	case first.Usage.InputTokens != 3000 || first.Usage.OutputTokens != 1500:
		t.Errorf("1975 counted %+v, want every attempt including the rejected one", first.Usage)
	case first.Seconds != 8:
		t.Errorf("1975 took %v seconds, want 8", first.Seconds)
	case first.Metered != 3:
		t.Errorf("1975 metered %d attempts, want all three", first.Metered)
	}
	// The lane that reports nothing is the case the report has to keep separate
	// from a lane that really cost nothing.
	if spends[1].Metered != 0 || spends[1].Attempts != 1 {
		t.Errorf("1976 metered %d of %d attempts, want none of the one", spends[1].Metered, spends[1].Attempts)
	}
	if all := report.Total(spends); all.Attempts != 4 || all.Pages != 3 || len(all.Engines) != 2 {
		t.Errorf("the total is %+v, want four attempts, three pages and both engines", all)
	}
}

func TestThePriceIsOffUntilSomebodySetsIt(t *testing.T) {
	spends := report.Cost([]ocr.Entry{
		{Year: 1975, OK: true, Usage: api.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000, TotalTokens: 2_000_000}},
	})
	free := report.CostTable(spends, report.Price{})
	if strings.Contains(free, "USD") {
		t.Errorf("a table with no price prints a money column:\n%s", free)
	}
	priced := report.CostTable(spends, report.Price{Input: 3, Output: 15})
	if !strings.Contains(priced, "USD") || !strings.Contains(priced, "18.00") {
		t.Errorf("a million tokens each way at 3 and 15 should come to 18.00:\n%s", priced)
	}
}

// A page read by a local program has a real cost in time and none in tokens,
// and the document has to say which of the two it is reporting.
func TestTheCostDocumentSaysWhenNobodyCountedTheTokens(t *testing.T) {
	spends := report.Cost([]ocr.Entry{
		{Year: 1975, Engine: "claude-cli", Seconds: 380, OK: true},
		{Year: 1975, Engine: "glm-ocr", Seconds: 1.4, OK: true, Usage: api.Usage{InputTokens: 900, OutputTokens: 700, TotalTokens: 1600}},
	})
	md := report.CostMarkdown(spends, report.Price{}, when)
	if !strings.Contains(md, "1 of 2 attempts came with no token counts") {
		t.Errorf("the document does not say the tokens are partial:\n%s", md)
	}
	if !strings.Contains(md, "claude-cli, glm-ocr") {
		t.Errorf("the document does not name both engines:\n%s", md)
	}
}

func TestAnEmptyLedgerIsAReportAndNotAnError(t *testing.T) {
	md := report.CostMarkdown(report.Cost(nil), report.Price{}, when)
	if !strings.Contains(md, "ledger is empty") {
		t.Errorf("an empty ledger produced:\n%s", md)
	}
}

// killed puts a job through enough failures to die, which is the only way to
// get a dead job with the history the report reads.
func killed(t *testing.T, q *queue.Queue, target string, reasons ...string) {
	t.Helper()
	job := queue.New(queue.StageOCR, target, "input", "prompt")
	job.Meta = map[string]string{"sheet": "7"}
	if _, err := q.Add(job); err != nil {
		t.Fatal(err)
	}
	for _, reason := range reasons {
		leased, err := q.Lease(queue.StageOCR, "box", "", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Fail(leased, reason); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheFailuresListNamesTheClassOfEveryDeadPage(t *testing.T) {
	q, err := queue.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	q.MaxAttempts = 2
	killed(t, q, "kvant_1975_1_p0007",
		"illegible: 9 unreadable spots",
		"illegible: 11 unreadable spots")
	killed(t, q, "kvant_1975_1_p0009",
		"short: 40 characters",
		"short: 12 characters; folio: no folio line")
	killed(t, q, "kvant_1976_2_p0003",
		"the service answered with an error: upstream connect error",
		"the service answered with an error: upstream connect error")

	list, err := report.Failures(q, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("the list has %d pages, want the three that died", len(list))
	}
	byTarget := map[string]report.Failure{}
	for _, fail := range list {
		byTarget[fail.Target] = fail
	}
	switch {
	case byTarget["kvant_1975_1_p0007"].Class() != "illegible":
		t.Errorf("page seven is class %q, want illegible", byTarget["kvant_1975_1_p0007"].Class())
	// Both attempts count, in the order the rules run, so a page that was short
	// twice and lost its folio once is short+folio and not folio+short.
	case byTarget["kvant_1975_1_p0009"].Class() != "short+folio":
		t.Errorf("page nine is class %q, want short+folio", byTarget["kvant_1975_1_p0009"].Class())
	// An outage is not a rule, and putting it in a rule column would blame the
	// scan for the network.
	case byTarget["kvant_1976_2_p0003"].Class() != "no rule":
		t.Errorf("the page that hit an outage is class %q, want no rule", byTarget["kvant_1976_2_p0003"].Class())
	case byTarget["kvant_1975_1_p0007"].Year != 1975 || byTarget["kvant_1976_2_p0003"].Issue != "kvant_1976_2":
		t.Error("a failure does not carry the issue it came from")
	case byTarget["kvant_1975_1_p0007"].Attempts != 2:
		t.Errorf("page seven records %d attempts, want two", byTarget["kvant_1975_1_p0007"].Attempts)
	}

	md := report.FailureMarkdown(list, 1970, 1989, when)
	switch {
	case !strings.Contains(md, "3 pages over 2 issues"):
		t.Errorf("the document does not count the pages and issues:\n%s", md)
	case !strings.Contains(md, "### kvant_1975_1"):
		t.Errorf("the document does not group by issue:\n%s", md)
	case !strings.Contains(md, "short+folio"):
		t.Errorf("the document does not print the class:\n%s", md)
	case strings.Contains(md, "Nothing."):
		t.Errorf("the document says nothing died, and three pages did:\n%s", md)
	}
}

// A run with nothing dead writes a file that says so. A missing file cannot be
// told apart from a report nobody generated.
func TestAnEmptyFailuresListStillWritesADocument(t *testing.T) {
	md := report.FailureMarkdown(nil, 1970, 1989, when)
	if !strings.Contains(md, "Nothing.") {
		t.Errorf("an empty list produced:\n%s", md)
	}
	if !strings.Contains(md, "1970 to 1989") {
		t.Errorf("the document does not say what range it covers:\n%s", md)
	}
}

// A dead job is a record of what happened, not of what is missing. A page that
// three lanes could not read and a fourth could leaves three failures behind it
// forever, and listing those made a complete year look like a broken one: 1975
// reported 115 pages that never read while all 928 of them sat in the corpus.
func TestAPageThatWasReadIsNotAFailure(t *testing.T) {
	q, err := queue.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	q.MaxAttempts = 2
	killed(t, q, "kvant_1975_1_p0007", "short: 40 characters", "short: 12 characters")
	killed(t, q, "kvant_1975_1_p0009", "illegible: 9 unreadable spots", "illegible: 11 unreadable spots")

	// Page seven was read afterwards by another lane. Page nine was not.
	read := func(id corpus.PageID) bool { return id.Index == 7 }
	list, err := report.Failures(q, read)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("the list has %d pages, want only the one still missing", len(list))
	}
	if list[0].Target != "kvant_1975_1_p0009" {
		t.Errorf("the list names %s, want the page that is not in the corpus", list[0].Target)
	}
}
