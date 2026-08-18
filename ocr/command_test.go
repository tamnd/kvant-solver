package ocr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

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
		name string
		args []string
		want string
	}{
		{
			"it complained on standard error",
			[]string{"-c", "echo 'the credentials expired' >&2; exit 1"},
			"the credentials expired",
		},
		{
			"it complained on standard output",
			[]string{"-c", "echo 'usage limit reached'; exit 1"},
			"usage limit reached",
		},
		{
			"it complained nowhere at all",
			[]string{"-c", "exit 1"},
			"and said nothing",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			engine := ocr.Command{Path: "/bin/sh", Args: c.args, Prompt: "read it"}
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
