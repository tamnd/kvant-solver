package problems

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

// gradable builds an entry with both halves printed in the given year.
func gradable(subject corpus.Subject, number, year int) Entry {
	return Entry{
		ID:      corpus.ProblemID{Subject: subject, Number: number}.String(),
		Subject: subject,
		Number:  number,
		Posed:   &Printing{Issue: fmt.Sprintf("kvant_%d_1", year)},
		Solved:  &Printing{Issue: fmt.Sprintf("kvant_%d_5", year)},
	}
}

func TestASetOnlyHoldsProblemsThatCanBeMarked(t *testing.T) {
	// A problem with no printed answer cannot be scored. Letting one into a set
	// means the agreement rate is computed over whichever members happened to
	// have ground truth, which is a number that looks like a measurement and is
	// not one.
	m := &Manifest{Entries: []Entry{
		gradable(corpus.Math, 301, 1975),
		{ID: "M302", Subject: corpus.Math, Number: 302, Posed: &Printing{Issue: "kvant_1975_1"}},
		{ID: "M303", Subject: corpus.Math, Number: 303, Solved: &Printing{Issue: "kvant_1975_5"}},
	}}
	set := Stratify(m, 10)
	if set.Size != 1 || set.Members[0] != "M301" {
		t.Fatalf("set holds %v, want only M301", set.Members)
	}
}

func TestTheSampleIsSplitByDecadeAndSubject(t *testing.T) {
	var entries []Entry
	for n := range 20 {
		entries = append(entries, gradable(corpus.Math, 100+n, 1975))
		entries = append(entries, gradable(corpus.Physics, 200+n, 1985))
	}
	set := Stratify(&Manifest{Entries: entries}, 5)
	if len(set.Strata) != 2 {
		t.Fatalf("got %d strata, want 2: %+v", len(set.Strata), set.Strata)
	}
	for _, st := range set.Strata {
		if st.Taken != 5 || st.Available != 20 {
			t.Fatalf("stratum %ds %s took %d of %d, want 5 of 20",
				st.Decade, st.Subject, st.Taken, st.Available)
		}
	}
	if set.Strata[0].Decade != 1970 || set.Strata[1].Decade != 1980 {
		t.Fatalf("decades came out %d and %d", set.Strata[0].Decade, set.Strata[1].Decade)
	}
	if set.Size != 10 {
		t.Fatalf("set size is %d, want 10", set.Size)
	}
}

func TestTheSameManifestAlwaysDrawsTheSameSet(t *testing.T) {
	// A set with a random seed has to record the seed or it cannot be rebuilt,
	// and a scorecard nobody can reproduce is an anecdote.
	var entries []Entry
	for n := range 30 {
		entries = append(entries, gradable(corpus.Math, 100+n, 1975))
	}
	m := &Manifest{Entries: entries}
	first := Stratify(m, 7)
	second := Stratify(m, 7)
	if strings.Join(first.Members, ",") != strings.Join(second.Members, ",") {
		t.Fatalf("two draws differ:\n%v\n%v", first.Members, second.Members)
	}
}

func TestTheDrawCoversTheWholeRange(t *testing.T) {
	// Taking the first n would sample only the years the corpus finished first.
	var entries []Entry
	for n := range 100 {
		entries = append(entries, gradable(corpus.Math, 100+n, 1975))
	}
	set := Stratify(&Manifest{Entries: entries}, 4)
	if set.Members[0] != "M100" {
		t.Fatalf("draw starts at %s, want M100", set.Members[0])
	}
	if last := set.Members[len(set.Members)-1]; last != "M175" {
		t.Fatalf("draw ends at %s, which is not spread across the range", last)
	}
}

func TestAskingForMoreThanExistsTakesEverything(t *testing.T) {
	set := Stratify(&Manifest{Entries: []Entry{
		gradable(corpus.Math, 301, 1975),
		gradable(corpus.Math, 302, 1975),
	}}, 50)
	if set.Size != 2 {
		t.Fatalf("set size is %d, want 2", set.Size)
	}
}

func TestTheAnswerIsSeparableFromTheStatement(t *testing.T) {
	// M9 must be able to hand a solver the question while provably withholding
	// the printed answer. If these two ever cross, the whole grading apparatus
	// turns into a paraphrase detector and still reports a high score.
	doc := Document{Question: "Условие задачи.", Answer: "Ответ и доказательство."}
	body := doc.Body()
	if got := Statement(body); got != "Условие задачи." {
		t.Fatalf("statement is %q", got)
	}
	if got := PublishedSolution(body); got != "Ответ и доказательство." {
		t.Fatalf("published solution is %q", got)
	}
	if strings.Contains(Statement(body), "Ответ") {
		t.Fatalf("the answer leaked into the statement: %q", Statement(body))
	}
}

func TestAProblemWithNoPrintedAnswerHasNoSolutionHalf(t *testing.T) {
	body := Document{Question: "Условие без ответа."}.Body()
	if got := PublishedSolution(body); got != "" {
		t.Fatalf("invented a published solution: %q", got)
	}
	if got := Statement(body); got != "Условие без ответа." {
		t.Fatalf("statement is %q", got)
	}
}
