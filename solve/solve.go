// Package solve works a Kvant problem and, separately, grades the answer
// against the one the magazine printed.
//
// The two halves are separate on purpose and the separation is enforced by the
// types rather than by care. Solve takes a Problem, and a Problem has nowhere
// to put a published solution: there is no field for it, so no call site can
// pass one by accident and no future edit can quietly start. Grade is the only
// function that accepts the printed answer, and it runs after everything else
// has finished and been recorded.
//
// This matters more here than it would elsewhere. Задачник «Кванта» printed its
// own solutions, which is the reason this archive is worth solving at all, and
// it is also the reason a leak would be invisible: a model shown the answer
// writes a correct solution, both judges pass it, and the scorecard reports a
// high agreement rate that measures nothing but paraphrase.
package solve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/answerguard"
	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
)

// Problem is everything the solver is allowed to know.
//
// There is deliberately no field for the published solution. That is the whole
// leak barrier: it is not a rule to remember, it is a struct with nothing to
// hold the answer in.
type Problem struct {
	ID      string
	Subject corpus.Subject
	Text    string
	// Year the magazine set the problem, or 0 when the file it came from does
	// not say. No prompt that solves shows it, and it is here for the grader,
	// which cannot ask whether a solution reached for something the magazine
	// did not have without a year to hold it to. The grader drops that
	// question rather than guess when this is 0.
	Year int
}

// Validate reports what is missing before any model is called, because a run
// that discovers an empty statement after four completions has spent real
// money to learn it.
func (p Problem) Validate() error {
	if p.ID == "" {
		return errors.New("problem has no id")
	}
	if strings.TrimSpace(p.Text) == "" {
		return fmt.Errorf("%s: the statement is empty, so there is nothing to solve", p.ID)
	}
	return nil
}

// Options is how hard to work.
type Options struct {
	Model string
	// Candidates solved independently before one is selected. Three is the
	// default because the point is disagreement: one candidate cannot be
	// checked against anything, and two give a tie with no way to break it.
	Candidates int
	// Verify runs the reference call, the selector and both judges. Without it
	// this is a single completion and the result is not evidence of anything.
	Verify bool
	// MaxCorrections bounds the repair loop. A solution that fails, is
	// corrected and fails again is rechecked from scratch rather than patched,
	// so this is small on purpose.
	MaxCorrections int
}

// Mode is the two settings anybody actually uses.
type Mode string

const (
	// Fast is one candidate and no judges, for checking that the pipeline runs.
	Fast Mode = "fast"
	// Slow is the real thing, and the only mode whose verdict means anything.
	Slow Mode = "slow"
)

// Apply fills in the options a mode implies.
func (m Mode) Apply(o Options) (Options, error) {
	switch m {
	case Fast:
		o.Verify = false
		o.Candidates = 1
		o.MaxCorrections = 0
	case Slow:
		o.Verify = true
		if o.Candidates < 1 {
			o.Candidates = 3
		}
	case "":
	default:
		return o, fmt.Errorf("unknown solve mode %q, try fast or slow", m)
	}
	if o.Candidates < 1 {
		o.Candidates = 1
	}
	if o.Candidates > 5 {
		return o, fmt.Errorf("%d candidates is more than this is built for, the limit is 5", o.Candidates)
	}
	return o, nil
}

// Judgement is what one judge decided.
type Judgement struct {
	Kind    string `yaml:"kind"`
	Verdict string `yaml:"verdict"`
	Text    string `yaml:"text"`
}

// Passed reports a clean verdict.
func (j Judgement) Passed() bool { return j.Verdict == Pass }

// The verdicts a judge may return.
const (
	Pass = "PASS"
	Fail = "FAIL"
	Skip = "SKIP"
)

// Result is one worked problem.
type Result struct {
	ID         string         `yaml:"id"`
	Subject    corpus.Subject `yaml:"subject"`
	Solution   string         `yaml:"solution"`
	Reference  string         `yaml:"reference,omitempty"`
	Candidates []string       `yaml:"candidates,omitempty"`
	Selected   int            `yaml:"selected,omitempty"`
	Judgements []Judgement    `yaml:"judgements,omitempty"`
	Verdict    string         `yaml:"verdict"`
	Model      string         `yaml:"model,omitempty"`
	Usage      api.Usage      `yaml:"usage,omitempty"`
	Elapsed    time.Duration  `yaml:"elapsed,omitempty"`
}

// Verified reports whether both judges passed, which is the only condition
// under which this corpus calls a solution solved.
func (r Result) Verified() bool { return r.Verdict == Pass }

// Engine solves problems through a model.
type Engine struct {
	Client   api.Completer
	Prompts  Prompts
	Progress func(string)
}

