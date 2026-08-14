// Package mathtex is where the mathematics of a body starts and stops, and what
// to do about a character that was left outside its TeX.
//
// It is a leaf package on purpose. Two things need it and they sit at opposite
// ends of the pipeline: extract, which writes the pages, and quality, whose M
// rules read them back and say whether the formulae survived. If each had its
// own idea of where a math span begins they would disagree about the same file,
// and an audit that disagrees with the tool that produced the corpus is worse
// than no audit. So the splitter is written once, here, under both of them.
package mathtex

import (
	"fmt"
	"regexp"
	"strings"
)

// A Span is one stretch of mathematics in a body, without its delimiters.
type Span struct {
	Text    string
	Display bool
	Line    int // the body line the opening delimiter sits on, counting from one

	// Start and End are where the text sits in the body, counted in runes
	// because Split walks it in runes. A rule needs only the text, but anything
	// that repairs the mathematics has to put it back where it came from, and
	// searching for the text again would find the wrong copy of a span that is
	// written twice on a line.
	Start, End int
}

// Split cuts a normalised body into its math spans.
//
// The rules are LaTeX's, which are not Markdown's: a backslash escapes the
// character after it, so \$ is a dollar sign and not a delimiter, two dollars
// open a display and one opens an inline span, and a display closes only on two.
// A page prints a price or a variable dollar rarely and it does happen,
// and getting them wrong is the difference between reporting that region and
// reporting the whole file as balanced.
//
// unclosed is the span left open at the end of the body, and nil when there is
// none. It carries the line the delimiter that opened it sits on, which is the
// line somebody has to go and look at; the end of the file is where the problem
// shows up and never where it is.
func Split(body string) (spans []Span, unclosed *Span) {
	const (
		none = iota
		inline
		display
	)
	state := none
	line := 1
	var open Span
	var start int
	rs := []rune(body)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\n':
			line++
			continue
		case '\\':
			// The escape takes the next character with it, whatever it is, and
			// a newline still has to be counted.
			if i+1 < len(rs) {
				if rs[i+1] == '\n' {
					line++
				}
				i++
			}
			continue
		case '$':
		default:
			continue
		}
		double := i+1 < len(rs) && rs[i+1] == '$'
		switch state {
		case none:
			open, start = Span{Display: double, Line: line}, i+1
			if double {
				state, start = display, i+2
				i++
			} else {
				state = inline
			}
			open.Start = start
		case inline:
			open.Text, open.End = string(rs[start:i]), i
			spans = append(spans, open)
			state = none
		case display:
			if !double {
				continue // a lone dollar inside a display is text
			}
			open.Text, open.End = string(rs[start:i]), i
			spans = append(spans, open)
			state = none
			i++
		}
	}
	if state != none {
		open.Text, open.End = string(rs[start:]), len(rs)
		return spans, &open
	}
	return spans, nil
}

// The repair is M03 read backwards. The rule says which characters are stranded
// out of their TeX and this puts them back, over the same split, so the two
// cannot disagree about where the mathematics is.
//
// A repair is not a rewrite. Every substitution here is one glyph for the TeX
// that prints that same glyph, so the corpus after it says exactly what it said
// before and compiles where it did not. Nothing here guesses at what the book
// meant, and the two characters where a guess would be needed are refused and
// handed back to whoever is running it. That is the difference between a repair
// and an invention, and it is why this is a table and not a model call.

