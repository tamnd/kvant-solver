// Package audit says whether an issue in the corpus is finished, and names
// every reason it is not.
//
// It is the gate the milestones are written against, and it exists because
// finished is not something a person can see by looking at a directory. An
// issue with 79 of its 80 pages looks exactly like one with all 80 until you
// count them, an article whose text is really the page before it looks like an
// article, and a formula that does not compile looks like a formula until the
// site is built. All three have happened.
//
// So the rule is that nothing is signed off on a reading of the output. The
// audit reads the corpus back, checks it against what the manifests say should
// be there, and prints a list. An empty list is the only pass.
package audit

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/mathtex"
	"github.com/tamnd/kvant-solver/ocr"
)

// Level says how bad a finding is.
//
// Two levels and not five. A finding either stops the issue being signed off or
// it does not, and a scale with a middle is a scale where everything lands in
// the middle. Warn is for the things that are known to be normal in some issues
// and wrong in others, such as a page no article claims, which is a cover most
// of the time and a lost contents row occasionally.
type Level string

const (
	Fail Level = "fail"
	Warn Level = "warn"
)

// Finding is one thing wrong.
type Finding struct {
	Level Level
	// Rule is the short name, so that a report can be counted by kind.
	Rule string
	// Where is the page or the article, as an id, or the issue when the finding
	// is about the issue as a whole.
	Where  string
	Detail string
}

func (f Finding) String() string {
	return fmt.Sprintf("%-4s %-16s %-24s %s", f.Level, f.Rule, f.Where, f.Detail)
}

// Report is one issue audited.
type Report struct {
	Issue    string
	Pages    int
	Articles int
	Findings []Finding
}

// OK is the exit criterion: no failures. Warnings are printed and do not stop
// a sign-off, because a run that treats every cover as a defect trains people
// to ignore the output.
func (r *Report) OK() bool {
	for _, finding := range r.Findings {
		if finding.Level == Fail {
			return false
		}
	}
	return true
}

// Counts is the summary line, findings by rule.
func (r *Report) Counts() string {
	counts := map[string]int{}
	for _, finding := range r.Findings {
		counts[finding.Rule]++
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, ", ")
}

func (r *Report) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s: %d pages, %d articles, %s\n", r.Issue, r.Pages, r.Articles, r.Counts())
	for _, finding := range r.Findings {
		fmt.Fprintf(&out, "  %s\n", finding)
	}
	return out.String()
}

func (r *Report) add(level Level, rule, where, detail string) {
	r.Findings = append(r.Findings, Finding{Level: level, Rule: rule, Where: where, Detail: detail})
}

// Page is one page file, read back.
type Page struct {
	Index int
	Front corpus.PageFront
	Body  string
}

// Article is one article file, read back.
type Article struct {
	Path  string
	Front corpus.ArticleFront
	Body  string
}

// Input is everything the audit compares.
type Input struct {
	Key corpus.IssueKey
	// Sheets is how many surfaces the scan has, from the issue manifest. Zero
	// means the manifest does not say, and then the count rule cannot run,
	// which is itself reported: an issue whose length nobody knows cannot be
	// declared complete.
	Sheets   int
	Rows     []manifest.Row
	Pages    []Page
	Articles []Article
	// Prompts are the hashes of the prompts in this build. A page written by
	// none of them is stale and is worth saying so, because the difference
	// between the old prompt and the new one is usually the reason a rule
	// started firing.
	//
	// It is a set and not one hash because a page can honestly have been read
	// by either of two prompts: the long one a general model is given, and the
	// sentence a document model is given. Both are current.
	Prompts []string
	// LaTeX is the renderer. Nil skips the formula rule, which is what a quick
	// pass over a whole year does; the sign-off run always passes one.
	LaTeX ocr.TeXChecker
}

// Issue audits one issue.
func Issue(in Input) *Report {
	report := &Report{Issue: in.Key.String(), Pages: len(in.Pages), Articles: len(in.Articles)}
	sort.Slice(in.Pages, func(i, j int) bool { return in.Pages[i].Index < in.Pages[j].Index })

	completeness(report, in)
	pages(report, in)
	articles(report, in)
	contents(report, in)
	return report
}

