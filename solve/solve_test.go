package solve

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
)

// fake answers without a server and keeps every request, which is what makes
// the leak test possible: the question is not what the solver did with the
// published solution, it is whether the published solution was ever put on the
// wire at all.
type fake struct {
	calls []api.Request
	reply func(stage string, n int) string
	err   error
}

func (f *fake) Complete(_ context.Context, r api.Request) (api.Response, error) {
	f.calls = append(f.calls, r)
	if f.err != nil {
		return api.Response{}, f.err
	}
	return api.Response{Model: "test", Text: f.reply(stageOf(r), len(f.calls))}, nil
}

// stageOf works out which call this is from the wording of the instruction,
// which is the same thing a reader of the transcript would do.
func stageOf(r api.Request) string {
	switch {
	case strings.Contains(r.Instructions, "choosing which"):
		return "select"
	case strings.Contains(r.Instructions, "You are checking a solution"):
		return "truth"
	case strings.Contains(r.Instructions, "trying to find something wrong"):
		return "audit"
	case strings.Contains(r.Instructions, "reviewed and rejected"):
		return "correct"
	case strings.Contains(r.Instructions, "You are marking a solution"):
		return "grade"
	case strings.Contains(r.Instructions, "Solve it the most direct"),
		strings.Contains(r.Instructions, "prefer a route other"),
		strings.Contains(r.Instructions, "test the answer against"),
		strings.Contains(r.Instructions, "from the constraints backwards"),
		strings.Contains(r.Instructions, "least sure of"):
		return "candidate"
	default:
		return "reference"
	}
}

func (f *fake) sent(needle string) int {
	n := 0
	for _, c := range f.calls {
		if strings.Contains(c.Instructions, needle) || strings.Contains(c.Input, needle) {
			n++
		}
	}
	return n
}

func mathProblem() Problem {
	return Problem{ID: "M311", Subject: corpus.Math, Text: "Докажите, что сумма делится на три."}
}

const solutionText = "Разложим сумму на множители и получим требуемое.\n\nОтвет: делится."

// pass answers every stage with something acceptable.
func pass(stage string, _ int) string {
	switch stage {
	case "select":
		return "CANDIDATE: 1"
	case "truth", "audit":
		return "Checked every step.\n\nVERDICT: PASS"
	case "grade":
		return "The answers agree.\n\nGRADE: CORRECT"
	default:
		return solutionText
	}
}

