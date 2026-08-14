package rubric

import (
	"strings"
	"unicode"
)

// Slug turns a printed banner into a name that can be a directory, a URL, a tag
// and a key in a YAML file.
//
// Transliteration and not translation. The banner is Russian and the slug has
// to survive a filesystem, a URL and a person reading a diff, so it is the
// sounds of the Russian written in Latin letters. Translating it would put a
// choice in the middle of an identifier, and the same banner would come out
// differently on two different days.
//
// The table is the standard the magazine's own site uses, which is BGN/PCGN
// with the soft signs dropped rather than the scholarly one with apostrophes,
// because an apostrophe in a filename is a fight nobody needs.
var letters = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e",
	'ж': "zh", 'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m",
	'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u",
	'ф': "f", 'х': "h", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch",
	'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya",
}

// Slug is the file name form of a printed rubric banner: transliterated to
// Latin, lower case, with everything else turned into a single dash.
func Slug(printed string) string {
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(printed)) {
		switch {
		case unicode.IsDigit(r) || (r < unicode.MaxASCII && unicode.IsLetter(r)):
			out.WriteRune(r)
			dash = false
		case letters[r] != "":
			out.WriteString(letters[r])
			dash = false
		case r == 'ъ' || r == 'ь':
			// Dropped, and dropped without a separator: тетрадь is tetrad and
			// not tetrad-.
		default:
			// Everything else is a separator, and the guillemets the magazine
			// puts round its own name are the common case.
			if !dash && out.Len() > 0 {
				out.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}