// Solve works one problem.
func (e *Engine) Solve(ctx context.Context, p Problem, opts Options) (Result, error) {
	if e.Client == nil {
		return Result{}, errors.New("no API client")
	}
	if err := p.Validate(); err != nil {
		return Result{}, err
	}
	started := time.Now()
	prompts := e.Prompts
	if prompts == nil {
		prompts = DefaultPrompts{}
	}

	res := Result{ID: p.ID, Subject: p.Subject, Verdict: Skip}
	var usage api.Usage

	// guard says whether the answer is going to be published. It is true for
	// the calls that produce solution text and false for the selector and the
	// judges, and that is not a shortcut.
	//
	// answerguard reads a line of English with no Cyrillic in it as a refusal,
	// which is exactly right for a page of a Russian magazine and exactly wrong
	// for a judge, whose whole job is to write English sentences like "cannot
	// verify the third step". Running the guard over a judgement would turn
	// every careful judge into a transport error. The judgements are not
	// published, so nothing is lost by leaving them unguarded.
	call := func(kind string, guard bool, instructions, input string) (string, error) {
		e.log("%s: %s", p.ID, kind)
		response, err := e.Client.Complete(ctx, api.Request{
			Model:        opts.Model,
			Instructions: instructions,
			Input:        input,
		})
		if err != nil {
			return "", fmt.Errorf("%s: %w", kind, err)
		}
		usage = usage.Add(response.Usage)
		if res.Model == "" {
			res.Model = response.Model
		}
		text := answerguard.Strip(strings.TrimSpace(response.Text))
		if text == "" {
			return "", fmt.Errorf("%s: the model returned nothing", kind)
		}
		if guard {
			// A response carrying the provider's own furniture is not a
			// solution, and letting it through would put it in the corpus.
			if leaks := answerguard.Check(text); len(leaks) > 0 {
				return "", fmt.Errorf("%s: the answer carries %d leaked marker(s), the first is %s: %s",
					kind, len(leaks), leaks[0].Kind, leaks[0].Detail)
			}
			text = answerguard.Normalise(text)
		}
		return text, nil
	}

	// The reference is solved before the candidates and independently of them,
	// so that the selector has something to compare against that did not come
	// out of the same conversation.
	if opts.Verify {
		instructions, input := prompts.Reference(p)
		reference, err := call("reference", true, instructions, input)
		if err != nil {
			return res, err
		}
		res.Reference = reference
	}

	for i := 1; i <= opts.Candidates; i++ {
		instructions, input := prompts.Candidate(p, i)
		candidate, err := call(fmt.Sprintf("candidate %d of %d", i, opts.Candidates), true, instructions, input)
		if err != nil {
			return res, err
		}
		res.Candidates = append(res.Candidates, candidate)
	}

	res.Selected = 1
	if len(res.Candidates) > 1 {
		instructions, input := prompts.Select(p, res.Reference, res.Candidates)
		text, err := call("select", false, instructions, input)
		if err != nil {
			return res, err
		}
		choice := SelectedCandidate(text)
		if choice < 1 || choice > len(res.Candidates) {
			return res, fmt.Errorf("the selector chose %d of %d candidates, which is not one of them",
				choice, len(res.Candidates))
		}
		res.Selected = choice
	}
	res.Solution = res.Candidates[res.Selected-1]

	if opts.Verify {
		for pass := 0; ; pass++ {
			judgements, ok, err := e.judge(ctx, call, prompts, p, res.Reference, res.Solution)
			if err != nil {
				return res, err
			}
			res.Judgements = judgements
			if ok {
				res.Verdict = Pass
				break
			}
			res.Verdict = Fail
			if pass >= opts.MaxCorrections {
				break
			}
			instructions, input := prompts.Correct(p, res.Solution, judgements)
			corrected, err := call(fmt.Sprintf("correct, pass %d", pass+1), true, instructions, input)
			if err != nil {
				return res, err
			}
			res.Solution = corrected
		}
	}

	res.Usage = usage
	res.Elapsed = time.Since(started).Round(time.Millisecond)
	return res, nil
}

// judge runs both judges and reports whether both passed.
//
// Both must pass. They are looking for different things: the truth judge asks
// whether the answer is right, and the audit judge tries to break it without
// having seen the reference, so a solution that talks its way past one still
// has to survive the other.
func (e *Engine) judge(ctx context.Context, call func(kind string, guard bool, instructions, input string) (string, error),
	prompts Prompts, p Problem, reference, solution string) ([]Judgement, bool, error) {
	instructions, input := prompts.TruthJudge(p, reference, solution)
	truthText, err := call("truth judge", false, instructions, input)
	if err != nil {
		return nil, false, err
	}
	truth := Judgement{Kind: "truth", Verdict: Verdict(truthText), Text: truthText}

	instructions, input = prompts.AuditJudge(p, solution)
	auditText, err := call("audit judge", false, instructions, input)
	if err != nil {
		return nil, false, err
	}
	audit := Judgement{Kind: "audit", Verdict: Verdict(auditText), Text: auditText}

	judgements := []Judgement{truth, audit}

	// Physics is graded on more than algebra. A result that is dimensionally
	// wrong is wrong however elegant the derivation, and the judges read prose
	// rather than check units, so this is done here and not asked of a model.
	if p.Subject == corpus.Physics {
		dim := Judgement{Kind: "dimension", Verdict: Pass}
		if problems := Dimensions(solution); len(problems) > 0 {
			dim.Verdict = Fail
			dim.Text = strings.Join(problems, "\n")
		}
		judgements = append(judgements, dim)
	}

	for _, j := range judgements {
		if !j.Passed() {
			return judgements, false, nil
		}
	}
	return judgements, true, nil
}

func (e *Engine) log(format string, args ...any) {
	if e.Progress != nil {
		e.Progress(fmt.Sprintf(format, args...))
	}
}