// completeness is the first rule and the one the milestone is written around:
// every sheet of the scan has a page file, numbered from one with no gaps.
func completeness(report *Report, in Input) {
	if in.Sheets <= 0 {
		report.add(Warn, "sheet_count", in.Key.String(),
			"the issue manifest does not say how many sheets the scan has, so completeness cannot be checked")
	}
	seen := map[int]bool{}
	for _, page := range in.Pages {
		if seen[page.Index] {
			report.add(Fail, "duplicate_page", pageID(in.Key, page.Index), "two page files claim this index")
		}
		seen[page.Index] = true
	}
	if in.Sheets > 0 {
		var missing []int
		for index := 1; index <= in.Sheets; index++ {
			if !seen[index] {
				missing = append(missing, index)
			}
		}
		if len(missing) > 0 {
			report.add(Fail, "missing_pages", in.Key.String(),
				fmt.Sprintf("%d of %d sheets have no page file: %s", len(missing), in.Sheets, runs(missing)))
		}
		for index := range seen {
			if index > in.Sheets {
				report.add(Fail, "extra_page", pageID(in.Key, index),
					fmt.Sprintf("the scan has %d sheets", in.Sheets))
			}
		}
	}
}

// pages runs the rules that are about one page on its own.
func pages(report *Report, in Input) {
	labels := map[string][]int{}
	for _, page := range in.Pages {
		where := pageID(in.Key, page.Index)
		if strings.TrimSpace(page.Body) == "" {
			report.add(Fail, "empty_page", where, "the page file has no body")
			continue
		}
		if page.Front.Issue != in.Key.String() {
			report.add(Fail, "wrong_issue", where,
				fmt.Sprintf("the front matter says %s", page.Front.Issue))
		}
		if stale(page.Front.PromptSHA256, in.Prompts) {
			report.add(Warn, "stale_prompt", where,
				"this page was read by a prompt that is not in this build")
		}
		if !ocr.HasFolioLine(page.Body) {
			report.add(Fail, "no_folio", where,
				"the page answers neither a number nor none, so it cannot go in the page map")
		}
		if label := page.Front.PageLabel; label != "" {
			labels[label] = append(labels[label], page.Index)
		}
		if _, unclosed := mathtex.Split(page.Body); unclosed != nil {
			report.add(Fail, "unclosed_math", where,
				fmt.Sprintf("a math span opened on line %d is never closed", unclosed.Line))
		}
		if in.LaTeX != nil {
			formulas(report, where, page.Body, in.LaTeX)
		}
		if n := strings.Count(page.Body, ocr.Illegible); n > ocr.MaxIllegible {
			report.add(Warn, "illegible", where,
				fmt.Sprintf("%d unreadable spots", n))
		}
		// The same count the OCR rule applies as a page is read, asked again of
		// a page that is already in the corpus. It is here because the failure
		// it catches is invisible: a model sampling at temperature writes
		// Russian with another alphabet welded into the middle of words, and the
		// result is the right length, in the right language and correctly
		// paginated. A run that was accepted before this rule existed is only
		// found by looking.
		if n, first := ocr.MixedWords(page.Body); n > ocr.MaxMixed {
			report.add(Warn, "mixed_script", where,
				fmt.Sprintf("%d words mix two alphabets, such as %q", n, first))
		}
	}
	// Two pages printing the same number is a misread digit, and it is worth a
	// failure because the page map is built on these and assemble places every
	// article with them.
	for label, indexes := range labels {
		if len(indexes) > 1 {
			sort.Ints(indexes)
			report.add(Fail, "duplicate_folio", in.Key.String(),
				fmt.Sprintf("sheets %v all print page %s", indexes, label))
		}
	}
}

// formulas is the KaTeX rule. Every span, not a sample: one formula that does
// not compile is a page the site cannot render, and finding that at publish
// time means finding it out about the whole corpus at once.
func formulas(report *Report, where, body string, checker ocr.TeXChecker) {
	spans, _ := mathtex.Split(body)
	for _, span := range spans {
		if err := checker.Check(span.Text, span.Display); err != nil {
			report.add(Fail, "latex", where,
				fmt.Sprintf("line %d: %s: %v", span.Line, clip(span.Text), err))
		}
	}
}

