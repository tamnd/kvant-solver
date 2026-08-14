package ocr_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/ocr"
)

// sheet writes a JPEG the shape of a scanned page, so that a crop of it is a
// crop of something real rather than of a byte slice with jpg on the end. The
// mark stands where the folio is printed, and where that is is the argument,
// because on the real scans it moves.
func sheet(t *testing.T, path string, mark image.Rectangle) {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 1200, 1861))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := mark.Min.Y; y < mark.Max.Y; y++ {
		for x := mark.Min.X; x < mark.Max.X; x++ {
			img.SetGray(x, y, color.Gray{})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := jpeg.Encode(file, img, nil); err != nil {
		t.Fatal(err)
	}
}

// folioAt is where the number sits on a page of the 1975 scans, low in the foot.
var folioAt = image.Rect(60, 1800, 100, 1830)

// The band has to hold the whole of the number and none of the line above it.
// A crop that clips the digits is not a near miss, it is a wrong page number:
// the top half of 51 taken off reads as 31, which is what a fixed band did on
// four sheets of the first issue, and each one was then rejected for
// disagreeing with the manifest.
func TestCropFootTakesTheLineTheFolioIsOn(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "page.jpg")
	out := filepath.Join(dir, "band.jpg")
	sheet(t, in, folioAt)

	if err := ocr.CropFoot(in, out, ocr.FolioBand); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	config, err := jpeg.DecodeConfig(file)
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1200 {
		t.Errorf("the band is %d wide, want the whole page", config.Width)
	}
	// The mark is 30 rows. The band is that plus a little white either side,
	// and nothing like the 74 rows a four per cent band would have taken.
	if config.Height < 32 || config.Height > 60 {
		t.Errorf("the band is %d high, want the 30 row mark and some white", config.Height)
	}
}

// The same page with the number printed higher up the foot. A fixed band is
// measured from the bottom edge, so this is the case it gets wrong, and the
// scans move by nearly four per cent of the page height across one issue.
func TestFootBandFollowsTheFolioUpThePage(t *testing.T) {
	for _, at := range []image.Rectangle{
		image.Rect(60, 1800, 100, 1830), // 2.7 per cent up, near the edge
		image.Rect(60, 1740, 100, 1770), // 6.5 per cent up, the highest seen
	} {
		img := image.NewGray(image.Rect(0, 0, 1200, 1861))
		for i := range img.Pix {
			img.Pix[i] = 0xff
		}
		for y := at.Min.Y; y < at.Max.Y; y++ {
			for x := at.Min.X; x < at.Max.X; x++ {
				img.SetGray(x, y, color.Gray{})
			}
		}
		band := ocr.FootBand(img, ocr.FolioBand)
		if band.Min.Y >= at.Min.Y || band.Max.Y <= at.Max.Y {
			t.Errorf("a folio at %v got the band %v, which does not hold it", at, band)
		}
	}
}

// A full bleed illustration running off the foot is not a page number, and the
// two colour inserts in the first issue are exactly this. Falling back to the
// fixed band gets no number out of them, which is the true answer: those pages
// print none.
func TestFootBandFallsBackOnAPictureThatReachesTheFoot(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 1200, 1861))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	for y := 1500; y < 1861; y++ {
		for x := 40; x < 1160; x++ {
			img.SetGray(x, y, color.Gray{})
		}
	}
	fallback := float64(ocr.FolioBand)
	band := ocr.FootBand(img, fallback)
	if got, want := band.Dy(), int(1861*fallback); got != want {
		t.Errorf("the band is %d high, want the %d row fallback", got, want)
	}
}

func TestCropFootRefusesSomethingThatIsNotAPage(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "page.jpg")
	if err := os.WriteFile(in, []byte("not a jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ocr.CropFoot(in, filepath.Join(dir, "band.jpg"), ocr.FolioBand); err == nil {
		t.Fatal("a file that is not a JPEG was cropped")
	}
}

func TestParseFolioTakesThePageNumberAndNotTheYear(t *testing.T) {
	cases := []struct {
		answer string
		want   int
		found  bool
	}{
		{"44", 44, true},
		{"  \n44\n", 44, true},
		{"1975", 0, false},
		{"", 0, false},
		{"Квант", 0, false},
		// The band on a closing page carries the colophon, and the year in it
		// is not the folio. The number that is a plausible page wins.
		{"1975 80", 80, true},
		// A band is two glyphs with no word around them to say they are a
		// number, so a model reading it as text returns the letters they look
		// like. Sheet 57 of 1975 №1 came back as SS.
		{"SS", 55, true},
		{"```markdown\nSS\n```", 55, true},
		{"S", 5, true},
		// And the narrowness that makes that safe. A word is not a folio, even
		// a short one made of the same letters.
		{"Bis", 0, false},
		{"SOIL", 0, false},
	}
	for _, c := range cases {
		got, found := ocr.ParseFolio(c.answer)
		if got != c.want || found != c.found {
			t.Errorf("ParseFolio(%q) = %d, %v, want %d, %v", c.answer, got, found, c.want, c.found)
		}
	}
}

