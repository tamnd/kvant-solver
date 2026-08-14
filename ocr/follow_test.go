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

// desk is a box with a browser on it and one conversation open. It answers the
// three commands a follow up sends and keeps the transcript, which is the only
// way to see that the question went to the right chat under the right profile.
type desk struct {
	commands []string
	asked    string

	pid int
	// alive counts polls before the tool exits, and answer is what it wrote by
	// then. An empty answer with a dead process is a call that produced nothing.
	alive  int
	answer string
	// together makes the tool exit in the same round as it writes the answer,
	// which is the ordinary case and the one a careless poll gets wrong.
	together bool

	noDisplay bool
	failStart error
	killed    string
	removed   bool
	log       string
}

func (d *desk) Run(ctx context.Context, host, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	d.commands = append(d.commands, command)
	switch {
	case strings.HasPrefix(command, "mkdir -p"):
		if d.noDisplay {
			return "display-down\n", nil
		}
		return "display-up\n", nil
	case strings.Contains(command, "follow-up"):
		if d.failStart != nil {
			return "", d.failStart
		}
		return fmt.Sprintf("%d\n", d.pid), nil
	case strings.Contains(command, "kill -0"):
		running := d.alive > 0
		if d.alive > 0 {
			d.alive--
		}
		written := !running || (d.together && d.alive == 0)
		state := "gone"
		if running && !written {
			state = "running"
		}
		file := "waiting"
		if written && d.answer != "" {
			file = "done"
		}
		return state + "\n" + file + "\n", nil
	case strings.HasPrefix(command, "cat"):
		return d.answer, nil
	case strings.HasPrefix(command, "kill -TERM"):
		d.killed = command
		return "", nil
	case strings.HasPrefix(command, "tail"):
		return d.log, nil
	case strings.HasPrefix(command, "rm -rf"):
		d.removed = true
		return "", nil
	}
	return "", fmt.Errorf("desk was sent a command it does not know: %s", command)
}

func (d *desk) Push(ctx context.Context, host string, local []string, remote string) error {
	for _, file := range local {
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		d.asked = string(raw)
	}
	return nil
}

func (d *desk) Pull(ctx context.Context, host, remote, local string) error { return nil }

func ask(machine *desk) Follow {
	return Follow{
		Host:  Host{Name: "server3", Tool: "/root/bin/chatgpt-tool", Lanes: 4},
		Shell: machine, Copy: machine,
		Conversation: "https://chatgpt.com/c/68a1f2c0-dead-beef",
		Profile:      "/root/.config/chatgpt-profile-3",
		Prompt:       "Close the formula on line 9 and change nothing else.",
		ID:           "kvant-1975-1-0050-ask-abc123",
		Sleep:        noSleep,
	}
}

func TestAFollowUpGoesToTheRightChatUnderTheRightProfile(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, answer: "the page, with the formula closed\n"}
	answer, err := ask(machine).Ask(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if answer != machine.answer {
		t.Errorf("got %q back, want the answer the box wrote", answer)
	}
	start := find(machine.commands, "follow-up")
	for _, want := range []string{
		"'https://chatgpt.com/c/68a1f2c0-dead-beef'",
		"--profile '/root/.config/chatgpt-profile-3'",
		"--prompt-file", "--out", "DISPLAY=':99'", "setsid",
	} {
		if !strings.Contains(start, want) {
			t.Errorf("the command does not contain %q:\n%s", want, start)
		}
	}
	// The question goes over as a file. It is mostly dollars and backslashes,
	// and a remote shell would like to expand every one of them.
	if machine.asked != "Close the formula on line 9 and change nothing else." {
		t.Errorf("the question that arrived is %q", machine.asked)
	}
	if !machine.removed {
		t.Error("the question was left on the box, and it quotes a page of the book")
	}
}

// The tool writes the answer and then exits, so a poll that asked about the
// file first could see an empty file and a dead process in one round and report
// a failure over a call that worked.
func TestAnAnswerWrittenAsTheToolExitsIsStillAnAnswer(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, answer: "the page\n", together: true}
	answer, err := ask(machine).Ask(context.Background())
	if err != nil {
		t.Fatalf("an answer that arrived as the tool exited was missed: %v", err)
	}
	if answer != "the page\n" {
		t.Errorf("got %q", answer)
	}
}

