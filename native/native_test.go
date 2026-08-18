package native

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/pdfsrc"
)

// The tests build pages rather than parsing them, because what is under test is
// what the positions mean and not how they are spelled in pdftotext's XHTML.
// The numbers are the ones the January 2010 issue is actually set in: a 659 by
// 859 point page, nine point body, six point scripts, twelve point leading.

const (
	body    = 9.0
	small   = 6.0
	leading = 12.0
)

// word places one word with its baseline at base and its size in points. The
// width is the rough one a proportional face gives, because nothing here
// measures a word across.
func word(text string, x, base, size float64) pdfsrc.Word {
	return pdfsrc.Word{
		Box: pdfsrc.Box{
			XMin: x, YMin: base - size,
			XMax: x + size*0.55*float64(len([]rune(text))), YMax: base,
		},
		Text: text,
	}
}

// line lays a run of words out on one baseline at the body size, which is what
// most of a line of this magazine is.
func line(x, base float64, words ...string) pdfsrc.Line {
	out := pdfsrc.Line{}
	for _, w := range words {
		word := word(w, x, base, body)
		x = word.XMax + 4
		out.Words = append(out.Words, word)
	}
	return box(out)
}

// box fits a line's box around the words in it, the way pdftotext does.
func box(l pdfsrc.Line) pdfsrc.Line {
	if len(l.Words) == 0 {
		return l
	}
	l.Box = l.Words[0].Box
	for _, w := range l.Words[1:] {
		l.XMin = min(l.XMin, w.XMin)
		l.YMin = min(l.YMin, w.YMin)
		l.XMax = max(l.XMax, w.XMax)
		l.YMax = max(l.YMax, w.YMax)
	}
	return l
}

func block(lines ...pdfsrc.Line) pdfsrc.Block {
	b := pdfsrc.Block{Lines: lines}
	if len(lines) == 0 {
		return b
	}
	b.Box = lines[0].Box
	for _, l := range lines[1:] {
		b.XMin = min(b.XMin, l.XMin)
		b.YMin = min(b.YMin, l.YMin)
		b.XMax = max(b.XMax, l.XMax)
		b.YMax = max(b.YMax, l.YMax)
	}
	return b
}

func sheet(blocks ...pdfsrc.Block) pdfsrc.LayoutPage {
	return pdfsrc.LayoutPage{Number: 1, Width: 659, Height: 859, Blocks: blocks}
}

// folio is the page number where this magazine prints it, alone at the head of
// the page. Most tests need one because a page without one is rejected before
// anything else about it is looked at.
func folioBlock(n string) pdfsrc.Block {
	return block(line(65, 74, n))
}

// prose is a block of ordinary text, enough of it that the median height and
// the median baseline of a line are the body's and not a script's.
func prose(x, base float64, words ...string) pdfsrc.Block {
	return block(line(x, base, words...))
}

func read(blocks ...pdfsrc.Block) Page {
	return Read(sheet(append([]pdfsrc.Block{folioBlock("64")}, blocks...)...))
}

func TestASubscriptGoesBackUnderTheLetterItBelongsTo(t *testing.T) {
	// The case the whole package is for. Flattened, this is "S KLM ≥ S MPB",
	// which says nothing about which letters are the areas and which are the
	// triangles.
	// The scripts sit where pdftotext puts them, which is in reading order right
	// after the letter they belong to and not at the end of the line.
	l := box(pdfsrc.Line{Words: []pdfsrc.Word{
		word("Тогда", 65, 129, body),
		word("S", 100, 129, body),
		word("KLM", 106, 130, small),
		word("≥", 125, 129, body),
		word("S", 135, 129, body),
		word("MPB", 141, 130, small),
		word("и", 160, 129, body),
	}})
	p := Read(sheet(folioBlock("64"), block(l)))
	if !strings.Contains(p.Body, `$S_{KLM} \ge S_{MPB}$`) {
		t.Fatalf("the formula came out as\n%s", p.Body)
	}
	if p.Scripts != 2 {
		t.Errorf("%d scripts were put back, want 2", p.Scripts)
	}
}

