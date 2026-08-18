package glossary

import "testing"

func terms() Glossary {
	return Glossary{Version: 1, Terms: []Term{
		{RU: "производная", EN: "derivative"},
		{RU: "ток", EN: "current"},
		{RU: "полное внутреннее отражение", EN: "total internal reflection"},
		{RU: "окружность", EN: "circle"},
	}}
}

func TestAnInflectedTermIsStillFound(t *testing.T) {
	// This is the test the whole package turns on. The glossary is written in the
	// nominative and Russian articles almost never use it, so a matcher that only
	// fires on the dictionary form is a glossary that is quietly not applied.
	g := terms()
	for _, body := range []string{
		"Найдём значение производной в этой точке.",
		"Производную функции берём по времени.",
		"График производных пересекает ось.",
	} {
		found := g.Mentioned(body, "en")
		if len(found) != 1 || found[0].RU != "производная" {
			t.Fatalf("%q matched %v, want производная", body, names(found))
		}
	}
}

func TestAMultiWordTermMatchesWhenAllItsWordsAppear(t *testing.T) {
	g := terms()
	body := "При полном внутреннем отражении луч не выходит из среды."
	found := g.Mentioned(body, "en")
	if len(found) != 1 || found[0].RU != "полное внутреннее отражение" {
		t.Fatalf("matched %v, want полное внутреннее отражение", names(found))
	}
}

func TestATermInsideAFormulaIsNotAMention(t *testing.T) {
	// Formulas are full of short fragments and «ток» is three letters. Matching
	// inside them produces rows nobody needs, and worse, makes a file stale for a
	// term it never used in prose.
	g := terms()
	body := "Рассмотрим величину $I_{ток} = 5$ А в этой цепи."
	if found := g.Mentioned(body, "en"); len(found) != 0 {
		t.Fatalf("matched inside a formula: %v", names(found))
	}
}

func TestANotMentionedTermIsNotOffered(t *testing.T) {
	g := terms()
	body := "Докажите, что сумма углов треугольника равна развёрнутому углу."
	if found := g.Mentioned(body, "en"); len(found) != 0 {
		t.Fatalf("offered rows for a body that uses none of them: %v", names(found))
	}
}

func TestARowWithNoRenderingIsNotMentioned(t *testing.T) {
	g := Glossary{Version: 1, Terms: []Term{{RU: "ток"}}}
	if found := g.Mentioned("Через проводник идёт ток.", "en"); len(found) != 0 {
		t.Fatalf("offered a row with no English: %v", names(found))
	}
}

func TestAShortWordIsNotStemmedIntoSomethingElse(t *testing.T) {
	// Stripping the ending off «ток» would leave «то», which is one of the
	// commonest words in Russian, and every page in the corpus would then be told
	// about electric current.
	if stem("ток") != "ток" {
		t.Fatalf("ток stemmed to %q", stem("ток"))
	}
	g := terms()
	body := "Известно, что то же самое верно и в общем случае."
	if found := g.Mentioned(body, "en"); len(found) != 0 {
		t.Fatalf("a short word was stemmed into a match: %v", names(found))
	}
}

func TestAShortTermIsStillFoundInflected(t *testing.T) {
	// The stemmer will not cut a three letter word down, so «ток» and «тока»
	// never meet in the stem set and the second matcher has to catch them. Short
	// words are most of the physics vocabulary, so this is not an edge case.
	g := terms()
	for _, body := range []string{
		"Через проводник идёт ток.",
		"Сила тока в цепи постоянна.",
		"Измерим напряжение и сравним с током.",
		"В обоих токах направление совпадает.",
	} {
		found := g.Mentioned(body, "en")
		if len(found) != 1 || found[0].RU != "ток" {
			t.Fatalf("%q matched %v, want ток", body, names(found))
		}
	}
}

func TestALongerWordThatMerelyStartsTheSameIsNotAMatch(t *testing.T) {
	// The tail that separates two stems has to be made of ending letters, which
	// is what keeps this from matching every word in the language that happens to
	// begin with a short term.
	g := terms()
	for _, body := range []string{
		"Токарь выточил деталь на станке.",
		"Токсичность вещества здесь ни при чём.",
	} {
		if found := g.Mentioned(body, "en"); len(found) != 0 {
			t.Fatalf("%q matched %v, want nothing", body, names(found))
		}
	}
}

func TestTheOrderOfTheOfferedRowsIsStable(t *testing.T) {
	// The rows go into a prompt and their hash decides staleness, so an order
	// that depends on map iteration would make a file stale at random.
	g := terms()
	body := "Ток течёт по окружности, и производная тока меняет знак."
	first := names(g.Mentioned(body, "en"))
	for range 20 {
		if got := names(g.Mentioned(body, "en")); !equal(got, first) {
			t.Fatalf("the order moved between runs: %v then %v", first, got)
		}
	}
	if len(first) != 3 {
		t.Fatalf("expected three rows, got %v", first)
	}
}

func TestTheHashOfTheOfferedRowsIsWhatChanges(t *testing.T) {
	// The composition of the staleness rule depends on this: showing a file the
	// rows it mentions means an unrelated term can be added without touching it.
	g := terms()
	body := "Через проводник идёт ток."
	before := TermsHash(g.Mentioned(body, "en"), "en")

	g.Terms = append(g.Terms, Term{RU: "дифракция", EN: "diffraction"})
	if after := TermsHash(g.Mentioned(body, "en"), "en"); after != before {
		t.Fatal("adding an unrelated term changed the rows this file was shown")
	}

	for i := range g.Terms {
		if g.Terms[i].RU == "ток" {
			g.Terms[i].EN = "electric current"
		}
	}
	if after := TermsHash(g.Mentioned(body, "en"), "en"); after == before {
		t.Fatal("changing a term this file uses left its rows unchanged")
	}
}

func names(terms []Term) []string {
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		out = append(out, t.RU)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
