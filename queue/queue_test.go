package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func open(t *testing.T) *Queue {
	t.Helper()
	q, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func add(t *testing.T, q *Queue, target string) Job {
	t.Helper()
	job := New(StageOCR, target, "input-"+target, "prompt-v1")
	added, err := q.Add(job)
	if err != nil {
		t.Fatalf("Add %s: %v", target, err)
	}
	if !added {
		t.Fatalf("Add %s: already present", target)
	}
	return job
}

// The id is what makes a rerun cheap: the same work names the same file, so the
// second run of a pipeline adds nothing.
func TestIDIsContentAddressed(t *testing.T) {
	first := NewID(StageOCR, "alg-i-iii/0045", "abc", "p1")
	if second := NewID(StageOCR, "alg-i-iii/0045", "abc", "p1"); first != second {
		t.Errorf("the same job got two ids: %s and %s", first, second)
	}
	if len(first) != 16 {
		t.Errorf("id %q is %d characters", first, len(first))
	}
	// A new prompt is new work. If it were not, a prompt change would leave the
	// corpus at the old prompt's output with nothing to show for it.
	if changed := NewID(StageOCR, "alg-i-iii/0045", "abc", "p2"); changed == first {
		t.Error("changing the prompt did not change the id")
	}
	if other := NewID(StageTranslate, "alg-i-iii/0045", "abc", "p1"); other == first {
		t.Error("two stages share an id")
	}
	// The separator matters: without it, target "ab"+sha "c" and target "a"+sha
	// "bc" would be the same job.
	if a, b := NewID(StageOCR, "ab", "c", ""), NewID(StageOCR, "a", "bc", ""); a == b {
		t.Error("the fields run together")
	}
}

func TestAddIsIdempotent(t *testing.T) {
	q := open(t)
	job := add(t, q, "alg-i-iii/0045")

	added, err := q.Add(job)
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Error("the same job was added twice")
	}

	// A finished job must not come back either, or every rerun of the pipeline
	// would redo the whole corpus.
	leased, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Finish(leased, true, ""); err != nil {
		t.Fatal(err)
	}
	if added, err := q.Add(job); err != nil || added {
		t.Errorf("a done job was re-added: %t %v", added, err)
	}
}

// A run is started for one book and resolves the image path from the book it
// was started with plus the page number in the target. Hand it a job from
// another volume and it reads Algebra VIII page 66 out of the Algebra I to III
// directory, or reads nothing and kills a page that was fine. Forty five
// Algebra VIII jobs sat in this stage next to a live Algebra I run once, which
// is how close this came to happening for real.
func TestLeaseStaysInsideItsGroup(t *testing.T) {
	q := open(t)
	add(t, q, "alg-viii/0066")
	add(t, q, "alg-i-iii/0066")

	job, err := q.Lease(StageOCR, "server3", "alg-i-iii", time.Minute)
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if job.Target != "alg-i-iii/0066" {
		t.Errorf("leased %s for a run of alg-i-iii", job.Target)
	}
	if _, err := q.Lease(StageOCR, "server3", "alg-i-iii", time.Minute); !errors.Is(err, ErrEmpty) {
		t.Errorf("another book's job was handed to an alg-i-iii run: %v", err)
	}

	// The other book is untouched and not spent an attempt, so its own run
	// still finds it waiting.
	other, err := q.Lease(StageOCR, "server2", "alg-viii", time.Minute)
	if err != nil {
		t.Fatalf("Lease for the other book: %v", err)
	}
	if other.Target != "alg-viii/0066" || other.Attempts != 1 {
		t.Errorf("the other book's job came back as %s on attempt %d", other.Target, other.Attempts)
	}
}

