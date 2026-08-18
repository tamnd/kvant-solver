package ocr_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

// shell is a program that says the given line and then fails, whichever
// operating system the test is running on. The lane runs whatever it is
// pointed at and the interesting part is what comes back from a program that
// exits non zero, so the cheapest such program will do.
func shell(script string) (string, []string) {
	if runtime.GOOS == "windows" {
		if script == "" {
			return "cmd", []string{"/c", "exit 1"}
		}
		return "cmd", []string{"/c", script + " & exit 1"}
	}
	if script == "" {
		return "/bin/sh", []string{"-c", "exit 1"}
	}
	return "/bin/sh", []string{"-c", script + "; exit 1"}
}

// A repair pass over 1975 printed "exit status 1:" seven hundred times with
// nothing after the colon, because the CLI it runs writes its errors on
// standard output along with everything else and the lane only kept standard
// error. An error nobody can read is an error nobody can fix.
func TestAFailedCommandSaysWhatWentWrong(t *testing.T) {
	image := filepath.Join(t.TempDir(), "0001.jpg")
	if err := os.WriteFile(image, []byte("not really a jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		script string
		want   string
	}{
		{
			"it complained on standard error",
			"echo the credentials expired 1>&2",
			"the credentials expired",
		},
		{
			"it complained on standard output",
			"echo usage limit reached",
			"usage limit reached",
		},
		{
			"it complained nowhere at all",
			"",
			"and said nothing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, args := shell(c.script)
			engine := ocr.Command{Path: path, Args: args, Prompt: "read it"}
			_, err := engine.Read(context.Background(), image)
			if err == nil {
				t.Fatal("a program that exits 1 read the page")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error is %q, want it to carry %q", err, c.want)
			}
		})
	}
}
