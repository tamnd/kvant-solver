package report_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/report"
)

// authorFixture writes one issue whose articles carry every shape of byline the
// report has to tell apart. The bodies are invented placeholders and none of
// them is text from the magazine.
func authorFixture(t *testing.T, bylines ...[]string) *corpus.Corpus {
	t.Helper()
	root := t.TempDir()
	for n, authors := range bylines {
		front := &corpus.ArticleFront{
			ID:        "1975-1-fixture-" + string(rune('a'+n)),
			Issue:     "kvant_1975_1",
			Year:      1975,
			Number:    "1",
			Title:     "Fixture article",
			Authors:   authors,
			PageFirst: n + 1,
			PageLast:  n + 1,
			Provenance: corpus.Provenance{
				Lang:       "ru",
				Source:     "fixture",
				Extraction: corpus.ExtractionVision,
			},
		}
		path := filepath.Join(root, "content/ru/1975/01/articles",
			string(rune('a'+n))+"_fixture.md")
		if err := corpus.Save(path, front, "Body of the fixture article.\n"); err != nil {
			t.Fatal(err)
		}
	}
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// found is the byline the report raised under this class, and a failure when it
// raised none or several.
func found(t *testing.T, bylines []report.Byline, class string) report.Byline {
	t.Helper()
	var out []report.Byline
	for _, b := range bylines {
		if b.Class == class {
			out = append(out, b)
		}
	}
	if len(out) != 1 {
		t.Fatalf("%d bylines of class %s, want 1: %v", len(out), class, out)
	}
	return out[0]
}

func TestABylineWithNoRussianInItIsNoise(t *testing.T) {
	c := authorFixture(t, []string{"xnyxzk"}, []string{"Ширшов А. И."})
	got, counts, err := report.Authors(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if counts.Articles != 2 || counts.Mentions != 2 || counts.Distinct != 2 {
		t.Fatalf("%+v", counts)
	}
	if name := found(t, got, report.NotAName).Name; name != "xnyxzk" {
		t.Errorf("raised %q", name)
	}
}

// The noise the decoder leaves on the end of a byline is Latin, because the
// name it was writing is not.
func TestANameWithNoiseOnTheEndIsRaisedAndTheNameIsNot(t *testing.T) {
	c := authorFixture(t, []string{"Абрамович В. С.l"}, []string{"Абрамович В. С."})
	got, _, err := report.Authors(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if name := found(t, got, report.LatinTail).Name; name != "Абрамович В. С.l" {
		t.Errorf("raised %q", name)
	}
	if len(got) != 1 {
		t.Errorf("the clean spelling of the same name was raised too: %v", got)
	}
}

func TestInitialsWithNoSurnameAreRaised(t *testing.T) {
	c := authorFixture(t, []string{"А. П."}, []string{"Гик Е. Я."})
	got, _, err := report.Authors(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if name := found(t, got, report.NoSurname).Name; name != "А. П." {
		t.Errorf("raised %q", name)
	}
}

// Every article that carries the bad spelling has to be listed, because the
// report is what the repair works off.
func TestEveryPlaceABadBylineAppearsIsListed(t *testing.T) {
	c := authorFixture(t, []string{"А. П."}, []string{"А. П."}, []string{"А. П."})
	got, _, err := report.Authors(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	row := found(t, got, report.NoSurname)
	if row.Count != 3 || len(row.Files) != 3 {
		t.Fatalf("count %d, files %v", row.Count, row.Files)
	}
	for _, f := range row.Files {
		if !strings.HasPrefix(f, "content/ru/1975/01/articles/") {
			t.Errorf("file %q is not a corpus path", f)
		}
	}
}

// A surname seen once next to one seen many times is the same surname read
// wrong on one page.
func TestARareNameOneLetterFromACommonOneIsRaised(t *testing.T) {
	bylines := [][]string{{"Дик Е. Я."}}
	for range 20 {
		bylines = append(bylines, []string{"Гик Е. Я."})
	}
	got, _, err := report.Authors(authorFixture(t, bylines...), "ru")
	if err != nil {
		t.Fatal(err)
	}
	row := found(t, got, report.LooksMisread)
	if row.Name != "Дик" || !strings.Contains(row.Note, "Гик") {
		t.Errorf("%q, note %q", row.Name, row.Note)
	}
}

// Two names that are both common are two people, however close they look.
// Штейнберг and Штернберг both wrote for the magazine.
func TestTwoCommonNamesThatLookAlikeAreLeftAlone(t *testing.T) {
	var bylines [][]string
	for range 10 {
		bylines = append(bylines, []string{"Штейнберг А. А."}, []string{"Штернберг А. А."})
	}
	got, _, err := report.Authors(authorFixture(t, bylines...), "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("two real surnames were called a misreading: %v", got)
	}
}

// Russian makes the feminine of a surname by adding a letter, so a name that is
// one letter longer than a common one is a different person and not a slip.
func TestAFeminineSurnameIsNotAMisreading(t *testing.T) {
	bylines := [][]string{{"Петрова М. К."}}
	for range 20 {
		bylines = append(bylines, []string{"Петров М. К."})
	}
	got, _, err := report.Authors(authorFixture(t, bylines...), "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a feminine surname was called a misreading: %v", got)
	}
}

func TestACorpusOfGoodBylinesRaisesNothing(t *testing.T) {
	c := authorFixture(t,
		[]string{"Ширшов А. И.", "Никитин А. А."},
		[]string{"Фельдман Н. И."},
		[]string{"Соловьёв Ю. П."},
	)
	got, counts, err := report.Authors(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("%v", got)
	}
	if counts.Mentions != 4 || counts.Distinct != 4 {
		t.Errorf("%+v", counts)
	}
	md := report.AuthorMarkdown(got, counts, time.Now())
	if !strings.Contains(md, "None of them looks wrong") {
		t.Errorf("a clean corpus reported as something to fix:\n%s", md)
	}
}

func TestTheDocumentSaysWhatIsWrongAndWhereItIs(t *testing.T) {
	c := authorFixture(t, []string{"xnyxzk"}, []string{"А. П."}, []string{"Ширшов А. И.l"})
	got, counts, err := report.Authors(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	md := report.AuthorMarkdown(got, counts, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"kvant report authors",
		"2026-08-19",
		"No Russian letter in it",
		"A name with noise stuck on the end",
		"Initials and no surname",
		"xnyxzk",
		"content/ru/1975/01/articles/",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("the document does not mention %q:\n%s", want, md)
		}
	}
}
