package manifest

import (
	"fmt"
	"slices"
	"strings"
)

// QuantumFile is the mapping between the English Quantum and the Kvant it was
// translated out of.
const QuantumFile = "quantum-map.yaml"

// Quantum is the whole run of the English magazine, one row per article, with
// the Russian original named where it could be worked out.
//
// It is a reference and not a source. Where a row has a Kvant key on it, the
// published English is a second opinion on our terminology and on the register
// we are aiming at, and a disagreement with it is worth looking at before it is
// overruled. None of the English text is here and none of it is in the corpus.
type Quantum struct {
	// Source is where the English side was read from, because a mapping is
	// only as good as the index behind it and the index is somebody else's
	// file that can change.
	Source string `yaml:"source"`

	Count  int `yaml:"count"`
	Mapped int `yaml:"mapped"`

	Articles []QuantumArticle `yaml:"articles"`
}

// How a row was matched. Both are made on the byline and neither has read a
// word of either article, so the distinction between them is the whole of the
// difference in how much a row is worth trusting.
const (
	// MatchAuthors is the strong one: the two magazines credit the same list
	// of people, and no other Kvant article is by that list.
	MatchAuthors = "authors"

	// MatchSomeAuthors is the weaker one: everybody the English credits wrote
	// the Russian article, but the Russian credits somebody else as well.
	// Quantum did shorten bylines, so these are usually right and they are
	// worth a second look before anything leans on one.
	MatchSomeAuthors = "some-authors"
)

// QuantumArticle is one article of the English magazine.
type QuantumArticle struct {
	Title   string   `yaml:"title"`
	Authors []string `yaml:"authors,omitempty"`

	// Year and Months are the cover date. Quantum was bimonthly, so the date
	// is a pair of months, except for the two 1990 pilot issues.
	Year   int    `yaml:"year"`
	Months string `yaml:"months"`
	Page   int    `yaml:"page"`

	// Department is the standing section it ran under. It is worth keeping
	// because it sorts the translated features away from the contest and
	// puzzle pages, which were written in English and have no original.
	Department string `yaml:"department,omitempty"`

	// Kvant is the issue the original appeared in, empty when the match could
	// not be made. KvantTitle is the Russian title, which is what makes a row
	// checkable by eye.
	Kvant      string `yaml:"kvant,omitempty"`
	KvantTitle string `yaml:"kvant_title,omitempty"`
	KvantPage  int    `yaml:"kvant_page,omitempty"`

	// Match says how the row was arrived at, so that a reader can tell the
	// strong claim from the weaker one without rerunning anything.
	Match string `yaml:"match,omitempty"`

	// Candidates is how many Russian articles have the same authors, recorded
	// only when that was not one. Zero is an article with no Russian original,
	// which is most of what Quantum commissioned itself. More than one is an
	// article whose authors are known and whose original is not, and the two
	// are different kinds of gap.
	Candidates int `yaml:"candidates,omitempty"`
}

// Issue is the cover date, written the way the index writes it.
func (a QuantumArticle) Issue() string { return fmt.Sprintf("%s %d", a.Months, a.Year) }

// ByKvant returns the English articles translated out of one issue.
func (q *Quantum) ByKvant(key string) []QuantumArticle {
	var out []QuantumArticle
	for _, a := range q.Articles {
		if a.Kvant == key {
			out = append(out, a)
		}
	}
	return out
}

// Ambiguous is how many articles have a Russian author and no single Russian
// article to point at. It is reported separately from the unmapped, because
// this is the pile that could still be resolved and the other one cannot.
func (q *Quantum) Ambiguous() int {
	n := 0
	for _, a := range q.Articles {
		if a.Kvant == "" && a.Candidates > 1 {
			n++
		}
	}
	return n
}

// Years is the run the index covers, oldest first.
func (q *Quantum) Years() []int {
	var out []int
	for _, a := range q.Articles {
		if !slices.Contains(out, a.Year) {
			out = append(out, a.Year)
		}
	}
	slices.Sort(out)
	return out
}

// Check looks for the mistakes this file can actually have in it.
func (q *Quantum) Check() error {
	if q.Source == "" {
		return fmt.Errorf("the map does not say where it came from")
	}
	seen := map[string]bool{}
	for _, a := range q.Articles {
		if a.Title == "" {
			return fmt.Errorf("an article in %s has no title", a.Issue())
		}
		if a.Year < 1990 || a.Year > 2001 {
			return fmt.Errorf("%s: %d is outside the run of the magazine", a.Title, a.Year)
		}
		key := strings.ToLower(a.Title) + "|" + a.Issue()
		if seen[key] {
			return fmt.Errorf("%s is in %s twice", a.Title, a.Issue())
		}
		seen[key] = true
		if a.Kvant != "" && a.Candidates > 1 {
			return fmt.Errorf("%s is mapped and ambiguous at the same time", a.Title)
		}
	}
	return nil
}
