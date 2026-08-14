package kvantdigital

import (
	"strings"
	"testing"
)

func TestParsePages(t *testing.T) {
	// The fixture is the first five and last four entries of the real page
	// list for 1975 number 1, which is enough to cover every shape of sheet
	// the viewer emits.
	sheets, err := ParsePages(open(t, "view_1975_1.html"), "kvant_1975_1")
	if err != nil {
		t.Fatal(err)
	}
	// Nine entries in the fixture, but the first two share an anchor: the
	// viewer opens with a spacer that repeats the cover so the two page spread
	// lands the right way round. Counting it would double the cover.
	if len(sheets) != 8 {
		t.Fatalf("got %d sheets, want 8 after the spacer is dropped", len(sheets))
	}

	first := sheets[0]
	if first.Ord != 0 || first.File != "0000" {
		t.Errorf("first sheet is ord %d file %q", first.Ord, first.File)
	}
	if first.Numbered() {
		t.Errorf("the front cover came back as printed page %d", first.Page)
	}
	if first.Label != "Обложка" {
		t.Errorf("first label is %q", first.Label)
	}
}

func TestSheetNumberingIsNotArithmetic(t *testing.T) {
	sheets, err := ParsePages(open(t, "view_1975_1.html"), "kvant_1975_1")
	if err != nil {
		t.Fatal(err)
	}
	// Printed page 1 is the fourth sheet and lives in the file called 0001,
	// and printed page 2 is the fifth sheet in the file called 0002. Every one
	// of those three numbers is different, which is why all three are kept.
	one, ok := SheetForPage(sheets, 1)
	if !ok {
		t.Fatal("printed page 1 is not in the list")
	}
	if one.Ord != 2 || one.File != "0001" {
		t.Errorf("printed page 1 is ord %d file %q, want ord 2 file 0001", one.Ord, one.File)
	}
	two, ok := SheetForPage(sheets, 2)
	if !ok {
		t.Fatal("printed page 2 is not in the list")
	}
	if two.Ord != 3 || two.File != "0002" {
		t.Errorf("printed page 2 is ord %d file %q, want ord 3 file 0002", two.Ord, two.File)
	}
	if _, ok := SheetForPage(sheets, 500); ok {
		t.Error("a page the issue does not have came back as found")
	}
}

func TestCoverBacksHaveLetteredFiles(t *testing.T) {
	sheets, err := ParsePages(open(t, "view_1975_1.html"), "kvant_1975_1")
	if err != nil {
		t.Fatal(err)
	}
	last := sheets[len(sheets)-1]
	// The back cover is filed as a suffix on the last numbered page, not as
	// sheet 81. Anything that guesses the file name from the sheet number gets
	// this wrong, and gets it wrong for every issue in the archive.
	if last.File != "0080_b" {
		t.Errorf("last file is %q, want 0080_b", last.File)
	}
	if last.Ord != 83 {
		t.Errorf("last sheet is ord %d, want 83", last.Ord)
	}
	if last.Numbered() {
		t.Error("the back cover came back as a numbered page")
	}
	if got := ScanURL("kvant_1975_1", last.File); got != "https://www.kvant.digital/data/kvant_1975_1/jpg/0080_b.jpg" {
		t.Errorf("ScanURL: %s", got)
	}
}

func TestEveryNumberedSheetHasAnImage(t *testing.T) {
	sheets, err := ParsePages(open(t, "view_1975_1.html"), "kvant_1975_1")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sheets {
		if !strings.HasPrefix(s.ImageURL, "https://www.kvant.digital/data/kvant_1975_1/jpg/") {
			t.Errorf("sheet %d has image url %q", s.Ord, s.ImageURL)
		}
	}
}

func TestParsePagesRefusesAPageWithNoSheets(t *testing.T) {
	_, err := ParsePages(strings.NewReader("<html><body></body></html>"), "kvant_1975_1")
	if err == nil {
		t.Fatal("an empty page list should be an error, not an empty slice")
	}
}
