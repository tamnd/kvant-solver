package ocr

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A follow up is the same transport as a batch, doing something much smaller.
//
// Reading a page costs a full call, which the pilot measured at between 63 and
// 151 seconds, and it costs it whether the page was wholly unreadable or wrong
// in one character. A page that came back with a single unclosed formula does
// not need reading again, it needs one question asked about it, and the model
// that read it still has the image in the conversation. So the fix goes back
// into the thread it came out of: chatgpt-tool follow-up navigates to the saved
// chat, adds a turn, and returns the reply.
//
// The reason it can work at all is a detail of the tool. ocr_image opens the
// account's ordinary chat, so its conversations persist and have URLs. search
// opens a temporary chat, which does not. Only pages read through the first
// path can be repaired through this one, and a page whose thread was never
// recorded goes back to the image, which is slow and always available.

// Follow is one question put to a conversation that already exists on a host.
type Follow struct {
	Host  Host
	Shell Shell
	Copy  Copier

	// Conversation is the chat URL, as chatgpt-tool wrote it into the front
	// matter of the answer.
	Conversation string
	// Profile is the Chrome profile directory on that host. It is not optional
	// in practice: a conversation belongs to one account, and a profile signed
	// in as somebody else gets a page that does not exist.
	Profile string
	Prompt  string
	// ID names the scratch directory on the host.
	ID string

	// Deadline bounds the whole call. Zero derives one from the host's per page
	// timeout, since a follow up is one page's worth of work.
	Deadline time.Duration
	Poll     time.Duration
	// Keep leaves the scratch directory on the host, for debugging. Off by
	// default: the prompt holds the whole transcription of a copyrighted page,
	// and these are rented boxes.
	Keep  bool
	Logf  func(string, ...any)
	Sleep func(ctx context.Context, d time.Duration) error
}

func (f Follow) logf(format string, args ...any) {
	if f.Logf != nil {
		f.Logf(format, args...)
	}
}

func (f Follow) poll() time.Duration {
	if f.Poll > 0 {
		return f.Poll
	}
	return DefaultPoll
}

func (f Follow) deadline() time.Duration {
	if f.Deadline > 0 {
		return f.Deadline
	}
	return f.Host.pageTimeout() * 3 / 2
}

func (f Follow) sleep(ctx context.Context, d time.Duration) error {
	if f.Sleep != nil {
		return f.Sleep(ctx, d)
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

// Validate checks a follow up before it opens an ssh connection.
func (f Follow) Validate() error {
	if err := f.Host.Validate(); err != nil {
		return err
	}
	if err := ValidBatchID(f.ID); err != nil {
		return err
	}
	if err := ValidConversation(f.Conversation); err != nil {
		return err
	}
	if strings.TrimSpace(f.Profile) == "" {
		return fmt.Errorf("follow up %s has no Chrome profile, and the conversation belongs to one account", f.ID)
	}
	if strings.TrimSpace(f.Prompt) == "" {
		return fmt.Errorf("follow up %s has nothing to ask", f.ID)
	}
	if f.Shell == nil || f.Copy == nil {
		return fmt.Errorf("follow up %s has no transport", f.ID)
	}
	return nil
}

// ConversationPrefix is what a saved ChatGPT conversation URL starts with.
const ConversationPrefix = "https://chatgpt.com/c/"

// ValidConversation says whether a string is a conversation this will open.
//
// The URL comes out of a file written by a tool on a rented box and goes into a
// command line on that box, so it is checked rather than quoted and hoped for.
// Restricting it to the one prefix also catches the case that matters more in
// practice: a page read before the tool recorded conversation URLs has an empty
// or truncated one, and asking a browser to navigate to that opens whatever
// happens to be there and reads the answer off somebody else's page.
func ValidConversation(url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("no conversation url, so this page cannot be repaired in its own thread")
	}
	rest, ok := strings.CutPrefix(url, ConversationPrefix)
	if !ok {
		return fmt.Errorf("conversation %q does not start with %s", condense(url), ConversationPrefix)
	}
	if rest == "" {
		return fmt.Errorf("conversation %q has no id after the prefix", condense(url))
	}
	for _, r := range rest {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("conversation %q has a %q in it", condense(url), r)
		}
	}
	return nil
}

