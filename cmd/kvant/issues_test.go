package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
)

// issueFixture writes an issue list of three issues over two years.
func issueFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	issues := &manifest.Issues{}
	for _, n := range []struct {
		year   int
		number string
	}{{1975, "1"}, {1975, "2"}, {2024, "5-6"}} {
		iss, err := manifest.NewIssue(n.year, n.number)
		if err != nil {
			t.Fatal(err)
		}
		iss.Sources.Digital = &manifest.Digital{URL: "https://kvant.digital/issues/x/"}
		issues.Add(iss)
	}
	issues.Sort()
	if err := store.Write(manifest.IssuesFile, issues); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestIssuesList(t *testing.T) {
	root := issueFixture(t)
	for _, args := range [][]string{
		{"issues", "list", "--corpus", root},
		{"issues", "list", "--corpus", root, "--long"},
		{"issues", "list", "--corpus", root, "--year", "1975"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%v): %v", args, err)
		}
	}
}

func TestIssuesListWithoutASyncSaysSo(t *testing.T) {
	err := run([]string{"issues", "list", "--corpus", t.TempDir()})
	if err == nil {
		t.Fatal("listing an empty corpus should be an error")
	}
	// The first thing anyone runs is list, and the answer has to name the
	// command that fills the file rather than the file that is not there.
	if got := err.Error(); got != "no issue list yet, run kvant issues sync first" {
		t.Errorf("error reads %q", got)
	}
}

func TestIssuesListRereadsWhatSyncWrote(t *testing.T) {
	root := issueFixture(t)
	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	// Round tripping matters more than it looks: Read runs with KnownFields on,
	// so a field written and not read back is a build break rather than a
	// silent drop.
	issues := &manifest.Issues{}
	if err := store.Read(manifest.IssuesFile, issues); err != nil {
		t.Fatal(err)
	}
	if issues.Count != 3 || issues.Years != 2 {
		t.Errorf("%d issues over %d years", issues.Count, issues.Years)
	}
	if issues.Issues[2].Number != "5-6" {
		t.Errorf("the double issue came back as %q", issues.Issues[2].Number)
	}
}

// deepFixture writes an issue list that has already been through a deep run, so
// every row carries the page count and the row counts only an issue page gives.
func deepFixture(t *testing.T) *manifest.Store {
	t.Helper()
	store, err := manifest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	issues := &manifest.Issues{}
	for _, n := range []struct {
		year   int
		number string
	}{{1972, "1"}, {1975, "1"}, {1975, "2"}} {
		iss, err := manifest.NewIssue(n.year, n.number)
		if err != nil {
			t.Fatal(err)
		}
		iss.Pages = 80
		iss.Sources.Digital = &manifest.Digital{
			URL:  "https://www.kvant.digital/issues/x/",
			Rows: 24, TextRows: 19,
		}
		issues.Add(iss)
	}
	issues.Sort()
	if err := store.Write(manifest.IssuesFile, issues); err != nil {
		t.Fatal(err)
	}
	return store
}

// index is what a sync knows before it opens anything: the issues that exist
// and the URL of each.
func index(t *testing.T, keys ...string) *manifest.Issues {
	t.Helper()
	out := &manifest.Issues{}
	for _, key := range keys {
		year, number, _ := strings.Cut(strings.TrimPrefix(key, "kvant_"), "_")
		y, err := strconv.Atoi(year)
		if err != nil {
			t.Fatal(err)
		}
		iss, err := manifest.NewIssue(y, number)
		if err != nil {
			t.Fatal(err)
		}
		iss.Sources.Digital = &manifest.Digital{URL: "https://www.kvant.digital/issues/x/"}
		out.Add(iss)
	}
	out.Sort()
	return out
}

func TestASyncKeepsWhatADeepRunLearned(t *testing.T) {
	store := deepFixture(t)
	fresh := index(t, "kvant_1972_1", "kvant_1975_1", "kvant_1975_2")
	if err := carryDeep(store, fresh); err != nil {
		t.Fatal(err)
	}
	// The index pass runs over every year on every sync, deep or not. Before
	// this, resyncing one year threw away the page counts and the row counts of
	// all fifty of the others, because the index has nothing to put there.
	for _, iss := range fresh.Issues {
		if iss.Pages != 80 {
			t.Errorf("%s came back with %d pages", iss.Key, iss.Pages)
		}
		if iss.Sources.Digital.Rows != 24 {
			t.Errorf("%s came back with %d contents rows", iss.Key, iss.Sources.Digital.Rows)
		}
	}
	if fresh.Count != 3 || fresh.Years != 2 {
		t.Errorf("%d issues over %d years", fresh.Count, fresh.Years)
	}
}

func TestASyncPicksUpAnIssueTheIndexHasStartedListing(t *testing.T) {
	store := deepFixture(t)
	fresh := index(t, "kvant_1972_1", "kvant_1975_1", "kvant_1975_2", "kvant_1975_3")
	if err := carryDeep(store, fresh); err != nil {
		t.Fatal(err)
	}
	got, ok := fresh.Get("kvant_1975_3")
	if !ok {
		t.Fatal("the new issue was lost")
	}
	if got.Pages != 0 {
		t.Errorf("an issue nobody has opened yet has %d pages", got.Pages)
	}
}

func TestASyncDropsAnIssueTheIndexHasStoppedListing(t *testing.T) {
	store := deepFixture(t)
	fresh := index(t, "kvant_1972_1", "kvant_1975_1")
	if err := carryDeep(store, fresh); err != nil {
		t.Fatal(err)
	}
	// Carrying it would make the file a record of everything the site has ever
	// said rather than a list of what is there now.
	if _, ok := fresh.Get("kvant_1975_2"); ok {
		t.Error("a row the index no longer names survived")
	}
	if fresh.Count != 2 {
		t.Errorf("%d issues after the drop", fresh.Count)
	}
}

func TestTheFirstSyncHasNothingToCarry(t *testing.T) {
	store, err := manifest.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fresh := index(t, "kvant_1975_1")
	if err := carryDeep(store, fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.Count != 1 {
		t.Errorf("%d issues, the run's own answers should have been left alone", fresh.Count)
	}
}

func TestSubcommandsNeedASubcommand(t *testing.T) {
	for _, args := range [][]string{{"issues"}, {"sources"}, {"people"}, {"issues", "nonsense"}, {"sources", "nonsense"}} {
		if err := run(args); err == nil {
			t.Errorf("run(%v) should be an error", args)
		}
	}
}

func TestProbeAndSyncNeedACorpus(t *testing.T) {
	// No corpus path and no KVANT_CORPUS is a mistake worth catching before a
	// single request goes out.
	t.Setenv("KVANT_CORPUS", "")
	for _, args := range [][]string{
		{"issues", "sync"},
		{"sources", "probe"},
		{"people", "sync"},
	} {
		if err := run(args); err == nil {
			t.Errorf("run(%v) should be an error", args)
		}
	}
}