func TestARaisedWordGoesBackAsAPower(t *testing.T) {
	l := box(pdfsrc.Line{Words: []pdfsrc.Word{
		word("при", 65, 129, body),
		word("N", 85, 129, body),
		word("=", 95, 129, body),
		word("2", 105, 129, body),
		word("k", 111, 125, small),
		word("мест", 120, 129, body),
	}})
	p := Read(sheet(folioBlock("64"), block(l)))
	if !strings.Contains(p.Body, `$N = 2^{k}$`) {
		t.Fatalf("the power came out as\n%s", p.Body)
	}
}

func TestAWordOnTheLineIsNotAScriptHowFarDownItSits(t *testing.T) {
	// A comma sits below the baseline and a footnote is set small, and either
	// test on its own would call one of them a subscript.
	l := line(65, 129, "утверждения", "и", "отрезок")
	l.Words = append(l.Words, word("прямой", 200, 131, body))
	p := Read(sheet(folioBlock("64"), block(box(l))))
	if strings.Contains(p.Body, "_{") {
		t.Fatalf("a word of the same size became a subscript:\n%s", p.Body)
	}
}

func TestARussianWordIsProseWhereverItStands(t *testing.T) {
	// The magazine writes units and short words inside its formulas, and a
	// formula that swallowed one would not parse anywhere downstream.
	p := read(prose(65, 129, "скорость", "v", "=", "10", "м/с", "равна"))
	if strings.Contains(p.Body, "$м/с") || strings.Contains(p.Body, "м/с$") {
		t.Fatalf("a Russian word was pulled into the formula:\n%s", p.Body)
	}
}

func TestABareNumberOnItsOwnIsNotAFormula(t *testing.T) {
	// A page carries dates, page references and footnote numbers, and wrapping
	// each of them in dollars would make most of the corpus mathematics.
	p := read(prose(65, 129, "в", "1970", "году", "журнал", "вышел"))
	if strings.Contains(p.Body, "$") {
		t.Fatalf("a year became a formula:\n%s", p.Body)
	}
	if p.Math != 0 {
		t.Errorf("%d words counted as mathematics, want none", p.Math)
	}
}

func TestAWordTheTypesetterBrokeIsPutBackTogether(t *testing.T) {
	// Two columns means heavy hyphenation, and halves left apart put a good
	// share of the vocabulary of every page out of reach of a search.
	p := read(block(
		line(65, 129, "величины", "равны", "соот-"),
		line(65, 129+leading, "ветственно", "друг", "другу."),
	))
	if !strings.Contains(p.Body, "соответственно") {
		t.Fatalf("the halves stayed apart:\n%s", p.Body)
	}
}

func TestACompoundNounKeepsItsHyphen(t *testing.T) {
	// The test cannot be the hyphen alone. This magazine is full of по-прежнему
	// and Али-Баба, and both would lose it.
	p := read(prose(65, 129, "по-прежнему", "на", "каждом", "шагу"))
	if !strings.Contains(p.Body, "по-прежнему") {
		t.Fatalf("a compound noun lost its hyphen:\n%s", p.Body)
	}
}

func TestAFractionIsCaughtByHowCloseTheLinesAre(t *testing.T) {
	// What a fraction looks like from underneath: the numerator and the
	// denominator arrive as two lines half a leading apart, and the rule between
	// them is not in the file at all.
	p := read(block(
		line(65, 100, "Отсюда", "следует", "что"),
		line(65, 100+leading, "работа", "поля", "равна"),
		line(65, 100+leading+5, "A"),
		line(65, 100+2*leading+5, "равна", "нулю", "на", "этом", "участке"),
	))
	if p.Stacked != 1 {
		t.Fatalf("%d stacked lines, want the one set half a leading up", p.Stacked)
	}
}

