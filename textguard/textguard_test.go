package textguard

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/pdfsrc"
)

// The canned poppler output below is the shape the real tools produce for an
// MCCME full issue PDF: six file pages, of which the first two are the cover
// and the contents, and four body pages carrying printed numbers 1 to 4.
const (
	info = `Title:           Квант, 2010, № 1
Producer:        Acrobat Distiller 9.0.0
Pages:           6
Encrypted:       no
Page size:       595.28 x 841.89 pts (A4)
PDF version:     1.4
`

	fontsSubset = `name                                 type              encoding         emb sub uni object ID
------------------------------------ ----------------- ---------------- --- --- --- ---------
KLPBAA+NimbusRomNo9L-Regu            Type 1C           Custom           yes yes yes      9  0
KLPBAB+NimbusRomNo9L-Medi            Type 1C           Custom           yes yes yes     11  0
`

	fontsNone = `name                                 type              encoding         emb sub uni object ID
------------------------------------ ----------------- ---------------- --- --- --- ---------
Helvetica                            Type 1            WinAnsi          no  no  no      10  0
`

	// One picture on file page 5, big enough to be the whole page. A4 is about
	// 97 square inches and this is 85 of them.
	imagesList = `page   num  type   width height color comp bpc  enc interp  object ID x-ppi y-ppi size ratio
   5     0 image    1200  1600  gray    1   8  jpeg   no         12  0   150   150  240K   12%
   3     0 image     300   200  gray    1   8  jpeg   no         14  0   150   150   12K    9%
`
)

// body is a page of set Russian text with its folio on the first line.
func body(folio int, size int) string {
	line := "Квант, задача с числами и буквами, набранная как текст.\n"
	return strconv.Itoa(folio) + "\n" + strings.Repeat(line, size) + "Конец страницы.\n"
}

// text is what pdftotext gives for the whole file: pages separated by form
// feeds, with the file's own numbering and the printed numbering two apart.
func text(page5, page6 string) string {
	pages := []string{
		"Квант\n",
		"В номере:\nЭллипс и парабола\n",
		body(1, 20),
		body(2, 20),
		page5,
		page6,
	}
	return strings.Join(pages, "\f") + "\f"
}

// guard builds a cache holding a pretend PDF and a poppler that answers for it.
func guard(t *testing.T, sheets []fetch.Sheet, fonts, whole string) (*Guard, *fetch.Cache) {
	t.Helper()
	cache, err := fetch.OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum, n, err := cache.Put(strings.NewReader("pretend this is a PDF"))
	if err != nil {
		t.Fatal(err)
	}
	idx := &fetch.Index{
		Issue:  "kvant_2010_1",
		Year:   2010,
		Sheets: sheets,
		PDF:    &fetch.Blob{URL: "https://kvant.ras.ru/pdf/2010/2010-01.pdf", SHA256: sum, Bytes: n},
	}
	if err := cache.WriteIndex(idx); err != nil {
		t.Fatal(err)
	}

	path := cache.Path(sum)
	g := New(cache)
	g.Run = &pdfsrc.FakeRunner{Out: map[string]string{
		"pdfinfo " + path:          info,
		"pdffonts " + path:         fonts,
		"pdftotext " + path + " -": whole,
		"pdfimages -list " + path:  imagesList,
	}}
	g.Log = func(format string, args ...any) { t.Logf(format, args...) }
	return g, cache
}

// scan is a downloaded sheet. Page 0 is a cover, which the magazine does not
// number.
func scan(ord int, file string, page int) fetch.Sheet {
	return fetch.Sheet{
		Ord:  ord,
		File: file,
		Page: page,
		Blob: fetch.Blob{URL: "https://www.kvant.digital/data/kvant_2010_1/jpg/" + file + ".jpg",
			SHA256: strings.Repeat("a", 63) + strconv.Itoa(ord), Bytes: 400000},
	}
}

