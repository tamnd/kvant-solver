package solve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
)

// Marking is one solution measured against the printed one.
type Marking struct {
	ID    string `yaml:"id"`
	Grade string `yaml:"grade"`
	// Notes is what the grader said. It is kept because the grade on its own
	// is not reviewable: a run that reports 60 per cent correct is worth
	// nothing if nobody can go and read why the other 40 were marked down.
	Notes string `yaml:"notes,omitempty"`
	// Right is which of the two answers the grader believes, and it carries
	// something only where the two differ. A grade of INCORRECT says the two
	// texts disagree and says nothing about which one to trust. Квант printed
	// corrections to its own answers, and a run that took the printed one as
	// ground truth by definition would file our correct solutions as misses and
	// never show anyone the page worth going back to.
	Right string `yaml:"right,omitempty"`
	// Anachronism is a method the solution used that the magazine did not have
	// when it set the problem, empty when there was none. It is not a fault and
	// it does not move the grade. A 1974 problem solved with a tool from 1990 is
	// still solved, and it is recorded because a scorecard that cannot tell the
	// two apart is claiming the solver did what the readers of the time did.
	Anachronism string    `yaml:"anachronism,omitempty"`
	Model       string    `yaml:"model,omitempty"`
	Usage       api.Usage `yaml:"usage,omitempty"`
}

// Overturns reports a marking that went against the solution and then said the
// solution was the right one anyway.
//
// These are the markings to read by hand. Each is either a misprint in the
// magazine, which is a page to go and check, or a grader talking itself into our
// answer, which is a prompt to fix. Both need a person, and neither shows up
// anywhere in the score.
func (m Marking) Overturns() bool { return m.Right == Marked && m.Grade != Correct }

// Grade marks a finished solution against the solution the magazine printed.
//
// It takes a Result rather than a problem and a string because it can only run
// on work that is already done. That ordering is the point of the whole
// package: by the time the published text is read into this process the
// solution is fixed, recorded and cannot be revised, so there is no path by
// which the answer could have informed it.
func (e *Engine) Grade(ctx context.Context, p Problem, res Result, published string, opts Options) (Marking, error) {
	if e.Client == nil {
		return Marking{}, errors.New("no API client")
	}
	if strings.TrimSpace(res.Solution) == "" {
		return Marking{}, fmt.Errorf("%s: there is no solution to mark", p.ID)
	}
	// A problem the magazine never answered has no ground truth, and marking
	// against an empty string would quietly score every one of them wrong.
	if strings.TrimSpace(published) == "" {
		return Marking{ID: p.ID, Grade: Ungraded,
			Notes: "the magazine printed no solution to this problem, so there is nothing to mark against"}, nil
	}

	grader, ok := e.Prompts.(Grader)
	if !ok {
		if e.Prompts == nil {
			grader = DefaultPrompts{}
		} else {
			return Marking{}, fmt.Errorf("%s: the prompts in use cannot grade", p.ID)
		}
	}

	e.log("%s: grade", p.ID)
	instructions, input := grader.Grade(p, res.Solution, published)
	response, err := e.Client.Complete(ctx, api.Request{
		Model:        opts.Model,
		Instructions: instructions,
		Input:        input,
	})
	if err != nil {
		return Marking{}, fmt.Errorf("grade %s: %w", p.ID, err)
	}
	text := strings.TrimSpace(response.Text)
	return Marking{
		ID:          p.ID,
		Grade:       ParseGrade(text),
		Right:       Adjudication(text),
		Anachronism: Anachronism(text),
		Notes:       text,
		Model:       response.Model,
		Usage:       response.Usage,
	}, nil
}

// Scorecard is a run over an evaluation set.
type Scorecard struct {
	Set       string `yaml:"set"`
	Model     string `yaml:"model,omitempty"`
	Mode      Mode   `yaml:"mode,omitempty"`
	Attempted int    `yaml:"attempted"`
	Verified  int    `yaml:"verified"`
	Correct   int    `yaml:"correct"`
	Partial   int    `yaml:"partial"`
	Incorrect int    `yaml:"incorrect"`
	Ungraded  int    `yaml:"ungraded"`
	// Overturned and Anachronistic are counted apart from the outcomes above
	// because neither is one. A problem can be marked incorrect and overturned,
	// or marked correct and solved with a method from thirty years later, and
	// adding either to the tally of outcomes would make the four columns stop
	// summing to the number attempted.
	Overturned    int       `yaml:"overturned,omitempty"`
	Anachronistic int       `yaml:"anachronistic,omitempty"`
	Usage         api.Usage `yaml:"usage,omitempty"`
	Markings      []Row     `yaml:"markings,omitempty"`
}

