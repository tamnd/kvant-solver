package refs

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
)

// Status is how far a citation got.
//
// Four outcomes and not two, because the interesting distinction is not
// resolved against unresolved. A pointer at page 32 of an issue we have not
// read yet is a perfectly good pointer that nothing can be done with today, and
// filing it with the ones nobody could make sense of would mean the report says
// the extraction is worse than it is and would keep saying so until the last
// year is read.
type Status string

const (
	// Linked is a citation that came out as a tag. This is the one that is worth
	// counting.
	Linked Status = "linked"
	// Issue is a citation that named an issue and no page, so an issue is as far
	// as it goes. It is resolved, it just does not point at one object.
	Issue Status = "issue"
	// Pending is a citation whose target exists in the printed contents and not
	// yet in the corpus. It becomes a link when that year is read.
	Pending Status = "pending"
	// Unresolved is a citation that points at nothing anybody can find.
	Unresolved Status = "unresolved"
)

// Ref is one edge of the graph.
type Ref struct {
	From      corpus.Tag `yaml:"from"`
	FromLabel string     `yaml:"from_label"`
	Kind      Kind       `yaml:"kind"`
	Cite      string     `yaml:"cite"`
	// Issue is the issue the citation resolved to, when it resolved to one.
	Issue   string     `yaml:"issue,omitempty"`
	Page    int        `yaml:"page,omitempty"`
	Problem string     `yaml:"problem,omitempty"`
	To      corpus.Tag `yaml:"to,omitempty"`
	ToLabel string     `yaml:"to_label,omitempty"`
	// Title is what the printed contents call the target, filled in for a
	// pending reference so that the graph says something useful before the year
	// it points at has been read.
	Title  string `yaml:"title,omitempty"`
	Status Status `yaml:"status"`
	// Why is the sentence for a person, on the references that need one.
	Why string `yaml:"why,omitempty"`
}

// Graph is the file written to manifests/refs/<year>.yaml.
type Graph struct {
	Year  int   `yaml:"year"`
	Count int   `yaml:"count"`
	Refs  []Ref `yaml:"refs"`
}

// Index is everything resolution needs to look at, loaded once.
type Index struct {
	// issues is every issue the archive holds, so that a citation to an issue
	// that was never printed can be told from one to an issue we have not read.
	issues map[string]bool
	// pages maps an issue and a printed page to the article that covers it.
	pages map[string]map[int]article
	// rows is the printed contents, which covers every year whether or not it
	// has been read, and is what makes a pending reference say something.
	rows map[string][]manifest.Row
	// problems maps M381 to the problem file's tag.
	problems map[string]article
}

type article struct {
	tag   corpus.Tag
	label string
	title string
}

