package ocr_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

// rentedBox is one of the boxes: it takes files, runs one command per page and
// answers with Markdown.
type rentedBox struct {
	mu sync.Mutex

	// answer is what the tool prints for a page. Empty means the fixture below.
	answer string
	// quota is the pool having nothing left, which the tool reports on a clean
	// exit with the refusal in its output.
	quota bool
	// quotaOnExit puts the same refusal on a failed exit instead, which is the
	// other way it arrives.
	quotaOnExit bool
	// readErr is a page that would not read for a reason that is the page's.
	readErr error
	// silent is the tool exiting without writing a page, which the command turns
	// into the marker and the tail of the log.
	silent string

	commands []string
	pushed   []string
	prompt   string
	removed  []string
}

const readPage = "⟦folio 25⟧\n\nВ отличие от задач 1—3, инварианты в них интересны сами по себе.\n"

// cappedLog is what server3 actually printed the first time this lane met the
// upload cap, wrapping and all. It is kept verbatim rather than tidied because
// the wrapping is the point: the tool prints through a console renderer, so the
// phrase that has to be recognised arrives broken across a line at whatever
// column the terminal happened to be.
const cappedLog = `──────────── OCR Batch  kvant-ocr/one/1/in  →  kvant-ocr/one/1/out ─────────────
  1 image(s)  ·  1 worker(s)  ·  6.0s between starts

  account pool: 11 verified account(s) available

  upload cap  0059.jpg  the rest of the batch is given up rather than retried,
because no account here can upload: all 11 verified slot(s) are banned, the
earliest lifts at 06:59:18, which is 6 minutes away
  fail  0059.jpg  0.0s  upload cap: all 11 verified slot(s) are banned, the
earliest lifts at 06:59:18, which is 6 minutes a
  OCR  done=0  fail=1 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 1/1 0:00:00 0:00:00

──────────────── Batch complete  0 succeeded  1 failed  (of 1) ─────────────────
`

// failedLog is the same shape with a failure that is the page's own, so the
// banner is there to be stripped but the quota is not there to be found.
const failedLog = `──────────── OCR Batch  kvant-ocr/one/1/in  →  kvant-ocr/one/1/out ─────────────
  1 image(s)  ·  1 worker(s)  ·  6.0s between starts

  account pool: 11 verified account(s) available

  fail  0059.jpg  41.2s  the composer never accepted the image
  OCR  done=0  fail=1 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ 1/1 0:00:41 0:00:00

──────────────── Batch complete  0 succeeded  1 failed  (of 1) ─────────────────
`

func (b *rentedBox) Run(_ context.Context, _, command string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commands = append(b.commands, command)
	switch {
	case strings.HasPrefix(command, "mkdir -p"):
		return "", nil
	case strings.HasPrefix(command, "rm -rf"):
		b.removed = append(b.removed, command)
		return "", nil
	case strings.Contains(command, " ocr-batch "):
		if b.quotaOnExit {
			return "", errors.New("chatgpt-profile-16 has no uploads left, the composer says 'wait 1 hour to upload again'")
		}
		// The tool exits cleanly with the pool empty and writes no page, so what
		// comes back is the marker the command prints and the tail of the log.
		if b.quota {
			return ocr.NoOutput + cappedLog, nil
		}
		if b.silent != "" {
			return ocr.NoOutput + b.silent, nil
		}
		if b.readErr != nil {
			return "", b.readErr
		}
		if b.answer != "" {
			return b.answer, nil
		}
		return readPage, nil
	}
	return "", fmt.Errorf("the box was sent a command it does not know: %s", command)
}

func (b *rentedBox) Push(_ context.Context, _ string, local []string, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, file := range local {
		name := filepath.Base(file)
		b.pushed = append(b.pushed, name)
		if strings.HasPrefix(name, "prompt-") {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			b.prompt = string(raw)
		}
	}
	return nil
}

func (b *rentedBox) Pull(context.Context, string, string, string) error { return nil }

func (b *rentedBox) prompts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, name := range b.pushed {
		if strings.HasPrefix(name, "prompt-") {
			n++
		}
	}
	return n
}

func remoteFor(box *rentedBox) *ocr.Remote {
	return &ocr.Remote{
		Host:   ocr.Host{Name: "server3", Tool: "/root/chatgpt-tool/.venv/bin/chatgpt-tool"},
		Shell:  box,
		Copy:   box,
		Prompt: "Read this page.",
		Model:  "gpt-5",
	}
}

