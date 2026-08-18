package ocr

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tamnd/kvant-solver/answerguard"
	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/mathtex"
	"github.com/tamnd/kvant-solver/queue"
)

// Sheet is one page image waiting to be read, with everything the rules need to
// judge what comes back.
type Sheet struct {
	// Image is the rendered page, the file pdfsrc wrote.
	Image string
	// Index is the position in the scan, one based, and it is what the page
	// file is named after.
	Index int
	// Expect is what rules 1 and 5 are held against.
	Expect Expect
}

// Runner reads an issue into the corpus.
//
// It is written around the queue rather than around a loop over the sheets, and
// the reason is arithmetic. The browser lane reads a page in about two and a
// half minutes, so one issue is two and a half hours and one year is a day and
// a half. Nothing that long survives on hope: a run is stopped, resumed on
// another machine, or restarted after a tunnel drops, and it has to pick up
// exactly where it was. The queue is the memory, the corpus is the output, and
// this type is only the thing that moves a page from one to the other.
type Runner struct {
	Queue  *queue.Queue
	Engine Engine
	Corpus *corpus.Corpus

	// Lang is the tree the pages are written into, which for OCR is always ru.
	Lang string
	// Issue is what is being read.
	Issue corpus.IssueKey
	// Source and Scan go into the provenance of every page: where the scan came
	// from and which file it was.
	Source string
	Scan   string
	// Prompt is the instruction the engine is given, and PromptSHA256 is what
	// goes into the page front matter so a page read by an older prompt is
	// findable later without reading it.
	Prompt       string
	PromptSHA256 string

	// Folio reads the printed number off the foot of a sheet when the engine
	// did not give one. Nil for a general model, which answers the question
	// itself; set for a document model, which never does.
	Folio *Folioer

	// Options carries the opt-in rules, which is the KaTeX checker.
	Options Options
	// Ledger records what each attempt cost. Nil for a run nobody is accounting
	// for, and the runner does not care which.
	Ledger *Ledger
	// Workers is how many pages are in flight at once. One by default: the
	// browser lane is one session per host and asking it for two is how an
	// account gets banned. A served model on a local card wants six or more, and
	// says so.
	Workers int
	// Host names this machine in the queue's lease and in the job history, so
	// that a job left leased by a box that died can be recognised.
	Host string

	Logf func(string, ...any)
	Now  func() time.Time
}

// Summary is what one run did. It is printed at the end and it is what the
// milestone's exit criterion is read off.
type Summary struct {
	Read     int
	Rejected int
	Dead     int
	Failed   int
	Elapsed  time.Duration
}

func (s Summary) String() string {
	return fmt.Sprintf("read %d, rejected %d, dead %d, errors %d, in %s",
		s.Read, s.Rejected, s.Dead, s.Failed, s.Elapsed.Round(time.Second))
}

// Target is the queue's name for one page. It is the page id, which is stable
// across runs and readable in a job file, and it is what GroupOf splits on so
// that one host takes one issue.
func (r *Runner) Target(index int) string {
	return corpus.PageID{Issue: r.Issue, Index: index}.String()
}

// Meta is what a job for this sheet has to carry.
//
// It is exported because a job is written in two places: here, when a sheet is
// first queued, and again when a dead page is revived for the repair lane. The
// second one is the reason it is a method rather than four lines inside
// Enqueue. A revived job keeps its id, because the work is the same work, but
// it must not keep the image path it was written with: the run that revives it
// has staged the sheet itself, possibly under a different cache root or on a
// different machine, and the old path is a file that is not there.
func (r *Runner) Meta(sheet Sheet) map[string]string {
	meta := map[string]string{
		"image": sheet.Image,
		"sheet": strconv.Itoa(sheet.Index),
	}
	if sheet.Expect.Folio > 0 {
		meta["folio"] = strconv.Itoa(sheet.Expect.Folio)
	}
	if sheet.Expect.Cover {
		meta["cover"] = "yes"
	}
	return meta
}

