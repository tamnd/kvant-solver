package corpus

import "testing"

// The whole point of normalising before hashing is that edits which change no
// content do not restale every translation of the file, so that is what this
// tests.
func TestHashIsStableAgainstNoise(t *testing.T) {
	base := "First line.\n\nSecond line.\n"
	same := []string{
		"First line.\n\nSecond line.\n",
		"First line.\r\n\r\nSecond line.\r\n",
		"First line.   \n\nSecond line.\t\n",
		"\n\nFirst line.\n\n\n\nSecond line.\n\n\n",
		"First line.\n\nSecond line.",
	}
	want := HashBody(base)
	for _, s := range same {
		if got := HashBody(s); got != want {
			t.Errorf("hash moved for a body that says the same thing: %q", s)
		}
	}
}

func TestHashMovesForRealEdits(t *testing.T) {
	want := HashBody("First line.\n\nSecond line.\n")
	different := []string{
		"First line.\n\nSecond line changed.\n",
		"First line.\nSecond line.\n",
		"first line.\n\nSecond line.\n",
		"",
	}
	for _, s := range different {
		if got := HashBody(s); got == want {
			t.Errorf("hash did not move for a real edit: %q", s)
		}
	}
}

func TestNormaliseEmpty(t *testing.T) {
	for _, s := range []string{"", "\n", "   \n\n\t\n"} {
		if got := Normalise(s); got != "" {
			t.Errorf("Normalise(%q) gave %q, want empty", s, got)
		}
	}
}

func TestHashStringDoesNotNormalise(t *testing.T) {
	if HashString("a ") == HashString("a") {
		t.Error("HashString should see a trailing space, a prompt is not a body")
	}
}
