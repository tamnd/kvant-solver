package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/native"
)

func emptyCorpus(t *testing.T) *corpus.Corpus {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// nativeFixture writes one page into a corpus the way the given lane would have
// written it, and hands back the corpus and the sheet it belongs to.
func nativeFixture(t *testing.T, extraction string) (*corpus.Corpus, corpus.IssueKey, fetch.Sheet) {
	t.Helper()
	c := emptyCorpus(t)
	key := corpus.IssueKey{Year: 2010, Number: "1"}
	sheet := fetch.Sheet{Ord: 6, Page: 5}
	front := &corpus.PageFront{
		Issue: key.String(), Year: key.Year, Number: key.Number,
		PageIndex: sheet.Ord + 1, PageLabel: "5",
		Provenance: corpus.Provenance{Lang: "ru", Source: "kvant_mccme", Extraction: extraction},
	}
	path := c.PagePath("ru", corpus.PageID{Issue: key, Index: sheet.Ord + 1})
	if err := corpus.Save(path, front, "Текст страницы, достаточно длинный, чтобы что-то значить.\n"); err != nil {
		t.Fatal(err)
	}
	return c, key, sheet
}

func TestAPageAnEarlierNativeRunWroteAndThisOneRejectsIsTakenBackOut(t *testing.T) {
	// The rules get tuned and the years get read again. kvant ocr leaves alone
	// any sheet that already has a file, so a page left behind by a run that has
	// just decided it is wrong is a page nothing will ever look at again.
	c, key, sheet := nativeFixture(t, corpus.ExtractionNative)
	dropped, err := dropStale(c, "ru", key, sheet, false)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("%d pages taken back, want 1", dropped)
	}
	if _, err := os.Stat(c.PagePath("ru", corpus.PageID{Issue: key, Index: sheet.Ord + 1})); !os.IsNotExist(err) {
		t.Fatalf("the page is still there, stat says %v", err)
	}
}

func TestAPageAModelReadIsLeftWhereItIs(t *testing.T) {
	// Somebody paid for that page. Whatever the native lane thinks of the file
	// it came out of, taking it back out is not its call.
	c, key, sheet := nativeFixture(t, corpus.ExtractionVision)
	dropped, err := dropStale(c, "ru", key, sheet, false)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("%d pages taken back, and none of them was this lane's to take", dropped)
	}
	if _, err := os.Stat(c.PagePath("ru", corpus.PageID{Issue: key, Index: sheet.Ord + 1})); err != nil {
		t.Fatalf("a page a model read was removed: %v", err)
	}
}

func TestADryRunCountsTheStalePagesAndRemovesNone(t *testing.T) {
	c, key, sheet := nativeFixture(t, corpus.ExtractionNative)
	dropped, err := dropStale(c, "ru", key, sheet, true)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("%d pages counted, want 1", dropped)
	}
	if _, err := os.Stat(c.PagePath("ru", corpus.PageID{Issue: key, Index: sheet.Ord + 1})); err != nil {
		t.Fatalf("a dry run removed a page: %v", err)
	}
}

func TestASheetWithNoPageFileIsNotAnError(t *testing.T) {
	// The ordinary case by a long way: most of what this lane rejects it is
	// seeing for the first time.
	c := emptyCorpus(t)
	key := corpus.IssueKey{Year: 2010, Number: "1"}
	dropped, err := dropStale(c, "ru", key, fetch.Sheet{Ord: 6, Page: 5}, false)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 0 {
		t.Fatalf("%d pages taken back from an empty corpus", dropped)
	}
}

func TestTheScanIsIndexedByThePrintedNumberAndNotByTheCount(t *testing.T) {
	// An issue with an insert has a sheet the PDF does not, and a run that
	// trusted the ordinals would file every page after it one slot late.
	idx := &fetch.Index{Sheets: []fetch.Sheet{
		{Ord: 0, Page: 0},
		{Ord: 1, Page: 0},
		{Ord: 2, Page: 1},
		{Ord: 3, Page: 0},
		{Ord: 4, Page: 2},
	}}
	sheets := sheetsByFolio(idx)
	if got := sheets[2].Ord; got != 4 {
		t.Errorf("page 2 is on sheet ordinal %d, want 4", got)
	}
	if _, ok := sheets[0]; ok {
		t.Error("an unnumbered sheet is in the map, and nothing in the file can be tied to it")
	}
}

func TestADuplicatePrintedNumberGoesToTheFirstSheetCarryingIt(t *testing.T) {
	// 1975 issue 8 prints page 30 twice and page 29 not at all, so this is a
	// defect in the archive and not a hypothetical.
	idx := &fetch.Index{Sheets: []fetch.Sheet{{Ord: 31, Page: 30}, {Ord: 32, Page: 30}}}
	if got := sheetsByFolio(idx)[30].Ord; got != 31 {
		t.Fatalf("page 30 is on sheet ordinal %d, want the first one", got)
	}
}

func TestTheRejectionTallyLeadsWithTheCommonestReason(t *testing.T) {
	// A run over a year prints this and nothing else about why it sent four
	// hundred pages away, so the order it comes in is the whole of the report.
	run := nativeRun{why: map[native.Rule]*reason{}}
	run.reject(1, native.RuleStacked, "a fraction")
	run.reject(2, native.RuleSoup, "a banner")
	run.reject(3, native.RuleStacked, "another fraction")
	reasons := run.reasons()
	if len(reasons) != 2 {
		t.Fatalf("%d rows in the tally, want 2: %v", len(reasons), reasons)
	}
	if got := reasons[0]; got[:14] != "  2 stacked   " {
		t.Errorf("the tally opens with %q, want the commonest reason first", got)
	}
	if got := sheetList(run.vision); got != "1,2,3" {
		t.Errorf("the repair line names sheets %q", got)
	}
}
