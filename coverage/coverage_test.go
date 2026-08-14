package coverage_test

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/coverage"
)

// whole is an issue with nothing outstanding, which every test below spoils in
// one way.
func whole() coverage.Issue {
	return coverage.Issue{
		Key: "kvant_1975_1", Year: 1975, Number: "1",
		Sheets: 84, Downloaded: 84, Pages: 84, Articles: 27, Rows: 27,
	}
}

func TestAnIssueWithEverythingIsComplete(t *testing.T) {
	if !whole().Complete() {
		t.Fatalf("state = %s, want done", coverage.State(whole()))
	}
}

// The state is the next thing to do and not everything that is wrong, so an
// issue held up at two stages names the earlier one.
func TestTheStateNamesTheFirstUnfinishedStage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spoil func(*coverage.Issue)
		want  string
	}{
		{"never fetched", func(i *coverage.Issue) { *i = coverage.Issue{Key: i.Key, Year: i.Year, Number: i.Number} }, "not fetched"},
		{"half fetched", func(i *coverage.Issue) { i.Downloaded, i.Pages, i.Articles = 40, 0, 0 }, "fetching, 40 of 84 sheets"},
		{"half read", func(i *coverage.Issue) { i.Pages, i.Articles = 71, 0 }, "reading, 71 of 84 pages"},
		{"pages that died", func(i *coverage.Issue) { i.Dead = 13 }, "13 pages need a person"},
		{"no contents", func(i *coverage.Issue) { i.Rows, i.Articles = 0, 0 }, "no contents rows"},
		{"half assembled", func(i *coverage.Issue) { i.Articles = 9 }, "assembling, 9 of 27 articles"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue := whole()
			tc.spoil(&issue)
			if got := coverage.State(issue); got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
			if issue.Complete() {
				t.Error("complete, and it is not")
			}
		})
	}
}

// A dead page is the case worth being careful about: every count is right and
// the issue is still not finished, because a page that ran out of attempts is
// not a page that will appear on its own.
func TestAnIssueWithADeadPageIsNotComplete(t *testing.T) {
	issue := whole()
	issue.Dead = 1
	if issue.Read() != true {
		t.Error("Read is false, and every sheet has a page file")
	}
	if issue.Complete() {
		t.Error("complete with a page that died")
	}
}

func TestYearsComeBackInOrderAndSoDoTheirIssues(t *testing.T) {
	years := coverage.Group([]coverage.Issue{
		{Key: "kvant_1976_2", Year: 1976, Number: "2", Sheets: 80, Downloaded: 80, Pages: 80, Rows: 20, Articles: 20},
		{Key: "kvant_1975_10", Year: 1975, Number: "10"},
		{Key: "kvant_1975_2", Year: 1975, Number: "2"},
		{Key: "kvant_1975_5-6", Year: 1975, Number: "5-6"},
	})
	if len(years) != 2 || years[0].Year != 1975 || years[1].Year != 1976 {
		t.Fatalf("years = %+v", years)
	}
	// Two before ten, and the double issue where its first number puts it.
	var got []string
	for _, issue := range years[0].Issues {
		got = append(got, issue.Number)
	}
	if strings.Join(got, ",") != "2,5-6,10" {
		t.Errorf("numbers = %v, want 2 5-6 10", got)
	}
	if years[0].Complete() {
		t.Error("1975 is complete, and none of its issues has a sheet")
	}
	if !years[1].Complete() {
		t.Error("1976 is not complete, and its only issue is")
	}
}

func TestTheTableTotalsEveryYearAndThenAllOfThem(t *testing.T) {
	years := coverage.Group([]coverage.Issue{
		{Key: "kvant_1975_1", Year: 1975, Number: "1", Sheets: 84, Downloaded: 84, Pages: 84, Rows: 27, Articles: 27},
		{Key: "kvant_1976_1", Year: 1976, Number: "1", Sheets: 80, Downloaded: 80, Pages: 40, Rows: 25, Dead: 3},
	})
	table := coverage.Table(years)
	lines := strings.Split(strings.TrimSpace(table), "\n")
	if len(lines) != 4 {
		t.Fatalf("table has %d lines, want a header, two years and a total:\n%s", len(lines), table)
	}
	if !strings.HasPrefix(lines[3], "all") || !strings.Contains(lines[3], "164") {
		t.Errorf("the total line is %q, want 164 sheets across both years", lines[3])
	}
	if !strings.Contains(lines[1], "1/1") || !strings.Contains(lines[2], "0/1") {
		t.Errorf("the year lines do not count complete issues:\n%s", table)
	}
}

func TestOutstandingLeavesTheFinishedIssuesOut(t *testing.T) {
	done := whole()
	half := whole()
	half.Key, half.Number, half.Pages = "kvant_1975_2", "2", 12
	out := coverage.Outstanding([]coverage.Issue{done, half})
	if len(out) != 1 || out[0].Key != "kvant_1975_2" {
		t.Fatalf("outstanding = %+v, want only the half read issue", out)
	}
}