// texOf is the glyph to TeX table, restricted to what M03 flags inside the
// mathematics: the Greek block and the increment sign.
//
// extract has its own and larger table, unicodeMath, and this is deliberately
// not that one. That table maps the glyphs the PDF fonts come out as and it
// runs while the run's font is still known. This one runs over Markdown with no
// font and no page behind it, so it holds only the characters somebody has
// actually seen stranded in this corpus. A table of substitutions nobody has
// checked against a printed page is a table that will be wrong somewhere and
// nobody will know which entry.
var texOf = map[rune]string{
	'α': `\alpha`, 'β': `\beta`, 'γ': `\gamma`, 'δ': `\delta`, 'ε': `\varepsilon`,
	'ζ': `\zeta`, 'η': `\eta`, 'θ': `\theta`, 'ϑ': `\vartheta`, 'ι': `\iota`,
	'κ': `\kappa`, 'λ': `\lambda`, 'μ': `\mu`, 'ν': `\nu`, 'ξ': `\xi`,
	'π': `\pi`, 'ρ': `\rho`, 'σ': `\sigma`, 'ς': `\varsigma`, 'τ': `\tau`,
	'υ': `\upsilon`, 'φ': `\varphi`, 'ϕ': `\varphi`, 'χ': `\chi`, 'ψ': `\psi`,
	'ω': `\omega`,
	'Γ': `\Gamma`, 'Δ': `\Delta`, 'Θ': `\Theta`, 'Λ': `\Lambda`, 'Ξ': `\Xi`,
	'Π': `\Pi`, 'Σ': `\Sigma`, 'Υ': `\Upsilon`, 'Φ': `\Phi`, 'Ψ': `\Psi`,
	'Ω': `\Omega`,

	// U+2206, the increment sign, which is not U+0394 and is what a text layer
	// gives for a capital delta often enough to be worth handling. On these
	// pages it is the delta of a small change, which physics writes on nearly
	// every page it has.
	'∆': `\Delta`,

	// The compatibility characters, which are the same letters again at
	// different code points and are what a census of a text layer turns up once
	// the Greek block is clear: the micro sign, U+00B5 rather than U+03BC,
	// which physics prints in front of half its units, and the ohm sign,
	// U+2126 rather than U+03A9, which it prints after the other half.
	// Neither is in the Greek block, so neither was caught by looking there,
	// and both print as the letter, so neither is visible by reading.
	// They are written as escapes and not as themselves, because µ and the
	// letter mu are the same shape in every font this source will be read in,
	// and a table where two entries look identical is a table somebody will
	// delete half of.
	'\u00b5': `\mu`,    // the micro sign, not the Greek mu above
	'\u2126': `\Omega`, // the ohm sign, not the Greek omega above

	// U+0131, the dotless i, which is what an accented i is set as. Three of
	// them, all \widetilde{ı} in § 16.
	'\u0131': `\imath`,
}

// ambiguous are the two capitals that are also operators. A capital sigma with
// a subscript under it is a sum and not the letter, a capital pi with one is a
// product, and neither difference survives being written as the letter.
//
// The corpus has seven stranded sigmas. Six are the letter, a set named Σ in
// § 11 and in the exercises of § 14, and one is the sum over σ in Γ in § 5, and
// the only thing in the Markdown that tells them apart is the subscript. So the
// shape decides: one of these followed by _ or ^ is refused and reported, and
// everything else is the letter. No pi is stranded anywhere in the corpus yet,
// and it is here because the next volume will have one.
var ambiguous = map[rune]string{
	'Σ': `\sum`,
	'Π': `\prod`,
}

// A Refusal is a character the repair would not touch, and why.
type Refusal struct {
	File string
	Line int
	Rune rune
	Why  string
	Span string
}

func (r Refusal) String() string {
	at := fmt.Sprintf("%s:%d", r.File, r.Line)
	if r.File == "" {
		at = fmt.Sprintf("line %d", r.Line)
	}
	what := fmt.Sprintf("%q ", r.Rune)
	if r.Rune == 0 {
		what = "" // the refusal is about a whole span rather than one character
	}
	return fmt.Sprintf("%s  %s%s: %s", at, what, r.Why, oneLine(r.Span, 50))
}

// Repair puts the stranded characters of one body back into their TeX and says
// what it left alone.
//
// It returns the new body, how many characters it replaced, and the refusals. A
// body it has nothing to do with comes back unchanged, rune for rune, so a
// caller can compare and write only what moved.
func Repair(body string) (string, int, []Refusal) {
	spans, unclosed := Split(body)
	rs := []rune(body)
	var b strings.Builder
	var refused []Refusal
	n, at := 0, 0
	if unclosed != nil {
		// A span that never closes has no end, so from its opening dollar on
		// there is no telling where the mathematics stops and the prose starts,
		// and a repair that guessed would rewrite prose. Everything before it
		// is closed and is repaired as usual: 15 pages of chapter VIII end in a
		// display that runs onto the next page, and dropping the whole page for
		// its last three lines would leave the other forty untouched for a
		// fault M01 reports separately anyway.
		refused = append(refused, Refusal{Line: unclosed.Line,
			Why:  "is in a math span that never closes, so the repair cannot see where it ends",
			Span: unclosed.Text})
	}
	for _, s := range spans {
		b.WriteString(string(rs[at:s.Start]))
		at = s.End
		fixed, count, ref := repairSpan([]rune(number(string(rs[s.Start:s.End]), s.Display)))
		for i := range ref {
			ref[i].Line, ref[i].Span = s.Line, s.Text
		}
		b.WriteString(fixed)
		n += count
		refused = append(refused, ref...)
	}
	b.WriteString(string(rs[at:]))
	return b.String(), n, refused
}