func TestThePublishedSolutionNeverReachesTheSolver(t *testing.T) {
	// This is the guarantee the whole package exists to provide, and it is the
	// one that cannot be checked by reading the code once, because the failure
	// is invisible: a solver shown the answer produces a correct solution, both
	// judges pass it, and the scorecard reports a high score that measures
	// paraphrase. So it is asserted on the wire.
	const marker = "ТАЙНЫЙМАРКЕР"
	published := "Ответ получается сложением. " + marker

	f := &fake{reply: pass}
	e := &Engine{Client: f}
	opts, err := Slow.Apply(Options{})
	if err != nil {
		t.Fatal(err)
	}

	res, err := e.Solve(context.Background(), mathProblem(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if n := f.sent(marker); n != 0 {
		t.Fatalf("the printed solution went out on %d of the %d solving calls", n, len(f.calls))
	}
	solving := len(f.calls)

	if _, err := e.Grade(context.Background(), mathProblem(), res, published, opts); err != nil {
		t.Fatal(err)
	}
	if n := f.sent(marker); n != 1 {
		t.Fatalf("the printed solution appears in %d calls, it should appear only in the grading call", n)
	}
	if stageOf(f.calls[solving]) != "grade" {
		t.Fatalf("the first call that saw the answer was a %q call", stageOf(f.calls[solving]))
	}
}

func TestSolveHasNowhereToPutTheAnswer(t *testing.T) {
	// The barrier above is not enforced by care at the call sites, it is
	// enforced by Problem having no field for a published solution. If somebody
	// ever adds one this test is what fails, and the comment is what explains
	// why it should not be added.
	// Year is on the list because the grader needs it and there is one struct.
	// It is the only field here no solving prompt is shown, and the test that
	// holds that line is the one below this file's fake grader.
	want := map[string]bool{"ID": true, "Subject": true, "Text": true, "Year": true}
	got := reflect.TypeOf(Problem{})
	if got.NumField() != len(want) {
		t.Fatalf("Problem has %d fields, want the %d the solver is allowed to see",
			got.NumField(), len(want))
	}
	for i := range got.NumField() {
		if name := got.Field(i).Name; !want[name] {
			t.Fatalf("Problem has a field %q that the solver is not allowed to see. "+
				"If that is the published solution, it belongs on Grade and nowhere else", name)
		}
	}
}

func TestBothJudgesHaveToPass(t *testing.T) {
	for _, tc := range []struct{ name, failing string }{
		{"the truth judge rejects it", "truth"},
		{"the audit judge rejects it", "audit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{reply: func(stage string, n int) string {
				if stage == tc.failing {
					return "This does not follow.\n\nVERDICT: FAIL"
				}
				return pass(stage, n)
			}}
			e := &Engine{Client: f}
			opts, _ := Slow.Apply(Options{})
			res, err := e.Solve(context.Background(), mathProblem(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if res.Verified() {
				t.Fatalf("the %s judge failed it and the result says verified", tc.failing)
			}
		})
	}
}

func TestAJudgementThatCannotBeReadIsNotAPass(t *testing.T) {
	// Defaulting the other way would let every malformed reply through as
	// verified, which is the failure mode that looks like success.
	f := &fake{reply: func(stage string, n int) string {
		if stage == "audit" {
			return "I had a look and it seems fine to me."
		}
		return pass(stage, n)
	}}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	res, err := e.Solve(context.Background(), mathProblem(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified() {
		t.Fatal("a judgement with no verdict in it was counted as approval")
	}
}

func TestASelectorThatChoosesNothingIsAnError(t *testing.T) {
	// Falling back to the first candidate would be worse than failing: the run
	// would carry on and the result would claim a selection nobody made.
	f := &fake{reply: func(stage string, n int) string {
		if stage == "select" {
			return "They are all much of a muchness."
		}
		return pass(stage, n)
	}}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	if _, err := e.Solve(context.Background(), mathProblem(), opts); err == nil {
		t.Fatal("a selector that named no candidate should stop the run")
	}
}

func TestTheCorrectionLoopIsBounded(t *testing.T) {
	f := &fake{reply: func(stage string, n int) string {
		switch stage {
		case "truth", "audit":
			return "Still wrong.\n\nVERDICT: FAIL"
		default:
			return pass(stage, n)
		}
	}}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{MaxCorrections: 2})
	res, err := e.Solve(context.Background(), mathProblem(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != Fail {
		t.Fatalf("verdict is %q, want FAIL", res.Verdict)
	}
	corrections := 0
	for _, c := range f.calls {
		if stageOf(c) == "correct" {
			corrections++
		}
	}
	if corrections != 2 {
		t.Fatalf("the loop ran %d corrections, want the 2 it was allowed", corrections)
	}
}

func TestFastModeDoesNotClaimToHaveCheckedAnything(t *testing.T) {
	f := &fake{reply: pass}
	e := &Engine{Client: f}
	opts, _ := Fast.Apply(Options{})
	res, err := e.Solve(context.Background(), mathProblem(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified() {
		t.Fatal("fast mode ran no judges, so it cannot report a solution as verified")
	}
	if res.Verdict != Skip {
		t.Fatalf("verdict is %q, want SKIP", res.Verdict)
	}
	if len(f.calls) != 1 {
		t.Fatalf("fast mode made %d calls, want 1", len(f.calls))
	}
}

func TestARefusalIsNotFiledAsASolution(t *testing.T) {
	f := &fake{reply: func(stage string, n int) string {
		if stage == "candidate" {
			return "I'm sorry, but I can't help with that."
		}
		return pass(stage, n)
	}}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	if _, err := e.Solve(context.Background(), mathProblem(), opts); err == nil {
		t.Fatal("a refusal should stop the run rather than become the solution")
	}
}

func TestAJudgeWritingEnglishIsNotMistakenForARefusal(t *testing.T) {
	// answerguard reads a line of English with no Cyrillic in it as a refusal,
	// which is right for a page of a Russian magazine and wrong for a judge.
	// Running the guard over a judgement would turn every careful judge into a
	// transport error, and the run would fail on its most useful replies.
	f := &fake{reply: func(stage string, n int) string {
		if stage == "audit" {
			return "I cannot verify the third step from what is written here.\n\nVERDICT: FAIL"
		}
		return pass(stage, n)
	}}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	res, err := e.Solve(context.Background(), mathProblem(), opts)
	if err != nil {
		t.Fatalf("the judge's English was treated as a transport failure: %v", err)
	}
	if res.Verified() {
		t.Fatal("the audit judge failed it")
	}
}

func TestATransportFailureIsReportedAndNotSwallowed(t *testing.T) {
	f := &fake{reply: pass, err: errors.New("the route is down")}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	if _, err := e.Solve(context.Background(), mathProblem(), opts); err == nil {
		t.Fatal("a dead route should be an error")
	}
}

func TestAnEmptyStatementIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	f := &fake{reply: pass}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	if _, err := e.Solve(context.Background(), Problem{ID: "M311"}, opts); err == nil {
		t.Fatal("an empty statement should be refused")
	}
	if len(f.calls) != 0 {
		t.Fatalf("made %d calls before noticing there was nothing to solve", len(f.calls))
	}
}

func TestMoreCandidatesThanTheLimitIsRefused(t *testing.T) {
	if _, err := Slow.Apply(Options{Candidates: 50}); err == nil {
		t.Fatal("50 candidates should be refused rather than run")
	}
	if _, err := Mode("thorough").Apply(Options{}); err == nil {
		t.Fatal("an unknown mode should be refused")
	}
}

func TestTheVerdictIsReadFromTheLastLineNotTheFirst(t *testing.T) {
	// A judge that quotes the format back before answering would otherwise be
	// read as having answered in its preamble.
	text := "You asked me to finish with VERDICT: PASS or VERDICT: FAIL.\n" +
		"The third step does not follow.\n\nVERDICT: FAIL"
	if got := Verdict(text); got != Fail {
		t.Fatalf("verdict read as %q, want FAIL", got)
	}
	if got := Verdict("no verdict here at all"); got != Fail {
		t.Fatalf("an unreadable judgement read as %q, want FAIL", got)
	}
	if got := Verdict("VERDICT: PASS"); got != Pass {
		t.Fatalf("verdict read as %q, want PASS", got)
	}
}

func TestTheSelectorsChoiceIsRead(t *testing.T) {
	for text, want := range map[string]int{
		"CANDIDATE: 2":      2,
		"candidate 3":       3,
		"CANDIDATE:1":       1,
		"2":                 2,
		"they are all fine": 0,
		"Reply with CANDIDATE: n.\n\nCANDIDATE: 3": 3,
	} {
		if got := SelectedCandidate(text); got != want {
			t.Fatalf("SelectedCandidate(%q) = %d, want %d", text, got, want)
		}
	}
}
