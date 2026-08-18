package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/pricing"
	"github.com/tamnd/kvant-solver/problems"
	"github.com/tamnd/kvant-solver/solve"
)

func runSolve(args []string) error {
	fs := pflag.NewFlagSet("solve", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", "ru", "which language to solve from")
	setName := fs.String("set", "smoke", "the evaluation set to run")
	only := fs.StringSlice("id", nil, "solve these problems instead of a set, for example M311")
	mode := fs.String("mode", "slow", "fast for one candidate and no judges, slow for the real thing")
	model := fs.String("model", "gpt-5", "which model to solve with")
	endpoint := fs.String("endpoint", "http://127.0.0.1:8077/v1/chat/completions", "the chatgpt-tool serve endpoint")
	key := fs.String("key", os.Getenv("KVANT_API_KEY"), "API key for the endpoint")
	candidates := fs.Int("candidates", 3, "independent attempts before one is selected")
	corrections := fs.Int("corrections", 1, "how many times a rejected solution may be repaired")
	timeout := fs.Duration("timeout", 10*time.Minute, "per call timeout")
	grade := fs.Bool("grade", true, "mark each solution against the one the magazine printed")
	write := fs.Bool("write", false, "write the solutions and the scorecard into the corpus")
	limit := fs.Int("limit", 0, "stop after this many problems, 0 for all of them")
	if err := fs.Parse(args); err != nil {
		return err
	}

	opts, err := solve.Mode(*mode).Apply(solve.Options{
		Model:          *model,
		Candidates:     *candidates,
		MaxCorrections: *corrections,
	})
	if err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	store, err := manifest.Open(c.Root)
	if err != nil {
		return err
	}

	ids, err := membership(store, *setName, *only)
	if err != nil {
		return err
	}
	if *limit > 0 && *limit < len(ids) {
		ids = ids[:*limit]
	}
	if len(ids) == 0 {
		return fmt.Errorf("nothing to solve")
	}

	// The rate card is corpus metadata, so the money in a scorecard carries the
	// day it was copied and where from. A corpus without one still gets the
	// token accounting, which is the half that cannot go stale.
	var rates *pricing.Card
	if store.Exists(manifest.PricingFile) {
		rates = &pricing.Card{}
		if err := store.Read(manifest.PricingFile, rates); err != nil {
			return err
		}
	}
	account := pricing.New(rates)

	engine := &solve.Engine{
		// The meter wraps the client rather than living in the engine, because
		// every call the run makes goes through here: the reference call, the
		// candidates, the selector, both judges, the repairs and the grader.
		Client: &pricing.Meter{
			Account: account,
			Client: &api.Client{
				URL:        *endpoint,
				APIKey:     *key,
				HTTPClient: &http.Client{Timeout: *timeout + time.Minute},
				UserAgent:  "kvant/" + version,
			},
		},
		Progress: func(line string) { fmt.Println("  " + line) },
	}

	card := solve.Scorecard{Set: *setName, Model: *model, Mode: solve.Mode(*mode)}
	if len(*only) > 0 {
		card.Set = "ad hoc, " + strings.Join(*only, " ")
	}
	ctx := context.Background()
	for i, id := range ids {
		fmt.Printf("[%d/%d] %s\n", i+1, len(ids), id)
		problem, published, err := readProblem(c, *lang, id)
		if err != nil {
			// One unreadable problem does not stop a set. The run is worth more
			// than the problem, and the scorecard is what says how much of the
			// set actually ran.
			fmt.Printf("  skipped: %v\n", err)
			continue
		}

		res, err := engine.Solve(ctx, problem, opts)
		if err != nil {
			fmt.Printf("  failed: %v\n", err)
			continue
		}

		marking := solve.Marking{ID: id, Grade: solve.Ungraded}
		if *grade {
			// Grading happens here, after Solve has returned, and this is the
			// only place in the program where the printed solution is read into
			// a variable that a model will see.
			marking, err = engine.Grade(ctx, problem, res, published, opts)
			if err != nil {
				fmt.Printf("  not marked: %v\n", err)
				marking = solve.Marking{ID: id, Grade: solve.Ungraded, Notes: err.Error()}
			}
		}
		card.Add(res, marking)
		fmt.Printf("  %s, %s, %s\n", res.Verdict, marking.Grade, res.Elapsed)

		if *write {
			if err := writeSolution(c, *lang, id, *setName, res, marking); err != nil {
				return err
			}
		}
	}

	card.Sort()
	report := card.Report() + account.Report()
	fmt.Println()
	fmt.Println(report)

	if !*write {
		return nil
	}
	path := filepath.Join(c.Root, "reports", "scorecard.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// membership works out which problems to run.
func membership(store *manifest.Store, setName string, only []string) ([]string, error) {
	if len(only) > 0 {
		return only, nil
	}
	set := &problems.Set{}
	if err := store.Read(problems.SetFile(setName), set); err != nil {
		return nil, fmt.Errorf("%w, run kvant eval --set %s --write first", err, setName)
	}
	return set.Members, nil
}

// readProblem loads one problem and returns the statement and, separately, the
// solution the magazine printed.
//
// The two come back as two values rather than one struct because the statement
// goes into solve.Problem and the printed solution does not go anywhere near
// it. Anything that reads a problem out of the corpus has to decide, at this
// line, what it is handing to the solver.
func readProblem(c *corpus.Corpus, lang, id string) (solve.Problem, string, error) {
	parsed, err := corpus.ParseProblemID(id)
	if err != nil {
		return solve.Problem{}, "", err
	}
	var front corpus.ProblemFront
	body, err := corpus.LoadUnchecked(c.ProblemPath(lang, parsed), &front)
	if err != nil {
		return solve.Problem{}, "", err
	}
	statement := problems.Statement(body)
	if strings.TrimSpace(statement) == "" {
		return solve.Problem{}, "", fmt.Errorf("%s has no statement in it", id)
	}
	// The year is off the issue that set the problem, not the one that printed
	// the answer, and it is left at 0 when the key does not parse. Only the
	// grader is shown it, and it drops its last question rather than ask about a
	// year it was not given.
	var year int
	if key, err := corpus.ParseIssueKey(front.PosedIn); err == nil {
		year = key.Year
	}
	return solve.Problem{
		ID:      front.ID,
		Subject: front.Subject,
		Text:    statement,
		Year:    year,
	}, problems.PublishedSolution(body), nil
}

// writeSolution files our answer under content/solutions/, which is a different
// tree from the one the scans go into.
func writeSolution(c *corpus.Corpus, lang, id, run string, res solve.Result, m solve.Marking) error {
	parsed, err := corpus.ParseProblemID(id)
	if err != nil {
		return err
	}
	path := c.SolutionPath(lang, parsed)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	front := corpus.SolutionFront{
		ID:                     res.ID,
		Subject:                res.Subject,
		SolvedBy:               res.Model,
		SolverRun:              run,
		Verified:               res.Verified(),
		GradedAgainstPublished: m.Grade != solve.Ungraded,
		Agreement:              strings.ToLower(m.Grade),
		RightAnswer:            rightAnswer(m),
		Anachronism:            m.Anachronism,
		// Extraction is left empty on purpose. The three it knows about are
		// native, publisher and vision, and all three mean text was recovered
		// from a page. Nothing was recovered here: this file was written by a
		// model that was shown a problem, and claiming one of the reading paths
		// would put a generated solution in the same bucket as a transcription.
		Provenance: corpus.Provenance{Lang: lang, Source: "kvant-solver"},
	}
	return corpus.Save(path, &front, body(res, m))
}

// body is the solution file. The judgements and the marking go in with it,
// because a solution in an archive that does not say how far it was checked is
// asking to be quoted as though it were the magazine's own.
func body(res solve.Result, m solve.Marking) string {
	var b strings.Builder
	b.WriteString("## Решение\n\n")
	b.WriteString(strings.TrimSpace(res.Solution))
	b.WriteString("\n\n## Проверка\n\n")
	if len(res.Judgements) == 0 {
		b.WriteString("Решение не проверялось.\n")
	}
	for _, j := range res.Judgements {
		fmt.Fprintf(&b, "- %s: %s\n", j.Kind, j.Verdict)
	}
	if m.Grade != "" && m.Grade != solve.Ungraded {
		fmt.Fprintf(&b, "\nСверено с опубликованным решением: %s.\n", strings.ToLower(m.Grade))
	}
	if m.Overturns() {
		b.WriteString("\nОтветы разошлись, и проверяющий счёл верным наш.\n")
	}
	if m.Anachronism != "" {
		fmt.Fprintf(&b, "\nИспользован метод, которого у журнала тогда не было: %s.\n", m.Anachronism)
	}
	return b.String()
}

// rightAnswer is the adjudication as it goes into the file, and it is left out
// where it says nothing the mark does not. BOTH is what a correct solution gets
// and UNCLEAR is the grader declining to answer, so writing either would add a
// field to every solution file that a reader has to learn to skip.
func rightAnswer(m solve.Marking) string {
	switch m.Right {
	case solve.Marked, solve.Printed, solve.Neither:
		return strings.ToLower(m.Right)
	}
	return ""
}
