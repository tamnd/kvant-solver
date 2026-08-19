package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/catalog"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
)

func runIssues(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("issues needs a subcommand, which is sync or list")
	}
	switch args[0] {
	case "sync":
		return runIssuesSync(args[1:])
	case "list":
		return runIssuesList(args[1:])
	default:
		return fmt.Errorf("unknown issues subcommand %q", args[0])
	}
}

// syncFlags are the flags every command that touches the network shares.
type syncFlags struct {
	root  *string
	delay *time.Duration
	years *[]int
}

func addSyncFlags(fs *pflag.FlagSet) syncFlags {
	return syncFlags{
		root:  fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout"),
		delay: fs.Duration("delay", source.DefaultDelay, "gap between two requests to one host"),
		years: fs.IntSlice("year", nil, "limit to these years, repeatable"),
	}
}

// catalogFor builds the three clients on one fetcher, so the delay is decided
// once for the whole run rather than per source.
func catalogFor(f syncFlags) *catalog.Catalog {
	fetcher := source.NewClient()
	fetcher.Delay = *f.delay
	return catalog.NewWith(fetcher)
}

func runIssuesSync(args []string) error {
	fs := pflag.NewFlagSet("issues sync", pflag.ContinueOnError)
	f := addSyncFlags(fs)
	deep := fs.Bool("deep", false, "also read every issue page for its contents and page count")
	mirror := fs.Bool("mirror", false, "with --deep, read both MCCME orderings and cross check them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	ctx := context.Background()
	c := catalogFor(f)

	// The errata start empty and the old file is folded back in at the end. A
	// finding is what this run saw, not what some run saw once, and Carry is
	// what keeps the resolutions and the years this run did not visit.
	old := readErrata(store)
	errata := &manifest.Errata{}
	var issues *manifest.Issues
	finish := func(toc *manifest.TOC, rubrics *manifest.Rubrics) error {
		errata.Carry(old, lookedAt(*deep && *mirror, *f.years))
		return writeAll(store, issues, toc, rubrics, errata)
	}

	issues, err = c.SyncIssues(ctx, errata)
	if err != nil {
		return err
	}
	// What a deep run learned about an issue is not in the index, so a run that
	// did not visit an issue has nothing to say about its page count or its row
	// counts and must not overwrite them with the nothing it has. Reading the
	// old file and merging this run's answers onto it keeps the years this run
	// skipped, which is what makes resyncing one year a safe thing to do.
	if err := carryDeep(store, issues); err != nil {
		return err
	}
	fmt.Printf("index: %d issues over %d years\n", issues.Count, issues.Years)

	if *deep {
		toc := &manifest.TOC{}
		if err := store.Read(manifest.TOCFile, toc); err != nil && !errors.Is(err, manifest.ErrMissing) {
			return err
		}
		rubrics := &manifest.Rubrics{}
		if err := store.Read(manifest.RubricsFile, rubrics); err != nil && !errors.Is(err, manifest.ErrMissing) {
			return err
		}
		if err := deepSync(ctx, c, issues, toc, rubrics, errata, *f.years, *mirror); err != nil {
			// A run that dies half way through has still learned something, so
			// what it learned is written before the error goes up. The error
			// that stopped the run is the one worth reporting, so a failure to
			// write goes to stderr and no further.
			if werr := finish(toc, rubrics); werr != nil {
				fmt.Fprintln(os.Stderr, "kvant: could not write what the run had:", werr)
			}
			return err
		}
		toc.Sort()
		rubrics.Sort()
		fmt.Printf("contents: %d rows, %d rubrics\n", toc.Rows(), rubrics.Count)
		return finish(toc, rubrics)
	}
	return finish(nil, nil)
}

// carryDeep folds this run's index answers onto the issues already on disk.
//
// The old file is the base and the fresh one is the overlay, because merge only
// takes a field it has something to put in it. So a URL the index just read
// wins, and a page count only a deep run could know survives a run that was not
// deep or was limited to another year.
func carryDeep(store *manifest.Store, fresh *manifest.Issues) error {
	old := &manifest.Issues{}
	switch err := store.Read(manifest.IssuesFile, old); {
	case errors.Is(err, manifest.ErrMissing):
		return nil
	case err != nil:
		return err
	}
	for _, iss := range fresh.Issues {
		old.Add(iss)
	}
	// An issue the index no longer lists is dropped rather than carried. The
	// index is the list of what exists, and keeping a row it stopped naming
	// would make the manifest a record of everything the site has ever said.
	kept := old.Issues[:0]
	for _, iss := range old.Issues {
		if _, ok := fresh.Get(iss.Key); ok {
			kept = append(kept, iss)
		}
	}
	old.Issues = kept
	old.Sort()
	*fresh = *old
	return nil
}

// lookedAt says whether this run examined the thing an old erratum is about,
// and so whether the run not repeating it means it is gone.
//
// The index and the mirror's site map are read in full on every run, so
// anything that comes out of that pass was examined again. A finding about one
// issue's contents only counts as examined when the issue was read again, which
// takes a deep run with the mirror on, and a run limited to a year or two only
// speaks for those years.
func lookedAt(mirrored bool, years []int) func(manifest.Erratum) bool {
	return func(x manifest.Erratum) bool {
		if x.Kind != "toc_row" && x.Kind != "toc_ordering" {
			return true
		}
		if !mirrored {
			return false
		}
		return len(years) == 0 || slices.Contains(years, manifest.KeyYear(x.Issue))
	}
}

// deepSync walks the issues one at a time. It is deliberately serial: the
// fetcher would serialise it anyway, and going issue by issue means a run that
// is interrupted leaves a manifest that is short rather than one that is full
// of holes.
func deepSync(ctx context.Context, c *catalog.Catalog, issues *manifest.Issues, toc *manifest.TOC, rubrics *manifest.Rubrics, errata *manifest.Errata, years []int, mirror bool) error {
	done := 0
	for i := range issues.Issues {
		iss := &issues.Issues[i]
		if len(years) > 0 && !slices.Contains(years, iss.Year) {
			continue
		}
		if err := c.SyncIssue(ctx, iss, toc, rubrics); err != nil {
			return err
		}
		if mirror {
			if err := c.SyncMirrorTOC(ctx, iss, toc, errata); err != nil {
				return err
			}
		}
		done++
		if done%25 == 0 {
			fmt.Printf("  %d issues read, at %s\n", done, iss.Key)
		}
	}
	fmt.Printf("deep: %d issues read\n", done)
	return nil
}

func runIssuesList(args []string) error {
	fs := pflag.NewFlagSet("issues list", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	years := fs.IntSlice("year", nil, "limit to these years, repeatable")
	long := fs.Bool("long", false, "one line per issue rather than one per year")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := manifest.Open(*root)
	if err != nil {
		return err
	}
	issues := &manifest.Issues{}
	if err := store.Read(manifest.IssuesFile, issues); err != nil {
		if errors.Is(err, manifest.ErrMissing) {
			return fmt.Errorf("no issue list yet, run kvant issues sync first")
		}
		return err
	}

	shown := 0
	for _, year := range issues.YearList() {
		if len(*years) > 0 && !slices.Contains(*years, year) {
			continue
		}
		inYear := issues.Year(year)
		shown += len(inYear)
		if !*long {
			numbers := make([]string, 0, len(inYear))
			for _, iss := range inYear {
				numbers = append(numbers, iss.Number)
			}
			fmt.Printf("%d  %2d  %s\n", year, len(inYear), strings.Join(numbers, " "))
			continue
		}
		for _, iss := range inYear {
			fmt.Printf("%-16s %-6s %4d pages %4d sheets  %s\n",
				iss.Key, iss.Number, iss.Pages, iss.Sheets, sourceList(iss))
		}
	}
	fmt.Printf("%d issues over %d years\n", shown, countYears(issues, *years))
	return nil
}

func sourceList(iss manifest.Issue) string {
	var out []string
	if iss.Sources.Digital != nil {
		out = append(out, catalog.SourceDigital)
	}
	if iss.Sources.MCCME != nil {
		out = append(out, catalog.SourceMCCME)
	}
	if iss.Sources.MathNet != nil {
		out = append(out, catalog.SourceMathNet)
	}
	return strings.Join(out, ", ")
}

func countYears(issues *manifest.Issues, years []int) int {
	if len(years) == 0 {
		return issues.Years
	}
	n := 0
	for _, y := range issues.YearList() {
		if slices.Contains(years, y) {
			n++
		}
	}
	return n
}

// readErrata loads the errata file if there is one. It is read rather than
// started fresh because a resolution written by a person lives in that file and
// a resync must not throw it away.
func readErrata(store *manifest.Store) *manifest.Errata {
	errata := &manifest.Errata{}
	if err := store.Read(manifest.ErrataFile, errata); err != nil && !errors.Is(err, manifest.ErrMissing) {
		fmt.Fprintln(os.Stderr, "kvant: could not read the old errata, starting a new file:", err)
		return &manifest.Errata{}
	}
	return errata
}

func writeAll(store *manifest.Store, issues *manifest.Issues, toc *manifest.TOC, rubrics *manifest.Rubrics, errata *manifest.Errata) error {
	if issues != nil {
		issues.Sort()
		if err := store.Write(manifest.IssuesFile, issues, "Every issue of Kvant, with the URL each source gave for it."); err != nil {
			return err
		}
	}
	if toc != nil {
		if err := store.Write(manifest.TOCFile, toc, "Tables of contents, one block per issue, in issue order."); err != nil {
			return err
		}
	}
	if rubrics != nil {
		if err := store.Write(manifest.RubricsFile, rubrics, "The rubrics seen in the contents, commonest first, with the spellings each was printed under."); err != nil {
			return err
		}
	}
	if errata != nil {
		errata.Sort()
		if err := store.Write(manifest.ErrataFile, errata,
			"Places the sources disagree. The resolution field is for a person to fill in and is kept across a resync."); err != nil {
			return err
		}
		if open := len(errata.Open()); open > 0 {
			fmt.Printf("errata: %d entries, %d still open\n", len(errata.Entries), open)
		}
	}
	return nil
}
