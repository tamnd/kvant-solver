package ocr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// box is a rented Linux machine, as far as this package can tell. It answers
// the four commands the protocol sends, keeps every command it was given so a
// test can read the transcript, and writes the Markdown a Pull would have
// fetched. What it does not do is take four hours.
type box struct {
	commands []string
	pushed   []string
	prompt   string
	pages    []string

	// pid is what start prints. done counts pages the tool has finished, which
	// advances by perPoll on every poll until stopAt, where the process exits.
	pid     int
	done    int
	perPoll int
	stopAt  int

	// failStart, failPolls and failPull are the ways a real run goes wrong.
	failStart error
	failPolls int
	failPull  error
	log       string
	removed   bool
	// noDisplay is a box with no Xvfb running, which is a box where every page
	// fails in a second and the account pool pays for it.
	noDisplay bool
	// killed is the stop command, if one arrived. A batch the driver walked
	// away from has to be killed on the host or it goes on eating the box.
	killed string
}

func (b *box) Run(ctx context.Context, host, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	b.commands = append(b.commands, command)
	switch {
	// Contains and not HasPrefix: the preflight empties the answers directory
	// in the same command it makes the directories with, so it opens with rm.
	case strings.Contains(command, "mkdir -p"):
		if b.noDisplay {
			return "display-down\n", nil
		}
		return "display-up\n", nil
	case strings.Contains(command, "ocr-batch"):
		if b.failStart != nil {
			return "", b.failStart
		}
		return fmt.Sprintf("%d\n", b.pid), nil
	case strings.Contains(command, "kill -0"):
		if b.failPolls > 0 {
			b.failPolls--
			return "", fmt.Errorf("ssh: connection closed")
		}
		b.done = min(b.done+b.perPoll, len(b.pages))
		alive := "running"
		if b.stopAt > 0 && b.done >= b.stopAt {
			alive = "gone"
		}
		return fmt.Sprintf("%d\n%s\n", b.done, alive), nil
	case strings.HasPrefix(command, "kill -TERM"):
		b.killed = command
		return "", nil
	case strings.HasPrefix(command, "tail"):
		return b.log, nil
	case strings.HasPrefix(command, "rm -rf"):
		b.removed = true
		return "", nil
	}
	return "", fmt.Errorf("box was sent a command it does not know: %s", command)
}

func (b *box) Push(ctx context.Context, host string, local []string, remote string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, file := range local {
		name := filepath.Base(file)
		b.pushed = append(b.pushed, name)
		if strings.HasPrefix(name, "prompt-") {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			b.prompt = string(raw)
			continue
		}
		b.pages = append(b.pages, name)
	}
	return nil
}

