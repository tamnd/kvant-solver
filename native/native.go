// Package native reads a born digital issue out of its own text layer.
//
// From 2007 the mirror's issue PDFs are set files rather than scans, and their
// text is the publisher's own typesetting: exact, free, and available in the
// time it takes to run pdftotext. That is the whole of the case for this
// package. A page it can read is a page no model has to be paid to read and no
// person has to check afterwards.
//
// The case has a limit, and the limit is mathematics. A text layer holds
// characters and their positions and nothing else, so a fraction arrives as a
// numerator on one line and a denominator on the next with the rule between
// them missing, a radical loses its sign, and an integral loses its limits.
// Nothing in the file says any of that happened. The rule this package is built
// on is therefore not "read what can be read": it is to reconstruct what the
// positions support, and to hand the page to the vision lane the moment the
// page shows a sign of holding mathematics that the positions cannot support.
// A page transcribed with a formula quietly missing from the middle of it is
// worse than a page that cost a model call, because the second one is right and
// the first one cannot be told apart from it downstream.
package native

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/pdfsrc"
)

// How a raised or lowered word is told from one on the line.
//
// Both tests have to pass. A subscript is set smaller and it sits lower, and
// either fact on its own is met by ordinary text: a comma sits low and a word
// in a footnote is small.
const (
	// ScriptSize is how much smaller than a word of running text a script is.
	ScriptSize = 0.85
	// SuperRise is how far above the line a superscript sits, as a share of the
	// height of a word of running text.
	SuperRise = 0.2
	// SubDrop is how far below the line a subscript sits. It is the smaller of
	// the two because a subscript hangs by its descender and a superscript is
	// lifted by its whole body.
	SubDrop = 0.06
)

// Page is one reconstructed page.
type Page struct {
	// Folio is the number printed on the page, and 0 for a page that prints
	// none. It is read off the page itself rather than taken from the manifest,
	// because it is the one thing on the page that can prove the page is the
	// one it was taken for.
	Folio int

	// Body is the transcription, in the same shape the vision lane writes:
	// a folio line, a column mark where the reading crosses the gutter, and
	// mathematics as LaTeX between dollars.
	Body string

	// Words is every word on the page and Math is how many of them came out
	// inside a formula. Scripts is how many raised or lowered words were put
	// back where they belong, which is the work this package exists to do.
	Words   int
	Math    int
	Scripts int

	// Stacked is how many lines were set closer to the line above them than the
	// paragraph they sit in allows.
	//
	// That is what a fraction looks like from underneath. The text layer holds
	// the numerator and the denominator as two lines half a line apart and says
	// nothing at all about the rule between them, so the characters arrive and
	// the division does not. The same measurement catches a radical, an
	// integral with its limits, a matrix, and a figure caption set into the
	// margin beside the text, which are the other four ways a page stops being
	// a single flow of lines. One of them is enough to send the page to a model.
	Stacked int

	// Soup is how many blocks came out as loose letters rather than words.
	//
	// The magazine sets its banners vertically up the outer margin and its
	// department headings in tracked display faces, and a text layer has no way
	// to say either. What arrives is every letter as its own word in whatever
	// order the glyphs were drawn in, which reads as КВ Н T К 2 « 0 1 К and is
	// three words of the magazine's name interleaved. Nothing can put that back
	// from the positions alone.
	Soup int

	// Columns is how many columns the reading crossed, which is 2 for an
	// ordinary page of this magazine.
	Columns int
}

// MathShare is how much of the page is mathematics.
func (p Page) MathShare() float64 {
	if p.Words == 0 {
		return 0
	}
	return float64(p.Math) / float64(p.Words)
}

// Marks in the page body, which are the vision lane's and are kept identical so
// that everything downstream reads a native page and a read page the same way.
const (
	ColumnMark = "⟦column⟧"
	FolioNone  = "⟦folio none⟧"
	FigureMark = "⟦figure⟧"
)

// FolioMark is the line that carries the printed number.
func FolioMark(n int) string { return fmt.Sprintf("⟦folio %d⟧", n) }

// token is one word on its way to becoming text.
type token struct {
	raw string
	sup string
	sub string
	// core is a word that is mathematics whatever its neighbours are: a Greek
	// letter, an operator, a word carrying a script.
	core bool
	// join is a word that belongs to a formula when it is next to one and is
	// ordinary text otherwise: a bare number, a single Latin letter, a bracket.
	join bool
}

