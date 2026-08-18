package problems

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// This file reads «Список читателей, приславших правильные решения», the page
// where the magazine named every reader who sent in a correct solution, town by
// town, with the numbers of the problems they solved after each name.
//
// It is the only difficulty measure the archive prints. Задачник «Кванта» never
// graded a problem and never marked one as hard, but it did publish, three or
// four issues later, that a hundred and twenty people solved М301 and eleven
// solved М304. That is a measurement taken on tens of thousands of Soviet
// schoolchildren, and nothing a model can be asked today replaces it.
//
// The page is set to save column inches, which is what makes the reading
// awkward. Only the last one or two digits of each number are printed, the
// width changes from issue to issue, ranges wrap round the end of a decade so
// that 7—0 means 307 to 310, and the problems nearly everybody solved are left
// out of the lists entirely and named in a sentence instead. Every one of those
// is handled here, and anything that cannot be read is counted rather than
// guessed at.

// Credit is one reader list, read.
type Credit struct {
	Issue   string `yaml:"issue"`
	Article string `yaml:"article"`
	// Span is every problem the opening sentence said the list covers. It is
	// kept because a problem inside the span with nobody named under it is a
	// measurement, and one outside it is a page this corpus has not read, and
	// the counts alone cannot tell those two apart.
	Span []string `yaml:"span,omitempty"`
	// Counts is how many readers were named for each problem, by identifier.
	Counts map[string]int `yaml:"counts,omitempty"`
	// Widely is the problems the magazine said nearly everyone got right, which
	// carry no names for exactly that reason. They are the easiest problems in
	// the span and they have to be told apart from the ones nobody solved, since
	// both come out of the lists with a count of zero.
	Widely []string `yaml:"widely,omitempty"`
	// Readers is how many names yielded at least one problem, and Unread is how
	// many printed numbers resolved to nothing. The second is the honesty of the
	// first: these pages are dense small print and the reading lane does not get
	// all of it.
	Readers int `yaml:"readers"`
	Unread  int `yaml:"unread,omitempty"`
}

// creditsTitle is the heading the magazine put on the list. The wording moved
// over the years and the reading lane does not always get the ending right, so
// the test is the two words that never changed.
var creditsTitle = regexp.MustCompile(`(?i)^\s*Список\s+читател`)

// IsCredits reports whether an article is one of the reader lists.
func IsCredits(title string) bool { return creditsTitle.MatchString(title) }

// sections are the two headings the list is split under when it covers both
// series. A list covering one series has no headings at all.
var (
	mathSection    = regexp.MustCompile(`(?m)^[ \t*_]*Математика[ \t*_]*$`)
	physicsSection = regexp.MustCompile(`(?m)^[ \t*_]*Физика[ \t*_]*$`)
)

// widelySolved matches the sentence that names the problems left out of the
// lists. The tail is taken from after the verb rather than from the whole
// sentence, because the sentence usually restates the span it is talking about
// and taking ids out of that would mark every problem in the issue as easy.
var widelySolved = regexp.MustCompile(`(?:справил\S*\s+с\s+задач\S*|верное\s+решение\s+задач\S*)\s*([^.]*)`)

// marker noise the assemble stage leaves in a body: folio and column breaks
// carry digits and would be read as problem numbers.
var pageMarker = regexp.MustCompile(`⟦[^⟧]*⟧`)

// token is one printed number, or a range of them.
var token = regexp.MustCompile(`(\d{1,4})\s*[а-яa-z]?\s*(?:[—–−-]\s*(\d{1,4})\s*[а-яa-z]?)?`)