// Pull writes out whatever the tool had finished by the time it was called,
// which is how a batch that died two thirds of the way through still returns
// two thirds of a chapter.
func (b *box) Pull(ctx context.Context, host, remote, local string) error {
	if b.failPull != nil {
		return b.failPull
	}
	if err := os.MkdirAll(local, 0o755); err != nil {
		return err
	}
	for _, page := range b.pages[:min(b.done, len(b.pages))] {
		body := "A IV.7  POLYNOMIALS  § 1\n\nThe page as the model read it.\n"
		if err := os.WriteFile(filepath.Join(local, OutputName(page)), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

func images(t *testing.T, n int) []string {
	t.Helper()
	directory := t.TempDir()
	var out []string
	for page := 1; page <= n; page++ {
		path := filepath.Join(directory, fmt.Sprintf("%04d.png", page))
		if err := os.WriteFile(path, fmt.Appendf(nil, "not really a png, page %d", page), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, path)
	}
	return out
}

func batch(t *testing.T, machine *box, pages int) Batch {
	t.Helper()
	return Batch{
		Host: Host{Name: "server3", Tool: "/root/chatgpt-tool/.venv/bin/chatgpt-tool", Lanes: 4},
		ID:   "kvant-1975-1-0001", Images: images(t, pages),
		Prompt: "Transcribe the page. Write $x$ for inline mathematics.",
		Dest:   filepath.Join(t.TempDir(), "raw"),
		Shell:  machine, Copy: machine, Sleep: noSleep,
	}
}

func TestABatchGoesOutRunsAndComesBack(t *testing.T) {
	machine := &box{pid: 4242, perPoll: 3}
	work := batch(t, machine, 6)

	result, err := work.Run(context.Background())
	if err != nil {
		t.Fatalf("a batch that worked returned an error: %v", err)
	}
	if result.Wrote != 6 || len(result.Missing) != 0 {
		t.Fatalf("wrote %d of %d, missing %v", result.Wrote, result.Pages, result.Missing)
	}
	if result.PID != 4242 {
		t.Errorf("pid came back as %d", result.PID)
	}
	// The lane count goes in the report so that a week of these lines can say
	// what a second lane was worth. A result without it is a run nobody can
	// learn a concurrency from afterwards.
	if result.Lanes != 4 {
		t.Errorf("the batch reported %d lanes, want the 4 it ran at", result.Lanes)
	}
	// Elapsed is stamped in a defer, and a defer that writes to a local rather
	// than to the named return leaves this at zero.
	if result.Elapsed <= 0 {
		t.Error("the batch reported taking no time at all")
	}
	if len(machine.pages) != 6 {
		t.Errorf("%d images reached the host, want 6", len(machine.pages))
	}
	for page := 1; page <= 6; page++ {
		name := fmt.Sprintf("%04d.md", page)
		if _, err := os.Stat(filepath.Join(work.Dest, name)); err != nil {
			t.Errorf("%s did not come back: %v", name, err)
		}
	}
	if !machine.removed {
		t.Error("the page images were left on a rented box")
	}
}

func TestTheProtocolRunsInOrder(t *testing.T) {
	machine := &box{pid: 7, perPoll: 10}
	work := batch(t, machine, 2)
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{"mkdir -p", "ocr-batch", "kill -0", "rm -rf"}
	if len(machine.commands) != len(want) {
		t.Fatalf("the host was sent %d commands, want %d: %v", len(machine.commands), len(want), machine.commands)
	}
	for i, prefix := range want {
		if !strings.Contains(machine.commands[i], prefix) {
			t.Errorf("command %d was %q, want one containing %q", i, machine.commands[i], prefix)
		}
	}
}

func TestTheStartCommandIsWhatTheToolExpects(t *testing.T) {
	machine := &box{pid: 7, perPoll: 10}
	work := batch(t, machine, 2)
	work.Host.RateDelay = 4.5
	work.Host.PageTimeout = 12 * time.Minute
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	command := machine.commands[1]
	for _, want := range []string{
		"setsid nohup", // outlives the ssh session
		"'/root/chatgpt-tool/.venv/bin/chatgpt-tool'", // the per host path, quoted
		"ocr-batch 'kvant-ocr/in/kvant-1975-1-0001' 'kvant-ocr/out/kvant-1975-1-0001'",
		"-j 4",
		"--rate-delay 4.5",
		"--ext png",
		"--skip-existing",
		"--timeout 720",
		"</dev/null",
		"& echo $!",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("the start command has no %q in it:\n%s", want, command)
		}
	}
	// The prompt goes in by command substitution rather than as a literal. A
	// prompt with dollar signs in it, which this one has, would otherwise be
	// expanded by the remote shell before the tool ever saw it.
	if !strings.Contains(command, `--prompt "$(cat 'kvant-ocr/prompt-`) {
		t.Errorf("the prompt is not read from its file:\n%s", command)
	}

	// The paths in this command are the same paths the mkdir made and rsync
	// pushed to, and they are relative to the login home. Changing directory
	// first makes every one of them mean kvant-ocr/kvant-ocr/..., which
	// on the real host killed the tool at the redirect before it opened a page
	// and left no log to say so.
	if strings.Contains(command, "cd ") {
		t.Errorf("the start command changes directory, so its relative paths no longer resolve:\n%s", command)
	}
	if strings.Contains(command, "kvant-ocr/kvant-ocr") {
		t.Errorf("a path is doubled:\n%s", command)
	}
	for _, want := range []string{
		"'kvant-ocr/in/kvant-1975-1-0001'",
		"'kvant-ocr/out/kvant-1975-1-0001'",
		">'kvant-ocr/logs/kvant-1975-1-0001.log'",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("the start command has no %q in it:\n%s", want, command)
		}
	}
	// The directories the command writes into are the ones the first command
	// made, or the tool has nowhere to put a page.
	made := machine.commands[0]
	for _, want := range []string{"'kvant-ocr/in/kvant-1975-1-0001'", "'kvant-ocr/out/kvant-1975-1-0001'", "'kvant-ocr/logs'"} {
		if !strings.Contains(made, want) {
			t.Errorf("mkdir does not make %s:\n%s", want, made)
		}
	}

	// Chrome needs a display. An ssh command carries none, and the failure
	// without one is not a failure to read a page, it is four accounts banned
	// for eight hours because the tool reads instant launch failures as a
	// throttle.
	if !strings.Contains(command, "DISPLAY=':99'") {
		t.Errorf("the start command gives Chrome no display:\n%s", command)
	}
}

func TestAHostWithNoDisplayIsRefusedBeforeAnythingIsSent(t *testing.T) {
	machine := &box{pid: 7, perPoll: 10, noDisplay: true}
	work := batch(t, machine, 3)
	result, err := work.Run(context.Background())
	if err == nil {
		t.Fatal("a box with no Xvfb should be refused, not sent three pages to fail")
	}
	if !strings.Contains(err.Error(), "Xvfb") || !strings.Contains(err.Error(), ":99") {
		t.Errorf("the error does not say what is wrong or where: %v", err)
	}
	if len(machine.pages) != 0 {
		t.Errorf("%d images were pushed to a box that cannot read them", len(machine.pages))
	}
	for _, command := range machine.commands {
		if strings.Contains(command, "ocr-batch") {
			t.Error("the tool was started on a box with no display, which is what bans accounts")
		}
	}
	if result.Wrote != 0 {
		t.Errorf("wrote = %d, want 0", result.Wrote)
	}
}

// The display is per host, because a box that runs Xvfb somewhere else is a
// configuration question and not a reason to fail.
func TestTheDisplayCanBeSetPerHost(t *testing.T) {
	machine := &box{pid: 7, perPoll: 10}
	work := batch(t, machine, 1)
	work.Host.Display = ":7"
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(machine.commands[0], "'Xvfb :7 '") {
		t.Errorf("the preflight looks for the wrong display:\n%s", machine.commands[0])
	}
	if !strings.Contains(machine.commands[1], "DISPLAY=':7'") {
		t.Errorf("the start command uses the wrong display:\n%s", machine.commands[1])
	}
}

func TestThePromptGoesUpUnderItsOwnHash(t *testing.T) {
	machine := &box{pid: 7, perPoll: 10}
	work := batch(t, machine, 1)
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if machine.prompt != work.Prompt {
		t.Fatalf("the host got a different prompt:\n%q\nwant\n%q", machine.prompt, work.Prompt)
	}
	var name string
	for _, pushed := range machine.pushed {
		if strings.HasPrefix(pushed, "prompt-") {
			name = pushed
		}
	}
	if name == "" {
		t.Fatalf("no prompt was pushed: %v", machine.pushed)
	}
	// Two different prompts must not share a file on the host, or a rerun with
	// a corrected prompt reads pages against the old one.
	other := &box{pid: 7, perPoll: 10}
	second := batch(t, other, 1)
	second.Prompt = work.Prompt + " Keep the running head."
	second.Shell, second.Copy = other, other
	if _, err := second.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, pushed := range other.pushed {
		if strings.HasPrefix(pushed, "prompt-") && pushed == name {
			t.Errorf("two prompts share the file %s", name)
		}
	}
}

func TestABatchThatDiedStillReturnsWhatItRead(t *testing.T) {
	// Four pages read, then the tool exits with two still to go.
	machine := &box{pid: 9, perPoll: 2, stopAt: 4, log: "Traceback: chrome profile 3 is gone"}
	work := batch(t, machine, 6)

	result, err := work.Run(context.Background())
	if err == nil {
		t.Fatal("a batch that stopped early reported success")
	}
	if !strings.Contains(err.Error(), "4 of 6") {
		t.Errorf("the error does not say how far it got: %v", err)
	}
	if result.Wrote != 4 {
		t.Errorf("wrote %d, want the 4 pages that were read", result.Wrote)
	}
	// Naming them is the point. These are the pages the queue has to try again,
	// and a count without names is not something a retry can act on.
	want := []string{"0005.png", "0006.png"}
	if fmt.Sprint(result.Missing) != fmt.Sprint(want) {
		t.Errorf("missing %v, want %v", result.Missing, want)
	}
	if result.Log == "" {
		t.Error("the remote log was not fetched for a batch that failed")
	}
	if !machine.removed {
		t.Error("a failed batch left the page images on a rented box")
	}
}

func TestTheImagesAreRemovedEvenWhenTheBatchNeverStarted(t *testing.T) {
	machine := &box{pid: 9, failStart: fmt.Errorf("no such file or directory")}
	work := batch(t, machine, 3)
	if _, err := work.Run(context.Background()); err == nil {
		t.Fatal("a batch that would not start reported success")
	}
	if !machine.removed {
		t.Error("the page images were left on the host after a failed start")
	}
}

func TestADroppedTunnelIsNotADeadBatch(t *testing.T) {
	// Three polls fail, then it answers. The batch is detached, so a poll that
	// cannot reach the host says nothing about whether the work is running.
	machine := &box{pid: 9, perPoll: 5, failPolls: 3}
	work := batch(t, machine, 5)
	result, err := work.Run(context.Background())
	if err != nil {
		t.Fatalf("three dropped polls killed a healthy batch: %v", err)
	}
	if result.Wrote != 5 {
		t.Errorf("wrote %d of 5", result.Wrote)
	}
}

func TestPollingGivesUpAtTheDeadline(t *testing.T) {
	machine := &box{pid: 9, perPoll: 0} // the tool is alive and doing nothing
	work := batch(t, machine, 4)
	work.Deadline = time.Nanosecond
	if _, err := work.Run(context.Background()); err == nil {
		t.Fatal("a batch that made no progress ran forever")
	}
}

// Giving up on a batch and leaving it running is what turned a slow fleet into
// a dead one over the pilot. server3 went 63 seconds a page, then 100, then
// 428, then 1381, then returned nothing, and an abandoned batch was found still
// running on server2 twenty seven minutes after the driver had written it off.
func TestABatchTheDriverGivesUpOnIsKilledOnTheHost(t *testing.T) {
	machine := &box{pid: 4242, perPoll: 0}
	work := batch(t, machine, 4)
	work.Deadline = time.Nanosecond
	if _, err := work.Run(context.Background()); err == nil {
		t.Fatal("a batch that made no progress reported success")
	}
	if machine.killed == "" {
		t.Fatal("the driver walked away and left the batch running on the host")
	}
	// The negative pid is what reaches Chrome. start uses setsid, so the pid is
	// a process group leader; killing it alone reparents the browsers to init
	// and leaves them exactly where they were, which is the whole problem.
	for _, want := range []string{"kill -TERM -4242", "kill -KILL -4242"} {
		if !strings.Contains(machine.killed, want) {
			t.Errorf("the stop command does not kill the process group:\n%s\nwant %q", machine.killed, want)
		}
	}
}

func TestABatchThatFinishedIsNotKilled(t *testing.T) {
	machine := &box{pid: 9, perPoll: 5}
	work := batch(t, machine, 5)
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatalf("a healthy batch failed: %v", err)
	}
	if machine.killed != "" {
		t.Errorf("a batch that read every page was killed anyway: %s", machine.killed)
	}
}

