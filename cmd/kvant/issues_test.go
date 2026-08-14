package main

import (
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
