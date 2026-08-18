// Package report turns what a long run left behind into the two documents the
// milestone asks for: the list of pages that never succeeded, and what the run
// cost per year.
//
// Both are built after the fact from records the run wrote as it went, the
// queue for failures and the ledger for cost, so a report can be produced on a
// machine that read nothing, from a checkout of the cache, and it says the same
// thing every time it is run.
package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
)

// Failure is one page that ran out of attempts.
type Failure struct {
	// ID is the queue's own name for the job, which is a hash of the target and
	// the prompt and not something a caller can work out. Repair needs it to put
	// the page back, so the report that finds the page hands it over rather than
	// making the next stage search a directory of every job ever run.
	ID     string
	Target string
	Issue  string
	Year   int
	Sheet  int
	// Rules is every rule that caught the page over all its attempts, not only
	// the last one. A page that was short once and unreadable twice is a page
	// with two things wrong with it and both belong in the class.
	Rules []ocr.Rule
	// Reason is the last thing that was said about it, which is the sentence
	// somebody reads first.
	Reason   string
	Attempts int
	Last     time.Time
	Host     string
	// Repair is true when the page is waiting on the repair queue, so the list
	// separates a page that has a plan from a page that has none.
	Repair bool
}

// Class is the short name for what is wrong with a page.
//
// It is the rules joined, or a phrase for the two cases where no rule fired.
// The milestone asks for every page that never succeeded with its class, and
// the class is the whole point of the document: 300 dead pages that are all the
// folio rule is one problem with the render, and 300 that are spread over eight
// rules is 300 problems.
func (f Failure) Class() string {
	if len(f.Rules) > 0 {
		names := make([]string, 0, len(f.Rules))
		for _, rule := range f.Rules {
			names = append(names, string(rule))
		}
		return strings.Join(names, "+")
	}
	if f.Reason == "" {
		return "unknown"
	}
	return "no rule"
}

// Failures reads the dead OCR jobs and turns each into a line of the report.
//
// The repair queue is read as well, because a dead page and a queued repair are
// the same page and the report should say which of the dead ones already has a
// second pass waiting.
//
// read says whether a page is in the corpus, and the ones that are do not
// appear. A dead job is a record of what happened rather than of what is
// missing: a page that three lanes could not read and a fourth could leaves the
// first three failures behind it forever. The first version of this listed
// them, so a complete year reported 115 pages that never read while every one
// of the 928 sat in the corpus. Passing nil lists everything, which is what a
// caller with no corpus to hand has to do.
func Failures(jobs *queue.Queue, read func(corpus.PageID) bool) ([]Failure, error) {
	dead, err := jobs.List(queue.StageOCR, queue.Dead)
	if err != nil {
		return nil, err
	}
	repairs, err := jobs.List(queue.StageRepair, queue.Pending)
	if err != nil {
		return nil, err
	}
	queued := map[string]bool{}
	for _, job := range repairs {
		queued[job.Target] = true
	}

	out := make([]Failure, 0, len(dead))
	for _, job := range dead {
		id, err := corpus.ParsePageID(job.Target)
		if err != nil {
			// Not a page, so not a page that failed. The queue carries other
			// stages and this report is only about the reading.
			continue
		}
		if read != nil && read(id) {
			continue
		}
		fail := Failure{
			ID:       job.ID,
			Target:   job.Target,
			Issue:    id.Issue.String(),
			Year:     id.Issue.Year,
			Sheet:    id.Index,
			Attempts: job.Attempts,
			Repair:   queued[job.Target],
		}
		if sheet, err := strconv.Atoi(job.Meta["sheet"]); err == nil {
			fail.Sheet = sheet
		}
		seen := map[ocr.Rule]bool{}
		for _, event := range job.History {
			if event.OK || event.Reason == "" {
				continue
			}
			fail.Reason, fail.Last, fail.Host = event.Reason, event.TS, event.Host
			for _, rule := range ocr.ParseRules(event.Reason) {
				if !seen[rule] {
					seen[rule] = true
					fail.Rules = append(fail.Rules, rule)
				}
			}
		}
		sortRules(fail.Rules)
		out = append(out, fail)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Target < out[b].Target })
	return out, nil
}

// sortRules puts them in the order Validate runs them rather than
// alphabetically, so that two pages with the same two rules print the same
// class.
func sortRules(rules []ocr.Rule) {
	order := map[ocr.Rule]int{}
	for i, rule := range ocr.AllRules {
		order[rule] = i
	}
	sort.Slice(rules, func(a, b int) bool { return order[rules[a]] < order[rules[b]] })
}

