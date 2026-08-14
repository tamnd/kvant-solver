package publisher

import (
	"hash/fnv"
	"sort"
)

// Candidate is an article the archive carries the text of and the corpus has
// read for itself, which is the only kind of article the two paths can be
// compared on.
type Candidate struct {
	Issue string
	Year  int
	Slug  string
	Title string
	URL   string
	// File is the assembled article in the corpus, which is the vision side of
	// the comparison. The article and not its pages: the pages carry the folio
	// lines and the column marks and half of a neighbouring piece at each end,
	// and assembly is what decides where this article stops.
	File string
}

// Sample picks the articles to compare, the same ones every time.
//
// Stratified by year, because the question is not how good the archive's text
// is, it is how good it is in 1993, and a sample drawn from the whole period
// would be drawn from wherever the archive happens to be densest. Fetching
// every one of them would work too and is thousands of requests at the polite
// rate, for an answer a few dozen a year already gives.
//
// The pick is by hash and not by chance. A rate that moves when nobody changed
// anything is a rate nobody trusts, so the same corpus and the same flag give
// the same articles, and adding a year does not reshuffle the years before it.
func Sample(all []Candidate, perYear int) []Candidate {
	if perYear <= 0 {
		return sorted(all)
	}
	byYear := map[int][]Candidate{}
	for _, c := range all {
		byYear[c.Year] = append(byYear[c.Year], c)
	}
	var out []Candidate
	for _, group := range byYear {
		sort.Slice(group, func(i, j int) bool {
			a, b := draw(group[i].Slug), draw(group[j].Slug)
			if a != b {
				return a < b
			}
			return group[i].Slug < group[j].Slug
		})
		out = append(out, group[:min(perYear, len(group))]...)
	}
	return sorted(out)
}

func sorted(list []Candidate) []Candidate {
	out := append([]Candidate(nil), list...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year < out[j].Year
		}
		if out[i].Issue != out[j].Issue {
			return out[i].Issue < out[j].Issue
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

func draw(slug string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(slug))
	return h.Sum64()
}

// DefaultPerYear is how many articles a year the diff is measured on.
//
// Twelve is one an issue on average, which is enough for a rate that does not
// swing on one abridged reprint and few enough that a decade is a few hundred
// polite requests rather than a few thousand.
const DefaultPerYear = 12