// number puts an equation number into the one command KaTeX knows.
//
// The magazine numbers a displayed equation in the right margin, usually (1) or
// (*), and a model transcribing that reaches for \eqno, which is what plain TeX
// calls it and what every textbook of the period was set in. KaTeX implements
// none of plain TeX's numbering and stops at the undefined control sequence, so
// a correct reading of a numbered equation fails the page. \tag is the same
// thing in the dialect KaTeX does read.
//
// It works in a display and nowhere else, which is no loss: a numbered equation
// is a displayed equation, and \eqno inside an inline span is a transcription
// that has already gone wrong. Inline, the number goes back to what it looks
// like on the page, which is the label standing after the formula.
func number(span string, display bool) string {
	return eqno.ReplaceAllStringFunc(span, func(match string) string {
		label := strings.TrimSpace(eqno.FindStringSubmatch(match)[1])
		label = strings.TrimSuffix(strings.TrimPrefix(label, "{"), "}")
		if display {
			return `\tag{` + label + `}`
		}
		return `\quad ` + label
	})
}

// eqno is \eqno with its argument, braced or not. Plain TeX takes the next
// group or the next token, and the magazine's numbers are short enough that
// both spellings turn up in the same issue.
var eqno = regexp.MustCompile(`\\eqno\s*(\{[^}]*\}|\S+)`)

// DropStray takes out a $$ that opens mathematics, closes nothing, and stands
// against the punctuation at the end of a sentence.
//
// The fault it repairs is one the text layer makes over and over. A numbered
// display set on its own line comes through as prose, with the pieces of it in
// inline spans and the display's own closing delimiter carried along to the end
// of the sentence:
//
//	(2) long$_A(M) =$ long(B) and long$_B(M) =$ long(A) $$.
//
// Nothing closes that $$, so everything after it on the page reads as
// mathematics. M01 reports the file, the M rules read prose as formulae, and
// Repair above will not touch a span whose end it cannot see.
//
// The three conditions are narrow on purpose, and each of them was put there by
// a page it got wrong. Chapter VIII has fourteen pages carrying an unclosed
// delimiter and only eight are this fault; the other six are pages where a
// display lost its opening delimiter instead, and on those the first unclosed
// delimiter the splitter meets is the closing one, so a repair that trusted the
// count alone deleted the end of a display that was perfectly good.
//
//   - It must be a display delimiter. A single stray $ is an inline formula
//     that was mangled far more often than it is a leftover: on VIII, p. 411 it
//     is the closing $ of $f=a_\lambda\chi_\lambda$ and taking it out breaks a
//     formula that reads correctly.
//   - The next character must be a full stop or a comma. A $$ alone on its line
//     is a display delimiter doing its job, as on VIII, p. 206.
//   - The body must balance once it is gone, which is the check that the rest
//     of the page was sound.
//
// The caller has the last condition, and it is the page's flags: this is not
// run on a page the extractor has already said it could not read. Four of the
// six carry tall-delimiter or dropped-glyph, which is a matrix or a bracket
// that arrived in pieces, and on those the delimiters mean nothing.
func DropStray(body string) (string, bool) {
	_, un := Split(body)
	if un == nil || !un.Display {
		return body, false
	}
	rs := []rune(body)
	if un.Start >= len(rs) || (rs[un.Start] != '.' && rs[un.Start] != ',') {
		return body, false
	}
	at := un.Start - 2 // the $$ itself
	if at < 0 {
		return body, false
	}
	// The space in front of it goes too. It is there to hold the delimiter off
	// the words, and left behind it is a gap before a full stop.
	cut := at
	for cut > 0 && rs[cut-1] == ' ' {
		cut--
	}
	out := string(rs[:cut]) + string(rs[un.Start:])
	if _, un := Split(out); un != nil {
		return body, false
	}
	return out, true
}

