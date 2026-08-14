package ocr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// OCR does not go over HTTP. The proxy on each box speaks chat completions and
// nothing else: it takes text and returns text, and there is no way to hand it
// an image. The only thing on those boxes that can read a page is chatgpt-tool
// ocr-batch, which walks a directory of images and mirrors it into a directory
// of Markdown. So the transport for this stage is ssh and rsync, the images go
// to the box, the batch runs there under setsid so it survives the ssh session,
// and the Markdown comes back.
//
// Everything about the protocol below follows from two facts. A page takes
// minutes, so a batch runs detached and is polled rather than waited on. And the
// boxes are rented, so the page images are removed from them when the batch is
// done rather than left to accumulate.

// RemoteRoot is the scratch directory on a host, relative to the login home.
const RemoteRoot = "kvant-ocr"

// DefaultPoll is how often a running batch is asked how far it has got. A page
// is minutes, so anything faster is just ssh handshakes.
const DefaultPoll = 30 * time.Second

// DefaultPageTimeout is the per image timeout handed to ocr-batch. The tool
// defaults to 120 seconds, which is under the measured 151 second round trip
// for nine tokens of text, so leaving it at the default would time out most of
// a page of mathematics.
const DefaultPageTimeout = 15 * time.Minute

// DefaultRateDelay is the minimum gap between job starts on one host. It is
// what keeps a browser pool from opening every tab at once.
const DefaultRateDelay = 6.0

// Host is one rented box that can run a batch.
type Host struct {
	// Name is the ssh destination, which is a stanza in the user's ssh config
	// and not a hostname this repo knows.
	Name string
	// Tool is the absolute path to chatgpt-tool on that box. It differs per
	// host, so it is discovered by fleet doctor and never assumed.
	Tool string
	// Lanes is -j, how many pages the host reads at once. Measured, not guessed:
	// each lane is a Chrome profile and a browser needs about a gigabyte.
	//
	// Zero means the host does no OCR at all, which is a real case: server1 has
	// under a gigabyte free and cannot run a browser, so it takes model calls
	// over HTTP and no page images. A run skips it rather than sending it one
	// batch it will fail.
	Lanes int
	// RateDelay is --rate-delay in seconds.
	RateDelay float64
	// PageTimeout is --timeout, per image.
	PageTimeout time.Duration
	// Root is the scratch directory, relative to the login home. Empty means
	// RemoteRoot.
	Root string
	// Display is the X display Chrome opens on. Empty means DefaultDisplay.
	//
	// A browser needs a display even when nobody is looking at it. An ssh
	// command carries none, so without this every page fails at
	// launch_persistent_context and the tool reads a pile of instant failures
	// as an IP level block and bans four accounts for eight hours. That is not
	// a hypothetical, it is what the first live batch did.
	Display string
}

// DefaultDisplay is where Xvfb is on these hosts. run-serve.sh starts it on :99
// and leaves it running, which is why serve works and an ssh command does not.
const DefaultDisplay = ":99"

func (h Host) display() string {
	if strings.TrimSpace(h.Display) != "" {
		return h.Display
	}
	return DefaultDisplay
}

func (h Host) root() string {
	if strings.TrimSpace(h.Root) != "" {
		return h.Root
	}
	return RemoteRoot
}

func (h Host) lanes() int {
	if h.Lanes > 0 {
		return h.Lanes
	}
	return 1
}

func (h Host) rateDelay() float64 {
	if h.RateDelay > 0 {
		return h.RateDelay
	}
	return DefaultRateDelay
}

func (h Host) pageTimeout() time.Duration {
	if h.PageTimeout > 0 {
		return h.PageTimeout
	}
	return DefaultPageTimeout
}

// Validate reports what a host is missing before a batch is built for it.
func (h Host) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("host has no ssh name")
	}
	if strings.TrimSpace(h.Tool) == "" {
		return fmt.Errorf("host %s has no chatgpt-tool path", h.Name)
	}
	if h.Lanes < 0 {
		return fmt.Errorf("host %s has negative lanes", h.Name)
	}
	return nil
}

// Shell runs a command on a host. fleet.SSH satisfies it, and so does a fake,
// which is the only reason any of this is testable without three rented boxes.
type Shell interface {
	Run(ctx context.Context, host, command string) (string, error)
}

