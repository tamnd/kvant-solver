package quantum_test

import (
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/quantum"
	"github.com/tamnd/kvant-solver/source/nsta"
)

// The spellings on the left are what a Russian surname turns into under one
// scheme or another, and every group here is one person.
func TestTwoSpellingsOfOneSurnameFoldTogether(t *testing.T) {
	for _, group := range [][]string{
		{"Solovyov", "Solovyev", "Soloviev", "Solovev"},
		{"Mishchenko", "Mischenko"},
		{"Smorodinsky", "Smorodinskii", "Smorodinskij"},
		{"Chernoutsan", "Chernoutzan"},
		{"Tikhomirov", "Tihomirov"},
	} {
		want := quantum.Fold(group[0])
		for _, other := range group[1:] {
			if got := quantum.Fold(other); got != want {
				t.Errorf("%s folds to %q and %s to %q", group[0], want, other, got)
			}
		}
	}
}

// The folding has to stop short of pulling different people together, which is
// the failure that would put wrong rows in the map.
func TestDifferentSurnamesStayApart(t *testing.T) {
	for _, pair := range [][2]string{
		{"Savin", "Savvin"},
		{"Vasilyev", "Vasilenko"},
		{"Gik", "Gil"},
		{"Fuchs", "Fomin"},
	} {
		if quantum.Fold(pair[0]) == quantum.Fold(pair[1]) {
			t.Errorf("%s and %s fold to the same thing", pair[0], pair[1])
		}
	}
}

func TestARussianSurnameReachesItsEnglishSpelling(t *testing.T) {
	for _, c := range [][2]string{
		{"Ширшов", "Shirshov"},
		{"Мищенко", "Mishchenko"},
		{"Стасенко", "Stasenko"},
		{"Фельдман", "Feldman"},
		{"Соловьёв", "Solovyov"},
		{"Чхартишвили", "Chkhartishvili"},
	} {
		ru, en := quantum.Fold(quantum.Translit(c[0])), quantum.Fold(c[1])
		if ru != en {
			t.Errorf("%s folds to %q and %s to %q", c[0], ru, c[1], en)
		}
	}
}

// Юрий is Yuri and Яков is Yakov, so those initials are a Y in English and
// nothing like a Y under a mechanical transliteration. Getting this wrong
// silently drops every article by either of them.
func TestAnInitialMatchesTheNameItStandsFor(t *testing.T) {
	for _, c := range []struct {
		ru, en string
		want   bool
	}{
		{"Ю.", "Y.", true},
		{"Ю.", "Yuri", true},
		{"Я.", "Y.", true},
		{"Е.", "Yevgeny", true},
		{"Е.", "Evgeny", true},
		{"А.", "Alexander", true},
		{"С.", "Sergey", true},
		{"С.", "Alexander", false},
		{"Л.", "A.", false},
	} {
		got := quantum.SameInitial(quantum.Initial(c.ru), quantum.Initial(c.en))
		if got != c.want {
			t.Errorf("%s against %s: %v", c.ru, c.en, got)
		}
	}
}

// A byline with no initial on one side is not a disagreement. Plenty of Kvant
// rows are a bare surname and refusing those would throw away good rows.
func TestAMissingInitialIsNotADisagreement(t *testing.T) {
	if !quantum.SameInitial("", "a") || !quantum.SameInitial("a", "") {
		t.Error("a missing initial was read as a clash")
	}
}

func TestARussianBylineComesApartIntoPeople(t *testing.T) {
	people := quantum.Russian("Ширшов А. И., Никитин А. А.")
	if len(people) != 2 {
		t.Fatalf("%d people: %v", len(people), people)
	}
	if people[0].Surname != quantum.Fold("Shirshov") {
		t.Errorf("first surname %q", people[0].Surname)
	}
}

func TestAnEnglishNameComesApartIntoAPerson(t *testing.T) {
	p, ok := quantum.English("A. Shirshov")
	if !ok || p.Surname != quantum.Fold("Shirshov") || !quantum.SameInitial(p.Initial, "a") {
		t.Fatalf("%v %v", p, ok)
	}
	// A byline that is nothing but initials has no surname to match on.
	if _, ok := quantum.English("A. B."); ok {
		t.Error("a bare pair of initials was read as a surname")
	}
}

// toc is a small table of contents with the shapes the match has to tell
// apart: a pair of authors nobody else shares, one prolific author, and a
// surname whose only appearance is after the English article came out.
func toc() *manifest.TOC {
	return &manifest.TOC{Issues: []manifest.IssueTOC{
		{Key: "kvant_1979_10", Rows: []manifest.Row{
			{Title: "Обобщённая сумма углов многогранника", Authors: "Ширшов А. И., Никитин А. А.", Page: 14},
			{Title: "Шахматная страничка", Authors: "Гик Е. Я.", Page: 60},
		}},
		{Key: "kvant_1983_7", Rows: []manifest.Row{
			{Title: "Алгебраические и трансцендентные числа", Authors: "Фельдман Н. И.", Page: 2},
			{Title: "Ещё одна шахматная страничка", Authors: "Гик Е. Я.", Page: 55},
			{Title: "Заметка без автора", Page: 61},
		}},
		{Key: "kvant_1999_3", Rows: []manifest.Row{
			{Title: "Поздняя статья", Authors: "Позднеев В. К.", Page: 8},
		}},
	}}
}

