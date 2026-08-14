// Package coverage answers the only question a run over twenty years needs
// answered: what is finished, and what is not.
//
// It exists because the milestone before this one was one issue, where the
// answer fits on a screen and the audit gives it. Twenty years is 240 issues
// and 16000 pages, and a run of that length is stopped and resumed on different
// machines over several days. Somebody has to be able to ask, at any point,
// which issues are done and which are half done, and get the same answer
// whatever machine they ask on.
//
// The answer is assembled from three places that each know part of it. The
// issue list says how many issues a year has. The page index in the cache says
// how many sheets an issue's scan has and how many of them are downloaded. The
// corpus says how many pages were read and how many articles were built. This
// package holds none of that itself; it lines the three up and says where they
// disagree.
package coverage

import (
	"fmt"
	"sort"
	"strings"
)

// Issue is what one issue has, counted in every place that counts it.
type Issue struct {
	Key    string
	Year   int
	Number string

	// Sheets is how many surfaces the scan has, from the page index. Zero means
	// the issue has never been fetched, and then nothing below it means
	// anything, which is why Fetched is a separate question from Read.
	Sheets int
	// Downloaded is how many of those sheets have their bytes in the cache.
	Downloaded int
	// Pages is how many page files the corpus holds for the issue.
	Pages int
	// Articles is how many article files were assembled out of them.
	Articles int
	// Rows is how many rows the printed contents has, which is what Articles
	// should equal once the issue is assembled.
	Rows int
	// Dead is how many pages ran out of attempts in the OCR queue. A dead page
	// is the difference between an issue that is still being worked on and one
	// that needs a person.
	Dead int
}

// Fetched is an issue whose scan is completely downloaded.
func (i Issue) Fetched() bool { return i.Sheets > 0 && i.Downloaded >= i.Sheets }

// Read is an issue with a page file for every sheet.
//
// The comparison is against the sheets in the index and not against what was
// downloaded, because an issue missing four sheets is missing four pages of the
// magazine whether or not this machine ever had the images.
func (i Issue) Read() bool { return i.Sheets > 0 && i.Pages >= i.Sheets }

// Assembled is an issue with an article for every row of its printed contents.
//
// An issue whose contents nobody has transcribed has no rows, and then there is
// nothing to be complete against. That is reported as not assembled rather than
// as trivially finished, because a year of issues with no contents at all is
// exactly the failure this is here to make visible.
func (i Issue) Assembled() bool { return i.Rows > 0 && i.Articles >= i.Rows }

// Complete is all three, and it is what the milestone means by done.
func (i Issue) Complete() bool { return i.Fetched() && i.Read() && i.Assembled() && i.Dead == 0 }

// Totals is a set of issues added up.
type Totals struct {
	Issues     int
	Complete   int
	Sheets     int
	Downloaded int
	Pages      int
	Articles   int
	Rows       int
	Dead       int
}

// Add folds one issue into the totals.
func (t *Totals) Add(i Issue) {
	t.Issues++
	if i.Complete() {
		t.Complete++
	}
	t.Sheets += i.Sheets
	t.Downloaded += i.Downloaded
	t.Pages += i.Pages
	t.Articles += i.Articles
	t.Rows += i.Rows
	t.Dead += i.Dead
}

// Year is the issues of one year.
type Year struct {
	Year   int
	Issues []Issue
}

// Totals adds up the year.
func (y Year) Totals() Totals {
	var t Totals
	for _, issue := range y.Issues {
		t.Add(issue)
	}
	return t
}

// Complete is a year with nothing outstanding in any of its issues.
func (y Year) Complete() bool {
	t := y.Totals()
	return t.Issues > 0 && t.Complete == t.Issues
}

// Group puts issues into years, in year order and issue order within a year.
func Group(issues []Issue) []Year {
	byYear := map[int][]Issue{}
	for _, issue := range issues {
		byYear[issue.Year] = append(byYear[issue.Year], issue)
	}
	years := make([]Year, 0, len(byYear))
	for year, list := range byYear {
		sort.Slice(list, func(a, b int) bool { return less(list[a], list[b]) })
		years = append(years, Year{Year: year, Issues: list})
	}
	sort.Slice(years, func(a, b int) bool { return years[a].Year < years[b].Year })
	return years
}

