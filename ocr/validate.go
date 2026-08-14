// Package ocr reads the pages of a scanned issue through a model and decides
// whether what came back is a transcription.
//
// The decision is the expensive half. A call is about two and a half minutes,
// measured, so an accepted page that is wrong is not caught by anything
// downstream until a person reads it, and there are 34000 of them. Everything
// here is written to reject cheaply and to say which rule rejected, because the
// retry that follows is chosen from the reason.
package ocr

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/answerguard"
	"github.com/tamnd/kvant-solver/mathtex"
)

// MinChars is the shortest a real page can be.
//
// A page of this magazine that carries text at all carries a column of it. The
// thinnest are the closing page of an article with four lines and a colophon,
// which comes to about 300 characters. Two hundred is under that and well above
// what a truncated answer or a one line apology comes to.
const MinChars = 200

// MaxIllegible is how many unreadable spots a page may carry and still be
// accepted. These are 1970s scans of 1970s printing, so some are expected. More
// than four is a model that gave up, and re-reading at a higher resolution is
// worth the call.
const MaxIllegible = 4

// Rule names the check that rejected a page. It goes in the queue history and
// in the failures report, and the retry policy reads it.
type Rule string

const (
	RuleShort     Rule = "short"     // 1, empty or under MinChars
	RuleMath      Rule = "math"      // 2, unbalanced $ or $$
	RuleLeak      Rule = "leak"      // 3, refusal, narration or the prompt back
	RuleLanguage  Rule = "language"  // 4, the page came back translated
	RuleFolio     Rule = "folio"     // 5, no folio line, or one that contradicts the sheet
	RuleIllegible Rule = "illegible" // 6, too many unreadable spots
	RuleLaTeX     Rule = "latex"     // 7, a math span does not parse
	RuleScript    Rule = "script"    // 8, Latin welded into the middle of Russian words
)

// Problem is one reason a page was not accepted.
type Problem struct {
	Rule   Rule
	Detail string
	Line   int
}

func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s: %s (line %d)", p.Rule, p.Detail, p.Line)
	}
	return fmt.Sprintf("%s: %s", p.Rule, p.Detail)
}

// Expect is what the pipeline already knows about a page before it is read,
// which is what makes rule 5 possible at all.
type Expect struct {
	// Issue is the issue key, for the message.
	Issue string
	// Sheet is the position in the scan, one based. It is not the printed
	// number and the two differ by the offset of the front matter.
	Sheet int
	// Folio is the printed number this page is expected to carry, and 0 when
	// nothing knows yet. The first issue read of a year sets it for the rest.
	Folio int
	// Cover is a page that prints no folio and carries almost no text: the two
	// covers and the inside covers. Rules 1 and 5 do not run on one.
	Cover bool
}

// Options turn the opt-in rules on.
type Options struct {
	// LaTeX runs rule 7. It is opt-in because it costs a JavaScript engine and
	// a parse per span, and the other rules catch almost everything.
	LaTeX TeXChecker
}

// TeXChecker parses a fragment and reports what went wrong. Behind an interface
// so the rule can be tested without a renderer.
type TeXChecker interface {
	Check(fragment string, display bool) error
}