func TestAParagraphSetNormallyHasNothingStackedInIt(t *testing.T) {
	p := read(block(
		line(65, 100, "первая", "строка", "абзаца"),
		line(65, 100+leading, "вторая", "строка", "абзаца"),
		line(65, 100+2*leading, "третья", "строка", "абзаца"),
		line(65, 100+3*leading, "четвертая", "строка."),
	))
	if p.Stacked != 0 {
		t.Fatalf("%d stacked lines in a plain paragraph", p.Stacked)
	}
}

func TestTheFolioIsReadOffThePageAndItsTwinIsDropped(t *testing.T) {
	// These files print the number twice, once on the page and once in the trim
	// below it, and the second one lands in the middle of the transcription.
	p := Read(sheet(
		folioBlock("64"),
		prose(65, 300, "текст", "статьи", "продолжается", "здесь."),
		block(line(171, 841, "64")),
	))
	if p.Folio != 64 {
		t.Fatalf("the folio came out as %d", p.Folio)
	}
	if !strings.HasPrefix(p.Body, FolioMark(64)) {
		t.Fatalf("the body does not open with the folio:\n%s", p.Body)
	}
	if strings.Contains(p.Body, "\n64\n") {
		t.Fatalf("the twin in the trim is still in the body:\n%s", p.Body)
	}
}

func TestAYearOnTheMastheadIsNotAPageNumber(t *testing.T) {
	// It is set exactly where a folio is set, and it was read as one before the
	// bound was there.
	p := Read(sheet(
		block(line(282, 74, "2010")),
		prose(65, 300, "текст", "статьи", "здесь."),
	))
	if p.Folio != 0 {
		t.Fatalf("the year was read as page %d", p.Folio)
	}
}

func TestTheMastheadIsNotAWordInTwoAlphabets(t *testing.T) {
	// The logo face gives the last letter of Квант as a Latin T and puts a bare
	// dollar after it. Left alone, that is a mixed alphabet word on every page
	// of every issue and a dollar the mathematics can never balance around.
	p := read(prose(282, 74, "КВАНT", "$", "2010/№1"))
	if !strings.Contains(p.Body, "КВАНТ 2010/№1") {
		t.Fatalf("the running head came out as\n%s", p.Body)
	}
	if strings.Contains(p.Body, "$") {
		t.Fatalf("the logo's dollar is still loose in the text:\n%s", p.Body)
	}
}

func TestTheRunningHeadDoesNotJoinTheParagraphUnderIt(t *testing.T) {
	// The head has no full stop and the paragraph under it opens in the middle of
	// a sentence, so the join test says join, and the name of the rubric ends up
	// as the first word of the article.
	p := read(
		prose(282, 74, "МНОГОГРАННЫЙ"),
		prose(65, 300, "течение", "нескольких", "месяцев", "полярной", "ночи."),
	)
	if strings.Contains(p.Body, "МНОГОГРАННЫЙ течение") {
		t.Fatalf("the running head ran into the prose:\n%s", p.Body)
	}
	if !strings.Contains(p.Body, "МНОГОГРАННЫЙ") {
		t.Fatalf("the running head is on the paper and it is gone:\n%s", p.Body)
	}
}

func TestADisplayBannerThatArrivedAsLooseLettersIsCaught(t *testing.T) {
	// The banner down the side of the задачник page, rotated and tracked wide.
	// The text layer has the letters and nothing that says they are a word, and
	// half of this block is single letters exactly, which is where the share was
	// set wrong the first time.
	p := read(block(
		line(228, 64, "КВ"),
		line(228, 66, "Н", "T", "К"),
		line(228, 68, "$", "2", "«"),
		line(228, 70, "0", "1", "К"),
		line(228, 71, "0", "/", "В"),
		line(228, 73, "№", "А"),
		line(228, 75, "1", "Н", "Т", "А", "»"),
		line(228, 76, "ЗАДАЧ"),
		line(228, 77, "Н", "А", "И"),
	))
	if p.Soup != 1 {
		t.Fatalf("%d blocks of letter soup, want the banner:\n%s", p.Soup, p.Body)
	}
}

