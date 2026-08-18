package publisher

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Diff is one article read twice, once by the archive's own text and once by a
// vision pass over the same pages.
type Diff struct {
	Issue string
	Year  int
	Slug  string
	Title string

	Count

	// Examples are the first few words the archive has that we do not, for
	// somebody to look at. A rate with no examples under it is a number nobody
	// can act on.
	Examples []Example
}

// Count is the arithmetic of one comparison.
//
// It is kept in two directions because the two directions mean different
// things. A word the archive has and the page does not is the archive being
// wrong, and that is what the trust verdict is about. A word the page has and
// the archive does not is almost never the archive being wrong, it is our
// article holding more of the page than the archive's article does, and that is
// a fact about where articles begin and end rather than about the text.
type Count struct {
	// Words is the archive's reading in words, and Changed is how many of them
	// our reading of the same pages does not have.
	Words   int
	Changed int

	// Ours is our reading in words, and Extra is how many of them the archive's
	// text does not account for.
	Ours  int
	Extra int

	// Near is how many of the changed words are the two readings spelling the
	// same word differently rather than one of them missing it. See near.
	Near int
}

// Rate is the share of the archive's own words that our reading of the same
// pages does not have. This is what the trust verdict is taken on.
func (c Count) Rate() float64 {
	if c.Words == 0 {
		return 0
	}
	return float64(c.Changed) / float64(c.Words)
}

// Missing is the share of the first reading's words the second does not have in
// any spelling.
//
// It is Rate with the near misses taken out, and it is the stricter of the two
// questions a comparison can ask. Rate counts every word the two put
// differently, which includes one of them having misread a letter in a word the
// other has right, and that is a defect nobody downstream would notice. This
// counts only text that is not there, which is the defect that makes a page
// unusable and cannot be recovered from later. See near for what separates them
// and for the limits of doing it by spelling.
func (c Count) Missing() float64 {
	if c.Words == 0 {
		return 0
	}
	return float64(c.Changed-c.Near) / float64(c.Words)
}

// Coverage is the share of our reading the archive's text accounts for.
//
// It is not an error rate and a low one is not a complaint about the archive.
// It is how much of the page the archive's article covers, and it is low
// wherever three short pieces share a page, because assembly cuts at the page
// and the archive cuts at the piece. It is here so that an archive text that
// stops halfway through a long article is visible instead of scoring a perfect
// rate on the half it did type.
func (c Count) Coverage() float64 {
	if c.Ours == 0 {
		return 1
	}
	return float64(c.Ours-c.Extra) / float64(c.Ours)
}

// Example is one place the two readings part company.
type Example struct {
	Publisher string
	Vision    string
}

// MaxExamples is how many disagreements are kept per article. Enough to see
// what kind of disagreement it is, few enough that a report of a hundred
// articles is still a page.
const MaxExamples = 5

// Compare measures how far apart two readings of the same article are.
//
// The unit is the word and not the character, because a character rate is
// dominated by whichever reading hyphenates differently and says nothing about
// whether the two say the same thing. Words are compared after normalisation,
// which takes out the things that are typography rather than text: case, the
// three kinds of dash, the two kinds of quote, the soft hyphen a line break
// leaves behind. What is left is a word that a reader would call the same word.
//
// The two directions are counted apart, and the first measurement said why.
// Three pieces share page 41 of the first issue of 1975, our reading of each of
// them is the whole page because assembly cuts at the page, and comparing that
// against the archive's few lines scored ninety per cent against text that is
// word for word right. Charging the archive for words it never claimed to carry
// measures our assembly and calls it their typing.
func Compare(publisher, vision string) (Count, []Example) {
	left, right := Words(publisher), Words(vision)
	changed, extra, close, examples := distance(left, right)
	return Count{
		Words: len(left), Changed: changed,
		Ours: len(right), Extra: extra,
		Near: close,
	}, examples
}

