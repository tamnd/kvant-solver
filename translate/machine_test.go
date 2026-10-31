package translate

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

// fakeMachine replaces every Cyrillic word with "vi" and, when lossy, drops
// the placeholders from any text that has some, the way a real service now
// and then does.
type fakeMachine struct {
	lossy bool
	sent  []string
}

var word = regexp.MustCompile(`\p{Cyrillic}+`)

func (f *fakeMachine) Translate(_ context.Context, _, _ string, texts []string) ([]string, error) {
	out := make([]string, len(texts))
	for i, t := range texts {
		f.sent = append(f.sent, t)
		t = word.ReplaceAllString(t, "vi")
		if f.lossy {
			t = placeholder.ReplaceAllString(t, "")
		}
		out[i] = t
	}
	return out, nil
}

func drafted(t *testing.T, m *fakeMachine, body string) Result {
	t.Helper()
	e := &Engine{Machine: m}
	res, err := e.Translate(context.Background(), Job{Lang: "vi", Body: body}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestADraftKeepsEveryFormulaByteForByte(t *testing.T) {
	body := "Пусть $n^2+1$ делится на $p$, тогда\n\n$$\n\\sum_{k=1}^n k = \\frac{n(n+1)}{2}\n$$\n\nи всё."
	res := drafted(t, &fakeMachine{}, body)
	if c := Verify(body, res.Body); len(c) != 0 {
		t.Fatalf("%v\n%s", c, res.Body)
	}
	if strings.Contains(res.Body, "Пусть") || !strings.Contains(res.Body, "$$\n\\sum") {
		t.Fatalf("got %q", res.Body)
	}
}

func TestTheServiceNeverSeesAFormula(t *testing.T) {
	m := &fakeMachine{}
	drafted(t, m, "Если $x>0$ и $$y=1$$, то ⟦figure⟧ видно.")
	for _, s := range m.sent {
		if strings.ContainsAny(s, "$") || strings.Contains(s, "figure") {
			t.Fatalf("sent %q", s)
		}
	}
}

func TestALostPlaceholderIsRecoveredPieceByPiece(t *testing.T) {
	body := "Пусть $a$ больше $b$ и меньше $c$."
	res := drafted(t, &fakeMachine{lossy: true}, body)
	if c := Verify(body, res.Body); len(c) != 0 {
		t.Fatalf("%v\n%s", c, res.Body)
	}
	if res.Body != "vi $a$ vi $b$ vi vi $c$." {
		t.Fatalf("got %q", res.Body)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("a recovered line was not reported")
	}
}

func TestMarkdownMarkersStayOutsideTheText(t *testing.T) {
	m := &fakeMachine{}
	res := drafted(t, m, "## Заголовок\n\n- пункт один\n1. пункт два\n> цитата\n")
	want := "## vi\n\n- vi vi\n1. vi vi\n> vi\n"
	if res.Body != want {
		t.Fatalf("got %q, want %q", res.Body, want)
	}
	for _, s := range m.sent {
		if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, ">") {
			t.Fatalf("sent marker %q", s)
		}
	}
}

func TestATableIsDraftedCellByCell(t *testing.T) {
	m := &fakeMachine{}
	res := drafted(t, m, "| Число | $x|y$ |\n|---|---|\n| два | $а$ и $б$ |\n")
	want := "| vi | $x|y$ |\n|---|---|\n| vi | $а$ vi $б$ |\n"
	if res.Body != want {
		t.Fatalf("got %q, want %q", res.Body, want)
	}
	for _, s := range m.sent {
		if strings.Contains(s, "$") {
			t.Fatalf("sent %q", s)
		}
	}
}

func TestAProblemLabelComesBackAsTheCorpusSpellsIt(t *testing.T) {
	res := drafted(t, &fakeMachine{}, "Задача М123 и Ф45 решены. См. задачу М7.")
	want := "Bài toán M123 vi F45 vi. vi. bài toán M7."
	if res.Body != want {
		t.Fatalf("got %q, want %q", res.Body, want)
	}
}

func TestTheMagazineNameIsNotTranslated(t *testing.T) {
	res := drafted(t, &fakeMachine{}, "Читатели «Кванта» пишут.")
	if res.Body != "vi «Kvant» vi." {
		t.Fatalf("got %q", res.Body)
	}
}

func TestALineWithNothingToTranslateIsNotSent(t *testing.T) {
	m := &fakeMachine{}
	res := drafted(t, m, "$x=1$\n\n⟦folio 12⟧\n\n12345\n")
	if len(m.sent) != 0 {
		t.Fatalf("sent %q", m.sent)
	}
	if res.Body != "$x=1$\n\n⟦folio 12⟧\n\n12345\n" {
		t.Fatalf("got %q", res.Body)
	}
}

func TestADraftIsStampedAsADraft(t *testing.T) {
	res := drafted(t, &fakeMachine{}, "Текст.")
	if res.Model != DraftModel || res.Prompt != DraftVersion {
		t.Fatalf("stamped %q %q", res.Model, res.Prompt)
	}
	stamp := Stamp("k", corpusProvenance("h"), "Текст.", nil, "vi", res.Model, "r")
	if stamp.TranslationQuality != "draft" {
		t.Fatalf("quality %q", stamp.TranslationQuality)
	}
}

func TestALongParagraphIsCutAtSentenceEnds(t *testing.T) {
	s := strings.Repeat("Это предложение. ", 600)
	parts := sentences(strings.TrimSpace(s), draftCallChars)
	if len(parts) < 2 {
		t.Fatalf("%d parts", len(parts))
	}
	for _, p := range parts {
		if len(p) > draftCallChars || !strings.HasSuffix(p, ".") {
			t.Fatalf("part of %d bytes ending %q", len(p), p[len(p)-5:])
		}
	}
}

func corpusProvenance(sha string) corpus.Provenance { return corpus.Provenance{ContentSHA256: sha} }

func TestTheMagazinesOwnWordsComeBackAsTheModelSpellsThem(t *testing.T) {
	res := drafted(t, &fakeMachine{}, "Решение задачи. Ответ: да. Задачник «Кванта» ждёт задачник.")
	want := "Lời giải bài toán. Đáp số: vi. Bài toán Kvant vi vi."
	if res.Body != want {
		t.Fatalf("got %q, want %q", res.Body, want)
	}
}

func TestAnIndentedLineKeepsItsFormulas(t *testing.T) {
	m := &fakeMachine{}
	body := "   Пусть $а=б$ и   $$в$$ тогда.  "
	res := drafted(t, m, body)
	if res.Body != "   vi $а=б$ vi   $$в$$ vi.  " {
		t.Fatalf("got %q", res.Body)
	}
}

// reorder swaps the two halves of a text round a comma, the way a translation
// moves a clause.
type reorder struct{}

func (reorder) Translate(_ context.Context, _, _ string, texts []string) ([]string, error) {
	out := make([]string, len(texts))
	for i, t := range texts {
		if a, b, ok := strings.Cut(t, ", "); ok {
			t = b + ", " + a
		}
		out[i] = t
	}
	return out, nil
}

func TestAFormulaMayTravelWithItsClause(t *testing.T) {
	e := &Engine{Machine: reorder{}}
	res, err := e.Translate(context.Background(), Job{Lang: "vi", Body: "при $V=50$, найдите $m$"}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Body != "найдите $m$, при $V=50$" {
		t.Fatalf("got %q", res.Body)
	}
}
