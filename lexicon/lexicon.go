// Package lexicon tells a Russian word the model spelled in two alphabets from
// a Russian word it failed to read.
//
// Rule 8 counts words that mix Cyrillic with something else, and for a long
// time it treated all of them the same. Reading the failures of 1976 through
// 1981 says they are two different things. однako and перpendикулярно and
// tetraэдра are «однако» and «перпендикулярно» and «тетраэдра» written with
// Latin letters in the middle: the model recognised the word and reached for
// the international spelling of it, which is a habit and not a misreading. Оlimпилад
// and MEMORIALНОГО and непrivibuous are not words in any alphabet and the page
// they came off was genuinely read wrong.
//
// The difference matters because the first kind is common and harmless and the
// second kind is the thing the rule exists to catch. Counting them together
// meant a page with seven honest code switches died next to a page that came
// back as noise, and the second lane then cost a hundred times as much per page
// to read the first kind again.
//
// A word is separated by asking whether it can be turned back into Russian. Two
// ways are tried, and both have to land on a form the corpus already uses, so
// this invents nothing: it recognises. Anything it cannot place stays counted,
// which is the conservative direction. Nothing here rewrites a page. The corpus
// keeps what the model wrote and this only decides whether that page is worth
// keeping, because a page the rules reject should be read again rather than
// patched.
package lexicon

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// File is where the lexicon lives under the corpus manifests.
//
// In the corpus and not in this repository, because it is made out of the
// corpus. A given commit of the text has a given lexicon and rule 8 gives the
// same answer for that commit today and next year, which is what kvant audit
// needs. Building it from whatever happens to be on disk at read time would
// make a page pass or fail depending on how much of the archive had been read
// when it went through, and two runs of the audit would disagree.
const File = "lexicon.txt"

// Lexicon is the set of Russian word forms the corpus uses.
type Lexicon struct {
	forms map[string]bool
}

// New builds a lexicon from word forms.
func New(forms []string) *Lexicon {
	l := &Lexicon{forms: make(map[string]bool, len(forms))}
	for _, form := range forms {
		l.forms[strings.ToLower(form)] = true
	}
	return l
}

// Len is how many forms the lexicon holds.
func (l *Lexicon) Len() int { return len(l.forms) }

// Has says whether a form is one the corpus uses.
func (l *Lexicon) Has(word string) bool { return l.forms[strings.ToLower(word)] }

// MinLen is the shortest word this will try to place.
//
// Below it the answer is never trustworthy. Фd and Крg are chess moves, which
// rule 8 already excludes by pattern, but Фh and Крd survive that pattern when
// the file or rank did not come through, and a three letter fragment is close
// enough to a dozen real words that a unique hit means nothing. Measured on
// 1975 and 1976 the short ones are 83 of 1063 mixed words and not one of them
// is a word.
const MinLen = 4

// Resolves says whether a mixed alphabet word is a Russian word the model spelled
// in two alphabets.
//
// False is the answer that costs nothing: it leaves the word counted and the
// page is judged the way it always was. True is the claim, so it is only made
// when exactly one reading of the word is a form the corpus already uses.
func (l *Lexicon) Resolves(word string) bool {
	if len([]rune(word)) < MinLen {
		return false
	}
	// Some of the alphabets look alike in print. Одеcca is «Одесса» with two
	// Latin c, and swapping the glyphs that a Russian face draws identically is
	// the whole of that repair. It is tried first because it is exact: there is
	// only one reading and either it is a word or it is not.
	if same, ok := homoglyphs(word); ok && l.Has(same) {
		return true
	}
	// Otherwise the Latin is a romanisation and one letter can stand for
	// several. Every reading is generated and exactly one of them has to be a
	// word, because two means the word is ambiguous and this cannot tell which
	// was printed.
	hit := ""
	for _, cand := range readings(word) {
		if !l.Has(cand) {
			continue
		}
		if hit != "" && hit != cand {
			return false
		}
		hit = cand
	}
	return hit != ""
}

// homoglyph maps a Latin letter to the Cyrillic letter a Russian face draws the
// same way. Only the pairs that are actually identical, because the point is
// that a reader cannot tell them apart either.
var homoglyph = map[rune]rune{
	'a': 'а', 'c': 'с', 'e': 'е', 'o': 'о', 'p': 'р', 'x': 'х', 'y': 'у',
	'A': 'А', 'B': 'В', 'C': 'С', 'E': 'Е', 'H': 'Н', 'K': 'К', 'M': 'М',
	'O': 'О', 'P': 'Р', 'T': 'Т', 'X': 'Х', 'Y': 'У',
}

// homoglyphs swaps the identical looking letters and reports whether what came
// out is Cyrillic throughout. A word still holding a b or a d was never a
// homoglyph problem.
func homoglyphs(word string) (string, bool) {
	var b strings.Builder
	for _, r := range word {
		if to, ok := homoglyph[r]; ok {
			b.WriteRune(to)
			continue
		}
		if !unicode.Is(unicode.Cyrillic, r) {
			return "", false
		}
		b.WriteRune(r)
	}
	return b.String(), true
}

// digraph is the Latin pairs that stand for one Russian letter. Longest first,
// and they are checked before the single letters so that sh is ш rather than с
// followed by х.
var digraph = map[string][]string{
	"sch": {"щ"}, "sh": {"ш"}, "ch": {"ч"}, "zh": {"ж"}, "ph": {"ф"},
	"th": {"т"}, "ck": {"к"}, "ts": {"ц"}, "yu": {"ю"}, "ya": {"я"},
	"kh": {"х"}, "ee": {"и"}, "oo": {"у"}, "ou": {"у"},
}

