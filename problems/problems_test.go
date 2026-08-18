package problems

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

func TestASolutionArticleIsNotFiledAsAStatement(t *testing.T) {
	// «Решения задач» contains «задач», so a naive test for the posed heading
	// matches it and files every published solution in the archive as a
	// statement. That would leave the whole corpus with no ground truth at all,
	// which is the one thing M8 exists to produce.
	kind, ok := KindOf("Решения задач М273—М279; Ф285—Ф290")
	if !ok || kind != Solved {
		t.Fatalf("KindOf(solutions) = %q, %v, want solved", kind, ok)
	}
	kind, ok = KindOf("Задачи М311—М315; Ф323—Ф327")
	if !ok || kind != Posed {
		t.Fatalf("KindOf(problems) = %q, %v, want posed", kind, ok)
	}
	if _, ok := KindOf("Использование неравенства Коши при решении задач"); ok {
		t.Fatal("an ordinary article about solving problems is not the Задачник")
	}
}

func TestAHeadingPromisesEveryNumberInItsSpan(t *testing.T) {
	got := Promised("Задачи М311—М315; Ф323—Ф327")
	want := []corpus.ProblemID{
		{Subject: corpus.Math, Number: 311},
		{Subject: corpus.Math, Number: 312},
		{Subject: corpus.Math, Number: 313},
		{Subject: corpus.Math, Number: 314},
		{Subject: corpus.Math, Number: 315},
		{Subject: corpus.Physics, Number: 323},
		{Subject: corpus.Physics, Number: 324},
		{Subject: corpus.Physics, Number: 325},
		{Subject: corpus.Physics, Number: 326},
		{Subject: corpus.Physics, Number: 327},
	}
	if len(got) != len(want) {
		t.Fatalf("promised %d numbers, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("promised[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestARunawaySpanIsNotAPromise(t *testing.T) {
	// A misread digit turns М311—М315 into М311—М915, and taking that at face
	// value would enter six hundred problems nobody printed into the gap list
	// and bury the real holes.
	got := Promised("Задачи М311—М915")
	if len(got) != 1 || got[0].Number != 311 {
		t.Fatalf("a 600 wide span should collapse to its first number, got %v", got)
	}
}

func TestBothAlphabetsAreTheSameProblem(t *testing.T) {
	// The magazine prints Cyrillic М. The reading lane sometimes emits Latin M,
	// which is the same glyph. If these split into two problems the archive
	// gets two half populated entries where it should have one whole one.
	cyrillic := Split("М311. Условие первой задачи.", Posed)
	latin := Split("M311. Условие первой задачи.", Posed)
	if len(cyrillic) != 1 || len(latin) != 1 {
		t.Fatalf("expected one problem from each alphabet, got %d and %d", len(cyrillic), len(latin))
	}
	if cyrillic[0].ID != latin[0].ID {
		t.Fatalf("М311 parsed as %v and M311 as %v, they are the same problem",
			cyrillic[0].ID, latin[0].ID)
	}
	if cyrillic[0].ID.String() != "M311" {
		t.Fatalf("canonical form is %q, want M311", cyrillic[0].ID.String())
	}
}

func TestTheFullStopMayFallOutsideTheEmphasis(t *testing.T) {
	// The magazine sets the mathematics as **М301.** and the physics on the
	// facing page as **Ф313**. Reading only the first form loses every physics
	// problem in the archive, and the run still looks healthy because the
	// mathematics comes through.
	items := Split("**М301.** Условие по математике.\n\n**Ф313**. Условие по физике.\n", Posed)
	if len(items) != 2 {
		t.Fatalf("split gave %d problems, want 2: %+v", len(items), items)
	}
	if items[0].ID.String() != "M301" || items[1].ID.String() != "F313" {
		t.Fatalf("got %s and %s, want M301 and F313", items[0].ID, items[1].ID)
	}
	if items[1].Text != "Условие по физике." {
		t.Fatalf("physics text is %q", items[1].Text)
	}
}

func TestAProblemRunsUntilTheNextNumber(t *testing.T) {
	body := "⟦folio 46⟧\n\nЗадачник «Кванта»\n\n" +
		"М311. Первое условие.\nВторая строка первого условия.\n\n" +
		"М312. Второе условие.\n\n" +
		"Ф323. Условие по физике.\n"
	items := Split(body, Posed)
	if len(items) != 3 {
		t.Fatalf("split gave %d problems, want 3", len(items))
	}
	if items[0].ID.Number != 311 || items[1].ID.Number != 312 {
		t.Fatalf("numbers came out as %v and %v", items[0].ID, items[1].ID)
	}
	if items[2].ID.Subject != corpus.Physics {
		t.Fatalf("Ф323 filed under %q, want physics", items[2].ID.Subject)
	}
	if want := "Первое условие.\nВторая строка первого условия."; items[0].Text != want {
		t.Fatalf("first problem text is %q, want %q", items[0].Text, want)
	}
	// The rubric heading above the first number belongs to no problem.
	if strings.Contains(items[0].Text, "Задачник") {
		t.Fatalf("the heading leaked into the first problem: %q", items[0].Text)
	}
}

func TestTheTwoHalvesArePairedAcrossIssues(t *testing.T) {
	articles := []Article{
		{
			Path:  "content/ru/1975/03/articles/12_zadachi.md",
			Front: corpus.ArticleFront{Issue: "kvant_1975_3", Year: 1975, Number: "3", Title: "Задачи М311—М312", PageLabels: "46-47"},
			Body:  "М311. Условие первой.\n\nМ312. Условие второй.\n",
		},
		{
			Path:  "content/ru/1975/07/articles/09_resheniya.md",
			Front: corpus.ArticleFront{Issue: "kvant_1975_7", Year: 1975, Number: "7", Title: "Решения задач М311—М312", PageLabels: "50-51"},
			Body:  "М311. Ответ на первую.\n\nМ312. Ответ на вторую.\n",
		},
	}
	res := Build(articles)
	if len(res.Manifest.Entries) != 2 {
		t.Fatalf("built %d entries, want 2", len(res.Manifest.Entries))
	}
	first := res.Manifest.Entries[0]
	if first.Posed == nil || first.Posed.Issue != "kvant_1975_3" {
		t.Fatalf("M311 posed in %v, want kvant_1975_3", first.Posed)
	}
	if first.Solved == nil || first.Solved.Issue != "kvant_1975_7" {
		t.Fatalf("M311 solved in %v, want kvant_1975_7", first.Solved)
	}
	if !first.Solvable() {
		t.Fatal("M311 has a published solution and should say so")
	}
	if len(res.Gaps) != 0 {
		t.Fatalf("a complete pair should leave no gaps, got %v", res.Gaps)
	}
}

func TestTheStatementAndTheAnswerAreKeptApart(t *testing.T) {
	// M9 hands the solver a statement and must be able to withhold the printed
	// answer. If the two are concatenated in one field that guarantee becomes a
	// matter of cutting the string in the right place.
	articles := []Article{
		{
			Front: corpus.ArticleFront{Issue: "kvant_1975_3", Year: 1975, Number: "3", Title: "Задачи М311"},
			Body:  "М311. Условие задачи.",
		},
		{
			Front: corpus.ArticleFront{Issue: "kvant_1975_7", Year: 1975, Number: "7", Title: "Решения задач М311"},
			Body:  "М311. Ответ и доказательство.",
		},
	}
	res := Build(articles)
	if len(res.Documents) != 1 {
		t.Fatalf("built %d documents, want 1", len(res.Documents))
	}
	doc := res.Documents[0]
	if doc.Question != "Условие задачи." {
		t.Fatalf("question is %q", doc.Question)
	}
	if doc.Answer != "Ответ и доказательство." {
		t.Fatalf("answer is %q", doc.Answer)
	}
	if !doc.Front.HasPublishedSolution {
		t.Fatal("the front matter should record that an answer was printed")
	}
	if doc.Front.PosedIn != "kvant_1975_3" || doc.Front.SolvedIn != "kvant_1975_7" {
		t.Fatalf("front matter points at %q and %q", doc.Front.PosedIn, doc.Front.SolvedIn)
	}
}

func TestAProblemWithNoPrintedAnswerIsMarkedAndKept(t *testing.T) {
	res := Build([]Article{{
		Front: corpus.ArticleFront{Issue: "kvant_1975_3", Year: 1975, Number: "3", Title: "Задачи М311"},
		Body:  "М311. Условие без ответа.",
	}})
	if len(res.Manifest.Entries) != 1 {
		t.Fatalf("built %d entries, want 1", len(res.Manifest.Entries))
	}
	if res.Manifest.Entries[0].Solvable() {
		t.Fatal("nothing published an answer, so it is not solvable")
	}
	if got := res.Manifest.Count(); got.PosedOnly != 1 || got.Both != 0 {
		t.Fatalf("counts came out %v", got)
	}
	if res.Documents[0].Front.HasPublishedSolution {
		t.Fatal("front matter claims a solution that was never printed")
	}
}

func TestAPromisedNumberThatIsNotOnThePageIsAGap(t *testing.T) {
	// The heading is a printed claim. Five promised and three delivered means
	// the reading lane lost two, and that is the whole point of the gap list.
	res := Build([]Article{{
		Front: corpus.ArticleFront{Issue: "kvant_1975_3", Year: 1975, Number: "3", Title: "Задачи М311—М313"},
		Body:  "М311. Первое.\n\nМ313. Третье.",
	}})
	if len(res.Gaps) != 1 {
		t.Fatalf("expected one gap, got %v", res.Gaps)
	}
	if res.Gaps[0].ID != "M312" {
		t.Fatalf("the missing problem is %q, want M312", res.Gaps[0].ID)
	}
}

func TestASecondPrintingDoesNotOverwriteTheFirst(t *testing.T) {
	articles := []Article{
		{
			Front: corpus.ArticleFront{Issue: "kvant_1975_3", Year: 1975, Number: "3", Title: "Задачи М311"},
			Body:  "М311. Условие как напечатано.",
		},
		{
			Front: corpus.ArticleFront{Issue: "kvant_1975_9", Year: 1975, Number: "9", Title: "Задачи М311"},
			Body:  "М311. Условие с поправкой.",
		},
	}
	res := Build(articles)
	entry := res.Manifest.Entries[0]
	if entry.Posed.Issue != "kvant_1975_3" {
		t.Fatalf("the first printing should win, got %q", entry.Posed.Issue)
	}
	if len(res.Gaps) != 1 || res.Gaps[0].Issue != "kvant_1975_9" {
		t.Fatalf("the reprint should be reported, got %v", res.Gaps)
	}
}

func TestTheManifestSortsIntoSeriesAndNumberOrder(t *testing.T) {
	res := Build([]Article{{
		Front: corpus.ArticleFront{Issue: "kvant_1975_3", Year: 1975, Number: "3", Title: "Задачи М311—М312; Ф323"},
		Body:  "Ф323. Физика.\n\nМ312. Второе.\n\nМ311. Первое.",
	}})
	got := []string{}
	for _, e := range res.Manifest.Entries {
		got = append(got, e.ID)
	}
	want := []string{"M311", "M312", "F323"}
	if len(got) != len(want) {
		t.Fatalf("manifest has %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("manifest order is %v, want %v", got, want)
		}
	}
}
