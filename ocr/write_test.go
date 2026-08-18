package ocr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/queue"
)

// named is an engine that only has to say what it is called. The write records
// the name in the provenance and never reads anything, because it is reached
// with the answer already in hand.
type named struct{}

func (named) Name() string { return "test-engine" }

func (named) Read(context.Context, string) (string, error) {
	return "", errors.New("the write must not read a page")
}

// writer builds the smallest runner that can write a page: a corpus on disk and
// an issue to be reading. No queue, because the write is reached with a job in
// hand and never asks one for anything.
func writer(t *testing.T, year int, number string) *Runner {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	key, err := corpus.NewIssueKey(year, number)
	if err != nil {
		t.Fatal(err)
	}
	return &Runner{
		Corpus:       store,
		Engine:       named{},
		Issue:        key,
		Source:       "kvant.digital",
		Scan:         "1975-01.pdf",
		PromptSHA256: strings.Repeat("a", 64),
	}
}

func pageJob(target, sheet string) queue.Job {
	j := queue.New(queue.StageOCR, target, strings.Repeat("b", 64), strings.Repeat("a", 64))
	j.Meta = map[string]string{"sheet": sheet}
	return j
}

// The lease keeps a foreign page out, and this is the write refusing one that
// got past it anyway. Worth its own test because the two guards fail for
// different reasons: the lease is an argument a new caller can forget to pass,
// and the write is the last thing that happens before a file lands on disk.
func TestAPageOfAnotherIssueIsRefusedRatherThanFiledUnderTheRunner(t *testing.T) {
	r := writer(t, 1975, "1")
	err := r.write(pageJob("kvant_1980_2_p0001", "1"), "какой-то текст страницы", "")
	if err == nil {
		t.Fatal("a page of 1980 №2 was written by a run of 1975 №1")
	}
	if !strings.Contains(err.Error(), "kvant_1980_2") || !strings.Contains(err.Error(), "kvant_1975_1") {
		t.Errorf("the refusal names neither issue plainly: %v", err)
	}
	for _, dir := range []string{filepath.Join("1975", "01"), filepath.Join("1980", "02")} {
		pages := filepath.Join(r.Corpus.Root, "content", "ru", dir, "pages")
		if entries, err := os.ReadDir(pages); err == nil && len(entries) > 0 {
			t.Errorf("%s holds %d pages, want none", dir, len(entries))
		}
	}
}

// The page number comes off the target too, not off the sheet number in the
// meta. They are the same whenever a job was written by Enqueue, and the target
// is the one of the two that names the file everything downstream reads.
func TestThePageIsFiledWhereItsTargetSaysAndNotWhereItsMetaDoes(t *testing.T) {
	r := writer(t, 1975, "1")
	if err := r.write(pageJob("kvant_1975_1_p0007", "9"), "какой-то текст страницы", ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.Corpus.Root, "content", "ru", "1975", "01", "pages", "0007.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "page_index: 7") {
		t.Errorf("the file at 0007.md does not say it is page seven:\n%s", raw)
	}
}
