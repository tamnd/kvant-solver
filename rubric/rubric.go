// Package rubric turns the standing section headings the magazine prints into
// stable names.
//
// The magazine runs the same sections for decades and prints their banners
// however the typesetter felt that month. Задачник «Кванта» appears with
// guillemets, with straight quotes, with no quotes at all, in capitals across
// the top of a page, and hyphenated across a line break. All five are one
// section, and a corpus that treats them as five has no section index at all.
//
// So a banner goes through here once, on its way from a page file into an
// article's front matter, and comes out as a slug. Everything downstream, the
// index, the tags, the site, joins on that slug and never on the printed text.
// The printed text is kept as well, because it is what the page says and a
// corpus that throws that away cannot be checked against the scan.
package rubric

import (
	"sort"
	"strings"
	"unicode"
)

// Rubric is one standing section.
type Rubric struct {
	// Slug is the stable name. It never changes once it is in the corpus.
	Slug string
	// Title is the section as it is usually printed, for a heading and a report.
	Title string
	// Known is false for a banner that matched nothing in the table. The slug is
	// still derived and still stable, so an unknown rubric is usable
	// immediately; the flag is what the audit counts so that a section the
	// magazine started in 1983 is noticed rather than accumulating quietly.
	Known bool
	// Group is true for a heading that sorts a contents page rather than
	// standing over an article. See groups.
	Group bool
}

// The table. These are the sections that carry across issues, which is what
// makes a slug worth pinning: a heading that appears once is an article title
// and is handled as one.
//
// Aliases are the spellings actually seen in the scans, after the case and the
// quotes have been normalised away. A spelling is added here when a page prints
// it, not in anticipation.
var table = []struct {
	slug    string
	title   string
	aliases []string
}{
	{"zadachnik-kvanta", "Задачник «Кванта»", []string{
		"задачник кванта", "задачник", "задачник кванта задачи м и ф",
	}},
	{"kvant-dlya-mladshih-shkolnikov", "Квант для младших школьников", []string{
		"квант для младших школьников", "для младших школьников",
	}},
	{"praktikum-abiturienta", "Практикум абитуриента", []string{
		"практикум абитуриента", "абитуриенту",
	}},
	{"matematicheskiy-kruzhok", "Математический кружок", []string{
		"математический кружок",
	}},
	{"fizicheskiy-fakultativ", "Физический факультатив", []string{
		"физический факультатив",
	}},
	{"laboratoriya-kvanta", "Лаборатория «Кванта»", []string{
		"лаборатория кванта", "лаборатория",
	}},
	{"kaleydoskop-kvanta", "Калейдоскоп «Кванта»", []string{
		"калейдоскоп кванта", "калейдоскоп",
	}},
	{"nashi-nablyudeniya", "Наши наблюдения", []string{
		"наши наблюдения",
	}},
	{"shahmatnaya-stranichka", "Шахматная страничка", []string{
		"шахматная страничка", "шахматы",
	}},
	{"otvety-ukazaniya-resheniya", "Ответы, указания, решения", []string{
		"ответы указания решения", "ответы и решения", "решения задач",
	}},
	{"novosti-nauki", "Новости науки", []string{
		"новости науки",
	}},
	{"informatsiya", "Информация", []string{
		"информация", "информация объявления",
	}},
	{"retsenzii-bibliografiya", "Рецензии, библиография", []string{
		"рецензии библиография", "рецензии", "книжная полка", "новые книги",
	}},
	{"zadachi-nashih-chitateley", "Задачи наших читателей", []string{
		"задачи наших читателей",
	}},
	{"iz-istorii-nauki", "Из истории науки", []string{
		"из истории науки", "страницы истории",
	}},
	{"smes", "Смесь", []string{
		"смесь",
	}},
	// From here down are the sections of 1990 to 2006, which is where the
	// magazine reorganised itself. Задачник and Практикум ran throughout, but
	// Физический факультатив stops and comes back in 1996, Школа в «Кванте»
	// takes over most of what the older teaching sections did, and four
	// sections start that had no Soviet equivalent at all.
	{"shkola-v-kvante", "Школа в «Кванте»", []string{
		"школа в кванте",
	}},
	{"olimpiady", "Олимпиады", []string{
		"олимпиады", "олимпиада",
	}},
	{"ekzamenatsionnye-materialy", "Экзаменационные материалы", []string{
		"экзаменационные материалы", "варианты вступительных экзаменов",
	}},
	{"igry-i-golovolomki", "Игры и головоломки", []string{
		"игры и головоломки",
	}},
	// Named for Анатолий Павлович Савин, who edited the magazine's mathematics
	// and ran this competition for younger readers from 1990.
	{"konkurs-imeni-a-p-savina", "Конкурс имени А. П. Савина", []string{
		"конкурс имени а п савина", "конкурс имени а п савина математика 6 8",
	}},
	{"kvant-ulybaetsya", "«Квант» улыбается", []string{
		"квант улыбается",
	}},
	{"nam-pishut", "Нам пишут", []string{
		"нам пишут",
	}},
	{"o-lyudyah", "О людях", []string{
		"о людях",
	}},
	{"matematicheskiy-mir", "Математический мир", []string{
		"математический мир",
	}},
	{"ugolok-kollektsionera", "Уголок коллекционера", []string{
		"уголок коллекционера",
	}},
	{"po-stranitsam-shkolnyh-uchebnikov", "По страницам школьных учебников", []string{
		"по страницам школьных учебников",
	}},
}