// A batch the caller cancelled is still burning a rented box, and the cancelled
// context is exactly the one that would stop the kill from being sent.
func TestCancellingTheRunStillStopsTheBatch(t *testing.T) {
	machine := &box{pid: 77, perPoll: 1}
	work := batch(t, machine, 4)
	ctx, cancel := context.WithCancel(context.Background())
	work.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := work.Run(ctx); err == nil {
		t.Fatal("a cancelled run reported success")
	}
	if machine.killed == "" {
		t.Error("a cancelled run left the batch running on the host")
	}
}

func TestTheDeadlineFollowsFromThePagesAndTheLanes(t *testing.T) {
	work := Batch{Host: Host{Lanes: 4, PageTimeout: 10 * time.Minute}, Images: make([]string, 12)}
	// Three rounds of four lanes, ten minutes a page, half again: 45 minutes.
	if got, want := work.deadline(), 45*time.Minute; got != want {
		t.Errorf("deadline %s, want %s", got, want)
	}
	// A partial round still counts as a round.
	work.Images = make([]string, 13)
	if got, want := work.deadline(), 60*time.Minute; got != want {
		t.Errorf("deadline %s for 13 pages, want %s", got, want)
	}
}

func TestCancellingStopsTheRun(t *testing.T) {
	machine := &box{pid: 9, perPoll: 1}
	work := batch(t, machine, 20)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := work.Run(ctx); err == nil {
		t.Fatal("a cancelled run reported success")
	}
}

