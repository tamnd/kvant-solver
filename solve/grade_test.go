package solve

import (
	"context"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

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
	var s Scorecard
	s.Add(Result{ID: "M1", Verdict: Pass}, Marking{ID: "M1", Grade: Correct})
	s.Add(Result{ID: "M2", Verdict: Pass}, Marking{ID: "M2", Grade: Incorrect})
	s.Add(Result{ID: "M3", Verdict: Fail}, Marking{ID: "M3", Grade: Correct})
	s.Add(Result{ID: "M4", Verdict: Pass}, Marking{ID: "M4", Grade: Ungraded})
	a := s.Agreement()
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
	report := s.Report()
	for _, want := range []string{"smoke", "ungraded", "could not be marked", "False pass rate"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report does not mention %q:\n%s", want, report)
		}
	}
}

// datedProblem is the same problem with the issue that set it known, which is
// the only thing that turns the anachronism question on.
func datedProblem() Problem {
	p := mathProblem()
	p.Year = 1974
	return p
}

// grader answers the marking with the reply given and everything else the way
// pass does, so that a test can say what came back from the grader and nothing
// else.
func grader(reply string) *Engine {
	return &Engine{Client: &fake{reply: func(stage string, n int) string {
		if stage == "grade" {
			return reply
		}
		return pass(stage, n)
	}}}
}