// Unstraddle moves a closing bracket that belongs to the prose back out of the
// mathematics it was left inside.
//
// The fault is the text layer putting the delimiter in the wrong place around a
// function whose name the magazine sets upright. The name comes through as prose,
// its opening bracket comes through as prose, and its closing bracket is swept
// into the formula with the argument:
//
//	the sum is equal to Tr($u)$.
//
// It renders the same as Tr($u$) does, which is why nobody sees it by reading,
// and it is not the same text. The mathematics of that span is "u)", so a
// translator asked to copy the formulae hands back "u" with the bracket set as
// prose, which is right, and the audit refuses the section because a translation
// may not alter mathematics. That is a real refusal: it cost a seventeen minute
// run of the appendix on the trace of an endomorphism, and nothing was written.
//
// The rule is deliberately narrower than "the brackets inside a span balance".
// Measured over the corpus, 979 of 18,664 spans do not balance their own
// brackets and most of them are innocent, because the book writes "(resp. $x$)"
// and labels list items "$\alpha$)". What makes this one a fault is the bracket
// standing immediately before the opening delimiter with nothing between them.
// That shape occurs 138 times in 66 pages and every one of them is a name in
// prose: Tr, det, Ker, Im, Card, cl, deg, rg, diag, int, Br, Nrd, Pc.
//
// Two conditions past the shape, and both are there to keep it from inventing:
//
//   - No more brackets come out than the line has open. A span may close a
//     bracket opened earlier in the same line, and the count is taken over the
//     line with the delimiters removed, so a bracket opened inside an earlier
//     span counts as much as one opened in the prose.
//   - Nothing but a delimiter moves. The body with every dollar sign taken out
//     has to be the body it started as, character for character, and a body that
//     fails that check is handed back untouched. It is a total proof and it is
//     cheap, and it is the only reason this can run unattended over 510 pages.
//
// It returns the new body and how many spans it repaired.
func Unstraddle(body string) (string, int) {
	spans, _ := Split(body)
	rs := []rune(body)
	var b strings.Builder
	n, at := 0, 0
	for _, s := range spans {
		if !straddles(rs, s) {
			continue
		}
		text := []rune(s.Text)
		cut := looseCloser(text)
		run := 1
		for cut+run < len(text) && text[cut+run] == ')' {
			run++
		}
		if open := looseOpeners(rs[lineStart(rs, s.Start):s.Start]); run > open {
			run = open
		}
		head := strings.TrimRight(string(text[:cut]), " \t")
		if run == 0 || head == "" {
			continue
		}
		b.WriteString(string(rs[at:s.Start]))
		b.WriteString(head)
		b.WriteString("$")
		b.WriteString(string(text[:cut])[len(head):]) // the spaces trimmed off it
		b.WriteString(strings.Repeat(")", run))
		b.WriteString(wrap(string(text[cut+run:])))
		at = s.End + 1 // the closing delimiter, which has been written already
		n++
	}
	if n == 0 {
		return body, 0
	}
	b.WriteString(string(rs[at:]))
	out := b.String()
	if strings.ReplaceAll(out, "$", "") != strings.ReplaceAll(body, "$", "") {
		return body, 0
	}
	return out, n
}

// Straddles are the spans a bracket from the prose closes inside, which is the
// fault Unstraddle repairs, read from the other side.
//
// The audit has it as M07 and the repair has it here, off the same test, so the
// rule cannot report a shape the repair does not know about and the repair
// cannot quietly leave one behind. Unstraddle repairs all but the spans where
// the line has fewer brackets open than the span closes, and those are the ones
// worth a person looking at, which is what M07 is for.
func Straddles(body string) []Span {
	spans, _ := Split(body)
	rs := []rune(body)
	var out []Span
	for _, s := range spans {
		if straddles(rs, s) {
			out = append(out, s)
		}
	}
	return out
}

// straddles is the shape: a bracket standing against the opening delimiter with
// nothing between them, and a closing one inside the span that closes nothing of
// the span's own. A display is set on its own lines, so there is no prose
// against it and nothing to have been swept in.
func straddles(rs []rune, s Span) bool {
	return !s.Display && s.Start >= 2 && rs[s.Start-2] == '(' && looseCloser([]rune(s.Text)) >= 0
}

