package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/publisher"
	"github.com/tamnd/kvant-solver/report"
	"github.com/tamnd/kvant-solver/source/archiveorg"
)

// runVision holds the model's reading of the Soviet years against the Internet
// Archive's reading of the same paper.
//
// There is no vision command to hang this off, and there should not be: the
// reading itself is kvant ocr and this does no reading at all. It is its own
// command because what it produces is a list of pages for a person to open, and
// that is a different job from making pages.
//
// It costs nothing. Three files per year, none of them large, and no model call
// anywhere in it. That is why it can be run over twenty years of corpus without
// anybody having to decide whether it is worth it.
func runVision(args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return fmt.Errorf("the only thing vision does is check, as in kvant vision check --year 1975")
	}
	fs := pflag.NewFlagSet("vision check", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	lang := fs.String("lang", corpus.DefaultLang, "the tree being checked")
	out := fs.String("out", "reports/vision-check.md", "where the document goes, relative to the corpus")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	issues, err := selected(f)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		return fmt.Errorf("name an --issue or a --year")
	}
	c, err := corpus.Open(*f.root)
	if err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	// Alignment is a running total over the whole year, so a run limited to one
	// issue still has to know how many sheets came before it. The flags pick
	// what is reported on and the manifest supplies the rest.
	all := &manifest.Issues{}
	if err := store.Read(manifest.IssuesFile, all); err != nil {
		return err
	}

	ctx := context.Background()
	client := archiveorg.New()
	client.Fetcher.Delay = *f.delay

	var checks []report.VisionCheck
	for _, year := range yearsOf(issues) {
		if year < archiveorg.FirstYear || year > archiveorg.LastYear {
			fmt.Printf("%d: the archive holds %d to %d, skipped\n", year, archiveorg.FirstYear, archiveorg.LastYear)
			continue
		}
		volume, err := client.Volume(ctx, year)
		if err != nil {
			return err
		}
		aligned, err := archiveorg.Align(volume, all.Issues)
		if err != nil {
			// A year whose leaves do not add up is reported and skipped rather
			// than failing the run. It is one year of twenty three and the
			// other twenty two are still worth checking.
			fmt.Printf("%d: %v\n", year, err)
			continue
		}
		for _, iss := range issues {
			if iss.Year != year {
				continue
			}
			leaves, ok := aligned[iss.Key]
			if !ok {
				continue
			}
			checked, agreed := archiveorg.Verify(leaves)
			fmt.Printf("%s: %d leaves, %d of the %d printed numbers sit where they should\n",
				iss.Key, len(leaves), agreed, checked)
			got, err := checkIssue(c, *lang, iss, leaves)
			if err != nil {
				return err
			}
			checks = append(checks, got...)
		}
	}
	if len(checks) == 0 {
		return fmt.Errorf("nothing was checked")
	}
	path := filepath.Join(*f.root, *out)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	doc := report.VisionMarkdown(checks, time.Now())
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		return err
	}
	t := report.TallyVision(checks)
	fmt.Printf("%d pages checked, %d over the line, %d sheets with no page. %s\n", t.Pages, t.Over, t.Unread, *out)
	return nil
}

// checkIssue compares one issue's pages against the leaves they line up with.
func checkIssue(c *corpus.Corpus, lang string, iss manifest.Issue, leaves []archiveorg.Leaf) ([]report.VisionCheck, error) {
	key, err := corpus.ParseIssueKey(iss.Key)
	if err != nil {
		return nil, err
	}
	indexes, err := c.Pages(lang, key)
	if err != nil {
		return nil, err
	}
	have := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		have[index] = true
	}
	out := make([]report.VisionCheck, 0, len(leaves))
	for i, leaf := range leaves {
		sheet := i + 1
		check := report.VisionCheck{Issue: iss.Key, Year: iss.Year, Sheet: sheet}
		if !have[sheet] {
			check.Unread = true
			out = append(out, check)
			continue
		}
		front := &corpus.PageFront{}
		body, err := corpus.LoadUnchecked(c.PagePath(lang, corpus.PageID{Issue: key, Index: sheet}), front)
		if err != nil {
			return nil, err
		}
		// The archive's reading is the first argument, so the words being
		// counted are the ones it found and Missing is the share of those our
		// page does not have. The other direction measures its OCR, which
		// nobody here is trying to improve.
		count, _ := publisher.Compare(leaf.Text, body)
		check.Count = count
		out = append(out, check)
	}
	return out, nil
}

// yearsOf is the years a selection touches, in order.
func yearsOf(issues []manifest.Issue) []int {
	seen := map[int]bool{}
	var out []int
	for _, iss := range issues {
		if !seen[iss.Year] {
			seen[iss.Year] = true
			out = append(out, iss.Year)
		}
	}
	sort.Ints(out)
	return out
}
