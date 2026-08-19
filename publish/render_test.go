package publish_test

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/publish"
)

func renderer(t *testing.T) *publish.Renderer {
	t.Helper()
	r, err := publish.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func render(t *testing.T, body string) string {
	t.Helper()
	out, bad, err := renderer(t).Render(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 0 {
		t.Fatalf("the body lost %d formulas: %v", len(bad), bad)
	}
	return out
}

// The reason the mathematics comes out before the Markdown parser sees it. A
// subscript is an underscore, an underscore is emphasis, and a parser given
// this first returns a sentence with an italic in the middle of it and two
// stray dollars, with no TeX left anywhere to typeset.
func TestASubscriptIsNotEmphasis(t *testing.T) {
	out := render(t, "Возьмем $x_1 + x_2 = y_1$ и продолжим.\n")

	if strings.Contains(out, "<em>") {
		t.Errorf("got %q, want no emphasis in a formula", out)
	}
	if strings.Contains(out, "$") {
		t.Errorf("got %q, want no delimiters left on the page", out)
	}
	if !strings.Contains(out, "katex") {
		t.Errorf("got %q, want the formula typeset", out)
	}
}

// Emphasis is still emphasis where there is no mathematics, which is the other
// half of the same claim.
func TestEmphasisOutsideAFormulaStillWorks(t *testing.T) {
	out := render(t, "Это *важно* понять.\n")
	if !strings.Contains(out, "<em>") {
		t.Errorf("got %q, want the emphasis kept", out)
	}
}

func TestDisplayMathIsTypesetAsDisplay(t *testing.T) {
	out := render(t, "Отсюда\n\n$$\\frac{1}{2}\\varepsilon$$\n\nи так далее.\n")
	if !strings.Contains(out, "katex-display") {
		t.Errorf("got %q, want a display formula", out)
	}
}

// The corpus holds the magazine's text and none of its pictures. An article
// that says look at the figure is unreadable if the site is silent about where
// the figure was, so the mark survives into the page.
func TestTheStructuralMarksSurvive(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{"a figure", "⟦figure⟧\n", `class="mark figure"`},
		{"a column break", "⟦column⟧\n", `class="mark column"`},
		{"a rubric mark", "⟦rubric⟧\n", `class="mark rubric"`},
		{"a printed page number", "текст ⟦folio 27⟧\n", `id="folio-27"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := render(t, test.body)
			if !strings.Contains(out, test.want) {
				t.Errorf("got %q, want %s in it", out, test.want)
			}
			if strings.Contains(out, "⟦") {
				t.Errorf("got %q, want the bracket itself gone", out)
			}
		})
	}
}

// About a hundred pages came back with a Markdown image where the prompt asked
// for a figure mark, always pointing at a placeholder that names no file
// anywhere. There is no picture to lose, so it becomes the mark it meant, and
// the caption the model read off the sheet comes with it.
func TestAFigureWrittenAsAnImageBecomesTheFigureMark(t *testing.T) {
	out := render(t, "текст\n\n![Рис. 4.](image)\n\nещё текст\n")

	if strings.Contains(out, "<img") {
		t.Errorf("got %q, want no image tag: the site publishes no pictures", out)
	}
	if !strings.Contains(out, `class="mark figure"`) {
		t.Errorf("got %q, want the figure mark in it", out)
	}
	if !strings.Contains(out, "Рис. 4.") {
		t.Errorf("got %q, want the caption kept", out)
	}
}

// The target is not shown and it is not followed either. Every one of them in
// the corpus is a placeholder, and the one that is not would be a link to
// somebody else's scan.
func TestTheImageTargetIsNotPublished(t *testing.T) {
	out := render(t, "![подпись](https://elsewhere.example/scan.jpg)\n")

	if strings.Contains(out, "elsewhere.example") {
		t.Errorf("got %q, want the target gone", out)
	}
	if err := publish.Guard("page.html", []byte(out)); err != nil {
		t.Errorf("the guard refused a rendered figure: %v", err)
	}
}

// A cover or a full page plate prints no number. There is nothing to show and
// nothing to anchor, so the mark leaves without a trace.
func TestAnUnnumberedSheetLeavesNothingBehind(t *testing.T) {
	out := render(t, "⟦folio none⟧\n\nТекст.\n")
	if strings.Contains(out, "folio") {
		t.Errorf("got %q, want no folio markup for a sheet with no number", out)
	}
	if !strings.Contains(out, "Текст") {
		t.Errorf("got %q, want the text kept", out)
	}
}

// The model writes down what it reads off the foot of the sheet, and what is
// printed there is not always a number. Whatever it is must not become part of
// an id or part of the markup.
func TestSomethingThatIsNotAPageNumberDoesNotBecomeMarkup(t *testing.T) {
	out := render(t, `текст ⟦folio "><script>x</script>⟧`+"\n")
	if strings.Contains(out, "<script") {
		t.Fatalf("got %q, want nothing executable on the page", out)
	}
	if strings.Contains(out, "id=") {
		t.Errorf("got %q, want no anchor built out of it", out)
	}
}

// These bodies were written by a model reading a scan, so an angle bracket in
// one of them is a character the magazine printed. Passing it through as markup
// would also be the one hole in the guard, since an img tag would put a request
// for a picture on the page without any file being written.
func TestRawMarkupInABodyIsPrintedAndNotObeyed(t *testing.T) {
	out := render(t, "Сравните <img src=\"http://example.com/scan.jpg\"> и это.\n")
	if strings.Contains(out, "<img") {
		t.Errorf("got %q, want the tag escaped rather than served", out)
	}
	if !strings.Contains(out, "&lt;img") {
		t.Errorf("got %q, want the characters the magazine printed", out)
	}
}

// TeX that does not parse is a fault in the extraction and it is reported as
// one. Printing the raw source and saying nothing would put that fault on the
// page looking deliberate, where nobody would ever go back and fix it, and
// refusing the whole body would lose a page of good text over one formula.
func TestTeXThatDoesNotParseIsMarkedAndCounted(t *testing.T) {
	out, bad, err := renderer(t).Render("Формула $\\frac{1}$ здесь.\n")
	if err != nil {
		t.Fatalf("the whole body was refused over one formula: %v", err)
	}
	if len(bad) != 1 {
		t.Fatalf("reported %d broken formulas, want 1", len(bad))
	}
	if !strings.Contains(out, "tex-failed") {
		t.Errorf("got %q, want the failure marked where it happened", out)
	}
	if !strings.Contains(out, "Формула") {
		t.Errorf("got %q, want the rest of the sentence kept", out)
	}
}

// Nothing above should cost anything on a body that is only prose.
func TestProseWithNoMathematicsComesThroughAsProse(t *testing.T) {
	out := render(t, "## Парабола\n\nПервый абзац.\n\nВторой абзац.\n")
	for _, want := range []string{"<h2", "Парабола", "<p>"} {
		if !strings.Contains(out, want) {
			t.Errorf("got %q, want %s in it", out, want)
		}
	}
}

// Two formulas on one line are the case a naive substitution gets wrong, by
// finding the first copy of the text twice or by pasting the second into the
// first one's place.
func TestTwoFormulasOnOneLineKeepTheirOwnPlaces(t *testing.T) {
	out := render(t, "при $a$ и при $b$ одновременно\n")
	a, b := strings.Index(out, ">a<"), strings.Index(out, ">b<")
	if a < 0 || b < 0 {
		t.Fatalf("got %q, want both formulas typeset", out)
	}
	if a > b {
		t.Errorf("got %q, want them in the order they were written", out)
	}
}
