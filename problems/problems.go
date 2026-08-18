// Package problems recovers Задачник «Кванта» from the assembled articles.
//
// The magazine prints a problem in one issue and its solution two to four
// issues later, under two rubric headings that name the numbers they carry:
// «Задачи М311—М315; Ф323—Ф327» and «Решения задач М273—М279; Ф285—Ф290». So
// the corpus already knows, before any text is read, which numbers ought to be
// on a page and whether they are being posed or answered. That is what makes
// this recoverable at all, and it is why the heading is treated as the claim
// and the body as the evidence rather than the other way round: a heading that
// promises five problems and a body that yields three is a page worth
// reporting, and neither number is trustworthy enough on its own to be the
// answer.
package problems

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// Rubric is the slug the assemble stage gives Задачник «Кванта».
const Rubric = "zadachnik-kvanta"

// ManifestFile is the problem index, under manifests/ in a corpus checkout.
const ManifestFile = "problems.yaml"

// Kind is which half of a problem an article carries.
type Kind string

// The two halves. A problem is posed once and solved at most once, and the gap
// between them is the two to four issues the magazine allowed its readers.
const (
	Posed  Kind = "posed"
	Solved Kind = "solved"
)

// Item is one problem as it appears in one article, which is either its
// statement or its published solution but never both.
type Item struct {
	ID   corpus.ProblemID
	Kind Kind
	Text string
}

// marker matches the number a problem is printed under, at the start of a line.
//
// Both alphabets are accepted because the magazine prints Cyrillic М and Ф and
// the reading lane does not always agree: М and M are the same glyph, and a
// model that has just written a formula in Latin will carry on into the label.
// Which alphabet reached the page says nothing about the problem, so both are
// read and the identifier is canonicalised to the Latin form the rest of the
// corpus uses.
//
// The emphasis is allowed to fall on either side of the full stop. The
// magazine sets the mathematics as **М301.** and the physics on the facing
// page as **Ф313**., and a pattern that admits only the first silently drops
// every physics problem in the archive while looking like it works.
var marker = regexp.MustCompile(`(?m)^[ \t*_]*([МMФF])[ \t]?(\d{1,4})[ \t*_]*\.`)

// subject maps a printed letter to its series. М and M are mathematics, Ф and F
// are physics.
var subject = map[string]corpus.Subject{
	"М": corpus.Math, "M": corpus.Math,
	"Ф": corpus.Physics, "F": corpus.Physics,
}

// Split cuts an article body into the problems it carries.
//
// A problem runs from its own number to the next one, which is how the page is
// laid out: there is no end marker, and the last problem simply runs to the end
// of the article. Anything before the first number is the rubric heading and
// the editorial note that goes with it, and it belongs to no problem.
func Split(body string, kind Kind) []Item {
	hits := marker.FindAllStringSubmatchIndex(body, -1)
	items := make([]Item, 0, len(hits))
	for i, h := range hits {
		letter := body[h[2]:h[3]]
		number, err := strconv.Atoi(body[h[4]:h[5]])
		if err != nil || number < 1 {
			continue
		}
		end := len(body)
		if i+1 < len(hits) {
			end = hits[i+1][0]
		}
		items = append(items, Item{
			ID:   corpus.ProblemID{Subject: subject[letter], Number: number},
			Kind: kind,
			Text: strings.TrimSpace(body[h[1]:end]),
		})
	}
	return items
}

// posedTitle and solvedTitle tell the two halves apart by their heading.
//
// The solved form is checked first because it contains the word the posed form
// is looking for: «Решения задач М273—М279» would match a naive test for
// «Задачи» and every solution in the archive would be filed as a statement.
var (
	solvedTitle = regexp.MustCompile(`(?i)^\s*Решени`)
	posedTitle  = regexp.MustCompile(`(?i)^\s*Задачи`)
)