// Words is the comparable form of a body: its words, in order, with the markup
// and the typography taken out.
//
// Heading lines go with the markup, on both sides. The archive serves the body
// of an article and prints its title around it, and our reading transcribes the
// title as part of the page, so a heading is content on one side and furniture
// on the other. Counting it would charge every article the same few words and
// charge the short ones most.
func Words(text string) []string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	text = strings.Join(kept, "\n")
	text = math.ReplaceAllStringFunc(text, loosen)
	text = drop.ReplaceAllString(text, " ")
	text = invisible.Replace(strings.ToLower(text))
	var out []string
	for _, field := range strings.FieldsFunc(text, separator) {
		out = append(out, strings.ReplaceAll(field, "ё", "е"))
	}
	return out
}

// drop is the markup that is not text.
//
// The folio and column lines are the transcription's own bookkeeping and the
// publisher's file has no equivalent, so counting them would charge the vision
// side for doing its job. A TeX control word goes for the same reason on the
// other side: \le and \leqslant are one relation printed by two houses, \dfrac
// and \frac are one fraction, and after the split a control word is
// indistinguishable from a Latin word, so leaving them in would score the same
// formula as five wrong words. What survives is the letters and the digits, and
// two readings that disagree about those disagree about the mathematics.
var drop = regexp.MustCompile(`⟦[^⟧]*⟧|⟪[^⟫]*⟫|\\(?:begin|end)\{[^}]*\}(?:\{[^}]*\})?|\\[a-zA-Z]+`)

// math is a formula, inline or displayed, which is the only place the next rule
// is allowed to touch.
var math = regexp.MustCompile(`\$\$[^$]*\$\$|\$[^$]*\$`)

