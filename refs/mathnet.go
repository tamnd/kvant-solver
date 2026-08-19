package refs

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source/mathnetru"
)

// MathNetFile is the concordance, which lives next to the year graphs.
//
// ReadAll skips it, because it globs the directory and keeps only the files
// named after a year. That is deliberate: this is not a graph of citations
// between Kvant articles and it does not belong in the same structure.
const MathNetFile = "mathnet.yaml"

// The statuses a row of the concordance can carry.
const (
	// MathNetLinked is a mathnet article we hold and have identified.
	MathNetLinked = "linked"

	// MathNetUnread is a mathnet article in an issue the corpus does not have.
	// Not a failure of the matching, and it is the answer for 2005, 2006 and
	// 2011 to 2013, which no source has given us a scan of.
	MathNetUnread = "unread"

	// MathNetUnassembled is a mathnet article in an issue the corpus has only
	// part of. Also not a failure of the matching: 2008 issue 1 is eight
	// articles here against mathnet's twenty-nine, so most of what does not
	// line up is a piece nobody has assembled yet rather than a piece we hold
	// and could not identify.
	MathNetUnassembled = "unassembled"

	// MathNetUnmatched is a mathnet article in an issue we hold in full, where
	// nothing in our copy of it lines up. This is the count worth watching,
	// because it is the only one of the three that means the matching failed.
	MathNetUnmatched = "unmatched"
)

// MathNet is manifests/refs/mathnet.yaml.
//
// What this is: every Kvant article mathnet.ru holds, tied to our tag for the
// same article. What it is not, despite the milestone asking for one, is a
// citation graph. Mathnet files Kvant under popular science rather than as a
// research journal and indexes no references for it at all: no article page in
// the run carries a bibliography, a cited-by list or a reference page. That was
// checked against a sample across the whole range before this was written, and
// it is a fact about the source rather than something that can be worked
// around.
//
// What the source does have is a permanent identifier per article. Ours are
// ours. This is the one name for a Kvant article that somebody outside this
// project would recognise, and pinning it to our tags is what makes an outside
// citation land on a file in this corpus.
type MathNet struct {
	Source string `yaml:"source"`

	// Count is every article mathnet lists. Linked is how many reached one of
	// ours, and the difference is accounted for by the two statuses.
	Count  int `yaml:"count"`
	Linked int `yaml:"linked"`

	Papers []MathNetPaper `yaml:"papers"`
}

// MathNetPaper is one article as mathnet lists it, with our tag for it when it
// was found.
type MathNetPaper struct {
	ID  string `yaml:"id"`
	URL string `yaml:"url"`

	Issue string `yaml:"issue"`
	Year  int    `yaml:"year"`

	Title     string   `yaml:"title"`
	Authors   []string `yaml:"authors,omitempty"`
	PageFirst int      `yaml:"page_first,omitempty"`
	PageLast  int      `yaml:"page_last,omitempty"`
	FullText  bool     `yaml:"full_text,omitempty"`

	To      corpus.Tag `yaml:"to,omitempty"`
	ToLabel string     `yaml:"to_label,omitempty"`

	// How says which of the two tests found it, so that a row matched only on
	// its title can be told from one where the printed pages agreed too.
	How    string `yaml:"how,omitempty"`
	Status string `yaml:"status"`
}

// The two ways a row can be identified, weakest last.
const (
	// ByTitle is the titles agreeing once punctuation and case are taken off.
	// This is the strong one, because a title is distinctive.
	ByTitle = "title"

	// ByPage is the printed first page agreeing, and it is only reached for
	// when the title did not match.
	//
	// It reads like the stronger test and it is not. A page number is one
	// integer and the two sides do not always agree on it: in 2008 issue 1 our
	// numbering runs two ahead of mathnet's, so the page test confidently ties
	// their solutions column to our obituary of Landau, which the title test
	// had already matched correctly. Page equality is worth having as a
	// fallback and it is not worth trusting over a title.
	ByPage = "page"
)

// ours is one article in the corpus, reduced to what the matching needs.
type ours struct {
	tag   corpus.Tag
	label string
	title string
	first int
}

// MathNetIndex is every article in the corpus, by issue, ready to be matched
// against.
type MathNetIndex struct {
	byIssue map[string][]ours
}