// articles runs the rules that are about the assembled view.
func articles(report *Report, in Input) {
	byIndex := map[int]bool{}
	for _, page := range in.Pages {
		byIndex[page.Index] = true
	}
	claimed := map[int]string{}
	for _, article := range in.Articles {
		where := article.Front.ID
		if where == "" {
			where = filepath.Base(article.Path)
		}
		if strings.TrimSpace(article.Front.Title) == "" {
			report.add(Fail, "no_title", where, "the article has no title")
		}
		if strings.TrimSpace(article.Body) == "" {
			report.add(Fail, "empty_article", where, "the article has no body")
		}
		first, last := article.Front.PageFirst, article.Front.PageLast
		if first <= 0 || last < first {
			report.add(Fail, "bad_range", where,
				fmt.Sprintf("the article claims sheets %d to %d", first, last))
			continue
		}
		for index := first; index <= last; index++ {
			if !byIndex[index] {
				report.add(Fail, "missing_source_page", where,
					fmt.Sprintf("sheet %d has no page file", index))
				continue
			}
			// Overlap is not always wrong: two items share a page where one
			// ends halfway down it. It is a warning so a real double claim,
			// where a whole run of pages is in two articles, is still visible.
			if other, ok := claimed[index]; ok {
				report.add(Warn, "shared_page", where,
					fmt.Sprintf("sheet %d is also claimed by %s", index, other))
			}
			claimed[index] = where
		}
	}
	for _, page := range in.Pages {
		if _, ok := claimed[page.Index]; !ok {
			report.add(Warn, "orphan_page", pageID(in.Key, page.Index),
				"no article claims this page, which is normal for a cover and not for a body page")
		}
	}
}

// contents is the rule that the assembled issue matches what the publisher
// printed. This is the one that catches a whole article having been lost, which
// none of the page rules can see: 80 good pages with one article missing from
// the contents is 80 good pages.
func contents(report *Report, in Input) {
	if len(in.Rows) == 0 {
		report.add(Warn, "no_contents", in.Key.String(),
			"no printed contents was reconciled for this issue, so the articles cannot be checked against it")
		return
	}
	have := make(map[string]bool, len(in.Articles))
	for _, article := range in.Articles {
		have[fold(article.Front.Title)] = true
	}
	for _, row := range in.Rows {
		if row.Title == "" {
			continue
		}
		if !have[fold(row.Title)] {
			report.add(Fail, "missing_article", in.Key.String(),
				fmt.Sprintf("the printed contents lists %q and the corpus has no article for it", row.Title))
		}
	}
}

// fold is what two spellings of one title have in common: case, spacing and
// the punctuation that moves between the two mirrors' typing.
func fold(title string) string {
	var out strings.Builder
	space := true
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'а' && r <= 'я', r == 'ё':
			out.WriteRune(r)
			space = false
		default:
			if !space {
				out.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(out.String())
}

// stale reports whether a page was read by a prompt this build no longer has.
// A page that recorded no hash at all is old rather than stale, and there is
// nothing useful to say about it that the rules do not already say.
func stale(sum string, current []string) bool {
	if sum == "" || len(current) == 0 {
		return false
	}
	return !slices.Contains(current, sum)
}

func pageID(key corpus.IssueKey, index int) string {
	return corpus.PageID{Issue: key, Index: index}.String()
}

// runs writes a list of numbers as ranges, because "3-19, 44" is readable and
// seventeen comma separated numbers are not.
func runs(numbers []int) string {
	if len(numbers) == 0 {
		return ""
	}
	sort.Ints(numbers)
	var parts []string
	start, prev := numbers[0], numbers[0]
	flush := func() {
		if start == prev {
			parts = append(parts, strconv.Itoa(start))
			return
		}
		parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(prev))
	}
	for _, n := range numbers[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		flush()
		start, prev = n, n
	}
	flush()
	return strings.Join(parts, ", ")
}

func clip(text string) string {
	runes := []rune(text)
	if len(runes) <= 50 {
		return text
	}
	return string(runes[:50]) + "…"
}
