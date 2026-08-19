package ocr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Remote reads one page on a rented box, through chatgpt-tool over ssh.
//
// The proxy on those boxes speaks chat completions and takes no images, so the
// only thing there that can read a page is the tool itself, and the transport
// is ssh and rsync rather than HTTP. One image goes out and one page of
// Markdown comes back, which is exactly the shape Engine asks for.
//
// Why a page at a time when batch.go can send a directory. The limit on these
// accounts is uploads per account per hour, not pages per minute: a batch of
// forty aimed at a pool with one upload left in it reads one page and reports
// thirty-nine failures in under two minutes, which is measured and not
// imagined. Sending one page makes the unit of work the same size as the unit
// of quota, so the run finds out it is out of uploads by being told once rather
// than by burning the whole work list to discover it.
type Remote struct {
	// Host is the box, its ssh name and the absolute path of chatgpt-tool on it.
	// Lanes is not read here: the pages in flight are the runner's workers.
	Host Host

	Shell Shell
	Copy  Copier

	// Prompt is the OCR instruction, pushed once per host and named by its hash.
	Prompt string

	// Model is what goes in the page front matter as extraction_model. The
	// program is a wrapper around whichever account the pool handed it, so the
	// corpus records the model asked for rather than the program run.
	Model string

	// Timeout bounds one page, and is passed to the tool as well as bounding the
	// ssh. Zero means DefaultPageTimeout.
	Timeout time.Duration

	// Logf reports the things that are worth saying but not worth failing over,
	// which here is the scratch directory that would not come off the box.
	Logf func(string, ...any)

	// pushed is the remote prompt path, resolved once however many workers call
	// Read. Pushing it per page would be one rsync per page for a file that does
	// not change.
	once    sync.Once
	pushed  string
	pushErr error

	// seq keeps two workers on one host from staging a page under the same name.
	// The base names collide constantly: every issue has an 0027.jpg.
	seq atomic.Uint64
}

// ErrNoQuota is the pool having no uploads left.
//
// It is a named error because it is not the page's fault and must not be
// written against the page. An account that is out of uploads refuses every
// image alike, so recording those refusals would mark good pages dead for a
// reason that will have evaporated by the time anybody reads the report, which
// is how a probe once put 64 pages in the failures list that had nothing wrong
// with them. The runner releases these jobs instead, and the straight failure
// counter stops the run so the next one starts when the quota has come back.
var ErrNoQuota = errors.New("the account pool has no uploads left")

// quotaSigns are what the tool says when the pool cannot read. It exits zero
// after saying any of them, so there is nothing but the text to go on.
//
// The list is copied from the tool rather than guessed at, because guessing at
// it is what went wrong the first time: the batch path says upload cap and the
// single image path says no uploads left, an earlier list had only the second,
// and so six batch refusals came back looking like six unreadable pages. Both
// vocabularies are here now, and anything added to the tool has to be added
// here too.
//
// Cloudflare belongs on this list even though it is not a quota. The question
// these signs answer is not was the account throttled, it is did anything look
// at the page, and a host held at the door has not.
var quotaSigns = []string{
	"upload cap",
	"no account here can upload",
	"are banned",
	"out of uploads",
	"no uploads left",
	"has no uploads",
	"to upload again",
	"cloudflare is holding this host",
}

// Name is the model recorded against the pages this reads.
func (r *Remote) Name() string {
	if strings.TrimSpace(r.Model) != "" {
		return r.Model
	}
	return "chatgpt"
}

func (r *Remote) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return DefaultPageTimeout
}

