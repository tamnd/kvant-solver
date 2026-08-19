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
	"regexp"
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/answerguard"
	"github.com/tamnd/kvant-solver/lexicon"
	"github.com/tamnd/kvant-solver/mathtex"
)

// MinChars is the shortest a real page can be.
//
// A page of this magazine that carries text at all carries a column of it. The
// thinnest are the closing page of an article with four lines and a colophon,
// which comes to about 300 characters. Two hundred is under that and well above
// what a truncated answer or a one line apology comes to.
const MinChars = 200

// MaxFurnitureRunes is how long a line can be and still be apparatus rather
// than text. The signature line at the foot of a sheet, the rule above a
// footnote and a running head are all under it, and a line of this magazine set
// in two columns is about 45 characters, so a sentence does not fit.
const MaxFurnitureRunes = 40

// MaxIllegible is how many unreadable spots a page may carry and still be
// accepted. These are 1970s scans of 1970s printing, so some are expected. More
// than four is a model that gave up, and re-reading at a higher resolution is
// worth the call.
const MaxIllegible = 4

// Rule names the check that rejected a page. It goes in the queue history and
// in the failures report, and the retry policy reads it.
type Rule string

// The nine rules, in the order Validate runs them.
const (
	RuleShort     Rule = "short"     // 1, empty or under MinChars
	RuleMath      Rule = "math"      // 2, unbalanced $ or $$
	RuleLeak      Rule = "leak"      // 3, refusal, narration or the prompt back
	RuleLanguage  Rule = "language"  // 4, the page came back translated
	RuleFolio     Rule = "folio"     // 5, no folio line, or one that contradicts the sheet
	RuleIllegible Rule = "illegible" // 6, too many unreadable spots
	RuleLaTeX     Rule = "latex"     // 7, a math span does not parse
	RuleScript    Rule = "script"    // 8, Latin welded into the middle of Russian words
	RuleRunaway   Rule = "runaway"   // 9, the decoder got stuck and repeated itself
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
	// Lexicon narrows rule 8 to the mixed words that are not just a Russian
	// word spelled in two alphabets. Nil is the rule as it stood before there
	// was a lexicon, counting every one of them, which is what a corpus with no
	// lexicon built for it should get.
	Lexicon *lexicon.Lexicon
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
	if !expect.Cover && !Plate(body) && len([]rune(body)) < MinChars {
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
	if problem, ok := checkScript(body, options.Lexicon); !ok {
		problems = append(problems, problem)
	}

	// Rule 9.
	if problem, ok := checkRunaway(body); !ok {
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

// Plate says whether a page is a full page figure, which is the one honest way
// a page of this magazine comes to forty characters.
//
// The prompt does not describe pictures. A figure gets a ⟦figure⟧ line and its
// printed caption and nothing else, so a sheet given over to one plate is a
// folio line, a marker and a line reading Рис. 9. Эпициклоиды. That is 42
// characters, measured on 1975 issue 5 sheet 31, and rule 1 was throwing every
// one of them back at a lane that read them correctly the first time and read
// them the same way the second.
//
// The whole page has to be accounted for, line by line, and that is what keeps
// the exemption away from a truncated answer. The prompt says where a caption
// goes: on the line under the marker. So a plate is a folio line, figure
// markers, the line under each one, the printer's furniture, and nothing else.
// A model that stopped in the middle of a column leaves a line of prose that
// belongs to no figure, and that line is what fails it, however short the page
// came to.
//
// Length is not asked about, and asking would be the wrong question twice over.
// A plate with three figures and three long captions is still a plate, and a
// page truncated after forty characters is still truncated.
//
// The furniture is the loose part and it is loose because it has to be. The
// second plate this was tested against, 1975 issue 5 sheet 35, is two figures
// and then the rule and signature line the magazine prints at the foot of every
// sheet, and there is no way to recognise a signature line except that it is
// short. What that costs was measured rather than argued: of 944 accepted body
// pages, 77 carry a figure in their first 200 characters and 2 of those 77 have
// nothing longer than a furniture line in front of them. So a body page cut off
// at exactly the wrong point could pass here about twice in a thousand, and
// both of the two are plates that a column of text continues under.
//
// The caption is the part that costs something. The prompt allows a figure with
// no caption and the magazine prints them, so an uncaptioned plate is a real
// page and this rejects it. It goes in the failures report as a class somebody
// has already looked at, which is the trade taken deliberately: a handful of
// plates read again by hand is cheaper than a truncated page going into the
// corpus quietly, because the first is visible and the second is not.
func Plate(text string) bool {
	if !HasFolioLine(text) {
		return false
	}
	figures, captions, after := 0, 0, false
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		caption := after
		after = false
		switch {
		case trimmed == "":
		case trimmed == FigureMarker:
			figures++
			after = true
		case folioLine.MatchString(trimmed), trimmed == NoFolio, trimmed == ColumnMarker:
		case caption:
			captions++
		case len([]rune(trimmed)) <= MaxFurnitureRunes:
		default:
			return false
		}
	}
	return figures > 0 && captions > 0
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
//
// It stayed at four when the lexicon narrowed what gets counted, which wants
// saying, because narrowing the count without moving the number is a loosening.
// It was left alone because the corpus measures the same either way: over the
// 977 pages accepted into 1975 and 1976 the most any one of them carries is
// exactly four unplaceable mixed words, so nothing in the corpus today got
// there by a margin this moves. What moves is the pages that were thrown away.
// Of seventeen dead ones read again, fourteen come in at four or under, and the
// three that do not are Оlimпилад six times over, MEMORIALНОГО COMПЛЕКСА and
// перpendикularы, which is the rule finding what it is for.
const MaxMixed = 4

// chessMove is a move in the notation this magazine prints, which mixes two
// alphabets on purpose.
//
// Шахматная страничка is a standing rubric and the moves in it are set the
// Russian way: the piece is a Cyrillic letter and the square is a Latin file
// and a rank, so Фd3 and Крg1 and Лf1 are correct transcriptions of correct
// printing. Rule 8 was rejecting the whole page for them. 1975 issue 6 sheet 60
// is one board and its solution and it came back with 21 of them, read the same
// way three times, which is what a page the rule is wrong about looks like.
//
// The pattern is tight on purpose. A piece letter has to start a word and a
// Latin file and rank have to close the move, which is a shape no Russian word
// takes and no misread has produced: dvижении and Funcцию and с怠митр are all
// still counted. Pawn moves and squares on their own are Latin throughout and
// were never a problem for a rule that only asks about mixing.
var chessMove = regexp.MustCompile(`(^|[^\p{L}])(Кр|[КФЛСП])[a-h]?[1-8]?[:×x]?[a-h][1-8]`)

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
//
// With a lexicon the count is narrower. Reading 1976 through 1981 says most of
// what rule 8 catches is not a misreading at all: однako and tetraэдра are
// «однако» and «тетраэдра» with the Latin spelling showing through, and the
// model that wrote them had read the page correctly. Those are not evidence
// against a page and counting them meant a page died for a habit. What is left
// after the lexicon has taken them out is Оlimпилад and MEMORIALНОГО, which are
// not words, and those are the evidence. See the lexicon package for why a word
// is only taken out when the corpus already uses the form it resolves to.
func checkScript(text string, lex *lexicon.Lexicon) (Problem, bool) {
	found, first := mixedWords(text, lex)
	if found <= MaxMixed {
		return Problem{}, true
	}
	return Problem{
		Rule: RuleScript,
		Detail: fmt.Sprintf("%d words mix two alphabets and are not Russian spelled in two of them, such as %q, want at most %d",
			found, first, MaxMixed),
	}, false
}

// MixedWords counts the words on a page that carry two alphabets and returns
// the first one, so the audit can report on a corpus that is already written.
// The rule above decides whether to accept a page as it is read; this is the
// same count asked of a page that was accepted a month ago.
func MixedWords(text string, lex *lexicon.Lexicon) (int, string) { return mixedWords(text, lex) }

// mixedWords counts the words carrying two alphabets and returns the first.
//
// The mathematics is blanked out first. A math span is Latin by definition and
// touches the Russian around it, so counting words inside one would report
// every page in the corpus.
func mixedWords(text string, lex *lexicon.Lexicon) (int, string) {
	runes := []rune(text)
	spans, _ := mathtex.Split(text)
	for _, span := range spans {
		for i := span.Start; i < span.End && i < len(runes); i++ {
			runes[i] = ' '
		}
	}

	found, first := 0, ""
	for _, word := range strings.FieldsFunc(chessMove.ReplaceAllString(string(runes), "$1 "), func(r rune) bool {
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
		if !cyrillic || !other {
			continue
		}
		// A word the lexicon can turn back into Russian is a spelling and not a
		// misreading, so it does not count against the page.
		if lex != nil && lex.Resolves(word) {
			continue
		}
		found++
		if first == "" {
			first = word
		}
	}
	return found, first
}

// MaxWordRunes is the longest run of letters that can still be a word.
//
// The longest one in the 891 pages this was set from is 33 letters, and that
// one is itself a misread, некоторыхfundamentальныхпроблемах with the spaces
// eaten. The longest honest Russian on those pages is 29. Above that the
// measurements stop: the next four are 48, 938, 2257 and 2917, and all four are
// a decoder that got stuck. Forty leaves the real text a third more room than
// it has ever used and still sits twenty times below the smallest failure.
const MaxWordRunes = 40

// MaxPageWords is the most words a printed page of this magazine can hold.
//
// The measurement is not close. Over the 16764 pages in the corpus the median
// is 434 words and the ninety ninth percentile is 877, which is a full page set
// in two columns with no figure on it. Then the distribution stops being a
// distribution: the next half percent runs 2733, 6429, 9496, and every one of
// those is a decoder that got stuck rather than a page somebody printed.
//
// Twelve hundred sits a third above the highest honest page, which is the same
// headroom MaxWordRunes leaves, and it sits below every failure but one. Ninety
// nine pages of the corpus are over it and ninety seven of those are over two
// thousand, so the band this threshold could be wrong about holds two pages.
const MaxPageWords = 1200

// checkRunaway is rule 9, and it is here because three pages of 1975 went into
// the corpus as немесямедысимедысимедысимедыси for two thousand nine hundred
// letters.
//
// A served model decoding greedily can fall into a loop and emit the same
// syllable until it runs out of context. It is the ugliest failure in the set
// and it was the only one no rule could see. The page is the right length,
// because it is far too long rather than too short. The mathematics balances,
// because there is none left. The language is Russian, the folio is right, and
// rule 8 is blind to it because a loop in Cyrillic never leaves the alphabet.
// One of the three was a 102 KB page file next to a median of 3 KB.
//
// The first signal is that no word is this long. A run of letters past
// MaxWordRunes is not a word in any language this magazine prints, so whatever
// produced it was not reading.
//
// The second signal is that no page is this long, and it is here because the
// first one only sees a loop that forgets to press space. This rule used to say
// that repetition was the wrong thing to look for, and reading the corpus
// against it says that was wrong: 1980 issue 7 sheet 51 is a 52 KB page whose
// text is MBТУ four hundred and twenty three times, and rule 9 could not see it
// because MBТУ is four letters long. Ninety seven more pages are the same
// failure with a different token. Rule 8 caught that one by luck, because the
// token it repeated happened to mix two alphabets, and a loop stuck on a
// Cyrillic word walks past every rule in this file.
//
// Length is the cheap way to ask. A loop has to put its output somewhere and a
// printed page has a size, so a page carrying three times more words than the
// longest sheet this magazine ever set did not come off that sheet. It costs one
// pass over the words that rule 8 is about to make anyway.
func checkRunaway(text string) (Problem, bool) {
	longest, word := LongestWord(text)
	if longest > MaxWordRunes {
		return Problem{
			Rule: RuleRunaway,
			Detail: fmt.Sprintf("a run of %d letters, %q, and no word is that long, so the model was repeating itself",
				longest, clip(word)),
		}, false
	}
	if n := PageWords(text); n > MaxPageWords {
		return Problem{
			Rule: RuleRunaway,
			Detail: fmt.Sprintf("%d words, and no page of this magazine holds more than %d, so the model was repeating itself",
				n, MaxPageWords),
		}, false
	}
	return Problem{}, true
}

// PageWords counts the words on a page.
//
// Exported for the same reason LongestWord is: the audit asks it of pages that
// were written before the rule existed, and ninety eight of those are in the
// corpus.
func PageWords(text string) int {
	return len(strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) }))
}

// LongestWord returns the longest run of letters on a page and how long it is.
//
// Exported for the same reason MixedWords is: the audit asks it of pages that
// were written before the rule existed, and three of those are in the corpus.
func LongestWord(text string) (int, string) {
	longest, found := 0, ""
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		if n := len([]rune(word)); n > longest {
			longest, found = n, word
		}
	}
	return longest, found
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

// ParseRules reads the rules back out of a line Reasons wrote.
//
// The failures report is built long after the run, from the queue history and
// from the ledger, and both of those hold the sentence rather than the problems
// it came from. Reading the names back is a little inelegant, but the
// alternative is a second copy of the rule list in every job file, and the
// format it is parsing is one this package also writes.
//
// Anything that is not a rule name is dropped. A page can fail for a reason the
// rules never had, such as the service being down, and calling that rule zero
// would put an outage in the same column as a bad scan.
func ParseRules(reason string) []Rule {
	known := map[Rule]bool{}
	for _, rule := range AllRules {
		known[rule] = true
	}
	seen := map[Rule]bool{}
	var out []Rule
	for part := range strings.SplitSeq(reason, ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			continue
		}
		rule := Rule(strings.TrimSpace(name))
		if known[rule] && !seen[rule] {
			seen[rule] = true
			out = append(out, rule)
		}
	}
	return out
}

// AllRules is the nine in the order Validate runs them, for the reports that
// print a column per rule.
//
// Adding a rule means adding it here as well, and the cost of forgetting is not
// a missing column. ParseRules reads this list to turn a stored reason back into
// the rule that produced it, so a rule absent from it is a rule the failure
// report cannot name: rule 9 was left off when it was written, and every page it
// killed came back classed "no rule", which the report glosses as the service
// failing rather than the page. Fifty eight pages of the Soviet decades were
// filed under an outage that never happened.
var AllRules = []Rule{
	RuleShort, RuleMath, RuleLeak, RuleLanguage,
	RuleFolio, RuleIllegible, RuleLaTeX, RuleScript,
	RuleRunaway,
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
