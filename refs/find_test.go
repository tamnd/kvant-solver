package refs

import "testing"

func TestTheJournalCitesItselfEveryWayItKnows(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []Citation
	}{{
		name: "year, issue and page, which is the full form",
		body: "см. «Квант», 1970, № 9, с. 32",
		want: []Citation{{Kind: KindPage, Year: 1970, Number: "9", Page: 32}},
	}, {
		name: "no year means the volume the article is printed in",
		body: "см. «Квант» № 4, с. 16",
		want: []Citation{{Kind: KindPage, Number: "4", Page: 16}},
	}, {
		name: "an issue and no page is a pointer at the issue",
		body: "об этом писал «Квант», 1974, № 5",
		want: []Citation{{Kind: KindIssue, Year: 1974, Number: "5"}},
	}, {
		name: "a page written with a Latin c, which is what a scan gives",
		body: "«Квант», 1975, № 1, c. 2",
		want: []Citation{{Kind: KindPage, Year: 1975, Number: "1", Page: 2}},
	}, {
		name: "the word за between the name and the year",
		body: "«Квант» за 1973 год, № 2, с. 11",
		want: []Citation{{Kind: KindPage, Year: 1973, Number: "2", Page: 11}},
	}}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Find(c.body)
			if len(got) != len(c.want) {
				t.Fatalf("found %d citations in %q, want %d: %+v", len(got), c.body, len(c.want), got)
			}
			for i := range got {
				if got[i].Kind != c.want[i].Kind || got[i].Year != c.want[i].Year ||
					got[i].Number != c.want[i].Number || got[i].Page != c.want[i].Page {
					t.Errorf("got %+v, want %+v", got[i], c.want[i])
				}
			}
		})
	}
}

// The sentence this is taken from is why the tail is read token by token rather
// than matched against a window. A window wide enough to hold a year and a page
// also holds the № 1 at the end, which belongs to a problem and not to the
// issue being cited.
func TestTheReaderStopsAtTheEndOfTheCitation(t *testing.T) {
	body := "(«Квант» № 1, 1975) допущена опечатка: в условиях задачи № 1"
	got := Find(body)
	if len(got) != 1 {
		t.Fatalf("found %d citations, want 1: %+v", len(got), got)
	}
	if got[0].Number != "1" || got[0].Year != 1975 || got[0].Page != 0 {
		t.Errorf("got %+v, want issue 1 of 1975 and no page", got[0])
	}
	if got[0].Kind != KindIssue {
		t.Errorf("kind is %s, want %s, because no page was printed", got[0].Kind, KindIssue)
	}
}

func TestTwoIssuesInOneCitationComeBackAsTwo(t *testing.T) {
	got := Find("см. «Квант», 1974, №№ 1 и 2")
	if len(got) != 2 {
		t.Fatalf("found %d citations, want 2: %+v", len(got), got)
	}
	if got[0].Number != "1" || got[1].Number != "2" {
		t.Errorf("got issues %q and %q, want 1 and 2", got[0].Number, got[1].Number)
	}
	for _, cite := range got {
		if cite.Year != 1974 {
			t.Errorf("year is %d, want 1974 on both", cite.Year)
		}
	}
}

// A page belongs to one issue, so a citation that named two cannot say which
// one the page is in, and saying nothing beats guessing.
func TestAPageIsDroppedWhenTheCitationNamesTwoIssues(t *testing.T) {
	got := Find("«Квант», 1974, №№ 1 и 2, с. 30")
	if len(got) != 2 {
		t.Fatalf("found %d citations, want 2: %+v", len(got), got)
	}
	for _, cite := range got {
		if cite.Kind != KindIssue {
			t.Errorf("kind is %s, want %s", cite.Kind, KindIssue)
		}
		if cite.Page != 0 {
			t.Errorf("page is %d, want it dropped", cite.Page)
		}
	}
}

// A year with no issue number points at a volume, and a volume is not something
// the graph has an edge to.
func TestABareYearIsNotACitation(t *testing.T) {
	for _, body := range []string{
		"журнал «Квант» выходит с 1970 года",
		"как писал «Квант», это давняя задача",
		"«Квант», 1975 год",
	} {
		if got := Find(body); len(got) != 0 {
			t.Errorf("found %d citations in %q, want none: %+v", len(got), body, got)
		}
	}
}

func TestProblemNumbersAreFoundInBothAlphabets(t *testing.T) {
	got := Find("это задачи М291 и M297, а также Ф1024")
	if len(got) != 3 {
		t.Fatalf("found %d citations, want 3: %+v", len(got), got)
	}
	want := []string{"M291", "M297", "F1024"}
	for i, cite := range got {
		if cite.Kind != KindProblem {
			t.Errorf("%d: kind is %s, want %s", i, cite.Kind, KindProblem)
		}
		if cite.Problem != want[i] {
			t.Errorf("%d: got %q, want %q", i, cite.Problem, want[i])
		}
	}
}

// The Cyrillic М and the Latin M are one letter to a reader and two to a
// program, and a scan of a Russian page produces both. They have to normalise
// to one thing or the same problem gets two tags.
func TestTheTwoAlphabetsGiveTheSameProblem(t *testing.T) {
	cyrillic := Find("задача М381")
	latin := Find("задача M381")
	if len(cyrillic) != 1 || len(latin) != 1 {
		t.Fatalf("got %d and %d citations, want 1 each", len(cyrillic), len(latin))
	}
	if cyrillic[0].Problem != latin[0].Problem {
		t.Errorf("%q and %q, want them equal", cyrillic[0].Problem, latin[0].Problem)
	}
}

func TestALetterInsideAWordIsNotAProblem(t *testing.T) {
	for _, body := range []string{
		"формула AM381 ничего не значит",
		"точка ОМ12 на чертеже",
	} {
		if got := Find(body); len(got) != 0 {
			t.Errorf("found %d citations in %q, want none: %+v", len(got), body, got)
		}
	}
}

// One digit next to a letter is a variable far more often than a problem, and
// the magazine had reached the hundreds by 1970 anyway.
func TestASingleDigitIsNotAProblem(t *testing.T) {
	if got := Find("пусть M1 и M2 середины сторон"); len(got) != 0 {
		t.Errorf("found %d citations, want none: %+v", len(got), got)
	}
}

func TestCitationsComeBackInTheOrderTheyAppear(t *testing.T) {
	got := Find("сначала М100, потом «Квант», 1974, № 5, и наконец Ф200")
	if len(got) != 3 {
		t.Fatalf("found %d citations, want 3: %+v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Offset < got[i-1].Offset {
			t.Errorf("citation %d starts at %d, before %d", i, got[i].Offset, got[i-1].Offset)
		}
	}
	if got[0].Problem != "M100" || got[1].Number != "5" || got[2].Problem != "F200" {
		t.Errorf("out of order: %+v", got)
	}
}

// The text is kept so that a citation nobody can resolve is still readable in
// the report and can be judged by a person against the printed page.
func TestTheTextOfTheCitationIsKept(t *testing.T) {
	got := Find("см. «Квант», 1970, № 9, с. 32, где это доказано")
	if len(got) != 1 {
		t.Fatalf("found %d citations, want 1", len(got))
	}
	if got[0].Text != "«Квант», 1970, № 9, с. 32" {
		t.Errorf("text is %q, want the citation and nothing after it", got[0].Text)
	}
}

func TestLeadingZerosComeOffTheNumbers(t *testing.T) {
	got := Find("задача М0381")
	if len(got) != 1 || got[0].Problem != "M381" {
		t.Fatalf("got %+v, want M381", got)
	}
}
