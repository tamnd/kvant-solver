package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
)

func runFetch(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("fetch needs a subcommand, which is pages or pdf")
	}
	switch args[0] {
	case "pages":
		return runFetchPages(args[1:])
	case "pdf":
		return runFetchPDF(args[1:])
	default:
		return fmt.Errorf("unknown fetch subcommand %q", args[0])
	}
}

// fetchFlags are what both fetch subcommands take. The cache is separate from
// the corpus and stays out of it: the corpus is text people read and this is
// twenty gigabytes of JPEG.
type fetchFlags struct {
	root   *string
	cache  *string
	delay  *time.Duration
	years  *[]int
	issues *[]string
}

func addFetchFlags(fs *pflag.FlagSet) fetchFlags {
	return fetchFlags{
		root:   fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout"),
		cache:  fs.String("cache", fetch.DefaultDir(), "where the downloaded scans live"),
		delay:  fs.Duration("delay", source.DefaultDelay, "gap between two requests to one host"),
		years:  fs.IntSlice("year", nil, "limit to these years, repeatable"),
		issues: fs.StringSlice("issue", nil, "limit to these issue keys, repeatable"),
	}
}

// selected reads the issue list and picks the issues a run is about.
func selected(f fetchFlags) ([]manifest.Issue, error) { return pickIssues(f, nil) }

// pickIssues is selected with an extra test, for the commands that take a range
// of years rather than a list of them. A nil test keeps everything the flags
// left.
func pickIssues(f fetchFlags, keep func(manifest.Issue) bool) ([]manifest.Issue, error) {
	store, err := manifest.Open(*f.root)
	if err != nil {
		return nil, err
	}
	issues := &manifest.Issues{}
	if err := store.Read(manifest.IssuesFile, issues); err != nil {
		if errors.Is(err, manifest.ErrMissing) {
			return nil, fmt.Errorf("no issue list yet, run kvant issues sync first")
		}
		return nil, err
	}

	var out []manifest.Issue
	for _, iss := range issues.Issues {
		if len(*f.years) > 0 && !slices.Contains(*f.years, iss.Year) {
			continue
		}
		if len(*f.issues) > 0 && !slices.Contains(*f.issues, iss.Key) {
			continue
		}
		if keep != nil && !keep(iss) {
			continue
		}
		out = append(out, iss)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no issues match, the list has %d", issues.Count)
	}
	return out, nil
}

// fetcher opens the cache and builds a client with the manners the flags ask
// for.
func fetcherFor(f fetchFlags) (*fetch.Fetcher, *fetch.Cache, error) {
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return nil, nil, err
	}
	client := source.NewClient()
	client.Delay = *f.delay
	fetcher := fetch.New(cache, client)
	fetcher.Log = func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) }
	return fetcher, cache, nil
}

func runFetchPages(args []string) error {
	fs := pflag.NewFlagSet("fetch pages", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	noProbe := fs.Bool("no-probe", false, "trust the thumbnail strip and do not look past the last sheet it names")
	if err := fs.Parse(args); err != nil {
		return err
	}
	issues, err := selected(f)
	if err != nil {
		return err
	}
	fetcher, cache, err := fetcherFor(f)
	if err != nil {
		return err
	}
	fetcher.Probe = !*noProbe
	fmt.Printf("%d issues into %s\n", len(issues), cache.Dir)

	ctx := context.Background()
	sheets := 0
	for i := range issues {
		iss := &issues[i]
		if iss.Sources.Digital == nil {
			fmt.Printf("%s: no scan anywhere, skipping\n", iss.Key)
			continue
		}
		idx, err := fetcher.Pages(ctx, iss)
		if idx != nil {
			// An issue that failed on sheet sixty still fetched fifty nine, and
			// they are in the cache whatever the error says.
			sheets += idx.Fetched()
		}
		if err != nil {
			// A run over a decade must not lose the nine issues it did because
			// the tenth broke, but a refusal is the one thing worth stopping the
			// whole run for.
			fmt.Fprintf(os.Stderr, "kvant: %v\n", err)
			if source.Fatal(err) {
				return err
			}
			continue
		}
	}

	files, bytes, err := cache.Size()
	if err != nil {
		return err
	}
	fmt.Printf("%d sheets over %d issues, the cache holds %d files and %s\n",
		sheets, len(issues), files, human(bytes))
	return nil
}

func runFetchPDF(args []string) error {
	fs := pflag.NewFlagSet("fetch pdf", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	issues, err := selected(f)
	if err != nil {
		return err
	}
	fetcher, cache, err := fetcherFor(f)
	if err != nil {
		return err
	}

	ctx := context.Background()
	got, missing := 0, 0
	for i := range issues {
		iss := &issues[i]
		if iss.Sources.MCCME == nil || iss.Sources.MCCME.PDFURL == "" {
			missing++
			continue
		}
		blob, err := fetcher.PDF(ctx, iss)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kvant: %v\n", err)
			if source.Fatal(err) {
				return err
			}
			continue
		}
		if blob != nil {
			got++
		}
	}

	files, bytes, err := cache.Size()
	if err != nil {
		return err
	}
	fmt.Printf("%d issue PDFs, %d issues the mirror has no PDF for, the cache holds %d files and %s\n",
		got, missing, files, human(bytes))
	return nil
}

// human is a byte count somebody can read at the end of an hour long run.
func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}
