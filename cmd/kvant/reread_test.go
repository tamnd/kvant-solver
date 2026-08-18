package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
)

// A page that was read is a done job, and a done job is invisible to both of
// the things that put work in the queue: Add refuses an id it already holds
// whatever state that id is in, and the dead list is the only list revive used
// to walk. So --reread deleted the page, printed the count it had deleted,
// queued nothing, and left a hole. It did that to ninety nine pages before
// anybody noticed, because every line it prints on the way is true.
//
// The two halves are tested apart because that is where they went wrong apart.
func TestRereadPutsADoneJobBackInTheQueue(t *testing.T) {
	root := t.TempDir()
	jobs, err := queue.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	c, runner, sheet := staged(t, jobs)

	job := queue.New(queue.StageOCR, runner.Target(sheet.Index), "sha-of-the-image", runner.PromptSHA256)
	job.Meta = runner.Meta(sheet)
	if _, err := jobs.Add(job); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Lease(queue.StageOCR, "worker", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	if state, err := jobs.Finish(job, true, ""); err != nil {
		t.Fatal(err)
	} else if state != queue.Done {
		t.Fatalf("the setup left the job %s, and this test is about a done one", state)
	}

	// The dead list alone, which is what a repair pass walks and what this used
	// to be. It has to find nothing here, or the fix is untested.
	dead, err := jobIDs(jobs, queue.Dead)
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 0 {
		t.Fatalf("a done job showed up among the dead: %v", dead)
	}

	// --reread throws the page away first. Without that, revive skips the sheet
	// on the page file, which is the guard and is meant to hold.
	page := c.PagePath("ru", corpus.PageID{Issue: runner.Issue, Index: sheet.Index})
	if err := os.Remove(page); err != nil {
		t.Fatal(err)
	}

	finished, err := jobIDs(jobs, queue.Dead, queue.Done)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := revive(jobs, runner, c, "ru", runner.Issue, []ocr.Sheet{sheet}, finished)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 {
		t.Fatalf("revive moved %d sheets, want 1", moved)
	}
	pending, err := jobs.List(queue.StageOCR, queue.Pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("%d jobs pending after the reread, want 1", len(pending))
	}
	if pending[0].Target != runner.Target(sheet.Index) {
		t.Errorf("the pending job is for %q, want %q", pending[0].Target, runner.Target(sheet.Index))
	}
}

// The guard the whole command is built on. A sheet named on the command line
// that still has its page is not work, whatever the queue thinks, because the
// corpus is what has to be complete and a call spent overwriting a good page is
// a call wasted. --reread earns its sheets by deleting first.
func TestASheetThatStillHasItsPageIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	jobs, err := queue.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	c, runner, sheet := staged(t, jobs)

	job := queue.New(queue.StageOCR, runner.Target(sheet.Index), "sha-of-the-image", runner.PromptSHA256)
	job.Meta = runner.Meta(sheet)
	if _, err := jobs.Add(job); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Lease(queue.StageOCR, "worker", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Finish(job, true, ""); err != nil {
		t.Fatal(err)
	}

	finished, err := jobIDs(jobs, queue.Dead, queue.Done)
	if err != nil {
		t.Fatal(err)
	}
	moved, err := revive(jobs, runner, c, "ru", runner.Issue, []ocr.Sheet{sheet}, finished)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 0 {
		t.Fatalf("revive moved %d sheets that still have their pages, want 0", moved)
	}
}

// staged writes one page of one issue and returns the corpus, a runner aimed at
// that issue, and the sheet the page was read from.
func staged(t *testing.T, jobs *queue.Queue) (*corpus.Corpus, *ocr.Runner, ocr.Sheet) {
	t.Helper()
	root := fixture(t)
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	key, err := corpus.ParseIssueKey("kvant_1975_1")
	if err != nil {
		t.Fatal(err)
	}
	runner := &ocr.Runner{
		Queue: jobs, Corpus: c, Lang: "ru", Issue: key,
		PromptSHA256: "sha-of-the-prompt",
	}
	image := filepath.Join(t.TempDir(), "0001.png")
	if err := os.WriteFile(image, []byte("not really a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return c, runner, ocr.Sheet{Image: image, Index: 1}
}
