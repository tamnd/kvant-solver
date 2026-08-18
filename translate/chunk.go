// Package translate turns the Russian corpus into other languages, and decides
// without a model call which of its output has gone stale.
//
// The deciding is the harder half. Translating twenty thousand pages is a
// question of time and tokens, but a corpus that is translated once and then
// edited is a corpus where nobody can say which English files still correspond
// to the Russian they came from. Rereading all of them through a model to find
// out costs as much as the translation did, so the answer has to be readable
// off the files themselves. That is what the hashes in corpus.Translated are
// for and it is why they are written even though nothing reads them at
// translation time.
package translate

import (
	"strings"

	"github.com/tamnd/kvant-solver/mathtex"
)

// ChunkChars is how much body goes in one call.
//
// A block is never split, so this is a target and not a limit: a chunk is
// blocks added until the next one would take it past this, and a single block
// longer than this goes on its own.
const ChunkChars = 6000

// ChunkSpans is how many pieces of mathematics go in one call.
//
// Characters alone are the wrong budget. What a model cannot do is copy forty
// formulas byte for byte while translating the sentences between them, and a
// chunk is refused for the mathematics long before it is refused for length. A
// Kvant page of worked algebra is short prose and dense notation, which is
// exactly the shape that slips past a character budget and fails.
//
// Splitting on this costs more calls and makes a slip cost one call instead of
// a whole article, which is the trade worth making while a refusal is expensive
// and a call is not.
const ChunkSpans = 15

// A Chunk is one call's worth of a body.
type Chunk struct {
	// Index is one based and Of is how many the body came to.
	Index, Of int
	Body      string
	// Spans is how much mathematics went with it, which is what the audit
	// counts back on the way in.
	Spans int
}

// Chunks cuts a body into pieces small enough to translate in one call.
//
// The boundary is a blank line, which is a boundary the Markdown already has,
// so joining the answers with a blank line between them reproduces the block
// structure of the source exactly. Nothing else has to be reassembled and no
// sentence is ever cut in half.
//
// What is lost is context: whoever translates chunk four has not read chunk
// three. The glossary is what makes up for that, and it is why the glossary is
// built before anything is translated rather than after.
func Chunks(body string) []Chunk {
	blocks := Blocks(body)
	if len(blocks) == 0 {
		return nil
	}
	var out []Chunk
	var current []string
	chars, spans := 0, 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		out = append(out, Chunk{Body: strings.Join(current, "\n\n"), Spans: spans})
		current, chars, spans = nil, 0, 0
	}

	for _, block := range blocks {
		n := countSpans(block)
		// A block that is over budget on its own still goes over on its own,
		// because splitting inside a block would cut a sentence or a display.
		// It gets a chunk to itself rather than dragging a neighbour with it.
		tooLong := chars+len(block) > ChunkChars
		tooMuchMath := spans+n > ChunkSpans
		if len(current) > 0 && (tooLong || tooMuchMath) {
			flush()
		}
		current = append(current, block)
		chars += len(block)
		spans += n
	}
	flush()

	for i := range out {
		out[i].Index, out[i].Of = i+1, len(out)
	}
	return out
}

// Blocks splits a body on blank lines and drops the empties.
func Blocks(body string) []string {
	var out []string
	for _, block := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n\n") {
		if trimmed := strings.TrimSpace(block); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// Join puts the translated chunks back together.
func Join(parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, "\n\n") + "\n"
}

// countSpans is how many pieces of mathematics a block carries.
func countSpans(block string) int {
	spans, _ := mathtex.Split(block)
	return len(spans)
}