// Read reconstructs one page of a file.
func Read(page pdfsrc.LayoutPage) Page {
	out := Page{}
	height := page.BodyHeight()
	folio, folioBlocks := readFolio(page)
	out.Folio = folio

	var body []string
	if folio > 0 {
		body = append(body, FolioMark(folio))
	} else {
		body = append(body, FolioNone)
	}

	mid := page.Width / 2
	column := 1
	// cap is the drop capital of an article, which is set in its own block
	// three lines deep and belongs to the first word of the next one.
	cap := ""
	open := false
	for i, block := range page.Blocks {
		if folioBlocks[i] {
			continue
		}
		if letter, ok := dropCap(block, height); ok {
			cap = letter
			continue
		}
		text, stats := readBlock(block, height)
		// The furniture is dropped before it is counted. It is set tight and in
		// six point, so counting it would put a stacked line and a handful of
		// numbers into the arithmetic the page is judged on.
		if text == "" || prePress.MatchString(text) {
			continue
		}
		out.Words += stats.words
		out.Math += stats.math
		out.Scripts += stats.scripts
		out.Stacked += stats.stacked
		out.Soup += stats.soup
		// The running head and the masthead are on the paper and they are kept,
		// and they are not part of the reading. A head that joined the paragraph
		// under it would put the name of the rubric into the middle of the first
		// sentence of the article, and one at the top of the right hand side
		// would move the reading into the second column before the first one had
		// been read at all.
		if edge(block, page) {
			body = append(body, text)
			open = false
			continue
		}
		if cap != "" {
			text, cap = cap+text, ""
		}
		crossed := column == 1 && block.XMin >= mid
		if crossed {
			column = 2
		}
		// A caption is not part of the sentence it interrupts, so it never joins
		// the block before it and it never leaves one open behind it.
		if figureLabel.MatchString(text) {
			if crossed {
				body = append(body, ColumnMark)
			}
			body = append(body, FigureMark, text)
			open = false
			continue
		}
		// A sentence that runs from one block into the next is one paragraph
		// that the drop capital or the picture beside it split in two. Joining
		// them where the words say so keeps the prose readable, and the column
		// mark still goes in, because the reading did cross the gutter whatever
		// the sentence was doing at the time.
		switch {
		case crossed:
			body = append(body, ColumnMark, text)
		case open && startsLower(text):
			body[len(body)-1] += " " + text
		default:
			body = append(body, text)
		}
		open = stats.open
	}
	out.Columns = column
	out.Body = strings.Join(body, "\n\n") + "\n"
	return out
}

// EdgeBand is how much of the head and the foot of a page is furniture rather
// than text: the running head, the masthead and the printed number.
const EdgeBand = 0.12

// edge reports whether a block sits in that band.
func edge(block pdfsrc.Block, page pdfsrc.LayoutPage) bool {
	if page.Height <= 0 {
		return false
	}
	return block.YMax < EdgeBand*page.Height || block.YMin > (1-EdgeBand)*page.Height
}

// dropCap reports the letter of a block that is one capital set several lines
// deep, which is how this magazine opens an article.
func dropCap(block pdfsrc.Block, height float64) (string, bool) {
	if height <= 0 || len(block.Lines) != 1 || len(block.Lines[0].Words) != 1 {
		return "", false
	}
	word := block.Lines[0].Words[0]
	if len([]rune(word.Text)) != 1 || !unicode.IsUpper([]rune(word.Text)[0]) {
		return "", false
	}
	if word.Height() < 1.8*height {
		return "", false
	}
	return word.Text, true
}

func startsLower(s string) bool {
	for _, r := range s {
		return unicode.IsLower(r)
	}
	return false
}

// endsOpen reports whether a block stops in the middle of a sentence.
func endsOpen(s string) bool {
	trimmed := strings.TrimRight(s, " ")
	if trimmed == "" {
		return false
	}
	last := []rune(trimmed)[len([]rune(trimmed))-1]
	return !strings.ContainsRune(".!?:;»\"…", last)
}

// counts is what one block contributed to the page's arithmetic.
type counts struct {
	words   int
	math    int
	scripts int
	stacked int
	soup    int
	// open is the block stopping in the middle of a sentence.
	open bool
}