// Load reads the corpus and the manifests into an index.
func Load(c *corpus.Corpus, lang string) (*Index, error) {
	store, err := manifest.Open(c.Root)
	if err != nil {
		return nil, err
	}
	idx := &Index{
		issues:   map[string]bool{},
		pages:    map[string]map[int]article{},
		rows:     map[string][]manifest.Row{},
		problems: map[string]article{},
	}

	issues := &manifest.Issues{}
	if err := store.Read(manifest.IssuesFile, issues); err != nil {
		return nil, err
	}
	for _, iss := range issues.Issues {
		idx.issues[iss.Key] = true
	}

	toc := &manifest.TOC{}
	if err := store.Read(manifest.TOCFile, toc); err != nil {
		return nil, err
	}
	for _, block := range toc.Issues {
		idx.rows[block.Key] = block.Rows
	}

	paths, err := filepath.Glob(filepath.Join(c.Root, "content", lang, "*", "*", "articles", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		var front corpus.ArticleFront
		if _, err := corpus.LoadUnchecked(path, &front); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if front.Tag == "" {
			continue
		}
		a := article{tag: front.Tag, label: front.ID, title: front.Title}
		if idx.pages[front.Issue] == nil {
			idx.pages[front.Issue] = map[int]article{}
		}
		for _, page := range Labels(front.PageLabels) {
			// The first article to claim a page keeps it. Assembly gives every
			// page to one article, so this only fires on a corpus that was
			// assembled from a contents with overlapping rows, and picking the
			// earlier one is the same answer a reader turning to the page would
			// give.
			if _, taken := idx.pages[front.Issue][page]; !taken {
				idx.pages[front.Issue][page] = a
			}
		}
	}

	problems, err := filepath.Glob(filepath.Join(c.Root, "content", lang, "problems", "*", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range problems {
		var front corpus.ProblemFront
		if _, err := corpus.LoadUnchecked(path, &front); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		idx.problems[front.ID] = article{tag: front.Tag, label: front.ID}
	}
	return idx, nil
}

// Labels expands a page_labels field into the printed pages it names.
//
// The field is written the way a contents line is written: 12 for one page,
// 12-15 for a run, and 12, 14, 16 for an article a full page advert or a colour
// plate broke into pieces.
func Labels(field string) []int {
	var out []int
	for part := range strings.SplitSeq(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		first, last, ok := strings.Cut(part, "-")
		from, err := strconv.Atoi(strings.TrimSpace(first))
		if err != nil {
			continue
		}
		to := from
		if ok {
			if n, err := strconv.Atoi(strings.TrimSpace(last)); err == nil {
				to = n
			}
		}
		if to < from || to-from > 200 {
			continue
		}
		for page := from; page <= to; page++ {
			out = append(out, page)
		}
	}
	return out
}

// Resolve turns one citation into an edge.
//
// from is the article doing the citing and year is its year, which is what a
// citation with no year printed means: the magazine writes «Квант» № 3 when it
// means the current volume and only prints the year when it does not.
func (idx *Index) Resolve(from corpus.Tag, fromLabel string, year int, cite Citation) Ref {
	ref := Ref{From: from, FromLabel: fromLabel, Kind: cite.Kind, Cite: cite.Text}
	if cite.Kind == KindProblem {
		return idx.resolveProblem(ref, cite)
	}

	want := cite.Year
	if want == 0 {
		want = year
	}
	// Kvant is monthly, so anything past twelve is not an issue number and the
	// archive not having it is beside the point. Saying so separately is worth a
	// line of code: it is the difference between a gap in the collection and a
	// digit the reading invented, and the second is the one somebody can fix.
	if n, err := strconv.Atoi(cite.Number); err != nil || n < 1 || n > 12 {
		ref.Status = Unresolved
		ref.Why = fmt.Sprintf("%q is not an issue number, the magazine prints twelve a year", cite.Number)
		return ref
	}
	key := fmt.Sprintf("kvant_%d_%s", want, cite.Number)
	if !idx.issues[key] {
		ref.Status, ref.Why = Unresolved, fmt.Sprintf("the archive has no issue %s", key)
		return ref
	}
	ref.Issue, ref.Page = key, cite.Page
	if cite.Kind == KindIssue {
		ref.Status = Issue
		return ref
	}
	if a, ok := idx.pages[key][cite.Page]; ok {
		ref.To, ref.ToLabel, ref.Title, ref.Status = a.tag, a.label, a.title, Linked
		return ref
	}
	// Not read yet, or read and the page belongs to no article. The printed
	// contents can still say what is on the page, and that is worth writing
	// down: it is the whole answer for a citation into a year nobody has got to.
	if title, ok := rowAt(idx.rows[key], cite.Page); ok {
		ref.Title, ref.Status = title, Pending
		ref.Why = fmt.Sprintf("%s is not in the corpus yet", key)
		return ref
	}
	ref.Status = Unresolved
	ref.Why = fmt.Sprintf("nothing in the contents of %s covers page %d", key, cite.Page)
	return ref
}

func (idx *Index) resolveProblem(ref Ref, cite Citation) Ref {
	ref.Problem = cite.Problem
	a, ok := idx.problems[cite.Problem]
	if !ok {
		ref.Status = Pending
		ref.Why = "the problems have not been extracted into the corpus yet"
		return ref
	}
	if a.tag == "" {
		ref.Status = Pending
		ref.Why = "the problem is in the corpus and has no tag, run kvant tags assign"
		return ref
	}
	ref.To, ref.ToLabel, ref.Status = a.tag, a.label, Linked
	return ref
}

// rowAt is the printed contents line that covers a page.
//
// A row says where it starts and the next one says where it ends, so the answer
// is the last row that starts at or before the page. Rows with no printed page
// are skipped rather than treated as page zero: the contents of this magazine
// carry lines for cover captions and competition results that have no page of
// their own.
func rowAt(rows []manifest.Row, page int) (string, bool) {
	best, found := manifest.Row{}, false
	for _, row := range rows {
		if row.Page <= 0 || row.Page > page {
			continue
		}
		if !found || row.Page > best.Page {
			best, found = row, true
		}
	}
	if !found || strings.TrimSpace(best.Title) == "" {
		return "", false
	}
	return best.Title, true
}

// Years is every year the corpus has articles for, in order.
//
// The reference pass runs over what has been read rather than over what the
// archive holds, because an unread year contributes no citations and asking for
// it only puts an empty graph on disk.
func Years(c *corpus.Corpus, lang string) ([]int, error) {
	paths, err := filepath.Glob(filepath.Join(c.Root, "content", lang, "*", "*", "articles"))
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, path := range paths {
		year, err := strconv.Atoi(filepath.Base(filepath.Dir(filepath.Dir(path))))
		if err != nil {
			continue
		}
		seen[year] = true
	}
	out := make([]int, 0, len(seen))
	for year := range seen {
		out = append(out, year)
	}
	sort.Ints(out)
	return out, nil
}

// Build reads every article of the years asked for and resolves what it cites.
func Build(c *corpus.Corpus, lang string, idx *Index, years []int) (map[int]*Graph, error) {
	out := map[int]*Graph{}
	for _, year := range years {
		paths, err := filepath.Glob(filepath.Join(c.Root, "content", lang, strconv.Itoa(year), "*", "articles", "*.md"))
		if err != nil {
			return nil, err
		}
		sort.Strings(paths)
		graph := &Graph{Year: year}
		for _, path := range paths {
			var front corpus.ArticleFront
			body, err := corpus.LoadUnchecked(path, &front)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			for _, cite := range Find(body) {
				graph.Refs = append(graph.Refs, idx.Resolve(front.Tag, front.ID, front.Year, cite))
			}
		}
		graph.Count = len(graph.Refs)
		out[year] = graph
	}
	return out, nil
}

// Counts is how many references of each status a set of graphs holds.
func Counts(graphs map[int]*Graph) map[Status]int {
	out := map[Status]int{}
	for _, graph := range graphs {
		for _, ref := range graph.Refs {
			out[ref.Status]++
		}
	}
	return out
}

// Dir is where the graphs are written, under the corpus manifests.
const Dir = "refs"

// Store points at manifests/refs, one file per year.
//
// A year to a file rather than one graph for the whole corpus, because the
// files are committed and reviewed: a rerun over 1975 should produce a diff
// about 1975 and not move every line of a five megabyte manifest.
func Store(c *corpus.Corpus) (*manifest.Store, error) {
	return manifest.At(filepath.Join(c.Root, "manifests", Dir))
}

// Save writes one year's graph to manifests/refs/<year>.yaml.
func Save(store *manifest.Store, graph *Graph) error {
	return store.Write(strconv.Itoa(graph.Year)+".yaml", graph,
		fmt.Sprintf("Cross references found in the %d articles, resolved against the printed contents.", graph.Year))
}