func TestAMarkingCanGoAgainstUsAndStillSayWeWereRight(t *testing.T) {
	// Квант printed corrections to its own answers. A run that treats the
	// printed one as ground truth by definition files our correct solutions as
	// misses and, worse, never shows anyone which page to go back to.
	e := grader("Ответы разошлись. В журнале потеряна двойка.\n\nGRADE: INCORRECT\nRIGHT: MARKED")
	m, err := e.Grade(context.Background(), datedProblem(), Result{Solution: solutionText}, "Ответ: 14.", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Grade != Incorrect {
		t.Fatalf("grade is %q, want INCORRECT: the adjudication must not move the mark", m.Grade)
	}
	if m.Right != Marked || !m.Overturns() {
		t.Fatalf("right is %q and overturns is %v", m.Right, m.Overturns())
	}
	var s Scorecard
	s.Add(Result{ID: m.ID}, m)
	if s.Overturned != 1 {
		t.Fatalf("overturned %d, want 1", s.Overturned)
	}
	if s.Correct != 0 || s.Incorrect != 1 || s.Score() != 0 {
		t.Fatalf("the score moved: %d correct, %d incorrect, score %v", s.Correct, s.Incorrect, s.Score())
	}
}

func TestAnAdjudicationThatCannotBeReadOverturnsNothing(t *testing.T) {
	// Defaulting the other way would report this run's own parse failures as
	// errata in the magazine, which is the one wrong answer here that would
	// send somebody to the archive to look for a mistake that is ours.
	if got := Adjudication("Both texts reach the same place."); got != Unclear {
		t.Fatalf("an unreadable adjudication read as %q, want UNCLEAR", got)
	}
	if got := Adjudication("RIGHT: one of MARKED, PRINTED, BOTH\n\nGRADE: CORRECT\nRIGHT: BOTH"); got != Both {
		t.Fatalf("read as %q, want the answer and not the instruction it quoted back", got)
	}
	if !(Marking{Grade: Correct, Right: Marked}).Overturns() == false {
		t.Fatal("a correct solution cannot overturn the answer it agrees with")
	}
}

func TestAMethodTheMagazineDidNotHaveIsRecordedAndNotMarkedDown(t *testing.T) {
	// The flag is not a fault. A 1974 problem solved with a tool from 1990 is
	// still solved, and the reason to record it is that a scorecard which cannot
	// tell the two apart is claiming the solver did what the readers did.
	e := grader("Ответ верный.\n\nGRADE: CORRECT\nRIGHT: BOTH\nANACHRONISM: базисы Грёбнера")
	m, err := e.Grade(context.Background(), datedProblem(), Result{Solution: solutionText}, "Ответ: делится.", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if m.Grade != Correct || m.Overturns() {
		t.Fatalf("grade is %q and overturns is %v", m.Grade, m.Overturns())
	}
	if m.Anachronism != "базисы Грёбнера" {
		t.Fatalf("anachronism is %q", m.Anachronism)
	}
	var s Scorecard
	s.Add(Result{ID: m.ID}, m)
	if s.Anachronistic != 1 || s.Correct != 1 || s.Score() != 1 {
		t.Fatalf("%d anachronistic, %d correct, score %v", s.Anachronistic, s.Correct, s.Score())
	}
}

func TestTheSeveralWaysAGraderSaysThereWasNoAnachronism(t *testing.T) {
	// Inventing one out of a malformed reply is worse than missing one, because
	// a name in this column is something a person will go and check.
	for _, none := range []string{
		"GRADE: CORRECT\nANACHRONISM: NONE",
		"GRADE: CORRECT\n**ANACHRONISM:** none.",
		"GRADE: CORRECT\nANACHRONISM: N/A",
		"GRADE: CORRECT",
		"The solution is fine and uses nothing later than the issue.",
	} {
		if got := Anachronism(none); got != "" {
			t.Errorf("%q was read as the method %q", none, got)
		}
	}
	if got := Anachronism("GRADE: CORRECT\n**ANACHRONISM**: the Cauchy-Schwarz inequality"); got != "the Cauchy-Schwarz inequality" {
		t.Fatalf("read as %q", got)
	}
}

func TestTheGraderIsNotAskedAboutAYearItWasNotGiven(t *testing.T) {
	// Most of the problem corpus knows what issue set it, but not all of it
	// does, and a grader asked to date a method against nothing will answer
	// anyway. That answer lands in a column nobody can check.
	undated, _ := DefaultPrompts{}.Grade(mathProblem(), solutionText, "Ответ: делится.")
	if strings.Contains(undated, "ANACHRONISM") {
		t.Fatalf("the grader was asked to date a problem with no year:\n%s", undated)
	}
	dated, _ := DefaultPrompts{}.Grade(datedProblem(), solutionText, "Ответ: делится.")
	for _, want := range []string{"ANACHRONISM", "1974", "RIGHT:"} {
		if !strings.Contains(dated, want) {
			t.Fatalf("the grade prompt does not mention %q:\n%s", want, dated)
		}
	}
}

func TestTheYearIsShownToTheGraderAndToNothingElse(t *testing.T) {
	// Same reasoning as the published solution: the year the problem was set is
	// a hint about the intended method, and a solver that knows the answer was
	// meant to come out of 1974 mathematics is being steered.
	f := &fake{reply: pass}
	e := &Engine{Client: f}
	opts, _ := Slow.Apply(Options{})
	if _, err := e.Solve(context.Background(), datedProblem(), opts); err != nil {
		t.Fatal(err)
	}
	if n := f.sent("1974"); n != 0 {
		t.Fatalf("the year reached %d of the solving calls", n)
	}
}

func TestTheReportNamesTheMarkingsSomebodyHasToRead(t *testing.T) {
	// Both counts are useless as bare numbers. An overturned marking is a page
	// to go and check and an anachronism is a solution to go and read, and
	// neither is findable in a scorecard that only says how many there were.
	s := Scorecard{Set: "smoke"}
	s.Add(Result{ID: "M311", Verdict: Pass}, Marking{ID: "M311", Grade: Incorrect, Right: Marked})
	s.Add(Result{ID: "M312", Verdict: Pass}, Marking{ID: "M312", Grade: Correct, Right: Both,
		Anachronism: "базисы Грёбнера"})
	report := s.Report()
	for _, want := range []string{"M311", "marked incorrect", "M312: базисы Грёбнера"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report does not mention %q:\n%s", want, report)
		}
	}
}

// row is a solved and marked problem, short enough to build a set out of in one
// line. The year and the subject come off the result and the grade off the
// marking, which is the split the scorecard has to put back together.
func row(id string, year int, subject corpus.Subject, solver, grade string, verified bool) (Result, Marking) {
	verdict := Fail
	if verified {
		verdict = Pass
	}
	return Result{ID: id, Year: year, Subject: subject, Model: solver, Verdict: verdict},
		Marking{ID: id, Grade: grade}
}

func TestTheScoreIsCutByDecade(t *testing.T) {
	// Fifty years of one magazine is fifty years of changing syllabus, and a
	// single percentage over the lot cannot say whether a model does better on
	// 1974 than on 2004.
	var s Scorecard
	s.Add(row("M1", 1974, corpus.Math, "a", Correct, true))
	s.Add(row("M2", 1978, corpus.Math, "a", Incorrect, true))
	s.Add(row("M3", 2004, corpus.Math, "a", Correct, true))

	decades := s.ByDecade()
	if len(decades) != 2 {
		t.Fatalf("cut into %d decades, want the 1970s and the 2000s: %+v", len(decades), decades)
	}
	if decades[0].Name != "1970s" || decades[0].Attempted != 2 || decades[0].Rate() != 0.5 {
		t.Fatalf("the 1970s came out %+v", decades[0])
	}
	if decades[1].Name != "2000s" || decades[1].Rate() != 1 {
		t.Fatalf("the 2000s came out %+v", decades[1])
	}
}

func TestAProblemWithNoYearIsKeptAndSortedLast(t *testing.T) {
	// Most of the archive knows what issue set it. Dropping the ones that do not
	// would make the rows add up to fewer problems than the run attempted, with
	// nothing on the page to say where the rest went.
	var s Scorecard
	s.Add(row("M1", 1974, corpus.Math, "a", Correct, true))
	s.Add(row("M2", 0, corpus.Math, "a", Incorrect, true))

	decades := s.ByDecade()
	if len(decades) != 2 || decades[1].Name != "unknown" {
		t.Fatalf("the undated problem is not last and named: %+v", decades)
	}
	var attempted int
	for _, d := range decades {
		attempted += d.Attempted
	}
	if attempted != s.Attempted {
		t.Fatalf("the decades account for %d problems, the run attempted %d", attempted, s.Attempted)
	}
}

func TestTheFalsePassRateIsCutTheSameWay(t *testing.T) {
	// The whole point of cutting it is to find where the judges are weakest. A
	// run whose judges pass everything in physics and nothing else is a run with
	// a physics prompt to fix, and the single number over the set hides it.
	var s Scorecard
	s.Add(row("F1", 1974, corpus.Physics, "a", Incorrect, true))
	s.Add(row("F2", 1974, corpus.Physics, "a", Correct, true))
	s.Add(row("M1", 1974, corpus.Math, "a", Correct, true))
	// Failed the judges and wrong anyway, so it is not a false pass of anything.
	s.Add(row("M2", 1974, corpus.Math, "a", Incorrect, false))

	byName := map[string]Stratum{}
	for _, t := range s.BySubject() {
		byName[t.Name] = t
	}
	if got := byName[string(corpus.Physics)]; got.FalsePassRate() != 0.5 {
		t.Fatalf("physics false pass rate is %v, want 0.5: %+v", got.FalsePassRate(), got)
	}
	if got := byName[string(corpus.Math)]; got.FalsePassRate() != 0 {
		t.Fatalf("mathematics false pass rate is %v, want 0: %+v", got.FalsePassRate(), got)
	}
}

func TestTheModelThatSolvedIsNotTheModelThatMarked(t *testing.T) {
	// Both are called Model, one on the result and one on the marking, and
	// putting the grader's name in the by model table would report every run as
	// one model no matter how many wrote the solutions.
	var s Scorecard
	res, m := row("M1", 1974, corpus.Math, "solver-one", Correct, true)
	m.Model = "grader"
	s.Add(res, m)
	res, m = row("M2", 1974, corpus.Math, "solver-two", Incorrect, true)
	m.Model = "grader"
	s.Add(res, m)

	models := s.ByModel()
	if len(models) != 2 || models[0].Name != "solver-one" || models[1].Name != "solver-two" {
		t.Fatalf("cut by the wrong model: %+v", models)
	}
}

func TestAnAxisWithOneValueGetsNoTable(t *testing.T) {
	// A table of one row is the total printed twice under a heading that
	// suggests a comparison nobody can make from it.
	var s Scorecard
	s.Set = "smoke"
	s.Add(row("M1", 1974, corpus.Math, "a", Correct, true))
	s.Add(row("M2", 1984, corpus.Math, "a", Incorrect, true))
	report := s.Report()
	if !strings.Contains(report, "By decade") {
		t.Fatalf("the decade table is missing:\n%s", report)
	}
	for _, unwanted := range []string{"By subject", "By model"} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("%q was printed for an axis with one value:\n%s", unwanted, report)
		}
	}
}