// Enqueue adds a job for every sheet that has no page file yet.
//
// Already read is decided by the corpus and not by the queue, because the
// corpus is the thing that has to be complete. A queue that was thrown away
// still resumes correctly; a corpus that was thrown away is the run happening
// again, which is what it should be.
func (r *Runner) Enqueue(sheets []Sheet) (int, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	added := 0
	for _, sheet := range sheets {
		path := r.Corpus.PagePath(r.lang(), corpus.PageID{Issue: r.Issue, Index: sheet.Index})
		if _, err := os.Stat(path); err == nil {
			continue
		}
		hash, err := hashFile(sheet.Image)
		if err != nil {
			return added, err
		}
		job := queue.New(queue.StageOCR, r.Target(sheet.Index), hash, r.PromptSHA256)
		job.Meta = r.Meta(sheet)
		ok, err := r.Queue.Add(job)
		if err != nil {
			return added, err
		}
		if ok {
			added++
		}
	}
	return added, nil
}

// Run drains the OCR queue for this issue.
//
// It returns when the queue has nothing pending, not when the sheets are all
// read, and those are different: a page that failed three times is dead rather
// than pending, and a run that waited for it would never end. The dead ones are
// counted and the audit names them.
func (r *Runner) Run(ctx context.Context) (Summary, error) {
	if err := r.check(); err != nil {
		return Summary{}, err
	}
	started := r.now()
	if _, err := r.Queue.Reap(queue.StageOCR); err != nil {
		return Summary{}, err
	}

	var (
		mu      sync.Mutex
		summary Summary
		failed  error
		// released remembers the pages this run handed back because their image
		// was not readable. Releasing is right, see one below, but it puts the job
		// straight back on pending where the next lease picks it up again, and if
		// the file is missing rather than briefly away that is a loop with nothing
		// in it but the CPU. Seeing the same page twice is the run finding out that
		// the machine is wrong and not the page.
		released = map[string]bool{}
		halt     bool
	)
	var wait sync.WaitGroup
	for range max(1, r.Workers) {
		wait.Go(func() {
			for {
				if ctx.Err() != nil {
					return
				}
				mu.Lock()
				stop := halt
				mu.Unlock()
				if stop {
					return
				}
				job, err := r.Queue.Lease(queue.StageOCR, r.host(), "", r.lease())
				if errors.Is(err, queue.ErrEmpty) {
					return
				}
				if err != nil {
					mu.Lock()
					failed = errors.Join(failed, err)
					mu.Unlock()
					return
				}
				state, problems, err := r.one(ctx, job)
				mu.Lock()
				switch {
				case err != nil:
					summary.Failed++
					r.log("%s: %v", job.Target, err)
					if state == queue.Pending {
						if released[job.Target] {
							halt = true
							failed = errors.Join(failed, fmt.Errorf(
								"%s: the page image was not readable twice, so it is gone rather than late and the run is stopping, check that the cache holds the staged sheets",
								job.Target))
						}
						released[job.Target] = true
					}
				case len(problems) > 0:
					summary.Rejected++
					if state == queue.Dead {
						summary.Dead++
					}
					r.log("%s: rejected, %s", job.Target, Reasons(problems))
				default:
					summary.Read++
				}
				mu.Unlock()
			}
		})
	}
	wait.Wait()
	summary.Elapsed = r.now().Sub(started)
	if ctx.Err() != nil {
		return summary, ctx.Err()
	}
	return summary, failed
}

// one reads a single page and files the result.
//
// Every exit goes through the queue, including the ones that are our fault
// rather than the model's. A page whose image has gone missing is released
// rather than failed, because the job is fine and the machine is not, and
// counting it against the three attempts would kill a page over a mount that
// came back a minute later.
func (r *Runner) one(ctx context.Context, job queue.Job) (queue.State, []Problem, error) {
	var spent meter
	state, problems, err := r.attempt(ctx, job, &spent)
	r.record(job, spent, problems, err)
	return state, problems, err
}

// meter is what one attempt spent. It is filled in by the read and emptied into
// the ledger once the attempt has been judged, because whether a page was worth
// the tokens is not known until the rules have run.
type meter struct {
	// read says the engine was actually asked. A job that never got that far
	// spent nothing and does not belong in a cost report.
	read    bool
	seconds float64
	usage   api.Usage
}

