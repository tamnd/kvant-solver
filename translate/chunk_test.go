package translate

import (
	"strings"
	"testing"
)

func TestAShortBodyIsOneChunk(t *testing.T) {
	body := "Первый абзац.\n\nВторой абзац.\n"
	chunks := Chunks(body)
	if len(chunks) != 1 {
		t.Fatalf("expected one chunk, got %d", len(chunks))
	}
	if chunks[0].Index != 1 || chunks[0].Of != 1 {
		t.Fatalf("numbered %d of %d, want 1 of 1", chunks[0].Index, chunks[0].Of)
	}
}

func TestNoChunkCutsInsideABlock(t *testing.T) {
	// The whole reason the boundary is a blank line is that joining the answers
	// with a blank line between them then reproduces the source structure exactly
	// and no sentence is ever cut in half.
	var b strings.Builder
	for range 10 {
		b.WriteString(strings.Repeat("слово ", 200))
		b.WriteString("\n\n")
	}
	blocks := Blocks(b.String())
	for _, c := range Chunks(b.String()) {
		for _, got := range Blocks(c.Body) {
			if !contains(blocks, got) {
				t.Fatalf("a chunk carries something that is not a whole block: %.40q", got)
			}
		}
	}
}

func TestEveryBlockSurvivesChunking(t *testing.T) {
	var b strings.Builder
	for i := range 30 {
		b.WriteString(strings.Repeat("текст ", 60))
		if i%3 == 0 {
			b.WriteString("$a^2 + b^2 = c^2$ и $x = 1$ и $y = 2$")
		}
		b.WriteString("\n\n")
	}
	body := b.String()
	want := Blocks(body)

	var got []string
	for _, c := range Chunks(body) {
		got = append(got, Blocks(c.Body)...)
	}
	if len(got) != len(want) {
		t.Fatalf("chunking turned %d blocks into %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block %d came back as %.40q, want %.40q", i, got[i], want[i])
		}
	}
}

func TestABodyThatIsMostlyMathematicsIsSplitOnTheMathematics(t *testing.T) {
	// This is the case a character budget alone gets wrong. Short prose with
	// dense notation is well under the character limit and is exactly the shape a
	// model fails to copy formula for formula.
	var b strings.Builder
	for range 12 {
		b.WriteString("Отсюда $a = 1$ и $b = 2$ и $c = 3$.\n\n")
	}
	chunks := Chunks(b.String())
	if len(chunks) < 2 {
		t.Fatalf("a body with %d formulas came out as %d chunk", 36, len(chunks))
	}
	for _, c := range chunks {
		if c.Spans > ChunkSpans && len(Blocks(c.Body)) > 1 {
			t.Fatalf("chunk %d carries %d spans, over the budget of %d", c.Index, c.Spans, ChunkSpans)
		}
	}
}

func TestABlockOverTheBudgetGetsAChunkOfItsOwn(t *testing.T) {
	// It cannot be split, so the only question is whether it drags a neighbour
	// over the budget with it.
	huge := strings.Repeat("длинный ", ChunkChars/4)
	body := "Короткий абзац.\n\n" + huge + "\n\nЕщё один короткий абзац."
	chunks := Chunks(body)
	if len(chunks) != 3 {
		t.Fatalf("expected three chunks, got %d", len(chunks))
	}
	if Blocks(chunks[1].Body)[0] != strings.TrimSpace(huge) {
		t.Fatal("the oversized block did not get a chunk to itself")
	}
}

func TestChunksAreNumberedForTheAudit(t *testing.T) {
	var b strings.Builder
	for range 40 {
		b.WriteString(strings.Repeat("текст ", 80))
		b.WriteString("\n\n")
	}
	chunks := Chunks(b.String())
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c.Index != i+1 || c.Of != len(chunks) {
			t.Fatalf("chunk %d numbered %d of %d", i, c.Index, c.Of)
		}
	}
}

func TestAnEmptyBodyIsNoChunksRatherThanOneEmptyOne(t *testing.T) {
	// A page that read as nothing should produce no calls at all, not one call
	// asking a model to translate a blank.
	for _, body := range []string{"", "\n", "   \n\n  \n"} {
		if chunks := Chunks(body); len(chunks) != 0 {
			t.Fatalf("%q gave %d chunks, want none", body, len(chunks))
		}
	}
}

func TestJoiningTheAnswersRebuildsTheBlockStructure(t *testing.T) {
	body := "First block.\n\nSecond block.\n\nThird block.\n"
	if got := Join(Blocks(body)); got != body {
		t.Fatalf("round trip gave %q, want %q", got, body)
	}
}

func TestJoinDropsTheBlanksAModelSendsBack(t *testing.T) {
	if got := Join([]string{"One.", "  ", "", "Two."}); got != "One.\n\nTwo.\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWindowsLineEndingsDoNotChangeTheBlocks(t *testing.T) {
	unix := Blocks("Первый.\n\nВторой.\n")
	windows := Blocks("Первый.\r\n\r\nВторой.\r\n")
	if len(unix) != len(windows) {
		t.Fatalf("CRLF gave %d blocks, LF gave %d", len(windows), len(unix))
	}
	for i := range unix {
		if unix[i] != windows[i] {
			t.Fatalf("block %d differs: %q against %q", i, windows[i], unix[i])
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
