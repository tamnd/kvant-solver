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
	got := rubric.Canonical("Практикум абиту\u00adриента")
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

// The sections of 1990 to 2006. Every one of these stands over rows of the
// contents of that period, and before this milestone every one of them
// resolved as unknown, which is the audit saying the taxonomy stopped in 1989.
func TestTheSectionsOfTheNineties(t *testing.T) {
	cases := map[string]string{
		"Школа в «Кванте»":                "shkola-v-kvante",
		"Олимпиады":                       "olimpiady",
		"Экзаменационные материалы":       "ekzamenatsionnye-materialy",
		"Игры и головоломки":              "igry-i-golovolomki",
		"Конкурс имени А. П. Савина":      "konkurs-imeni-a-p-savina",
		"«Квант» улыбается":               "kvant-ulybaetsya",
		"Нам пишут":                       "nam-pishut",
		"О людях":                         "o-lyudyah",
		"Математический мир":              "matematicheskiy-mir",
		"Уголок коллекционера":            "ugolok-kollektsionera",
		"По страницам школьных учебников": "po-stranitsam-shkolnyh-uchebnikov",
	}
	for printed, want := range cases {
		got := rubric.Canonical(printed)
		if got.Slug != want || !got.Known {
			t.Errorf("%q resolved to %q (known %v), want %q", printed, got.Slug, got.Known, want)
		}
		if got.Group {
			t.Errorf("%q came back as a contents bucket", printed)
		}
	}
}

// The renames. The archive's contents calls two sections by shorter names than
// the banners the pages print, and they are one section each.
func TestTheShortenedNamesAreTheSameSections(t *testing.T) {
	for _, pair := range [][2]string{
		{"Шахматы", "Шахматная страничка"},
		{"Калейдоскоп", "Калейдоскоп «Кванта»"},
		{"«Квант» для младших школьников", "Квант для младших школьников"},
		{"Лаборатория", "Лаборатория «Кванта»"},
	} {
		short, long := rubric.Canonical(pair[0]), rubric.Canonical(pair[1])
		if short.Slug != long.Slug {
			t.Errorf("%q is %q and %q is %q, want one section", pair[0], short.Slug, pair[1], long.Slug)
		}
	}
}

// Основные статьи and Разное stand over about half the rows in the archive and
// over no article the magazine ever printed. They are the site's own buckets,
// and a section index that ranks them first is ranking a filing decision.
func TestTheContentsBucketsAreNotSections(t *testing.T) {
	for _, printed := range []string{"Основные статьи", "Разное", "Открывающие статьи"} {
		got := rubric.Canonical(printed)
		if !got.Group {
			t.Errorf("%q came back as a section of the magazine", printed)
		}
		if !got.Known || got.Slug == "" {
			t.Errorf("%q resolved to %+v, want a known bucket with a slug", printed, got)
		}
	}
	// And they are not in the section table, which is what the index is built
	// from.
	for _, r := range rubric.All() {
		if r.Slug == "raznoe" || r.Slug == "osnovnye-stati" {
			t.Errorf("%q is in the section table", r.Title)
		}
	}
}

func TestAnEmptyBannerIsNothing(t *testing.T) {
	if got := rubric.Canonical("   "); got.Slug != "" || got.Known {
		t.Fatalf("blank resolved to %+v, want the zero rubric", got)
	}
}
