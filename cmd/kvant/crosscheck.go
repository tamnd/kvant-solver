package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/problems"
	"github.com/tamnd/kvant-solver/report"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
)

// runProblemsCrosscheck compares the recovered problems against the joined
// pages on kvant.digital and writes reports/problems-crosscheck.md.
//
// The site is asked once per problem and the answer is kept in the cache, so a
// second run over the same numbers touches the network for nothing and gives
// the same document. That matters more here than politeness: a report that
// changes because the run happened on a different afternoon is not a
// measurement of anything.
func runProblemsCrosscheck(args []string) error {
	fs := pflag.NewFlagSet("problems crosscheck", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	cacheDir := fs.String("cache", fetch.DefaultDir(), "where the fetched pages are kept")
	delay := fs.Duration("delay", source.DefaultDelay, "gap between two requests to one host")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the problems were written into")
	subject := fs.String("subject", "", "math or physics, both by default")
	limit := fs.Int("limit", 0, "stop after this many problems, all of them by default")
	again := fs.Bool("again", false, "ask the site again for problems already fetched")
	offline := fs.Bool("offline", false, "compare only what the cache already holds")
	out := fs.String("out", "", "write here instead of corpus/reports/problems-crosscheck.md")
	stdout := fs.Bool("stdout", false, "print the document instead of writing it")
	if err := fs.Parse(args); err != nil {
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
	m := &problems.Manifest{}
	if err := store.Read(problems.ManifestFile, m); err != nil {
		return fmt.Errorf("%w, run kvant problems build first", err)
	}
	cache, err := fetch.OpenCache(*cacheDir)
	if err != nil {
		return err
	}
	joins := problems.Joins{Dir: filepath.Join(cache.Dir, "crosscheck")}

	client := kvantdigital.New()
	client.Fetcher = source.NewClient()
	client.Fetcher.Delay = *delay

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	x := &problems.Crosscheck{}
	asked, held := 0, 0
	for _, e := range m.Entries {
		if ctx.Err() != nil {
			break
		}
		if *subject != "" && string(e.Subject) != *subject {
			continue
		}
		if *limit > 0 && len(x.Checks)+len(x.Absent) >= *limit {
			break
		}
		joined, ok, err := joins.Get(e.ID)
		if err != nil {
			return err
		}
		switch {
		case ok && !*again:
			held++
		case *offline:
			// Nothing cached and nothing to be fetched. The problem is not
			// counted either way, because a number nobody asked about is not a
			// number the site does not have.
			continue
		default:
			joined, err = ask(ctx, client, e)
			if err != nil {
				if source.Fatal(err) || ctx.Err() != nil {
					return err
				}
				// One page that will not load is not a run. It is reported and
				// left out rather than filed as a number the site does not
				// carry, which is a different thing and a thing this would
				// otherwise get wrong.
				fmt.Printf("  %s: %v\n", e.ID, err)
				continue
			}
			asked++
			if err := joins.Put(joined); err != nil {
				return err
			}
		}
		if joined.Absent {
			x.Absent = append(x.Absent, e.ID)
			continue
		}
		statement, err := ours(c, *lang, e)
		if err != nil {
			return err
		}
		x.Checks = append(x.Checks, problems.Cross(e, statement, joined))
	}

	t := x.Tally()
	fmt.Printf("%d problems compared, %d the site does not index, %d asked for and %d already held\n",
		t.Compared, t.Absent, asked, held)
	fmt.Printf("posed: %d of %d name the same issue, solved: %d of %d\n",
		t.Posed.Same, t.Posed.Checked(), t.Solved.Same, t.Solved.Checked())
	if t.Statements > 0 {
		fmt.Printf("%d statements compared, %.1f%% of their words ours does not have\n",
			t.Statements, t.Text.Rate()*100)
	}
	for _, c := range x.Disagreements() {
		if c.Posed == problems.Moved {
			fmt.Printf("  %s posed: we say %s, the site says %s\n", c.ID, c.OurPosed, c.TheirPosed)
		}
		if c.Solved == problems.Moved {
			fmt.Printf("  %s solved: we say %s, the site says %s\n", c.ID, c.OurSolved, c.TheirSolved)
		}
	}

	md := report.CrosscheckMarkdown(x, time.Now())
	if *stdout {
		fmt.Print(md)
		return nil
	}
	path := *out
	if path == "" {
		path = filepath.Join(c.Root, "reports", "problems-crosscheck.md")
	}
	if err := writeReport(path, md); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// ask fetches one problem and turns it into the source neutral form.
func ask(ctx context.Context, client *kvantdigital.Client, e problems.Entry) (problems.Joined, error) {
	letter := "m"
	if e.Subject == corpus.Physics {
		letter = "f"
	}
	p, err := client.Problem(ctx, letter, e.Number)
	if errors.Is(err, source.ErrNotFound) {
		// The site's problem index is a fraction of the historical numbering,
		// so a number it does not have is the usual answer and not a failure.
		return problems.Joined{ID: e.ID, Absent: true}, nil
	}
	if err != nil {
		return problems.Joined{}, err
	}
	j := problems.Joined{ID: p.ID, URL: p.URL, Condition: p.Condition.Text}
	if p.Solution != nil {
		j.Solution = p.Solution.Text
	}
	for _, ref := range p.Refs {
		switch ref.Kind {
		case kvantdigital.KindCondition:
			j.PosedIn = ref.IssueKey
		case kvantdigital.KindSolution:
			j.SolvedIn = ref.IssueKey
		}
	}
	return j, nil
}

// ours is the statement this corpus holds for a problem, or the empty string
// where it holds none.
//
// A problem whose statement has not been read has a manifest row and no file,
// which is the state that makes the site's reference worth having, so a missing
// file is an answer here rather than an error.
func ours(c *corpus.Corpus, lang string, e problems.Entry) (string, error) {
	if e.Posed == nil {
		return "", nil
	}
	id, err := corpus.ParseProblemID(e.ID)
	if err != nil {
		return "", err
	}
	// Unchecked, because this is about the words. A file whose hash has drifted
	// is somebody's edit and the audit is where that gets reported.
	body, err := corpus.LoadUnchecked(c.ProblemPath(lang, id), &corpus.ProblemFront{})
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return problems.Statement(body), nil
}
