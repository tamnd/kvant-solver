package kvantdigital

import (
	"strings"
	"testing"
)

func TestAFirstPublicationNoteIsNotTheArticle(t *testing.T) {
	// Every article page has a text block. On a page from a year with no
	// publisher text it holds a line saying where the piece first appeared,
	// and reading that as the article would mark half the archive as needing
	// no transcription.
	a, err := ParseArticle(open(t, "article_scan_only.html"), "bronshteyn-ellips-d2e5763b")
	if err != nil {
		t.Fatal(err)
	}
	if a.HasText {
		t.Errorf("the first publication note counted as %d runes of article text", a.TextRunes)
	}
	if a.Text != "" {
		t.Error("text was kept for an article that has none")
	}
}

func TestPublisherTextIsKeptAsMarkup(t *testing.T) {
	a, err := ParseArticle(open(t, "article_with_text.html"), "aslamazov-lunnyiy_tormoz-312a44df")
	if err != nil {
		t.Fatal(err)
	}
	if !a.HasText {
		t.Fatalf("no publisher text found, the block holds %d runes", a.TextRunes)
	}
	// Flattening the block to a string throws the mathematics and the figures
	// away, and those are the parts worth having.
	for _, want := range []string{"<p>", "<figure", "class=\"tex\""} {
		if !strings.Contains(a.Text, want) {
			t.Errorf("the kept text has no %s in it", want)
		}
	}
}

func TestSheetsComeOffTheSlides(t *testing.T) {
	a, err := ParseArticle(open(t, "article_scan_only.html"), "bronshteyn-ellips-d2e5763b")
	if err != nil {
		t.Fatal(err)
	}
	// Four slides, one of them repeated: the viewer duplicates the last sheet
	// of a spread and a duplicate would be fetched twice.
	want := []string{"0004", "0005", "0006"}
	if len(a.SheetFiles) != len(want) {
		t.Fatalf("got %v", a.SheetFiles)
	}
	for i, w := range want {
		if a.SheetFiles[i] != w {
			t.Errorf("sheet %d is %q, want %q", i, a.SheetFiles[i], w)
		}
	}
}

func TestTheDownloadLinkIsReadAndNotBuilt(t *testing.T) {
	a, err := ParseArticle(open(t, "article_scan_only.html"), "bronshteyn-ellips-d2e5763b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.DownloadURL, "https://www.kvant.digital/rpc/dl/o/?payload=") {
		t.Errorf("download url is %q", a.DownloadURL)
	}
}
