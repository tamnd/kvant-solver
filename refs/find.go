// Package refs turns the pointers Kvant articles make at each other into a
// graph.
//
// The magazine cross references itself constantly and does it in prose. There
// is no citation format and no marker to look for: an article says see «Квант»,
// 1970, № 9, с. 32, or refers to problem М381, or names an issue and leaves the
// reader to find the page. So resolving a reference is a matching problem
// against the printed contents rather than a parse, and it is worth being
// explicit that it will not resolve all of them. What matters is that the ones
// it does resolve are right and the ones it does not are listed.
//
// Two things are looked for and nothing else. A pointer at an issue, which is
// the journal's name followed by a number and usually a year and sometimes a
// page, and a pointer at a problem, which is an M or an F and three or four
// digits. A bare year with no issue number is not a pointer at an object and is
// left alone: «Квант», 1975 год means the volume, and there is nothing to link
// it to.
package refs

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Kind is what a citation points at.
type Kind string

const (
	// KindIssue is a whole issue, as «Квант», 1974, № 5.
	KindIssue Kind = "issue"
	// KindPage is an issue and a printed page, which is a pointer at one
	// article rather than at the issue, as «Квант», 1970, № 9, с. 32.
	KindPage Kind = "page"
	// KindProblem is a problem by its printed number, as М381.
	KindProblem Kind = "problem"
)

// Citation is one pointer found in a body, before anything is looked up.
type Citation struct {
	Kind Kind
	// Text is what was matched, so that a reference nobody can resolve can
	// still be read in the report and judged by a person.
	Text string
	// Year is the year printed in the citation, or zero. The magazine leaves it
	// out when it means the current volume, so a caller resolves a zero against
	// the year of the article doing the citing.
	Year   int
	Number string
	// Page is the printed page, or zero.
	Page int
	// Problem is the number, normalised to Latin M or F and no leading zeros.
	Problem string
	// Offset is where in the body the citation starts, which orders them and
	// tells two identical citations in one article apart.
	Offset int
}

// Journal is the name as the magazine prints it when citing itself, in the
// angle quotes it always carries.
const Journal = "«Квант»"

var (
	yearRe   = regexp.MustCompile(`^(19|20)\d{2}`)
	numberRe = regexp.MustCompile(`^№\s*№?\s*(\d{1,2})`)
	// Both alphabets, because с and c are the same letter to a reader and not
	// to a program, and OCR of a Russian page produces either.
	pageRe = regexp.MustCompile(`^[сcCС]\.\s*(\d{1,3})`)
	andRe  = regexp.MustCompile(`^и\s+(\d{1,2})`)
	// The words that hold a citation together and mean nothing on their own.
	filler = map[string]bool{"за": true, "г": true, "гг": true, "год": true, "года": true, "году": true, "№": true}
	// A problem number as printed. Both alphabets again: М and M look the same
	// on paper and the reading of a scan mixes them freely.
	problemRe = regexp.MustCompile(`([МMФF])\s?(\d{2,4})`)
)

// Find returns every citation in a body, in the order they appear.
func Find(body string) []Citation {
	var out []Citation
	out = append(out, findJournal(body)...)
	out = append(out, findProblems(body)...)
	sortByOffset(out)
	return out
}

// findJournal walks the tail of every «Квант» in the body.
//
// The tail is read token by token rather than by matching a pattern against a
// window of fixed length, because the window catches things that are not part
// of the citation. In one 1975 article the sentence continues «Квант» № 1, 1975)
// допущена опечатка: в условиях задачи № 1, and a window wide enough to hold a
// year and a page also holds that second № 1, which belongs to a problem in
// another issue entirely. Stopping at the first token that is not part of a
// citation gets it right.
func findJournal(body string) []Citation {
	var out []Citation
	for at := 0; ; {
		i := strings.Index(body[at:], Journal)
		if i < 0 {
			return out
		}
		start := at + i
		at = start + len(Journal)
		out = append(out, readTail(body, start, at)...)
	}
}