func TestLeaseThenFinish(t *testing.T) {
	q := open(t)
	add(t, q, "alg-i-iii/0045")

	job, err := q.Lease(StageOCR, "server3", "", 10*time.Minute)
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	if job.Attempts != 1 {
		t.Errorf("attempts = %d after one lease", job.Attempts)
	}
	if job.Lease == nil || job.Lease.Host != "server3" {
		t.Fatalf("lease = %+v", job.Lease)
	}
	// The deadline is what the work is expected to take plus room for the rsync
	// back and the validation, which happen after the model has answered.
	if want := 15 * time.Minute; job.Lease.Until.Sub(job.Created).Round(time.Minute) < want {
		t.Errorf("lease until %s, want at least %s of room", job.Lease.Until, want)
	}

	if _, err := q.Lease(StageOCR, "server1", "", time.Minute); !errors.Is(err, ErrEmpty) {
		t.Errorf("a leased job was handed out twice: %v", err)
	}

	state, err := q.Finish(job, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if state != Done {
		t.Errorf("state = %s, want done", state)
	}
	if _, _, err := q.Find(StageOCR, job.ID); err != nil {
		t.Errorf("the finished job is gone: %v", err)
	}
	stats, err := q.Stats(StageOCR)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Counts[Done] != 1 || stats.Counts[Leased] != 0 {
		t.Errorf("stats = %+v", stats.Counts)
	}
}

// A failure with attempts left goes back to pending, because the next attempt
// lands on a different host and that is usually all it takes.
func TestFailureRetriesThenDies(t *testing.T) {
	q := open(t)
	q.MaxAttempts = 3
	add(t, q, "alg-i-iii/0045")

	for attempt := 1; attempt <= 2; attempt++ {
		job, err := q.Lease(StageOCR, "server3", "", time.Minute)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		state, err := q.Fail(job, "unbalanced $")
		if err != nil {
			t.Fatal(err)
		}
		if state != Pending {
			t.Fatalf("attempt %d put the job in %s, want pending", attempt, state)
		}
	}

	job, err := q.Lease(StageOCR, "server2", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if job.Attempts != 3 {
		t.Errorf("attempts = %d on the third lease", job.Attempts)
	}
	state, err := q.Fail(job, "unbalanced $")
	if err != nil {
		t.Fatal(err)
	}
	if state != Dead {
		t.Fatalf("the third failure put the job in %s, want dead", state)
	}

	// A dead job keeps every reason, not just the last one, or the audit has
	// nothing to say beyond that it failed.
	dead, err := q.List(StageOCR, Dead)
	if err != nil || len(dead) != 1 {
		t.Fatalf("dead = %d %v", len(dead), err)
	}
	if len(dead[0].History) != 3 {
		t.Errorf("history has %d entries, want one per attempt", len(dead[0].History))
	}
	hosts := []string{dead[0].History[0].Host, dead[0].History[2].Host}
	if hosts[0] != "server3" || hosts[1] != "server2" {
		t.Errorf("history lost the hosts: %v", hosts)
	}
	if _, err := q.Lease(StageOCR, "server3", "", time.Minute); !errors.Is(err, ErrEmpty) {
		t.Errorf("a dead job was handed out again: %v", err)
	}
}

// This is the whole crash recovery story: a worker that is killed leaves a
// lease with a deadline in the past, and any worker starting up reaps it.
func TestExpiredLeasesComeBack(t *testing.T) {
	q := open(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	q.Now = func() time.Time { return now }
	add(t, q, "alg-i-iii/0045")

	job, err := q.Lease(StageOCR, "server3", "", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	// Still working. Reaping now would take the job away from a live worker,
	// which is worse than waiting, because both would write the same output.
	reaped, err := q.Reap(StageOCR)
	if err != nil || len(reaped) != 0 {
		t.Fatalf("reaped a live lease: %v %v", reaped, err)
	}
	stats, _ := q.Stats(StageOCR)
	if stats.Expired != 0 {
		t.Errorf("expired = %d while the lease is good", stats.Expired)
	}

	now = now.Add(20 * time.Minute)
	stats, _ = q.Stats(StageOCR)
	if stats.Expired != 1 {
		t.Errorf("expired = %d after the deadline passed", stats.Expired)
	}
	reaped, err = q.Reap(StageOCR)
	if err != nil || len(reaped) != 1 || reaped[0] != job.ID {
		t.Fatalf("Reap = %v %v", reaped, err)
	}

	back, err := q.Lease(StageOCR, "server2", "", time.Minute)
	if err != nil {
		t.Fatalf("the reaped job did not come back: %v", err)
	}
	if back.Attempts != 2 {
		t.Errorf("attempts = %d, want the reap to have cost an attempt", back.Attempts)
	}
	if len(back.History) == 0 || !strings.Contains(back.History[0].Reason, "lease expired") {
		t.Errorf("the reap left no reason: %+v", back.History)
	}
}

// A job that has burnt its attempts and is then reaped is dead, not pending
// forever. Without this a host that hangs on every page would spin for good.
func TestReapRespectsTheAttemptBound(t *testing.T) {
	q := open(t)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	q.Now, q.MaxAttempts = func() time.Time { return now }, 1
	add(t, q, "alg-i-iii/0045")

	if _, err := q.Lease(StageOCR, "server3", "", time.Minute); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if _, err := q.Reap(StageOCR); err != nil {
		t.Fatal(err)
	}
	stats, _ := q.Stats(StageOCR)
	if stats.Counts[Dead] != 1 {
		t.Errorf("counts = %+v, want the job dead", stats.Counts)
	}
}

// The claim is the rename. Two workers picking the same job is normal and
// exactly one of them must win, or the same page is transcribed twice and paid
// for twice.
func TestConcurrentLeasesHandEachJobOutOnce(t *testing.T) {
	q := open(t)
	const jobs, workers = 40, 8
	for index := range jobs {
		add(t, q, fmt.Sprintf("alg-i-iii/%04d", index))
	}

	var mu sync.Mutex
	seen := map[string]int{}
	var group sync.WaitGroup
	for worker := range workers {
		group.Go(func() {
			for {
				job, err := q.Lease(StageOCR, fmt.Sprintf("w%d", worker), "", time.Minute)
				if errors.Is(err, ErrEmpty) {
					return
				}
				if err != nil {
					t.Errorf("Lease: %v", err)
					return
				}
				mu.Lock()
				seen[job.ID]++
				mu.Unlock()
				if _, err := q.Finish(job, true, ""); err != nil {
					t.Errorf("Finish: %v", err)
					return
				}
			}
		})
	}
	group.Wait()

	if len(seen) != jobs {
		t.Errorf("handed out %d distinct jobs, want %d", len(seen), jobs)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("job %s went out %d times", id, count)
		}
	}
	stats, _ := q.Stats(StageOCR)
	if stats.Counts[Done] != jobs {
		t.Errorf("done = %d, want %d", stats.Counts[Done], jobs)
	}
}

// Adding the same job from several goroutines at once must add it once.
func TestConcurrentAdd(t *testing.T) {
	q := open(t)
	job := New(StageOCR, "alg-i-iii/0045", "abc", "p1")
	var mu sync.Mutex
	var added int
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			ok, err := q.Add(job)
			if err != nil {
				t.Errorf("Add: %v", err)
				return
			}
			if ok {
				mu.Lock()
				added++
				mu.Unlock()
			}
		})
	}
	group.Wait()
	stats, _ := q.Stats(StageOCR)
	if stats.Counts[Pending] != 1 {
		t.Errorf("pending = %d after eight adds of one job", stats.Counts[Pending])
	}
	if added != 1 {
		t.Errorf("Add reported %d insertions", added)
	}
}

