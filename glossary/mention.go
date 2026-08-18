package glossary

import (
	"sort"
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/mathtex"
)

// Mentioned is the rows a body should be shown.
//
// Nothing is gained by sending the whole glossary with every chunk. It is the
// largest part of the prompt, most of it is irrelevant to any one article, and
// under the third staleness test a file is invalidated when a row it was shown
// changes, so a file shown everything is stale every time anybody touches
// anything.
//
// The matching is on stems rather than on the words themselves, because Russian
// is inflected and the glossary is written in the nominative. «производная»
// appears in a real article as «производной», «производную» and «производных»,
// and exact matching finds the dictionary form almost nowhere. A glossary that
// only fires on the nominative is a glossary that is quietly not being applied.
//
// Formulas are cut out before matching. A term like «ток» is three letters and
// the inside of a LaTeX span is full of three letter fragments, so matching
// there produces rows nobody needs and, worse, makes a file stale for a term it
// never used in prose.
func (g Glossary) Mentioned(body, lang string) []Term {
	words := wordsIn(prose(body))
	var out []Term
	for _, t := range g.Terms {
		if t.In(lang) == "" {
			continue
		}
		if mentions(words, t.RU) {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Key(out[i].RU) < Key(out[j].RU) })
	return out
}

// mentions reports whether every word of the term appears in the body.
//
// Adjacency is not checked, so «полное внутреннее отражение» matches a body
// that has all three words scattered about. That is deliberate and it errs in
// the safe direction: showing a translator a row it did not need costs a line
// of prompt, and withholding one it did need costs a wrong term in the archive
// that nobody will notice. Over inclusion also makes the file stale slightly
// more often than it strictly must, which is the price and it is the cheaper
// of the two mistakes.
func mentions(w words, term string) bool {
	parts := split(strings.ToLower(term))
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !w.has(p) {
			return false
		}
	}
	return true
}

// words is a body reduced to what it can be matched against: the set of stems
// for the common case, and the stems themselves again in a slice for the case
// where the stemmer stopped short.
type words struct {
	set  map[string]bool
	list []string
}

// has reports whether the body used this word in some form.
func (w words) has(term string) bool {
	s := stem(term)
	if w.set[s] {
		return true
	}
	// The stemmer refuses to cut a short word down to a fragment, so «ток» stays
	// whole and so does «тока», and the two never meet in the set. Rather than
	// lower that floor, which is what would make «ток» collide with «то», the
	// leftover cases are caught here: one stem is the other with a short tail of
	// ending letters on it.
	for _, candidate := range w.list {
		if inflectionOf(candidate, s) {
			return true
		}
	}
	return false
}

// inflectionOf reports whether two stems are the same word with different
// endings on them.
//
// The tail has to be short and has to be made of the letters Russian endings
// are made of, which is what keeps «токарь» from matching «ток» while «током»
// and «токах» still do. Three letters is the longest ending this needs to cover
// and going further starts pulling in unrelated words.
func inflectionOf(a, b string) bool {
	if a == b {
		return true
	}
	long, short := []rune(a), []rune(b)
	if len(long) < len(short) {
		long, short = short, long
	}
	if len(short) < shortestMatch || !strings.HasPrefix(string(long), string(short)) {
		return false
	}
	tail := long[len(short):]
	if len(tail) > longestTail {
		return false
	}
	for _, r := range tail {
		if !strings.ContainsRune(endingLetters, r) {
			return false
		}
	}
	return true
}

// shortestMatch is how much of a word has to be shared before a difference in
// the tail is read as an inflection rather than as two different words. Below
// this «то» would be an inflection of «ток».
const shortestMatch = 3

// longestTail is the longest Russian ending this treats as an ending.
const longestTail = 3

// endingLetters is every letter that appears in the endings above. A tail with
// anything else in it is part of the word and not an ending.
const endingLetters = "аеийоуыьэюяхвм"

// prose is the body with the mathematics taken out.
//
// The span offsets are counted in runes, not bytes, so this walks the body in
// runes too. Slicing a Cyrillic body by rune offsets as though they were byte
// offsets cuts in the wrong place and, since almost every letter here is two
// bytes, cuts roughly twice as much prose as it should.
func prose(body string) string {
	spans, _ := mathtex.Split(body)
	if len(spans) == 0 {
		return body
	}
	rs := []rune(body)
	var b strings.Builder
	last := 0
	for _, s := range spans {
		if s.Start < last || s.End > len(rs) {
			continue
		}
		b.WriteString(string(rs[last:s.Start]))
		b.WriteString(" ")
		last = s.End
	}
	if last <= len(rs) {
		b.WriteString(string(rs[last:]))
	}
	return b.String()
}

// split cuts a string into its words, on anything that is not a letter or a
// digit.
func split(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// wordsIn reduces a body to the stems in it.
func wordsIn(body string) words {
	w := words{set: map[string]bool{}}
	for _, raw := range split(strings.ToLower(body)) {
		s := stem(raw)
		if w.set[s] {
			continue
		}
		w.set[s] = true
		w.list = append(w.list, s)
	}
	return w
}

// endings are the Russian inflections this strips, longest first.
//
// This is not a morphological analyser and it is not trying to be. It is the
// set of noun and adjective endings that carry the cases a technical term
// actually turns up in, and it is applied only when what is left is still long
// enough to be a stem. Getting this wrong in the generous direction costs an
// unnecessary glossary row; a real stemmer costs a dependency and a class of
// bugs that are hard to see from the outside.
var endings = []string{
	"ированием", "ированию", "ирование",
	"ами", "ями", "ого", "его", "ому", "ему", "ыми", "ими", "ией", "иях", "иям",
	"ая", "яя", "ое", "ее", "ые", "ие", "ый", "ий", "ой", "ей", "ом", "ем",
	"ым", "им", "ых", "их", "ую", "юю", "ою", "ею", "ах", "ях", "ов", "ев",
	"ам", "ям", "ии", "ия", "ию", "ье", "ья", "ью",
	"а", "я", "о", "е", "у", "ю", "ы", "и", "й", "ь",
}

// minStem is how much has to survive for the result to be a stem rather than a
// fragment. Below this the endings start eating short words whole: «ток» would
// lose its к and collide with «то».
const minStem = 4

// stem cuts the inflection off a Russian word.
func stem(word string) string {
	runes := []rune(word)
	for _, e := range endings {
		er := []rune(e)
		if len(runes)-len(er) < minStem {
			continue
		}
		if strings.HasSuffix(word, e) {
			return string(runes[:len(runes)-len(er)])
		}
	}
	return word
}
