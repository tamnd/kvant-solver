// Package pdfsrc wraps the poppler command line tools.
//
// Everything this project learns about a PDF comes through here: the page
// count, the native text layer, the fonts, the embedded images, and the page
// renders that would feed a vision model. Keeping the exec calls in one place
// means the rest of the code can be tested without poppler installed, and it
// means there is one answer to what flags each tool is called with.
package pdfsrc

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs an external command and returns its standard output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands for real.
type ExecRunner struct{}

// Run executes the command and returns stdout. A non-zero exit becomes an
// error carrying the first line of stderr, which is where poppler puts the
// useful part of a failure and everything after it is noise.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			return stdout.Bytes(), fmt.Errorf("%s: %w", name, err)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return stdout.Bytes(), nil
}

// FakeRunner replays canned output, keyed by the whole command line joined
// with spaces. A test registers the exact invocations it expects, so changing
// the flags of a poppler call is something a test notices rather than
// something that quietly starts measuring a different thing.
type FakeRunner struct {
	Out  map[string]string
	Err  map[string]error
	Seen []string
}

// Run looks the command line up in Out.
func (f *FakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.Seen = append(f.Seen, key)
	if err, ok := f.Err[key]; ok {
		return nil, err
	}
	out, ok := f.Out[key]
	if !ok {
		return nil, fmt.Errorf("FakeRunner: no canned output for %q", key)
	}
	return []byte(out), nil
}

// Available reports whether the poppler tools this package needs are on the
// path. The commands that need them say so and stop rather than failing four
// issues into a run.
func Available() error {
	var missing []string
	for _, tool := range []string{"pdfinfo", "pdftotext", "pdffonts", "pdftoppm", "pdfimages"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("poppler is not installed: %s not on the path", strings.Join(missing, ", "))
	}
	return nil
}
