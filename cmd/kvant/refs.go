package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/refs"
)

func runRefs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("refs needs a subcommand, which is build or show")
	}
	switch args[0] {
	case "build":
		return runRefsBuild(args[1:])
	case "show":
		return runRefsShow(args[1:])
	default:
		return fmt.Errorf("unknown refs subcommand %q", args[0])
	}
}

// runRefsBuild finds what the articles of a year cite and writes the resolved
// graph to manifests/refs/<year>.yaml.
//
// It is safe to run again over a year that already has a graph, and it has to
// be: a reference into 1983 cannot resolve until 1983 is read, so every year
// already built goes a little further every time another year lands. That is
// what --all is for.
func runRefsBuild(args []string) error {
	fs := pflag.NewFlagSet("refs build", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to read")
	year := fs.IntSlice("year", nil, "limit to these years, repeatable")
	from := fs.Int("from", 0, "first year, for a range rather than a list")
	to := fs.Int("to", 0, "last year of that range")
	all := fs.Bool("all", false, "every year the corpus has articles for")
	dry := fs.Bool("dry-run", false, "print the counts and write nothing")
	strict := fs.Bool("strict", false, "exit non zero if the unresolved rate is over the threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	years, err := pickYears(c, *lang, *year, *from, *to, *all)
	if err != nil {
		return err
	}
	idx, err := refs.Load(c, *lang)
	if err != nil {
		return err
	}
	graphs, err := refs.Build(c, *lang, idx, years)
	if err != nil {
		return err
	}

	if !*dry {
		store, err := refs.Store(c)
		if err != nil {
			return err
		}
		for _, y := range years {
			if err := refs.Save(store, graphs[y]); err != nil {
				return err
			}
		}
	}

	counts := refs.Counts(graphs)
	total := 0
	for _, n := range counts {
		total += n
	}
	rate, ok := refs.OK(graphs)
	fmt.Printf("%d citations over %d years: %d linked, %d to an issue, %d pending, %d unresolved\n",
		total, len(years), counts[refs.Linked], counts[refs.Issue], counts[refs.Pending], counts[refs.Unresolved])
	fmt.Printf("%.1f%% unresolved, and the threshold is %.0f%%\n", rate*100, refs.Threshold*100)
	if *strict && !ok {
		return fmt.Errorf("%.1f%% of citations resolved to nothing, over the %.0f%% threshold",
			rate*100, refs.Threshold*100)
	}
	return nil
}

// runRefsShow prints what one article cites and where each citation landed,
// which is how a person checks the matching by hand against the printed page.
func runRefsShow(args []string) error {
	fs := pflag.NewFlagSet("refs show", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to read")
	status := fs.String("status", "", "print only references with this status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("give a year, or a tag or a label of an article")
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	store, err := refs.Store(c)
	if err != nil {
		return err
	}
	for _, what := range rest {
		graphs, err := refs.Lookup(store, c, *lang, what)
		if err != nil {
			return err
		}
		for _, graph := range graphs {
			for _, ref := range graph.Refs {
				if *status != "" && string(ref.Status) != *status {
					continue
				}
				fmt.Println(" ", refs.Line(ref))
			}
		}
	}
	return nil
}

// runReportRefs writes reports/refs-unresolved.md out of the graphs already on
// disk, so that the report is of the last build rather than of a fresh one that
// might disagree with what was committed.
func runReportRefs(args []string) error {
	fs := pflag.NewFlagSet("report refs", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	out := fs.String("out", "", "write here instead of corpus/reports/refs-unresolved.md")
	stdout := fs.Bool("stdout", false, "print the document instead of writing it")
	strict := fs.Bool("strict", false, "exit non zero if the unresolved rate is over the threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	store, err := refs.Store(c)
	if err != nil {
		return err
	}
	graphs, err := refs.ReadAll(store)
	if err != nil {
		return err
	}
	if len(graphs) == 0 {
		return fmt.Errorf("no graphs in manifests/refs, run kvant refs build first")
	}

	md := refs.Report(graphs, time.Now())
	if *stdout {
		fmt.Print(md)
		return nil
	}
	path := *out
	if path == "" {
		if *root == "" {
			return fmt.Errorf("no corpus, so nowhere to write, pass --corpus or --out")
		}
		path = filepath.Join(*root, "reports", "refs-unresolved.md")
	}
	if err := writeReport(path, md); err != nil {
		return err
	}
	rate, ok := refs.OK(graphs)
	fmt.Printf("%d years, %.1f%% unresolved, written to %s\n", len(graphs), rate*100, path)
	if *strict && !ok {
		return fmt.Errorf("%.1f%% of citations resolved to nothing, over the %.0f%% threshold",
			rate*100, refs.Threshold*100)
	}
	return nil
}

// pickYears turns the four ways of naming a set of years into one list.
//
// Nothing given is not everything. A bare kvant refs build over a corpus with
// forty years in it is an hour of work somebody did not ask for, so it is an
// error that says what to pass instead.
func pickYears(c *corpus.Corpus, lang string, list []int, from, to int, all bool) ([]int, error) {
	have, err := refs.Years(c, lang)
	if err != nil {
		return nil, err
	}
	if len(have) == 0 {
		return nil, fmt.Errorf("no articles in the %s tree, so there is nothing to read", lang)
	}
	if all {
		return have, nil
	}
	if len(list) > 0 {
		sort.Ints(list)
		return list, nil
	}
	if from == 0 && to == 0 {
		return nil, fmt.Errorf("say which years, with --year, --from and --to, or --all for the %d the corpus has", len(have))
	}
	var out []int
	for _, year := range have {
		if from > 0 && year < from {
			continue
		}
		if to > 0 && year > to {
			continue
		}
		out = append(out, year)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no year the corpus has articles for is in that range")
	}
	return out, nil
}
