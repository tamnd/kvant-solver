package answerguard

import (
	"fmt"
	"strings"
	"unicode"
)

// The prompt is in English and the page is in Russian, and a model asked in one
// language about a picture in another sometimes answers in the language it was
// asked in. What comes back then is not a bad transcription, it is a
// translation: fluent, plausible, the right length, and about the right page.
// Every rule in this package other than this one passes it.
//
// It is worth its own check because it is the one failure that survives review.
// A refusal is obvious to anyone who opens the file. An English paragraph in
// content/ru is obvious too, but only if somebody opens that particular file,
// and there are 34000 of them.

// MinProse is how many letters of prose there have to be before the language of
// a page is worth an opinion. A page that is one large figure and a caption has
// almost no prose on it, and calling that page English because its four letters
// are a formula label would reject a page that is fine.
const MinProse = 100

// MinCyrillic is the share of the prose letters that have to be Russian.
//
// It is low on purpose. A real page of this magazine runs above 90 percent, and
// the ones that do not are the pages that quote a foreign name or an English
// abstract, which are real pages and must pass. Anything under a fifth is not a
// page with quotations on it, it is a page in the wrong language.
const MinCyrillic = 0.2

// Russian reports the answer being in the wrong language, and nothing else. It
// is separate from Check so a caller reading a page that really is in English,
// which the back cover sometimes is, can leave it off.
func Russian(text string) (Leak, bool) {
	letters, cyrillic := 0, 0
	for _, r := range Prose(text) {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Cyrillic, r) {
			cyrillic++
		}
	}
	if letters < MinProse {
		return Leak{}, false
	}
	share := float64(cyrillic) / float64(letters)
	if share >= MinCyrillic {
		return Leak{}, false
	}
	return Leak{
		Kind: "language",
		Detail: fmt.Sprintf("%.0f percent of the %d prose letters are Russian, want at least %.0f percent",
			share*100, letters, MinCyrillic*100),
	}, true
}

// Prose is the text with the mathematics taken out, which is what the language
// question is about. A formula is written in Latin letters on a Russian page and
// counting those would make a page of algebra look like a translation.
//
// The math is found by the delimiters rather than parsed, and an unbalanced one
// takes the rest of the text with it. That is the safe way round here: an
// unbalanced delimiter is already a rejection under its own rule, and a
// language complaint on top of it says nothing new.
func Prose(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' {
			i++ // an escaped dollar is a dollar sign and not a delimiter
			continue
		}
		if runes[i] != '$' {
			b.WriteRune(runes[i])
			continue
		}
		closer := "$"
		if i+1 < len(runes) && runes[i+1] == '$' {
			closer = "$$"
			i++
		}
		rest := string(runes[i+1:])
		end := strings.Index(rest, closer)
		if end < 0 {
			break
		}
		i += len([]rune(rest[:end+len(closer)])) + 1
	}
	return b.String()
}
