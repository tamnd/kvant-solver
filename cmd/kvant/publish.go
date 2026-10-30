package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/publish"
)

func runPublish(args []string) error {
	fs := pflag.NewFlagSet("publish", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	out := fs.String("out", "site", "where to write the site")
	lang := fs.String("lang", "all", "the trees to publish, comma separated, or all")
	siteURL := fs.String("site-url", publish.DefaultSiteURL, "where the site will be served from")
	canonical := fs.String("canonical-url", "", "the copy every page names as the real one (default --site-url)")
	noSheets := fs.Bool("no-sheets", false, "leave out the pages that show every sheet of an issue")
	workers := fs.Int("workers", 0, "how many issues to render at once (default one per CPU)")
	maxFiles := fs.Int("max-files", 0, "fail if the site is more files than this")
	maxMB := fs.Int64("max-mb", 0, "fail if the site is more megabytes than this")
	clean := fs.Bool("clean", true, "empty --out before building")
	quiet := fs.Bool("quiet", false, "print the totals and nothing else")
	strict := fs.Bool("strict", false, "fail if any formula could not be typeset")
	report := fs.String("report", "", "write the formulas that could not be typeset to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	var langs []string
	if *lang != "all" && *lang != "" {
		for _, l := range strings.Split(*lang, ",") {
			if l = strings.TrimSpace(l); l != "" {
				langs = append(langs, l)
			}
		}
	}
	if *clean {
		// A site built over an old one keeps every page the new build no
		// longer writes, and those pages link to things that are gone.
		if err := os.RemoveAll(*out); err != nil {
			return err
		}
	}
	site := &publish.Site{
		Corpus: c, Langs: langs, Out: *out, Strict: *strict,
		SiteURL: *siteURL, CanonicalURL: *canonical, NoSheets: *noSheets,
		Workers: *workers, MaxFiles: *maxFiles, MaxBytes: *maxMB << 20,
	}
	if !*quiet {
		site.Logf = func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}
	}

	start := time.Now()
	stats, err := site.Build()
	if *report != "" {
		if err := writeRefused(*report, site); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d files, %.1f MB in %s: %d languages, %d issues, %d articles, %d pages, %d problems\n",
		stats.Files, float64(stats.Bytes)/(1<<20), time.Since(start).Round(time.Second),
		stats.Langs, stats.Issues, stats.Articles, stats.Pages, stats.Problems)
	if stats.BadMath > 0 {
		fmt.Printf("%d formulas could not be typeset and are marked on the page they are on\n",
			stats.BadMath)
	}
	return err
}

// writeRefused writes the report of what the build could not typeset.
//
// It is written even when the build failed, because a run that stopped part way
// still learned something and throwing it away means reading the whole corpus
// again to learn it twice.
func writeRefused(path string, site *publish.Site) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := publish.WriteRefused(f, site.Refused(), time.Now().Format(time.DateOnly)); err != nil {
		return err
	}
	return f.Close()
}
