package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Asking a fresh question, as against reading a page or following one up.
//
// This is in package ocr and it is not OCR. What it needs is the transport, and
// the transport is here: a Host, an ssh Shell, an rsync Copier, a scratch
// directory prepared on a rented box, a detached process polled to completion
// and killed by process group when the deadline passes. Every one of those was
// written for reading pages and every one of them is right for this. A second
// copy of it in another package would be a second copy to get wrong.
//
// The difference from Follow is one line of the command and all of the meaning.
// Follow speaks into a conversation that already holds a page image, which is
// what makes a repair cheap and what makes it impossible to move between
// accounts. Ask opens a new conversation with nothing in it, so it goes to any
// host that will take work, and the reply comes back with the conversation URL
// so that a caller who wants to ask again knows where.
//
// It needs chatgpt-tool ask, which is tamnd/chatgpt-tool#7. A host without it
// fails the call with the tool's own usage message, which is clear enough that
// it is not worth probing for.

// Ask is one question put to a new conversation on a host.
type Ask struct {
	Host  Host
	Shell Shell
	Copy  Copier

	// Prompt is the whole question. It goes over as a file and never onto a
	// command line: these prompts carry LaTeX, which is mostly dollars and
	// backslashes, and a remote shell would expand every one of them.
	Prompt string
	// ID names the scratch directory on the host.
	ID string
	// Profile is the Chrome profile to ask from. Empty lets the tool route to
	// whichever account is least recently used, which is what a fresh question
	// should do; it is set only when a caller needs a particular account.
	Profile string

	Deadline time.Duration
	Poll     time.Duration
	// Keep leaves the scratch directory on the host, for debugging. Off by
	// default: the prompt quotes the corpus, and these are rented boxes.
	Keep  bool
	Logf  func(string, ...any)
	Sleep func(ctx context.Context, d time.Duration) error
}

// Answer is what came back.
type Answer struct {
	Text string
	// Conversation is the chat URL, empty when the tool did not report one.
	Conversation string
	Model        string
	Elapsed      time.Duration
}

func (a Ask) logf(format string, args ...any) {
	if a.Logf != nil {
		a.Logf(format, args...)
	}
}

func (a Ask) poll() time.Duration {
	if a.Poll > 0 {
		return a.Poll
	}
	return DefaultPoll
}

// deadline bounds the whole call. A question is not a page: the translation
// prompts run to a section of a book and the answer to one is minutes of
// generation, so the default is three times a page rather than one and a half.
func (a Ask) deadline() time.Duration {
	if a.Deadline > 0 {
		return a.Deadline
	}
	return a.Host.pageTimeout() * 3
}