func TestAParagraphOfRussianIsNotLetterSoup(t *testing.T) {
	// Russian one letter words are among its commonest, and a rule that counted
	// them would send every page of prose in the archive to a model.
	p := read(prose(65, 300,
		"И", "в", "чем", "же", "тут", "дело,", "если", "с", "той", "же", "точки", "у", "нас"))
	if p.Soup != 0 {
		t.Fatalf("a Russian sentence was read as display type:\n%s", p.Body)
	}
}

func TestThePrePressFurnitureIsNotOnThePaper(t *testing.T) {
	// The layout file's name and the time it was saved are set outside the trim.
	// Nobody who has read the magazine has ever seen either of them.
	p := read(
		prose(65, 300, "текст", "статьи", "здесь."),
		prose(65, 845, "61-64.p65"),
		prose(200, 845, "05.02.10,", "17:53"),
	)
	if strings.Contains(p.Body, "p65") || strings.Contains(p.Body, "17:53") {
		t.Fatalf("the typesetter's furniture is in the corpus:\n%s", p.Body)
	}
}

func TestAFigureCaptionIsMarkedTheWayTheVisionLaneMarksIt(t *testing.T) {
	p := read(
		prose(65, 300, "Тогда, как показано на рисунке,"),
		prose(65, 400, "Рис.", "7"),
		prose(65, 450, "площади", "равны."),
	)
	if !strings.Contains(p.Body, FigureMark+"\n\nРис. 7") {
		t.Fatalf("the caption is not marked:\n%s", p.Body)
	}
	if strings.Contains(p.Body, "рисунке, Рис.") {
		t.Fatalf("the caption was joined onto the sentence it interrupts:\n%s", p.Body)
	}
}

func TestTheDropCapitalOpensTheWordAfterIt(t *testing.T) {
	// An article opens with one capital set three lines deep in a block of its
	// own, and without this the first word of the magazine's best writing is
	// missing its first letter.
	p := read(
		block(box(pdfsrc.Line{Words: []pdfsrc.Word{word("П", 65, 300, 3*body)}})),
		prose(90, 300, "усть", "K,", "L,", "M", "середины", "сторон."),
	)
	if !strings.Contains(p.Body, "Пусть") {
		t.Fatalf("the drop capital did not join its word:\n%s", p.Body)
	}
}

func TestTheColumnMarkGoesInWhereTheReadingCrossesTheGutter(t *testing.T) {
	p := read(
		prose(65, 300, "конец", "левой", "колонки."),
		prose(350, 120, "начало", "правой", "колонки."),
	)
	if !strings.Contains(p.Body, ColumnMark) {
		t.Fatalf("no column mark on a two column page:\n%s", p.Body)
	}
	if p.Columns != 2 {
		t.Errorf("%d columns, want 2", p.Columns)
	}
}

func TestASentenceSplitByAPictureIsReadAsOneSentence(t *testing.T) {
	// pdftotext breaks a paragraph wherever a figure sits in it, and a corpus
	// that kept the break would have half sentences all through it.
	p := read(
		prose(65, 300, "Оба неравенства следуют из"),
		prose(65, 400, "очевидного утверждения о площадях."),
	)
	if !strings.Contains(p.Body, "из очевидного") {
		t.Fatalf("the halves of the sentence stayed apart:\n%s", p.Body)
	}
}

func TestAPageWithNoNumberOnItSaysSo(t *testing.T) {
	p := Read(sheet(prose(65, 300, "обложка", "без", "номера")))
	if p.Folio != 0 || !strings.HasPrefix(p.Body, FolioNone) {
		t.Fatalf("folio %d, body opens\n%s", p.Folio, p.Body)
	}
}