// Validate applies the rules and returns everything that failed.
//
// All of them run rather than stopping at the first. A page that is short and
// has no folio is a different failure from one that is only short: the first is
// a truncated answer and the second is a page whose head the scan lost, and the
// retry differs.
func Validate(text string, expect Expect, options Options) []Problem {
	var problems []Problem

	// Rule 3 first, even though it is numbered third. A refusal is short, has
	// no folio and no mathematics, so checking it first means the report says
	// what actually happened rather than listing four symptoms.
	for _, leak := range answerguard.Check(text) {
		problems = append(problems, Problem{Rule: RuleLeak, Detail: leak.Kind + ": " + leak.Detail, Line: leak.Line})
	}
	if len(problems) > 0 {
		return problems
	}

	body := strings.TrimSpace(text)

	// Rule 4. It comes early for the same reason: a page answered in English is
	// not a page with a folio problem, it is a page in the wrong language, and
	// the report should say so once.
	if leak, found := answerguard.Russian(body); found {
		return []Problem{{Rule: RuleLanguage, Detail: leak.Detail}}
	}

	// Rule 1.
	if !expect.Cover && len([]rune(body)) < MinChars {
		problems = append(problems, Problem{
			Rule:   RuleShort,
			Detail: fmt.Sprintf("%d characters, want at least %d", len([]rune(body)), MinChars),
		})
	}

	// Rule 2.
	if problem, ok := checkMath(body); !ok {
		problems = append(problems, problem)
	}

	// Rule 6.
	if n := strings.Count(body, Illegible); n > MaxIllegible {
		problems = append(problems, Problem{
			Rule:   RuleIllegible,
			Detail: fmt.Sprintf("%d unreadable spots, want at most %d", n, MaxIllegible),
		})
	}

	// Rule 5.
	if problem, ok := checkFolio(body, expect); !ok {
		problems = append(problems, problem)
	}

	// Rule 8.
	if problem, ok := checkScript(body); !ok {
		problems = append(problems, problem)
	}

	// Rule 7.
	if options.LaTeX != nil {
		problems = append(problems, checkLaTeX(body, options.LaTeX)...)
	}
	return problems
}

// OK says whether a page is accepted.
func OK(text string, expect Expect, options Options) bool {
	return len(Validate(text, expect, options)) == 0
}

// checkMath is rule 2. The splitter is mathtex, which is the same one the
// corpus was written with, so a span the audit reads later is the span this
// rule looked at.
func checkMath(text string) (Problem, bool) {
	_, unclosed := mathtex.Split(text)
	if unclosed == nil {
		return Problem{}, true
	}
	what := "an inline $"
	if unclosed.Display {
		what = "a display $$"
	}
	return Problem{
		Rule:   RuleMath,
		Detail: what + " opened here is never closed",
		Line:   unclosed.Line,
	}, false
}

// checkFolio is rule 5.
//
// Two things are asked. The page has to answer the question, with a number or
// with none, because the page map is built out of those lines and a page that
// answered neither leaves a hole in it. And where the pipeline already expects a
// number, the printed one has to be within one of it.
//
// One page of slack, and the reason is the magazine rather than the model. An
// issue of Квант is printed as sheets, so a sheet of the scan is one printed
// page for most of the run and two for the years the covers were counted, and
// the offset moves by one at the boundary. Being out by more than that is a
// misread digit or a page fetched out of order, which is worth a call.
func checkFolio(text string, expect Expect) (Problem, bool) {
	if expect.Cover {
		return Problem{}, true
	}
	if !HasFolioLine(text) {
		return Problem{
			Rule:   RuleFolio,
			Detail: "no ⟦folio⟧ line, so this page cannot go in the page map",
		}, false
	}
	printed, ok := Folio(text)
	if !ok || expect.Folio == 0 {
		return Problem{}, true
	}
	if diff := printed - expect.Folio; diff > 1 || diff < -1 {
		return Problem{
			Rule:   RuleFolio,
			Detail: fmt.Sprintf("the page prints %d, sheet %d of %s was expected to print %d", printed, expect.Sheet, expect.Issue, expect.Folio),
		}, false
	}
	return Problem{}, true
}

// MaxMixed is how many words may mix two alphabets before a page is re-read.
//
// Not zero, because a scan of small print does produce the odd one honestly:
// МГУ read with a Latin M is a real misread and one of those on a page is not
// evidence that the whole page is wrong. Four is the same allowance rule 6
// gives unreadable spots, and it sits well clear of both measurements it was
// set from: a good read of these sheets averages 1.3 a page and the bad one
// averaged 14.3.
const MaxMixed = 4