// readTail reads the year, issue numbers and page that follow the journal's
// name, and returns one citation per issue number.
func readTail(body string, start, at int) []Citation {
	year, page := 0, 0
	var numbers []string
	pos, end := at, at
	for pos < len(body) {
		skip := len(body[pos:]) - len(strings.TrimLeft(body[pos:], " \t,"))
		if skip == 0 && pos > at {
			// Nothing between the last token and this one, so this is not a
			// continuation of the citation.
			break
		}
		pos += skip
		rest := body[pos:]
		switch {
		case yearRe.MatchString(rest):
			m := yearRe.FindString(rest)
			year, _ = strconv.Atoi(m)
			pos += len(m)
			end = pos
		case numberRe.MatchString(rest):
			m := numberRe.FindStringSubmatch(rest)
			numbers = append(numbers, strings.TrimLeft(m[1], "0"))
			pos += len(m[0])
			end = pos
		case len(numbers) > 0 && andRe.MatchString(rest):
			// №№ 1 и 2, which is two issues and not a range.
			m := andRe.FindStringSubmatch(rest)
			numbers = append(numbers, strings.TrimLeft(m[1], "0"))
			pos += len(m[0])
			end = pos
		case pageRe.MatchString(rest):
			m := pageRe.FindStringSubmatch(rest)
			page, _ = strconv.Atoi(m[1])
			pos += len(m[0])
			end = pos
		default:
			word := leadingWord(rest)
			if word == "" || !filler[strings.ToLower(strings.TrimSuffix(word, "."))] {
				pos = len(body)
				break
			}
			pos += len(word)
			pos += len(body[pos:]) - len(strings.TrimPrefix(body[pos:], "."))
		}
	}
	if len(numbers) == 0 {
		// A year on its own points at a volume and not at anything citable, and
		// so does the journal's name in the middle of a sentence.
		return nil
	}
	text := strings.TrimRight(strings.TrimSpace(body[start:end]), ",. ")
	kind := KindIssue
	if page > 0 {
		kind = KindPage
	}
	out := make([]Citation, 0, len(numbers))
	for _, number := range numbers {
		out = append(out, Citation{
			Kind: kind, Text: text, Year: year, Number: number, Page: page, Offset: start,
		})
	}
	// A page belongs to one issue. Where the citation named two, as №№ 1 и 2,
	// the page is not about either of them in particular and saying nothing is
	// better than guessing.
	if len(out) > 1 {
		for i := range out {
			out[i].Kind, out[i].Page = KindIssue, 0
		}
	}
	return out
}

// leadingWord is the run of letters at the front of a string, which is how the
// tail reader decides whether what it is looking at holds the citation together
// or ends it.
func leadingWord(s string) string {
	for i, r := range s {
		if !unicode.IsLetter(r) {
			return s[:i]
		}
	}
	return s
}

// findProblems picks the M and F numbers out of a body.
//
// The letter has to be a word on its own, so that the М of а М381 is a citation
// and the М inside a word is not. Two digits is the floor because the magazine
// started numbering at M1 and had reached the hundreds by 1970, and a single
// digit next to a letter is far more often a variable than a problem.
func findProblems(body string) []Citation {
	var out []Citation
	for _, m := range problemRe.FindAllStringSubmatchIndex(body, -1) {
		start := m[2]
		if start > 0 && isWordRune(before(body, start)) {
			continue
		}
		letter := "M"
		if body[m[2]:m[3]] == "Ф" || body[m[2]:m[3]] == "F" {
			letter = "F"
		}
		number := strings.TrimLeft(body[m[4]:m[5]], "0")
		if number == "" {
			continue
		}
		out = append(out, Citation{
			Kind: KindProblem, Text: body[m[0]:m[1]],
			Problem: letter + number, Offset: m[0],
		})
	}
	return out
}

func before(body string, at int) rune {
	for i := at - 1; i >= 0 && i > at-5; i-- {
		if r := rune(body[i]); r < 0x80 {
			return r
		}
		if body[i]&0xC0 != 0x80 {
			return []rune(body[i:at])[0]
		}
	}
	return ' '
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func sortByOffset(list []Citation) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Offset < list[j].Offset })
}
