package problems

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/publisher"
)

// This file compares the problems recovered off the scanned pages against a
// source that prints both halves of a problem on one page.
//
// The recovery here is a join. A problem is cut out of one issue, its solution
// is cut out of another issue two to four numbers later, and the two are put
// together on the strength of a number printed in a heading. Nothing in that
// chain is checked by the chain itself: a misread digit in a heading pairs a
// statement with somebody else's answer, and the result looks exactly like a
// problem that worked. kvant.digital has the same pairing, made by hand from
// the same magazine, and comparing the two is the only outside opinion this
// project can get on whether the join is right.
//
// What comes back is worth more than a tick. The site names the issue each half
// was printed in even where this corpus has not read that year, so a problem
// whose statement is missing here comes back with the issue that carries it,
// which turns a gap in the manifest into a page to go and read.

// Joined is one problem as a source that carries both halves has it.
//
// It is a plain struct rather than the site's own type so this package does not
// depend on any one source. kvant.digital is the only one that joins the halves
// today and there is no reason it has to be the last.
type Joined struct {
	ID  string `yaml:"id"`
	URL string `yaml:"url,omitempty"`
	// PosedIn and SolvedIn are corpus issue keys, so kvant_1975_3. Empty means
	// the source does not say, which for a solution usually means the magazine
	// never printed one.
	PosedIn  string `yaml:"posed_in,omitempty"`
	SolvedIn string `yaml:"solved_in,omitempty"`
	// Condition is the statement as the source has it, already in the LaTeX and
	// plain text form the site serves. Solution is the same for the answer, and
	// is often empty on pages where the site indexes the solution and carries
	// only the scan of it.
	Condition string `yaml:"condition,omitempty"`
	Solution  string `yaml:"solution,omitempty"`
	// Absent records that the source does not carry this number at all. It is
	// kept rather than left as a missing file so that a second run does not ask
	// again for the several hundred numbers the answer is already known for.
	Absent bool `yaml:"absent,omitempty"`
}

// Verdict is what one half of one problem came to.
type Verdict string

// The five outcomes. They are kept apart rather than collapsed into agree and
// disagree because only one of them is a fault and the others are work.
const (
	// Same is the two sources naming the same issue. This is the check passing.
	Same Verdict = "same"
	// Moved is the two sources naming different issues, which means one of them
	// has the problem on the wrong page and it is worth a person looking.
	Moved Verdict = "moved"
	// OnlyRead is this corpus having the half and the other source not indexing
	// it, which is the ordinary state of most of the archive: the site's problem
	// index is a fraction of the historical numbering.
	OnlyRead Verdict = "only read"
	// OnlyIndexed is the other source having the half and this corpus not. That
	// is the useful direction, because it names the issue to go and read.
	OnlyIndexed Verdict = "only indexed"
	// Neither is both sources silent, which for a solution is them agreeing that
	// the magazine never printed one.
	Neither Verdict = "neither"
)

// Check is one problem compared against the joined page.
type Check struct {
	ID  string `yaml:"id"`
	URL string `yaml:"url,omitempty"`

	Posed  Verdict `yaml:"posed"`
	Solved Verdict `yaml:"solved"`

	OurPosed    string `yaml:"our_posed,omitempty"`
	TheirPosed  string `yaml:"their_posed,omitempty"`
	OurSolved   string `yaml:"our_solved,omitempty"`
	TheirSolved string `yaml:"their_solved,omitempty"`

	// Condition is the word by word comparison of the two statements, in the
	// same two directions the publisher diff counts. It is the only measurement
	// of a vision pass in this project taken against text a person typed off the
	// same page, so it says what the reading is worth on the material the
	// benchmark is built from rather than on the archive at large.
	Condition publisher.Count     `yaml:"condition,omitempty"`
	Examples  []publisher.Example `yaml:"examples,omitempty"`
}

// Agrees reports whether the check found nothing to look at. A half the other
// source does not index is not a disagreement, and neither is a solution both
// sources say does not exist.
func (c Check) Agrees() bool { return c.Posed != Moved && c.Solved != Moved }

// TheirsRunsBackwards reports whether the other source has the answer printed
// before the problem.
//
// The magazine could not do that, so where it shows up the other source has the
// slip and this corpus does not. It is worth saying out loud in the report:
// otherwise a disagreement that has already been settled by the calendar sits
// in the list looking like an open question about our own reading.
func (c Check) TheirsRunsBackwards() bool {
	if c.TheirPosed == "" || c.TheirSolved == "" {
		return false
	}
	return earlier(c.TheirSolved, c.TheirPosed)
}

// earlier orders two issue keys by the shelf.
func earlier(a, b string) bool {
	ay, by := manifest.KeyYear(a), manifest.KeyYear(b)
	if ay != by {
		return ay < by
	}
	return manifest.FirstNumber(number(a)) < manifest.FirstNumber(number(b))
}

// number is the issue number out of a key, so 5-6 out of kvant_1976_5-6.
func number(key string) string {
	i := strings.LastIndex(key, "_")
	if i < 0 {
		return ""
	}
	return key[i+1:]
}

