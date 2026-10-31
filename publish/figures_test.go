package publish

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/figures"
)

func TestTheGuardLetsThroughOnlyItsOwnFigures(t *testing.T) {
	own := figureMarkup("<p>⟦img 1970/09/0016-1.jpg 531x527⟧</p>")
	if !strings.Contains(own, "<figure") {
		t.Fatalf("no figure in %q", own)
	}
	if err := Guard("a.html", []byte(own)); err != nil {
		t.Errorf("own figure refused: %v", err)
	}
	for _, page := range []string{
		`<img src="https://example.com/x.jpg">`,
		`<img src="https://kvant-figures.tamnd.com/1970/09/0016-1.jpg" onerror="x" width="1" height="1" loading="lazy" alt="">`,
	} {
		if Guard("a.html", []byte(page)) == nil {
			t.Errorf("let through %q", page)
		}
	}
	if out := figureMarkup("<p>⟦img ../../scan.jpg 1x1⟧</p>"); strings.Contains(out, "<img") {
		t.Errorf("a sentinel naming another path became %q", out)
	}
}

func TestAFigureGoesOverItsMarkAndAboveABareCaption(t *testing.T) {
	m := &figures.Manifest{Figures: []figures.Entry{
		{Page: 5, N: 1, Key: "рис1маятник", File: "1970/09/0005-1.jpg", Width: 10, Height: 20},
		{Page: 5, N: 2, Key: "рис2струя", File: "1970/09/0005-2.jpg", Width: 30, Height: 40},
	}}
	body := "Один.\n\n⟦figure⟧\n\nРис. 1. Маятник\n\nДва.\n\nРис. 2. Струя\n"
	front := corpus.ArticleFront{PageFirst: 4, PageLast: 5}
	anchors := figures.Anchors(body)
	got := insert(body, anchors, placeAt(m, front, anchors))
	want := "Один.\n\n\n⟦img 1970/09/0005-1.jpg 10x20⟧\n\n\nРис. 1. Маятник\n\nДва.\n\n\n⟦img 1970/09/0005-2.jpg 30x40⟧\n\nРис. 2. Струя\n"
	if got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	// a figure from a page the article does not cover is not its own
	front = corpus.ArticleFront{PageFirst: 7, PageLast: 9}
	if got := insert(body, anchors, placeAt(m, front, anchors)); got != body {
		t.Errorf("placed a figure from another article's page: %q", got)
	}
}
