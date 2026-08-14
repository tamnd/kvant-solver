package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
	"github.com/tamnd/kvant-solver/report"
)

func runReport(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("report needs a subcommand, which is failures or cost")
	}
	switch args[0] {
	case "failures":
		return runReportFailures(args[1:])
	case "cost":
		return runReportCost(args[1:])
	default:
		return fmt.Errorf("unknown report subcommand %q", args[0])
	}
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
	list, err := report.Failures(jobs)
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
			if (*from == 0 || entry.Year >= *from) && (*to == 0 || entry.Year <= *to) {
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
