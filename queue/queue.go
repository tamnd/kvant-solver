// Package queue is the work list on disk.
//
// At the 148 seconds a page measured in the OCR audit, nothing worth doing fits
// in one process lifetime. One issue is two and a half hours, one year is a
// day and a half, and the archive is 34000 pages. Laptops sleep, tunnels drop,
// and somebody will hit Ctrl-C. So the state of the work lives in files, one
// per job, and any process can pick up where the last one stopped.
//
// The design is four rules:
//
// Leases, not locks. A worker claims a job by renaming it into leased/ with a
// deadline. A worker that dies holds nothing: its lease expires and the job
// comes back. There is no lock file to clean up and no pid to trust.
//
// Content addressed ids. The id is a hash of what the job is, so running the
// pipeline again produces the same ids, and work already done is skipped by the
// file being there. No separate ledger to keep in step.
//
// One side effect. A job writes its output file and nothing else, by temp file
// and rename, so a job that dies halfway leaves either the old output or the
// new one and never half of either.
//
// Bounded attempts. Three by default. After that the job is dead and the audit
// has to name it, because a page that silently retries forever is a page that
// never appears in the corpus and never appears in a report either.
package queue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Stage is a kind of work. Each has its own directory, so draining the OCR
// queue does not touch translation.
type Stage string

const (
	StageOCR       Stage = "ocr"
	StageRepair    Stage = "repair"
	StageTranslate Stage = "translate"
	StageSolve     Stage = "solve"
	StageJudge     Stage = "judge"
)

// Stages is every stage, in the order the pipeline runs them.
var Stages = []Stage{StageOCR, StageRepair, StageTranslate, StageSolve, StageJudge}

// State is where a job is. These are directory names.
type State string

const (
	Pending State = "pending"
	Leased  State = "leased"
	Done    State = "done"
	Failed  State = "failed"
	Dead    State = "dead"
)

// States is every state, in the order a report should list them.
var States = []State{Pending, Leased, Done, Failed, Dead}

// DefaultMaxAttempts is how many times a job is tried before it is dead.
//
// Three, because the failures worth retrying on this fleet are a dropped
// tunnel, a session that logged itself out, and a page the model garbled once.
// A fourth attempt has never fixed any of those; what fixes them is a person
// reading the reason, which is what dead is for.
const DefaultMaxAttempts = 3

// LeaseSlack is added to a job's expected duration to get its lease deadline.
// It covers the rsync back and the validation, which happen after the model has
// answered and before the job is marked done.
const LeaseSlack = 5 * time.Minute

// Job is one unit of work.
type Job struct {
	ID     string `json:"id"`
	Stage  Stage  `json:"stage"`
	Target string `json:"target"`
	// InputSHA256 and PromptSHA256 are what the id is made of, kept in the file
	// so a job can say why it is not the same job as the one before it. A new
	// prompt is a new id, which is the point: the corpus is rebuilt rather than
	// silently left at the old prompt's output.
	InputSHA256  string    `json:"input_sha256,omitempty"`
	PromptSHA256 string    `json:"prompt_sha256,omitempty"`
	Attempts     int       `json:"attempts"`
	Created      time.Time `json:"created"`
	Lease        *Lease    `json:"lease,omitempty"`
	History      []Event   `json:"history,omitempty"`
	// Meta carries whatever the stage needs and the queue does not care about,
	// such as the dpi a retry should escalate to.
	Meta map[string]string `json:"meta,omitempty"`
}

// Lease is a claim with a deadline on it.
type Lease struct {
	Host  string    `json:"host"`
	Until time.Time `json:"until"`
	PID   int       `json:"pid"`
}

// Event is one attempt, kept so that a dead job says what went wrong every time
// rather than only the last time.
type Event struct {
	TS     time.Time `json:"ts"`
	Host   string    `json:"host"`
	OK     bool      `json:"ok"`
	Reason string    `json:"reason,omitempty"`
}