// wrap puts what is left of a span back in delimiters, with the space at either
// end left outside where it reads as the space between two words.
func wrap(rest string) string {
	inner := strings.TrimSpace(rest)
	if inner == "" {
		return rest // nothing but space, and it is prose now
	}
	lead := rest[:len(rest)-len(strings.TrimLeft(rest, " \t\n\r"))]
	return lead + "$" + inner + "$" + rest[len(lead)+len(inner):]
}

// looseCloser is where the first bracket that closes nothing of its own sits.
func looseCloser(rs []rune) int {
	depth := 0
	for i, r := range rs {
		switch r {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// looseOpeners is how many brackets are still open at the end of a stretch of
// body, counting prose and mathematics alike.
func looseOpeners(rs []rune) int {
	depth := 0
	for _, r := range rs {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
	}
	return depth
}

// lineStart is the first rune of the line position i sits on.
func lineStart(rs []rune, i int) int {
	for i > 0 && rs[i-1] != '\n' {
		i--
	}
	return i
}

// repairSpan rewrites the inside of one math span.
func repairSpan(rs []rune) (string, int, []Refusal) {
	var b strings.Builder
	var refused []Refusal
	n := 0
	for i, r := range rs {
		if op, ok := ambiguous[r]; ok && i+1 < len(rs) && (rs[i+1] == '_' || rs[i+1] == '^') {
			refused = append(refused, Refusal{Rune: r,
				Why: fmt.Sprintf("carries a subscript, so it is %s rather than %s", op, texOf[r])})
			b.WriteRune(r)
			continue
		}
		tex, ok := texOf[r]
		if !ok {
			b.WriteRune(r)
			continue
		}
		b.WriteString(tex)
		n++
		// \Gamma written against a following letter names a command that does
		// not exist, so the two are separated the way extract separates them.
		if i+1 < len(rs) && isTeXLetter(rs[i+1]) {
			b.WriteByte(' ')
		}
	}
	return b.String(), n, refused
}

func isTeXLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// stackedRowRE is a matrix the layer flattened: a superscript holding one row
// and a subscript holding the other, on the same base, in either order.
//
// Two things have to hold and both are needed. The scripts are adjacent, which
// is what makes it a matrix rather than a script: the layer raised the top row
// and lowered the bottom row of the same thing, so they come back stuck
// together. And each holds two or more entries with a space between them,
// because the page puts a gap between two columns. Entries are letters and
// digits, which is what the corners of a matrix hold in the Eléments: A B over
// C D, 1 0 over 0 0, a b over c d.
//
// A lone script with a space in it is not this and must not be counted. M_{I J}
// was one, 21 times in chapter VIII, and it was a real defect: the page prints
// the set difference M_{I−J} and the space is the room TeX left for a sign that
// is drawn and not set, so it is in no font and no text layer. That is a
// different defect with a different repair, and reporting it here would have
// put 21 findings under a heading that names the wrong fault. It is repaired
// now, in extract.Minus, which reads the rules pdftocairo reports and puts the
// sign back, so the shape no longer occurs. This paragraph stays because the
// exclusion is still what keeps a script with a space in it from being read as
// a row.
var stackedRowRE = regexp.MustCompile(
	`\^\{` + rowGroup + `\}_\{` + rowGroup + `\}|_\{` + rowGroup + `\}\^\{` + rowGroup + `\}`)

// rowGroup is one row: two or more entries with a space between them.
const rowGroup = `[A-Za-z0-9]+(?: +[A-Za-z0-9]+)+`

// StackedMatrices counts the matrices a body has flattened.
//
// It is exported because the same count is wanted from two sides: here, to flag
// the page while the volume is being read, and in the audit, to say what the
// committed corpus still carries. Two implementations of one shape would drift,
// and the one in the audit would be the one nobody checked against a PDF.
func StackedMatrices(body string) int {
	return len(stackedRowRE.FindAllString(body, -1))
}

// StackedRows is every flattened matrix row in a body, as it is written.
//
// StackedMatrices counts them; this names them, which is what an audit finding
// needs so that somebody can find the thing on the page without opening the
// file.
func StackedRows(body string) []string {
	return stackedRowRE.FindAllString(body, -1)
}
