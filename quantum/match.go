package quantum

import (
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source/nsta"
)

// Person is a name cut down to the two things a match can be made on.
type Person struct {
	Surname string // folded, so that two spellings of it are one string
	Initial string // the letters the given name can begin with, possibly empty
}

// Russian reads a byline off a Russian table of contents, which writes the
// surname first and the initials after it: Ширшов А. И.
func Russian(byline string) []Person {
	var out []Person
	for part := range strings.SplitSeq(byline, ",") {
		words := words(part)
		if len(words) == 0 {
			continue
		}
		p := Person{Surname: Fold(Translit(words[0]))}
		if len(words) > 1 {
			p.Initial = Initial(words[1])
		}
		if len(p.Surname) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// English reads one name off a Quantum byline, which writes it the way a name
// is written in English: Dmitry Fuchs, or A. Shirshov, or Alexander A. Pukhov.
func English(name string) (Person, bool) {
	words := words(name)
	if len(words) == 0 {
		return Person{}, false
	}
	surname := words[len(words)-1]
	// A trailing initial is not a surname. The index has a few bylines that
	// end in one and they carry no surname to match on at all.
	if len(surname) < 3 || strings.HasSuffix(surname, ".") {
		return Person{}, false
	}
	p := Person{Surname: Fold(surname)}
	if len(words) > 1 {
		p.Initial = Initial(words[0])
	}
	if len(p.Surname) < 3 {
		return Person{}, false
	}
	return p, true
}

// words splits a name and drops the punctuation a byline is held together
// with, keeping the full stop so that a bare initial can be told from a short
// surname.
func words(s string) []string {
	var out []string
	for w := range strings.FieldsSeq(s) {
		w = strings.Trim(w, ",;:()[]«»\"'")
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// Article is one row of a Russian table of contents, reduced to what the match
// needs and where to point afterwards.
type Article struct {
	Key     string
	Year    int
	Title   string
	Page    int
	Authors []Person
}

// Index is every Russian article that carries a byline, arranged so that the
// articles by one person can be found without walking the whole corpus.
type Index struct {
	articles []Article
	by       map[string][]int // folded surname to the articles it appears on
}

// NewIndex reads the tables of contents. Rows with no byline are left out:
// they cannot be matched on a byline and keeping them would only slow the
// walk down.
func NewIndex(toc *manifest.TOC) *Index {
	idx := &Index{by: map[string][]int{}}
	for _, issue := range toc.Issues {
		// An unreadable key is not worth stopping the build over, but it does
		// have to be left out: without a year there is nothing to check the
		// direction of the translation against.
		key, err := corpus.ParseIssueKey(issue.Key)
		if err != nil {
			continue
		}
		for _, row := range issue.Rows {
			people := Russian(row.Authors)
			if len(people) == 0 {
				continue
			}
			at := len(idx.articles)
			idx.articles = append(idx.articles, Article{
				Key: issue.Key, Year: key.Year, Title: row.Title, Page: row.Page, Authors: people,
			})
			seen := map[string]bool{}
			for _, p := range people {
				if !seen[p.Surname] {
					seen[p.Surname] = true
					idx.by[p.Surname] = append(idx.by[p.Surname], at)
				}
			}
		}
	}
	return idx
}

// Len is how many Russian articles the index holds.
func (i *Index) Len() int { return len(i.articles) }

// Result is what the match came to for one English article.
type Result struct {
	// Article is the Russian original, set only when exactly one survived.
	Article *Article

	// Whole says the two bylines are the same list of people. When it is false
	// the English credits a subset of them, which Quantum did often enough to
	// be worth keeping and which is a weaker claim, so it is written down.
	Whole bool

	// Candidates is how many Russian articles carry the same set of authors.
	// Zero means nobody by that name wrote for Kvant, which is the answer for
	// everything Quantum commissioned in English. More than one means the
	// authors are right and there is nothing here to choose between them.
	Candidates int
}

// Match looks for the Russian article an English one was translated from.
//
// The byline is the whole of the evidence. The titles are no help, because a
// translated title is rewritten rather than translated and Обобщённая сумма
// углов многогранника came out as Adding Angles in Three Dimensions. Neither
// is the date, because Quantum drew on the whole back catalogue and printed
// that one eighteen years after Kvant did.
//
// So a row is claimed only when every author of the English article matches an
// author of the Russian one on both surname and initial, the two have the same
// number of authors, and exactly one Russian article does all of that. A single
// author with a common surname usually leaves dozens of candidates and is left
// alone, which is correct: Гик wrote three hundred pieces for Kvant and nothing
// in the index says which of them this is.
func (i *Index) Match(e nsta.Entry) Result {
	var people []Person
	for _, name := range e.Authors {
		if p, ok := English(name); ok {
			people = append(people, p)
		}
	}
	if len(people) == 0 {
		return Result{}
	}

	// Start from the articles by the rarest of the authors and narrow down,
	// rather than intersecting every author's list.
	rarest := 0
	for n, p := range people {
		if len(i.by[p.Surname]) < len(i.by[people[rarest].Surname]) {
			rarest = n
		}
	}

	// Two passes over the same shortlist. The first wants the two bylines to
	// be the same list of people, which is the claim worth making. The second
	// settles for the English crediting some of them, which Quantum did when
	// it shortened a byline of three to one, and it only runs when the first
	// found nothing so that it can never spoil a whole match.
	var whole, part []int
	for _, at := range i.by[people[rarest].Surname] {
		// A translation cannot come out before the thing it translates. This
		// costs nothing and it rules out the later half of a long career,
		// which is what most of the prolific authors are buried under.
		if i.articles[at].Year > e.Year {
			continue
		}
		if !i.fits(i.articles[at], people) {
			continue
		}
		part = append(part, at)
		if len(i.articles[at].Authors) == len(people) {
			whole = append(whole, at)
		}
	}
	if len(whole) == 1 {
		return Result{Article: &i.articles[whole[0]], Whole: true, Candidates: 1}
	}
	if len(whole) == 0 && len(part) == 1 {
		return Result{Article: &i.articles[part[0]], Candidates: 1}
	}
	if len(whole) > 1 {
		return Result{Candidates: len(whole)}
	}
	return Result{Candidates: len(part)}
}

// fits reports whether every one of these people wrote the Russian article.
// The article may credit more of them, which the caller weighs.
func (a *Index) fits(article Article, people []Person) bool {
	if len(article.Authors) < len(people) {
		return false
	}
	used := make([]bool, len(article.Authors))
	for _, p := range people {
		found := false
		for n, other := range article.Authors {
			if used[n] || other.Surname != p.Surname || !SameInitial(p.Initial, other.Initial) {
				continue
			}
			used[n] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

// Build turns the English index and the Russian tables of contents into the
// mapping manifest.
func Build(entries []nsta.Entry, toc *manifest.TOC) *manifest.Quantum {
	idx := NewIndex(toc)
	out := &manifest.Quantum{Source: nsta.IndexURL}
	for _, e := range entries {
		row := manifest.QuantumArticle{
			Title:      e.Title,
			Authors:    e.Authors,
			Year:       e.Year,
			Months:     e.Months,
			Page:       e.Page,
			Department: e.Department,
		}
		switch got := idx.Match(e); {
		case got.Article != nil:
			row.Kvant = got.Article.Key
			row.KvantTitle = got.Article.Title
			row.KvantPage = got.Article.Page
			row.Match = manifest.MatchAuthors
			if !got.Whole {
				row.Match = manifest.MatchSomeAuthors
			}
			out.Mapped++
		default:
			row.Candidates = got.Candidates
		}
		out.Articles = append(out.Articles, row)
	}
	out.Count = len(out.Articles)
	sort.SliceStable(out.Articles, func(a, b int) bool {
		return out.Articles[a].Title < out.Articles[b].Title
	})
	return out
}
