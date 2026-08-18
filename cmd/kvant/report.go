package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/publisher"
	"github.com/tamnd/kvant-solver/queue"
	"github.com/tamnd/kvant-solver/report"
)

func runReport(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("report needs a subcommand, which is failures, cost, diff or refs")
	}
	switch args[0] {
	case "failures":
		return runReportFailures(args[1:])
	case "cost":
		return runReportCost(args[1:])
	case "diff":
		return runReportDiff(args[1:])
	case "refs":
		return runReportRefs(args[1:])
	default:
		return fmt.Errorf("unknown report subcommand %q", args[0])
	}
}

// runReportDiff writes reports/publisher-diff.md: how far the text the archive
// carries agrees with the text we read off the same pages, per year.
//
// It reads what kvant publisher fetched and asks the site for nothing, so it
// can be run again over the same sample and give the same answer, which is what
// makes the rate a measurement rather than a reading of the day.
func runReportDiff(args []string) error {
	fs := pflag.NewFlagSet("report diff", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	from := fs.Int("from", 0, "first year to compare")
	to := fs.Int("to", 0, "last year to compare")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the articles were read into")
	perYear := fs.Int("per-year", publisher.DefaultPerYear, "articles a year the sample was drawn at")
	out := fs.String("out", "", "write here instead of corpus/reports/publisher-diff.md")
	stdout := fs.Bool("stdout", false, "print the document instead of writing it")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*f.root)
	if err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	toc := &manifest.TOC{}
	if err := store.Read(manifest.TOCFile, toc); err != nil {
		return err
	}
	issues, err := pickIssues(f, yearsBetween(*from, *to))
	if err != nil {
		return err
	}
	candidates, err := publisher.Candidates(c, *lang, toc, issues)
	if err != nil {
		return err
	}
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	text := publisher.Store{Dir: filepath.Join(cache.Dir, "publisher")}

	var diffs []publisher.Diff
	for _, candidate := range publisher.Sample(candidates, *perYear) {
		theirs, ok, err := text.Get(candidate.Issue, candidate.Slug)
		if err != nil {
			return err
		}
		if !ok {
			// Not fetched is not a disagreement. Counting it as one would make
			// the rate a report on how much of the sample has been downloaded.
			continue
		}
		// Loaded rather than checked, because the comparison is about the words
		// and a file whose hash has drifted is a different complaint that the
		// audit already makes.
		ours, err := corpus.LoadUnchecked(candidate.File, &corpus.ArticleFront{})
		if err != nil {
			return err
		}
		count, examples := publisher.Compare(theirs, ours)
		diffs = append(diffs, publisher.Diff{
			Issue: candidate.Issue, Year: candidate.Year,
			Slug: candidate.Slug, Title: candidate.Title,
			Count: count, Examples: examples,
		})
	}

	md := report.DiffMarkdown(publisher.Years(diffs), diffs, *perYear, time.Now())
	if *stdout {
		fmt.Print(md)
		return nil
	}
	path := *out
	if path == "" {
		if *f.root == "" {
			return fmt.Errorf("no corpus, so nowhere to write, pass --corpus or --out")
		}
		path = filepath.Join(*f.root, "reports", "publisher-diff.md")
	}
	if err := writeReport(path, md); err != nil {
		return err
	}
	fmt.Printf("%d articles compared, written to %s\n", len(diffs), path)
	return nil
}