// Row is one problem's line in a scorecard: the marking, and the three things
// the score has to be cut by.
//
// They are stored on the line rather than worked out at report time because a
// scorecard read back off disk has to be able to produce the same report it
// produced when it was written. A breakdown that lives only in the process that
// ran the set is a breakdown that vanishes from every scorecard anybody reads
// again.
type Row struct {
	Marking `yaml:",inline"`
	Subject corpus.Subject `yaml:"subject,omitempty"`
	Year    int            `yaml:"year,omitempty"`
	// Solver is the model that wrote the solution, which is not Marking.Model.
	// That one is the model that marked it, and a run where the two are equal is
	// a run whose grader is checking its own work.
	Solver string `yaml:"solver,omitempty"`
	// Verified is what both judges said, kept here so that the false pass rate
	// can be cut the same three ways as the score.
	Verified bool `yaml:"verified"`
}

// Add records one problem.
func (s *Scorecard) Add(res Result, m Marking) {
	s.Attempted++
	if res.Verified() {
		s.Verified++
	}
	switch m.Grade {
	case Correct:
		s.Correct++
	case Partial:
		s.Partial++
	case Incorrect:
		s.Incorrect++
	default:
		s.Ungraded++
	}
	if m.Overturns() {
		s.Overturned++
	}
	if m.Anachronism != "" {
		s.Anachronistic++
	}
	s.Usage = s.Usage.Add(res.Usage).Add(m.Usage)
	s.Markings = append(s.Markings, Row{
		Marking:  m,
		Subject:  res.Subject,
		Year:     res.Year,
		Solver:   res.Model,
		Verified: res.Verified(),
	})
}

// Graded is how many problems actually got a mark.
//
// The ungraded ones are left out of both halves of the score on purpose. A
// problem whose printed solution came off the scan too damaged to read is a
// failure of this corpus, not of the solver, and counting it as a miss would
// make every improvement to the reading lane look like an improvement to the
// solver.
func (s *Scorecard) Graded() int { return s.Correct + s.Partial + s.Incorrect }

// Score is the fraction marked correct, of those that could be marked. Partial
// credit counts as a miss: the magazine printed one answer and either the
// solution reached it or it did not.
func (s *Scorecard) Score() float64 {
	if s.Graded() == 0 {
		return 0
	}
	return float64(s.Correct) / float64(s.Graded())
}

// Agreement is where the judges and the printed answer disagree.
//
// This is the number that says whether the verification is worth its tokens. A
// solution both judges passed and the magazine marks wrong is a false pass, and
// a run with many of them is a run whose judges are agreeing with the solver
// rather than checking it. Nothing else in the pipeline can measure that,
// because the judges are the only check the solver has and they cannot audit
// themselves.
type Agreement struct {
	VerifiedAndCorrect   int `yaml:"verified_and_correct"`
	VerifiedAndWrong     int `yaml:"verified_and_wrong"`
	UnverifiedAndCorrect int `yaml:"unverified_and_correct"`
	UnverifiedAndWrong   int `yaml:"unverified_and_wrong"`
}

// FalsePassRate is the share of verified solutions the magazine marks wrong.
func (a Agreement) FalsePassRate() float64 {
	total := a.VerifiedAndCorrect + a.VerifiedAndWrong
	if total == 0 {
		return 0
	}
	return float64(a.VerifiedAndWrong) / float64(total)
}

