// Package quantum maps the English Quantum back onto the Kvant it came out of.
//
// Quantum ran from 1990 to 2001 and a large part of it was Kvant in
// translation, but neither magazine ever printed the correspondence. The
// English index gives a title, a byline and an issue, and the Russian table of
// contents gives the same three things, and the only thing the two have in
// common is the people. So the match is made on the byline: the surnames are
// transliterated onto a common skeleton and a row is only claimed when exactly
// one Russian article has that set of authors.
//
// Nothing here calls a model and nothing here reads a page. That is deliberate.
// The mapping is a reference for checking terminology and register, and a
// reference that was guessed at is worse than no reference, because a wrong row
// argues for a wrong word with the authority of the published English.
package quantum

import (
	"strings"
	"unicode"
)

// latin is the scheme Quantum's translators mostly used, which is BGN/PCGN in
// its ordinary newspaper form: Мищенко comes out Mishchenko and Ширшов comes
// out Shirshov.
var latin = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// Translit writes a Russian name in Latin letters.
func Translit(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if to, ok := latin[r]; ok {
			b.WriteString(to)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// folds are the differences between one transliteration scheme and the next,
// applied in order so that the longer digraph wins over the shorter one inside
// it. Every rule here is a place two spellings of the same surname disagree and
// no rule here distinguishes two surnames that are actually different, which is
// the whole test of whether a rule belongs in this list.
var folds = [...][2]string{
	{"shch", "s"}, {"sch", "s"}, {"sh", "s"}, {"zh", "z"},
	{"kh", "h"}, {"ch", "c"}, {"ts", "c"}, {"tz", "c"},
	{"yo", "e"}, {"io", "e"}, {"ye", "e"}, {"ie", "e"},
	{"yu", "u"}, {"iu", "u"}, {"ya", "a"}, {"ia", "a"},
	{"j", "i"}, {"y", "i"}, {"w", "v"},
}

// Fold reduces a Latin surname to the skeleton that two transliterations of the
// same Russian name have in common.
//
// It is lossy on purpose. Соловьёв is Solovyov, Solovyev, Soloviev and Solovev
// depending on who was typing, and a mapping that only matched one of them
// would miss most of the run. What it does not do is stretch far enough to pull
// two different surnames together: the rules only rewrite spellings of the same
// sound, and the doubled letters it collapses are the ones English doubles and
// Russian does not.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	for _, f := range folds {
		out = strings.ReplaceAll(out, f[0], f[1])
	}
	return squeeze(out)
}

// squeeze collapses a run of one vowel to a single vowel.
//
// This is here for the -ский ending, which is the commonest ending a Russian
// surname has. It transliterates as iy and the rules above turn that into ii,
// while the English spelling is a plain y and comes out as one i, so without
// this every Smorodinsky and Sosinsky in the magazine is missed.
//
// Only vowels, because a doubled consonant is usually really there. Саввин and
// Савин are two surnames and collapsing the pair would merge them.
func squeeze(s string) string {
	var b strings.Builder
	var last rune
	for _, r := range s {
		if r != last || !strings.ContainsRune("aeiou", r) {
			b.WriteRune(r)
		}
		last = r
	}
	return b.String()
}

// initials is what a Russian given name initial can turn into in English.
//
// This is a separate table from the transliteration because an initial is not
// a shortened word, it is a letter somebody chose. Юрий is Yuri and Яков is
// Yakov, so Ю and Я begin with a Y in English and with a U and an A under any
// mechanical scheme, and Евгений is written Evgeny by one translator and
// Yevgeny by the next.
var initials = map[rune]string{
	'а': "a", 'б': "b", 'в': "vw", 'г': "g", 'д': "d", 'е': "ey", 'ё': "ey",
	'ж': "zj", 'з': "z", 'и': "i", 'й': "yi", 'к': "kc", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "kh", 'ц': "tc", 'ч': "c", 'ш': "s", 'щ': "s",
	'ы': "y", 'э': "e", 'ю': "yui", 'я': "yai",
}

// Initial is the set of Latin letters a name can begin with. A Latin name gives
// back the letter it starts with and nothing else.
func Initial(name string) string {
	for _, r := range strings.ToLower(name) {
		if set, ok := initials[r]; ok {
			return set
		}
		if r >= 'a' && r <= 'z' {
			return string(r)
		}
		if !unicode.IsLetter(r) {
			continue
		}
		return ""
	}
	return ""
}

// SameInitial reports whether two initials can be the same person's. An empty
// one on either side is not a disagreement: plenty of Quantum bylines are a
// bare surname, and refusing those would throw away the rows most likely to be
// a translation.
func SameInitial(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return strings.ContainsAny(a, b)
}
