package ocr

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ImagePlaceholder is replaced by the page image path wherever it appears in a
// Command's arguments. A command that never mentions it is expected to read the
// path out of the prompt instead, which is what an agent style tool does.
const ImagePlaceholder = "{image}"

// Command is an engine that runs a local program to read a page.
//
// It exists because the fast lane cannot read every page and something has to
// read the rest. Of 84 sheets of 1975 №1, GLM-OCR produced 71 the rules accept
// and 13 they do not, and no amount of retrying moves that number: the sampling
// is pinned at zero, so a page that came back wrong comes back identically
// wrong. A repair has to change something, and the thing worth changing is the
// model.
//
// The measurement behind picking a command rather than another endpoint: the
// Claude CLI reading these sheets six at a time came to 7.5 seconds a page with
// about 97 per cent word agreement against a careful reference, where the card
// gives 1.4 seconds and 81 per cent. That is the right way round for a repair
// lane, which handles the pages that are hard and few.
//
// The program is given the prompt on standard input and answers on standard
// output. That is the whole contract, and it is deliberately the plainest one
// available: every tool in this space speaks it, and a lane that needs an API
// key, a tunnel and a server is a lane that is down when it is needed.
type Command struct {
	// Model is what goes in the page front matter as extraction_model. It is
	// separate from the program name because the program is a wrapper and the
	// corpus should record what actually read the page.
	Model string

	// Path is the program and Args are its arguments, with ImagePlaceholder
	// standing for the page image.
	Path string
	Args []string

	// Prompt goes on standard input. The image path is appended to it, because
	// a tool that reads files needs telling which one and there is nowhere else
	// to say so.
	Prompt string

	// Dir is the working directory, empty for the current one.
	Dir string

	// Timeout bounds one page. Zero means DefaultPageTimeout.
	Timeout time.Duration
}

// Name is the model name for the report, or the program name when the caller
// did not give one.
func (c Command) Name() string {
	if c.Model != "" {
		return c.Model
	}
	return filepath.Base(c.Path)
}

func (c Command) Read(ctx context.Context, image string) (string, error) {
	if strings.TrimSpace(c.Path) == "" {
		return "", fmt.Errorf("no command to run")
	}
	path, err := filepath.Abs(image)
	if err != nil {
		return "", err
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultPageTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := make([]string, len(c.Args))
	for i, arg := range c.Args {
		args[i] = strings.ReplaceAll(arg, ImagePlaceholder, path)
	}

	var out, errs bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.Dir = c.Dir
	cmd.Stdin = strings.NewReader(c.ask(path))
	cmd.Stdout = &out
	cmd.Stderr = &errs
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("read %s: %w after %s", filepath.Base(image), ctx.Err(), timeout)
		}
		return "", fmt.Errorf("read %s: %w: %s", filepath.Base(image), err, condense(errs.String()))
	}
	return out.String(), nil
}

// ask is the prompt as the program receives it.
//
// The sentence at the end is not decoration. A tool that reads files reads the
// one it is told to, and a prompt that only describes what a page looks like
// leaves it guessing or, worse, answering about a page it has seen before.
func (c Command) ask(image string) string {
	return strings.TrimSpace(c.Prompt) + "\n\nThe page image is at " + image +
		". Read it and answer with the transcription and nothing else.\n"
}
