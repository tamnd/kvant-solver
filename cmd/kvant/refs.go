package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/refs"
	"github.com/tamnd/kvant-solver/source/mathnetru"
)

func runRefs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("refs needs a subcommand, which is build, show or mathnet")
	}
	switch args[0] {
	case "build":
		return runRefsBuild(args[1:])
	case "show":
		return runRefsShow(args[1:])
	case "mathnet":
		return runRefsMathNet(args[1:])
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

// runRefsMathNet writes manifests/refs/mathnet.yaml, which ties every Kvant
// article mathnet.ru holds to our tag for the same article.
//
// It fetches one page per issue and none per article. The issue page already
// carries the title, the byline, the page range and the permanent identifier,
// so going to the article pages would be six thousand requests for nothing.
//
// It takes the bibliography and nothing else. Mathnet has full text behind a
// good part of this range and none of it is read, because the archive has its
// own text and what this source is worth is the identifier.
//
// The run resumes. An issue already in the manifest is not asked for again, and
// an issue the server will not answer for is reported and skipped rather than
// taken as a reason to throw away the ones that did answer. Both exist because
// the server times out often enough that a hundred and sixty three requests in
// a row does not finish, and refetching what we already have to get at what we
// do not is the rudest possible way to ask.
func runRefsMathNet(args []string) error {
	fs := pflag.NewFlagSet("refs mathnet", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to match against")
	from := fs.Int("from", 0, "first year to fetch, all of them by default")
	to := fs.Int("to", 0, "last year of that range")
	pause := fs.Duration("pause", time.Second, "wait this long between issue pages")
	refresh := fs.Bool("refresh", false, "ask the site again for issues the manifest already holds")
	dry := fs.Bool("dry-run", false, "print the counts and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	idx, err := refs.LoadMathNet(c, *lang)
	if err != nil {
		return err
	}
	store, err := refs.Store(c)
	if err != nil {
		return err
	}
	held := map[string][]mathnetru.PaperRef{}
	if !*refresh {
		old, err := refs.ReadMathNet(store)
		if err != nil {
			return err
		}
		held = old.Held()
	}

	ctx := context.Background()
	client := mathnetru.New()
	issues, err := client.Contents(ctx)
	if err != nil {
		return err
	}
	kept := issues[:0]
	for _, issue := range issues {
		if (*from == 0 || issue.Year >= *from) && (*to == 0 || issue.Year <= *to) {
			kept = append(kept, issue)
		}
	}
	issues = kept
	sort.SliceStable(issues, func(a, b int) bool { return issues[a].Year < issues[b].Year })

	papers := map[string][]mathnetru.PaperRef{}
	var resumed int
	var dead []string
	for n, issue := range issues {
		key := fmt.Sprintf("kvant_%d_%s", issue.Year, issue.Number)
		if got, ok := held[key]; ok {
			papers[key], resumed = got, resumed+1
			continue
		}
		if n > 0 {
			time.Sleep(*pause)
		}
		got, err := client.Issue(ctx, issue.Year, issue.Query)
		if err != nil {
			fmt.Printf("\r%s: %v\n", key, err)
			dead = append(dead, key)
			continue
		}
		papers[key] = got
		fmt.Printf("\r%d of %d issues, %d articles", n+1, len(issues), countPapers(papers))
	}
	fmt.Println()
	if resumed > 0 {
		fmt.Printf("%d issues came out of the manifest and were not asked for again\n", resumed)
	}

	m := refs.BuildMathNet(issues, papers, idx)
	if err := m.Check(); err != nil {
		return err
	}
	counts := m.Counts()
	years := m.Years()
	if len(years) == 0 {
		return fmt.Errorf("no issue answered, so there is nothing to write")
	}
	fmt.Printf("%d articles on mathnet, %d to %d\n", m.Count, years[0], years[len(years)-1])
	fmt.Printf("  tied to one of ours: %d\n", counts[refs.MathNetLinked])
	fmt.Printf("  in an issue the corpus does not have: %d\n", counts[refs.MathNetUnread])
	fmt.Printf("  in an issue only part of which is assembled: %d\n", counts[refs.MathNetUnassembled])
	fmt.Printf("  in an issue we hold in full, matched to nothing: %d\n", counts[refs.MathNetUnmatched])
	if *dry {
		return nil
	}
	if err := refs.SaveMathNet(store, m); err != nil {
		return err
	}
	// Written first, then complained about. The whole point of getting this far
	// with issues missing is that the next run starts from what this one got.
	if len(dead) > 0 {
		return fmt.Errorf("%d issues the server would not answer for, run again to pick them up: %s",
			len(dead), strings.Join(dead, " "))
	}
	return nil
}

// countPapers is the running total for the progress line.
func countPapers(papers map[string][]mathnetru.PaperRef) int {
	n := 0
	for _, list := range papers {
		n += len(list)
	}
	return n
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