// letter is what one Latin letter can be. Several of them are genuinely
// ambiguous into Russian, c is к in kinematics and ц in centre and с in
// physico, and the ambiguity is kept rather than guessed at: every reading is
// tried and a word that produces two of them is refused.
var letter = map[rune][]string{
	'a': {"а"}, 'b': {"б", "в"}, 'c': {"к", "ц", "с"}, 'd': {"д"},
	'e': {"е", "э"}, 'f': {"ф"}, 'g': {"г"}, 'h': {"х", "г", ""},
	'i': {"и", "й"}, 'j': {"й", "ж"}, 'k': {"к"}, 'l': {"л"},
	'm': {"м"}, 'n': {"н"}, 'o': {"о"}, 'p': {"п"}, 'q': {"к"},
	'r': {"р"}, 's': {"с", "з"}, 't': {"т"}, 'u': {"у", "ю"},
	'v': {"в"}, 'w': {"в"}, 'x': {"кс"}, 'y': {"ы", "й", "у", "и"},
	'z': {"з"},
}

// maxReadings caps the fan out. A word with eight ambiguous letters in a row is
// not going to resolve to exactly one form and generating four thousand
// spellings of it to find that out is work for nothing.
const maxReadings = 4096

// readings is every way the Latin in a word could have been Russian.
//
// The Cyrillic already in the word is left alone. It is the part the model got
// right and it is what makes this tractable: перpendикулярно has one Latin run
// of nine letters in it and the twenty letters around it pin the answer.
func readings(word string) []string {
	out := []string{""}
	low := strings.ToLower(word)
	runes := []rune(low)
	for i := 0; i < len(runes); {
		var opts []string
		width := 1
		for _, n := range []int{3, 2} {
			if i+n > len(runes) {
				continue
			}
			if got, ok := digraph[string(runes[i:i+n])]; ok {
				opts, width = got, n
				break
			}
		}
		if opts == nil {
			if got, ok := letter[runes[i]]; ok {
				opts = got
			} else {
				// Cyrillic, or a letter from an alphabet this does not know. It
				// stands for itself, and if it is not Cyrillic the check at the
				// end throws the reading away.
				opts = []string{string(runes[i])}
			}
		}
		next := make([]string, 0, len(out)*len(opts))
		for _, prefix := range out {
			for _, opt := range opts {
				next = append(next, prefix+opt)
			}
		}
		if len(next) > maxReadings {
			return nil
		}
		out = next
		i += width
	}

	kept := out[:0]
	for _, cand := range out {
		if cyrillicOnly(cand) {
			kept = append(kept, cand)
		}
	}
	return kept
}

func cyrillicOnly(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if !unicode.Is(unicode.Cyrillic, r) {
			return false
		}
	}
	return true
}

// MinCount is how often a form has to appear before it is a word.
//
// The lexicon is built out of pages a model read, so some of what is in them is
// not Russian. Once is the signature of that: a form that appears a single time
// across four thousand pages is more often a misread than a rare word, and
// letting those in would mean the thing that decides whether a page was read
// correctly is partly made of pages that were not. Two costs recall, 42.1% of
// mixed words placed against 38.6%, and it halves the file and takes the
// one-offs out.
const MinCount = 2

// Collect counts the Russian words in a corpus.
//
// Only the words that are Cyrillic throughout. A mixed word is the thing being
// judged and letting it into the lexicon would let a page vouch for its own
// misreadings.
func Collect(root, lang string) (map[string]int, error) {
	paths, err := filepath.Glob(filepath.Join(root, "content", lang, "*", "*", "pages", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	count := map[string]int{}
	for _, path := range paths {
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for _, word := range Words(string(text)) {
			if cyrillicOnly(word) {
				count[strings.ToLower(word)]++
			}
		}
	}
	return count, nil
}

// Words splits text the way rule 8 splits it, so that the lexicon is counting
// the same things the rule is asking about.
func Words(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) })
}

// Forms is the lexicon as a sorted list, which is how it is written.
func Forms(count map[string]int, min int) []string {
	out := make([]string, 0, len(count))
	for form, n := range count {
		if n >= min {
			out = append(out, form)
		}
	}
	sort.Strings(out)
	return out
}

// Write saves a lexicon, one form to a line.
//
// A plain sorted list rather than YAML or the counts as well, because this file
// is committed and read in diffs. Adding a year to the corpus should show up as
// the words that year brought and nothing else, and carrying the frequencies
// would rewrite every line on every build.
func Write(path string, forms []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()

	w := bufio.NewWriter(temp)
	for _, form := range forms {
		if _, err := fmt.Fprintln(w, form); err != nil {
			_ = temp.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

// Load reads a lexicon off disk.
func Load(path string) (*Lexicon, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	l := &Lexicon{forms: map[string]bool{}}
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scan.Scan() {
		form := strings.TrimSpace(scan.Text())
		if form != "" {
			l.forms[strings.ToLower(form)] = true
		}
	}
	return l, scan.Err()
}

// Path is where the lexicon sits for a corpus.
func Path(root string) string { return filepath.Join(root, "manifests", File) }

// Open loads the lexicon of a corpus, and reports a missing one as nil rather
// than as an error.
//
// Missing is the ordinary state of a corpus nobody has built one for, and rule
// 8 without a lexicon is the rule as it was before there was one. A run should
// say so and carry on rather than refuse to read.
func Open(root string) (*Lexicon, error) {
	l, err := Load(Path(root))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return l, err
}