// NewID is the content address of a job: the same work always gets the same
// name, so a rerun skips what is done without keeping a ledger.
func NewID(stage Stage, target, inputSHA, promptSHA string) string {
	sum := sha256.Sum256([]byte(string(stage) + "\x00" + target + "\x00" + inputSHA + "\x00" + promptSHA))
	return hex.EncodeToString(sum[:])[:16]
}

// New builds a job with its id filled in.
func New(stage Stage, target, inputSHA, promptSHA string) Job {
	return Job{
		ID: NewID(stage, target, inputSHA, promptSHA), Stage: stage, Target: target,
		InputSHA256: inputSHA, PromptSHA256: promptSHA, Created: time.Now().UTC(),
	}
}

// Queue is a directory of job files.
type Queue struct {
	Root string
	// MaxAttempts is the bound. Zero means DefaultMaxAttempts.
	MaxAttempts int
	// Now is the clock, replaceable because everything here is about deadlines
	// and no test should wait for one.
	Now func() time.Time
}

// Open makes the directories a queue needs and returns it.
func Open(root string) (*Queue, error) {
	q := &Queue{Root: root}
	for _, stage := range Stages {
		for _, state := range States {
			if err := os.MkdirAll(q.dir(stage, state), 0o755); err != nil {
				return nil, err
			}
		}
	}
	return q, nil
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now().UTC()
	}
	return time.Now().UTC()
}

func (q *Queue) maxAttempts() int {
	if q.MaxAttempts > 0 {
		return q.MaxAttempts
	}
	return DefaultMaxAttempts
}

func (q *Queue) dir(stage Stage, state State) string {
	return filepath.Join(q.Root, string(stage), string(state))
}

func (q *Queue) path(stage Stage, state State, id string) string {
	return filepath.Join(q.dir(stage, state), id+".json")
}

// ErrEmpty is returned by Lease when there is nothing to do. It is a normal
// end of run, not a failure, so callers compare against it rather than logging
// it as an error.
var ErrEmpty = errors.New("no pending jobs")

// Add puts a job in pending unless it is already somewhere.
//
// Already somewhere includes done, which is what makes a rerun cheap: the
// second run of the same pipeline adds nothing and the queue is empty.
func (q *Queue) Add(job Job) (bool, error) {
	if job.ID == "" || job.Stage == "" {
		return false, fmt.Errorf("job needs an id and a stage")
	}
	for _, state := range States {
		if _, err := os.Stat(q.path(job.Stage, state, job.ID)); err == nil {
			return false, nil
		}
	}
	if job.Created.IsZero() {
		job.Created = q.now()
	}
	if err := os.MkdirAll(q.dir(job.Stage, Pending), 0o755); err != nil {
		return false, err
	}
	return q.create(Pending, job)
}

// temp writes a job to a uniquely named file in the destination directory and
// returns its path.
//
// Unique matters. The first version used <id>.json.tmp, which is fine until two
// goroutines add the same job at once: they write the same temp file over each
// other and both rename it, and the count of what was inserted is wrong. Found
// by the test, not by reasoning about it.
func (q *Queue) temp(state State, job Job) (string, error) {
	raw, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp(q.dir(job.Stage, state), job.ID+".*.tmp")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, os.Chmod(name, 0o644)
}