func sheets() []fetch.Sheet {
	return []fetch.Sheet{
		scan(0, "0000", 0),
		scan(1, "0001", 1),
		scan(2, "0002", 2),
		scan(3, "0003", 3),
		scan(4, "0004", 4),
	}
}

func issue() *manifest.Issue {
	return &manifest.Issue{Key: "kvant_2010_1", Year: 2010, Number: "1", Dir: "2010/01"}
}

func TestABornDigitalIssueIsReadNatively(t *testing.T) {
	g, cache := guard(t, sheets(), fontsSubset, text(body(3, 20), body(4, 20)))

	out, err := g.Issue(context.Background(), issue(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.PDF == nil || !out.PDF.Born {
		t.Fatalf("the file was not measured as born digital: %+v", out.PDF)
	}
	// Printed page 1 is the third page of the file, because the cover and the
	// contents come first. Nothing is told that: it is read off the folios.
	if out.PDF.Offset != 2 {
		t.Errorf("the printed numbering is %d pages behind the file, not 2", out.PDF.Offset)
	}
	if out.Native != 3 {
		t.Errorf("%d pages went native, the file has 4 body pages and one of them is a picture", out.Native)
	}
	// The cover is not in the PDF and no publisher carries it.
	if out.Vision != 2 {
		t.Errorf("%d pages went to vision", out.Vision)
	}

	idx, err := cache.ReadIndex("kvant_2010_1")
	if err != nil {
		t.Fatal(err)
	}
	one, _ := idx.Get("0001")
	if one.Path != Native {
		t.Errorf("printed page 1 took the %s path: %s", one.Path, one.Why)
	}
	if one.Why == "" {
		t.Error("a decision was written with no measurement behind it")
	}
}

func TestAPageOfPicturesGoesToVisionInsideANativeIssue(t *testing.T) {
	// File page 5 is printed page 3, and it is the one the images list covers.
	g, cache := guard(t, sheets(), fontsSubset, text(body(3, 20), body(4, 20)))
	if _, err := g.Issue(context.Background(), issue(), nil); err != nil {
		t.Fatal(err)
	}
	idx, err := cache.ReadIndex("kvant_2010_1")
	if err != nil {
		t.Fatal(err)
	}
	three, _ := idx.Get("0003")
	if three.Path != Vision {
		t.Fatalf("a page that is seven eighths picture took the %s path", three.Path)
	}
	if !strings.Contains(three.Why, "picture") {
		t.Errorf("the reason given was %q", three.Why)
	}
}

func TestAThinTextLayerIsNotTrusted(t *testing.T) {
	// Printed page 4 carries a formula plate: the text layer holds a caption
	// and the mathematics is a picture that pdfimages did not list because it
	// is vector art rather than a raster.
	g, cache := guard(t, sheets(), fontsSubset, text(body(3, 20), "4\nРис. 2.\n"))
	if _, err := g.Issue(context.Background(), issue(), nil); err != nil {
		t.Fatal(err)
	}
	idx, err := cache.ReadIndex("kvant_2010_1")
	if err != nil {
		t.Fatal(err)
	}
	four, _ := idx.Get("0004")
	if four.Path != Vision {
		t.Fatalf("a page with eight characters of text took the %s path", four.Path)
	}
	if !strings.Contains(four.Why, "characters") {
		t.Errorf("the reason given was %q", four.Why)
	}
}

func TestAScanInAPDFWrapperIsNotNativeText(t *testing.T) {
	// This is what the mirror has for the early 2000s: a PDF whose pages are
	// photographs. Taking the existence of a PDF as proof of a text layer is
	// exactly the assumption this milestone exists to remove.
	g, _ := guard(t, sheets(), fontsNone, text(body(3, 20), body(4, 20)))
	out, err := g.Issue(context.Background(), issue(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.PDF.Born {
		t.Fatal("a file with no embedded fonts was called born digital")
	}
	if out.Native != 0 {
		t.Errorf("%d pages were read off a scan", out.Native)
	}
	if out.Vision != len(sheets()) {
		t.Errorf("%d of %d pages went to vision", out.Vision, len(sheets()))
	}
	if !strings.Contains(out.PDF.Why, "scan") {
		t.Errorf("the reason given was %q", out.PDF.Why)
	}
}

func TestAnEncodingMistakeIsRepairedRatherThanPaidFor(t *testing.T) {
	// This is what a third of the files from 2021 on look like: the fonts have
	// no ToUnicode map, so pdftotext hands the CP1251 bytes back as the Latin-1
	// characters standing for them. Calling that a missing text layer would send
	// nine hundred pages of set type through a vision model for nothing.
	garbled := strings.Repeat("Ëâàíò, çàäà÷à ñ ÷èñëàìè.\n", 40)
	whole := strings.Join([]string{"Ëâàíò\n", "Â íîìåðå:\n", garbled, garbled, garbled, garbled}, "\f") + "\f"
	g, _ := guard(t, sheets(), fontsSubset, whole)

	out, err := g.Issue(context.Background(), issue(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.PDF.Born {
		t.Fatalf("a repairable text layer was thrown away: %s", out.PDF.Why)
	}
	if out.PDF.Recode != pdfsrc.CP1251 {
		t.Errorf("the manifest says to read the text back as %q", out.PDF.Recode)
	}
	if !strings.Contains(out.PDF.Why, pdfsrc.CP1251) {
		t.Errorf("the reason given was %q", out.PDF.Why)
	}
}

func TestTextThatIsNotRussianInAnyEncodingIsNotATextLayer(t *testing.T) {
	// A text layer in the wrong encoding is worse than none, because it looks
	// like success all the way to a corpus full of nonsense. The repair above is
	// only allowed to run when it makes the page more Russian than it was.
	garbled := strings.Repeat("Lvant, zadacha s chislami and more filler.\n", 40)
	whole := strings.Join([]string{"Lvant\n", "V nomere:\n", garbled, garbled, garbled, garbled}, "\f") + "\f"
	g, _ := guard(t, sheets(), fontsSubset, whole)

	out, err := g.Issue(context.Background(), issue(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.PDF.Born {
		t.Fatal("a text layer with no Russian in it was called born digital")
	}
	if out.PDF.Recode != "" {
		t.Errorf("a repair that changed nothing was written down as %q", out.PDF.Recode)
	}
	if !strings.Contains(out.PDF.Why, "Russian") {
		t.Errorf("the reason given was %q", out.PDF.Why)
	}
}

func TestPublisherTextSavesAVisionCall(t *testing.T) {
	g, cache := guard(t, sheets(), fontsSubset, text(body(3, 20), "4\nРис. 2.\n"))
	rows := []manifest.Row{
		{Title: "Эллипс", Page: 1, HasText: false, Source: "kvant_digital"},
		{Title: "Парабола", Page: 4, HasText: true, Slug: "parabola", Source: "kvant_digital"},
	}
	out, err := g.Issue(context.Background(), issue(), rows)
	if err != nil {
		t.Fatal(err)
	}
	if out.Publisher != 1 {
		t.Fatalf("%d pages took the publisher path, one article has text", out.Publisher)
	}
	idx, err := cache.ReadIndex("kvant_2010_1")
	if err != nil {
		t.Fatal(err)
	}
	four, _ := idx.Get("0004")
	if four.Path != Publisher {
		t.Errorf("printed page 4 took the %s path: %s", four.Path, four.Why)
	}
}

func TestASheetThatWasNeverDownloadedIsNotDecided(t *testing.T) {
	rest := sheets()
	rest[2].Blob = fetch.Blob{}
	g, _ := guard(t, rest, fontsSubset, text(body(3, 20), body(4, 20)))

	out, err := g.Issue(context.Background(), issue(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Undecided != 1 {
		t.Errorf("%d sheets are undecided, one has no bytes", out.Undecided)
	}
}

func TestAnIssueWithNoScanSaysSo(t *testing.T) {
	cache, err := fetch.OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := New(cache)
	out, err := g.Issue(context.Background(), issue(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Total() != 0 || !strings.Contains(out.Note, "fetch pages") {
		t.Errorf("got %d pages and the note %q", out.Total(), out.Note)
	}
}

func TestTheOffsetNeedsMoreThanOneAgreement(t *testing.T) {
	// Two pages that happen to open with a number are a coincidence, and a
	// coincidence must not move the whole issue's page mapping.
	pages := []string{"7\nчто-то\n", "Квант\n", "Квант\n", "Квант\n"}
	if off, votes := offset(pages); off != 0 || votes != 0 {
		t.Errorf("%d folio moved the mapping by %d", votes, off)
	}
}

func TestAnArticleCoversThePagesUpToTheNextOne(t *testing.T) {
	rows := []manifest.Row{
		{Title: "Эллипс", Page: 3, HasText: true},
		{Title: "Парабола", Page: 6},
		{Title: "Задачник", Page: 9, Pages: []int{9, 12}, HasText: true},
	}
	pub := publisherPages(rows)
	for _, p := range []int{3, 4, 5, 9, 12} {
		if !pub[p] {
			t.Errorf("page %d is not covered", p)
		}
	}
	for _, p := range []int{2, 6, 7, 8, 10, 11} {
		if pub[p] {
			t.Errorf("page %d is covered and should not be", p)
		}
	}
}

func TestAFolioUnderARunningHeadStillCounts(t *testing.T) {
	// This is how the mirror's own PDFs are set: the running head first, the
	// page number under it, and nothing at the foot of the page.
	var pages []string
	for p := 1; p <= 8; p++ {
		pages = append(pages, fmt.Sprintf("КВАНT 2007/№1\n\n%d\n\nтекст страницы\n", p+2))
	}
	off, votes := offset(pages)
	if off != -2 {
		t.Errorf("the printed numbering runs two ahead of the file and the offset came out %d", off)
	}
	// Six of the eight vote. The last two print folios past the end of this
	// eight page extract, and a folio larger than the file is not a folio.
	if votes != 6 {
		t.Errorf("%d pages voted", votes)
	}
}

func TestAnEquationNumberIsNotAFolio(t *testing.T) {
	pages := []string{"текст\n(3)\n", "текст\n(4)\n", "текст\n(5)\n", "текст\n(6)\n"}
	if off, _ := offset(pages); off != 0 {
		t.Errorf("equation numbers moved the mapping by %d", off)
	}
}

func TestPagesAreCutOnTheFormFeeds(t *testing.T) {
	pages := splitPages("one\ftwo\f", 2)
	if len(pages) != 2 || strings.TrimSpace(pages[1]) != "two" {
		t.Fatalf("got %q", pages)
	}
	// A file whose text stops short still has a slot for every page, so an
	// index into the result is a page number.
	if pages := splitPages("one\f", 3); len(pages) != 3 {
		t.Errorf("got %d pages for a three page file", len(pages))
	}
}

func TestAHandfulOfEquationNumbersDoesNotRenumberAnIssue(t *testing.T) {
	// The 2021 double issue prints its folios in Symbol, so pdftotext reads
	// none of them, and seven numbers loose in the text agreed on an offset of
	// fifty one. Seven out of sixty eight is not a numbering scheme.
	pages := make([]string, 68)
	for i := range pages {
		pages[i] = "КВАНТ\nтекст без колонцифры\nещё текст\n"
	}
	for _, i := range []int{51, 52, 53, 54, 55, 56, 57} {
		pages[i] = "1\nтекст\n"
	}
	if off, votes := offset(pages); off != 0 || votes != 0 {
		t.Errorf("%d numbers moved a 68 page file by %d", votes, off)
	}
}