func TestMarkFolioLeavesAPageThatAnsweredAlone(t *testing.T) {
	text := "⟦folio 12⟧\n\nТекст."
	if got := ocr.MarkFolio(text, 44, true); got != text {
		t.Errorf("a page that answered was rewritten to %q", got)
	}
}

func TestMarkFolioWritesWhatTheBandSaid(t *testing.T) {
	got := ocr.MarkFolio("Текст.", 44, true)
	if !strings.HasPrefix(got, "⟦folio 44⟧\n\n") {
		t.Errorf("got %q, want the folio line first", got)
	}
	if n, ok := ocr.Folio(got); !ok || n != 44 {
		t.Errorf("the marked page reads back as %d, %v", n, ok)
	}
}

// A band with no number in it is a cover, and saying none is the true answer.
// Guessing a number here would put a made up page in the page map, which is
// what everything downstream is placed by.
func TestMarkFolioSaysNoneWhenTheBandHadNoNumber(t *testing.T) {
	got := ocr.MarkFolio("Текст.", 0, false)
	if !strings.HasPrefix(got, ocr.NoFolio) {
		t.Errorf("got %q, want the none line", got)
	}
	if !ocr.HasFolioLine(got) {
		t.Error("a page marked none does not read as having answered")
	}
}

func TestFolioerReadsTheBandThroughTheEngine(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "0044.jpg")
	sheet(t, page, folioAt)

	var asked string
	folio := &ocr.Folioer{Engine: &engine{answer: func(image string, _ int) string {
		asked = image
		return "44"
	}}}
	number, printed, err := folio.Read(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if !printed || number != 44 {
		t.Fatalf("got %d, %v, want 44", number, printed)
	}
	// What the engine saw is the crop and not the page, which is the entire
	// point: the page has body text on it and the crop does not.
	if !strings.HasPrefix(asked, "kvant-folio-") {
		t.Errorf("the engine was shown %q, want the cropped band", asked)
	}
}

// The whole fast lane in one test: a document model that transcribes the page
// and never mentions the number, a band read that does, and a page file that
// ends up with a folio line the rules can use.
func TestTheRunnerTakesTheFolioFromTheBandWhenTheEngineGaveNone(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		if strings.HasPrefix(image, "kvant-folio-") {
			return "2"
		}
		// The same page, with the folio line taken off, which is what a
		// recogniser returns.
		return strings.SplitN(page(2), "\n\n", 2)[1]
	})
	for i := 1; i <= 3; i++ {
		sheet(t, filepath.Join(images, fmt.Sprintf("%04d.jpg", i)), folioAt)
	}
	runner.Folio = &ocr.Folioer{Engine: runner.Engine}

	list, err := ocr.Sheets(images, func(index int) ocr.Expect {
		return ocr.Expect{Issue: "kvant_1975_1", Folio: 2}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Enqueue(list); err != nil {
		t.Fatal(err)
	}
	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Read != 3 {
		t.Fatalf("%s, want three pages read", summary)
	}

	front := &corpus.PageFront{}
	body, err := corpus.Load(runner.Corpus.PagePath("ru",
		corpus.PageID{Issue: runner.Issue, Index: 1}), front)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := ocr.Folio(body); !ok || n != 2 {
		t.Errorf("the page reads back as folio %d, %v, want 2", n, ok)
	}
	if front.PageLabel != "2" {
		t.Errorf("the front matter says page %q, want 2", front.PageLabel)
	}
}

// A Folioer with no engine is the general model lane, where the page prompt
// asks the folio question itself. It answers nothing rather than failing, so
// that the same runner serves both lanes.
func TestAFolioerWithNoEngineSaysNothing(t *testing.T) {
	var folio *ocr.Folioer
	number, printed, err := folio.Read(context.Background(), "nowhere.jpg")
	if err != nil || printed || number != 0 {
		t.Fatalf("got %d, %v, %v, want a quiet nothing", number, printed, err)
	}
}