// groups are the two headings the archive's own contents sorts every item
// under, and they are not sections of the magazine.
//
// Основные статьи and Разное stand over about half the rows of the whole
// archive, from 1970 to now, and no issue of Kvant has ever printed either of
// them over an article. They are the site's buckets: the pieces it treats as
// the issue's articles, and everything else. Открывающие статьи is the same
// thing for the piece that opens an issue.
//
// They resolve like any other heading, so a row carrying one still gets a
// stable slug, and they come back with Group set so that the section index can
// leave them out. Counting them as sections would put the two largest entries
// in the taxonomy on things nobody ever printed.
var groups = []struct {
	slug    string
	title   string
	aliases []string
}{
	{"osnovnye-stati", "Основные статьи", []string{"основные статьи"}},
	{"raznoe", "Разное", []string{"разное"}},
	{"otkryvayushchie-stati", "Открывающие статьи", []string{"открывающие статьи"}},
}

// index is the aliases flattened, built once.
var index = func() map[string]Rubric {
	m := map[string]Rubric{}
	for _, row := range table {
		known := Rubric{Slug: row.slug, Title: row.title, Known: true}
		m[key(row.title)] = known
		for _, alias := range row.aliases {
			m[key(alias)] = known
		}
	}
	for _, row := range groups {
		bucket := Rubric{Slug: row.slug, Title: row.title, Known: true, Group: true}
		m[key(row.title)] = bucket
		for _, alias := range row.aliases {
			m[key(alias)] = bucket
		}
	}
	return m
}()

// Canonical resolves a printed banner.
//
// It always returns a rubric. A banner nobody has seen before is not an error,
// because the magazine did start new sections and a run of 34000 pages should
// not stop at the first one; it comes back with Known false and a slug derived
// from what was printed.
func Canonical(printed string) Rubric {
	trimmed := strings.TrimSpace(printed)
	if trimmed == "" {
		return Rubric{}
	}
	if found, ok := index[key(trimmed)]; ok {
		return found
	}
	return Rubric{Slug: Slug(trimmed), Title: title(trimmed)}
}

// Known says whether a banner is one of the standing sections.
func Known(printed string) bool { return Canonical(printed).Known }

// All lists the table, by slug, for the index page and for a test that wants to
// assert the whole set at once.
func All() []Rubric {
	out := make([]Rubric, 0, len(table))
	for _, row := range table {
		out = append(out, Rubric{Slug: row.slug, Title: row.title, Known: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// key is what two spellings of the same banner have in common.
//
// Case goes, because a banner set in capitals across the top of a page is the
// same section. Punctuation goes, because the guillemets, the straight quotes
// and the comma between Ответы and решения all move between issues. The soft
// hyphen goes, because a banner broken across a line carries one and it is
// invisible in a diff. What is left is the words, and the words are what
// identify the section.
func key(printed string) string {
	var out strings.Builder
	space := true
	for _, r := range strings.ToLower(printed) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			// ё folds to е. The magazine prints its own section titles both
			// ways in the same year, and Всё and Все are not two sections.
			if r == 'ё' {
				r = 'е'
			}
			out.WriteRune(r)
			space = false
		case r == '­' || r == '​':
			// A soft hyphen or a zero width space joins the halves of a word
			// that was broken across a line, so it separates nothing.
		default:
			if !space {
				out.WriteByte(' ')
				space = true
			}
		}
	}
	return strings.TrimSpace(out.String())
}

// title tidies a banner for display. A section set in capitals is printed that
// way for the typography and not because the words are capitals, so it comes
// back in sentence case; a banner that was already mixed case is left alone,
// because it carries proper nouns we should not be guessing at.
func title(printed string) string {
	trimmed := strings.Join(strings.Fields(printed), " ")
	if !allCaps(trimmed) {
		return trimmed
	}
	lowered := []rune(strings.ToLower(trimmed))
	for i, r := range lowered {
		if unicode.IsLetter(r) {
			lowered[i] = unicode.ToUpper(r)
			break
		}
	}
	return string(lowered)
}

func allCaps(text string) bool {
	letters := 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		if unicode.IsLower(r) {
			return false
		}
		letters++
	}
	return letters > 0
}
