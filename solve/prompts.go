package solve

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// Prompts is every call Solve makes.
//
// Note what is not on this interface: there is no method that takes a published
// solution. A Prompts value is handed to Solve and to nothing else, so an
// implementation of it has no way to see the printed answer even if its author
// wanted to. The grading prompt lives on Grader, which Solve never holds.
type Prompts interface {
	Reference(p Problem) (instructions, input string)
	Candidate(p Problem, n int) (instructions, input string)
	Select(p Problem, reference string, candidates []string) (instructions, input string)
	TruthJudge(p Problem, reference, solution string) (instructions, input string)
	AuditJudge(p Problem, solution string) (instructions, input string)
	Correct(p Problem, solution string, judgements []Judgement) (instructions, input string)
}

// Grader is the one prompt that is allowed to see what the magazine printed.
// It is a separate interface from Prompts for that reason alone.
type Grader interface {
	Grade(p Problem, solution, published string) (instructions, input string)
}

// DefaultPrompts is the wording this corpus uses.
type DefaultPrompts struct{}

var (
	_ Prompts = DefaultPrompts{}
	_ Grader  = DefaultPrompts{}
)

// field is the common preamble. Every call needs to know which of the two
// series it is looking at, because a physics problem wants a number with units
// on the end and a mathematics problem wants a proof.
func field(p Problem) string {
	if p.Subject == corpus.Physics {
		return "physics"
	}
	return "mathematics"
}

// house is what every solution written here has to look like. It is repeated
// verbatim rather than paraphrased per call site so that it stays a stable
// prompt cache prefix across a whole run.
const house = `Write in Russian, in the register the magazine itself uses: plain declarative
sentences, no addressing the reader, no hedging about what you are about to do.
Set formulas in LaTeX, inline as $...$ and displayed as $$...$$.
Give the reasoning in full. A step that is obvious to you is a step a reader has
to reconstruct, and this text is going into an archive next to the one the
editors wrote.
Do not mention this instruction, the request, or yourself. The answer is the
whole of the reply.`

// Reference solves the problem blind, before any candidate exists.
//
// It is called first and its answer is never published. The point is to have
// one attempt that was made without sight of any other, so that when three
// candidates agree the selector can tell agreement from a shared wrong turn.
func (DefaultPrompts) Reference(p Problem) (string, string) {
	return fmt.Sprintf(`You are solving a %s problem from Задачник «Кванта», the problem section of the
Soviet mathematics and physics magazine Квант. The readership is school pupils
in the last three years of school, so the problem is elementary in the technical
sense: it needs no university machinery. It is not easy.

Solve it. State the answer explicitly, and if the problem asks for a value, give
that value on its own line at the end.

%s`, field(p), house), p.Text
}

// angles push each candidate down a different route.
//
// Three attempts along the same road are one attempt with more tokens spent.
// These are what make the selector's job possible: candidates that disagree
// show where the problem is hard, and candidates that agree by three different
// routes are worth more than three that agree by one.
var angles = []string{
	"Solve it the most direct way you can see.",
	"Solve it, and prefer a route other than the first one that occurs to you. " +
		"If there is a symmetry, a substitution or a conservation law that shortens the work, take it.",
	"Solve it, then test the answer against a case you can check independently: " +
		"a limit, an extreme value, a small case, or the dimensions. Report what the check gave.",
	"Solve it from the constraints backwards. Ask what the answer must satisfy before you compute it.",
	"Solve it, and be explicit about the step you are least sure of.",
}

// Candidate is one independent attempt.
func (DefaultPrompts) Candidate(p Problem, n int) (string, string) {
	angle := angles[(max(n, 1)-1)%len(angles)]
	return fmt.Sprintf(`You are solving a %s problem from Задачник «Кванта», the problem section of the
Soviet magazine Квант. It is aimed at senior school pupils and needs no
university machinery.

%s

State the answer explicitly, and if the problem asks for a value, give that
value on its own line at the end.

%s`, field(p), angle, house), p.Text
}