// write puts a job file down atomically, so a reader never sees half of one.
// An existing file is replaced, which is what an update wants.
func (q *Queue) write(state State, job Job) error {
	name, err := q.temp(state, job)
	if err != nil {
		return err
	}
	if err := os.Rename(name, q.path(job.Stage, state, job.ID)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// create puts a job file down only if it is not there, and says whether it did.
//
// The exclusion is os.Link, which fails with EEXIST and does so atomically. The
// stat above it is a cheap check across all five states; this is the one that
// actually decides, because between a stat and a write there is room for
// another process to do the same thing.
func (q *Queue) create(state State, job Job) (bool, error) {
	name, err := q.temp(state, job)
	if err != nil {
		return false, err
	}
	defer os.Remove(name)
	err = os.Link(name, q.path(job.Stage, state, job.ID))
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (q *Queue) read(state State, stage Stage, id string) (Job, error) {
	raw, err := os.ReadFile(q.path(stage, state, id))
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return Job{}, fmt.Errorf("decode job %s: %w", id, err)
	}
	return job, nil
}

// ids lists the job ids in one directory, in a stable order so that two workers
// starting together try the same job first and exactly one of them wins the
// rename, rather than both wandering off and colliding later.
func (q *Queue) ids(stage Stage, state State) ([]string, error) {
	entries, err := os.ReadDir(q.dir(stage, state))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(out)
	return out, nil
}

// Lease claims the next pending job for a host, out of one group.
//
// The claim is the rename itself. Two workers that pick the same job both call
// rename, the loser gets ENOENT because the file is already gone, and it moves
// on to the next one. That is the whole mutual exclusion, and it works between
// processes and between machines sharing a directory, which a mutex in this
// program would not.
//
// The group is the part of the target before the slash, which for OCR is the
// book. It is a parameter and not an option because a caller that leases across
// books is a caller that has already gone wrong: an OCR run knows the page
// number from the target and resolves the image against the book it was started
// with, so a job from another book reads the wrong file and writes a real page
// of one volume over a real page of another. An empty group takes anything, and
// only a caller that owns the whole stage should pass it.
func (q *Queue) Lease(stage Stage, host, group string, expected time.Duration) (Job, error) {
	ids, err := q.ids(stage, Pending)
	if err != nil {
		return Job{}, err
	}
	for _, id := range ids {
		job, err := q.read(Pending, stage, id)
		if err != nil {
			if os.IsNotExist(err) {
				continue // somebody else took it between the listing and here
			}
			return Job{}, err
		}
		if group != "" && GroupOf(job.Target) != group {
			continue
		}
		from, to := q.path(stage, Pending, id), q.path(stage, Leased, id)
		if err := os.Rename(from, to); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Job{}, err
		}
		job.Attempts++
		job.Lease = &Lease{Host: host, Until: q.now().Add(expected + LeaseSlack), PID: os.Getpid()}
		if err := q.write(Leased, job); err != nil {
			return Job{}, err
		}
		return job, nil
	}
	return Job{}, ErrEmpty
}

// GroupOf is the part of a target before the slash, or the whole target when
// there is no slash in it.
func GroupOf(target string) string {
	group, _, ok := strings.Cut(target, "/")
	if !ok {
		return target
	}
	return group
}

// Finish records the result of a leased job.
//
// A failure that has attempts left goes back to pending, because the next
// attempt will very likely land on a different host and that is usually all it
// takes. A failure that is out of attempts is dead, and dead jobs are the
// output of this package that anybody actually reads.
func (q *Queue) Finish(job Job, ok bool, reason string) (State, error) {
	// The host is read before the lease is cleared. Doing it the other way
	// round wrote an empty host on every event, which the history test caught:
	// a dead job that does not say which hosts failed it is a dead job nobody
	// can act on.
	host := hostOf(job)
	job.Lease = nil
	job.History = append(job.History, Event{TS: q.now(), Host: host, OK: ok, Reason: reason})

	state := Done
	switch {
	case ok:
		state = Done
	case job.Attempts >= q.maxAttempts():
		state = Dead
	default:
		state = Pending
	}
	if err := q.write(state, job); err != nil {
		return "", err
	}
	if err := os.Remove(q.path(job.Stage, Leased, job.ID)); err != nil && !os.IsNotExist(err) {
		return state, err
	}
	return state, nil
}

func hostOf(job Job) string {
	if job.Lease != nil {
		return job.Lease.Host
	}
	// Finish clears the lease before recording, so fall back to the last host
	// that touched it rather than writing an empty field.
	if n := len(job.History); n > 0 {
		return job.History[n-1].Host
	}
	return ""
}

// Fail is Finish with ok false, which is the common call and reads better at
// the call site than a bare false.
func (q *Queue) Fail(job Job, reason string) (State, error) { return q.Finish(job, false, reason) }

// Release hands a leased job back without spending the attempt.
//
// Fail is for a page that was read and came back wrong. This is for a batch
// that never went out at all: an ssh that would not connect, an rsync with
// nowhere to write, a batch this program refused to assemble. Twenty one pages
// of Algebra I went from pending to dead in forty one seconds one morning, three
// attempts each, without a single image leaving the laptop, because the only
// way to give a job back was to fail it. Attempts are for the model's mistakes.
//
// The attempt is given back too, since Lease spent it, and the handing back is
// still written into the history: a job that quietly loses attempts is a job
// that can loop forever with nothing to read afterwards.
func (q *Queue) Release(job Job, reason string) error {
	host := hostOf(job)
	job.Lease = nil
	if job.Attempts > 0 {
		job.Attempts--
	}
	job.History = append(job.History, Event{TS: q.now(), Host: host, OK: false,
		Reason: "handed back without an attempt: " + reason})
	if err := q.write(Pending, job); err != nil {
		return err
	}
	if err := os.Remove(q.path(job.Stage, Leased, job.ID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Outstanding is every target with a job still to run, and where that job is.
//
// It exists because the id is content addressed on the input hash and a page
// image is not immutable: a retry re-renders page 70 at 600 dpi, the hash
// changes, and the next Add sees work it has never seen before and queues the
// same page twice. Both then land in one batch, pointing at one file, and the
// batch is refused. Targets, not hashes, are what one page means.
func (q *Queue) Outstanding(stage Stage) (map[string]State, error) {
	out := map[string]State{}
	for _, state := range []State{Dead, Pending, Leased} {
		jobs, err := q.List(stage, state)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			out[job.Target] = state
		}
	}
	return out, nil
}

// Reap returns jobs whose lease has expired.
//
// This is the whole crash recovery story. A worker that is killed, or a laptop
// that sleeps through a lease, leaves a job in leased with a deadline in the
// past. Any worker starting up calls Reap and the job is pending again. Nothing
// has to notice the crash and nothing has to be cleaned up by hand.
func (q *Queue) Reap(stage Stage) ([]string, error) {
	ids, err := q.ids(stage, Leased)
	if err != nil {
		return nil, err
	}
	now := q.now()
	var reaped []string
	for _, id := range ids {
		job, err := q.read(Leased, stage, id)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return reaped, err
		}
		if job.Lease != nil && now.Before(job.Lease.Until) {
			continue
		}
		state, err := q.Finish(job, false, "lease expired, the worker did not come back")
		if err != nil {
			return reaped, err
		}
		if state != Done {
			reaped = append(reaped, id)
		}
	}
	return reaped, nil
}

// Retry moves failed and dead jobs back to pending and clears their attempt
// count, for after the thing that was breaking them has been fixed.
func (q *Queue) Retry(stage Stage, states ...State) (int, error) {
	if len(states) == 0 {
		states = []State{Failed, Dead}
	}
	var moved int
	for _, state := range states {
		if state == Pending || state == Done {
			return moved, fmt.Errorf("retry from %s makes no sense", state)
		}
		ids, err := q.ids(stage, state)
		if err != nil {
			return moved, err
		}
		for _, id := range ids {
			job, err := q.read(state, stage, id)
			if err != nil {
				return moved, err
			}
			job.Attempts, job.Lease = 0, nil
			if err := q.write(Pending, job); err != nil {
				return moved, err
			}
			if err := os.Remove(q.path(stage, state, id)); err != nil && !os.IsNotExist(err) {
				return moved, err
			}
			moved++
		}
	}
	return moved, nil
}

// Reset puts one job back in pending wherever it is, with its attempts cleared,
// and says whether it found it.
//
// Retry is the same idea over a whole state, for after a fix. This is for one
// job that is done and is wanted again anyway, which is what translate -force
// asks for. The id is the content address of the work, so a chunk that is asked
// again is the same job and not a new one: the history of what the last answer
// was and which host gave it stays on the file, and that is the point of doing
// it this way rather than by writing a fresh id nobody can line up with the old
// one.
//
// A leased job is left alone. Resetting one would hand the same work to a second
// worker while the first is still holding it, and the lease is there to expire
// on its own.
func (q *Queue) Reset(stage Stage, id string) (bool, error) {
	for _, state := range []State{Done, Failed, Dead} {
		job, err := q.read(state, stage, id)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		job.Attempts, job.Lease = 0, nil
		if err := q.write(Pending, job); err != nil {
			return false, err
		}
		if err := os.Remove(q.path(stage, state, id)); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

// Drain removes pending jobs. Done, failed and dead stay: they are the record
// of what happened, and a queue that forgets its dead jobs is a queue that
// reports a clean run.
func (q *Queue) Drain(stage Stage) (int, error) {
	ids, err := q.ids(stage, Pending)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := os.Remove(q.path(stage, Pending, id)); err != nil && !os.IsNotExist(err) {
			return 0, err
		}
	}
	return len(ids), nil
}

// Stats counts one stage.
type Stats struct {
	Stage   Stage         `json:"stage"`
	Counts  map[State]int `json:"counts"`
	Expired int           `json:"expired"`
}

// Total is every job in the stage, whatever state it is in.
func (s Stats) Total() int {
	var total int
	for _, count := range s.Counts {
		total += count
	}
	return total
}

// Stats counts the jobs in a stage and says how many leases have expired, which
// is the number that says a worker died rather than that work is in flight.
func (q *Queue) Stats(stage Stage) (Stats, error) {
	out := Stats{Stage: stage, Counts: map[State]int{}}
	for _, state := range States {
		ids, err := q.ids(stage, state)
		if err != nil {
			return out, err
		}
		out.Counts[state] = len(ids)
	}
	now := q.now()
	ids, err := q.ids(stage, Leased)
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		job, err := q.read(Leased, stage, id)
		if err != nil {
			continue
		}
		if job.Lease == nil || !now.Before(job.Lease.Until) {
			out.Expired++
		}
	}
	return out, nil
}

// List reads every job in a state, for reports and for the audit that has to
// name the dead ones.
func (q *Queue) List(stage Stage, state State) ([]Job, error) {
	ids, err := q.ids(stage, state)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(ids))
	for _, id := range ids {
		job, err := q.read(state, stage, id)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return out, err
		}
		out = append(out, job)
	}
	return out, nil
}

