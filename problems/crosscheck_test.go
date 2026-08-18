package problems

import (
	"path/filepath"
	"testing"
)

func entry(id, posed, solved string) Entry {
	e := Entry{ID: id}
	if posed != "" {
		e.Posed = &Printing{Issue: posed}
	}
	if solved != "" {
		e.Solved = &Printing{Issue: solved}
	}
	return e
}

func TestTwoSourcesOnTheSamePageAgree(t *testing.T) {
	c := Cross(entry("M301", "kvant_1975_1", "kvant_1975_8"), "",
		Joined{ID: "M301", PosedIn: "kvant_1975_1", SolvedIn: "kvant_1975_8"})
	if c.Posed != Same || c.Solved != Same {
		t.Fatalf("verdicts are %q and %q", c.Posed, c.Solved)
	}
	if !c.Agrees() {
		t.Fatal("nothing here needs a person to look at it")
	}
}

func TestAProblemFiledOnTheWrongPageIsCaught(t *testing.T) {
	// The case this was written for. A problem printed on the page that opens
	// the solutions article was read as a published answer, so our solved half
	// names an issue the site gives as the one that set it.
	c := Cross(entry("F351", "", "kvant_1975_8"), "",
		Joined{ID: "F351", PosedIn: "kvant_1975_8", SolvedIn: "kvant_1976_4"})
	if c.Solved != Moved {
		t.Fatalf("solved verdict is %q, want moved", c.Solved)
	}
	if c.Agrees() {
		t.Fatal("a moved half is exactly what somebody has to look at")
	}
	if c.Posed != OnlyIndexed {
		t.Fatalf("posed verdict is %q, and the site is the only one with that half", c.Posed)
	}
}

func TestAHalfOnlyOneSideHasIsNotADisagreement(t *testing.T) {
	// Most of the archive is like this in both directions, and counting it as a
	// disagreement would make the rate a report on how much of the site has been
	// digitised.
	ours := Cross(entry("M311", "kvant_1975_3", ""), "", Joined{ID: "M311"})
	if ours.Posed != OnlyRead || ours.Solved != Neither {
		t.Fatalf("verdicts are %q and %q", ours.Posed, ours.Solved)
	}
	if !ours.Agrees() {
		t.Fatal("a number the site does not index is not a fault in our reading")
	}
}

func TestTheSiteAnsweringAProblemBeforeItWasSetIsTheSiteBeingWrong(t *testing.T) {
	// F372, verbatim off the site: the condition in the twelfth issue of 1976
	// and the solution in the ninth. The magazine could not print an answer
	// three issues before the problem, so the disagreement is settled without
	// anybody opening a page.
	c := Cross(entry("F372", "kvant_1975_12", ""), "",
		Joined{ID: "F372", PosedIn: "kvant_1976_12", SolvedIn: "kvant_1976_9"})
	if c.Posed != Moved {
		t.Fatalf("posed verdict is %q, want moved", c.Posed)
	}
	if !c.TheirsRunsBackwards() {
		t.Fatal("the site has the answer printed before the problem and that should be visible")
	}
}

func TestAJoinedIssueNumberIsOrderedOnTheShelf(t *testing.T) {
	// The double issues are the reason the comparison cannot be a string one.
	if !earlier("kvant_1976_5-6", "kvant_1976_9") {
		t.Error("kvant_1976_5-6 comes before kvant_1976_9 on the shelf")
	}
	if earlier("kvant_1976_10", "kvant_1976_9") {
		t.Error("issue 10 is not before issue 9")
	}
	if !earlier("kvant_1975_12", "kvant_1976_1") {
		t.Error("the year is read first")
	}
}

