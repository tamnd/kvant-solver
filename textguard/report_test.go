package textguard

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
)

func plan() *manifest.Paths {
	p := &manifest.Paths{}
	p.Set(manifest.IssuePaths{Key: "kvant_1975_1", Year: 1975, Sheet: 80,
		PathCount: manifest.PathCount{Vision: 80}, Note: "scan only"})
	p.Set(manifest.IssuePaths{Key: "kvant_1975_2", Year: 1975, Sheet: 80,
		PathCount: manifest.PathCount{Vision: 76, Publisher: 4}})
	p.Set(manifest.IssuePaths{Key: "kvant_2010_1", Year: 2010, Sheet: 68,
		PathCount: manifest.PathCount{Native: 60, Vision: 8},
		PDF:       &manifest.Layer{Pages: 68, Born: true, Fonts: 12, Embedded: 12, Subset: 11, Runes: 3100, Cyrillic: 0.65}})
	p.Set(manifest.IssuePaths{Key: "kvant_2004_1", Year: 2004, Sheet: 64,
		PathCount: manifest.PathCount{Vision: 64},
		PDF:       &manifest.Layer{Pages: 64, Fonts: 1, Why: "no embedded fonts, so the file is a scan in a wrapper"}})
	p.Sort()
	return p
}

func TestTheReportPricesEachDecade(t *testing.T) {
	out := Report(plan(), DefaultPrice)
	for _, want := range []string{"## By year", "## By decade", "| 1970s |", "| 2000s |", "| 2010s |"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report has no %q", want)
		}
	}
	// 1975 is 156 vision pages and 2010 is 8, so the two decades cannot be
	// priced the same.
	if strings.Count(out, "| 1970s | 160 | 156 |") != 1 {
		t.Errorf("the 1970s row is not what was counted:\n%s", out)
	}
}

func TestTheReportSaysWhatItAssumed(t *testing.T) {
	out := Report(plan(), DefaultPrice)
	if !strings.Contains(out, "## What the estimates assume") {
		t.Fatal("the report prices a decade without saying how")
	}
	for _, want := range []string{"input tokens", "seconds a page", "assumptions and not measurements"} {
		if !strings.Contains(out, want) {
			t.Errorf("the assumptions do not mention %q", want)
		}
	}
}

func TestTheReportSaysWhichPDFsWereMeasured(t *testing.T) {
	out := Report(plan(), DefaultPrice)
	if !strings.Contains(out, "## Which PDFs are born digital") {
		t.Fatal("nothing in the report backs the claim that the late years are native")
	}
	// One issue of 2004 has a PDF and it is a scan. Saying so is the whole
	// value of the section: it is where an assumed native year would be caught.
	if !strings.Contains(out, "kvant_2004_1: no embedded fonts") {
		t.Errorf("the scan in a PDF wrapper is not named:\n%s", out)
	}
	if !strings.Contains(out, "| 2010 | 1 | 1 | 1 |") {
		t.Errorf("2010 is not reported as measured and born digital:\n%s", out)
	}
}

func TestUndecidedSheetsAreCalledOut(t *testing.T) {
	p := plan()
	p.Set(manifest.IssuePaths{Key: "kvant_1976_1", Year: 1976, Sheet: 80,
		PathCount: manifest.PathCount{Undecided: 80}})
	p.Sort()
	out := Report(p, DefaultPrice)
	if !strings.Contains(out, "kvant fetch pages") {
		t.Error("80 sheets have no decision and the report does not say what to do about it")
	}
}

func TestThePriceIsPerPageAndPerLane(t *testing.T) {
	p := Price{InputTokens: 1000, OutputTokens: 1000, InputRate: 1, OutputRate: 1, SecondsPerPage: 36, Lanes: 2}
	if got := p.PageCost(); got != 0.002 {
		t.Errorf("a page costs %v", got)
	}
	if got := p.LaneHours(100); got != 1 {
		t.Errorf("100 pages at 36 seconds is %v lane hours", got)
	}
	if got := p.Days(4800); got != 1 {
		t.Errorf("4800 pages across two lanes is %v days", got)
	}
}