func imageOn(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "0027.jpg")
	if err := os.WriteFile(path, []byte("not really a jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The whole of the contract: an image goes to the box, the tool is asked to
// read it, and the Markdown comes back.
func TestAPageGoesOutAndTheMarkdownComesBack(t *testing.T) {
	box := &rentedBox{}
	got, err := remoteFor(box).Read(context.Background(), imageOn(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != readPage {
		t.Errorf("read %q, want the page the box answered with", got)
	}
	if !slices.Contains(box.pushed, "0027.jpg") {
		t.Errorf("pushed %v, want the page image among them", box.pushed)
	}

	// A browser needs a display even when nobody is looking at it, and an ssh
	// command carries none. Without this every page fails at launch in about a
	// second and the tool reads the pile of instant failures as an IP level
	// block, which bans the accounts behind them for eight hours.
	var ran string
	for _, command := range box.commands {
		if strings.Contains(command, " ocr-batch ") {
			ran = command
		}
	}
	if !strings.Contains(ran, "DISPLAY=") {
		t.Errorf("the read ran as %q, want it to carry a display", ran)
	}
}

// The page is read out of a file the tool wrote and not off its standard
// output.
//
// The single image subcommand prints through a console renderer: it frames the
// text in box drawing characters and rewraps it to the terminal width, so the
// page arrives padded, boxed, and broken at eighty columns with nothing left to
// tell one of the magazine's line breaks from one the renderer put in.
// --no-verbose does not turn that off. The first page this lane ever read came
// back that way and went into the corpus that way, so this is a regression test
// and not a preference.
func TestThePageIsReadOutOfAFileAndNotOffTheConsole(t *testing.T) {
	box := &rentedBox{}
	if _, err := remoteFor(box).Read(context.Background(), imageOn(t)); err != nil {
		t.Fatal(err)
	}

	var ran string
	for _, command := range box.commands {
		if strings.Contains(command, " ocr-batch ") {
			ran = command
		}
	}
	if strings.Contains(ran, "--no-verbose") {
		t.Errorf("the read ran as %q, want it not to rely on quietening the console", ran)
	}
	if !strings.Contains(ran, "0027.md") {
		t.Errorf("the read ran as %q, want it to come back with the file the tool wrote", ran)
	}
}

// When the tool writes no page there is nothing to hand the rules, and the only
// account of why is its log. Answering with an empty page instead would have the
// run record a blank sheet as read.
func TestAPageTheToolNeverWroteIsAFailureWithTheLogInIt(t *testing.T) {
	box := &rentedBox{silent: "chromedriver could not start on display :99\n"}
	_, err := remoteFor(box).Read(context.Background(), imageOn(t))
	if err == nil {
		t.Fatal("the read succeeded, want the missing page reported")
	}
	if !strings.Contains(err.Error(), "chromedriver") {
		t.Errorf("the read failed with %v, want the log quoted in it", err)
	}
	if errors.Is(err, ocr.ErrNoQuota) {
		t.Errorf("the read returned %v, want it not to blame the quota", err)
	}
}

// The failure has to say why, and the tool's log says why in the middle of
// itself. A report cut to the first line of the log is a report of the tool's
// own heading, which is what a run of six failures came back as and the reason
// none of them could be read.
func TestTheFailureQuotesTheReasonAndNotTheBanner(t *testing.T) {
	box := &rentedBox{silent: failedLog}
	_, err := remoteFor(box).Read(context.Background(), imageOn(t))
	if err == nil {
		t.Fatal("the read succeeded, want the missing page reported")
	}
	if strings.Contains(err.Error(), "OCR Batch") || strings.Contains(err.Error(), "image(s)") {
		t.Errorf("the read failed with %v, want the reason rather than the heading", err)
	}
	if !strings.Contains(err.Error(), "composer never accepted the image") {
		t.Errorf("the read failed with %v, want it to say what the tool said", err)
	}
}

// A pool with nothing left has not looked at the page, so the page must not be
// told it failed. This is the distinction the whole design rests on: the
// difference between this page is bad and this lane is not reading.
func TestAPoolWithNoUploadsLeftIsNotThePagesFault(t *testing.T) {
	for _, test := range []struct {
		name string
		box  *rentedBox
	}{
		{"on a clean exit", &rentedBox{quota: true}},
		{"on a failed exit", &rentedBox{quotaOnExit: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := remoteFor(test.box).Read(context.Background(), imageOn(t))
			if !errors.Is(err, ocr.ErrNoQuota) {
				t.Fatalf("the read returned %v, want it to name the quota", err)
			}
		})
	}
}

// A page that would not read for its own reasons is still a page that failed,
// and dressing that up as a quota problem would hide it forever.
func TestAPageThatWillNotReadIsStillThePagesFailure(t *testing.T) {
	box := &rentedBox{readErr: errors.New("the browser gave up on the upload")}
	_, err := remoteFor(box).Read(context.Background(), imageOn(t))
	if err == nil {
		t.Fatal("the read succeeded, want the failure reported")
	}
	if errors.Is(err, ocr.ErrNoQuota) {
		t.Errorf("the read returned %v, want it not to blame the quota", err)
	}
}

// These are rented boxes. A scan that stayed behind because a read failed is
// how a magazine accumulates on somebody else's disk.
func TestTheImageComesOffTheBoxWhateverHappened(t *testing.T) {
	for _, test := range []struct {
		name string
		box  *rentedBox
	}{
		{"after a page that read", &rentedBox{}},
		{"after a page that did not", &rentedBox{readErr: errors.New("no")}},
		{"after the quota ran out", &rentedBox{quota: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _ = remoteFor(test.box).Read(context.Background(), imageOn(t))
			if len(test.box.removed) == 0 {
				t.Errorf("nothing was removed from the box, want the page image taken off it")
			}
		})
	}
}

// The prompt is what the answers are a function of, so it is named by its hash
// and pushed once. One rsync per page for a file that does not change is one
// round trip per page to these boxes for nothing.
func TestThePromptIsPushedOnceHoweverManyPagesAreRead(t *testing.T) {
	box := &rentedBox{}
	remote := remoteFor(box)
	dir := t.TempDir()
	for i := range 5 {
		path := filepath.Join(dir, fmt.Sprintf("%04d.jpg", i))
		if err := os.WriteFile(path, []byte("not really a jpeg"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := remote.Read(context.Background(), path); err != nil {
			t.Fatal(err)
		}
	}
	if got := box.prompts(); got != 1 {
		t.Errorf("the prompt went out %d times, want once", got)
	}
	if box.prompt != "Read this page." {
		t.Errorf("the box was given %q, want the prompt it was configured with", box.prompt)
	}
}

// The tool stamps its own provenance on what it writes: which account read the
// page, which conversation it was, how long it took. That is the tool's history
// and not the magazine's, and the corpus writes its own front matter from the
// queue, so a second block arriving in the same file would be read as text and
// land in the middle of an article.
func TestTheToolsOwnFrontMatterDoesNotReachTheCorpus(t *testing.T) {
	for _, test := range []struct {
		name string
		in   string
		want string
	}{
		{
			"a block the tool wrote",
			"---\nsource: /root/kvant-ocr/in/probe1/0027.jpg\nmodel: gpt-5\n---\n\n⟦folio 25⟧\n",
			"⟦folio 25⟧\n",
		},
		{
			"a page with none",
			"⟦folio 25⟧\n\nПервая строка.\n",
			"⟦folio 25⟧\n\nПервая строка.\n",
		},
		{
			// A page that opens on a rule is a page, and a reader that took the
			// rule for a fence would swallow everything up to the next one.
			"a page that opens on a horizontal rule",
			"--- a heading ---\n\nПервая строка.\n",
			"--- a heading ---\n\nПервая строка.\n",
		},
		{
			// An opening fence with no closing one is not front matter, and
			// guessing that it is would throw a whole page away.
			"a fence that never closes",
			"---\nsource: somewhere\n\n⟦folio 25⟧\n",
			"---\nsource: somewhere\n\n⟦folio 25⟧\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := ocr.StripFrontMatter(test.in); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

// The quota is per account and the accounts are per host, so an empty host is
// not an empty fleet.
func TestThePoolMovesOnWhenOneHostIsEmpty(t *testing.T) {
	empty, full := &rentedBox{quota: true}, &rentedBox{}
	pool := &ocr.Pool{Lanes: []ocr.Engine{remoteFor(empty), remoteFor(full)}}

	got, err := pool.Read(context.Background(), imageOn(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != readPage {
		t.Errorf("read %q, want the page the second host answered with", got)
	}
}

// A page that will not read is unlikely to read anywhere else, and trying every
// host in turn would spend the whole fleet on one bad scan.
func TestThePoolDoesNotShopABadPageAroundTheFleet(t *testing.T) {
	first, second := &rentedBox{readErr: errors.New("the browser gave up")}, &rentedBox{}
	pool := &ocr.Pool{Lanes: []ocr.Engine{remoteFor(first), remoteFor(second)}}

	if _, err := pool.Read(context.Background(), imageOn(t)); err == nil {
		t.Fatal("the read succeeded, want the first host's failure returned")
	}
	if len(second.commands) != 0 {
		t.Errorf("the second host was asked %v, want it left alone", second.commands)
	}
}

// When every host is empty the caller has to hear that the fleet is out, not a
// list of hosts, because the thing to do about it is wait.
func TestAPoolThatIsEntirelyEmptySaysSo(t *testing.T) {
	pool := &ocr.Pool{Lanes: []ocr.Engine{
		remoteFor(&rentedBox{quota: true}),
		remoteFor(&rentedBox{quota: true}),
	}}
	_, err := pool.Read(context.Background(), imageOn(t))
	if !errors.Is(err, ocr.ErrNoQuota) {
		t.Fatalf("the pool returned %v, want it to name the quota", err)
	}
}
