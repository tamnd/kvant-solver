package rubric_test

import (
	"testing"

	"github.com/tamnd/kvant-solver/rubric"
)

// The five spellings of one banner that the 1975 scans actually print. All of
// them are the same section, and a corpus that says otherwise has no section
// index.
func TestOneSectionHoweverItIsPrinted(t *testing.T) {
	for _, printed := range []string{
		"Задачник «Кванта»",
		`Задачник "Кванта"`,
		"ЗАДАЧНИК «КВАНТА»",
		"Задачник  «Кванта»",
		"Задачник «Кванта»  ",
	} {
		got := rubric.Canonical(printed)
		if got.Slug != "zadachnik-kvanta" {
			t.Errorf("%q resolved to %q, want zadachnik-kvanta", printed, got.Slug)
		}
		if !got.Known {
			t.Errorf("%q was not recognised as a standing section", printed)
		}
	}
}

// A banner broken across a line carries a soft hyphen, which is invisible in a
// diff and would otherwise split one section into two.
func TestABannerBrokenAcrossALine(t *testing.T) {
	got := rubric.Canonical("Практикум абиту­риента")
	if got.Slug != "praktikum-abiturienta" {
		t.Fatalf("resolved to %q, want praktikum-abiturienta", got.Slug)
	}
}

// A section nobody has seen before is not an error. The magazine started new
// ones and a run of 34000 pages must not stop at the first.
func TestAnUnknownBannerStillGetsAStableSlug(t *testing.T) {
	got := rubric.Canonical("Компьютерный класс")
	if got.Known {
		t.Fatal("an invented section came back as a standing one")
	}
	if got.Slug != "kompyuternyy-klass" {
		t.Fatalf("slug is %q, want kompyuternyy-klass", got.Slug)
	}
	again := rubric.Canonical("КОМПЬЮТЕРНЫЙ КЛАСС")
	if again.Slug != got.Slug {
		t.Fatalf("the same section in capitals slugged to %q and %q", got.Slug, again.Slug)
	}
	if again.Title != "Компьютерный класс" {
		t.Fatalf("title is %q, want it back in sentence case", again.Title)
	}
}

func TestSlugsAreFilenames(t *testing.T) {
	cases := map[string]string{
		"Задачник «Кванта»":            "zadachnik-kvanta",
		"Квант для младших школьников": "kvant-dlya-mladshih-shkolnikov",
		"Ответы, указания, решения":    "otvety-ukazaniya-resheniya",
		"Шахматная страничка":          "shahmatnaya-stranichka",
		"Лаборатория «Кванта»":         "laboratoriya-kvanta",
		"Физический факультатив":       "fizicheskiy-fakultativ",
		"Наши наблюдения":              "nashi-nablyudeniya",
		"Смесь":                        "smes",
		"Задачи М1234, М1235 и М1236":  "zadachi-m1234-m1235-i-m1236",
		"  Из  истории   науки  ":      "iz-istorii-nauki",
		"Что такое ъ и ь?":             "chto-takoe-i",
		"Квант — 50 лет":               "kvant-50-let",
		"«Квант» и «Наука и жизнь»":    "kvant-i-nauka-i-zhizn",
		"Формула $E=mc^2$ на обложке":  "formula-e-mc-2-na-oblozhke",
		"Год 1975": "god-1975",
	}
	for printed, want := range cases {
		if got := rubric.Slug(printed); got != want {
			t.Errorf("Slug(%q) = %q, want %q", printed, got, want)
		}
	}
}

// Two sections must never share a slug, or the index silently merges them.
func TestSlugsAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, r := range rubric.All() {
		if other, ok := seen[r.Slug]; ok {
			t.Errorf("%q and %q both slug to %q", other, r.Title, r.Slug)
		}
		seen[r.Slug] = r.Title
	}
	if len(seen) < 15 {
		t.Fatalf("the table holds %d sections, which is fewer than the magazine runs", len(seen))
	}
}

// Every title in the table has to resolve to its own slug, otherwise the table
// disagrees with itself and a page transcribed exactly as printed misses.
func TestTheTableResolvesItsOwnTitles(t *testing.T) {
	for _, r := range rubric.All() {
		got := rubric.Canonical(r.Title)
		if got.Slug != r.Slug || !got.Known {
			t.Errorf("%q resolved to %q (known %v), want %q", r.Title, got.Slug, got.Known, r.Slug)
		}
	}
}

func TestAnEmptyBannerIsNothing(t *testing.T) {
	if got := rubric.Canonical("   "); got.Slug != "" || got.Known {
		t.Fatalf("blank resolved to %+v, want the zero rubric", got)
	}
}