// FailureMarkdown is the reports/ocr-failures.md document.
//
// It is written for somebody who has to work through the list by hand, so it
// leads with the count per class and then gives the pages grouped by issue.
// Empty is a real answer and prints as one: a run with nothing dead should
// produce a file that says so rather than no file, because a missing file is
// indistinguishable from a report nobody generated.
func FailureMarkdown(list []Failure, from, to int, generated time.Time) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Pages that never read\n\n")
	fmt.Fprintf(&out, "Every page of %d to %d that ran out of attempts, with the rule that killed it.\n", from, to)
	fmt.Fprintf(&out, "Generated %s by kvant report failures.\n\n", generated.UTC().Format("2006-01-02"))

	if len(list) == 0 {
		out.WriteString("Nothing. Every page of every issue in the range was accepted.\n")
		return out.String()
	}

	fmt.Fprintf(&out, "%d pages over %d issues.\n\n", len(list), countIssues(list))
	out.WriteString("## By class\n\n")
	out.WriteString("| class | pages | what it means |\n|---|---|---|\n")
	for _, row := range byClass(list) {
		fmt.Fprintf(&out, "| %s | %d | %s |\n", row.class, row.count, meaning(row.class))
	}

	out.WriteString("\n## By issue\n\n")
	for _, group := range byIssue(list) {
		fmt.Fprintf(&out, "### %s\n\n", group.issue)
		out.WriteString("| sheet | attempts | class | last reason | repair queued |\n|---|---|---|---|---|\n")
		for _, fail := range group.pages {
			fmt.Fprintf(&out, "| %d | %d | %s | %s | %s |\n",
				fail.Sheet, fail.Attempts, fail.Class(), cell(fail.Reason), yesno(fail.Repair))
		}
		out.WriteString("\n")
	}
	return out.String()
}

type classRow struct {
	class string
	count int
}

func byClass(list []Failure) []classRow {
	count := map[string]int{}
	for _, fail := range list {
		count[fail.Class()]++
	}
	rows := make([]classRow, 0, len(count))
	for class, n := range count {
		rows = append(rows, classRow{class: class, count: n})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].count != rows[b].count {
			return rows[a].count > rows[b].count
		}
		return rows[a].class < rows[b].class
	})
	return rows
}

type issueGroup struct {
	issue string
	pages []Failure
}

func byIssue(list []Failure) []issueGroup {
	order := []string{}
	pages := map[string][]Failure{}
	for _, fail := range list {
		if _, ok := pages[fail.Issue]; !ok {
			order = append(order, fail.Issue)
		}
		pages[fail.Issue] = append(pages[fail.Issue], fail)
	}
	sort.Strings(order)
	groups := make([]issueGroup, 0, len(order))
	for _, issue := range order {
		list := pages[issue]
		sort.Slice(list, func(a, b int) bool { return list[a].Sheet < list[b].Sheet })
		groups = append(groups, issueGroup{issue: issue, pages: list})
	}
	return groups
}

func countIssues(list []Failure) int {
	seen := map[string]bool{}
	for _, fail := range list {
		seen[fail.Issue] = true
	}
	return len(seen)
}

// meaning says what to do about a class, because the rule name alone tells
// somebody which check fired and not what they are looking at.
func meaning(class string) string {
	if strings.Contains(class, "+") {
		return "more than one rule, read the pages"
	}
	switch ocr.Rule(class) {
	case ocr.RuleShort:
		return "the answer was truncated or the sheet is nearly blank"
	case ocr.RuleMath:
		return "unbalanced math delimiters, usually a formula run into the text"
	case ocr.RuleLeak:
		return "a refusal or the model narrating instead of transcribing"
	case ocr.RuleLanguage:
		return "the page came back translated instead of transcribed"
	case ocr.RuleFolio:
		return "no printed number, or one that contradicts the sheet"
	case ocr.RuleIllegible:
		return "the scan is too poor to read, try a higher resolution render"
	case ocr.RuleLaTeX:
		return "a math span does not parse"
	case ocr.RuleScript:
		return "two alphabets welded into one word, the lane was sampling"
	case "no rule":
		return "the service failed rather than the page, worth retrying"
	}
	return "no attempt was recorded"
}

// cell keeps a reason inside a table cell. The pipe is what a Markdown table
// cannot carry and the rules do not write one, but a service error might.
func cell(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "|", "/")
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > 90 {
		return string([]rune(text)[:89]) + "…"
	}
	if text == "" {
		return "nothing recorded"
	}
	return text
}

func yesno(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
