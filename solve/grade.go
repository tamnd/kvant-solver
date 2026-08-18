package solve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/api"
)

// Marking is one solution measured against the printed one.
type Marking struct {
	ID    string `yaml:"id"`
	Grade string `yaml:"grade"`
	// Notes is what the grader said. It is kept because the grade on its own
	// is not reviewable: a run that reports 60 per cent correct is worth
	// nothing if nobody can go and read why the other 40 were marked down.
	Notes string    `yaml:"notes,omitempty"`
	Model string    `yaml:"model,omitempty"`
	Usage api.Usage `yaml:"usage,omitempty"`
}

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
		ID:    p.ID,
		Grade: ParseGrade(text),
		Notes: text,
		Model: response.Model,
		Usage: response.Usage,
	}, nil
}

// Scorecard is a run over an evaluation set.
type Scorecard struct {
	Set       string    `yaml:"set"`
	Model     string    `yaml:"model,omitempty"`
	Mode      Mode      `yaml:"mode,omitempty"`
	Attempted int       `yaml:"attempted"`
	Verified  int       `yaml:"verified"`
	Correct   int       `yaml:"correct"`
	Partial   int       `yaml:"partial"`
	Incorrect int       `yaml:"incorrect"`
	Ungraded  int       `yaml:"ungraded"`
	Usage     api.Usage `yaml:"usage,omitempty"`
	Markings  []Marking `yaml:"markings,omitempty"`
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
	s.Usage = s.Usage.Add(res.Usage).Add(m.Usage)
	s.Markings = append(s.Markings, m)
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

// Compare pairs each result with its marking.
func Compare(results []Result, markings []Marking) Agreement {
	grade := map[string]string{}
	for _, m := range markings {
		grade[m.ID] = m.Grade
	}
	var a Agreement
	for _, r := range results {
		g, ok := grade[r.ID]
		if !ok || g == Ungraded {
			continue
		}
		switch {
		case r.Verified() && g == Correct:
			a.VerifiedAndCorrect++
		case r.Verified():
			a.VerifiedAndWrong++
		case g == Correct:
			a.UnverifiedAndCorrect++
		default:
			a.UnverifiedAndWrong++
		}
	}
	return a
}

// Sort puts the markings in a stable order so that a rerun over the same set
// produces a diffable scorecard.
func (s *Scorecard) Sort() {
	sort.SliceStable(s.Markings, func(i, j int) bool { return s.Markings[i].ID < s.Markings[j].ID })
}

// Report is reports/scorecard.md.
func (s *Scorecard) Report(a Agreement) string {
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