// loosen puts every digit of a formula on its own.
//
// TeX lets a one character argument go without braces and the archive takes the
// offer, so their \dfrac13 and our \frac{1}{3} are the same third written two
// ways, and one is a word thirteen while the other is a one and a three. This
// is the shortest rule that makes the two agree, and it costs a year printed
// inside a formula being read digit by digit on both sides, which changes
// nothing.
func loosen(formula string) string {
	var b strings.Builder
	for _, r := range formula {
		if unicode.IsDigit(r) {
			b.WriteString(" ")
			b.WriteRune(r)
			b.WriteString(" ")
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// invisible is the characters a line break leaves behind. They go before the
// split rather than after, or a word broken across a column would come back as
// two words on one side and one on the other.
var invisible = strings.NewReplacer("\u00ad", "", "\u200b", "", "\u200d", "", "\u2060", "")

// separator is everything that is not a letter or a digit.
//
// This is coarser than splitting on spaces and it is coarser on purpose. Two
// readings of the same formula agree on its letters and digits and agree on
// nothing else: one writes $x^4-3x^3+3=0$ and the other puts spaces around the
// operators, and on a space split that is one word against five, all of them
// wrong, in a magazine where a good part of every page is formulae. Splitting
// here makes the two spell it the same way. It also splits a hyphenated word in
// two, on both sides, which costs nothing.
func separator(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// distance is the word level edit distance between two readings, split into the
// edits that cost the archive a word and the edits that cost us one, with the
// first few of the former kept as examples.
//
// It is the full Wagner-Fischer table rather than a diff library, because an
// article is a few thousand words and the table is a few million cells, which
// is milliseconds, and because the alternative is a dependency for one page of
// code that every reader of this repo already knows.
func distance(a, b []string) (changed, extra, close int, examples []Example) {
	// The rows are kept as one slice each, and the moves are kept alongside so
	// that the examples come out of the same table the number does.
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	trace := make([][]byte, len(a)+1)
	for i := range trace {
		trace[i] = make([]byte, len(b)+1)
	}
	for j := 0; j <= len(b); j++ {
		prev[j] = j
		trace[0][j] = 'i'
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		trace[i][0] = 'd'
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1]
				trace[i][j] = '='
				continue
			}
			sub, del, ins := prev[j-1], prev[j], cur[j-1]
			switch {
			case sub <= del && sub <= ins:
				cur[j], trace[i][j] = sub+1, 's'
			case del <= ins:
				cur[j], trace[i][j] = del+1, 'd'
			default:
				cur[j], trace[i][j] = ins+1, 'i'
			}
		}
		prev, cur = cur, prev
	}
	return walk(trace, a, b)
}

// walk reads the moves back from the end of the table, counts them by direction
// and keeps the first few in reading order.
//
// Only the archive's own words become examples. An insertion is a word of ours
// the archive's article does not reach, and a table of five of those says
// nothing except that the two cut the page in different places, which the
// coverage figure already says in one number.
func walk(trace [][]byte, a, b []string) (changed, extra, close int, examples []Example) {
	var out []Example
	i, j := len(a), len(b)
	for i > 0 || j > 0 {
		switch trace[i][j] {
		case '=':
			i, j = i-1, j-1
			continue
		case 's':
			changed++
			if near(a[i-1], b[j-1]) {
				close++
			}
			out = append(out, Example{Publisher: a[i-1], Vision: b[j-1]})
			i, j = i-1, j-1
		case 'd':
			changed++
			out = append(out, Example{Publisher: a[i-1]})
			i--
		default:
			extra++
			j--
		}
	}
	// Read back to front, so reverse, and keep the head.
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	if len(out) > MaxExamples {
		out = out[:MaxExamples]
	}
	return changed, extra, close, out
}

// near reports whether two words that came out different are the same word
// spelled two ways.
//
// The distinction matters because the two failures it separates are not
// comparable. A word one reading has and the other does not have at all is text
// gone missing, and that is what a comparison is run to find. A word both
// readings have and spell within a character or two of each other is one of them
// having misread a letter: кабитации for кавитации, зреня for зрения. The words
// are still there, a reader still gets the sentence, and no amount of it makes a
// page unusable the way a dropped line does.
//
// It is a heuristic and it cannot be anything else. It cannot tell a misread
// from a different form of the same word, and физике against физфаке counts as
// near here when it is two different words. So the count is a floor under how
// much of a disagreement is noise rather than a measurement of it, and it is
// reported that way.
func near(a, b string) bool {
	x, y := []rune(a), []rune(b)
	allowed := 1
	if max(len(x), len(y)) > 5 {
		allowed = 2
	}
	if abs(len(x)-len(y)) > allowed {
		return false
	}
	return edits(x, y, allowed) <= allowed
}

// edits is the character level distance between two words, giving up once it is
// past the limit, which is what keeps this cheap enough to run per substitution.
func edits(a, b []rune, limit int) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1]
			} else {
				cur[j] = 1 + min(prev[j-1], min(prev[j], cur[j-1]))
			}
			best = min(best, cur[j])
		}
		if best > limit {
			return limit + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Year is the disagreement for one year, over the articles sampled in it. Its
// rate and coverage are the year's words added up rather than its rates
// averaged, so that a one paragraph filler does not count as much as a four
// page piece.
type Year struct {
	Year     int
	Articles int
	Count

	// Worst is the article with the highest rate, which is where somebody
	// looking at this should start.
	Worst Diff
}

// Years groups article diffs by year.
func Years(diffs []Diff) []Year {
	index := map[int]*Year{}
	for _, d := range diffs {
		y, ok := index[d.Year]
		if !ok {
			y = &Year{Year: d.Year}
			index[d.Year] = y
		}
		y.Articles++
		y.Words += d.Words
		y.Changed += d.Changed
		y.Ours += d.Ours
		y.Extra += d.Extra
		if d.Rate() > y.Worst.Rate() {
			y.Worst = d
		}
	}
	out := make([]Year, 0, len(index))
	for _, y := range index {
		out = append(out, *y)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out
}