// Crosscheck is one run over a set of problems.
type Crosscheck struct {
	Checks []Check `yaml:"checks"`
	// Absent is the problems whose number the other source does not carry at
	// all. It is a count of what could not be checked rather than a list of
	// faults, and it is most of any run: the site's index is on the order of
	// fifteen hundred items against a numbering that runs past two thousand in
	// each series.
	Absent []string `yaml:"absent,omitempty"`
}

// Cross compares one problem.
//
// The statement is passed in rather than read here because this package does
// not open files, and because the caller has already had to load the problem
// file to know there is one.
func Cross(e Entry, statement string, j Joined) Check {
	c := Check{ID: e.ID, URL: j.URL, TheirPosed: j.PosedIn, TheirSolved: j.SolvedIn}
	if e.Posed != nil {
		c.OurPosed = e.Posed.Issue
	}
	if e.Solved != nil {
		c.OurSolved = e.Solved.Issue
	}
	c.Posed = verdict(c.OurPosed, c.TheirPosed)
	c.Solved = verdict(c.OurSolved, c.TheirSolved)
	if j.Condition != "" && statement != "" {
		c.Condition, c.Examples = publisher.Compare(j.Condition, statement)
	}
	return c
}

func verdict(ours, theirs string) Verdict {
	switch {
	case ours == "" && theirs == "":
		return Neither
	case ours == "":
		return OnlyIndexed
	case theirs == "":
		return OnlyRead
	case ours == theirs:
		return Same
	default:
		return Moved
	}
}

// Halves is the arithmetic of one half over a whole run.
type Halves struct {
	Same        int
	Moved       int
	OnlyRead    int
	OnlyIndexed int
	Neither     int
}

// Checked is the number of problems both sources name an issue for, which is
// the only population the agreement rate can be taken over.
func (h Halves) Checked() int { return h.Same + h.Moved }

// Rate is the share of those the two sources agree on.
func (h Halves) Rate() float64 {
	if h.Checked() == 0 {
		return 0
	}
	return float64(h.Same) / float64(h.Checked())
}

func (h *Halves) add(v Verdict) {
	switch v {
	case Same:
		h.Same++
	case Moved:
		h.Moved++
	case OnlyRead:
		h.OnlyRead++
	case OnlyIndexed:
		h.OnlyIndexed++
	case Neither:
		h.Neither++
	}
}

// Tally is what a run came to.
type Tally struct {
	Compared int
	Absent   int
	Posed    Halves
	Solved   Halves

	// Text is the two statements compared, over every problem where both
	// sources carry the words.
	Text       publisher.Count
	Statements int
}

// Tally walks the run.
func (x *Crosscheck) Tally() Tally {
	t := Tally{Compared: len(x.Checks), Absent: len(x.Absent)}
	for _, c := range x.Checks {
		t.Posed.add(c.Posed)
		t.Solved.add(c.Solved)
		if c.Condition.Words == 0 {
			continue
		}
		t.Statements++
		t.Text.Words += c.Condition.Words
		t.Text.Changed += c.Condition.Changed
		t.Text.Ours += c.Condition.Ours
		t.Text.Extra += c.Condition.Extra
	}
	return t
}

// Disagreements is every problem the two sources file on different pages, worst
// first is meaningless here so they come out in problem order.
func (x *Crosscheck) Disagreements() []Check {
	var out []Check
	for _, c := range x.Checks {
		if !c.Agrees() {
			out = append(out, c)
		}
	}
	return out
}

// ToRead is every half the other source has and this corpus does not, which is
// a list of issues to go and read rather than a list of faults.
func (x *Crosscheck) ToRead() []Check {
	var out []Check
	for _, c := range x.Checks {
		if c.Posed == OnlyIndexed || c.Solved == OnlyIndexed {
			out = append(out, c)
		}
	}
	return out
}

// Worst is the problems whose two statements are furthest apart, which is where
// to look first when the reading of a page is in question.
func (x *Crosscheck) Worst(n int) []Check {
	var out []Check
	for _, c := range x.Checks {
		if c.Condition.Words > 0 {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Condition.Rate() > out[j].Condition.Rate()
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Joins is where the fetched pages are kept.
//
// In the cache and not in the corpus, for the same reason the publisher text is:
// this is evidence about the corpus rather than part of it. Keeping it means a
// run of the report asks the site for nothing and gives the same answer twice,
// which is what makes the rate a measurement rather than a reading of the day.
type Joins struct{ Dir string }

// Path is where one problem's page goes.
func (s Joins) Path(id string) string { return filepath.Join(s.Dir, id+".yaml") }

// Put writes one fetched problem.
func (s Joins) Put(j Joined) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(j)
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path(j.ID), data, 0o644)
}

// Get reads one back. A missing file means this problem has not been fetched,
// which is the normal state of most of them and not an error.
func (s Joins) Get(id string) (Joined, bool, error) {
	data, err := os.ReadFile(s.Path(id))
	if os.IsNotExist(err) {
		return Joined{}, false, nil
	}
	if err != nil {
		return Joined{}, false, err
	}
	var j Joined
	if err := yaml.Unmarshal(data, &j); err != nil {
		return Joined{}, false, err
	}
	return j, true, nil
}
