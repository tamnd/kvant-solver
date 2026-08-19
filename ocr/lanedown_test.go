package ocr_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

// deadLane is the endpoint that is not there. Every page fails the same way and
// none of them fails because of anything on the page.
type deadLane struct {
	mu    sync.Mutex
	calls int
}

func (d *deadLane) Name() string { return "dead-lane" }

func (d *deadLane) Read(_ context.Context, _ string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return "", errors.New(`call chat completions: dial tcp 127.0.0.1:8000: connect: connection refused`)
}

func (d *deadLane) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// A page that errors has told us nothing about the page, and when every page
// does it the thing that is wrong is not the pages.
//
// A repair of 1347 script failures went through the whole work list in under
// fifteen seconds against a vLLM that had died, printed 4002 connection
// refusals, and gave each issue a summary reading read 0, rejected 0, dead 0.
// Nothing was written and no job was harmed, which is the trouble: that is what
// a finished issue looks like, and the command exited as though the work had
// been done. Forty minutes later somebody has to notice the corpus did not
// grow.
//
// So the run stops and says which error it stopped on, and the count matters as
// much as the error: a halt that arrives after the last page is not a halt.
func TestARunStopsWhenTheLaneStopsAnswering(t *testing.T) {
	const pages = 40
	runner, _, images := setup(t, func(string, int) string { return "" })
	for i := 4; i <= pages; i++ {
		name := filepath.Join(images, fmt.Sprintf("%04d.jpg", i))
		if err := os.WriteFile(name, fmt.Appendf(nil, "not really a jpeg, page %d", i), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lane := &deadLane{}
	runner.Engine = lane

	added, err := runner.Enqueue(sheets(t, images))
	if err != nil {
		t.Fatal(err)
	}
	if added != pages {
		t.Fatalf("queued %d sheets, want %d", added, pages)
	}

	summary, err := runner.Run(context.Background())
	if !errors.Is(err, ocr.ErrLaneDown) {
		t.Fatalf("the run returned %v, want it to name the lane", err)
	}

	// Workers already reading when the halt is set finish their page, so the
	// count lands a little over the threshold and must not land near the work
	// list. Reaching the end would mean the run drained 40 pages against an
	// endpoint that answered none of them, which is the bug.
	if got := lane.count(); got < ocr.MaxStraightFailures || got > ocr.MaxStraightFailures+runner.Workers {
		t.Errorf("the lane was asked %d times, want it to stop within %d of %d",
			got, runner.Workers, ocr.MaxStraightFailures)
	}
	if summary.Read != 0 || summary.Rejected != 0 {
		t.Errorf("got %s, want nothing read and nothing rejected against a lane that answered nothing", summary)
	}
	if summary.Failed != lane.count() {
		t.Errorf("%s, but the lane was asked %d times", summary, lane.count())
	}
}

// The counter is about pages in a row, not pages in total. A lane that reads
// most of what it is given and drops one here and there is a lane worth
// finishing with, and the old behaviour of stepping over a single failure is
// the thing that must survive.
func TestOneBadPageInAmongGoodOnesDoesNotStopTheRun(t *testing.T) {
	const pages = 30
	runner, _, images := setup(t, func(string, int) string { return "" })
	for i := 4; i <= pages; i++ {
		name := filepath.Join(images, fmt.Sprintf("%04d.jpg", i))
		if err := os.WriteFile(name, fmt.Appendf(nil, "not really a jpeg, page %d", i), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner.Engine = &flaky{}
	runner.Workers = 1

	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("the run stopped on %v, and a lane that reads is not a lane that is down", err)
	}
	if summary.Read == 0 {
		t.Fatalf("got %s, want the pages that read to have read", summary)
	}
	if summary.Failed == 0 {
		t.Fatalf("got %s, want the failing pages to have failed, or this proves nothing", summary)
	}
}

// flaky fails every third page and reads the rest.
type flaky struct {
	mu sync.Mutex
	n  int
}

func (f *flaky) Name() string { return "flaky-lane" }

func (f *flaky) Read(_ context.Context, image string) (string, error) {
	f.mu.Lock()
	f.n++
	nth := f.n
	f.mu.Unlock()
	if nth%3 == 0 {
		return "", errors.New("read timed out")
	}
	return page(sheetOf(filepath.Base(image))), nil
}