// StackedLeading is how close two lines have to be, against the leading of the
// paragraph they are in, before they are taken to be stacked rather than set
// one after the other.
//
// Two thirds is well under anything a typesetter does to running text and well
// over the nothing that separates a numerator from its denominator.
const StackedLeading = 0.7

// readBlock turns one paragraph into text.
func readBlock(block pdfsrc.Block, height float64) (string, counts) {
	var (
		stats  counts
		tokens []token
	)
	stats.stacked = stacked(block)
	if soup(block) {
		stats.soup = 1
	}

	for _, line := range block.Lines {
		base := line.Base()
		for _, w := range line.Words {
			text, ok := masthead(w.Text)
			if !ok {
				continue
			}
			w.Text = text
			stats.words++
			switch scriptOf(w, base, height) {
			case super:
				if n := len(tokens); n > 0 && tokens[n-1].sup == "" {
					tokens[n-1].sup = w.Text
					tokens[n-1].core = true
					stats.scripts++
					continue
				}
			case sub:
				if n := len(tokens); n > 0 && tokens[n-1].sub == "" {
					tokens[n-1].sub = w.Text
					tokens[n-1].core = true
					stats.scripts++
					continue
				}
			}
			tokens = append(tokens, classify(w.Text))
		}
	}
	// A word broken across a line ending in a hyphen is one word. The magazine
	// hyphenates heavily in two columns, and leaving the halves apart puts a
	// good share of the vocabulary of every page out of reach of a search.
	tokens = dehyphenate(tokens)
	text, math := render(tokens)
	stats.math = math
	stats.open = endsOpen(text)
	return text, stats
}

// stacked counts the lines of a block that sit closer to the line above them
// than the block's own leading allows.
//
// The leading is taken from the block itself and not from the page, because a
// footnote is set tighter than the body and a verse epigraph looser, and a
// measurement made on the page would call one of them a fraction. The median
// is what a block does normally, so a block that is nothing but a stacked
// formula measures itself as normal and is caught by the page's math share
// instead.
func stacked(block pdfsrc.Block) int {
	if len(block.Lines) < 3 {
		return 0
	}
	gaps := make([]float64, 0, len(block.Lines)-1)
	for i := 1; i < len(block.Lines); i++ {
		gaps = append(gaps, block.Lines[i].Base()-block.Lines[i-1].Base())
	}
	leading := slices.Clone(gaps)
	slices.Sort(leading)
	normal := leading[len(leading)/2]
	if normal <= 0 {
		return 0
	}
	n := 0
	for _, gap := range gaps {
		if gap < StackedLeading*normal {
			n++
		}
	}
	return n
}

// SoupShare is how many of a block's words have to be single letters before the
// block is taken to be something other than writing.
//
// Russian has a dozen one letter words and they are among its commonest, so a
// line of prose is a few percent single letters and a block of loose ones is
// nearly all of them. Half is a long way from either, and half itself counts:
// the masthead on the задачник page of 2010 issue 1 is twelve loose letters in
// twenty four words and it is not writing.
const SoupShare = 0.5

// SoupWords is the shortest block the test runs on. Under this the share says
// nothing: "И в чем же тут дело" is six words and four of them are one letter.
const SoupWords = 8

// soup reports whether a block came out as loose single letters.
//
// It is two different things that measure the same: a banner set rotated and
// tracked wide, which the layer holds letter by letter with nothing to say they
// are a word, and a run of algebra with its operators and its arrows set as
// separate glyphs, which is the same wreck with the same cause. Neither is
// worth telling apart here, because both mean the page goes to a model.
func soup(block pdfsrc.Block) bool {
	words, letters := 0, 0
	for _, line := range block.Lines {
		for _, w := range line.Words {
			words++
			if len([]rune(w.Text)) == 1 && unicode.IsLetter([]rune(w.Text)[0]) {
				letters++
			}
		}
	}
	return words >= SoupWords && float64(letters) >= SoupShare*float64(words)
}

// script says whether a word sits on the line, above it or below it.
type script int

const (
	onLine script = iota
	super
	sub
)

func scriptOf(w pdfsrc.Word, base, height float64) script {
	if height <= 0 || w.Height() >= ScriptSize*height {
		return onLine
	}
	switch {
	case w.Base() < base-SuperRise*height:
		return super
	case w.Base() > base+SubDrop*height:
		return sub
	}
	return onLine
}

