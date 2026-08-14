package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sort"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
	"github.com/tamnd/kvant-solver/report"
)

// runRepair reads the pages the first lane could not, through a second one.
//
// A decade is read by the card because the card is the only thing fast enough,
// and the card leaves a tail: pages where it welds two alphabets into a word,
// pages where the folio band will not resolve, pages it simply gives up on.
// Asking the card again buys nothing, because the sampling is pinned and the
// answer is the same one. So the tail goes to a model that reads instructions,
// at a hundred times the cost a page and on one or two percent of the pages,
// which is the whole reason the split is worth having.
//
// The work list is the queue and not a person. Every dead OCR job is a page
// that failed, the queue knows which and why, and this command turns that into
// a second pass without anybody copying sheet numbers out of a report.
func runRepair(args []string) error {
	fs := pflag.NewFlagSet("repair", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	from := fs.Int("from", 0, "first year to repair, for a range rather than a list")
	to := fs.Int("to", 0, "last year of that range")
	queueDir := fs.String("queue", "", "where the job queue lives, cache/queue by default")
	work := fs.String("work", "", "where page images are staged, cache/work by default")
	// The defaults are the other lane. Somebody running this has already had the
	// card fail on these pages.
	engineFlags := addLaneFlags(fs, "cli")
	workers := fs.Int("workers", 1, "pages in flight at once")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the pages are written into")
	checkTeX := fs.Bool("latex", true, "run every formula through KaTeX before accepting a page")
	ledgerPath := fs.String("ledger", "", "where the cost ledger is appended, cache/ledger/ocr.jsonl by default")
	noLedger := fs.Bool("no-ledger", false, "do not record what the run cost")
	rules := fs.StringSlice("rule", nil, "repair only the pages a named rule killed, repeatable")
	limit := fs.Int("limit", 0, "stop after this many pages, for a run somebody is watching")
	dry := fs.Bool("dry-run", false, "print the work list and read nothing")
	if err := fs.Parse(args); err != nil {
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
	jobs, err := queue.Open(dirOr(*queueDir, cache.Dir, "queue"))
	if err != nil {
		return err
	}
	failures, err := report.Failures(jobs)
	if err != nil {
		return err
	}

	wanted, err := wantedIssues(f, yearsBetween(*from, *to))
	if err != nil {
		return err
	}
	// The job id goes with the sheet number. The queue names a job by a hash of
	// its target and its prompt, so nothing downstream can work out which file
	// to reset from a page number alone, and the list that found the dead pages
	// is the one place that already knows.
	todo := map[string][]int{}
	dead := map[string]string{}
	for _, fail := range failures {
		if _, ok := wanted[fail.Issue]; !ok {
			continue
		}
		if len(*rules) > 0 && !named(fail, *rules) {
			continue
		}
		todo[fail.Issue] = append(todo[fail.Issue], fail.Sheet)
		dead[fail.Target] = fail.ID
	}
	if len(todo) == 0 {
		fmt.Println("nothing dead in the range, so nothing to repair")
		return nil
	}

	keys := make([]string, 0, len(todo))
	pages := 0
	for key, sheets := range todo {
		sort.Ints(sheets)
		keys = append(keys, key)
		pages += len(sheets)
	}
	sort.Strings(keys)
	fmt.Printf("%d pages over %d issues\n", pages, len(keys))
	if *dry {
		for _, key := range keys {
			fmt.Printf("  %s: %v\n", key, todo[key])
		}
		return nil
	}

	reader, err := engineFlags.build()
	if err != nil {
		return err
	}
	options := ocr.Options{}
	if *checkTeX {
		renderer, err := katex.New()
		if err != nil {
			return err
		}
		options.LaTeX = renderer
	}
	var ledger *ocr.Ledger
	if !*noLedger {
		ledger, err = ocr.OpenLedger(fileOr(*ledgerPath, cache.Dir, "ledger", "ocr.jsonl"))
		if err != nil {
			return err
		}
		defer func() { _ = ledger.Close() }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	done := 0
	for _, issueKey := range keys {
		if ctx.Err() != nil {
			break
		}
		if *limit > 0 && done >= *limit {
			fmt.Printf("stopping at the %d page limit, %d pages are still dead\n", *limit, pages-done)
			break
		}
		iss := wanted[issueKey]
		key, err := corpus.ParseIssueKey(issueKey)
		if err != nil {
			return err
		}
		idx, err := cache.ReadIndex(issueKey)
		if err != nil {
			return err
		}
		staged, err := sheetsFor(cache, idx, "", dirOr(*work, cache.Dir, "work"))
		if err != nil {
			return err
		}
		sheets := pick(staged, todo[issueKey])
		if len(sheets) == 0 {
			fmt.Printf("%s: the dead sheets are not downloaded here, skipping\n", issueKey)
			continue
		}
		if *limit > 0 && done+len(sheets) > *limit {
			sheets = sheets[:*limit-done]
		}

		runner := &ocr.Runner{
			Queue: jobs, Engine: reader.Engine, Corpus: c,
			Lang: *lang, Issue: key,
			Source: scanSource(iss), Scan: idx.Issue,
			Prompt: reader.Prompt, PromptSHA256: reader.SHA256,
			Folio:   reader.Folio,
			Options: options, Workers: *workers, Ledger: ledger,
			Logf: func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) },
		}
		// The dead jobs go back to pending. A page that already has a page file
		// is left alone, which is revive's own rule and the right one: the corpus
		// is what has to be complete, and re-reading a page that is already good
		// spends the expensive lane on nothing.
		revived, err := revive(jobs, runner, c, *lang, key, sheets, dead)
		if err != nil {
			return err
		}
		if revived == 0 {
			continue
		}
		if _, err := runner.Enqueue(sheets); err != nil {
			return err
		}
		fmt.Printf("%s: %d pages through %s\n", issueKey, revived, reader.Engine.Name())
		summary, err := runner.Run(ctx)
		fmt.Printf("%s: %s\n", issueKey, summary)
		if err != nil {
			return err
		}
		done += revived
	}
	return nil
}

// wantedIssues is the selection as a lookup, because the work list here comes
// from the queue and has to be checked against the flags rather than built from
// them.
func wantedIssues(f fetchFlags, keep func(manifest.Issue) bool) (map[string]*manifest.Issue, error) {
	issues, err := pickIssues(f, keep)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*manifest.Issue, len(issues))
	for i := range issues {
		out[issues[i].Key] = &issues[i]
	}
	return out, nil
}

// named says whether a failure was caused by one of the rules asked for. It is
// how a run says repair the folio failures and leave the rest, which is worth
// having when one class is a render problem with a known fix and another is not.
func named(fail report.Failure, rules []string) bool {
	for _, rule := range fail.Rules {
		if slices.Contains(rules, string(rule)) {
			return true
		}
	}
	return false
}