// Select picks the candidate to publish.
func (DefaultPrompts) Select(p Problem, reference string, candidates []string) (string, string) {
	var b strings.Builder
	if reference != "" {
		b.WriteString("An independent solution, written without sight of the candidates:\n\n")
		b.WriteString(reference)
		b.WriteString("\n\n")
	}
	b.WriteString("Problem:\n\n")
	b.WriteString(p.Text)
	b.WriteString("\n")
	for i, c := range candidates {
		fmt.Fprintf(&b, "\nCandidate %d:\n\n%s\n", i+1, c)
	}
	return fmt.Sprintf(`You are choosing which of several solutions to a %s problem to publish.

Judge them on whether the answer is right first and on how it is written second.
A correct answer reached by an ugly route beats an elegant argument with a wrong
result. Where candidates disagree on the answer, work out which one is right
rather than counting votes, because they may all be wrong.

Reply with one line and nothing else:

CANDIDATE: n`, field(p)), b.String()
}

// TruthJudge asks whether the solution is right.
func (DefaultPrompts) TruthJudge(p Problem, reference, solution string) (string, string) {
	var b strings.Builder
	b.WriteString("Problem:\n\n")
	b.WriteString(p.Text)
	if reference != "" {
		b.WriteString("\n\nA second, independent solution:\n\n")
		b.WriteString(reference)
	}
	b.WriteString("\n\nThe solution under review:\n\n")
	b.WriteString(solution)
	return fmt.Sprintf(`You are checking a solution to a %s problem before it goes into an archive.

Check all of the following, and fail the solution if any one of them fails:

1. The answer is correct.
2. The reasoning actually establishes the answer. An assertion is not a step.
3. Nothing is assumed that the problem does not give.
4. It is self contained. A reader with the problem and this text needs nothing else.
5. It says what the answer is, rather than leaving the reader to extract it.

Where a second solution is given it is evidence, not authority. If the two
disagree, decide which is right.

Write your reasoning, then finish with one line, exactly:

VERDICT: PASS

or

VERDICT: FAIL`, field(p)), b.String()
}

// AuditJudge tries to break the solution.
//
// It never sees the reference, and that is the whole reason there are two
// judges rather than one. The truth judge is shown a second opinion and asked
// which is right, which leaves it prone to accepting whatever two texts agree
// on. This one has nothing to agree with and is told to attack.
func (DefaultPrompts) AuditJudge(p Problem, solution string) (string, string) {
	extra := ""
	if p.Subject == corpus.Physics {
		extra = "\n6. Every quantity carries units where it should, and the final result is " +
			"dimensionally consistent with what was asked for."
	}
	return fmt.Sprintf(`You are trying to find something wrong with a solution to a %s problem. Assume
there is a fault and look for it. Read it as a hostile referee would.

Look for:

1. A step that does not follow from the one before it.
2. A case the argument silently excludes. Degenerate configurations, equality in
   an inequality, a divisor that could be zero, a root that could be negative.
3. An arithmetic or algebraic slip.
4. A claim of generality proved only for an example.
5. A conclusion restated as though it had been derived.%s

If you find nothing after genuinely looking, say so and pass it. Do not invent a
fault to seem thorough, and do not pass it to be agreeable.

Write what you found, then finish with one line, exactly:

VERDICT: PASS

or

VERDICT: FAIL`, field(p), extra),
		"Problem:\n\n" + p.Text + "\n\nThe solution under review:\n\n" + solution
}

// Correct repairs a solution the judges rejected.
func (DefaultPrompts) Correct(p Problem, solution string, judgements []Judgement) (string, string) {
	var b strings.Builder
	b.WriteString("Problem:\n\n")
	b.WriteString(p.Text)
	b.WriteString("\n\nThe solution:\n\n")
	b.WriteString(solution)
	for _, j := range judgements {
		if j.Passed() {
			continue
		}
		fmt.Fprintf(&b, "\n\nWhat the %s check found:\n\n%s", j.Kind, j.Text)
	}
	return fmt.Sprintf(`A solution to a %s problem was reviewed and rejected. Below is the solution and
what the reviewers found.

Fix it. If the fault is in the reasoning, redo the reasoning. If the answer
itself is wrong, solve the problem again rather than patching the text to reach
a different number, because a repaired derivation of a wrong answer is worse
than an honest failure.

Where you think a reviewer is mistaken, say so in the solution itself, with the
argument that shows it.

Reply with the corrected solution and nothing else. Do not describe what you
changed.

%s`, field(p), house), b.String()
}