func TestTheStatementsAreComparedWordByWord(t *testing.T) {
	c := Cross(entry("M301", "kvant_1975_1", ""),
		"Докажите, что точка обруча описывает ту же кривую.",
		Joined{ID: "M301", PosedIn: "kvant_1975_1",
			Condition: "Докажите, что точка обруча описывает ту же траекторию."})
	if c.Condition.Words != 8 {
		t.Fatalf("the site has %d words, want 8", c.Condition.Words)
	}
	if c.Condition.Changed != 1 {
		t.Fatalf("%d words differ, want the one we read differently", c.Condition.Changed)
	}
	if len(c.Examples) == 0 {
		t.Fatal("a rate with no example under it is a number nobody can act on")
	}
}

func TestARunIsTalliedAndTheWorstComesFirst(t *testing.T) {
	x := &Crosscheck{
		Checks: []Check{
			Cross(entry("M301", "kvant_1975_1", "kvant_1975_8"), "одно и то же",
				Joined{PosedIn: "kvant_1975_1", SolvedIn: "kvant_1975_8", Condition: "одно и то же"}),
			Cross(entry("M302", "kvant_1975_1", ""), "совсем другие слова здесь",
				Joined{PosedIn: "kvant_1975_2", Condition: "одно и то же"}),
		},
		Absent: []string{"M999"},
	}
	tally := x.Tally()
	if tally.Compared != 2 || tally.Absent != 1 {
		t.Fatalf("tally is %+v", tally)
	}
	if tally.Posed.Same != 1 || tally.Posed.Moved != 1 || tally.Posed.Checked() != 2 {
		t.Fatalf("posed came out %+v", tally.Posed)
	}
	if tally.Posed.Rate() != 0.5 {
		t.Fatalf("agreement is %v, want half", tally.Posed.Rate())
	}
	if tally.Statements != 2 {
		t.Fatalf("%d statements compared", tally.Statements)
	}
	if got := x.Worst(1); len(got) != 1 || got[0].ID != "M302" {
		t.Fatalf("worst is %+v, want the one that disagrees", got)
	}
	if got := x.Disagreements(); len(got) != 1 || got[0].ID != "M302" {
		t.Fatalf("disagreements are %+v", got)
	}
}

func TestTheWorkListIsTheHalvesOnlyTheSiteHas(t *testing.T) {
	x := &Crosscheck{Checks: []Check{
		Cross(entry("M261", "", "kvant_1975_1"), "", Joined{PosedIn: "kvant_1974_5", SolvedIn: "kvant_1975_1"}),
		Cross(entry("M311", "kvant_1975_3", ""), "", Joined{PosedIn: "kvant_1975_3"}),
	}}
	toRead := x.ToRead()
	if len(toRead) != 1 || toRead[0].TheirPosed != "kvant_1974_5" {
		t.Fatalf("the work list is %+v, want the issue we have not read", toRead)
	}
}

func TestAFetchedPageIsKeptAndReadBack(t *testing.T) {
	// The store is what makes a second run offline and the report reproducible,
	// so a page that does not survive the round trip is a report that changes
	// under you.
	s := Joins{Dir: filepath.Join(t.TempDir(), "crosscheck")}
	if _, ok, err := s.Get("M301"); err != nil || ok {
		t.Fatalf("an empty store should say nothing is there: %v %v", ok, err)
	}
	want := Joined{ID: "M301", URL: "https://example.test/m301/", PosedIn: "kvant_1975_1",
		SolvedIn: "kvant_1975_8", Condition: "Условие."}
	if err := s.Put(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("M301")
	if err != nil || !ok {
		t.Fatalf("reading it back gave %v %v", ok, err)
	}
	if got != want {
		t.Fatalf("came back as %+v", got)
	}
}

func TestANumberTheSiteDoesNotHaveIsRememberedAsAnAnswer(t *testing.T) {
	// Not remembering it means asking the site again for several hundred
	// numbers whose answer is already known, every run.
	s := Joins{Dir: t.TempDir()}
	if err := s.Put(Joined{ID: "M999", Absent: true}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("M999")
	if err != nil || !ok {
		t.Fatalf("reading it back gave %v %v", ok, err)
	}
	if !got.Absent {
		t.Fatal("the store forgot that the site does not carry this number")
	}
}