func TestRetryAndDrain(t *testing.T) {
	q := open(t)
	q.MaxAttempts = 1
	for index := range 3 {
		add(t, q, fmt.Sprintf("alg-i-iii/%04d", index))
	}
	for range 3 {
		job, err := q.Lease(StageOCR, "server3", "", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := q.Fail(job, "the tunnel dropped"); err != nil {
			t.Fatal(err)
		}
	}
	stats, _ := q.Stats(StageOCR)
	if stats.Counts[Dead] != 3 {
		t.Fatalf("counts = %+v", stats.Counts)
	}

	moved, err := q.Retry(StageOCR)
	if err != nil || moved != 3 {
		t.Fatalf("Retry = %d %v", moved, err)
	}
	stats, _ = q.Stats(StageOCR)
	if stats.Counts[Pending] != 3 || stats.Counts[Dead] != 0 {
		t.Errorf("counts after retry = %+v", stats.Counts)
	}
	// Retry clears the count, or a job fixed by a person would die again on its
	// first attempt.
	back, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if back.Attempts != 1 {
		t.Errorf("attempts = %d after retry, want the count cleared", back.Attempts)
	}
	if len(back.History) == 0 {
		t.Error("retry threw away the history, so nobody can see what was wrong")
	}

	drained, err := q.Drain(StageOCR)
	if err != nil || drained != 2 {
		t.Fatalf("Drain = %d %v", drained, err)
	}
	stats, _ = q.Stats(StageOCR)
	if stats.Counts[Pending] != 0 {
		t.Errorf("pending = %d after drain", stats.Counts[Pending])
	}
	// Drain leaves the leased job alone: it belongs to a worker that is still
	// running, and it leaves the record of what happened.
	if stats.Counts[Leased] != 1 {
		t.Errorf("drain took a job away from a live worker: %+v", stats.Counts)
	}
}

// A done job asked for again is the same job. That is what -force wants: the
// answer is there and somebody wants another one anyway, usually because the
// account was being served a cut down model when the first one was written.
func TestResetAsksForDoneWorkAgain(t *testing.T) {
	q := open(t)
	job := add(t, q, "alg-i-iii/0045")
	leased, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Finish(leased, true, ""); err != nil {
		t.Fatal(err)
	}
	if added, err := q.Add(job); err != nil || added {
		t.Fatalf("Add of a done job = %v %v, want it left alone", added, err)
	}

	found, err := q.Reset(StageOCR, job.ID)
	if err != nil || !found {
		t.Fatalf("Reset = %v %v", found, err)
	}
	stats, _ := q.Stats(StageOCR)
	if stats.Counts[Pending] != 1 || stats.Counts[Done] != 0 {
		t.Fatalf("counts after reset = %+v", stats.Counts)
	}
	back, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if back.Attempts != 1 {
		t.Errorf("attempts = %d, want the count cleared", back.Attempts)
	}
	if len(back.History) == 0 {
		t.Error("reset threw away the history of the answer that was there")
	}

	// The one it is holding belongs to a worker that has not come back yet, and
	// handing the same work out twice is what the lease is there to prevent.
	if found, err := q.Reset(StageOCR, back.ID); err != nil || found {
		t.Errorf("Reset of a leased job = %v %v, want it left alone", found, err)
	}
	if found, err := q.Reset(StageOCR, "no such job"); err != nil || found {
		t.Errorf("Reset of a job that is not there = %v %v", found, err)
	}
}

func TestRetryFromPendingIsRefused(t *testing.T) {
	q := open(t)
	if _, err := q.Retry(StageOCR, Done); err == nil {
		t.Error("retry from done was accepted")
	}
	if _, err := q.Retry(StageOCR, Pending); err == nil {
		t.Error("retry from pending was accepted")
	}
}

// Job files are the durable part. Anything the queue writes has to survive a
// process that is killed in the middle of writing it.
func TestWritesAreAtomic(t *testing.T) {
	q := open(t)
	job := add(t, q, "alg-i-iii/0045")
	entries, err := os.ReadDir(q.dir(StageOCR, Pending))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", entry.Name())
		}
	}
	raw, err := os.ReadFile(filepath.Join(q.dir(StageOCR, Pending), job.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"target": "alg-i-iii/0045"`) {
		t.Errorf("job file:\n%s", raw)
	}
}

// A queue in a directory that does not exist yet is the first run, not an
// error.
func TestOpenCreatesTheTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work", "queue")
	q, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range Stages {
		for _, state := range States {
			if _, err := os.Stat(q.dir(stage, state)); err != nil {
				t.Errorf("%s/%s: %v", stage, state, err)
			}
		}
	}
	if _, err := q.Lease(StageSolve, "server3", "", time.Minute); !errors.Is(err, ErrEmpty) {
		t.Errorf("an empty stage returned %v, want ErrEmpty", err)
	}
}

// Stages do not see each other, so draining OCR cannot take out translation.
func TestStagesAreSeparate(t *testing.T) {
	q := open(t)
	add(t, q, "alg-i-iii/0045")
	if _, err := q.Add(New(StageTranslate, "alg-i-iii/0045", "abc", "p1")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Drain(StageOCR); err != nil {
		t.Fatal(err)
	}
	stats, _ := q.Stats(StageTranslate)
	if stats.Counts[Pending] != 1 {
		t.Errorf("translate = %+v after draining ocr", stats.Counts)
	}
}

func TestStatsTotalAndTable(t *testing.T) {
	q := open(t)
	add(t, q, "alg-i-iii/0045")
	add(t, q, "alg-i-iii/0046")
	stats, err := q.Stats(StageOCR)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total() != 2 {
		t.Errorf("Total = %d", stats.Total())
	}
	table := Table([]Stats{stats})
	if !strings.Contains(table, "ocr") || !strings.Contains(table, "pending") {
		t.Errorf("table:\n%s", table)
	}
}

func TestParse(t *testing.T) {
	if stage, err := ParseStage(" OCR "); err != nil || stage != StageOCR {
		t.Errorf("ParseStage = %q %v", stage, err)
	}
	if _, err := ParseStage("render"); err == nil || !strings.Contains(err.Error(), "ocr") {
		t.Errorf("ParseStage(render) = %v, want the valid ones listed", err)
	}
	if state, err := ParseState("Dead"); err != nil || state != Dead {
		t.Errorf("ParseState = %q %v", state, err)
	}
	if _, err := ParseState("stuck"); err == nil {
		t.Error("an unknown state was accepted")
	}
}

func TestAddRejectsAJobWithNoID(t *testing.T) {
	q := open(t)
	if _, err := q.Add(Job{Stage: StageOCR, Target: "x"}); err == nil {
		t.Error("a job with no id was accepted")
	}
	if _, err := q.Add(Job{ID: "abc", Target: "x"}); err == nil {
		t.Error("a job with no stage was accepted")
	}
}

func TestFindMissing(t *testing.T) {
	q := open(t)
	if _, _, err := q.Find(StageOCR, "nosuchjob"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Find = %v, want not exist", err)
	}
}

// Meta is how a retry escalates the dpi, so it has to survive the round trip
// through the file.
func TestMetaSurvives(t *testing.T) {
	q := open(t)
	job := New(StageOCR, "alg-i-iii/0045", "abc", "p1")
	job.Meta = map[string]string{"dpi": "300"}
	if _, err := q.Add(job); err != nil {
		t.Fatal(err)
	}
	leased, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.Meta["dpi"] != "300" {
		t.Errorf("meta = %v", leased.Meta)
	}
	leased.Meta["dpi"] = "400"
	if _, err := q.Fail(leased, "blurry"); err != nil {
		t.Fatal(err)
	}
	again, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if again.Meta["dpi"] != "400" {
		t.Errorf("the escalated dpi was lost: %v", again.Meta)
	}
}

func TestReleaseGivesTheAttemptBackAndSaysWhy(t *testing.T) {
	q := open(t)
	job := New(StageOCR, "alg-i-iii/0070", "sha", "prompt")
	if _, err := q.Add(job); err != nil {
		t.Fatal(err)
	}
	leased, err := q.Lease(StageOCR, "server3", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.Attempts != 1 {
		t.Fatalf("attempts = %d after leasing, want 1", leased.Attempts)
	}

	if err := q.Release(leased, "ssh: no route to host"); err != nil {
		t.Fatal(err)
	}
	back, state, err := q.Find(StageOCR, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state != Pending {
		t.Errorf("state = %s, want pending", state)
	}
	if back.Attempts != 0 {
		t.Errorf("attempts = %d, want the attempt given back", back.Attempts)
	}
	if back.Lease != nil {
		t.Error("the lease is still on a job that was handed back")
	}
	// Silence would make a job that loops look like a job that never ran.
	if len(back.History) != 1 || !strings.Contains(back.History[0].Reason, "no route to host") {
		t.Errorf("history = %+v, want the reason it came back", back.History)
	}
	if back.History[0].Host != "server3" {
		t.Errorf("history says host %q, want server3", back.History[0].Host)
	}
}

// Three releases in a row must not drive the count below zero, or the job
// becomes one that can never die.
func TestReleasingAJobThatWasNeverLeasedIsHarmless(t *testing.T) {
	q := open(t)
	job := New(StageOCR, "alg-i-iii/0070", "sha", "prompt")
	if _, err := q.Add(job); err != nil {
		t.Fatal(err)
	}
	if err := q.Release(job, "never mind"); err != nil {
		t.Fatal(err)
	}
	back, _, err := q.Find(StageOCR, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Attempts != 0 {
		t.Errorf("attempts = %d, want 0", back.Attempts)
	}
}

// Outstanding is what stops one page being queued twice. Done is not in it: a
// page read at the old prompt is work again when the prompt changes.
func TestOutstandingIsEveryTargetStillToRun(t *testing.T) {
	q := open(t)
	pending := New(StageOCR, "alg-i-iii/0061", "a", "p")
	leased := New(StageOCR, "alg-i-iii/0062", "b", "p")
	dead := New(StageOCR, "alg-i-iii/0063", "c", "p")
	done := New(StageOCR, "alg-i-iii/0064", "d", "p")
	for _, job := range []Job{pending, leased, dead, done} {
		if _, err := q.Add(job); err != nil {
			t.Fatal(err)
		}
	}
	// Lease hands out the lowest id first, so they all come out and the ones
	// this test is not using go back. Releasing inside the loop would put a job
	// back where the next Lease finds it, and the loop would never end.
	held := map[string]Job{}
	for range 4 {
		job, err := q.Lease(StageOCR, "server3", "", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		held[job.ID] = job
	}
	take := func(id string) Job {
		t.Helper()
		job, ok := held[id]
		if !ok {
			t.Fatalf("job %s never came out of the queue", id)
		}
		return job
	}
	if _, err := q.Finish(take(done.ID), true, ""); err != nil {
		t.Fatal(err)
	}
	out := take(dead.ID)
	out.Attempts = DefaultMaxAttempts
	if _, err := q.Fail(out, "no answer came back"); err != nil {
		t.Fatal(err)
	}
	if err := q.Release(take(pending.ID), "putting this one back"); err != nil {
		t.Fatal(err)
	}

	outstanding, err := q.Outstanding(StageOCR)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]State{
		"alg-i-iii/0061": Pending,
		"alg-i-iii/0062": Leased,
		"alg-i-iii/0063": Dead,
	}
	if len(outstanding) != len(want) {
		t.Fatalf("outstanding = %+v, want %+v", outstanding, want)
	}
	for target, state := range want {
		if outstanding[target] != state {
			t.Errorf("%s = %q, want %q", target, outstanding[target], state)
		}
	}
}