// Copier moves files between here and a host.
type Copier interface {
	// Push copies local files into a remote directory, which it creates.
	Push(ctx context.Context, host string, local []string, remote string) error
	// Pull copies the contents of a remote directory into a local one.
	Pull(ctx context.Context, host, remote, local string) error
}

// Batch is one directory of page images read on one host.
type Batch struct {
	Host Host
	// ID names the batch on the host. It goes in a path, so it is checked.
	ID string
	// Images are local page image paths. They are pushed in this order and the
	// output is matched back to them by name.
	Images []string
	// Prompt is the OCR instruction text, handed to the tool on its command
	// line rather than left to the tool's built-in prompt.
	Prompt string
	// Dest is the local directory the Markdown is pulled back into.
	Dest string

	Shell Shell
	Copy  Copier

	// Poll is how often the batch is asked how far it has got.
	Poll time.Duration
	// Deadline bounds the whole batch. Zero derives one from the page count,
	// the lanes and the per page timeout, which is the honest bound: a batch
	// cannot legitimately take longer than reading every page slowly.
	Deadline time.Duration
	// Keep leaves the page images on the host. Off by default: these are rented
	// boxes and a scanned book should not sit on one after it has been read.
	Keep bool
	Logf func(string, ...any)
	// Sleep is the wait between polls, replaceable so that a test of a run that
	// takes an hour does not take an hour.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Result is what a batch produced.
type Result struct {
	Host string `json:"host"`
	ID   string `json:"id"`
	// Pages is how many images went out, Wrote how many Markdown files came
	// back, and Missing which pages did not.
	Pages int `json:"pages"`
	Wrote int `json:"wrote"`
	// Lanes is how many pages the host was reading at once. Without it the
	// usage log cannot answer what a second lane bought, which is the whole
	// question fleet bench exists to settle.
	Lanes   int      `json:"lanes,omitempty"`
	Missing []string `json:"missing,omitempty"`
	PID     int      `json:"pid"`
	Elapsed Duration `json:"elapsed"`
	// Log is the tail of the remote log, kept only when something went wrong.
	// A successful batch's log is on the box and nobody reads it.
	Log string `json:"log,omitempty"`
}

// Duration prints as a string in a report rather than as a nanosecond count.
type Duration time.Duration

func (d Duration) String() string { return time.Duration(d).Round(time.Second).String() }

// MarshalJSON writes the duration as the string String prints.
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(d.String())), nil
}

// UnmarshalJSON reads back what MarshalJSON wrote.
//
// A type that can write a report it cannot read is a report nothing can total
// up, and totalling the reports over a week is the whole reason the elapsed
// times are kept. The number form is accepted too, because a hand written file
// is likelier to carry one than "20m0s".
func (d *Duration) UnmarshalJSON(raw []byte) error {
	if len(raw) > 0 && raw[0] == '"' {
		text, err := strconv.Unquote(string(raw))
		if err != nil {
			return err
		}
		value, err := time.ParseDuration(text)
		if err != nil {
			return err
		}
		*d = Duration(value)
		return nil
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return fmt.Errorf("duration: want \"20m0s\" or a nanosecond count, got %s", raw)
	}
	*d = Duration(value)
	return nil
}

// PerPage is what one page cost, which is the number the fleet is ranked on.
func (r Result) PerPage() time.Duration {
	if r.Wrote == 0 {
		return 0
	}
	return time.Duration(r.Elapsed) / time.Duration(r.Wrote)
}

// Summary is the one line a run prints per batch.
func (r Result) Summary() string {
	line := fmt.Sprintf("%s %s: %d of %d pages in %s", r.Host, r.ID, r.Wrote, r.Pages, r.Elapsed)
	if r.Wrote > 0 {
		line += fmt.Sprintf(", %s a page", r.PerPage().Round(time.Second))
	}
	if n := len(r.Missing); n > 0 {
		line += fmt.Sprintf(", %d missing", n)
	}
	return line
}

func (b Batch) logf(format string, args ...any) {
	if b.Logf != nil {
		b.Logf(format, args...)
	}
}

func (b Batch) poll() time.Duration {
	if b.Poll > 0 {
		return b.Poll
	}
	return DefaultPoll
}