// Read sends one page to the box and returns the Markdown it answered with.
//
// The image is removed afterwards whatever happened. These are rented boxes and
// a scanned magazine should not accumulate on one because a read failed.
func (r *Remote) Read(ctx context.Context, image string) (string, error) {
	if err := r.Host.Validate(); err != nil {
		return "", err
	}
	if r.Shell == nil || r.Copy == nil {
		return "", fmt.Errorf("host %s has no transport", r.Host.Name)
	}
	prompt, err := r.prompt(ctx)
	if err != nil {
		return "", err
	}

	root := r.Host.root()
	base := filepath.Base(image)
	dir := path(root, "one", strconv.FormatUint(r.seq.Add(1), 10))
	in, out, log := path(dir, "in"), path(dir, "out"), path(dir, "log")
	if _, err := r.Shell.Run(ctx, r.Host.Name, "mkdir -p "+quote(in)+" "+quote(out)); err != nil {
		return "", fmt.Errorf("prepare %s: %w", r.Host.Name, err)
	}
	defer func() {
		if _, err := r.Shell.Run(context.WithoutCancel(ctx), r.Host.Name, "rm -rf "+quote(dir)); err != nil {
			r.logf("could not remove %s from %s: %v", dir, r.Host.Name, err)
		}
	}()
	if err := r.Copy.Push(ctx, r.Host.Name, []string{image}, in); err != nil {
		return "", fmt.Errorf("push %s to %s: %w", base, r.Host.Name, err)
	}

	answer, err := r.Shell.Run(ctx, r.Host.Name, r.command(in, out, log, prompt, base))
	if err != nil {
		// The pool says it is empty on a failed exit as readily as on a clean
		// one, so the text is read before the exit code is believed.
		if sign := quotaSign(err.Error()); sign != "" {
			return "", fmt.Errorf("%s: %w: %s", r.Host.Name, ErrNoQuota, sign)
		}
		return "", fmt.Errorf("read %s on %s: %w", base, r.Host.Name, err)
	}
	if reason, ok := strings.CutPrefix(answer, NoOutput); ok {
		if sign := quotaSign(reason); sign != "" {
			return "", fmt.Errorf("%s: %w: %s", r.Host.Name, ErrNoQuota, sign)
		}
		return "", fmt.Errorf("read %s on %s: %s", base, r.Host.Name, toolReason(reason))
	}

	text := StripFrontMatter(answer)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("read %s on %s: the tool wrote a page with nothing in it", base, r.Host.Name)
	}
	return text, nil
}

// NoOutput opens what the box prints when the tool wrote no page. The tail of
// the log follows it, which is the only account of why.
const NoOutput = "kvant: no page was written\n"

// command is the tool run over one image, and then the page it wrote.
//
// It is ocr-batch over a directory of one rather than the single image
// subcommand, which sounds like the long way round and is not. The single
// image form prints the page through a console renderer: it frames the text in
// box drawing characters and rewraps it to the terminal width, so the page
// arrives padded, boxed, and broken at eighty columns with no way left to tell
// one of the magazine's line breaks from one the renderer put in. ocr-batch
// writes a file instead, and a file is what was asked for. That is not a
// prediction, it is what the first page through this lane came back as.
//
// The tool's own chatter goes to a log rather than to standard output, so what
// comes back is the page and nothing else, and the log is quoted only when
// there is no page to quote instead.
//
// The display matters as much as the command. Chrome needs one even when nobody
// is looking, an ssh command carries none, and the tool reads a pile of instant
// launch failures as an IP level block and bans the accounts behind them for
// eight hours.
func (r *Remote) command(in, out, log, prompt, base string) string {
	ext := strings.TrimPrefix(filepath.Ext(base), ".")
	if ext == "" {
		ext = "jpg"
	}
	page := path(out, OutputName(base))
	return fmt.Sprintf(
		"DISPLAY=%s %s ocr-batch %s %s -j 1 --ext %s --timeout %d --prompt \"$(cat %s)\" >%s 2>&1; "+
			"if [ -s %s ]; then cat %s; else printf %s; tail -n 40 %s; fi",
		quote(r.Host.display()), quote(r.Host.Tool), quote(in), quote(out), quote(ext),
		int(r.timeout().Seconds()), quote(prompt), quote(log),
		quote(page), quote(page), quote(NoOutput), quote(log))
}

func (r *Remote) logf(format string, args ...any) {
	if r.Logf != nil {
		r.Logf(format, args...)
	}
}

// prompt puts the instruction on the host once and returns its remote path.
//
// Named by hash, because the prompt is what the answers are a function of. Two
// runs with different prompts must not share a file, and a host that already
// has this exact one needs nothing copied.
func (r *Remote) prompt(ctx context.Context) (string, error) {
	r.once.Do(func() {
		if strings.TrimSpace(r.Prompt) == "" {
			r.pushErr = fmt.Errorf("host %s was given no prompt", r.Host.Name)
			return
		}
		sum := sha256.Sum256([]byte(r.Prompt))
		name := "prompt-" + hex.EncodeToString(sum[:])[:8] + ".md"
		root := r.Host.root()

		scratch, err := os.MkdirTemp("", "kvant-prompt-")
		if err != nil {
			r.pushErr = err
			return
		}
		defer func() { _ = os.RemoveAll(scratch) }()
		local := filepath.Join(scratch, name)
		if err := os.WriteFile(local, []byte(r.Prompt), 0o644); err != nil {
			r.pushErr = err
			return
		}
		if _, err := r.Shell.Run(ctx, r.Host.Name, "mkdir -p "+quote(root)); err != nil {
			r.pushErr = fmt.Errorf("prepare %s: %w", r.Host.Name, err)
			return
		}
		if err := r.Copy.Push(ctx, r.Host.Name, []string{local}, root); err != nil {
			r.pushErr = fmt.Errorf("push prompt to %s: %w", r.Host.Name, err)
			return
		}
		r.pushed = path(root, name)
	})
	return r.pushed, r.pushErr
}