// runReportFailures writes reports/ocr-failures.md, which is the milestone's
// list of every page that never succeeded with the class of its failure.
func runReportFailures(args []string) error {
	fs := pflag.NewFlagSet("report failures", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	cacheDir := fs.String("cache", fetch.DefaultDir(), "where the downloaded scans live")
	queueDir := fs.String("queue", "", "where the job queue lives, cache/queue by default")
	from := fs.Int("from", 1970, "first year the report covers, for the heading")
	to := fs.Int("to", 1989, "last year of that range")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the pages were read into")
	out := fs.String("out", "", "write here instead of corpus/reports/ocr-failures.md")
	stdout := fs.Bool("stdout", false, "print the document instead of writing it")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cache, err := fetch.OpenCache(*cacheDir)
	if err != nil {
		return err
	}
	jobs, err := queue.Open(dirOr(*queueDir, cache.Dir, "queue"))
	if err != nil {
		return err
	}
	// The corpus is what says a page is finished. Without one the report can
	// only list the queue, which is a record of everything that ever went wrong
	// rather than of what is still wrong.
	var read func(corpus.PageID) bool
	if *root != "" {
		c, err := corpus.Open(*root)
		if err != nil {
			return err
		}
		read = func(id corpus.PageID) bool { return c.HasPage(*lang, id) }
	}
	list, err := report.Failures(jobs, read)
	if err != nil {
		return err
	}
	// The range is a filter and not only a heading. A machine that read 1975 and
	// 1998 holds both in one queue, and a report that says 1970 to 1989 has to
	// mean it.
	kept := list[:0]
	for _, fail := range list {
		if fail.Year >= *from && fail.Year <= *to {
			kept = append(kept, fail)
		}
	}
	md := report.FailureMarkdown(kept, *from, *to, time.Now())
	if *stdout {
		fmt.Print(md)
		return nil
	}
	path := *out
	if path == "" {
		if *root == "" {
			return fmt.Errorf("no corpus, so nowhere to write, pass --corpus or --out")
		}
		path = filepath.Join(*root, "reports", "ocr-failures.md")
	}
	if err := writeReport(path, md); err != nil {
		return err
	}
	fmt.Printf("%d pages that never read, written to %s\n", len(kept), path)
	return nil
}

func runReportCost(args []string) error {
	fs := pflag.NewFlagSet("report cost", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	cacheDir := fs.String("cache", fetch.DefaultDir(), "where the downloaded scans live")
	ledgerPath := fs.String("ledger", "", "the ledger to read, cache/ledger/ocr.jsonl by default")
	from := fs.Int("from", 0, "first year to count")
	to := fs.Int("to", 0, "last year to count")
	// Priced only when somebody says what the price is. Three of the four lanes
	// bill nothing per token and the fourth changes its rates, so a number baked
	// into the repo would be authoritative and wrong.
	in := fs.Float64("price-in", 0, "cost per million input tokens")
	outPrice := fs.Float64("price-out", 0, "cost per million output tokens")
	currency := fs.String("currency", "USD", "what those prices are in")
	md := fs.Bool("markdown", false, "write corpus/reports/ocr-cost.md instead of printing a table")
	out := fs.String("out", "", "write the document here instead")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cache, err := fetch.OpenCache(*cacheDir)
	if err != nil {
		return err
	}
	entries, err := ocr.ReadLedger(fileOr(*ledgerPath, cache.Dir, "ledger", "ocr.jsonl"))
	if err != nil {
		return err
	}
	if *from > 0 || *to > 0 {
		kept := entries[:0]
		for _, entry := range entries {
			year := entry.PageYear()
			if (*from == 0 || year >= *from) && (*to == 0 || year <= *to) {
				kept = append(kept, entry)
			}
		}
		entries = kept
	}
	spends := report.Cost(entries)
	price := report.Price{Input: *in, Output: *outPrice, Currency: *currency}

	if !*md && *out == "" {
		if len(spends) == 0 {
			fmt.Println("the ledger is empty, so nothing has been read on this machine")
			return nil
		}
		fmt.Print(report.CostTable(spends, price))
		return nil
	}
	path := *out
	if path == "" {
		if *root == "" {
			return fmt.Errorf("no corpus, so nowhere to write, pass --corpus or --out")
		}
		path = filepath.Join(*root, "reports", "ocr-cost.md")
	}
	if err := writeReport(path, report.CostMarkdown(spends, price, time.Now())); err != nil {
		return err
	}
	fmt.Printf("%d years of reading, written to %s\n", len(spends), path)
	return nil
}