// Agreement is worked out from the scorecard's own lines.
//
// It used to take the results and the markings and pair them by id. It reads
// them off one line each now, because the pairing was a second chance to lose a
// problem: a result with no marking, or a marking with no result, simply fell
// out of all four boxes and the totals still added up.
func (s *Scorecard) Agreement() Agreement {
	var a Agreement
	for _, row := range s.Markings {
		if row.Grade == Ungraded {
			continue
		}
		switch {
		case row.Verified && row.Grade == Correct:
			a.VerifiedAndCorrect++
		case row.Verified:
			a.VerifiedAndWrong++
		case row.Grade == Correct:
			a.UnverifiedAndCorrect++
		default:
			a.UnverifiedAndWrong++
		}
	}
	return a
}

// Stratum is the score over one slice of the set.
type Stratum struct {
	Name      string `yaml:"name"`
	Attempted int    `yaml:"attempted"`
	Graded    int    `yaml:"graded"`
	Correct   int    `yaml:"correct"`
	Verified  int    `yaml:"verified"`
	FalsePass int    `yaml:"false_pass"`
}

// Rate is the share marked correct of those that could be marked, which is what
// the milestone calls the agreement rate.
func (t Stratum) Rate() float64 {
	if t.Graded == 0 {
		return 0
	}
	return float64(t.Correct) / float64(t.Graded)
}

// FalsePassRate is the share of verified solutions the magazine marks wrong.
func (t Stratum) FalsePassRate() float64 {
	if t.Verified == 0 {
		return 0
	}
	return float64(t.FalsePass) / float64(t.Verified)
}