// Credits reads a reader list, and reports whether the article was one.
//
// The announced span is the whole of the reading. Every printed number is
// matched against it by its last digits and kept only when exactly one problem
// in the span could have been meant, so a list whose opening sentence did not
// survive the scan yields nothing rather than a page of numbers attached to
// whichever problems they happened to look like.
func Credits(art Article) (*Credit, bool) {
	if !IsCredits(art.Front.Title) {
		return nil, false
	}
	body := pageMarker.ReplaceAllString(art.Body, " ")
	announced := announcement(body)
	if len(announced) == 0 {
		return nil, false
	}

	c := &Credit{Issue: art.Front.Issue, Article: art.Path, Counts: map[string]int{}}
	for _, id := range announced {
		c.Span = append(c.Span, id.String())
	}
	for _, part := range split(body, announced) {
		pool := narrow(announced, part.subject)
		if len(pool) == 0 {
			continue
		}
		for _, id := range idsIn(widely(part.text), pool) {
			c.Widely = appendOnce(c.Widely, id.String())
		}
		for chunk := range strings.SplitSeq(part.text, ";") {
			named, unread := solved(chunk, pool)
			c.Unread += unread
			if len(named) == 0 {
				continue
			}
			c.Readers++
			for _, id := range named {
				c.Counts[id.String()]++
			}
		}
	}
	sort.Strings(c.Widely)
	return c, true
}

// part is one stretch of the list and the series it credits.
type part struct {
	subject corpus.Subject
	text    string
}

// split works out which series each paragraph of the list is crediting.
//
// Knowing that matters more than it looks. Where the two series are announced
// together the printed numbers are usually one digit long, and one digit is
// ambiguous across two spans ten problems wide: a bare 5 in a list covering
// М276—М285 and Ф293—Ф302 is either М285 or Ф295 and there is nothing in the
// number to say which.
//
// A Математика or Физика heading settles it, and about half the issues have
// them. The rest open each series with a sentence naming its problems in full,
// «Почти все читатели справились с задачами Ф293, Ф297 и Ф302», so a paragraph
// whose full identifiers are all of one series switches the reading to that
// series and the paragraphs after it inherit that. A paragraph before anything
// has settled gets no series and its numbers are matched against everything
// announced, which resolves where the two spans have no last digits in common
// and drops the number where they do.
func split(body string, announced []corpus.ProblemID) []part {
	var parts []part
	current := corpus.Subject("")
	for para := range strings.SplitSeq(body, "\n\n") {
		switch {
		case mathSection.MatchString(para):
			current = corpus.Math
		case physicsSection.MatchString(para):
			current = corpus.Physics
		default:
			if s, ok := series(para, announced); ok {
				current = s
			}
		}
		parts = append(parts, part{current, para})
	}
	return parts
}

// series is the one series a paragraph names in full, if it names only one.
func series(para string, announced []corpus.ProblemID) (corpus.Subject, bool) {
	var found corpus.Subject
	for _, id := range idsIn(para, announced) {
		if found != "" && found != id.Subject {
			return "", false
		}
		found = id.Subject
	}
	return found, found != ""
}

// narrow is the problems of one series out of everything announced, or all of
// them where the list did not say.
func narrow(announced []corpus.ProblemID, subject corpus.Subject) []corpus.ProblemID {
	if subject == "" {
		return announced
	}
	var out []corpus.ProblemID
	for _, id := range announced {
		if id.Subject == subject {
			out = append(out, id)
		}
	}
	return out
}

// announcement is what the opening sentence says the list covers.
//
// It is the paragraph naming the most problems and not the first one naming
// any, because the assemble stage puts the list on the page where it was
// printed and the tail of the previous article is often above it. Every issue
// read so far opens with a solution to one problem running over from the page
// before, and taking the first paragraph with a number in it produced a list
// that claimed to cover that one problem and nothing else.
func announcement(body string) []corpus.ProblemID {
	var best []corpus.ProblemID
	for para := range strings.SplitSeq(body, "\n\n") {
		if !strings.Contains(para, "задач") {
			continue
		}
		if ids := Promised(para); len(ids) > len(best) {
			best = ids
		}
	}
	return best
}