// Ask sends the prompt into the conversation and returns the reply.
//
// The shape is the batch protocol with one page in it. The call runs detached
// under setsid and is polled, for the same two reasons: it takes minutes, so a
// dropped tunnel must not kill it, and it starts a browser, so when the driver
// gives up the whole process group has to go with it rather than be left
// holding a Chrome on a box that is already loaded.
func (f Follow) Ask(ctx context.Context) (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}
	root := f.Host.root()
	dir := path(root, "ask", f.ID)
	promptFile := path(dir, "ask.md")
	answerFile := path(dir, "answer.md")
	logFile := path(dir, "log")

	if err := prepare(ctx, f.Shell, f.Host, "", dir); err != nil {
		return "", err
	}
	// The prompt carries the whole transcription of a page of the magazine, so it
	// does not stay on the box after the answer has been read.
	defer func() {
		if f.Keep {
			return
		}
		if _, err := f.Shell.Run(context.WithoutCancel(ctx), f.Host.Name, "rm -rf "+quote(dir)); err != nil {
			f.logf("could not remove %s from %s: %v", dir, f.Host.Name, err)
		}
	}()

	if err := f.push(ctx, dir); err != nil {
		return "", err
	}
	pid, err := f.start(ctx, promptFile, answerFile, logFile)
	if err != nil {
		return "", err
	}
	f.logf("%s: asking %s in %s, pid %d", f.Host.Name, f.ID, f.Conversation, pid)

	started := time.Now()
	if err := f.wait(ctx, answerFile, pid, started); err != nil {
		f.stop(ctx, pid)
		if tail := f.tail(ctx, logFile); tail != "" {
			return "", fmt.Errorf("%w: %s", err, condense(tail))
		}
		return "", err
	}
	answer, err := f.Shell.Run(ctx, f.Host.Name, "cat "+quote(answerFile))
	if err != nil {
		return "", fmt.Errorf("read the answer from %s: %w", f.Host.Name, err)
	}
	f.logf("%s: %s answered in %s", f.Host.Name, f.ID, time.Since(started).Round(time.Second))
	return answer, nil
}

// push writes the prompt to a local temp file and copies it over.
//
// By file rather than on the command line. The prompt holds LaTeX, so it is
// mostly dollars and backslashes, and every one of those is a thing a remote
// shell would like to expand.
func (f Follow) push(ctx context.Context, dir string) error {
	scratch, err := os.MkdirTemp("", "kvant-ask-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	local := filepath.Join(scratch, "ask.md")
	if err := os.WriteFile(local, []byte(f.Prompt), 0o600); err != nil {
		return err
	}
	if err := f.Copy.Push(ctx, f.Host.Name, []string{local}, dir); err != nil {
		return fmt.Errorf("push the question to %s: %w", f.Host.Name, err)
	}
	return nil
}

func (f Follow) start(ctx context.Context, promptFile, answerFile, logFile string) (int, error) {
	command := fmt.Sprintf(
		"DISPLAY=%s setsid nohup %s follow-up %s --prompt-file %s --profile %s --out %s "+
			"--timeout %d --no-verbose >%s 2>&1 </dev/null & echo $!",
		quote(f.Host.display()), quote(f.Host.Tool), quote(f.Conversation),
		quote(promptFile), quote(f.Profile), quote(answerFile),
		int(f.Host.pageTimeout().Seconds()), quote(logFile))

	output, err := f.Shell.Run(ctx, f.Host.Name, command)
	if err != nil {
		return 0, fmt.Errorf("start the follow up on %s: %w", f.Host.Name, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(lastLine(output)))
	if err != nil {
		return 0, fmt.Errorf("start the follow up on %s: no pid in %q", f.Host.Name, condense(output))
	}
	return pid, nil
}

// wait polls until the answer file has something in it or the tool stops.
//
// Liveness is asked first and the file second, and the order is the whole of
// the correctness here. The tool writes the answer and then exits, so a poll
// that looked at the file first would be able to see an empty file and a dead
// process in the same round while the answer was being written between the two
// checks, and report a failure over a call that succeeded.
func (f Follow) wait(ctx context.Context, answerFile string, pid int, started time.Time) error {
	command := fmt.Sprintf("kill -0 %d 2>/dev/null && echo running || echo gone; test -s %s && echo done || echo waiting",
		pid, quote(answerFile))
	deadline := f.deadline()
	for {
		if err := f.sleep(ctx, f.poll()); err != nil {
			return err
		}
		output, err := f.Shell.Run(ctx, f.Host.Name, command)
		if err != nil {
			f.logf("%s: poll failed, will try again: %v", f.Host.Name, err)
			if elapsed := time.Since(started); elapsed > deadline {
				return fmt.Errorf("follow up %s on %s: gave up after %s: %w", f.ID, f.Host.Name, elapsed.Round(time.Second), err)
			}
			continue
		}
		done := strings.Contains(output, "done")
		if done {
			return nil
		}
		if strings.Contains(output, "gone") {
			return fmt.Errorf("follow up %s on %s stopped without writing an answer", f.ID, f.Host.Name)
		}
		if elapsed := time.Since(started); elapsed > deadline {
			return fmt.Errorf("follow up %s on %s: no answer after %s, giving up", f.ID, f.Host.Name, elapsed.Round(time.Second))
		}
	}
}

func (f Follow) stop(ctx context.Context, pid int) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), StopGrace+30*time.Second)
	defer cancel()
	command := fmt.Sprintf("kill -TERM -%d 2>/dev/null; sleep %d; kill -KILL -%d 2>/dev/null; true",
		pid, int(StopGrace.Seconds()), pid)
	if _, err := f.Shell.Run(ctx, f.Host.Name, command); err != nil {
		f.logf("%s: could not stop the follow up at pid %d, it may still be running: %v", f.Host.Name, pid, err)
		return
	}
	f.logf("%s: stopped the follow up at pid %d and everything under it", f.Host.Name, pid)
}

func (f Follow) tail(ctx context.Context, logFile string) string {
	output, err := f.Shell.Run(context.WithoutCancel(ctx), f.Host.Name, "tail -n 25 "+quote(logFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}