func entry(title string, year int, authors ...string) nsta.Entry {
	return nsta.Entry{Title: title, Authors: authors, Year: year, Months: "May/Jun", Page: 4}
}

func TestAPairOfAuthorsPinsDownTheRussianOriginal(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(
		entry("Adding Angles in Three Dimensions", 1997, "A. Shirshov", "A. Nikitin"))
	if got.Article == nil {
		t.Fatalf("no match, %d candidates", got.Candidates)
	}
	if got.Article.Key != "kvant_1979_10" || !got.Whole {
		t.Errorf("matched %s, whole %v", got.Article.Key, got.Whole)
	}
}

// The titles are no help. A translated title was rewritten rather than
// translated, and the match has to hold up without it.
func TestOneAuthorWithARareSurnameIsEnough(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(entry("Algebraic and Transcendental Numbers", 2000, "N. Feldman"))
	if got.Article == nil || got.Article.Key != "kvant_1983_7" {
		t.Fatalf("%v", got)
	}
}

// A prolific author leaves nothing to choose between. Guessing would be worse
// than the gap, because a wrong row argues for a wrong word with the authority
// of the published English.
func TestAProlificAuthorIsLeftUnmatched(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(entry("Chess Corner", 1995, "E. Gik"))
	if got.Article != nil {
		t.Fatalf("matched %s when the author wrote several", got.Article.Key)
	}
	if got.Candidates != 2 {
		t.Errorf("candidates %d, want 2", got.Candidates)
	}
}

// A translation cannot come out before the thing it translates.
func TestARussianArticlePrintedLaterIsNotTheOriginal(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(entry("Something Early", 1993, "V. Pozdneyev"))
	if got.Article != nil {
		t.Errorf("matched %s, printed after the English", got.Article.Key)
	}
}

func TestAnArticleWithNoRussianAuthorIsNotMatched(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(entry("Cowculations", 1995, "David Halliday"))
	if got.Article != nil || got.Candidates != 0 {
		t.Errorf("%v", got)
	}
}

// A row with no byline cannot be matched on one, and it is not a candidate for
// anything either.
func TestAnUnsignedArticleIsNotMatched(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(entry("Kaleidoscope", 1995))
	if got.Article != nil || got.Candidates != 0 {
		t.Errorf("%v", got)
	}
}

func TestTheMapCountsWhatItMappedAndWhatItCouldNot(t *testing.T) {
	m := quantum.Build([]nsta.Entry{
		entry("Adding Angles in Three Dimensions", 1997, "A. Shirshov", "A. Nikitin"),
		entry("Chess Corner", 1995, "E. Gik"),
		entry("Cowculations", 1995, "David Halliday"),
	}, toc())

	if m.Count != 3 || m.Mapped != 1 || m.Ambiguous() != 1 {
		t.Errorf("count %d, mapped %d, ambiguous %d", m.Count, m.Mapped, m.Ambiguous())
	}
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	mapped := m.ByKvant("kvant_1979_10")
	if len(mapped) != 1 || mapped[0].KvantTitle != "Обобщённая сумма углов многогранника" {
		t.Fatalf("%v", mapped)
	}
	if mapped[0].Match != manifest.MatchAuthors {
		t.Errorf("match %q", mapped[0].Match)
	}
}

// Quantum shortened a byline of two to one often enough that it is worth
// keeping, and it is a weaker claim than the two bylines agreeing, so the row
// says which one it is.
func TestAShortenedBylineIsMatchedAndSaidToBeWeaker(t *testing.T) {
	got := quantum.NewIndex(toc()).Match(entry("Adding Angles", 1997, "A. Nikitin"))
	if got.Article == nil || got.Article.Key != "kvant_1979_10" {
		t.Fatalf("%v", got)
	}
	if got.Whole {
		t.Error("a shortened byline was reported as the whole one")
	}
}

func TestAMapWithNoSourceIsRefused(t *testing.T) {
	m := &manifest.Quantum{Articles: []manifest.QuantumArticle{{Title: "x", Year: 1995}}}
	if err := m.Check(); err == nil {
		t.Fatal("a map that does not say where it came from was accepted")
	}
}

func TestAnArticleOutsideTheRunIsRefused(t *testing.T) {
	m := &manifest.Quantum{Source: nsta.IndexURL,
		Articles: []manifest.QuantumArticle{{Title: "x", Year: 2004}}}
	if err := m.Check(); err == nil {
		t.Fatal("an article from after the magazine closed was accepted")
	}
}
