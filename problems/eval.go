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
	Decade  int    `yaml:"decade"`
	Subject string `yaml:"subject"`
	// Rubric is the section of the magazine the cell was drawn from. It is a
	// third axis and not a label, because the sections are not equally hard and
	// a set that let one of them float would report a solver getting better in a
	// decade where the reading lane happened to finish more of the junior pages.
	Rubric    string `yaml:"rubric,omitempty"`
	Available int    `yaml:"available"`
	Taken     int    `yaml:"taken"`
}

// cell is what a stratum is keyed by while the draw is running.
type cell struct {
	decade  int
	subject string
	rubric  string
}

// Gradable reports whether a problem can be scored, which means the magazine
// printed an answer and this corpus has both halves.
//
// A set drawn without this filter measures nothing: the solver would be asked
// questions no one can mark, and the agreement rate would be computed over
// whichever subset happened to have ground truth.
func Gradable(e Entry) bool { return e.Posed != nil && e.Solved != nil }

// Stratify draws up to perCell problems from every decade, subject and rubric
// present.
//
// The draw is evenly spaced through each cell rather than random, so a set is
// reproducible from the manifest alone with no seed to record and no drift
// when the corpus grows a problem in the middle of a range.
//
// A problem whose rubric nothing recorded is drawn from its own cell named
// unknown rather than being folded into the commonest one. Folding it would put
// problems of unknown difficulty into a stratum that claims to be one
// difficulty, which is the one thing this function exists to prevent.
func Stratify(m *Manifest, perCell int) *Set {
	pools := map[cell][]Entry{}
	for _, e := range m.Entries {
		if !Gradable(e) {
			continue
		}
		decade := manifest.KeyYear(e.Posed.Issue) / 10 * 10
		if decade == 0 {
			continue
		}
		rubric := e.Rubric()
		if rubric == "" {
			rubric = "unknown"
		}
		key := cell{decade: decade, subject: string(e.Subject), rubric: rubric}
		pools[key] = append(pools[key], e)
	}

	keys := make([]cell, 0, len(pools))
	for k := range pools {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.decade != b.decade {
			return a.decade < b.decade
		}
		if a.subject != b.subject {
			return a.subject < b.subject
		}
		return a.rubric < b.rubric
	})

	set := &Set{}
	for _, k := range keys {
		pool := pools[k]
		sort.SliceStable(pool, func(i, j int) bool { return pool[i].Number < pool[j].Number })
		taken := spread(pool, perCell)
		set.Strata = append(set.Strata, Stratum{
			Decade:    k.decade,
			Subject:   k.subject,
			Rubric:    k.rubric,
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

// Rubrics is every section the set drew from, in the order the strata are in.
//
// A set that comes back with one rubric is not a broken draw. Every М and Ф
// number in the archive was set in Задачник «Кванта», so a set of them is one
// rubric wide by construction, and the axis earns its place by catching the
// issues where it is not: a Задачник page filed under «Смесь» by the archive, or
// an issue whose rubric the tag stage never reached, both of which have to be
// visible rather than averaged in.
func (s *Set) Rubrics() []string {
	var out []string
	seen := map[string]bool{}
	for _, st := range s.Strata {
		if st.Rubric != "" && !seen[st.Rubric] {
			seen[st.Rubric] = true
			out = append(out, st.Rubric)
		}
	}
	sort.Strings(out)
	return out
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
		fmt.Fprintf(&b, "\n  %ds %-8s %-28s %d of %d",
			st.Decade, st.Subject, st.Rubric, st.Taken, st.Available)
	}
	if rubrics := s.Rubrics(); len(rubrics) == 1 {
		fmt.Fprintf(&b, "\nevery problem came out of %s, so the rubric axis separates nothing in this set",
			rubrics[0])
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