// record writes the ledger line for one attempt.
//
// A failure to write it is logged and swallowed. The ledger is accounting and
// the corpus is the work, and losing a line of the former is not a reason to
// stop reading pages.
func (r *Runner) record(job queue.Job, spent meter, problems []Problem, err error) {
	if r.Ledger == nil || !spent.read {
		return
	}
	entry := Entry{
		TS:      r.now().UTC(),
		Target:  job.Target,
		Issue:   r.Issue.String(),
		Year:    r.Issue.Year,
		Engine:  r.Engine.Name(),
		Host:    r.host(),
		Seconds: spent.seconds,
		Usage:   spent.usage,
		OK:      err == nil && len(problems) == 0,
	}
	switch {
	case len(problems) > 0:
		entry.Reason = Reasons(problems)
	case err != nil:
		entry.Reason = condense(err.Error())
	}
	if err := r.Ledger.Append(entry); err != nil {
		r.log("%s: the ledger line did not write: %v", job.Target, err)
	}
}

// read asks the engine and times it, taking the token counts from the lanes
// that keep them.
func (r *Runner) read(ctx context.Context, image string, spent *meter) (string, error) {
	started := r.now()
	var (
		raw string
		err error
	)
	if metered, ok := r.Engine.(Metered); ok {
		raw, spent.usage, err = metered.ReadMetered(ctx, image)
	} else {
		raw, err = r.Engine.Read(ctx, image)
	}
	spent.read = true
	spent.seconds = r.now().Sub(started).Seconds()
	return raw, err
}

func (r *Runner) attempt(ctx context.Context, job queue.Job, spent *meter) (queue.State, []Problem, error) {
	image := job.Meta["image"]
	if image == "" {
		state, err := r.Queue.Fail(job, "the job carries no image path")
		return state, nil, orErr(err, fmt.Errorf("%s: no image path", job.Target))
	}
	if _, err := os.Stat(image); err != nil {
		if release := r.Queue.Release(job, "the page image is not readable here"); release != nil {
			return "", nil, release
		}
		return queue.Pending, nil, fmt.Errorf("%s: %w", job.Target, err)
	}

	raw, err := r.read(ctx, image, spent)
	if err != nil {
		state, failErr := r.Queue.Fail(job, condense(err.Error()))
		return state, nil, orErr(failErr, err)
	}

	// The provider's own error page arrives with a successful status and reads
	// like an answer, so it is caught before the rules run. It is not the page's
	// fault and the reason should say so, otherwise the failures report blames
	// a scan for an outage.
	if failure := ProviderFailure(raw); failure != "" {
		state, failErr := r.Queue.Fail(job, "the service answered with an error: "+failure)
		return state, nil, orErr(failErr, fmt.Errorf("%s: %s", job.Target, failure))
	}

	text := mathtex.Numbers(answerguard.Normalise(answerguard.Strip(StripToolHeader(raw))))

	// The folio pass runs before the rules and not after them, because rule 5
	// is the rule it feeds. A band that cannot be read is not the page's fault
	// and does not kill the job: the page goes on to be judged with no folio
	// line, which is the honest description of what was read.
	if r.Folio != nil && !HasFolioLine(text) {
		number, printed, err := r.Folio.Read(ctx, image)
		if err != nil {
			r.log("%s: the folio band did not read: %v", job.Target, err)
		} else {
			text = MarkFolio(text, number, printed)
		}
	}

	expect := r.expect(job)
	if problems := Validate(text, expect, r.Options); len(problems) > 0 {
		state, err := r.Queue.Fail(job, Reasons(problems))
		if err != nil {
			return state, problems, err
		}
		if state == queue.Dead {
			if err := r.repair(job, problems); err != nil {
				return state, problems, err
			}
		}
		return state, problems, nil
	}

	if err := r.write(job, text, image); err != nil {
		state, failErr := r.Queue.Fail(job, condense(err.Error()))
		return state, nil, orErr(failErr, err)
	}
	state, err := r.Queue.Finish(job, true, FolioNote(text))
	return state, nil, err
}