// KindOf reads a title and says which half the article is, if it is either.
func KindOf(title string) (Kind, bool) {
	switch {
	case solvedTitle.MatchString(title):
		return Solved, true
	case posedTitle.MatchString(title):
		return Posed, true
	default:
		return "", false
	}
}

// headingRange matches one М311—М315 span in a title. The dash is whatever the
// typesetter used, and the reading lane passes through all of them.
var headingRange = regexp.MustCompile(`([МMФF])\s?(\d{1,4})\s*(?:[—–−-]\s*([МMФF])?\s?(\d{1,4}))?`)

// Promised is the numbers a title says the article carries.
//
// This is the cross check. The title is a printed claim about the contents, so
// a number promised here and missing from the body is a problem the reading
// lane lost, and a number in the body that no heading promised is one it
// invented. Both are worth a line in the gap list, and telling them apart is
// most of what makes that list readable.
func Promised(title string) []corpus.ProblemID {
	var out []corpus.ProblemID
	for _, m := range headingRange.FindAllStringSubmatch(title, -1) {
		series, ok := subject[m[1]]
		if !ok {
			continue
		}
		first, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		last := first
		if m[4] != "" {
			if n, err := strconv.Atoi(m[4]); err == nil {
				last = n
			}
		}
		// A span that runs backwards, or one long enough to be a misread digit
		// rather than a range, is not a promise anybody made. The magazine
		// prints four to six problems per series per issue.
		if last < first || last-first > 20 {
			last = first
		}
		for n := first; n <= last; n++ {
			out = append(out, corpus.ProblemID{Subject: series, Number: n})
		}
	}
	return out
}

// Printing is one appearance of a problem in the archive.
type Printing struct {
	Issue   string `yaml:"issue"`
	Article string `yaml:"article"`
	Pages   string `yaml:"pages,omitempty"`
	Tag     string `yaml:"tag,omitempty"`
}

// Entry is one problem across both of its printings.
type Entry struct {
	ID      string         `yaml:"id"`
	Subject corpus.Subject `yaml:"subject"`
	Number  int            `yaml:"number"`
	Posed   *Printing      `yaml:"posed,omitempty"`
	Solved  *Printing      `yaml:"solved,omitempty"`
	Authors []string       `yaml:"authors,omitempty"`
}

// Solvable reports whether the magazine printed an answer. A problem without
// one is not a defect in the corpus: those are the ones the solver has no
// ground truth for, and M8 asks for them to be marked rather than hidden.
func (e Entry) Solvable() bool { return e.Solved != nil }

// Manifest is manifests/problems.yaml.
type Manifest struct {
	Entries []Entry `yaml:"problems"`
}

// Sort puts the manifest in series and number order so that a rebuild over
// unchanged content produces no diff.
func (m *Manifest) Sort() {
	sort.SliceStable(m.Entries, func(i, j int) bool {
		a, b := m.Entries[i], m.Entries[j]
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Number < b.Number
	})
}

// Counts is the summary a build prints and a report repeats.
type Counts struct {
	Total      int
	Math       int
	Physics    int
	Both       int
	PosedOnly  int
	SolvedOnly int
}

// Count walks the manifest. SolvedOnly is the interesting number: a solution
// whose statement was never found means the issue that posed it has not been
// read yet, or was read badly, and it points at a year rather than at a page.
func (m *Manifest) Count() Counts {
	var c Counts
	for _, e := range m.Entries {
		c.Total++
		if e.Subject == corpus.Physics {
			c.Physics++
		} else {
			c.Math++
		}
		switch {
		case e.Posed != nil && e.Solved != nil:
			c.Both++
		case e.Posed != nil:
			c.PosedOnly++
		default:
			c.SolvedOnly++
		}
	}
	return c
}

// String is the one line summary.
func (c Counts) String() string {
	return fmt.Sprintf("%d problems, %d M and %d F, %d with both halves, %d posed only, %d solved only",
		c.Total, c.Math, c.Physics, c.Both, c.PosedOnly, c.SolvedOnly)
}
