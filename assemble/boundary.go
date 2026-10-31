package assemble

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/rubric"
)

// The magazine starts a piece wherever the last one stopped, not at the top of
// a page: a problem set ends a third of the way down and the next article's
// heading follows under it. The contents only knows pages, so a page split
// alone hands the tail of one piece to the next, which then opens on somebody
// else's problems 5 to 8.
//
// The heading is what marks the real start, so it is looked for on the first
// page and the page is cut there. The contents' title is typed and the page's is
// read, and the reading is wrong in small ways, a ъ read as ы, a line break in
// the middle of a word, so the match is on letters only and forgives an edit
// per six of them.

// headingLine is the longest a line of a heading runs. Titles are set large and
// broken over several short lines; a line of body text is longer than this.
const headingLine = 70

// headingLines is how many lines a heading is allowed to be broken over.
const headingLines = 6

// minHead is how much text has to come before the heading for the cut to be
// worth making. Less than this is a running head or a stray caption, which
// does no harm staying where it is.
const minHead = 80

// cutAt returns the byte offset in body where the article titled title starts,
// or 0 when the page opens with it, does not show it, or shows too little
// before it to be anybody's tail.
func cutAt(body, title string, authors []string) int {
	// A title in brackets is the editors' name for an untitled piece, and
	// nothing on the page spells it.
	if strings.HasPrefix(strings.TrimSpace(title), "[") {
		return 0
	}
	want := letters(title)
	if utf8.RuneCountInString(want) < 4 {
		return 0
	}
	lines := strings.SplitAfter(body, "\n")
	offsets := make([]int, len(lines))
	at := 0
	for i, line := range lines {
		offsets[i] = at
		at += len(line)
	}
	for i := range lines {
		if !headingish(lines[i]) {
			continue
		}
		if !titled(lines[i:], want) {
			continue
		}
		start := i
		// The byline and the section banner sit just above the heading and
		// belong to the article, not to whatever came before it.
		for taken := 0; start > 0 && taken < 4; {
			prev := strings.TrimSpace(lines[start-1])
			if prev == "" {
				start--
				continue
			}
			if strings.HasPrefix(prev, ocr.RubricMarker) || byline(prev, authors) || initials.MatchString(prev) || banner(prev) {
				start--
				taken++
				continue
			}
			break
		}
		cut := offsets[start]
		if utf8.RuneCountInString(letters(ocr.StripMarkers(body[:cut]))) < minHead {
			return 0
		}
		return cut
	}
	return 0
}

// headingish says whether a line could be part of a heading. The reading puts
// a whole paragraph on one line, so a short line is a heading, a byline, a
// caption or the last words of a paragraph, and only the first of those spells
// the title.
func headingish(line string) bool {
	line = strings.TrimSpace(line)
	return line != "" && !strings.HasPrefix(line, "⟦") && utf8.RuneCountInString(line) <= headingLine
}

// titled says whether the heading lines starting here spell want.
func titled(lines []string, want string) bool {
	n := utf8.RuneCountInString(want)
	slack := max(1, n/6)
	have := ""
	for k, taken := 0, 0; k < len(lines) && taken < headingLines; k++ {
		// A heading set as two blocks, the rubric word and the numbers under
		// it, comes back with a blank line between them.
		if strings.TrimSpace(lines[k]) == "" && k > 0 {
			continue
		}
		if !headingish(lines[k]) {
			break
		}
		taken++
		have += letters(lines[k])
		if near(have, want, slack) {
			return true
		}
		if utf8.RuneCountInString(have) > n+slack {
			break
		}
	}
	return false
}

// byline says whether a short line names one of the authors.
func byline(line string, authors []string) bool {
	if utf8.RuneCountInString(line) > headingLine {
		return false
	}
	have := letters(line)
	for _, author := range authors {
		fields := strings.Fields(author)
		if len(fields) == 0 {
			continue
		}
		surname := []rune(letters(fields[len(fields)-1]))
		if len(surname) < 3 {
			continue
		}
		// The stem, so that a byline in another case still matches.
		stem := string(surname[:min(len(surname), 5)])
		if strings.Contains(have, stem) {
			return true
		}
	}
	return false
}

// lookalike folds the Latin letters the reading puts in Russian words, an M
// for an М in a problem number most often, onto the Cyrillic ones.
var lookalike = map[rune]rune{
	'a': 'а', 'b': 'в', 'c': 'с', 'e': 'е', 'h': 'н', 'k': 'к', 'm': 'м',
	'o': 'о', 'p': 'р', 't': 'т', 'x': 'х', 'y': 'у', 'ё': 'е',
}

// letters lowercases s and keeps only its letters and digits, with ё as е and
// Latin lookalikes as Cyrillic, which is the part of a title that survives
// being typeset and read back.
func letters(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if c, ok := lookalike[r]; ok {
			r = c
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// near says whether a and b are within slack edits of each other.
func near(a, b string, slack int) bool {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d > slack || -d > slack {
		return false
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			best = min(best, cur[j])
		}
		if best > slack {
			return false
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)] <= slack
}

// initials is a byline broken before the surname, "В. И." on a line of its own.
var initials = regexp.MustCompile(`^(\p{Lu}\.\s*){1,3}$`)

// banner says whether a line is a section banner the reading did not mark, or
// one of the small kickers the magazine sets over a piece.
func banner(line string) bool {
	if rubric.Known(line) {
		return true
	}
	switch letters(line) {
	case "информация", "нашкалендарь", "уголокколлекционера", "наблюденияиэксперименты", "калейдоскоп", "калейдоскопкванта":
		return true
	}
	return false
}
