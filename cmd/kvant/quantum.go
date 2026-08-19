package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/quantum"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/nsta"
)

func runQuantum(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("quantum needs a subcommand, which is sync or report")
	}
	switch args[0] {
	case "sync":
		return runQuantumSync(args[1:])
	case "report":
		return runQuantumReport(args[1:])
	default:
		return fmt.Errorf("unknown quantum subcommand %q", args[0])
	}
}

// runQuantumSync builds manifests/quantum-map.yaml out of the NSTA index and
// the tables of contents already in the corpus.
//
// No page is read and no model is called. The English side is a list of
// bylines and the Russian side is a list of bylines, and the mapping is what
// the two have in common, so the whole thing runs in a second off one HTTP
// request.
func runQuantumSync(args []string) error {
	fs := pflag.NewFlagSet("quantum sync", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	from := fs.String("index", "", "read the index from this file instead of from NSTA")
	dry := fs.Bool("dry-run", false, "print the counts and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := manifest.Open(*root)
	if err != nil {
		return err
	}
	var toc manifest.TOC
	if err := store.Read(manifest.TOCFile, &toc); err != nil {
		return fmt.Errorf("the mapping is made against the printed contents: %w", err)
	}

	entries, err := readQuantumIndex(*from)
	if err != nil {
		return err
	}
	m := quantum.Build(entries, &toc)
	if err := m.Check(); err != nil {
		return err
	}

	years := m.Years()
	fmt.Printf("%d articles in Quantum, %d to %d\n", m.Count, years[0], years[len(years)-1])
	fmt.Printf("  mapped onto a Kvant article: %d\n", m.Mapped)
	fmt.Printf("  the authors are known and the article is not: %d\n", m.Ambiguous())
	fmt.Printf("  no earlier Kvant article by those authors: %d\n", m.Count-m.Mapped-m.Ambiguous())
	if *dry {
		return nil
	}
	return store.Write(manifest.QuantumFile, m,
		"The English Quantum, 1990 to 2001, against the Kvant it was translated out of.",
		"Built by kvant quantum sync from the NSTA index and manifests/toc.yaml.",
		"A row is mapped only when one Kvant article has exactly the same authors.")
}

// readQuantumIndex reads the cumulative index, off disk when a file was given.
// The index is one page that changes about never, so a copy of it is a
// reasonable thing to keep and rerun against.
func readQuantumIndex(path string) ([]nsta.Entry, error) {
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return nsta.ParseIndex(f)
	}
	client := source.NewClient()
	res, err := client.Get(context.Background(), nsta.IndexURL)
	if err != nil {
		return nil, err
	}
	return nsta.ParseIndex(bytes.NewReader(res.Body))
}

// runQuantumReport says what the mapping covers, per year of Kvant, so that a
// year about to be translated can be checked against the English first.
func runQuantumReport(args []string) error {
	fs := pflag.NewFlagSet("quantum report", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := manifest.Open(*root)
	if err != nil {
		return err
	}
	var m manifest.Quantum
	if err := store.Read(manifest.QuantumFile, &m); err != nil {
		return err
	}

	byYear := map[int]int{}
	for _, a := range m.Articles {
		if a.Kvant == "" {
			continue
		}
		key, err := corpus.ParseIssueKey(a.Kvant)
		if err != nil {
			return err
		}
		byYear[key.Year]++
	}
	years := make([]int, 0, len(byYear))
	for y := range byYear {
		years = append(years, y)
	}
	sort.Ints(years)

	fmt.Printf("%d of %d Quantum articles map onto a Kvant article.\n\n", m.Mapped, m.Count)
	fmt.Println("Kvant year  articles carried into English")
	for _, y := range years {
		fmt.Printf("  %d      %d\n", y, byYear[y])
	}
	return nil
}
