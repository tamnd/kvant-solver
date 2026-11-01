package problems

import (
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/corpus"
)

// spilled reads the problems a Задачник heading promised off the articles next
// to it.
//
// Assembly cuts at the page and the magazine cuts at the article, so the last
// problems of a spread routinely share a page with the heading of whatever
// comes next, and when the cut at that heading misses, the whole page goes to
// the next article. The words are then in the corpus under the wrong title. A
// number counts here only when a heading of the same issue promised it and no
// Задачник page carried it, so an ordinary article that happens to open a line
// with «М5.» cannot invent a problem.
func spilled(art Article, missing map[corpus.ProblemID]Kind) []Item {
	var out []Item
	for _, item := range Split(spillRegion(art), Posed) {
		kind, ok := missing[item.ID]
		if !ok {
			continue
		}
		item.Kind = kind
		out = append(out, item)
	}
	return out
}

// spillRegion is the part of an article that can belong to the article before
// it: everything above its own heading, or its first page when the heading is
// not found.
func spillRegion(art Article) string {
	body := art.Body
	if at := headingAt(body, art.Front.Title); at >= 0 {
		return body[:at]
	}
	const folio = "⟦folio"
	first := strings.Index(body, folio)
	if first < 0 {
		return body
	}
	if next := strings.Index(body[first+len(folio):], folio); next >= 0 {
		return body[:first+len(folio)+next]
	}
	return body
}

// headingAt is the offset of the first short line that spells the title, or -1.
func headingAt(body, title string) int {
	want := spelled(title)
	if len([]rune(want)) < 4 {
		return -1
	}
	at := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		have := spelled(line)
		if have != "" && len([]rune(strings.TrimSpace(line))) <= 120 && strings.HasPrefix(have, want) {
			return at
		}
		at += len(line)
	}
	return -1
}

// spelled is the letters of s, lower case, with everything else dropped.
func spelled(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