// By cuts the score along an axis, in name order.
//
// A line the axis has nothing to say about is named unknown and kept rather than
// dropped, and unknown sorts last. Dropping it would make the rows add up to
// fewer problems than the run attempted with nothing on the page to say why.
func (s *Scorecard) By(axis func(Row) string) []Stratum {
	index := map[string]*Stratum{}
	for _, row := range s.Markings {
		name := strings.TrimSpace(axis(row))
		if name == "" {
			name = "unknown"
		}
		into, ok := index[name]
		if !ok {
			into = &Stratum{Name: name}
			index[name] = into
		}
		into.Attempted++
		if row.Grade != Ungraded {
			into.Graded++
		}
		if row.Grade == Correct {
			into.Correct++
		}
		if row.Verified {
			into.Verified++
			if row.Grade != Correct && row.Grade != Ungraded {
				into.FalsePass++
			}
		}
	}
	out := make([]Stratum, 0, len(index))
	for _, stratum := range index {
		out = append(out, *stratum)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == "unknown") != (out[j].Name == "unknown") {
			return out[j].Name == "unknown"
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ByDecade cuts the score by the decade the magazine set the problem in.
//
// This is the axis the archive exists for. Fifty years of one magazine is fifty
// years of changing syllabus and changing fashion in what a problem looks like,
// and a single percentage over the lot says nothing about whether a model does
// better on 1974 than on 2004.
func (s *Scorecard) ByDecade() []Stratum {
	return s.By(func(row Row) string {
		if row.Year <= 0 {
			return ""
		}
		return fmt.Sprintf("%ds", row.Year/10*10)
	})
}

// BySubject separates the mathematics from the physics. They are marked by
// different standards, since a physics answer carries units and a number to a
// stated precision, and one number over both hides which of the two is dragging.
func (s *Scorecard) BySubject() []Stratum {
	return s.By(func(row Row) string { return string(row.Subject) })
}

// ByModel cuts by the model that wrote the solution, which is the axis that
// makes a scorecard worth keeping after the run: two models over the same set
// are only comparable if the same corpus and the same grader marked them both.
func (s *Scorecard) ByModel() []Stratum {
	return s.By(func(row Row) string { return row.Solver })
}

// strata writes one of the three tables, or nothing where the axis has one value
// and the table would just be the total again under another heading.
func strata(b *strings.Builder, title string, rows []Stratum) {
	if len(rows) < 2 {
		return
	}
	fmt.Fprintf(b, "### %s\n\n", title)
	b.WriteString("| | attempted | graded | correct | agreement | false pass |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %d | %d | %d | %.1f%% | %.1f%% |\n",
			row.Name, row.Attempted, row.Graded, row.Correct,
			100*row.Rate(), 100*row.FalsePassRate())
	}
	b.WriteString("\n")
}

// Sort puts the markings in a stable order so that a rerun over the same set
// produces a diffable scorecard.
func (s *Scorecard) Sort() {
	sort.SliceStable(s.Markings, func(i, j int) bool { return s.Markings[i].ID < s.Markings[j].ID })
}

// Report is reports/scorecard.md.
func (s *Scorecard) Report() string {
	a := s.Agreement()
	var b strings.Builder
	fmt.Fprintf(&b, "# Scorecard: %s\n\n", s.Set)
	if s.Model != "" {
		fmt.Fprintf(&b, "Model %s, mode %s.\n\n", s.Model, s.Mode)
	}
	fmt.Fprintf(&b, "%d problems attempted, %d of them graded against the printed solution.\n\n",
		s.Attempted, s.Graded())
	b.WriteString("| outcome | count |\n| --- | --- |\n")
	fmt.Fprintf(&b, "| correct | %d |\n", s.Correct)
	fmt.Fprintf(&b, "| partial | %d |\n", s.Partial)
	fmt.Fprintf(&b, "| incorrect | %d |\n", s.Incorrect)
	fmt.Fprintf(&b, "| ungraded | %d |\n\n", s.Ungraded)
	fmt.Fprintf(&b, "Score %.1f per cent, counting only the problems that could be marked.\n\n",
		100*s.Score())
	if s.Ungraded > 0 {
		fmt.Fprintf(&b, "%d problems could not be marked, because the printed solution is missing "+
			"or came off the scan too damaged to read. They are left out of the score rather than "+
			"counted as misses, since those are pages this corpus failed to read and not problems "+
			"the solver got wrong.\n\n", s.Ungraded)
	}
	if decades, subjects, models := s.ByDecade(), s.BySubject(), s.ByModel(); len(decades)+len(subjects)+len(models) > 3 {
		b.WriteString("## How the score is spread\n\n")
		b.WriteString("Agreement is the share of the graded problems where our answer and the " +
			"printed one match. False pass is the share of the solutions both judges approved " +
			"that the magazine then marks wrong, which is the only measure of the judges that " +
			"does not come from the judges.\n\n")
		strata(&b, "By decade", decades)
		strata(&b, "By subject", subjects)
		strata(&b, "By model", models)
	}
	if s.Overturned > 0 {
		fmt.Fprintf(&b, "## Where the grader sided with us\n\n%d markings went against the "+
			"solution and then named it the right answer of the two. Each is either something "+
			"Квант misprinted, something this corpus read off the scan wrong, or a grader that "+
			"argued itself round, and the three are only told apart by reading the page. None of "+
			"them move the score, which is against what the magazine printed either way.\n\n",
			s.Overturned)
		for _, m := range s.Markings {
			if m.Overturns() {
				fmt.Fprintf(&b, "- %s, marked %s\n", m.ID, strings.ToLower(m.Grade))
			}
		}
		b.WriteString("\n")
	}
	if s.Anachronistic > 0 {
		fmt.Fprintf(&b, "## Methods the magazine did not have\n\n%d solutions used something a "+
			"reader of the issue could not have had. That is not a fault and nothing was marked "+
			"down for it. It is here because a problem set for schoolchildren and solved with a "+
			"theorem from thirty years later has not been solved the way the magazine meant it.\n\n",
			s.Anachronistic)
		for _, m := range s.Markings {
			if m.Anachronism != "" {
				fmt.Fprintf(&b, "- %s: %s\n", m.ID, m.Anachronism)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("## What the judges caught\n\n")
	fmt.Fprintf(&b, "%d verified and correct, %d verified and wrong, %d unverified and correct, "+
		"%d unverified and wrong.\n\n",
		a.VerifiedAndCorrect, a.VerifiedAndWrong, a.UnverifiedAndCorrect, a.UnverifiedAndWrong)
	fmt.Fprintf(&b, "False pass rate %.1f per cent. That is the share of solutions both judges "+
		"approved and the magazine marks wrong, and it is the only measure of the judges that does "+
		"not come from the judges themselves.\n", 100*a.FalsePassRate())
	if u := s.Usage.Normalized(); u.OutputTokens > 0 {
		fmt.Fprintf(&b, "\n%d input tokens and %d output tokens across solving and grading.\n",
			u.InputTokens, u.OutputTokens)
	}
	return b.String()
}