// checkScript is rule 8, and it is here because of a run that passed every
// other rule and was not a transcription.
//
// A multilingual model sampling at temperature does not fail loudly. It writes
// Russian with fragments of another alphabet welded into the middle of words,
// dvижении for движении and Funcцию for функцию, and on one run of 1975 №1
// there were 836 of them across 83 of the 84 pages. Every one of those pages
// was the right length, in the right language, with balanced mathematics and a
// correct folio, so rules 1 through 7 all passed it. The words are the thing
// that is wrong and nothing else looks at the words.
//
// A word here means a run of letters, and mixing alphabets inside one is the
// signal. Between words is normal: this magazine prints Latin variable names in
// its prose all the time.
//
// The first version of this asked only about Latin, and that was too narrow.
// Reading a physics page against the scan turned up с怠митр for сантиметр, one
// Han character in the middle of a Russian word, which the Latin test could not
// see. A model that is loose enough to reach for one alphabet is loose enough
// to reach for any, so the question is now about Cyrillic mixed with anything
// rather than about Latin.
func checkScript(text string) (Problem, bool) {
	found, first := mixedWords(text)
	if found <= MaxMixed {
		return Problem{}, true
	}
	return Problem{
		Rule: RuleScript,
		Detail: fmt.Sprintf("%d words mix two alphabets, such as %q, want at most %d",
			found, first, MaxMixed),
	}, false
}

// MixedWords counts the words on a page that carry two alphabets and returns
// the first one, so the audit can report on a corpus that is already written.
// The rule above decides whether to accept a page as it is read; this is the
// same count asked of a page that was accepted a month ago.
func MixedWords(text string) (int, string) { return mixedWords(text) }

// mixedWords counts the words carrying two alphabets and returns the first.
//
// The mathematics is blanked out first. A math span is Latin by definition and
// touches the Russian around it, so counting words inside one would report
// every page in the corpus.
func mixedWords(text string) (int, string) {
	runes := []rune(text)
	spans, _ := mathtex.Split(text)
	for _, span := range spans {
		for i := span.Start; i < span.End && i < len(runes); i++ {
			runes[i] = ' '
		}
	}

	found, first := 0, ""
	for _, word := range strings.FieldsFunc(string(runes), func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		var cyrillic, other bool
		for _, r := range word {
			if unicode.Is(unicode.Cyrillic, r) {
				cyrillic = true
				continue
			}
			// Everything else that is a letter counts, which is the point of
			// the change: Latin, Han, Greek, Armenian. A page of this magazine
			// is Russian prose, and a letter inside a Russian word that is not
			// a Russian letter is a misread whatever alphabet it came from.
			if unicode.IsLetter(r) {
				other = true
			}
		}
		if cyrillic && other {
			found++
			if first == "" {
				first = word
			}
		}
	}
	return found, first
}

// checkLaTeX is rule 7. Every span on the page is parsed, not a sample: a page
// with one formula that does not compile is a page the site cannot render, and
// finding that out at publish time means finding it out about the whole corpus
// at once.
func checkLaTeX(text string, checker TeXChecker) []Problem {
	spans, _ := mathtex.Split(text)
	var problems []Problem
	for _, span := range spans {
		if err := checker.Check(span.Text, span.Display); err != nil {
			problems = append(problems, Problem{
				Rule:   RuleLaTeX,
				Detail: fmt.Sprintf("%s: %v", clip(span.Text), err),
				Line:   span.Line,
			})
		}
	}
	return problems
}

func clip(s string) string {
	runes := []rune(s)
	if len(runes) <= 60 {
		return s
	}
	return string(runes[:60]) + "…"
}

// Reasons joins the problems into the one line that goes in the queue history.
func Reasons(problems []Problem) string {
	if len(problems) == 0 {
		return ""
	}
	parts := make([]string, 0, len(problems))
	for _, problem := range problems {
		parts = append(parts, problem.String())
	}
	return strings.Join(parts, "; ")
}

// Rules returns the distinct rules that rejected a page, which is what the
// retry policy switches on.
func Rules(problems []Problem) []Rule {
	seen := map[Rule]bool{}
	var out []Rule
	for _, problem := range problems {
		if !seen[problem.Rule] {
			seen[problem.Rule] = true
			out = append(out, problem.Rule)
		}
	}
	return out
}
