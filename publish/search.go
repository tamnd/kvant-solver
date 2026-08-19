package publish

import (
	"encoding/json"
	"strings"
	"unicode"
)

// The search index is built here and shipped as a file, because the site has no
// JavaScript and is not going to grow any.
//
// That sounds like a contradiction and it is not. Search on a static site is
// either somebody else's server, which means handing the corpus to a third
// party and putting a network call in front of a reader who only wanted to read
// a 1975 article about the Coriolis force, or it is an index the browser
// downloads and searches itself. The second is the right shape and the index is
// the hard half of it: knowing every document, its title, who wrote it, which
// section it ran in and what it says. That half is done here, once, at build
// time, and written where anything can read it.
//
// What consumes it is deliberately left open. A reader can grep it, a script
// can join it against the corpus, and a front end that wants live search has
// the index it needs without this build having to pick its library. The pages
// themselves stay readable with scripting off, which is the requirement.

// Record is one document in the index.
//
// The field names are short because there are twenty two thousand of them and
// the names are repeated in every one.
type Record struct {
	Kind    string   `json:"kind"`
	Href    string   `json:"href"`
	Title   string   `json:"title"`
	Year    int      `json:"year,omitempty"`
	Issue   string   `json:"issue,omitempty"`
	Authors []string `json:"authors,omitempty"`
	Rubric  string   `json:"rubric,omitempty"`
	Tag     string   `json:"tag,omitempty"`
	Pages   string   `json:"pages,omitempty"`
	// Text is the opening of the body with the markup taken off. It is a
	// snippet and not the whole thing: the corpus is four and a half megabytes
	// of Markdown and an index carrying all of it is the corpus again, in a
	// worse format, next to the corpus.
	Text string `json:"text,omitempty"`
}

// snippetRunes is how much of a body goes into the index. Enough to tell two
// articles with similar titles apart and to catch the terms an opening
// paragraph uses, and not so much that the index stops being an index.
const snippetRunes = 400

// searchIndex writes search.json.
//
// It carries no build date. Two builds of one corpus have to come out
// byte for byte the same, because that is what makes a rebuild reviewable as a
// diff, and a timestamp is the usual way that gets lost.
func (s *Site) searchIndex() error {
	records := make([]Record, 0, len(s.docs))
	for _, d := range s.docs {
		records = append(records, Record{
			Kind:    d.Kind,
			Href:    d.Href,
			Title:   d.Title,
			Year:    d.Year,
			Issue:   d.Issue,
			Authors: d.Authors,
			Rubric:  d.Rubric,
			Tag:     d.Tag,
			Pages:   d.Pages,
			Text:    cut(d.Text, snippetRunes),
		})
	}
	data, err := json.Marshal(struct {
		Count   int      `json:"count"`
		Records []Record `json:"records"`
	}{Count: len(records), Records: records})
	if err != nil {
		return err
	}
	return s.write("search.json", data)
}

// plain takes the markup off a body.
//
// It is deliberately rough. What the index needs is the words, and a real
// Markdown walk to get them would mean rendering every body twice for the sake
// of a four hundred character snippet. So the reading marks go, the mathematics
// goes, and the characters Markdown uses for emphasis and headings go.
//
// The mathematics goes rather than being kept as TeX because \frac{a}{b} is not
// a word anybody searches for, and leaving it in would fill the snippet of a
// mathematical article with backslashes instead of its subject.
func plain(body string) string {
	var out strings.Builder
	out.Grow(len(body))
	marks := 0 // nesting depth inside a ⟦ ⟧ mark
	inMath := false
	dollars := false // the previous rune was part of a delimiter
	for _, r := range body {
		wasDollars := dollars
		dollars = r == '$'
		switch {
		case r == '⟦':
			marks++
		case r == '⟧':
			if marks > 0 {
				marks--
			}
		case marks > 0:
			// inside a mark, which is the reading's note to itself
		case r == '$':
			// A run of dollars is one delimiter, so that the two of a display
			// formula open it once rather than opening and closing it.
			if !wasDollars {
				inMath = !inMath
			}
		case inMath:
			// inside a formula
		case r == '#' || r == '*' || r == '_' || r == '`' || r == '>':
			out.WriteByte(' ')
		default:
			out.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// cut trims a snippet to length, at a word boundary when there is one nearby,
// so that the index does not end every entry in the middle of a word.
func cut(text string, limit int) string {
	rs := []rune(text)
	if len(rs) <= limit {
		return text
	}
	rs = rs[:limit]
	for i := len(rs) - 1; i > limit-40 && i > 0; i-- {
		if unicode.IsSpace(rs[i]) {
			return strings.TrimRight(string(rs[:i]), " ") + "…"
		}
	}
	return string(rs) + "…"
}