// quotaSign reports which of the pool's ways of saying it is empty appeared in
// some output, or the empty string.
// quotaSign is the sign the text carries, or empty if it carries none.
//
// The whitespace is flattened before the search because the tool prints through
// a console renderer that rewraps everything to the terminal width, so the
// phrase to be matched arrives broken across a line at whatever column the box
// happened to be. Matching the flattened text makes the search independent of
// how wide the terminal was.
func quotaSign(text string) string {
	flat := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for _, sign := range quotaSigns {
		if strings.Contains(flat, sign) {
			return sign
		}
	}
	return ""
}

// toolReason picks the account of a failure out of the tool's log.
//
// The log opens with a banner and a count of images and closes with a progress
// bar and a rule, and the reason a page did not read is one line in the middle
// of that. Condensing the whole thing puts the banner in the failure message
// and truncates before ever reaching the reason, which is how a run once
// reported six refusals as six copies of its own heading. So the rules and the
// progress bar are dropped, what is left is flattened back into one line, and
// the report starts at the last thing the tool called a failure.
func toolReason(log string) string {
	var kept []string
	for line := range strings.SplitSeq(log, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.ContainsAny(line, "─━╭╰│") {
			continue
		}
		kept = append(kept, line)
	}
	flat := strings.Join(strings.Fields(strings.Join(kept, " ")), " ")
	for _, marker := range []string{"upload cap", "fail ", "error"} {
		if at := strings.LastIndex(strings.ToLower(flat), marker); at >= 0 {
			flat = flat[at:]
			break
		}
	}
	if strings.TrimSpace(flat) == "" {
		return "the tool wrote no page and said nothing about why"
	}
	return condense(flat)
}

// StripFrontMatter removes a leading YAML block from a page the tool wrote.
//
// The tool stamps what read a page, which conversation it was and how long it
// took at the top of its own output. That is its provenance and not the
// magazine's, and the corpus writes its own front matter from the queue, so two
// blocks would arrive in one file and the second one would be read as text. A
// page that does not open with a fence is returned untouched.
func StripFrontMatter(text string) string {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return text
	}
	// "---" with something after it on the same line is a horizontal rule or the
	// start of a page, not a fence.
	line, rest, ok := strings.Cut(trimmed[len("---"):], "\n")
	if !ok || strings.TrimSpace(line) != "" {
		return text
	}
	for {
		line, tail, ok := strings.Cut(rest, "\n")
		if !ok {
			// An opening fence with no closing one is not front matter, and
			// throwing the page away on that reading would lose a whole page to a
			// guess.
			return text
		}
		if strings.TrimSpace(line) == "---" {
			return strings.TrimLeft(tail, "\r\n")
		}
		rest = tail
	}
}

// Pool spreads pages over several boxes.
//
// The quota is per account and the accounts are per host, so two hosts are two
// pools and not one bigger one. Rotating means a run keeps reading while one
// host is empty, and it is a plain rotation rather than anything cleverer
// because the thing being balanced refills on a clock nothing here can see.
type Pool struct {
	Lanes []Engine
	next  atomic.Uint64
}

// Name is the model, which every lane in a pool shares. A pool of lanes that
// disagreed about it would write two different models into one issue's pages.
func (p *Pool) Name() string {
	if len(p.Lanes) == 0 {
		return "chatgpt"
	}
	return p.Lanes[0].Name()
}

// Read hands the page to the next lane, and on to the one after that when the
// lane it picked has nothing left.
//
// Only ErrNoQuota is worth moving a page for. A page that failed to read is
// likely to fail again on another box, and trying every host in turn would
// spend the whole fleet on one bad scan.
func (p *Pool) Read(ctx context.Context, image string) (string, error) {
	if len(p.Lanes) == 0 {
		return "", errors.New("the pool has no hosts")
	}
	var failed error
	start := p.next.Add(1) - 1
	for i := range p.Lanes {
		lane := p.Lanes[(start+uint64(i))%uint64(len(p.Lanes))]
		text, err := lane.Read(ctx, image)
		if err == nil {
			return text, nil
		}
		failed = errors.Join(failed, err)
		if !errors.Is(err, ErrNoQuota) {
			return "", err
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	// Every host was out, so the pool is out, and the caller has to hear that
	// rather than a list of hosts.
	return "", fmt.Errorf("%w: every host in the pool: %w", ErrNoQuota, failed)
}