func TestABatchIDCannotEscapeItsDirectory(t *testing.T) {
	for _, id := range []string{"", "../../etc", "alg i", "a;rm -rf /", "$(whoami)", ".hidden", "'"} {
		if err := ValidBatchID(id); err == nil {
			t.Errorf("%q was accepted as a batch id", id)
		}
	}
	for _, id := range []string{"kvant-1975-1-0001", "alg_iv_vii.2", "abc123"} {
		if err := ValidBatchID(id); err != nil {
			t.Errorf("%q was rejected: %v", id, err)
		}
	}
}

func TestTwoImagesWithOneNameAreRejected(t *testing.T) {
	machine := &box{pid: 9, perPoll: 9}
	work := batch(t, machine, 1)
	// Same base name from two directories. The output is matched back by name,
	// so this would file one answer as both pages.
	work.Images = append(work.Images, filepath.Join(t.TempDir(), filepath.Base(work.Images[0])))
	if err := work.Validate(); err == nil {
		t.Fatal("a batch with two images of the same name was accepted")
	}
}

func TestABatchWithNothingToDoIsAnError(t *testing.T) {
	machine := &box{}
	work := batch(t, machine, 1)
	work.Images = nil
	if err := work.Validate(); err == nil {
		t.Error("an empty batch was accepted")
	}
	work = batch(t, machine, 1)
	work.Host.Tool = ""
	if err := work.Validate(); err == nil {
		t.Error("a host with no chatgpt-tool path was accepted")
	}
	work = batch(t, machine, 1)
	work.Prompt = "  "
	if err := work.Validate(); err == nil {
		t.Error("a batch with no prompt was accepted")
	}
}

