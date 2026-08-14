package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
)

// planned writes the two things textguard reads: an issue list in a corpus,
// and a page manifest in a cache with a scan already downloaded.
func planned(t *testing.T) (root, cacheDir string) {
	t.Helper()
	root, cacheDir = t.TempDir(), t.TempDir()

	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	issues := &manifest.Issues{}
	iss, err := manifest.NewIssue(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	iss.Sources.Digital = &manifest.Digital{URL: "https://www.kvant.digital/issues/1975/1/", Rows: 2, TextRows: 1}
	issues.Add(iss)
	issues.Sort()
	if err := store.Write(manifest.IssuesFile, issues); err != nil {
		t.Fatal(err)
	}

	toc := &manifest.TOC{}
	toc.Set(iss.Key, []manifest.Row{
		{Title: "Эллипс", Page: 1, Source: "kvant_digital"},
		{Title: "Парабола", Page: 3, HasText: true, Slug: "parabola", Source: "kvant_digital"},
	})
	if err := store.Write(manifest.TOCFile, toc); err != nil {
		t.Fatal(err)
	}

	cache, err := fetch.OpenCache(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	idx := &fetch.Index{Issue: iss.Key, Year: 1975}
	for ord, page := range []int{0, 1, 2, 3} {
		sum, n, err := cache.Put(strings.NewReader("sheet " + iss.Key + string(rune('a'+ord))))
		if err != nil {
			t.Fatal(err)
		}
		idx.Set(fetch.Sheet{Ord: ord, File: "000" + string(rune('0'+ord)), Page: page,
			Blob: fetch.Blob{URL: "https://www.kvant.digital/x.jpg", SHA256: sum, Bytes: n}})
	}
	if err := cache.WriteIndex(idx); err != nil {
		t.Fatal(err)
	}
	return root, cacheDir
}

func TestTextguardWritesThePlanAndTheReport(t *testing.T) {
	root, cacheDir := planned(t)
	if err := run([]string{"textguard", "--corpus", root, "--cache", cacheDir, "--all"}); err != nil {
		t.Fatal(err)
	}

	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := &manifest.Paths{}
	if err := store.Read(manifest.PathsFile, paths); err != nil {
		t.Fatal(err)
	}
	if paths.Totals.Total() != 4 {
		t.Errorf("the plan covers %d sheets, the scan has 4", paths.Totals.Total())
	}
	// 1975 has no PDF anywhere, so the only saving is the one article
	// kvant.digital carries the text of, which starts on page 3 and runs to the
	// end of the issue.
	if paths.Totals.Publisher != 1 {
		t.Errorf("%d pages took the publisher path", paths.Totals.Publisher)
	}
	if paths.Totals.Vision != 3 {
		t.Errorf("%d pages went to vision", paths.Totals.Vision)
	}

	body, err := os.ReadFile(filepath.Join(root, "reports", "paths.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## By year", "## By decade", "What the estimates assume", "| 1975 |"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the report has no %q", want)
		}
	}
}

func TestTextguardWantsToBeToldWhatToLookAt(t *testing.T) {
	root, cacheDir := planned(t)
	err := run([]string{"textguard", "--corpus", root, "--cache", cacheDir})
	if err == nil {
		t.Fatal("a bare textguard should say what it wants rather than read fifty years")
	}
	if !strings.Contains(err.Error(), "--all") {
		t.Errorf("error reads %q", err)
	}
}

func TestTextguardOnAYearLeavesTheRestAlone(t *testing.T) {
	root, cacheDir := planned(t)
	if err := run([]string{"textguard", "--corpus", root, "--cache", cacheDir, "--all"}); err != nil {
		t.Fatal(err)
	}
	// A year nothing is known about is not a reason to throw away what is.
	if err := run([]string{"textguard", "--corpus", root, "--cache", cacheDir, "--year", "1975"}); err != nil {
		t.Fatal(err)
	}
	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := &manifest.Paths{}
	if err := store.Read(manifest.PathsFile, paths); err != nil {
		t.Fatal(err)
	}
	if len(paths.Issues) != 1 || paths.Totals.Total() != 4 {
		t.Errorf("the second run left %d issues and %d pages", len(paths.Issues), paths.Totals.Total())
	}
}

func TestFetchSaysSoWhenNoIssueMatches(t *testing.T) {
	root, cacheDir := planned(t)
	err := run([]string{"fetch", "pages", "--corpus", root, "--cache", cacheDir, "--year", "1999"})
	if err == nil {
		t.Fatal("fetching a year the archive does not have should be an error")
	}
	if !strings.Contains(err.Error(), "no issues match") {
		t.Errorf("error reads %q", err)
	}
}
