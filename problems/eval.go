package problems

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/manifest"
)

// EvalDir is where the built sets go, under manifests/ in a corpus checkout.
const EvalDir = "evaluation"

// Set is a list of problems to solve, with the reason it holds those and not
// others written down next to it.
//
// The criteria are recorded rather than just the result because a set is a
// claim about coverage, and a list of two hundred numbers with no statement of
// what it was drawn from cannot be checked or rebuilt. Anyone reading a
// scorecard needs to know whether a bad decade is bad because the solver
// struggles with it or because the set drew four problems from it.
type Set struct {
	Name    string    `yaml:"name"`
	Why     string    `yaml:"why"`
	Size    int       `yaml:"size"`
	Strata  []Stratum `yaml:"strata"`
	Members []string  `yaml:"members"`
}

// Stratum is one cell of the sample and what it drew.
type Stratum struct {
	Decade    int    `yaml:"decade"`
	Subject   string `yaml:"subject"`
	Available int    `yaml:"available"`
	Taken     int    `yaml:"taken"`
}

// Gradable reports whether a problem can be scored, which means the magazine
// printed an answer and this corpus has both halves.
//
// A set drawn without this filter measures nothing: the solver would be asked
// questions no one can mark, and the agreement rate would be computed over
// whichever subset happened to have ground truth.
func Gradable(e Entry) bool { return e.Posed != nil && e.Solved != nil }

// Stratify draws up to perCell problems from every decade and subject present.
//
// The draw is evenly spaced through each cell rather than random, so a set is
// reproducible from the manifest alone with no seed to record and no drift
// when the corpus grows a problem in the middle of a range.
func Stratify(m *Manifest, perCell int) *Set {
	cells := map[string][]Entry{}
	for _, e := range m.Entries {
		if !Gradable(e) {
			continue
		}
		decade := manifest.KeyYear(e.Posed.Issue) / 10 * 10
		if decade == 0 {
			continue
		}
		cells[fmt.Sprintf("%d/%s", decade, e.Subject)] = append(cells[fmt.Sprintf("%d/%s", decade, e.Subject)], e)
	}

	keys := make([]string, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	set := &Set{}
	for _, k := range keys {
		pool := cells[k]
		sort.SliceStable(pool, func(i, j int) bool { return pool[i].Number < pool[j].Number })
		taken := spread(pool, perCell)
		var decade int
		var subject string
		if _, err := fmt.Sscanf(k, "%d/%s", &decade, &subject); err != nil {
			continue
		}
		set.Strata = append(set.Strata, Stratum{
			Decade:    decade,
			Subject:   subject,
			Available: len(pool),
			Taken:     len(taken),
		})
		for _, e := range taken {
			set.Members = append(set.Members, e.ID)
		}
	}
	set.Size = len(set.Members)
	return set
}

// spread picks n items evenly spaced through a slice, which for a pool sorted
// by problem number means the sample covers the whole range rather than
// bunching at whichever end the corpus happens to be complete on.
func spread(pool []Entry, n int) []Entry {
	if n <= 0 || len(pool) == 0 {
		return nil
	}
	if n >= len(pool) {
		return pool
	}
	out := make([]Entry, 0, n)
	for i := range n {
		out = append(out, pool[i*len(pool)/n])
	}
	return out
}

// SetFile is where a named set is written.
func SetFile(name string) string {
	return fmt.Sprintf("%s/%s.yaml", EvalDir, name)
}

// Describe is the summary line a build prints.
func (s *Set) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d problems over %d strata", s.Name, s.Size, len(s.Strata))
	for _, st := range s.Strata {
		fmt.Fprintf(&b, "\n  %ds %-8s %d of %d", st.Decade, st.Subject, st.Taken, st.Available)
	}
	return b.String()
}

// Statement returns the half of a problem file a solver is allowed to see.
//
// This is the seam M9 depends on. The published answer lives in the same file
// under a heading of its own, and everything that talks to a model goes
// through here so that withholding it is one function rather than a rule every
// call site has to remember.
func Statement(body string) string {
	_, rest, ok := strings.Cut(body, "## Условие")
	if !ok {
		rest = body
	}
	statement, _, _ := strings.Cut(rest, "## Решение")
	return strings.TrimSpace(statement)
}

// PublishedSolution returns the answer the magazine printed, which is shown to
// the grader and to nobody else.
func PublishedSolution(body string) string {
	_, answer, ok := strings.Cut(body, "## Решение")
	if !ok {
		return ""
	}
	return strings.TrimSpace(answer)
}