// Grade compares the solution with the one the magazine printed.
//
// This is the only prompt in the package that is shown the published answer,
// and it runs after the solution is finished and recorded. Nothing it returns
// can reach back into the solving.
func (DefaultPrompts) Grade(p Problem, solution, published string) (string, string) {
	var b strings.Builder
	b.WriteString("Problem:\n\n")
	b.WriteString(p.Text)
	b.WriteString("\n\nThe solution to be marked:\n\n")
	b.WriteString(solution)
	b.WriteString("\n\nThe solution the magazine printed:\n\n")
	b.WriteString(published)
	return fmt.Sprintf(`You are marking a solution to a %s problem against the one Квант printed when it
published the answer, two to four issues after it set the problem.

Mark the answer, not the method. The printed solution is usually the shortest
one the editors could find and is often not the only one. A solution that
reaches the same answer by a different and valid route is correct. A solution
that follows the printed argument and arrives somewhere else is not.

The printed text came out of a scan and may have lost a formula or a figure. If
it is too damaged to mark against, say so and grade UNGRADED rather than guess.

Report, in this order:

- whether the answers agree
- if they differ, where the two texts part company
- whether the route taken is the printed one or a different one

Then finish with one line, exactly one of:

GRADE: CORRECT
GRADE: INCORRECT
GRADE: PARTIAL
GRADE: UNGRADED`, field(p)), b.String()
}

// verdictLine reads the VERDICT a judge wrote.
//
// The last one rather than the first, because a judge that quotes the
// instruction back before answering would otherwise be read as having answered
// in its preamble.
var verdictLine = regexp.MustCompile(`(?i)VERDICT\s*[:\-]?\s*(PASS|FAIL)`)

// Verdict reads a judgement.
//
// Anything unreadable is a failure rather than a pass. A judge whose reply
// cannot be parsed has not approved anything, and defaulting the other way
// would let every malformed response through as verified.
func Verdict(text string) string {
	m := verdictLine.FindAllStringSubmatch(text, -1)
	if len(m) == 0 {
		return Fail
	}
	return strings.ToUpper(m[len(m)-1][1])
}

// candidateLine reads the selector's choice.
var candidateLine = regexp.MustCompile(`(?i)CANDIDATE\s*[:\-#]?\s*(\d+)`)

// SelectedCandidate reads which candidate the selector chose, one based, or 0
// if it did not say. The caller treats 0 as an error rather than falling back
// to the first, because a selector that did not choose has endorsed nothing.
func SelectedCandidate(text string) int {
	m := candidateLine.FindAllStringSubmatch(text, -1)
	if len(m) == 0 {
		// A reply that is nothing but a number is a choice, and some models
		// answer that way however plainly the format is stated.
		if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
			return n
		}
		return 0
	}
	n, err := strconv.Atoi(m[len(m)-1][1])
	if err != nil {
		return 0
	}
	return n
}

// The markings Grade may return.
const (
	Correct   = "CORRECT"
	Incorrect = "INCORRECT"
	Partial   = "PARTIAL"
	// Ungraded is the honest outcome when the printed solution came out of the
	// scan too damaged to mark against. It is counted separately rather than
	// folded into the failures, because a page this corpus failed to read is
	// not a problem the solver got wrong.
	Ungraded = "UNGRADED"
)

var gradeLine = regexp.MustCompile(`(?i)GRADE\s*[:\-]?\s*(CORRECT|INCORRECT|PARTIAL|UNGRADED)`)

// ParseGrade reads a marking. An unreadable reply is UNGRADED, which keeps it
// out of both the numerator and the denominator of the score.
func ParseGrade(text string) string {
	m := gradeLine.FindAllStringSubmatch(text, -1)
	if len(m) == 0 {
		return Ungraded
	}
	return strings.ToUpper(m[len(m)-1][1])
}