// write puts one accepted page in the corpus.
func (r *Runner) write(job queue.Job, text, image string) error {
	index, err := strconv.Atoi(job.Meta["sheet"])
	if err != nil {
		return fmt.Errorf("%s: the job carries no sheet number", job.Target)
	}
	id := corpus.PageID{Issue: r.Issue, Index: index}
	front := &corpus.PageFront{
		Issue:     r.Issue.String(),
		Year:      r.Issue.Year,
		Number:    r.Issue.Number,
		PageIndex: index,
		PageLabel: pageLabel(text),
		Rubrics:   Rubrics(text),
		Illegible: strings.Count(text, Illegible),
		Provenance: corpus.Provenance{
			Lang:            r.lang(),
			Source:          r.Source,
			SourceScan:      r.Scan,
			Extraction:      corpus.ExtractionVision,
			ExtractionModel: r.Engine.Name(),
			PromptSHA256:    r.PromptSHA256,
		},
	}
	_ = image
	return corpus.Save(r.Corpus.PagePath(r.lang(), id), front, text)
}

// repair queues a dead page for a second look.
//
// A page that failed three times is not read again the same way, because three
// identical attempts have already said what that produces. What goes on the
// repair queue is the rule that killed it, so that whoever drains that queue
// knows whether they are looking for a scan to re-render at a higher resolution
// or a page the model will not read at all.
func (r *Runner) repair(job queue.Job, problems []Problem) error {
	repair := queue.New(queue.StageRepair, job.Target, job.InputSHA256, job.PromptSHA256)
	repair.Meta = maps.Clone(job.Meta)
	if repair.Meta == nil {
		repair.Meta = map[string]string{}
	}
	rules := Rules(problems)
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, string(rule))
	}
	repair.Meta["rules"] = strings.Join(names, ",")
	repair.Meta["reasons"] = Reasons(problems)
	// The one repair that is mechanical. An unreadable page and a formula that
	// will not parse are both usually the render and not the model, so the next
	// attempt gets more pixels rather than another try at the same ones.
	if slices.Contains(rules, RuleIllegible) || slices.Contains(rules, RuleLaTeX) {
		repair.Meta["dpi"] = "400"
	}
	_, err := r.Queue.Add(repair)
	return err
}

func (r *Runner) expect(job queue.Job) Expect {
	expect := Expect{Issue: r.Issue.String(), Cover: job.Meta["cover"] == "yes"}
	expect.Sheet, _ = strconv.Atoi(job.Meta["sheet"])
	expect.Folio, _ = strconv.Atoi(job.Meta["folio"])
	return expect
}

// pageLabel is the printed number as a string, empty when the page prints none.
// It is a string in the front matter because a few pages are numbered with
// roman numerals and one supplement is numbered with letters.
func pageLabel(text string) string {
	if n, ok := Folio(text); ok {
		return strconv.Itoa(n)
	}
	return ""
}

// lease is how long a worker holds a page. It is the page timeout plus the
// queue's own slack, so a page that is slow is not taken away from the worker
// that is still reading it.
func (r *Runner) lease() time.Duration { return DefaultPageTimeout }

func (r *Runner) check() error {
	switch {
	case r.Queue == nil:
		return errors.New("no queue")
	case r.Engine == nil:
		return errors.New("no engine")
	case r.Corpus == nil:
		return errors.New("no corpus")
	case r.Issue.Zero():
		return errors.New("no issue")
	case strings.TrimSpace(r.PromptSHA256) == "":
		return errors.New("no prompt hash, so a page could not say what read it")
	}
	return nil
}

func (r *Runner) lang() string {
	if r.Lang == "" {
		return corpus.DefaultLang
	}
	return r.Lang
}

func (r *Runner) host() string {
	if r.Host == "" {
		name, err := os.Hostname()
		if err != nil || name == "" {
			return "unknown"
		}
		return name
	}
	return r.Host
}

func (r *Runner) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

func (r *Runner) log(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// Sheets turns a directory of rendered page images into the work list.
//
// The order is the file order, which pdfsrc wrote as %04d, and the index is the
// position in that order rather than anything read off the page. The printed
// number comes from the page itself later, which is the only place it is
// trustworthy.
func Sheets(dir string, expect func(index int) Expect) ([]Sheet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || mediaType(entry.Name()) == "" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	sheets := make([]Sheet, 0, len(names))
	for i, name := range names {
		index := i + 1
		sheet := Sheet{Image: filepath.Join(dir, name), Index: index}
		if expect != nil {
			sheet.Expect = expect(index)
		}
		sheet.Expect.Sheet = index
		sheets = append(sheets, sheet)
	}
	return sheets, nil
}

func hashFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return corpus.HashString(string(raw)), nil
}

func orErr(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
