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

// counter is a box with a browser on it and no conversation open yet. It is the
// desk of follow_test.go with one difference that matters: it answers ask
// rather than follow-up, and it writes a meta file.
type counter struct {
	commands []string
	asked    string

	pid   int
	alive int

	answer string
	meta   string

	noDisplay bool
	killed    string
	removed   bool
	log       string
}

func (c *counter) Run(ctx context.Context, host, command string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.commands = append(c.commands, command)
	switch {
	case strings.HasPrefix(command, "mkdir -p"):
		if c.noDisplay {
			return "display-down\n", nil
		}
		return "display-up\n", nil
	case strings.Contains(command, " ask "):
		return fmt.Sprintf("%d\n", c.pid), nil
	case strings.Contains(command, "kill -0"):
		running := c.alive > 0
		if c.alive > 0 {
			c.alive--
		}
		state := "gone"
		if running {
			state = "running"
		}
		file := "waiting"
		if !running && c.answer != "" {
			file = "done"
		}
		return state + "\n" + file + "\n", nil
	case strings.Contains(command, "meta.json"):
		return c.meta, nil
	case strings.HasPrefix(command, "cat"):
		return c.answer, nil
	case strings.HasPrefix(command, "kill -TERM"):
		c.killed = command
		return "", nil
	case strings.HasPrefix(command, "tail"):
		return c.log, nil
	case strings.HasPrefix(command, "rm -rf"):
		c.removed = true
		return "", nil
	}
	return "", fmt.Errorf("counter was sent a command it does not know: %s", command)
}

func (c *counter) Push(ctx context.Context, host string, local []string, remote string) error {
	for _, file := range local {
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		c.asked = string(raw)
	}
	return nil
}

func (c *counter) Pull(ctx context.Context, host, remote, local string) error { return nil }

// question is the prompt this exists for: a glossary batch, which is LaTeX and
// therefore dollars and backslashes and braces from end to end.
const question = "Render each term in Vietnamese.\n\n1. semisimple ring\n2. $\\mathbf{Z}[x]$-module\n3. free module of rank $n$\n"

func newChat(machine *counter) Ask {
	return Ask{
		Host:  Host{Name: "server3", Tool: "/root/bin/chatgpt-tool", Lanes: 4},
		Shell: machine, Copy: machine,
		Prompt: question,
		ID:     "glossary-vi-0001",
		Sleep:  noSleep,
	}
}

func TestAQuestionGoesOverAsAFileAndArrivesByteForByte(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "1. vành nửa đơn\n"}
	answer, err := newChat(machine).Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if answer.Text != machine.answer {
		t.Errorf("got %q back, want the answer the box wrote", answer.Text)
	}
	// This is the whole reason the prompt is not on the command line. Every
	// dollar and every backslash here is something a remote shell would have
	// eaten, and what comes out the other end would still have looked like a
	// prompt.
	if machine.asked != question {
		t.Errorf("the question that arrived is not the one that was sent:\n%q", machine.asked)
	}
	start := find(machine.commands, " ask ")
	for _, want := range []string{"--prompt-file", "--out", "--meta", "DISPLAY=':99'", "setsid", "--no-verbose"} {
		if !strings.Contains(start, want) {
			t.Errorf("the command does not contain %q:\n%s", want, start)
		}
	}
	if !machine.removed {
		t.Error("the question was left on the box, and it quotes the corpus")
	}
}

// A fresh question belongs to no account, so the tool routes it. Naming a
// profile here would tie the whole glossary run to one inbox and its quota.
func TestNoProfileMeansTheToolPicksTheAccount(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer"}
	if _, err := newChat(machine).Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(find(machine.commands, " ask "), "--profile") {
		t.Error("a profile was named for a question that did not ask for one")
	}
}

func TestAProfileIsPassedWhenItIsAskedFor(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer"}
	value := newChat(machine)
	value.Profile = "/root/.config/chatgpt-profile-3"
	if _, err := value.Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(find(machine.commands, " ask "), "--profile '/root/.config/chatgpt-profile-3'") {
		t.Errorf("the profile did not reach the tool: %s", find(machine.commands, " ask "))
	}
}