// classify decides what a word is before it has any neighbours.
func classify(raw string) token {
	t := token{raw: raw}
	letters, cyrillic, mathRunes := 0, 0, 0
	for _, r := range raw {
		if unicode.IsLetter(r) {
			letters++
			if unicode.Is(unicode.Cyrillic, r) {
				cyrillic++
			}
		}
		if isTeXMath(r) {
			mathRunes++
		}
	}
	switch {
	case cyrillic > 0:
		// A Russian word is prose even when it stands in the middle of a
		// formula, and a formula that swallowed one would not parse.
		return t
	case mathRunes > 0:
		t.core = true
	case letters > 0 && letters <= 2 && len(raw) <= 2:
		// A letter on its own is a variable. Two is for the ones the magazine
		// sets as a pair, and anything longer is a word in another alphabet
		// rather than a name for a quantity.
		t.join = true
	case isNumber(raw):
		t.join = true
	case raw != "" && strings.Trim(raw, "=+-<>()[]{}/|,.:;·") == "":
		t.join = true
	}
	return t
}

// dehyphenate joins the words the typesetter broke across a line.
//
// A hyphen inside a line is the word's own, so this can only be decided over
// the block and not over the line: what says the hyphen was the typesetter's
// is that the halves on either side of it are Russian and the second one is
// lower case, which the second half of a broken word is and the second half of
// a compound noun in a title is not.
func dehyphenate(tokens []token) []token {
	out := make([]token, 0, len(tokens))
	for _, t := range tokens {
		n := len(out)
		if n == 0 {
			out = append(out, t)
			continue
		}
		last := out[n-1]
		if !strings.HasSuffix(last.raw, "-") || last.core || last.join || last.sup != "" || last.sub != "" {
			out = append(out, t)
			continue
		}
		if !hasCyrillic(last.raw) || !hasCyrillic(t.raw) || !startsLower(t.raw) {
			out = append(out, t)
			continue
		}
		out[n-1] = token{raw: strings.TrimSuffix(last.raw, "-") + t.raw, sup: t.sup, sub: t.sub}
	}
	return out
}

// masthead rewrites the running head this mirror's files carry, and says
// whether the word survives at all.
//
// The magazine's name is set in its own logo face on every page, and that face
// does not map to Unicode here. The word arrives as КВАНT, whose last letter is
// a Latin T inside a Russian word, and after it comes a lone dollar for the
// flourish on the logo. Left alone the first is a word in two alphabets on
// every page of every issue, which is what the script rule rejects a page for,
// and the second is a dollar loose in the text, which the mathematics on the
// page can then never balance around.
func masthead(word string) (string, bool) {
	switch word {
	case "$":
		return "", false
	case "КВАНT":
		return "КВАНТ", true
	}
	return word, true
}

// prePress reports whether a block is the typesetter's furniture rather than
// the magazine's.
//
// These files were made from the pre-press pages, and those carry the name of
// the layout file and the time it was last saved outside the trim. Neither is
// on the paper, so a corpus that kept them would have several thousand pages
// carrying a string that no reader of the magazine has ever seen.
var prePress = regexp.MustCompile(`^(\d+[-–]\d+\.p65|\d\d\.\d\d\.\d\d,\s*\d\d:\d\d)$`)

// figureLabel is a caption that is only the figure's number, which is how the
// magazine labels a drawing it discusses in the text.
var figureLabel = regexp.MustCompile(`^(Рис|рис|Табл)\.\s*\d+`)

// render writes the tokens of one block out, wrapping every run of them that is
// mathematics in dollars, and says how many words ended up inside one.
//
// The run and not the word is the unit, because $x$ $=$ $1$ is three formulas
// where the page has one, and because a KaTeX check of the pieces would pass
// where a check of the whole would not.
func render(tokens []token) (string, int) {
	var (
		out  []string
		math int
	)
	for i := 0; i < len(tokens); {
		// A token opens a formula if it is mathematics on its own, or if it is
		// one of the signs that only mean something between two things that are.
		opens := tokens[i].core || (tokens[i].join && runHasCore(tokens, i))
		if !opens {
			out = append(out, plain(tokens[i]))
			i++
			continue
		}
		j := i
		for j < len(tokens) && (tokens[j].core || tokens[j].join) {
			j++
		}
		// A formula does not end on a comma or a full stop that belongs to the
		// sentence around it.
		for j > i+1 && isTrailingPunctuation(tokens[j-1]) {
			j--
		}
		out = append(out, "$"+formula(tokens[i:j])+"$")
		math += j - i
		i = j
	}
	return join(out), math
}

