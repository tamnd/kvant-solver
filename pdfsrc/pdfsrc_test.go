package pdfsrc

import (
	"context"
	"slices"
	"testing"
)

// The canned output is real, trimmed. The file is the January 2010 issue from
// the MCCME mirror, born digital and set in subset Type 1C.
const (
	info2010 = `Title:           Квант. 2010. № 1
Producer:        Acrobat Distiller 8.1.0 (Windows)
CreationDate:    Mon Feb  8 12:41:03 2010
Pages:           68
Encrypted:       no
Page size:       481.89 x 680.315 pts
PDF version:     1.4
`
	fonts2010 = `name                                 type              encoding         emb sub uni object ID
------------------------------------ ----------------- ---------------- --- --- --- ---------
NPHNJC+PragmaticaC                   Type 1C           Custom           yes yes yes     14  0
NPHNJD+NewtonC                       Type 1C           WinAnsi          yes yes yes     21  0
Helvetica                            Type 1            Standard         no  no  no      33  0
`
	images2010 = `page   num  type   width height color comp bpc  enc interp  object ID x-ppi y-ppi size ratio
   4     0 image     680   492  rgb     3   8  jpeg   no        58  0   150   150  204K  20%
`
)

func fake() *FakeRunner {
	return &FakeRunner{Out: map[string]string{
		"pdfinfo /tmp/kvant_2010_1.pdf":                   info2010,
		"pdffonts /tmp/kvant_2010_1.pdf":                  fonts2010,
		"pdfimages -list -f 4 -l 4 /tmp/kvant_2010_1.pdf": images2010,
	}}
}

func src() *Source { return &Source{Path: "/tmp/kvant_2010_1.pdf", Run: fake()} }

func TestInfo(t *testing.T) {
	info, err := src().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Pages != 68 {
		t.Errorf("pages is %d", info.Pages)
	}
	if info.Encrypted {
		t.Error("the file is not encrypted")
	}
	// The page size is what an image has to be measured against, so a page
	// size that did not parse would make every coverage fraction meaningless.
	if info.WidthPt != 481.89 || info.HeightPt != 680.315 {
		t.Errorf("page is %v by %v pt", info.WidthPt, info.HeightPt)
	}
}

func TestAMissingPageCountIsAnError(t *testing.T) {
	s := &Source{Path: "/tmp/x.pdf", Run: &FakeRunner{Out: map[string]string{
		"pdfinfo /tmp/x.pdf": "Producer: nothing useful\n",
	}}}
	if _, err := s.Info(context.Background()); err == nil {
		t.Error("pdfinfo said nothing about pages and that was accepted")
	}
}

func TestFonts(t *testing.T) {
	fonts, err := src().Fonts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(fonts) != 3 {
		t.Fatalf("got %d fonts", len(fonts))
	}
	first := fonts[0]
	if first.Type != "Type 1C" {
		t.Errorf("type is %q, the column is two words wide", first.Type)
	}
	if !first.Embedded || !first.Subset {
		t.Errorf("%s parsed as embedded=%v subset=%v", first.Name, first.Embedded, first.Subset)
	}
	// The stock font is the one that would be read as embedded if the columns
	// were counted from the left instead of from the right.
	if fonts[2].Embedded {
		t.Errorf("%s is not embedded", fonts[2].Name)
	}
}

func TestImages(t *testing.T) {
	images, err := src().Images(context.Background(), 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 1 {
		t.Fatalf("got %d images", len(images))
	}
	im := images[0]
	if im.Width != 680 || im.Height != 492 {
		t.Errorf("image is %d by %d", im.Width, im.Height)
	}
	// The object id column is two fields wide. Counting past it wrong is how
	// the resolution comes back as zero and every area calculation collapses.
	if im.XPPI != 150 || im.YPPI != 150 {
		t.Errorf("resolution is %d by %d ppi", im.XPPI, im.YPPI)
	}
	if im.Enc != "jpeg" {
		t.Errorf("encoding is %q", im.Enc)
	}
}

func TestMeasureTextCountsRussian(t *testing.T) {
	// A real page of the magazine, near enough: Russian prose with a formula
	// and a page number in it.
	m := MeasureText(7, "Задача 3. Докажите, что x + y = 5 при любом n.\n7\n")
	if m.Runes == 0 {
		t.Fatal("nothing counted")
	}
	if m.Cyrillic < 0.6 {
		t.Errorf("cyrillic share is %.2f, the line is mostly Russian", m.Cyrillic)
	}
}

func TestMeasureTextOnAnEmptyLayer(t *testing.T) {
	// A scan with no text layer returns page separators and nothing else. It
	// must not come back as a page with a high Cyrillic share and no letters.
	m := MeasureText(7, "\f\n")
	if m.Runes != 0 || m.Cyrillic != 0 {
		t.Errorf("got %d runes and %.2f cyrillic", m.Runes, m.Cyrillic)
	}
}

func TestSpreadPagesStaysInTheBody(t *testing.T) {
	got := SpreadPages(68, 5)
	if len(got) != 5 {
		t.Fatalf("got %v", got)
	}
	if got[0] < 17 || got[len(got)-1] > 51 {
		t.Errorf("%v strays out of the middle half of a 68 page issue", got)
	}
	if !slices.IsSorted(got) {
		t.Errorf("%v is not in order", got)
	}
}

func TestSpreadPagesOnAShortFile(t *testing.T) {
	got := SpreadPages(3, 10)
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("got %v, a three page file has three pages to sample", got)
	}
}

func TestTheFlagsArePinned(t *testing.T) {
	// Changing the flags of a poppler call changes what is being measured, so
	// it is worth having to change a test to do it.
	f := fake()
	s := &Source{Path: "/tmp/kvant_2010_1.pdf", Run: f}
	if _, err := s.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Images(context.Background(), 4, 4); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"pdfinfo /tmp/kvant_2010_1.pdf",
		"pdfimages -list -f 4 -l 4 /tmp/kvant_2010_1.pdf",
	}
	if !slices.Equal(f.Seen, want) {
		t.Errorf("ran %v", f.Seen)
	}
}

func TestCP1251ComesBackOutOfLatin1(t *testing.T) {
	// The left hand side is what pdftotext hands back for the mirror's own
	// 2021 file, verbatim, and the right hand side is what the page says.
	cases := []struct{ got, want string }{
		{"ÊÂÀÍT  2021/¹11-12", "КВАНT  2021/№11-12"},
		{"LTE-ëåììà è ðåêóððåíòû", "LTE-лемма и рекурренты"},
		{"Ï.ÊÎÆÅÂÍÈÊÎÂ", "П.КОЖЕВНИКОВ"},
	}
	for _, c := range cases {
		out, ok := RecodeCP1251(c.got)
		if !ok || out != c.want {
			t.Errorf("%q came back as %q, wanted %q", c.got, out, c.want)
		}
	}
}

func TestTextThatCameOutRightIsLeftAlone(t *testing.T) {
	// Russian that pdftotext already understood holds characters no single byte
	// stands for, and running the repair over it would destroy it.
	in := "Многогранный Делоне"
	if out, ok := RecodeCP1251(in); ok || out != in {
		t.Errorf("good text was repaired into %q", out)
	}
}
