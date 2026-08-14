package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/publisher"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
)

// runPublisher downloads the text the archive already holds, for the articles
// this corpus has read for itself.
//
// It is its own stage rather than part of a report because it is the only part
// of the comparison that touches the network. A report is built from what the
// run wrote and can be built again on a train; this asks a charity's server for
// a page at a time with a delay between, and it stops where it got to.
func runPublisher(args []string) error {
	fs := pflag.NewFlagSet("publisher", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	from := fs.Int("from", 0, "first year to fetch, for a range rather than a list")
	to := fs.Int("to", 0, "last year of that range")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the articles were read into")
	perYear := fs.Int("per-year", publisher.DefaultPerYear, "articles to sample a year, zero for all of them")
	again := fs.Bool("refetch", false, "fetch an article the cache already holds")
	dry := fs.Bool("dry-run", false, "print the sample and ask for nothing")
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
		if errors.Is(err, manifest.ErrMissing) {
			return fmt.Errorf("no contents yet, run kvant issues sync first")
		}
		return err
	}
	issues, err := pickIssues(f, yearsBetween(*from, *to))
	if err != nil {
		return err
	}
	all, err := publisher.Candidates(c, *lang, toc, issues)
	if err != nil {
		return err
	}
	sample := publisher.Sample(all, *perYear)
	fmt.Printf("%d articles the archive carries the text of and we have read, %d sampled\n", len(all), len(sample))
	if *dry {
		for _, candidate := range sample {
			fmt.Printf("  %s %s\n", candidate.Issue, candidate.Slug)
		}
		return nil
	}

	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	text := publisher.Store{Dir: filepath.Join(cache.Dir, "publisher")}
	client := kvantdigital.New()
	client.Fetcher = source.NewClient()
	client.Fetcher.Delay = *f.delay

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fetched, held, missing := 0, 0, 0
	for _, candidate := range sample {
		if ctx.Err() != nil {
			break
		}
		if !*again {
			if _, ok, err := text.Get(candidate.Issue, candidate.Slug); err != nil {
				return err
			} else if ok {
				held++
				continue
			}
		}
		article, err := client.Article(ctx, candidate.URL, candidate.Slug)
		if err != nil {
			// One article that will not load is not a run. The contents said
			// the text was there and the page says otherwise, which is worth
			// printing and worth carrying on past.
			fmt.Printf("  %s %s: %v\n", candidate.Issue, candidate.Slug, err)
			missing++
			continue
		}
		if !article.HasText {
			fmt.Printf("  %s %s: the contents said there is text and the page carries none\n",
				candidate.Issue, candidate.Slug)
			missing++
			continue
		}
		md, err := publisher.Markdown(article.Text)
		if err != nil {
			return err
		}
		if err := text.Put(candidate.Issue, candidate.Slug, md); err != nil {
			return err
		}
		fetched++
	}
	fmt.Printf("%d fetched, %d already here, %d the site does not carry after all\n", fetched, held, missing)
	return nil
}