func TestQuotingSurvivesTheAwkwardCharacters(t *testing.T) {
	cases := map[string]string{
		"plain":     `'plain'`,
		"with sp":   `'with sp'`,
		"it's":      `'it'\''s'`,
		"$(whoami)": `'$(whoami)'`,
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestTheOutputNameIsTheImageNameWithMarkdownOnIt(t *testing.T) {
	for in, want := range map[string]string{
		"0042.png": "0042.md",
		"0042.jpg": "0042.md",
		"0042":     "0042.md",
	} {
		if got := OutputName(in); got != want {
			t.Errorf("OutputName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPollOutputIsReadLoosely(t *testing.T) {
	// A poll that cannot be read is treated as still running, because
	// abandoning a live batch over a stray line of shell noise costs more than
	// one more poll.
	for _, test := range []struct {
		output string
		count  int
		alive  bool
	}{
		{"12\nrunning\n", 12, true},
		{"12\ngone\n", 12, false},
		{"0\nrunning\n", 0, true},
		{"", 0, true},
		{"bash: warning: setlocale\n7\nrunning\n", 7, true},
	} {
		count, alive := parsePoll(test.output)
		if count != test.count || alive != test.alive {
			t.Errorf("parsePoll(%q) = %d, %t, want %d, %t", test.output, count, alive, test.count, test.alive)
		}
	}
}

func TestASummaryReadsAsOneLine(t *testing.T) {
	result := Result{Host: "server3", ID: "kvant-1975-1-0001", Pages: 25, Wrote: 24,
		Missing: []string{"0017.png"}, Elapsed: Duration(50 * time.Minute)}
	got := result.Summary()
	for _, want := range []string{"server3", "24 of 25", "50m0s", "2m5s a page", "1 missing"} {
		if !strings.Contains(got, want) {
			t.Errorf("the summary has no %q in it: %s", want, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("the summary has a newline in it: %q", got)
	}
	// A batch that read nothing must not divide by zero working out its rate.
	if (Result{}).PerPage() != 0 {
		t.Error("an empty result reported a rate")
	}
}

// TestTheAnswersDirectoryIsEmptiedBeforeTheToolStarts. The poll counts the
// files in it and calls the batch finished when the count reaches the number of
// images, so an answer an earlier run left there is counted as this one's. It
// has cost two runs. The first time, two runs of one volume were given the same
// batch name and a batch of four was declared complete before the tool had
// started; the second time the same seven pictures went out under a rewritten
// prompt, which gave them the same name again, and came back in twelve seconds
// a page still wearing the answers of the prompt before it.
func TestTheAnswersDirectoryIsEmptiedBeforeTheToolStarts(t *testing.T) {
	machine := &box{pid: 11, perPoll: 4}
	work := batch(t, machine, 2)
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := "kvant-ocr/out/" + work.ID
	var cleared, started bool
	for _, command := range machine.commands {
		switch {
		case strings.Contains(command, "rm -rf '"+out+"'"):
			cleared = true
		case strings.Contains(command, "ocr-batch"):
			if !cleared {
				t.Fatalf("the tool started with the answers of an earlier run still in %s", out)
			}
			started = true
		}
	}
	if !cleared || !started {
		t.Errorf("cleared %v started %v, want the answers emptied and then the tool run", cleared, started)
	}
}

func TestKeepLeavesTheImagesWhereTheyAre(t *testing.T) {
	machine := &box{pid: 9, perPoll: 9}
	work := batch(t, machine, 2)
	work.Keep = true
	if _, err := work.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if machine.removed {
		t.Error("keep was asked for and the images were removed anyway")
	}
}