func (a Ask) sleep(ctx context.Context, d time.Duration) error {
	if a.Sleep != nil {
		return a.Sleep(ctx, d)
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

// Validate checks a question before it opens an ssh connection.
func (a Ask) Validate() error {
	if err := a.Host.Validate(); err != nil {
		return err
	}
	if err := ValidBatchID(a.ID); err != nil {
		return err
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return fmt.Errorf("question %s has nothing to ask", a.ID)
	}
	if a.Shell == nil || a.Copy == nil {
		return fmt.Errorf("question %s has no transport", a.ID)
	}
	return nil
}

// Do asks and returns the answer.
func (a Ask) Do(ctx context.Context) (Answer, error) {
	if err := a.Validate(); err != nil {
		return Answer{}, err
	}
	// Not under ask/, which is where a follow up puts its scratch. Two kinds of
	// call under one directory name is one directory to read wrong at three in
	// the morning with a box misbehaving.
	dir := path(a.Host.root(), "chat", a.ID)
	promptFile := path(dir, "ask.md")
	answerFile := path(dir, "answer.md")
	metaFile := path(dir, "meta.json")
	logFile := path(dir, "log")

	if err := prepare(ctx, a.Shell, a.Host, "", dir); err != nil {
		return Answer{}, err
	}
	defer func() {
		if a.Keep {
			return
		}
		if _, err := a.Shell.Run(context.WithoutCancel(ctx), a.Host.Name, "rm -rf "+quote(dir)); err != nil {
			a.logf("could not remove %s from %s: %v", dir, a.Host.Name, err)
		}
	}()

	if err := a.push(ctx, dir); err != nil {
		return Answer{}, err
	}
	pid, err := a.start(ctx, promptFile, answerFile, metaFile, logFile)
	if err != nil {
		return Answer{}, err
	}
	a.logf("%s: asking %s, pid %d", a.Host.Name, a.ID, pid)

	started := time.Now()
	if err := a.wait(ctx, answerFile, pid, started); err != nil {
		a.stop(ctx, pid)
		if tail := a.tail(ctx, logFile); tail != "" {
			return Answer{}, fmt.Errorf("%w: %s", err, condense(tail))
		}
		return Answer{}, err
	}
	text, err := a.Shell.Run(ctx, a.Host.Name, "cat "+quote(answerFile))
	if err != nil {
		return Answer{}, fmt.Errorf("read the answer from %s: %w", a.Host.Name, err)
	}
	// The service answers its own failures in the same place it answers
	// questions, with nothing else to say it did. Read as an answer, that page is
	// a judge with no verdict in it, and the caller files the exercise as failed
	// for a reason that has nothing to do with the exercise.
	if why := ProviderFailure(text); why != "" {
		return Answer{}, fmt.Errorf("question %s on %s: the service answered with its own error page: %s",
			a.ID, a.Host.Name, why)
	}
	out := Answer{Text: text, Elapsed: time.Since(started)}
	// The metadata is a convenience and its absence is not a failure. An older
	// tool does not write it, and the answer is the answer either way.
	if raw, err := a.Shell.Run(ctx, a.Host.Name, "cat "+quote(metaFile)+" 2>/dev/null || true"); err == nil {
		var m struct {
			URL   string `json:"url"`
			Model string `json:"model"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(raw)), &m) == nil {
			out.Conversation, out.Model = m.URL, m.Model
		}
	}
	a.logf("%s: %s answered in %s", a.Host.Name, a.ID, out.Elapsed.Round(time.Second))
	return out, nil
}

func (a Ask) push(ctx context.Context, dir string) error {
	scratch, err := os.MkdirTemp("", "kvant-ask-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	local := filepath.Join(scratch, "ask.md")
	if err := os.WriteFile(local, []byte(a.Prompt), 0o600); err != nil {
		return err
	}
	if err := a.Copy.Push(ctx, a.Host.Name, []string{local}, dir); err != nil {
		return fmt.Errorf("push the question to %s: %w", a.Host.Name, err)
	}
	return nil
}

func (a Ask) start(ctx context.Context, promptFile, answerFile, metaFile, logFile string) (int, error) {
	profile := ""
	if a.Profile != "" {
		profile = " --profile " + quote(a.Profile)
	}
	command := fmt.Sprintf(
		"DISPLAY=%s setsid nohup %s ask --prompt-file %s --out %s --meta %s%s "+
			"--timeout %d --no-verbose >%s 2>&1 </dev/null & echo $!",
		quote(a.Host.display()), quote(a.Host.Tool),
		quote(promptFile), quote(answerFile), quote(metaFile), profile,
		int(a.deadline().Seconds()), quote(logFile))

	output, err := a.Shell.Run(ctx, a.Host.Name, command)
	if err != nil {
		return 0, fmt.Errorf("start the question on %s: %w", a.Host.Name, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lastLine(output)))
	if err != nil {
		return 0, fmt.Errorf("start the question on %s: no pid in %q", a.Host.Name, condense(output))
	}
	return pid, nil
}

// wait polls until the answer file has something in it or the tool stops.
//
// Liveness first and the file second, for the reason Follow.wait gives: the
// tool writes the answer and then exits, so asking the other way round can see
// an empty file and a dead process in one round while the answer is being
// written between the two checks.
func (a Ask) wait(ctx context.Context, answerFile string, pid int, started time.Time) error {
	command := fmt.Sprintf("kill -0 %d 2>/dev/null && echo running || echo gone; test -s %s && echo done || echo waiting",
		pid, quote(answerFile))
	deadline := a.deadline()
	for {
		if err := a.sleep(ctx, a.poll()); err != nil {
			return err
		}
		output, err := a.Shell.Run(ctx, a.Host.Name, command)
		if err != nil {
			a.logf("%s: poll failed, will try again: %v", a.Host.Name, err)
			if elapsed := time.Since(started); elapsed > deadline {
				return fmt.Errorf("question %s on %s: gave up after %s: %w", a.ID, a.Host.Name, elapsed.Round(time.Second), err)
			}
			continue
		}
		if strings.Contains(output, "done") {
			return nil
		}
		if strings.Contains(output, "gone") {
			return fmt.Errorf("question %s on %s stopped without writing an answer", a.ID, a.Host.Name)
		}
		if elapsed := time.Since(started); elapsed > deadline {
			return fmt.Errorf("question %s on %s: no answer after %s, giving up", a.ID, a.Host.Name, elapsed.Round(time.Second))
		}
	}
}

func (a Ask) stop(ctx context.Context, pid int) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), StopGrace+30*time.Second)
	defer cancel()
	command := fmt.Sprintf("kill -TERM -%d 2>/dev/null; sleep %d; kill -KILL -%d 2>/dev/null; true",
		pid, int(StopGrace.Seconds()), pid)
	if _, err := a.Shell.Run(ctx, a.Host.Name, command); err != nil {
		a.logf("%s: could not stop the question at pid %d, it may still be running: %v", a.Host.Name, pid, err)
		return
	}
	a.logf("%s: stopped the question at pid %d and everything under it", a.Host.Name, pid)
}

func (a Ask) tail(ctx context.Context, logFile string) string {
	output, err := a.Shell.Run(context.WithoutCancel(ctx), a.Host.Name, "tail -n 25 "+quote(logFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}
