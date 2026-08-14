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
