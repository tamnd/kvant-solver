package main

import (
	"errors"
	"fmt"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/coverage"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/queue"
)

func runCoverage(args []string) error {
	fs := pflag.NewFlagSet("coverage", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	from := fs.Int("from", 0, "first year, for a range rather than a list")
	to := fs.Int("to", 0, "last year of that range")
	queueDir := fs.String("queue", "", "where the job queue lives, cache/queue by default")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to count")
	detail := fs.Bool("issues", false, "print every issue instead of a year at a time")
	todo := fs.Bool("todo", false, "print only the issues that are not finished")
	strict := fs.Bool("strict", false, "exit non zero unless everything selected is finished")
	if err := fs.Parse(args); err != nil {
		return err
	}

	issues, err := pickIssues(f, yearsBetween(*from, *to))
	if err != nil {
		return err
	}
	c, err := corpus.Open(*f.root)
	if err != nil {
		return err
	}
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	toc := &manifest.TOC{}
	if err := store.Read(manifest.TOCFile, toc); err != nil && !errors.Is(err, manifest.ErrMissing) {
		return err
	}
	// The queue is opened read only in spirit: nothing here leases or writes,
	// and a run that has never happened leaves an empty one, which counts zero
	// dead pages and is the right answer.
	jobs, err := queue.Open(dirOr(*queueDir, cache.Dir, "queue"))
	if err != nil {
		return err
	}
	dead, err := deadByIssue(jobs)
	if err != nil {
		return err
	}

	counted := make([]coverage.Issue, 0, len(issues))
	for i := range issues {
		row, err := count(c, cache, toc, dead, *lang, &issues[i])
		if err != nil {
			return err
		}
		counted = append(counted, row)
	}

	switch {
	case *todo:
		out := coverage.Outstanding(counted)
		if len(out) == 0 {
			fmt.Println("nothing outstanding")
			break
		}
		fmt.Print(coverage.IssueTable(out))
	case *detail:
		fmt.Print(coverage.IssueTable(counted))
	default:
		fmt.Print(coverage.Table(coverage.Group(counted)))
	}

	if *strict {
		if out := coverage.Outstanding(counted); len(out) > 0 {
			return fmt.Errorf("%d of %d issues are not finished", len(out), len(counted))
		}
	}
	return nil
}

// count is one issue looked up in every place that knows something about it.
func count(c *corpus.Corpus, cache *fetch.Cache, toc *manifest.TOC, dead map[string]int, lang string, iss *manifest.Issue) (coverage.Issue, error) {
	row := coverage.Issue{Key: iss.Key, Year: iss.Year, Number: iss.Number, Dead: dead[iss.Key]}

	key, err := corpus.ParseIssueKey(iss.Key)
	if err != nil {
		return row, err
	}
	idx, err := cache.ReadIndex(iss.Key)
	if err != nil {
		return row, err
	}
	row.Sheets = len(idx.Sheets)
	for i := range idx.Sheets {
		if idx.Sheets[i].Have() && cache.Has(idx.Sheets[i].SHA256) {
			row.Downloaded++
		}
	}
	pages, err := c.Pages(lang, key)
	if err != nil {
		return row, err
	}
	row.Pages = len(pages)
	articles, err := c.Articles(lang, key)
	if err != nil {
		return row, err
	}
	row.Articles = len(articles)
	if rows, ok := toc.Get(iss.Key); ok {
		row.Rows = len(rows)
	}
	return row, nil
}

// deadByIssue counts the pages that ran out of attempts, per issue.
//
// The queue holds one job per page and its target is the page id, so the issue
// has to be parsed back out of it. A target that does not parse is counted
// nowhere rather than reported: the OCR stage is not the only thing that will
// ever use this queue, and a translation job is not a page that failed.
func deadByIssue(jobs *queue.Queue) (map[string]int, error) {
	list, err := jobs.List(queue.StageOCR, queue.Dead)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, job := range list {
		id, err := corpus.ParsePageID(job.Target)
		if err != nil {
			continue
		}
		out[id.Issue.String()]++
	}
	return out, nil
}

// yearsBetween turns --from and --to into a test, and nothing into no test at
// all. One of the two on its own is an open ended range, which is how a run
// says everything since 1970.
func yearsBetween(from, to int) func(manifest.Issue) bool {
	if from == 0 && to == 0 {
		return nil
	}
	return func(iss manifest.Issue) bool {
		if from > 0 && iss.Year < from {
			return false
		}
		if to > 0 && iss.Year > to {
			return false
		}
		return true
	}
}
