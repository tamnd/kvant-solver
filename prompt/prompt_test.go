package prompt

import (
	"strings"
	"testing"
)

func TestThePagePromptSaysWhatThisMagazineNeeds(t *testing.T) {
	text := OCRPage()
	// Every one of these is a rule something downstream reads. Losing one in an
	// edit is not a worse transcription, it is a stage that stops working, and
	// at 148 seconds a page the re-read costs a day a year. So they are pinned
	// here rather than trusted to review.
	for _, want := range []string{
		"$...$",    // inline math, what the KaTeX check parses
		"$$...$$",  // display math
		"⟦folio",   // the printed page number, which the page map reads
		"⟦rubric⟧", // what assemble splits on
		"⟦column⟧", // the column break, so reading order is checkable
		"⟦figure⟧", // figures are marked, never described
		"⟪illegible⟫",
		`\mathrm{tg}`, // the Russian function names, kept
		"3{,}14",      // the decimal comma
		"М1234",       // the Cyrillic problem number
		"Задачник",    // at least one rubric named, so the model knows the shape
		"do not translate",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the page prompt no longer says %q", want)
		}
	}
	// The magazine sets these the Russian way and a model left to itself will
	// quietly Anglicise them, which changes what the formula says to a reader
	// who knows the convention.
	for _, wrong := range []string{"never tan, cot, arctan", "not $3.14$"} {
		if !strings.Contains(text, wrong) {
			t.Errorf("the page prompt no longer forbids the wrong form: %q", wrong)
		}
	}
}

func TestThePromptHashIsStableAndSpecific(t *testing.T) {
	first, second := OCRPageSHA256(), OCRPageSHA256()
	if first != second {
		t.Fatalf("the same prompt hashed two ways: %s and %s", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("a sha256 in hex is 64 characters, got %d", len(first))
	}
	if SHA256("a") == SHA256("a\n") {
		t.Error("a trailing newline is a different prompt and has to hash differently")
	}
}

func TestThePromptIsTrimmedAndEndsWithOneNewline(t *testing.T) {
	text := OCRPage()
	if strings.HasPrefix(text, "\n") {
		t.Error("the prompt starts with a blank line")
	}
	if !strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\n\n") {
		t.Error("the prompt should end with exactly one newline")
	}
}