func TestAFollowUpThatDiesWithNothingIsKilledAndReported(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, log: "playwright: target closed"}
	if _, err := ask(machine).Ask(context.Background()); err == nil {
		t.Fatal("a call that wrote no answer was reported as a success")
	} else if !strings.Contains(err.Error(), "target closed") {
		t.Errorf("the error does not carry what the box said: %v", err)
	}
	// Same reason as a batch. This started a browser, and walking away from it
	// leaves a Chrome on a box that is already loaded.
	if !strings.Contains(machine.killed, "kill -TERM -4242") {
		t.Errorf("the process group was not killed, got %q", machine.killed)
	}
}

func TestABoxWithNoDisplayIsNotAsked(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, answer: "the page", noDisplay: true}
	if _, err := ask(machine).Ask(context.Background()); err == nil {
		t.Fatal("a follow up was sent to a box with no Xvfb")
	}
	if find(machine.commands, "follow-up") != "" {
		t.Error("the tool was started anyway")
	}
}

// The URL comes out of a file written on a rented box and goes into a command
// line on that box.
func TestAConversationUrlIsCheckedBeforeItIsUsed(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"https://chatgpt.com/",
		"http://chatgpt.com/c/abc",
		"https://example.com/c/abc",
		"https://chatgpt.com/c/",
		"https://chatgpt.com/c/abc'; rm -rf /; echo '",
		"https://chatgpt.com/c/abc?x=1",
		"https://chatgpt.com/c/abc/../../settings",
	}
	for _, url := range bad {
		if err := ValidConversation(url); err == nil {
			t.Errorf("%q was accepted as a conversation", url)
		}
	}
	if err := ValidConversation("https://chatgpt.com/c/68a1f2c0-dead-beef"); err != nil {
		t.Errorf("a real conversation url was refused: %v", err)
	}
}

// A conversation belongs to one account, so a follow up without the profile
// that owns it signs in as somebody else and reads a page that is not there.
func TestAFollowUpWithoutAProfileIsRefused(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, answer: "the page"}
	value := ask(machine)
	value.Profile = ""
	if _, err := value.Ask(context.Background()); err == nil {
		t.Fatal("a follow up went out with no profile")
	}
	if len(machine.commands) > 0 {
		t.Errorf("it opened a connection first: %v", machine.commands)
	}
}

func TestKeepLeavesTheQuestionOnTheBox(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, answer: "the page"}
	value := ask(machine)
	value.Keep = true
	if _, err := value.Ask(context.Background()); err != nil {
		t.Fatal(err)
	}
	if machine.removed {
		t.Error("the scratch directory was removed under -keep, which is what -keep is for")
	}
}

// A tool that sits there with a browser open and never answers is the case the
// deadline is for. The batch protocol learned this the expensive way: work
// nobody is waiting for any more goes on eating the box.
func TestAFollowUpThatNeverAnswersGivesUp(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1_000_000}
	value := ask(machine)
	value.Deadline = time.Nanosecond
	if _, err := value.Ask(context.Background()); err == nil {
		t.Fatal("a call that never answered ran for ever")
	} else if !strings.Contains(err.Error(), "giving up") {
		t.Errorf("it stopped for some other reason: %v", err)
	}
	if machine.killed == "" {
		t.Error("the call that ran out of time was left running on the box")
	}
}

func find(commands []string, part string) string {
	for _, command := range commands {
		if strings.Contains(command, part) {
			return command
		}
	}
	return ""
}

func TestTheScratchDirectoryIsUnderTheHostRoot(t *testing.T) {
	machine := &desk{pid: 4242, alive: 1, answer: "the page"}
	if _, err := ask(machine).Ask(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(RemoteRoot, "ask", "kvant-1975-1-0050-ask-abc123")
	if !strings.Contains(machine.commands[0], want) {
		t.Errorf("the scratch directory is not %s:\n%s", want, machine.commands[0])
	}
}
