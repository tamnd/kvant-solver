package problems

import (
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

// list is a reader list article, the way the magazine set one.
func list(body string) Article {
	return Article{
		Path: "content/ru/1975/11/articles/13_spisok.md",
		Front: corpus.ArticleFront{
			Issue: "kvant_1975_11", Year: 1975, Number: "11",
			Title:  "Список читателей, приславших правильные решения",
			Rubric: "zadachnik-kvanta",
		},
		Body: body,
	}
}

const twoSeries = `В этом номере мы публикуем фамилии тех, кто прислал правильные решения задач М306—М315 и Ф318—Ф327 (жирные цифры после фамилии — последние цифры номеров решенных задач).

**Математика**

В большинстве писем содержалось верное решение задачи **М306**. Остальные задачи решили: *Д. Азов* (Челябинск) **1—5**; *Г. Алексанян* (Степанакерт) **9, 1**; *Б. Соломяк* (Ленинград) **7—0**.

**Физика**

Почти все читатели, приславшие решения задач **Ф318—Ф327**, справились с задачами **Ф320** и **Ф322**. Остальные задачи правильно решили: *Х. Абдуллин* (Алма-Ата) **9, 5, 6**; *М. Агеев* (Тула) **7, 8, 1**.
`

func TestTheReaderListIsCounted(t *testing.T) {
	c, ok := Credits(list(twoSeries))
	if !ok {
		t.Fatal("the list was not read at all")
	}
	if c.Readers != 5 {
		t.Fatalf("counted %d readers, want the 5 named", c.Readers)
	}
	// Азов solved М311 to М315, Алексанян М309 and М311, Соломяк М307 to М310.
	want := map[string]int{
		"M307": 1, "M308": 1, "M309": 2, "M310": 1,
		"M311": 2, "M312": 1, "M313": 1, "M314": 1, "M315": 1,
		"F318": 1, "F319": 1, "F321": 1, "F325": 1, "F326": 1, "F327": 1,
	}
	for id, n := range want {
		if c.Counts[id] != n {
			t.Fatalf("%s credited to %d readers, want %d: %v", id, c.Counts[id], n, c.Counts)
		}
	}
	if len(c.Counts) != len(want) {
		t.Fatalf("credited %d problems, want %d: %v", len(c.Counts), len(want), c.Counts)
	}
	if c.Unread != 0 {
		t.Fatalf("%d printed numbers could not be read", c.Unread)
	}
}

func TestARangeThatWrapsRoundTheDecadeIsRead(t *testing.T) {
	// The magazine prints only the last digit, so 7—0 in a list covering
	// М306—М315 is 307 to 310 and not an empty range or seven to zero.
	c, _ := Credits(list(twoSeries))
	for _, id := range []string{"M307", "M308", "M309", "M310"} {
		if c.Counts[id] == 0 {
			t.Fatalf("%s got no credit from the wrapped range: %v", id, c.Counts)
		}
	}
	if c.Counts["M305"] != 0 || c.Counts["M316"] != 0 {
		t.Fatalf("the range ran outside the span it was announced in: %v", c.Counts)
	}
}

func TestTheProblemsNobodyWasNamedForAreNotConfused(t *testing.T) {
	// М306, Ф320 and Ф322 carry no names because nearly everyone solved them,
	// and that is the opposite of what a count of zero usually means. The
	// sentence that says so also restates the whole span, and reading the span
	// out of it would mark every problem in the issue as easy.
	c, _ := Credits(list(twoSeries))
	want := map[string]bool{"M306": true, "F320": true, "F322": true}
	if len(c.Widely) != len(want) {
		t.Fatalf("read %v as widely solved, want %v", c.Widely, want)
	}
	for _, id := range c.Widely {
		if !want[id] {
			t.Fatalf("read %v as widely solved, want %v", c.Widely, want)
		}
	}
}

func TestTheSeriesIsFollowedWithoutAHeading(t *testing.T) {
	// Half the issues print no Математика and Физика headings and open each
	// series with a sentence naming its problems in full. Without following
	// that, every physics reader in those issues is credited to a mathematics
	// problem, since one printed digit fits both spans.
	c, ok := Credits(list(`В этом номере мы публикуем фамилии тех, кто прислал правильные решения задач М276 — М285; Ф293 — Ф302. Жирные цифры после фамилии — последние цифры номера решенной задачи.

Большинство читателей справилось с задачами М276, М281, М282. Остальные задачи решили: Д. Азов (Челябинск) 7, 9; В. Басманов (Воронеж) 7.

Почти все читатели справились с задачами Ф293, Ф297 и Ф302. Правильные решения остальных задач прислали: Х. Абдулин (Алма-Ата) 5; Г. Айзин (Брест) 4, 9, 0.
`))
	if !ok {
		t.Fatal("the list was not read at all")
	}
	if c.Counts["M277"] != 2 || c.Counts["M279"] != 1 {
		t.Fatalf("the mathematics half came out %v", c.Counts)
	}
	if c.Counts["F295"] != 1 || c.Counts["F294"] != 1 || c.Counts["F299"] != 1 || c.Counts["F300"] != 1 {
		t.Fatalf("the physics half came out %v", c.Counts)
	}
	if c.Counts["M275"] != 0 || c.Counts["F297"] != 0 {
		t.Fatalf("a number was matched outside the series it was printed under: %v", c.Counts)
	}
}

func TestANumberThatCouldBeTwoProblemsIsDroppedAndCounted(t *testing.T) {
	// One digit against two spans that share their last digits is not readable,
	// and filing it under either would invent a measurement. It is counted so
	// that a page the reading lane made a mess of does not pass for a page
	// nobody in the country solved.
	c, ok := Credits(list(`Правильные решения задач М276—М285 и Ф293—Ф302 прислали: Д. Азов (Челябинск) 7; В. Басманов (Воронеж) 9.
`))
	if !ok {
		t.Fatal("the list was not read at all")
	}
	if len(c.Counts) != 0 {
		t.Fatalf("resolved an ambiguous number: %v", c.Counts)
	}
	if c.Unread != 2 {
		t.Fatalf("%d numbers reported unread, want 2", c.Unread)
	}
}

func TestTheSpanComesFromTheListAndNotFromThePageAboveIt(t *testing.T) {
	// The assemble stage puts the list where it was printed, and in every issue
	// read so far a solution from the previous article runs over onto the same
	// page. Taking the first paragraph with a number in it produced a list that
	// covered that one problem and nothing else.
	c, ok := Credits(list(`⟦folio 51⟧

Ф299. По гибкому шлангу сечением S течет жидкость плотности ρ со скоростью v. Найти натяжение нити АВ.

Следовательно, ее натяжение равно нулю.

В этом номере мы приводим список читателей, приславших верные решения задач М286—М295 и Ф303—Ф312.

Математика

Задачи решили: Д. Азов (Челябинск) 6, 7.
`))
	if !ok {
		t.Fatal("the list was not read at all")
	}
	if len(c.Span) != 20 {
		t.Fatalf("the list says it covers %d problems, want 20: %v", len(c.Span), c.Span)
	}
	if c.Counts["M286"] != 1 || c.Counts["M287"] != 1 {
		t.Fatalf("counts came out %v", c.Counts)
	}
}

func TestAnArticleThatIsNotAListIsNotRead(t *testing.T) {
	art := list(twoSeries)
	art.Front.Title = "Задачи М306—М310; Ф318—Ф322"
	if _, ok := Credits(art); ok {
		t.Fatal("read a page of problems as a page of names")
	}
}

func TestTheCountsReachTheManifest(t *testing.T) {
	// Nil and zero are different answers. A problem the list covered and named
	// nobody for is the hardest thing in the issue, and one no list has been
	// read for is a page this corpus has not got to.
	res := Build([]Article{
		{Path: "posed.md", Body: "**М306.** Докажите.\n\n**М307.** Докажите.\n\n**М316.** Докажите.",
			Front: corpus.ArticleFront{Issue: "kvant_1975_1", Year: 1975, Number: "1",
				Title: "Задачи М306, М307, М316", Rubric: "zadachnik-kvanta"}},
		list(twoSeries),
	})
	got := map[string]*int{}
	widely := map[string]bool{}
	for _, e := range res.Manifest.Entries {
		got[e.ID] = e.Solvers
		widely[e.ID] = e.WidelySolved
	}
	if got["M307"] == nil || *got["M307"] != 1 {
		t.Fatalf("M307 solvers came out %v", got["M307"])
	}
	if got["M306"] == nil || *got["M306"] != 0 {
		t.Fatalf("M306 was in the span and named nobody, so it should be nought: %v", got["M306"])
	}
	if !widely["M306"] {
		t.Fatal("M306 lost the note that nearly everyone solved it")
	}
	if got["M316"] != nil {
		t.Fatalf("M316 is in no list that has been read and got a count of %v", *got["M316"])
	}
	if len(res.Credits) != 1 {
		t.Fatalf("the build recorded %d reader lists", len(res.Credits))
	}
}

func TestTheSummaryCountsTheListsAndWhatTheyMissed(t *testing.T) {
	c := CountCredits([]Credit{
		{Counts: map[string]int{"M301": 12, "M302": 4}, Widely: []string{"M303"}, Readers: 14, Unread: 3},
		{Counts: map[string]int{"M302": 1, "M310": 9}, Readers: 10},
	})
	if c.Lists != 2 || c.Readers != 24 || c.Unread != 3 || c.Widely != 1 {
		t.Fatalf("counted %+v", c)
	}
	// M302 is named in both lists and is one problem.
	if c.Problems != 3 {
		t.Fatalf("credited %d problems, want 3", c.Problems)
	}
}