// widely is the fragment of a sentence that names the problems left out of the
// lists, or the empty string.
func widely(text string) string {
	m := widelySolved.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// idsIn reads full identifiers out of a fragment and keeps the ones the list
// announced. The magazine writes these out in full, unlike the lists.
func idsIn(fragment string, pool []corpus.ProblemID) []corpus.ProblemID {
	var out []corpus.ProblemID
	for _, id := range Promised(fragment) {
		if slices.Contains(pool, id) {
			out = append(out, id)
		}
	}
	return out
}

// solved reads one reader's line: the problems credited to them, and how many
// printed numbers on it could not be resolved.
//
// A reader is a name, a town in brackets and then the numbers, and the numbers
// are taken from after the last closing bracket. A stretch of text with no
// bracket in it is not a reader and is passed over: the page carries the
// sentence announcing the span, and often the tail of the article above it, and
// both are full of problem numbers that would otherwise be counted as somebody
// having solved something. The cost is the occasional entry whose brackets the
// scan lost, which is a reader dropped rather than a reader invented.
func solved(chunk string, pool []corpus.ProblemID) ([]corpus.ProblemID, int) {
	i := strings.LastIndex(chunk, ")")
	if i < 0 {
		return nil, 0
	}
	tail := strings.Map(func(r rune) rune {
		if r == '*' || r == '_' || r == '`' {
			return -1
		}
		return r
	}, chunk[i+1:])

	var unread int
	seen := map[corpus.ProblemID]bool{}
	var out []corpus.ProblemID
	for _, m := range token.FindAllStringSubmatch(tail, -1) {
		first, ok := resolve(m[1], pool)
		if !ok {
			unread++
			continue
		}
		run := []corpus.ProblemID{first}
		if m[2] != "" {
			last, ok := resolve(m[2], pool)
			if !ok || last.Number < first.Number {
				unread++
				continue
			}
			run = between(first, last, pool)
		}
		for _, id := range run {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out, unread
}

// resolve turns the last digits of a number back into the problem the magazine
// meant, by matching against the span it announced.
//
// Exactly one match or none. Two problems in the span ending in the same digits
// means the printed number cannot be read, which happens when the scan lost the
// opening sentence and the span came out wider than it was. Filing it under
// either would invent a measurement.
func resolve(printed string, pool []corpus.ProblemID) (corpus.ProblemID, bool) {
	var found corpus.ProblemID
	var n int
	for _, id := range pool {
		if strings.HasSuffix(strconv.Itoa(id.Number), printed) {
			found, n = id, n+1
		}
	}
	return found, n == 1
}

// between is every problem of the span from one to another inclusive. The
// magazine prints 7—0 for 307 to 310, so the run is taken off the span rather
// than counted out arithmetically.
func between(first, last corpus.ProblemID, pool []corpus.ProblemID) []corpus.ProblemID {
	var out []corpus.ProblemID
	for _, id := range pool {
		if id.Subject == first.Subject && id.Number >= first.Number && id.Number <= last.Number {
			out = append(out, id)
		}
	}
	return out
}

func appendOnce(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}

// CreditCounts is the summary a build prints for the reader lists.
type CreditCounts struct {
	Lists    int
	Problems int
	Readers  int
	Widely   int
	Unread   int
}

// CountCredits walks the lists a build read.
func CountCredits(credits []Credit) CreditCounts {
	var c CreditCounts
	problems := map[string]bool{}
	for _, credit := range credits {
		c.Lists++
		c.Readers += credit.Readers
		c.Unread += credit.Unread
		c.Widely += len(credit.Widely)
		for id := range credit.Counts {
			problems[id] = true
		}
	}
	c.Problems = len(problems)
	return c
}

// String is the one line summary. The unread count is on it because these pages
// are four columns of six point names and the reading lane does not get all of
// it, and a difficulty measure that does not say how much of the page it missed
// is not a measure anybody should use.
func (c CreditCounts) String() string {
	return fmt.Sprintf("%d reader lists, %d problems credited, %d names read, "+
		"%d problems nearly everyone solved, %d printed numbers unreadable",
		c.Lists, c.Problems, c.Readers, c.Widely, c.Unread)
}