// join puts the words of a block back into a line of prose.
//
// The punctuation that closes a sentence is a word of its own in the text
// layer, and it is one here too once a formula has handed its full stop back to
// the sentence it was standing in. Joining on a plain space would leave that
// stop floating a space clear of the formula before it.
func join(words []string) string {
	var b strings.Builder
	for i, w := range words {
		if i > 0 && !isPunctuation(w) {
			b.WriteByte(' ')
		}
		b.WriteString(w)
	}
	return b.String()
}

func isPunctuation(w string) bool {
	return w != "" && strings.Trim(w, ",.;:!?") == ""
}

// runHasCore reports whether the run of joinable words starting here holds
// anything that is mathematics on its own account. Without it a page number, a
// date and a footnote number would each become a formula.
func runHasCore(tokens []token, i int) bool {
	for ; i < len(tokens); i++ {
		if tokens[i].core {
			return true
		}
		if !tokens[i].join {
			return false
		}
	}
	return false
}

func isTrailingPunctuation(t token) bool {
	return !t.core && (t.raw == "," || t.raw == "." || t.raw == ";" || t.raw == ":")
}

// formula writes a run of tokens as LaTeX.
func formula(tokens []token) string {
	var b strings.Builder
	for i, t := range tokens {
		part := tex(t.raw)
		if t.sub != "" {
			part += "_{" + tex(t.sub) + "}"
		}
		if t.sup != "" {
			part += "^{" + tex(t.sup) + "}"
		}
		// The words were separate on the page but the punctuation between them
		// was not, and a corpus of formulas written as "AB = CD , EF" reads as
		// machine output even though it renders correctly.
		if i > 0 && !strings.HasPrefix(part, ",") && !strings.HasPrefix(part, ".") {
			b.WriteByte(' ')
		}
		b.WriteString(part)
	}
	return b.String()
}

// plain writes a word that is not mathematics.
func plain(t token) string {
	// A dollar in the running text is the logo on the masthead, which this
	// mirror's font gives as one, and an odd number of them would leave the
	// page looking like a formula that was never closed.
	out := strings.ReplaceAll(t.raw, "$", `\$`)
	if t.sup != "" {
		// A raised number after a Russian word is a footnote mark, and it is
		// kept where it was rather than dropped into the line.
		out += "$^{" + tex(t.sup) + "}$"
	}
	if t.sub != "" {
		out += "$_{" + tex(t.sub) + "}$"
	}
	return out
}

// MaxFolio is the largest number that can be a page number.
//
// The magazine has never run past 80 pages, and the number a text layer offers
// that is larger than this is the year off the masthead or the postcode off the
// colophon. Both are set exactly where a folio is set and are the two ways this
// went wrong before the bound was here.
const MaxFolio = 400

// readFolio finds the printed page number and says which blocks carry it.
//
// It is a block on its own holding one number at the head or the foot of the
// page, which is where this magazine prints it. A number anywhere else is an
// equation number and is not evidence of anything. Every block holding that
// same number comes back and not only the first, because these files carry the
// folio twice, once on the page and once in the trim below it, and the second
// one would otherwise land in the middle of the transcription.
func readFolio(page pdfsrc.LayoutPage) (folio int, blocks map[int]bool) {
	const edge = 0.12
	blocks = make(map[int]bool)
	for i, b := range page.Blocks {
		if len(b.Lines) != 1 || len(b.Lines[0].Words) != 1 {
			continue
		}
		n, err := strconv.Atoi(b.Lines[0].Words[0].Text)
		if err != nil || n <= 0 || n > MaxFolio {
			continue
		}
		if page.Height > 0 {
			top := b.YMin < edge*page.Height
			bottom := b.YMax > (1-edge)*page.Height
			if !top && !bottom {
				continue
			}
		}
		if folio == 0 {
			folio = n
		}
		if n == folio {
			blocks[i] = true
		}
	}
	return folio, blocks
}

func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	digits := false
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits = true
		case r == ',' || r == '.' || r == '-' || r == '−':
		default:
			return false
		}
	}
	return digits
}
