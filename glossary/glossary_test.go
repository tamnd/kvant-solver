package glossary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAGlossaryWithATermTwiceIsRefused(t *testing.T) {
	// Two rows for one term means whichever is found first wins, and which that
	// is depends on sort order, so it shows up as one article in forty using the
	// wrong word.
	_, err := Parse([]byte(`
version: 1
terms:
  - ru: производная
    en: derivative
  - ru: Производная
    en: differential coefficient
`), "test")
	if err == nil {
		t.Fatal("a glossary with the same term twice was accepted")
	}
}

func TestAnUnknownColumnIsRefusedRatherThanIgnored(t *testing.T) {
	// A typo in a language tag would otherwise mean the column is silently
	// dropped and the term is quietly not applied to that language.
	_, err := Parse([]byte(`
version: 1
terms:
  - ru: производная
    eng: derivative
`), "test")
	if err == nil {
		t.Fatal("an unknown column was accepted")
	}
}

func TestATermWithNoRussianIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
version: 1
terms:
  - en: derivative
`), "test")
	if err == nil {
		t.Fatal("a row with no ru was accepted")
	}
}

func TestReorderingIsNotAChange(t *testing.T) {
	// This is the one that matters most. A version bump invalidates every file
	// translated against the old version, so bumping it because the file was
	// rewritten in a different order would mean retranslating the corpus for
	// nothing.
	a := []Term{{RU: "ток", EN: "current"}, {RU: "производная", EN: "derivative"}}
	b := []Term{{RU: "производная", EN: "derivative"}, {RU: "ток", EN: "current"}}
	if !SameTerms(a, b) {
		t.Fatal("a reordering was reported as a change")
	}
	c := []Term{{RU: "производная", EN: "derivative"}, {RU: "ток", EN: "amperage"}}
	if SameTerms(a, c) {
		t.Fatal("a changed rendering was reported as no change")
	}
}

func TestTheVersionMovesOnlyWhenATermMoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glossary.yaml")

	g := &Glossary{Terms: []Term{{RU: "производная", EN: "derivative"}}}
	version, bumped, err := g.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 || !bumped {
		t.Fatalf("a new glossary saved as version %d, bumped %v, want 1 and true", version, bumped)
	}

	same := &Glossary{Terms: []Term{{RU: "производная", EN: "derivative"}}}
	version, bumped, err = same.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 || bumped {
		t.Fatalf("saving the same terms gave version %d, bumped %v, want 1 and false", version, bumped)
	}

	more := &Glossary{Terms: []Term{
		{RU: "производная", EN: "derivative"},
		{RU: "ток", EN: "current"},
	}}
	version, bumped, err = more.Save(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != 2 || !bumped {
		t.Fatalf("adding a term gave version %d, bumped %v, want 2 and true", version, bumped)
	}
}

func TestASavedGlossaryReadsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glossary.yaml")
	g := &Glossary{Terms: []Term{
		{RU: "ток", EN: "current", VI: "dòng điện", Note: "not amperage"},
		{RU: "производная", EN: "derivative", Quantum: "derivative"},
	}}
	if _, _, err := g.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(b, path)
	if err != nil {
		t.Fatal(err)
	}
	if !SameTerms(g.Terms, back.Terms) {
		t.Fatalf("the glossary did not survive a round trip: %+v", back.Terms)
	}
	if back.Terms[0].RU != "производная" {
		t.Fatalf("the saved glossary was not sorted, first row is %q", back.Terms[0].RU)
	}
}

func TestTheTermsHashCoversTheRenderingAndNotJustTheTerm(t *testing.T) {
	// A file is stale when a row it was shown changed. If the hash only covered
	// the Russian, changing derivative to differential coefficient would leave
	// every file that uses the word looking current.
	a := []Term{{RU: "производная", EN: "derivative"}}
	b := []Term{{RU: "производная", EN: "differential coefficient"}}
	if TermsHash(a, "en") == TermsHash(b, "en") {
		t.Fatal("changing the English left the hash alone")
	}
	// The hash goes into front matter and is compared against a value computed
	// on another machine on another day, so it has to be a function of the rows
	// and of nothing else.
	again := []Term{{RU: "производная", EN: "derivative"}}
	if TermsHash(a, "en") != TermsHash(again, "en") {
		t.Fatal("the same rows hashed to two different values")
	}
}

func TestTheTermsHashDoesNotDependOnOrder(t *testing.T) {
	a := []Term{{RU: "ток", EN: "current"}, {RU: "производная", EN: "derivative"}}
	b := []Term{{RU: "производная", EN: "derivative"}, {RU: "ток", EN: "current"}}
	if TermsHash(a, "en") != TermsHash(b, "en") {
		t.Fatal("the hash moved when only the order did")
	}
}

func TestTheTermsHashIsPerLanguage(t *testing.T) {
	// The Vietnamese file and the English file are shown different columns of the
	// same rows, so editing the Vietnamese must not make the English stale.
	terms := []Term{{RU: "ток", EN: "current", VI: "dòng điện"}}
	edited := []Term{{RU: "ток", EN: "current", VI: "dòng"}}
	if TermsHash(terms, "en") != TermsHash(edited, "en") {
		t.Fatal("editing the Vietnamese moved the English hash")
	}
	if TermsHash(terms, "vi") == TermsHash(edited, "vi") {
		t.Fatal("editing the Vietnamese left the Vietnamese hash alone")
	}
}

func TestARowWithNoRenderingIsNotOffered(t *testing.T) {
	g := Glossary{Version: 1, Terms: []Term{
		{RU: "ток", EN: "current"},
		{RU: "производная"},
	}}
	index := g.In("en")
	if len(index) != 1 {
		t.Fatalf("expected one usable row, got %d", len(index))
	}
	if _, ok := index[Key("производная")]; ok {
		t.Fatal("a row with no English was offered to an English translation")
	}
}
