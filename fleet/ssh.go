// Package fleet is the three rented boxes: what is installed on them, the
// tunnels that reach their loopback listeners, and the supervisor that puts a
// tunnel back when it dies.
//
// Every listener in the fleet binds 127.0.0.1, which is correct and stays that
// way. The only path in is ssh, so ssh is the transport for everything here:
// the port forwards, the probes, and later the OCR batches, which do not go
// over HTTP at all.
package fleet

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Runner runs a command on a host. It is an interface because every test in
// this package would otherwise need three rented Linux boxes.
type Runner interface {
	Run(ctx context.Context, host string, command string) (string, error)
}

// SSH runs commands with the ssh binary, using the user's own config. Reading
// ~/.ssh/config is the point: server1 is tam and server3 is root, the key is
// id_ed25519_deploy, and none of that belongs in this repo.
type SSH struct {
	// Binary is the ssh to run. Empty means the one on PATH.
	Binary string
	// Timeout bounds one command. It is not the tunnel's timeout; a tunnel runs
	// for hours.
	Timeout time.Duration
	// Options are extra -o flags. BatchMode is always set, because a command
	// that stops for a passphrase in an unattended run hangs until somebody
	// notices days later.
	Options []string
}

func (s SSH) binary() string {
	if strings.TrimSpace(s.Binary) != "" {
		return s.Binary
	}
	return "ssh"
}

func (s SSH) args(host string, rest ...string) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10"}
	for _, option := range s.Options {
		args = append(args, "-o", option)
	}
	args = append(args, host)
	return append(args, rest...)
}

// Run executes a shell command on the host and returns its standard output.
//
// Standard error is folded into the error rather than dropped, because the one
// thing worth knowing when a remote command fails is what it printed.
func (s SSH) Run(ctx context.Context, host, command string) (string, error) {
	if strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("no host")
	}
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, s.binary(), s.args(host, command)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(out)
		}
		if ctx.Err() != nil {
			return out, fmt.Errorf("%s: %w after %s", host, ctx.Err(), s.Timeout)
		}
		return out, fmt.Errorf("%s: %s: %w", host, condense(detail), err)
	}
	return out, nil
}

func condense(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return text
}
