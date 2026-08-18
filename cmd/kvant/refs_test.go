package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
)

// refsFixture builds a corpus the reference pass has something to chew on: one
// issue assembled out of its pages, tagged, with a page of prose that cites the
// magazine and a problem.
func refsFixture(t *testing.T) string {
	t.Helper()
	root := resplitFixture(t, []manifest.Row{
		{Page: 1, Title: "Эллипс", Authors: "Бронштейн", Source: "fixture"},
		{Page: 5, Title: "Парабола", Authors: "Бронштейн", Source: "fixture"},
	})
	// The citation goes into a page of the issue, so that assemble carries it
	// into the article body the way the real text arrives.
	page := filepath.Join(root, "content/ru/1975/01/pages/0006.md")
	text, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	body := string(text) + "\nСм. «Квант», 1975, № 1, с. 1, а также задачу М381.\n"
	if err := os.WriteFile(page, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"tags", "assign", "--corpus", root}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRefsBuildWritesAGraphTheReportCanRead(t *testing.T) {
	root := refsFixture(t)
	if err := run([]string{"refs", "build", "--corpus", root, "--year", "1975"}); err != nil {
		t.Fatal(err)
	}
	graph := filepath.Join(root, "manifests/refs/1975.yaml")
	if _, err := os.Stat(graph); err != nil {
		t.Fatalf("no graph written: %v", err)
	}
	if err := run([]string{"report", "refs", "--corpus", root}); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(filepath.Join(root, "reports/refs-unresolved.md"))
	if err != nil {
		t.Fatalf("no report written: %v", err)
	}
	for _, want := range []string{"1975", "threshold", "linked"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the report does not mention %q:\n%s", want, out)
		}
	}
}

// The report is of the graphs on disk, so asking for one before anything has
// been built is a mistake worth naming rather than an empty document.
func TestReportRefsSaysToBuildFirst(t *testing.T) {
	root := refsFixture(t)
	err := run([]string{"report", "refs", "--corpus", root})
	if err == nil {
		t.Fatal("the report was written with no graphs to write it from")
	}
	if !strings.Contains(err.Error(), "refs build") {
		t.Errorf("the error is %q, and it should say what to run", err)
	}
}

// A bare build over a corpus of forty years is an hour of work nobody asked
// for, so the years have to be named.
func TestRefsBuildRefusesToGuessTheYears(t *testing.T) {
	root := refsFixture(t)
	err := run([]string{"refs", "build", "--corpus", root})
	if err == nil {
		t.Fatal("build ran with no years given")
	}
	if !strings.Contains(err.Error(), "--all") {
		t.Errorf("the error is %q, and it should offer --all", err)
	}
	if err := run([]string{"refs", "build", "--corpus", root, "--all"}); err != nil {
		t.Fatalf("--all did not run: %v", err)
	}
}

// Building again over an unchanged corpus has to leave the file alone, because
// the manifests are committed and a rerun that shuffles them makes every diff
// unreadable.
func TestBuildingTwiceLeavesTheSameFile(t *testing.T) {
	root := refsFixture(t)
	graph := filepath.Join(root, "manifests/refs/1975.yaml")
	if err := run([]string{"refs", "build", "--corpus", root, "--year", "1975"}); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"refs", "build", "--corpus", root, "--year", "1975"}); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(graph)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("two builds over one corpus wrote different files")
	}
}

// A dry run is for looking before writing, so it must not write.
func TestRefsBuildDryRunWritesNothing(t *testing.T) {
	root := refsFixture(t)
	if err := run([]string{"refs", "build", "--corpus", root, "--year", "1975", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "manifests/refs/1975.yaml")); err == nil {
		t.Error("a dry run wrote the graph")
	}
}
