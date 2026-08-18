package solve

import (
	"context"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

func TestAPhysicsAnswerThatDroppedItsUnitsIsCaught(t *testing.T) {
	// The derivation is in metres and seconds throughout and the answer is a
	// bare number, so the units went missing between the last line of algebra
	// and the result. A prose judge reads past this; it is exactly the kind of
	// thing a cheap mechanical check is good at.
	got := Dimensions("Скорость равна $v = s/t$, где $s = 100$ м и $t = 20$ с.\n\nОтвет: 5.")
	if len(got) != 1 {
		t.Fatalf("expected one complaint, got %v", got)
	}
	if !strings.Contains(got[0], "bare number") {
		t.Fatalf("complaint reads %q", got[0])
	}
}

func TestAPhysicsAnswerWithItsUnitsIsLeftAlone(t *testing.T) {
	if got := Dimensions("Путь $s = 100$ м за $t = 20$ с.\n\nОтвет: 5 м/с."); got != nil {
		t.Fatalf("a correct answer was complained about: %v", got)
	}
}

func TestADimensionlessAnswerIsNotAskedForUnits(t *testing.T) {
	// Plenty of the physics in this magazine asks for a ratio, an angle or a
	// proof. Demanding units of those would send the correction loop after
	// solutions with nothing wrong with them, and would invite a model to bolt
	// units onto a dimensionless answer to satisfy the checker.
	for _, solution := range []string{
		"Отношение масс не зависит от выбора системы отсчёта.\n\nОтвет: 2.",
		"Груз массой $m = 2$ кг поднимают на $h = 3$ м.\n\nОтвет: КПД равен 80 %.",
		"Угол падения равен углу отражения при $h = 3$ м.\n\nОтвет: 30°.",
		"Скорость вырастет в $n = 4$ раза при пути $s = 10$ м.\n\nОтвет: в 4 раза.",
	} {
		if got := Dimensions(solution); got != nil {
			t.Fatalf("complained about %q: %v", solution, got)
		}
	}
}

func TestAProofWithNoUnitsAnywhereIsNotChecked(t *testing.T) {
	if got := Dimensions("Докажем неравенство методом индукции. При $n = 1$ оно очевидно."); got != nil {
		t.Fatalf("a proof was checked for units: %v", got)
	}
}

func TestARussianWordIsNotMistakenForAUnit(t *testing.T) {
	// Several of the unit abbreviations are ordinary Russian words. «с» is the
	// preposition with, «г» abbreviates год and «А» labels a point in a
	// diagram, so a rule that does not require a digit in front reads units
	// into most sentences in the magazine.
	if usesUnits("Тело движется с постоянной скоростью из точки А в точку Б.") {
		t.Fatal("read a unit out of ordinary Russian prose")
	}
	if !usesUnits("Тело прошло 100 м за 20 с.") {
		t.Fatal("failed to read the units off a sentence that has them")
	}
	// The boundary has to be a letter class rather than \b, which is ASCII only
	// in this engine and finds no boundary at all between two Cyrillic letters.
	if usesUnits("Прошло 100 метров, что немало.") {
		t.Fatal("matched the м inside метров")
	}
}

func TestAPhysicsSolutionIsJudgedOnItsUnitsAsWellAsItsProse(t *testing.T) {
	// Both judges pass it and it is still not verified, because the dimensional
	// check is not a model and cannot be talked round.
	f := &fake{reply: func(stage string, n int) string {
		if stage == "candidate" || stage == "reference" || stage == "correct" {
			return "Скорость равна $v = s/t$, где $s = 100$ м и $t = 20$ с.\n\nОтвет: 5."
		}
		return pass(stage, n)
	}}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	res, err := e.Solve(context.Background(),
		Problem{ID: "F323", Subject: corpus.Physics, Text: "Найдите скорость."}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified() {
		t.Fatal("a physics answer with no units on it was verified")
	}
	var found bool
	for _, j := range res.Judgements {
		if j.Kind == "dimension" && !j.Passed() {
			found = true
		}
	}
	if !found {
		t.Fatalf("no dimensional finding in %+v", res.Judgements)
	}
}

func TestAMathematicsSolutionIsNotGivenADimensionalCheck(t *testing.T) {
	f := &fake{reply: pass}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	res, err := e.Solve(context.Background(), mathProblem(), opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range res.Judgements {
		if j.Kind == "dimension" {
			t.Fatal("a mathematics problem was checked for units")
		}
	}
}

func TestAProblemTheMagazineNeverAnsweredIsNotMarkedWrong(t *testing.T) {
	// Marking against an empty string would score every unanswered problem as a
	// miss, and there are hundreds of them: the magazine set problems it never
	// got round to printing solutions for.
	f := &fake{reply: pass}
	e := &Engine{Client: f}
	m, err := e.Grade(context.Background(), mathProblem(), Result{Solution: solutionText}, "", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Grade != Ungraded {
		t.Fatalf("grade is %q, want UNGRADED", m.Grade)
	}
	if len(f.calls) != 0 {
		t.Fatal("spent a call marking against a solution that was never printed")
	}
}

func TestAGradingThatCannotBeReadIsUngraded(t *testing.T) {
	if got := ParseGrade("It looks broadly right to me."); got != Ungraded {
		t.Fatalf("an unreadable marking read as %q, want UNGRADED", got)
	}
	if got := ParseGrade("GRADE: INCORRECT"); got != Incorrect {
		t.Fatalf("read as %q, want INCORRECT", got)
	}
	if got := ParseGrade("GRADE: CORRECT"); got != Correct {
		t.Fatalf("read as %q, want CORRECT", got)
	}
}

func TestUngradedProblemsAreLeftOutOfTheScore(t *testing.T) {
	// A problem whose printed solution came off the scan too damaged to read is
	// a failure of this corpus, not of the solver. Counting it as a miss would
	// make every improvement to the reading lane look like an improvement to
	// the solver, which is the opposite of what the scorecard is for.
	var s Scorecard
	s.Add(Result{ID: "M1", Verdict: Pass}, Marking{ID: "M1", Grade: Correct})
	s.Add(Result{ID: "M2", Verdict: Pass}, Marking{ID: "M2", Grade: Incorrect})
	s.Add(Result{ID: "M3", Verdict: Pass}, Marking{ID: "M3", Grade: Ungraded})
	if s.Attempted != 3 {
		t.Fatalf("attempted %d, want 3", s.Attempted)
	}
	if s.Graded() != 2 {
		t.Fatalf("graded %d, want 2", s.Graded())
	}
	if s.Score() != 0.5 {
		t.Fatalf("score is %v, want 0.5 over the two that could be marked", s.Score())
	}
}

func TestPartialCreditCountsAsAMiss(t *testing.T) {
	var s Scorecard
	s.Add(Result{ID: "M1"}, Marking{ID: "M1", Grade: Partial})
	if s.Score() != 0 {
		t.Fatalf("score is %v, the magazine printed one answer and this was not it", s.Score())
	}
	if s.Graded() != 1 {
		t.Fatalf("graded %d, want 1", s.Graded())
	}
}

func TestAFalsePassIsCounted(t *testing.T) {
	// This is the only number in the run that measures the judges, because the
	// judges are the only check the solver has and they cannot audit
	// themselves.
	results := []Result{
		{ID: "M1", Verdict: Pass},
		{ID: "M2", Verdict: Pass},
		{ID: "M3", Verdict: Fail},
		{ID: "M4", Verdict: Pass},
	}
	markings := []Marking{
		{ID: "M1", Grade: Correct},
		{ID: "M2", Grade: Incorrect},
		{ID: "M3", Grade: Correct},
		{ID: "M4", Grade: Ungraded},
	}
	a := Compare(results, markings)
	if a.VerifiedAndCorrect != 1 || a.VerifiedAndWrong != 1 || a.UnverifiedAndCorrect != 1 {
		t.Fatalf("agreement came out %+v", a)
	}
	if a.FalsePassRate() != 0.5 {
		t.Fatalf("false pass rate is %v, want 0.5", a.FalsePassRate())
	}
	// M4 was never marked, so it belongs in none of the four boxes.
	total := a.VerifiedAndCorrect + a.VerifiedAndWrong + a.UnverifiedAndCorrect + a.UnverifiedAndWrong
	if total != 3 {
		t.Fatalf("compared %d problems, want the 3 that were marked", total)
	}
}

func TestTheReportSaysWhatWasLeftOut(t *testing.T) {
	// A scorecard that prints a percentage and not the size of the sample it
	// dropped is a scorecard that reads better than the run deserves.
	s := Scorecard{Set: "smoke"}
	s.Add(Result{ID: "M1", Verdict: Pass}, Marking{ID: "M1", Grade: Correct})
	s.Add(Result{ID: "M2", Verdict: Pass}, Marking{ID: "M2", Grade: Ungraded})
	report := s.Report(Compare(nil, nil))
	for _, want := range []string{"smoke", "ungraded", "could not be marked", "False pass rate"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report does not mention %q:\n%s", want, report)
		}
	}
}
