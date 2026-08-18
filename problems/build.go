package problems

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// Article is what a build needs from one assembled article.
type Article struct {
	Path  string
	Front corpus.ArticleFront
	Body  string
}

// Document is one problem ready to be written to
// content/<lang>/problems/{m,f}/NNNN.md, with both halves joined.
//
// The statement and the published solution are kept apart rather than
// concatenated, because M9 must be able to hand a solver the statement while
// provably withholding the answer. A format that glues them together would
// make that a matter of remembering to cut the string in the right place, and
// the whole grading apparatus is worthless the first time somebody forgets.
type Document struct {
	Front    corpus.ProblemFront
	Question string
	Answer   string
}

// Gap is a problem the archive promised and the corpus does not have, or one
// it has and nothing promised.
type Gap struct {
	ID     string `yaml:"id"`
	Issue  string `yaml:"issue"`
	Kind   Kind   `yaml:"kind"`
	Reason string `yaml:"reason"`
}

// Result is one pass over the assembled articles.
type Result struct {
	Manifest  Manifest
	Documents []Document
	Gaps      []Gap
}

// Build recovers every problem the given articles carry.
//
// It takes articles rather than a corpus path because the pairing is archive
// wide: a problem posed in 1975 issue 3 is answered in 1975 issue 7, so this
// cannot run one issue at a time and there is no useful partial answer. The
// caller loads the whole run and passes it in.
func Build(articles []Article) Result {
	// Sorting by issue makes the first printing of a number the posed one when
	// the archive prints the same number twice, which it does when a problem is
	// reprinted with a correction.
	sorted := append([]Article(nil), articles...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].Front, sorted[j].Front
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		return a.Number < b.Number
	})

	var res Result
	entries := map[corpus.ProblemID]*Entry{}
	text := map[corpus.ProblemID]map[Kind]string{}
	// Where the words came from, carried forward from the article they were cut
	// out of. A problem file is a join of two pages rather than a reading of
	// its own, so inventing provenance for it would be a lie, and leaving it
	// blank would make the corpus validator right to complain.
	prov := map[corpus.ProblemID]corpus.Provenance{}

	for _, art := range sorted {
		kind, ok := KindOf(art.Front.Title)
		if !ok {
			continue
		}
		found := map[corpus.ProblemID]bool{}
		for _, item := range Split(art.Body, kind) {
			found[item.ID] = true
			entry := entries[item.ID]
			if entry == nil {
				entry = &Entry{
					ID:      item.ID.String(),
					Subject: item.ID.Subject,
					Number:  item.ID.Number,
				}
				entries[item.ID] = entry
				text[item.ID] = map[Kind]string{}
			}
			printing := &Printing{
				Issue:   art.Front.Issue,
				Article: art.Path,
				Pages:   art.Front.PageLabels,
				Tag:     string(art.Front.Tag),
				Rubric:  rubric(art.Front),
			}
			// The first printing of a half is the one kept. A second is either a
			// reprint or a number the reading lane misread onto the wrong page,
			// and either way the earlier issue is the one that printed it.
			switch {
			case kind == Posed && entry.Posed == nil:
				entry.Posed = printing
				text[item.ID][Posed] = item.Text
				prov[item.ID] = art.Front.Provenance
			case kind == Solved && entry.Solved == nil:
				entry.Solved = printing
				text[item.ID][Solved] = item.Text
			default:
				res.Gaps = append(res.Gaps, Gap{
					ID:    item.ID.String(),
					Issue: art.Front.Issue,
					Kind:  kind,
					Reason: fmt.Sprintf("printed again, the %s half is already taken from %s",
						kind, printingIssue(entry, kind)),
				})
			}
			if len(art.Front.Authors) > 0 && kind == Posed {
				entry.Authors = art.Front.Authors
			}
		}
		// What the heading promised and the body did not deliver.
		for _, id := range Promised(art.Front.Title) {
			if !found[id] {
				res.Gaps = append(res.Gaps, Gap{
					ID:     id.String(),
					Issue:  art.Front.Issue,
					Kind:   kind,
					Reason: "the heading lists it and the page does not carry it",
				})
			}
		}
	}

	for id, entry := range entries {
		res.Manifest.Entries = append(res.Manifest.Entries, *entry)
		// A problem whose statement has not been read yet gets a manifest row
		// and no file. The row is the record that the archive printed a
		// solution to something this corpus cannot show, which points at a year
		// still to read rather than at a page to fix, and a file with no
		// statement would be an empty problem pretending to be a whole one.
		if entry.Posed == nil {
			res.Gaps = append(res.Gaps, Gap{
				ID:     id.String(),
				Issue:  entry.Solved.Issue,
				Kind:   Solved,
				Reason: "the solution is here and the issue that posed it has not been read",
			})
			continue
		}
		res.Documents = append(res.Documents, document(id, entry, text[id], prov[id]))
	}
	res.Manifest.Sort()
	sort.SliceStable(res.Documents, func(i, j int) bool {
		a, b := res.Documents[i].Front, res.Documents[j].Front
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.ID < b.ID
	})
	sort.SliceStable(res.Gaps, func(i, j int) bool {
		if res.Gaps[i].Issue != res.Gaps[j].Issue {
			return res.Gaps[i].Issue < res.Gaps[j].Issue
		}
		return res.Gaps[i].ID < res.Gaps[j].ID
	})
	return res
}

// rubric is the section of the magazine an article ran in.
//
// The permanent slug is preferred over the heading printed on the page, because
// the heading is whatever the typesetter set that month and the slug is the same
// string across fifty years. The heading is the fallback rather than nothing,
// since an article the tag stage has not reached yet still knows what it was
// printed under and dropping that would lose the only difficulty signal the
// archive gives.
func rubric(front corpus.ArticleFront) string {
	if front.Rubric != "" {
		return front.Rubric
	}
	return strings.TrimSpace(front.RubricSub)
}

func printingIssue(e *Entry, kind Kind) string {
	if kind == Posed && e.Posed != nil {
		return e.Posed.Issue
	}
	if e.Solved != nil {
		return e.Solved.Issue
	}
	return "an earlier issue"
}

func document(id corpus.ProblemID, e *Entry, halves map[Kind]string, prov corpus.Provenance) Document {
	// The hash is of the body about to be written, which Save fills in. Copying
	// the article's would claim this file is that one.
	prov.ContentSHA256 = ""
	doc := Document{
		Front: corpus.ProblemFront{
			ID:                   id.String(),
			Subject:              id.Subject,
			Authors:              e.Authors,
			HasPublishedSolution: e.Solved != nil,
			Provenance:           prov,
		},
		Question: halves[Posed],
		Answer:   halves[Solved],
	}
	if e.Posed != nil {
		doc.Front.PosedIn = e.Posed.Issue
		doc.Front.PosedPages = e.Posed.Pages
	}
	if e.Solved != nil {
		doc.Front.SolvedIn = e.Solved.Issue
		doc.Front.SolvedPages = e.Solved.Pages
	}
	return doc
}

// Body is the file that goes under content/<lang>/problems/.
//
// The two headings are fixed strings rather than anything derived from the
// page, so that a reader and a parser can both find the answer without knowing
// which issue it came out of.
func (d Document) Body() string {
	var b strings.Builder
	b.WriteString("## Условие\n\n")
	if d.Question != "" {
		b.WriteString(d.Question)
	} else {
		b.WriteString("Условие не найдено в прочитанных выпусках.\n")
	}
	if d.Answer != "" {
		b.WriteString("\n\n## Решение\n\n")
		b.WriteString(d.Answer)
	}
	b.WriteString("\n")
	return b.String()
}