// LoadMathNet reads the corpus articles.
//
// It reads the files rather than the printed contents in manifests/toc.yaml,
// because the concordance has to point at a file that exists. A row of the
// contents is not something a tag can be handed out for.
func LoadMathNet(c *corpus.Corpus, lang string) (*MathNetIndex, error) {
	idx := &MathNetIndex{byIssue: map[string][]ours{}}
	dir := filepath.Join(c.Root, "content", lang)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") || !strings.Contains(filepath.ToSlash(path), "/articles/") {
			return nil
		}
		f := &corpus.ArticleFront{}
		if _, err := corpus.LoadUnchecked(path, f); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		idx.byIssue[f.Issue] = append(idx.byIssue[f.Issue], ours{
			tag: f.Tag, label: f.ID, title: foldTitle(f.Title), first: f.PageFirst,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return idx, nil
}

// Held is how many articles the corpus has assembled for this issue.
//
// Zero, some, or as many as mathnet lists are three different situations and
// the concordance says which of them a miss happened in. Counting them all as
// a failure of the matching would turn the one number worth watching into a
// report on how much of the magazine has been assembled.
func (i *MathNetIndex) Held(issue string) int { return len(i.byIssue[issue]) }

// Has reports whether the corpus holds any of this issue.
func (i *MathNetIndex) Has(issue string) bool { return i.Held(issue) > 0 }

// hit is what one paper was matched to.
type hit struct {
	row ours
	how string
}

// match assigns the papers of one issue to our articles in it.
//
// A whole issue at a time and in two passes, rather than a paper at a time,
// because the two tests are not equally good and an article can only belong to
// one paper. Every title that agrees is settled first, and only then are the
// leftovers offered the page test, over the articles nobody has claimed. Doing
// it a paper at a time lets a page collision take an article out from under the
// title that really owned it, which is exactly what 2008 issue 1 does.
func (i *MathNetIndex) match(issue string, papers []mathnetru.PaperRef) map[string]hit {
	rows := i.byIssue[issue]
	out := make(map[string]hit, len(papers))
	taken := make([]bool, len(rows))

	byTitle := map[string][]int{}
	for n, row := range rows {
		if row.title != "" {
			byTitle[row.title] = append(byTitle[row.title], n)
		}
	}
	for _, paper := range papers {
		// Exactly one, because two articles in an issue under the same folded
		// title leave nothing to choose between them, and a coin toss here
		// puts a wrong permanent link on a file.
		at := byTitle[foldTitle(paper.Title)]
		if len(at) != 1 || taken[at[0]] {
			continue
		}
		taken[at[0]] = true
		out[paper.ID] = hit{row: rows[at[0]], how: ByTitle}
	}
	for _, paper := range papers {
		if _, done := out[paper.ID]; done || paper.PageFirst == 0 {
			continue
		}
		for n, row := range rows {
			if !taken[n] && row.first == paper.PageFirst {
				taken[n] = true
				out[paper.ID] = hit{row: row, how: ByPage}
				break
			}
		}
	}
	return out
}

// foldTitle strips everything the two sides spell differently: case, the
// punctuation the magazine sets titles with, and the runs of whitespace a
// scan leaves behind.
func foldTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// BuildMathNet ties the mathnet listing to the corpus.
//
// issues carries the printed number for each one, which is what the corpus key
// is built from, so a double issue printed as 5-6 lands on kvant_YYYY_5-6 the
// way the rest of the archive names it.
func BuildMathNet(issues []mathnetru.IssueRef, papers map[string][]mathnetru.PaperRef, idx *MathNetIndex) *MathNet {
	out := &MathNet{Source: mathnetru.ContentsURL()}
	for _, issue := range issues {
		key := fmt.Sprintf("kvant_%d_%s", issue.Year, issue.Number)
		found := idx.match(key, papers[key])
		for _, paper := range papers[key] {
			row := MathNetPaper{
				ID: paper.ID, URL: paper.URL,
				Issue: key, Year: issue.Year,
				Title: paper.Title, Authors: paper.Authors,
				PageFirst: paper.PageFirst, PageLast: paper.PageLast,
				FullText: paper.FullText,
			}
			switch got, ok := found[paper.ID]; {
			case ok:
				row.To, row.ToLabel = got.row.tag, got.row.label
				row.How, row.Status = got.how, MathNetLinked
				out.Linked++
			case idx.Held(key) == 0:
				row.Status = MathNetUnread
			case idx.Held(key) < len(papers[key]):
				row.Status = MathNetUnassembled
			default:
				row.Status = MathNetUnmatched
			}
			out.Papers = append(out.Papers, row)
		}
	}
	out.Count = len(out.Papers)
	sort.SliceStable(out.Papers, func(a, b int) bool {
		if out.Papers[a].Year != out.Papers[b].Year {
			return out.Papers[a].Year < out.Papers[b].Year
		}
		if out.Papers[a].Issue != out.Papers[b].Issue {
			return out.Papers[a].Issue < out.Papers[b].Issue
		}
		return out.Papers[a].PageFirst < out.Papers[b].PageFirst
	})
	return out
}

// Counts is how many rows carry each status.
func (m *MathNet) Counts() map[string]int {
	out := map[string]int{}
	for _, p := range m.Papers {
		out[p.Status]++
	}
	return out
}

// Years is every year in the concordance, in order.
func (m *MathNet) Years() []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range m.Papers {
		if !seen[p.Year] {
			seen[p.Year] = true
			out = append(out, p.Year)
		}
	}
	sort.Ints(out)
	return out
}

// Check refuses a concordance that would be misleading to commit.
func (m *MathNet) Check() error {
	if m.Source == "" {
		return fmt.Errorf("the concordance does not say where it came from")
	}
	seen := map[string]bool{}
	tags := map[corpus.Tag]string{}
	for _, p := range m.Papers {
		if p.ID == "" || p.Title == "" {
			return fmt.Errorf("a row with no identifier or no title: %+v", p)
		}
		if seen[p.ID] {
			return fmt.Errorf("%s is listed twice", p.ID)
		}
		seen[p.ID] = true
		if p.Year < mathnetru.FirstYear {
			return fmt.Errorf("%s is dated %d and mathnet starts at %d", p.ID, p.Year, mathnetru.FirstYear)
		}
		if p.To == "" {
			continue
		}
		// One tag to one paper. Two mathnet articles pointing at one of our
		// files means the matching went wrong, and a permanent link is exactly
		// the kind of thing nobody rechecks once it is written down.
		if other, ok := tags[p.To]; ok {
			return fmt.Errorf("%s and %s both claim %s", other, p.ID, p.To)
		}
		tags[p.To] = p.ID
	}
	return nil
}

// SaveMathNet writes manifests/refs/mathnet.yaml.
func SaveMathNet(store *manifest.Store, m *MathNet) error {
	return store.Write(MathNetFile, m,
		"Every Kvant article mathnet.ru holds, against our tag for the same article.",
		"Built by kvant refs mathnet. Bibliography and permanent links only.",
		"Mathnet indexes no references for Kvant, so this is a concordance and not a citation graph.")
}
