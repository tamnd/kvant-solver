package figures

import (
	"regexp"
	"strings"
	"unicode"
)

// An Anchor is where the text says a figure stands: a ⟦figure⟧ mark, or a
// caption line ("Рис. 5. ...") the reading wrote without one. A mark with its
// caption under it is one anchor and not two.
type Anchor struct {
	Line   int    // the line it is on, from zero
	Marker bool   // a ⟦figure⟧ mark rather than a bare caption
	Number string // the figure's printed number, when it has one
	Key    string // what finds it again: the first letters of its caption or the text after it
}

const marker = "⟦figure⟧"

// caption is a line that starts a figure caption in Russian, in the languages
// the corpus is translated into, or as the reading sometimes spells it.
var caption = regexp.MustCompile(`^\s*(?:Рис|Рисунок|Hình|Fig|Figure|图)\.?\s*(\d+)`)

// Anchors finds the anchors in a page or an article body, in order.
func Anchors(body string) []Anchor {
	lines := strings.Split(body, "\n")
	var out []Anchor
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == marker:
			a := Anchor{Line: i, Marker: true}
			if j := nextText(lines, i); j >= 0 {
				a.Key = key(lines[j])
				if m := caption.FindStringSubmatch(lines[j]); m != nil {
					a.Number = m[1]
					i = j // the caption belongs to the mark
				}
			}
			out = append(out, a)
		case caption.MatchString(line):
			m := caption.FindStringSubmatch(line)
			out = append(out, Anchor{Line: i, Number: m[1], Key: key(line)})
		}
	}
	return out
}

// nextText is the next line with words on it, within two blank lines.
func nextText(lines []string, i int) int {
	for j := i + 1; j < len(lines) && j <= i+3; j++ {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "⟦") {
			return -1
		}
		return j
	}
	return -1
}

// key is the first forty letters and digits of a line, lowercased, which
// survives the reading's spacing and punctuation.
func key(line string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.ToLower(line) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			if n++; n == 40 {
				break
			}
		}
	}
	return b.String()
}