// less orders two issues of the same year by their number, which is usually a
// small integer and is sometimes 5-6 for a double issue. Comparing the strings
// would put 10 before 2.
func less(a, b Issue) bool {
	if a.Year != b.Year {
		return a.Year < b.Year
	}
	na, nb := firstNumber(a.Number), firstNumber(b.Number)
	if na != nb {
		return na < nb
	}
	return a.Number < b.Number
}

func firstNumber(number string) int {
	head, _, _ := strings.Cut(number, "-")
	n := 0
	for _, r := range head {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// Table is the per year roll-up, which is what a run prints when it stops.
func Table(years []Year) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%-6s  %8s  %10s  %8s  %8s  %8s  %6s\n",
		"year", "issues", "downloaded", "sheets", "pages", "articles", "dead")
	var all Totals
	for _, year := range years {
		t := year.Totals()
		fmt.Fprintf(&out, "%-6d  %4d/%-3d  %10d  %8d  %8d  %8d  %6d\n",
			year.Year, t.Complete, t.Issues, t.Downloaded, t.Sheets, t.Pages, t.Articles, t.Dead)
		all.Issues += t.Issues
		all.Complete += t.Complete
		all.Sheets += t.Sheets
		all.Downloaded += t.Downloaded
		all.Pages += t.Pages
		all.Articles += t.Articles
		all.Dead += t.Dead
	}
	if len(years) > 1 {
		fmt.Fprintf(&out, "%-6s  %4d/%-3d  %10d  %8d  %8d  %8d  %6d\n",
			"all", all.Complete, all.Issues, all.Downloaded, all.Sheets, all.Pages, all.Articles, all.Dead)
	}
	return out.String()
}

// IssueTable is the same thing an issue at a time, with a last column that says
// what is holding each one up.
func IssueTable(issues []Issue) string {
	sorted := make([]Issue, len(issues))
	copy(sorted, issues)
	sort.Slice(sorted, func(a, b int) bool { return less(sorted[a], sorted[b]) })

	var out strings.Builder
	fmt.Fprintf(&out, "%-18s  %10s  %8s  %8s  %8s  %6s  %s\n",
		"issue", "downloaded", "sheets", "pages", "articles", "dead", "state")
	for _, issue := range sorted {
		fmt.Fprintf(&out, "%-18s  %10d  %8d  %8d  %8d  %6d  %s\n",
			issue.Key, issue.Downloaded, issue.Sheets, issue.Pages, issue.Articles, issue.Dead, State(issue))
	}
	return out.String()
}

// State is the short answer for one issue: what is the next thing to do to it.
//
// The order is the order of the pipeline, because the first unfinished stage is
// the only one worth naming. An issue that has not been fetched has no pages
// either, and reporting both would put the same issue on two work lists.
func State(i Issue) string {
	switch {
	case i.Sheets == 0:
		return "not fetched"
	case !i.Fetched():
		return fmt.Sprintf("fetching, %d of %d sheets", i.Downloaded, i.Sheets)
	case !i.Read():
		return fmt.Sprintf("reading, %d of %d pages", i.Pages, i.Sheets)
	case i.Dead > 0:
		return fmt.Sprintf("%d pages need a person", i.Dead)
	case i.Rows == 0:
		return "no contents rows"
	case !i.Assembled():
		return fmt.Sprintf("assembling, %d of %d articles", i.Articles, i.Rows)
	default:
		return "done"
	}
}

// Outstanding is every issue that is not finished, in pipeline order, which is
// the work list a run is started from.
func Outstanding(issues []Issue) []Issue {
	var out []Issue
	for _, issue := range issues {
		if !issue.Complete() {
			out = append(out, issue)
		}
	}
	sort.Slice(out, func(a, b int) bool { return less(out[a], out[b]) })
	return out
}
