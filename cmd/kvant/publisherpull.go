package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/publisher"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
)

// runPublisherPull downloads every piece of text the archive has typed, for
// keeping rather than for measuring.
//
// kvant publisher fetches a sample and compares it against what a model read
// off the scan of the same article, which is a measurement of the model. This
// fetches the lot. The two commands ask the site for the same kind of page and
// mean opposite things by it: there the typed text is evidence about our
// reading, and here it is the reading, and the model is the thing that does not
// have to run.
//
// It is safe to stop. Every article that arrives is written before the next one
// is asked for, and a second run skips what is already here, so an interrupted
// year costs the article it was in the middle of and nothing else.
//
// A third of the rows come back saying the site does not carry the text after
// all, and that is expected rather than a fault. Over 1975 and 1976 it was 53
// of 153, and 46 of those 53 are the problem columns, whose text the site
// serves on a problem page and not on an article page. kvant problems already
// fetches those by that route. The remaining seven are puzzle pages and the
// annual index, which have no running text to carry. Nothing here chases them,
// because a command that followed a second route on a guess would be fetching
// one thing under another thing's name.
func runPublisherPull(args []string) error {
	fs := pflag.NewFlagSet("publisher pull", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	from := fs.Int("from", 0, "first year to fetch, for a range rather than a list")
	to := fs.Int("to", 0, "last year of that range")
	again := fs.Bool("refetch", false, "fetch an article the store already holds")
	dry := fs.Bool("dry-run", false, "count what there is to fetch and ask the site for nothing")
	if err := fs.Parse(args); err != nil {
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
	all := publisher.WithText(toc, issues)
	if len(all) == 0 {
		fmt.Printf("no article in %d issues has publisher text\n", len(issues))
		return nil
	}

	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	text := publisher.Store{Dir: filepath.Join(cache.Dir, "publisher")}

	held, err := countHeld(text, all)
	if err != nil {
		return err
	}
	want := len(all) - held
	if *again {
		want = len(all)
	}
	fmt.Printf("%d articles the archive carries the text of over %d issues, %d already here, %d to fetch\n",
		len(all), len(issues), held, want)
	if *dry {
		for _, key := range issueOrder(all) {
			fmt.Printf("  %s: %d\n", key, len(byIssueKey(all)[key]))
		}
		return nil
	}

	client := kvantdigital.New()
	client.Fetcher = source.NewClient()
	client.Fetcher.Delay = *f.delay

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	groups := byIssueKey(all)
	var fetched, skipped, absent int
	for _, key := range issueOrder(all) {
		if ctx.Err() != nil {
			break
		}
		var got, had, none int
		for _, candidate := range groups[key] {
			if ctx.Err() != nil {
				break
			}
			if !*again {
				if _, ok, err := text.Get(candidate.Issue, candidate.Slug); err != nil {
					return err
				} else if ok {
					had++
					continue
				}
			}
			ok, err := pullText(ctx, client, text, candidate)
			switch {
			case err != nil:
				// One article that will not load is not a run. The contents
				// said the text was there and the page says otherwise, which is
				// worth printing and worth carrying on past.
				fmt.Printf("  %s %s: %v\n", candidate.Issue, candidate.Slug, err)
				none++
			case !ok:
				fmt.Printf("  %s %s: the contents said there is text and the page carries none\n",
					candidate.Issue, candidate.Slug)
				none++
			default:
				got++
			}
		}
		fetched, skipped, absent = fetched+got, skipped+had, absent+none
		if got > 0 || none > 0 {
			fmt.Printf("  %s: %d fetched, %d already here, %d the site does not carry\n", key, got, had, none)
		}
	}
	fmt.Printf("%d fetched, %d already here, %d the site does not carry after all, stored in %s\n",
		fetched, skipped, absent, text.Dir)
	if ctx.Err() != nil {
		fmt.Println("stopped early, run the same command again to carry on from here")
	}
	return nil
}

// pullText fetches one article's text and puts it in the store.
//
// A false with no error is the site not carrying the text after all, which the
// contents said it does. That is a fact about the archive rather than a fault
// in the run, and there are enough of them that stopping on one would mean
// never finishing a year.
func pullText(ctx context.Context, client *kvantdigital.Client, store publisher.Store, c publisher.Candidate) (bool, error) {
	article, err := client.Article(ctx, c.URL, c.Slug)
	if err != nil {
		return false, err
	}
	if !article.HasText {
		return false, nil
	}
	md, err := publisher.Markdown(article.Text)
	if err != nil {
		return false, err
	}
	return true, store.Put(c.Issue, c.Slug, md)
}

func countHeld(store publisher.Store, all []publisher.Candidate) (int, error) {
	n := 0
	for _, c := range all {
		_, ok, err := store.Get(c.Issue, c.Slug)
		if err != nil {
			return 0, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

func byIssueKey(all []publisher.Candidate) map[string][]publisher.Candidate {
	out := map[string][]publisher.Candidate{}
	for _, c := range all {
		out[c.Issue] = append(out[c.Issue], c)
	}
	return out
}

func issueOrder(all []publisher.Candidate) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range all {
		if !seen[c.Issue] {
			seen[c.Issue] = true
			out = append(out, c.Issue)
		}
	}
	sort.Strings(out)
	return out
}