// deadline is how long the batch is allowed to take.
//
// The bound is every page taking its full per page timeout, spread over the
// lanes, plus half again. A batch that exceeds that is not slow, it is stuck,
// and the honest thing is to stop polling and report it rather than hold a
// queue lease open until somebody notices tomorrow.
func (b Batch) deadline() time.Duration {
	if b.Deadline > 0 {
		return b.Deadline
	}
	rounds := (len(b.Images) + b.Host.lanes() - 1) / b.Host.lanes()
	return time.Duration(rounds) * b.Host.pageTimeout() * 3 / 2
}

func (b Batch) sleep(ctx context.Context, d time.Duration) error {
	if b.Sleep != nil {
		return b.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Validate checks a batch before it moves a single byte.
//
// The id ends up in a shell command and in a path on someone else's machine, so
// it is restricted to what a directory name should be rather than quoted and
// hoped for.
func (b Batch) Validate() error {
	if err := b.Host.Validate(); err != nil {
		return err
	}
	if err := ValidBatchID(b.ID); err != nil {
		return err
	}
	if len(b.Images) == 0 {
		return fmt.Errorf("batch %s has no images", b.ID)
	}
	if strings.TrimSpace(b.Prompt) == "" {
		return fmt.Errorf("batch %s has no prompt", b.ID)
	}
	if strings.TrimSpace(b.Dest) == "" {
		return fmt.Errorf("batch %s has nowhere to put the output", b.ID)
	}
	if b.Shell == nil || b.Copy == nil {
		return fmt.Errorf("batch %s has no transport", b.ID)
	}
	seen := map[string]string{}
	for _, image := range b.Images {
		name := filepath.Base(image)
		// The output is matched back to the input by name, so two inputs with
		// the same base name would silently share one answer.
		if other, ok := seen[name]; ok {
			return fmt.Errorf("batch %s has two images named %s: %s and %s", b.ID, name, other, image)
		}
		seen[name] = image
	}
	return nil
}

// ValidBatchID says whether a name is safe to use as a remote directory.
func ValidBatchID(id string) error {
	if id == "" {
		return fmt.Errorf("batch has no id")
	}
	if len(id) > 64 {
		return fmt.Errorf("batch id %q is too long", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("batch id %q has a %q in it, want letters, digits, dash, underscore or dot", id, r)
		}
	}
	if strings.HasPrefix(id, ".") {
		return fmt.Errorf("batch id %q starts with a dot", id)
	}
	return nil
}

// Run performs the whole protocol and returns what came back.
//
// The steps are: make the directories, push the prompt, push the images, start
// the tool detached, poll until it stops or finishes, pull the Markdown, and
// remove the images from the host. The last one runs even when an earlier step
// failed, because leaving a book on a rented box is the failure that matters
// after everything else has been retried.
// The return value is named so that the deferred stamp below lands in what the
// caller gets. Assigning to a local in a defer is assigning to something the
// return statement has already copied, and the elapsed time came back zero.
func (b Batch) Run(ctx context.Context) (result Result, err error) {
	if err := b.Validate(); err != nil {
		return Result{}, err
	}
	result = Result{Host: b.Host.Name, ID: b.ID, Pages: len(b.Images), Lanes: b.Host.Lanes}
	started := time.Now()
	defer func() { result.Elapsed = Duration(time.Since(started)) }()

	root := b.Host.root()
	in := path(root, "in", b.ID)
	out := path(root, "out", b.ID)
	logFile := path(root, "logs", b.ID+".log")

	// The answers directory is emptied rather than reused, which is what the
	// empty first argument to prepare is for. What the poll counts is the files
	// in it, so an answer an earlier run left behind is counted as this run's:
	// a batch of four was declared finished before the tool had started and
	// four answers to a question nobody had asked came home. That has happened
	// twice, once when two runs of one volume shared a name and once when the
	// same seven pictures went out under a rewritten prompt and came back in
	// twelve seconds a page wearing the old prompt's answers.
	if err := prepare(ctx, b.Shell, b.Host, out, in, out, path(root, "logs")); err != nil {
		return result, err
	}
	// Removing the images is not conditional on the rest working.
	defer func() {
		if b.Keep {
			return
		}
		if _, err := b.Shell.Run(context.WithoutCancel(ctx), b.Host.Name, "rm -rf "+quote(in)); err != nil {
			b.logf("could not remove %s from %s: %v", in, b.Host.Name, err)
		}
	}()

	prompt, err := b.pushPrompt(ctx, root)
	if err != nil {
		return result, err
	}
	b.logf("%s: pushing %d images to %s", b.Host.Name, len(b.Images), in)
	if err := b.Copy.Push(ctx, b.Host.Name, b.Images, in); err != nil {
		return result, fmt.Errorf("push images to %s: %w", b.Host.Name, err)
	}

	pid, err := b.start(ctx, root, in, out, prompt, logFile)
	if err != nil {
		return result, err
	}
	result.PID = pid
	b.logf("%s: batch %s running as pid %d, %d lanes, %d pages", b.Host.Name, b.ID, pid, b.Host.lanes(), len(b.Images))

	wrote, pollErr := b.wait(ctx, out, pid, started)
	result.Wrote = wrote
	if pollErr != nil {
		b.stop(ctx, pid)
	}

	// The output is pulled whether or not the run finished. A batch that died
	// two thirds of the way through still read two thirds of a chapter, and
	// paying for those pages twice is the one thing worth avoiding here.
	if err := b.Copy.Pull(context.WithoutCancel(ctx), b.Host.Name, out+"/", b.Dest); err != nil {
		if pollErr == nil {
			return result, fmt.Errorf("pull from %s: %w", b.Host.Name, err)
		}
		b.logf("could not pull %s from %s: %v", out, b.Host.Name, err)
	}
	result.Missing = b.missing()
	result.Wrote = len(b.Images) - len(result.Missing)

	if pollErr != nil || len(result.Missing) > 0 {
		result.Log = b.tail(ctx, logFile)
	}
	return result, pollErr
}

// prepare makes the scratch directories on a host and answers the only question
// worth asking before anything is sent: is there a display for Chrome to open
// on.
//
// One round trip for both. A browser started without a display fails in a
// second, and the tool reads a pile of instant failures as an IP level block
// and bans the accounts behind them for eight hours, so asking first costs one
// ssh and saves a pool.
//
// clear is a directory to empty first, or the empty string for none. It is
// removed before the others are made, so naming it in dirs as well is how a
// caller says empty this one and then make it again.
func prepare(ctx context.Context, shell Shell, host Host, clear string, dirs ...string) error {
	quoted := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		quoted = append(quoted, quote(dir))
	}
	command := "mkdir -p " + strings.Join(quoted, " ")
	if clear != "" {
		command = "rm -rf " + quote(clear) + " && " + command
	}
	ready, err := shell.Run(ctx, host.Name, fmt.Sprintf(
		"%s && (pgrep -f %s >/dev/null && echo display-up || echo display-down)",
		command, quote("Xvfb "+host.display()+" ")))
	if err != nil {
		return fmt.Errorf("prepare %s: %w", host.Name, err)
	}
	if strings.Contains(ready, "display-down") {
		return fmt.Errorf("%s has no Xvfb on %s, so Chrome cannot start: run chatgpt-tool's run-serve.sh there first",
			host.Name, host.display())
	}
	return nil
}

// pushPrompt puts the prompt on the host, named by its hash.
//
// By hash, because the prompt is what the answers are a function of. Two runs
// with different prompts must not share a file, and a host that already has
// this exact prompt needs nothing copied.
func (b Batch) pushPrompt(ctx context.Context, root string) (string, error) {
	sum := sha256.Sum256([]byte(b.Prompt))
	name := "prompt-" + hex.EncodeToString(sum[:])[:8] + ".md"

	scratch, err := os.MkdirTemp("", "kvant-prompt-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	local := filepath.Join(scratch, name)
	if err := os.WriteFile(local, []byte(b.Prompt), 0o644); err != nil {
		return "", err
	}
	if err := b.Copy.Push(ctx, b.Host.Name, []string{local}, root); err != nil {
		return "", fmt.Errorf("push prompt to %s: %w", b.Host.Name, err)
	}
	return path(root, name), nil
}

// start launches the tool detached and returns its pid.
//
// setsid and nohup, so the batch outlives the ssh session that started it: a
// dropped laptop lid must not kill four hours of reading. Standard output goes
// to a log file and standard input to /dev/null, because ssh will not return
// while the backgrounded process still holds the channel open.
//
// The prompt is passed as "$(cat file)". Command substitution output is not
// re-expanded by the shell, so the dollar signs of the LaTeX in that prompt
// arrive at the tool as themselves.
//
// There is no cd. Every path here is already relative to the login home, which
// is where an ssh command starts and where rsync puts the images. The first
// version of this changed directory into the scratch root first, which made
// kvant-ocr/in/<id> mean kvant-ocr/kvant-ocr/in/<id>; the redirect to
// a log file under a directory that did not exist killed the tool before it
// opened a page, and the run reported four pages missing with an empty log,
// because there was no log. Forty seconds for four pages of mathematics was the
// tell.
func (b Batch) start(ctx context.Context, _, in, out, prompt, logFile string) (int, error) {
	command := fmt.Sprintf(
		"DISPLAY=%s setsid nohup %s ocr-batch %s %s -j %d --rate-delay %s --ext png "+
			"--skip-existing --recursive --timeout %d --prompt \"$(cat %s)\" >%s 2>&1 </dev/null & echo $!",
		quote(b.Host.display()), quote(b.Host.Tool), quote(in), quote(out),
		b.Host.lanes(), strconv.FormatFloat(b.Host.rateDelay(), 'f', -1, 64),
		int(b.Host.pageTimeout().Seconds()), quote(prompt), quote(logFile))

	output, err := b.Shell.Run(ctx, b.Host.Name, command)
	if err != nil {
		return 0, fmt.Errorf("start batch on %s: %w", b.Host.Name, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lastLine(output)))
	if err != nil {
		return 0, fmt.Errorf("start batch on %s: no pid in %q", b.Host.Name, condense(output))
	}
	return pid, nil
}

// wait polls until the batch finishes, dies, or runs out of time.
//
// One ssh per poll, carrying both numbers: how many files are written and
// whether the process is still there. Two calls would be two handshakes for
// facts that have to agree with each other.
func (b Batch) wait(ctx context.Context, out string, pid int, started time.Time) (int, error) {
	command := fmt.Sprintf("ls -1 %s 2>/dev/null | grep -c '\\.md$'; kill -0 %d 2>/dev/null && echo running || echo gone",
		quote(out), pid)
	deadline := b.deadline()
	var last int
	for {
		if err := b.sleep(ctx, b.poll()); err != nil {
			return last, err
		}
		output, err := b.Shell.Run(ctx, b.Host.Name, command)
		if err != nil {
			// A poll that fails is a dropped tunnel far more often than a dead
			// batch, and the batch is detached, so the right move is to poll
			// again rather than abandon work that is still running.
			b.logf("%s: poll failed, will try again: %v", b.Host.Name, err)
			if elapsed := time.Since(started); elapsed > deadline {
				return last, fmt.Errorf("batch %s on %s: gave up after %s: %w", b.ID, b.Host.Name, elapsed.Round(time.Second), err)
			}
			continue
		}
		count, alive := parsePoll(output)
		if count > last {
			b.logf("%s: batch %s at %d of %d pages after %s", b.Host.Name, b.ID, count, len(b.Images), time.Since(started).Round(time.Second))
		}
		last = count
		if count >= len(b.Images) {
			return last, nil
		}
		if !alive {
			return last, fmt.Errorf("batch %s on %s stopped with %d of %d pages written",
				b.ID, b.Host.Name, count, len(b.Images))
		}
		if elapsed := time.Since(started); elapsed > deadline {
			return last, fmt.Errorf("batch %s on %s: %d of %d pages after %s, giving up",
				b.ID, b.Host.Name, count, len(b.Images), elapsed.Round(time.Second))
		}
	}
}

// StopGrace is how long the tool is given to close its browser contexts before
// the group is killed outright.
const StopGrace = 5 * time.Second

// stop kills a batch the driver has given up on, and everything it started.
//
// The batch is detached on purpose, so if this does not kill it nothing will.
// Leaving it running is neither free nor neutral. The pilot abandoned a batch
// on server2 at 22m56s and found it still going twenty seven minutes later with
// two Chrome instances under it, on a six core box already at load 11. The next
// batch sent there was slower, was abandoned in its turn, and left another
// behind it. Measured over the pilot, server3 went 63 seconds a page, then 100,
// then 428, then 1381, then returned nothing at all. That decay is not the
// model getting tired, it is the box filling up with work nobody is waiting for
// any more, and it is self inflicted.
//
// The negative pid is the whole point. start uses setsid, so the pid is a
// process group leader and -pid reaches the tool, Chrome, and Chrome's
// renderers together. Killing the leader alone reparents the browsers to init
// and leaves them exactly where they were.
//
// Failure here is logged and not returned. The caller is already reporting a
// batch that did not finish, and a second error about the cleanup would replace
// the reason the pages are missing with a footnote about it.
func (b Batch) stop(ctx context.Context, pid int) {
	// A caller that gave up because its own context was cancelled still has to
	// clean up after itself, so the kill runs on a context that outlives it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), StopGrace+30*time.Second)
	defer cancel()
	command := fmt.Sprintf("kill -TERM -%d 2>/dev/null; sleep %d; kill -KILL -%d 2>/dev/null; true",
		pid, int(StopGrace.Seconds()), pid)
	if _, err := b.Shell.Run(ctx, b.Host.Name, command); err != nil {
		b.logf("%s: could not stop batch %s at pid %d, it may still be running: %v", b.Host.Name, b.ID, pid, err)
		return
	}
	b.logf("%s: stopped batch %s at pid %d and everything under it", b.Host.Name, b.ID, pid)
}

// parsePoll reads the two lines the poll command prints. Anything it cannot
// read is treated as still running, because the alternative is abandoning a
// live batch over a stray line of shell noise.
func parsePoll(output string) (int, bool) {
	var count int
	alive := true
	for line := range strings.FieldsSeq(output) {
		switch line {
		case "running":
			alive = true
		case "gone":
			alive = false
		default:
			if value, err := strconv.Atoi(line); err == nil {
				count = value
			}
		}
	}
	return count, alive
}

// missing is the pages that went out and did not come back.
func (b Batch) missing() []string {
	var out []string
	for _, image := range b.Images {
		path := filepath.Join(b.Dest, OutputName(filepath.Base(image)))
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			out = append(out, filepath.Base(image))
		}
	}
	sort.Strings(out)
	return out
}

// OutputName is what ocr-batch calls the Markdown for an image. The tool
// mirrors the input tree and swaps the extension, so 0042.png becomes 0042.md.
func OutputName(image string) string {
	return strings.TrimSuffix(image, filepath.Ext(image)) + ".md"
}

// tail reads the end of the remote log, which is the only account of what the
// tool thought it was doing when a batch went wrong.
func (b Batch) tail(ctx context.Context, logFile string) string {
	output, err := b.Shell.Run(context.WithoutCancel(ctx), b.Host.Name, "tail -n 25 "+quote(logFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

func path(parts ...string) string { return strings.Join(parts, "/") }

// quote wraps a string so the remote shell takes it literally. Single quotes
// with the usual escape for a single quote inside them, which is the only form
// that is safe for every byte a path can hold.
func quote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return lines[len(lines)-1]
}

func condense(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 120 {
		text = text[:120] + "..."
	}
	return text
}

// Rsync copies files with the rsync binary over the user's own ssh config.
type Rsync struct {
	// Binary is the rsync to run. Empty means the one on PATH.
	Binary string
	// Timeout bounds one transfer. A batch of 25 pages at 300 dpi is about five
	// megabytes, so this is generous rather than tight.
	Timeout time.Duration
	Logf    func(string, ...any)
}

func (r Rsync) binary() string {
	if strings.TrimSpace(r.Binary) != "" {
		return r.Binary
	}
	return "rsync"
}

func (r Rsync) run(ctx context.Context, args ...string) error {
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	command := exec.CommandContext(ctx, r.binary(), args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync: %s: %w", condense(string(output)), err)
	}
	return nil
}

// Push copies local files into a remote directory.
//
// Archive and compress, and no --delete anywhere in this package: the remote
// directory is scratch that this tool created, but a flag that removes files
// on someone else's machine has no business being on a path that also takes a
// user supplied directory name.
//
// The destination directory is made by the mkdir that opens the protocol
// rather than by --mkpath, because the rsync macOS ships is openrsync and
// reports itself as 2.6.9 compatible, which predates that flag by fifteen
// years.
func (r Rsync) Push(ctx context.Context, host string, local []string, remote string) error {
	if len(local) == 0 {
		return nil
	}
	args := append([]string{"-az"}, local...)
	return r.run(ctx, append(args, host+":"+remote+"/")...)
}

// Pull copies a remote directory's contents into a local one.
func (r Rsync) Pull(ctx context.Context, host, remote, local string) error {
	if err := os.MkdirAll(local, 0o755); err != nil {
		return err
	}
	return r.run(ctx, "-az", host+":"+remote, local+"/")
}
