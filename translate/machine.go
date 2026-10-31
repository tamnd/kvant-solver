package translate

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tamnd/kvant-solver/glossary"
	"github.com/tamnd/kvant-solver/mathtex"
)

// Machine is a sentence translation service: plain text in, plain text out,
// one answer per text. It knows nothing about formulas or Markdown, which is
// why it never sees any.
type Machine interface {
	Translate(ctx context.Context, from, to string, texts []string) ([]string, error)
}

// A draft is what a Machine writes. It is stamped DraftModel in place of a
// model name, DraftVersion in place of a prompt hash and corpus.QualityDraft
// as its quality, so the site can say plainly that it is a rough pass and a
// model run knows to replace it.
//
// DraftVersion moves when the harness changes in a way worth redrafting for.
const (
	DraftModel   = "draft"
	DraftVersion = "draft-1"
)

// The sizes of one call. The service takes a few thousand characters a call
// comfortably, and fewer bigger calls are kinder to it than many small ones.
const (
	draftCallChars = 4000
	draftCallTexts = 64
)

// draft translates one job through e.Machine.
//
// The service is fast and free and has no idea what a formula is, so the
// harness does everything a prompt would otherwise ask a model to do. Each
// line is cut away from its Markdown markers. Every formula, page marker,
// link target, problem label and glossary term is swapped for a numbered
// placeholder the service carries through untouched. Whatever comes back is
// checked for every placeholder exactly once; a line that lost one is sent
// again in the pieces between its placeholders, which cannot lose any. Only
// then are the placeholders put back. The mathematics in a draft is the
// mathematics of the Russian byte for byte, which Verify confirms at the end
// rather than assumes.
func (e *Engine) draft(ctx context.Context, job Job, g *glossary.Glossary) (Result, error) {
	start := time.Now()
	res := Result{Prompt: DraftVersion, Model: DraftModel}
	if g != nil {
		res.Terms = g.Mentioned(job.Body, job.Lang)
	}
	from := e.From
	if from == "" {
		from = "ru"
	}
	d := &drafter{lang: job.Lang, terms: res.Terms}

	var title, body draftDoc
	if job.Title != "" {
		title = d.parse(job.Title)
	}
	body = d.parse(job.Body)
	segs := append(title.segments(), body.segments()...)
	res.Chunks = len(segs)
	e.report(fmt.Sprintf("%d lines to draft", len(segs)))

	if err := d.translate(ctx, e.Machine, from, job.Lang, segs); err != nil {
		return res, err
	}
	res.Title = strings.TrimSpace(title.render())
	res.Body = body.render()
	res.Warnings = append(res.Warnings, d.warnings...)
	// A model's slip is a warning, because a model was asked to copy the
	// formulas and sometimes does not. A draft never sends them anywhere, so a
	// difference here is a fault in the harness, and the file is refused
	// rather than written with its algebra changed.
	if complaint := sameMath(job.Body, res.Body); complaint != "" {
		return res, fmt.Errorf("the draft changed the mathematics: %s", complaint)
	}
	if complaint := sameMath(job.Title, res.Title); complaint != "" {
		return res, fmt.Errorf("the draft changed the title's mathematics: %s", complaint)
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// sameMath is Verify for a draft. The formulas have to come back exactly, but
// not necessarily in the same order: Vietnamese puts the clauses of a sentence
// the other way round from Russian as often as not, and a formula travels with
// its clause. What is compared is the formulas as a multiset.
func sameMath(src, out string) string {
	before, _ := mathtex.Split(src)
	after, unclosed := mathtex.Split(out)
	if unclosed != nil {
		return fmt.Sprintf("a math span left open at line %d", unclosed.Line)
	}
	if len(before) != len(after) {
		return fmt.Sprintf("the source has %d math spans and the draft has %d", len(before), len(after))
	}
	count := map[string]int{}
	for _, s := range before {
		count[fmt.Sprint(s.Display, s.Text)]++
	}
	for _, s := range after {
		k := fmt.Sprint(s.Display, s.Text)
		if count[k] == 0 {
			return fmt.Sprintf("%q is not a formula of the source", s.Text)
		}
		count[k]--
	}
	return ""
}

// hold is one protected stretch: what it was in the source and what goes back
// in its place.
type hold struct{ src, put string }

// draftSeg is one run of prose the service is asked to translate.
type draftSeg struct {
	masked string // the prose with ⟦n⟧ for each hold
	holds  []hold
	lead   string // whitespace outside the prose, kept as it was
	trail  string
	out    string // the masked translation, placeholders still in
	done   bool
}

// draftDoc is a body as literal text with segments between.
type draftDoc []any // string or *draftSeg

func (d draftDoc) segments() []*draftSeg {
	var out []*draftSeg
	for _, p := range d {
		if s, ok := p.(*draftSeg); ok {
			out = append(out, s)
		}
	}
	return out
}

func (d draftDoc) render() string {
	var b strings.Builder
	for _, p := range d {
		switch v := p.(type) {
		case string:
			b.WriteString(v)
		case *draftSeg:
			b.WriteString(v.lead)
			b.WriteString(v.restore())
			b.WriteString(v.trail)
		}
	}
	return b.String()
}

type drafter struct {
	lang     string
	terms    []glossary.Term
	warnings []string
}

var (
	fence       = regexp.MustCompile("^\\s*(```|~~~)")
	tableRule   = regexp.MustCompile(`^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	lineMarkers = regexp.MustCompile(`^\s*(?:(?:#{1,6}|>|[-*+]|\d{1,3}[.)])\s+)*`)
	placeholder = regexp.MustCompile(`⟦\s*(\d+)\s*⟧`)
	cyrillic    = regexp.MustCompile(`\p{Cyrillic}`)
)

// parse cuts a body into literal text and segments.
func (d *drafter) parse(body string) draftDoc {
	math := mathRanges(body)
	var doc draftDoc
	inFence := false
	offset := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		lineStart := offset
		offset += len(line)
		text := strings.TrimSuffix(line, "\n")
		nl := line[len(text):]
		if fence.MatchString(text) {
			inFence = !inFence
			doc = append(doc, line)
			continue
		}
		if inFence || strings.TrimSpace(text) == "" || tableRule.MatchString(text) {
			doc = append(doc, line)
			continue
		}
		spans := clip(math, lineStart, lineStart+len(text))
		if strings.HasPrefix(strings.TrimSpace(text), "|") {
			doc = append(doc, d.cells(text, spans)...)
		} else {
			// Markers are only markers outside a formula.
			pre := lineMarkers.FindString(text)
			for _, s := range spans {
				if s[0] < len(pre) {
					pre = pre[:s[0]]
				}
			}
			doc = append(doc, pre)
			doc = append(doc, d.segment(text[len(pre):], shift(spans, -len(pre))))
		}
		doc = append(doc, nl)
	}
	return doc
}

// cells splits a table row on the bars that are not inside a formula and
// makes each cell its own segment.
func (d *drafter) cells(text string, spans [][2]int) draftDoc {
	var doc draftDoc
	last := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if text[i] != '|' || inside(spans, i) {
			continue
		}
		if i > last {
			doc = append(doc, d.segment(text[last:i], clip(spans, last, i)))
		}
		doc = append(doc, "|")
		last = i + 1
	}
	if last < len(text) {
		doc = append(doc, d.segment(text[last:], clip(spans, last, len(text))))
	}
	return doc
}

// protect is one kind of thing the service must not touch, and what to put
// back for it.
type protect struct {
	re  *regexp.Regexp
	put func(d *drafter, m []string) string
}

var verbatim = func(_ *drafter, m []string) string { return m[0] }

var protects = []protect{
	{regexp.MustCompile(`⟦[^⟧]*⟧`), verbatim},
	{regexp.MustCompile("`[^`]+`"), verbatim},
	{regexp.MustCompile(`!?\[[^\]]*\]\([^)]*\)`), verbatim},
	{regexp.MustCompile(`\[\^[^\]]+\]`), verbatim},
	{regexp.MustCompile(`https?://\S+`), verbatim},
	{regexp.MustCompile(`</?[a-zA-Z][^>]*>`), verbatim},
	// A problem label, with the word for problem when it is there, so that
	// «Задача М123» comes back as the corpus spells it in every language
	// rather than as whatever the service thinks a задача is today.
	{regexp.MustCompile(`(?:([Зз]адач[аиуеой]{1,2})\s+)?([МФ])\s?(\d{1,4})`), func(d *drafter, m []string) string {
		label := map[string]string{"М": "M", "Ф": "F"}[m[2]] + m[3]
		if d.lang == "ru" {
			label = m[2] + m[3]
		}
		if m[1] == "" {
			return label
		}
		word := problemWord[d.lang]
		if word == "" {
			return m[1] + " " + label
		}
		if unicode.IsLower(firstRune(m[1])) {
			word = strings.ToLower(word[:1]) + word[1:]
		}
		return word + " " + label
	}},
	{regexp.MustCompile(`«Квант[а-яё]*»`), func(d *drafter, m []string) string {
		if d.lang == "ru" {
			return m[0]
		}
		return "«Kvant»"
	}},
}

// draftTerms are the few words of this magazine that the service gets wrong
// every time, in every inflection, put back the way the model translations
// spell them. A задача in Kvant is a problem to solve and never a task, and an
// решение is its solution and never a decision. The glossary does this job for
// a model; the service cannot be shown a glossary, so the words are swapped
// out before it sees them. Vietnamese does not inflect, which is what makes
// this safe there.
var draftTerms = map[string][]protect{
	"en": {
		term(`[Рр]ис\.`, "Fig."),
		term(`[Рр]исун(?:ок|ка|ку|ке|ком|ки|ков|кам|ками|ках)`, "figure"),
	},
	"zh": {
		term(`[Рр]ис\.`, "图"),
		term(`[Рр]исун(?:ок|ка|ку|ке|ком|ки|ков|кам|ками|ках)`, "图"),
	},
	"vi": {
		term(`[Зз]адачник[а-я]*\s+«Кванта»`, "Bài toán Kvant"),
		term(`«Квант[а-я]*»\s+для\s+младших\s+школьников`, "«Kvant» cho học sinh nhỏ"),
		term(`[Зз]адач(?:а|и|у|е|ей|ею|ам|ами|ах)?`, "bài toán"),
		term(`[Рр]ешени(?:е|я|ю|ем|и|й|ям|ями|ях)`, "lời giải"),
		term(`[Оо]твет(?:ы|а|у|ом|е|ов|ам|ами|ах)?`, "đáp số"),
		term(`[Уу]казани(?:е|я|ю|ем|и|й|ям|ями|ях)`, "gợi ý"),
		// «Рис. 3» is a figure, which the service serves up as rice.
		term(`[Рр]ис(?:\.|унок|унка|унку|унке|унком|унки|унков|ункам|унками|унках)`, "hình"),
	},
}

// term is a draft term whose rendering takes the capital of the source.
func term(pattern, put string) protect {
	return protect{regexp.MustCompile(pattern), func(_ *drafter, m []string) string {
		if unicode.IsUpper(firstRune(m[0])) {
			r := []rune(put)
			if r[0] != '«' {
				r[0] = unicode.ToUpper(r[0])
			}
			return string(r)
		}
		return put
	}}
}

var problemWord = map[string]string{"en": "Problem", "vi": "Bài toán", "zh": "习题"}

// segment masks one run of prose.
func (d *drafter) segment(text string, math [][2]int) *draftSeg {
	core := strings.TrimSpace(text)
	lead := text[:strings.Index(text, core)]
	if core == "" {
		return &draftSeg{lead: text, done: true}
	}
	trail := text[len(lead)+len(core):]
	math = clip(math, len(lead), len(lead)+len(core))

	type cut struct {
		at, end int
		put     string
	}
	var cuts []cut
	for _, m := range math {
		cuts = append(cuts, cut{m[0], m[1], core[m[0]:m[1]]})
	}
	for _, p := range append(protects[:len(protects):len(protects)], draftTerms[d.lang]...) {
		for _, loc := range p.re.FindAllStringSubmatchIndex(core, -1) {
			if !bounded(core, loc[0], loc[1]) {
				continue
			}
			groups := make([]string, len(loc)/2)
			for i := range groups {
				if loc[2*i] >= 0 {
					groups[i] = core[loc[2*i]:loc[2*i+1]]
				}
			}
			cuts = append(cuts, cut{loc[0], loc[1], p.put(d, groups)})
		}
	}
	for _, t := range d.terms {
		put := t.In(d.lang)
		if put == "" || utf8.RuneCountInString(t.RU) < 4 {
			continue
		}
		lower := strings.ToLower(core)
		key := strings.ToLower(t.RU)
		for i := 0; ; {
			j := strings.Index(lower[i:], key)
			if j < 0 {
				break
			}
			at := i + j
			end := at + len(key)
			if len(lower) == len(core) && bounded(core, at, end) {
				cuts = append(cuts, cut{at, end, put})
			}
			i = end
		}
	}
	// The earliest cut wins, and of two that start together the longer one,
	// so a formula is never half inside a label.
	sort.SliceStable(cuts, func(i, j int) bool {
		if cuts[i].at != cuts[j].at {
			return cuts[i].at < cuts[j].at
		}
		return cuts[i].end > cuts[j].end
	})
	s := &draftSeg{lead: lead, trail: trail}
	var b strings.Builder
	pos := 0
	for _, c := range cuts {
		if c.at < pos {
			continue
		}
		b.WriteString(core[pos:c.at])
		fmt.Fprintf(&b, "⟦%d⟧", len(s.holds))
		s.holds = append(s.holds, hold{src: core[c.at:c.end], put: c.put})
		pos = c.end
	}
	b.WriteString(core[pos:])
	s.masked = b.String()
	if !cyrillic.MatchString(placeholder.ReplaceAllString(s.masked, "")) {
		s.out, s.done = s.masked, true
	}
	return s
}

// restore puts the holds back into the translation.
func (s *draftSeg) restore() string {
	return placeholder.ReplaceAllStringFunc(s.out, func(m string) string {
		n, _ := strconv.Atoi(placeholder.FindStringSubmatch(m)[1])
		if n < len(s.holds) {
			return s.holds[n].put
		}
		return m
	})
}

// intact reports whether every placeholder came back exactly once.
func (s *draftSeg) intact(out string) bool {
	seen := make([]int, len(s.holds))
	for _, m := range placeholder.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[1])
		if n >= len(seen) {
			return false
		}
		seen[n]++
	}
	for _, c := range seen {
		if c != 1 {
			return false
		}
	}
	return true
}

// translate sends every segment that needs it, then sends again in pieces
// whatever came back with a placeholder lost.
func (d *drafter) translate(ctx context.Context, m Machine, from, to string, segs []*draftSeg) error {
	var todo []*draftSeg
	for _, s := range segs {
		if !s.done {
			todo = append(todo, s)
		}
	}
	// Round one: whole segments, long ones cut at sentence ends.
	var texts []string
	var owner []int
	for i, s := range todo {
		for _, part := range sentences(s.masked, draftCallChars) {
			texts = append(texts, part)
			owner = append(owner, i)
		}
	}
	outs, err := batch(ctx, m, from, to, texts)
	if err != nil {
		return err
	}
	joined := make([][]string, len(todo))
	for k, out := range outs {
		joined[owner[k]] = append(joined[owner[k]], tidy(out, texts[k]))
	}
	var broken []*draftSeg
	for i, s := range todo {
		out := strings.Join(joined[i], " ")
		if s.intact(out) {
			s.out, s.done = out, true
			continue
		}
		broken = append(broken, s)
	}
	if len(broken) == 0 {
		return nil
	}

	// Round two: the prose between placeholders, piece by piece. Nothing the
	// service sees has a placeholder in it, so nothing can be lost.
	type piece struct {
		seg   *draftSeg
		parts []string
		idx   []int // which parts are prose to send
	}
	var pieces []piece
	texts = texts[:0]
	for _, s := range broken {
		p := piece{seg: s, parts: splitKeep(s.masked)}
		for i, part := range p.parts {
			if !placeholder.MatchString(part) && cyrillic.MatchString(part) {
				p.idx = append(p.idx, i)
				texts = append(texts, strings.TrimSpace(part))
			}
		}
		pieces = append(pieces, p)
	}
	outs, err = batch(ctx, m, from, to, texts)
	if err != nil {
		return err
	}
	k := 0
	for _, p := range pieces {
		for _, i := range p.idx {
			part := p.parts[i]
			core := strings.TrimSpace(part)
			lead := part[:strings.Index(part, core)]
			p.parts[i] = lead + tidy(outs[k], core) + part[len(lead)+len(core):]
			k++
		}
		p.seg.out, p.seg.done = strings.Join(p.parts, ""), true
	}
	d.warnings = append(d.warnings, fmt.Sprintf(
		"%d of %d lines lost a placeholder and were drafted piece by piece", len(broken), len(todo)))
	return nil
}

// batch sends texts in as few calls as fit the call sizes.
func batch(ctx context.Context, m Machine, from, to string, texts []string) ([]string, error) {
	out := make([]string, 0, len(texts))
	for i := 0; i < len(texts); {
		j, chars := i, 0
		for j < len(texts) && j-i < draftCallTexts && (j == i || chars+len(texts[j]) <= draftCallChars) {
			chars += len(texts[j])
			j++
		}
		got, err := m.Translate(ctx, from, to, texts[i:j])
		if err != nil {
			return nil, err
		}
		if len(got) != j-i {
			return nil, fmt.Errorf("sent %d texts and got %d back", j-i, len(got))
		}
		out = append(out, got...)
		i = j
	}
	return out, nil
}

// tidy undoes what the service does to text on the way through: entities it
// escaped and space it trimmed or doubled.
func tidy(out, src string) string {
	if !strings.Contains(src, "&") {
		out = html.UnescapeString(out)
	}
	return strings.TrimSpace(out)
}

// sentences cuts text no longer than limit, at sentence ends where it can.
func sentences(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}
	var out []string
	for len(text) > limit {
		cut := -1
		for i := limit; i > limit/2; i-- {
			if text[i] == ' ' && i > 0 && strings.ContainsRune(".!?;…", rune(text[i-1])) {
				cut = i
				break
			}
		}
		if cut < 0 {
			cut = strings.LastIndex(text[:limit], " ")
		}
		if cut <= 0 {
			cut = limit
			for !utf8.RuneStart(text[cut]) {
				cut--
			}
		}
		out = append(out, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	return append(out, text)
}

// splitKeep cuts masked text into placeholders and the text between them.
func splitKeep(s string) []string {
	var out []string
	last := 0
	for _, loc := range placeholder.FindAllStringIndex(s, -1) {
		if loc[0] > last {
			out = append(out, s[last:loc[0]])
		}
		out = append(out, s[loc[0]:loc[1]])
		last = loc[1]
	}
	if last < len(s) {
		out = append(out, s[last:])
	}
	return out
}

// mathRanges is every formula in a body with its delimiters, as byte ranges.
// A formula left open runs to the end of the body, which keeps the service off
// it rather than guessing where it should have closed.
func mathRanges(body string) [][2]int {
	spans, unclosed := mathtex.Split(body)
	runeAt := make([]int, 0, len(body)+1)
	for i := range body {
		runeAt = append(runeAt, i)
	}
	runeAt = append(runeAt, len(body))
	at := func(r int) int {
		if r < 0 {
			return 0
		}
		if r >= len(runeAt) {
			return len(body)
		}
		return runeAt[r]
	}
	var out [][2]int
	for _, s := range spans {
		w := 1
		if s.Display {
			w = 2
		}
		out = append(out, [2]int{at(s.Start - w), at(s.End + w)})
	}
	if unclosed != nil {
		w := 1
		if unclosed.Display {
			w = 2
		}
		out = append(out, [2]int{at(unclosed.Start - w), len(body)})
	}
	return out
}

// clip keeps the parts of ranges inside [from, to), relative to the body.
func clip(ranges [][2]int, from, to int) [][2]int {
	var out [][2]int
	for _, r := range ranges {
		a, b := max(r[0], from), min(r[1], to)
		if a < b {
			out = append(out, [2]int{a - from, b - from})
		}
	}
	return out
}

func shift(ranges [][2]int, by int) [][2]int {
	out := make([][2]int, len(ranges))
	for i, r := range ranges {
		out[i] = [2]int{r[0] + by, r[1] + by}
	}
	return out
}

func inside(ranges [][2]int, i int) bool {
	for _, r := range ranges {
		if i >= r[0] && i < r[1] {
			return true
		}
	}
	return false
}

// bounded reports whether text[at:end] is not part of a longer word.
func bounded(text string, at, end int) bool {
	if at > 0 {
		r, _ := utf8.DecodeLastRuneInString(text[:at])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if f := firstRune(text[at:end]); unicode.IsLetter(f) || unicode.IsDigit(f) {
				return false
			}
		}
	}
	if end < len(text) {
		r, _ := utf8.DecodeRuneInString(text[end:])
		l, _ := utf8.DecodeLastRuneInString(text[at:end])
		if (unicode.IsLetter(r) || unicode.IsDigit(r)) && (unicode.IsLetter(l) || unicode.IsDigit(l)) {
			return false
		}
	}
	return true
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}
