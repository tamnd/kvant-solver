package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/pdfsrc"
	"github.com/tamnd/kvant-solver/textguard"
)

func runTextguard(args []string) error {
	fs := pflag.NewFlagSet("textguard", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	all := fs.Bool("all", false, "every issue there is, rather than a named year")
	report := fs.String("report", "", "where the report goes, reports/paths.md in the corpus by default")
	price := textguard.DefaultPrice
	fs.IntVar(&price.InputTokens, "input-tokens", price.InputTokens, "input tokens a vision page is assumed to take")
	fs.IntVar(&price.OutputTokens, "output-tokens", price.OutputTokens, "output tokens a vision page is assumed to produce")
	fs.Float64Var(&price.InputRate, "input-rate", price.InputRate, "dollars per million input tokens")
	fs.Float64Var(&price.OutputRate, "output-rate", price.OutputRate, "dollars per million output tokens")
	fs.Float64Var(&price.SecondsPerPage, "seconds-per-page", price.SecondsPerPage, "seconds a vision page takes on one lane")
	fs.IntVar(&price.Lanes, "lanes", price.Lanes, "how many lanes run at once")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Deciding the path for fifty years reads every page manifest and every
	// issue PDF in the cache, so it is asked for rather than defaulted to.
	if !*all && len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name a --year or an --issue, or say --all for the whole archive")
	}

	issues, err := selected(f)
	if err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}

	// The contents are what say whether the publisher already carries the text
	// of an article, and a run without them is a run that prices every page as
	// vision. That is a worse estimate rather than a wrong one, so it is a note
	// and not a failure.
	toc := &manifest.TOC{}
	if err := store.Read(manifest.TOCFile, toc); err != nil {
		if !errors.Is(err, manifest.ErrMissing) {
			return err
		}
		fmt.Fprintln(os.Stderr, "kvant: no contents yet, so no page can take the publisher path. Run kvant issues sync --deep.")
	}

	if err := needPoppler(cache, issues); err != nil {
		return err
	}

	// The old file is read first so that a run over one year updates that year
	// and leaves the rest of the archive alone.
	paths := &manifest.Paths{}
	if err := store.Read(manifest.PathsFile, paths); err != nil && !errors.Is(err, manifest.ErrMissing) {
		return err
	}

	g := textguard.New(cache)
	g.Log = func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) }
	ctx := context.Background()
	for i := range issues {
		iss := &issues[i]
		rows, _ := toc.Get(iss.Key)
		out, err := g.Issue(ctx, iss, rows)
		if err != nil {
			return err
		}
		paths.Set(out)
	}
	paths.Sort()

	if err := store.Write(manifest.PathsFile, paths,
		"How every page of every issue is going to be read, and what was measured to decide that.",
		"Written by kvant textguard. The page by page decisions live in the page manifests next to the scans."); err != nil {
		return err
	}

	out := *report
	if out == "" {
		// The store is the manifests directory of the checkout, so its parent
		// is the checkout.
		out = filepath.Join(filepath.Dir(store.Dir), "reports", "paths.md")
	}
	if err := writeReport(out, textguard.Report(paths, price)); err != nil {
		return err
	}

	t := paths.Totals
	fmt.Printf("%d pages: %d native, %d publisher, %d vision, about $%.0f and %.0f lane hours\n",
		t.Total(), t.Native, t.Publisher, t.Vision, price.Cost(t.Vision), price.LaneHours(t.Vision))
	fmt.Printf("%s\n", out)
	return nil
}

// needPoppler stops before the first issue rather than after it. An issue with
// a PDF in the cache cannot be measured without poppler, and finding that out
// on issue four of a hundred wastes the first three.
func needPoppler(cache *fetch.Cache, issues []manifest.Issue) error {
	for i := range issues {
		idx, err := cache.ReadIndex(issues[i].Key)
		if err != nil {
			return err
		}
		if idx.PDF != nil && cache.Has(idx.PDF.SHA256) {
			return pdfsrc.Available()
		}
	}
	return nil
}

func writeReport(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