// The conversation url is what makes a second question cheap: it is the handle
// a follow up needs, and without it every round trip starts from nothing.
func TestTheConversationUrlComesBack(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer",
		meta: `{"url": "https://chatgpt.com/c/68a1f2c0-dead-beef", "model": "gpt-5"}`}
	answer, err := newChat(machine).Do(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if answer.Conversation != "https://chatgpt.com/c/68a1f2c0-dead-beef" {
		t.Errorf("the conversation url is %q", answer.Conversation)
	}
	if answer.Model != "gpt-5" {
		t.Errorf("the model is %q", answer.Model)
	}
}

// An older tool on a box that has not been updated writes no meta file. The
// answer is the answer either way, and losing the url is not losing the work.
func TestAMissingMetaFileIsNotAFailure(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer"}
	got, err := newChat(machine).Do(context.Background())
	if err != nil {
		t.Fatalf("a call whose meta file was absent was reported as failed: %v", err)
	}
	if got.Text != "an answer" || got.Conversation != "" {
		t.Errorf("got %+v", got)
	}
}

func TestAQuestionThatDiesWithNothingIsKilledAndReported(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, log: "playwright: target closed"}
	if _, err := newChat(machine).Do(context.Background()); err == nil {
		t.Fatal("a call that wrote no answer was reported as a success")
	} else if !strings.Contains(err.Error(), "target closed") {
		t.Errorf("the error does not carry what the box said: %v", err)
	}
	if !strings.Contains(machine.killed, "kill -TERM -4242") {
		t.Error("the process group was left running on the box")
	}
}

// Same lesson as the batch protocol. A browser nobody is waiting for any more
// goes on eating a box that is already loaded.
func TestAQuestionThatNeverAnswersGivesUp(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1_000_000}
	value := newChat(machine)
	value.Deadline = time.Nanosecond
	if _, err := value.Do(context.Background()); err == nil {
		t.Fatal("a call that never answered ran for ever")
	} else if !strings.Contains(err.Error(), "giving up") {
		t.Errorf("it stopped for some other reason: %v", err)
	}
	if machine.killed == "" {
		t.Error("the call that ran out of time was left running on the box")
	}
}

func TestABoxWithNoDisplayIsNotSentAQuestion(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer", noDisplay: true}
	if _, err := newChat(machine).Do(context.Background()); err == nil {
		t.Fatal("a question was sent to a box with no Xvfb")
	}
	if find(machine.commands, " ask ") != "" {
		t.Error("the tool was started anyway")
	}
}

func TestAnEmptyQuestionIsRefusedBeforeAnyConnection(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer"}
	value := newChat(machine)
	value.Prompt = "   \n"
	if _, err := value.Do(context.Background()); err == nil {
		t.Fatal("a browser was started to ask nothing")
	}
	if len(machine.commands) > 0 {
		t.Errorf("it opened a connection first: %v", machine.commands)
	}
}

// A follow up puts its scratch under ask/. Two kinds of call under one name is
// one directory to read wrong when a box is misbehaving.
func TestTheScratchDirectoryIsItsOwn(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer"}
	if _, err := newChat(machine).Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(RemoteRoot, "chat", "glossary-vi-0001")
	if !strings.Contains(machine.commands[0], want) {
		t.Errorf("the scratch directory is not %s:\n%s", want, machine.commands[0])
	}
}

func TestKeepLeavesTheQuestionOnTheBoxAfterANewChat(t *testing.T) {
	machine := &counter{pid: 4242, alive: 1, answer: "an answer"}
	value := newChat(machine)
	value.Keep = true
	if _, err := value.Do(context.Background()); err != nil {
		t.Fatal(err)
	}
	if machine.removed {
		t.Error("the scratch directory was removed under -keep, which is what -keep is for")
	}
}