// Find looks a job up wherever it is.
func (q *Queue) Find(stage Stage, id string) (Job, State, error) {
	for _, state := range States {
		job, err := q.read(state, stage, id)
		if err == nil {
			return job, state, nil
		}
		if !os.IsNotExist(err) {
			return Job{}, "", err
		}
	}
	return Job{}, "", os.ErrNotExist
}

// Table renders queue stats the way queue stats prints them.
func Table(rows []Stats) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%-10s  %8s  %8s  %8s  %8s  %8s  %8s\n",
		"stage", "pending", "leased", "done", "failed", "dead", "expired")
	for _, row := range rows {
		fmt.Fprintf(&out, "%-10s  %8d  %8d  %8d  %8d  %8d  %8d\n",
			row.Stage, row.Counts[Pending], row.Counts[Leased], row.Counts[Done],
			row.Counts[Failed], row.Counts[Dead], row.Expired)
	}
	return out.String()
}

// ParseStage turns a command line argument into a stage.
func ParseStage(name string) (Stage, error) {
	stage := Stage(strings.ToLower(strings.TrimSpace(name)))
	if slices.Contains(Stages, stage) {
		return stage, nil
	}
	names := make([]string, len(Stages))
	for index, value := range Stages {
		names[index] = string(value)
	}
	return "", fmt.Errorf("unknown stage %q, want one of %s", name, strings.Join(names, ", "))
}

// ParseState turns a command line argument into a state.
func ParseState(name string) (State, error) {
	state := State(strings.ToLower(strings.TrimSpace(name)))
	if slices.Contains(States, state) {
		return state, nil
	}
	names := make([]string, len(States))
	for index, value := range States {
		names[index] = string(value)
	}
	return "", fmt.Errorf("unknown state %q, want one of %s", name, strings.Join(names, ", "))
}