func TestASetThatIsAllOneThingGetsNoBreakdownAtAll(t *testing.T) {
	var s Scorecard
	s.Set = "smoke"
	s.Add(row("M1", 1974, corpus.Math, "a", Correct, true))
	s.Add(row("M2", 1974, corpus.Math, "a", Incorrect, true))
	if report := s.Report(); strings.Contains(report, "How the score is spread") {
		t.Fatalf("a breakdown was printed for a set with nothing to break down:\n%s", report)
	}
}

func TestAScorecardOffDiskStillProducesItsBreakdown(t *testing.T) {
	// The reason the subject, the year and the solver are on the line at all.
	// Working them out at report time from the results would mean the breakdown
	// only exists in the process that ran the set, and reports/scorecard.md is
	// read long after that process is gone.
	var s Scorecard
	s.Set = "smoke"
	s.Add(row("M1", 1974, corpus.Math, "a", Correct, true))
	s.Add(row("F1", 2004, corpus.Physics, "b", Incorrect, true))

	text, err := yaml.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Scorecard
	if err := yaml.Unmarshal(text, &back); err != nil {
		t.Fatal(err)
	}
	if got, want := back.Report(), s.Report(); got != want {
		t.Fatalf("the scorecard read back reports differently:\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(back.Report(), "1970s") {
		t.Fatalf("the decades did not survive the round trip:\n%s", text)
	}
}
