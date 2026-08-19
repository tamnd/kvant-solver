package publish_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/publish"
)

func refusedReport(t *testing.T, list []publish.Refused) string {
	t.Helper()
	var out strings.Builder
	if err := publish.WriteRefused(&out, list, "2026-08-19"); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// Every row has to be enough to fix the page without opening the site, because
// the person reading the report is going to reread a sheet and needs to know
// which one and what was wrong with it.
func TestTheReportSaysWhichFileAndWhatTheTeXWas(t *testing.T) {
	got := refusedReport(t, []publish.Refused{
		{Where: "1975/04/pages/0011.md", BadMath: publish.BadMath{
			Line: 9, TeX: `-10\celsius`, Err: errors.New(`Undefined control sequence: \celsius at position 4: -10\̲c̲e̲l̲s̲i̲u̲s̲`)}},
	})

	for _, want := range []string{
		"1975/04/pages/0011.md", "| 9 |", `\celsius`, "1 formula over 1 file",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report has no %q in it:\n%s", want, got)
		}
	}
	// KaTeX points at the character it stopped on with a run of combining marks,
	// which is unreadable in a table and says nothing the TeX column does not.
	if strings.Contains(got, "at position") {
		t.Errorf("the report kept the position marker:\n%s", got)
	}
}

// The year table is what makes the report worth having over the log: it says
// whether this is a handful of pages or a decade that needs rereading.
func TestTheReportCountsByYear(t *testing.T) {
	got := refusedReport(t, []publish.Refused{
		{Where: "1975/04/pages/0011.md", BadMath: publish.BadMath{Line: 1, TeX: "a"}},
		{Where: "1975/04/pages/0011.md", BadMath: publish.BadMath{Line: 4, TeX: "b"}},
		{Where: "1980/02/pages/0007.md", BadMath: publish.BadMath{Line: 2, TeX: "c"}},
	})

	if !strings.Contains(got, "| 1975 | 2 | 1 |") {
		t.Errorf("1975 is not counted as two formulas on one file:\n%s", got)
	}
	if !strings.Contains(got, "| 1980 | 1 | 1 |") {
		t.Errorf("1980 is not counted:\n%s", got)
	}
	if !strings.Contains(got, "3 formulas over 2 files") {
		t.Errorf("the totals are wrong:\n%s", got)
	}
}

// A pipe is a Markdown table separator and TeX uses it for absolute values and
// for set builder notation, so it turns up in exactly the rows this report is
// made of.
func TestAPipeInAFormulaDoesNotBreakTheTable(t *testing.T) {
	got := refusedReport(t, []publish.Refused{
		{Where: "1975/04/pages/0011.md", BadMath: publish.BadMath{Line: 1, TeX: `|x| \qq`}},
	})
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "| 1975/04") && strings.Count(line, "|") != 5+2 {
			t.Errorf("the row has the wrong number of cells: %q", line)
		}
	}
}

// A corpus with nothing wrong with it is the goal, and a report that says so is
// more useful than an empty file.
func TestAReportOfNothingSaysSo(t *testing.T) {
	got := refusedReport(t, nil)
	if !strings.Contains(got, "Every formula in the corpus parses") {
		t.Errorf("got %q, want it to say the corpus is clean", got)
	}
}
