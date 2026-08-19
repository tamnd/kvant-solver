package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/publish"
)

func runPublish(args []string) error {
	fs := pflag.NewFlagSet("publish", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	out := fs.String("out", "site", "where to write the site")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to publish, one language per site")
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
	site := &publish.Site{Corpus: c, Lang: *lang, Out: *out, Strict: *strict}
	if !*quiet {
		site.Logf = func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}
	}

	stats, err := site.Build()
	if *report != "" {
		if err := writeRefused(*report, site); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d files: %d issues, %d articles, %d pages\n",
		stats.Files, stats.Issues, stats.Articles, stats.Pages)
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
