package translate

import (
	"context"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/glossary"
)

// fake answers without a server and keeps every request it was sent.
type fake struct {
	calls  []api.Request
	answer func(i int, r api.Request) string
}

func (f *fake) Complete(_ context.Context, r api.Request) (api.Response, error) {
	f.calls = append(f.calls, r)
	text := r.Input
	if f.answer != nil {
		text = f.answer(len(f.calls)-1, r)
	}
	return api.Response{Model: "test-model", Text: text, Usage: api.Usage{OutputTokens: 10}}, nil
}

func engine(f *fake) *Engine { return &Engine{Client: f} }

func TestAnUnknownLanguageIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	f := &fake{}
	_, err := engine(f).Translate(context.Background(),
		Job{Lang: "fr", Body: "Текст."}, nil, Options{})
	if err == nil {
		t.Fatal("a language this corpus does not translate into was accepted")
	}
	if len(f.calls) != 0 {
		t.Fatalf("%d calls were made for a language that was going to be refused", len(f.calls))
	}
}

func TestAnEmptyBodyCostsNothing(t *testing.T) {
	f := &fake{}
	res, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "   \n\n  "}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("a blank page cost %d calls", len(f.calls))
	}
	if res.Body != "" {
		t.Fatalf("a blank page came back as %q", res.Body)
	}
}

func TestOnlyTheGlossaryRowsTheBodyUsesGoIntoThePrompt(t *testing.T) {
	// The glossary is the largest part of the prompt and a file shown every row
	// is a file that goes stale whenever anybody touches anything.
	g := &glossary.Glossary{Version: 1, Terms: []glossary.Term{
		{RU: "ток", EN: "current"},
		{RU: "дифракция", EN: "diffraction"},
	}}
	f := &fake{}
	res, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "Через проводник идёт ток."}, g, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Terms) != 1 || res.Terms[0].RU != "ток" {
		t.Fatalf("the file was shown %d rows: %+v", len(res.Terms), res.Terms)
	}
	instructions := f.calls[0].Instructions
	if !strings.Contains(instructions, "ток = current") {
		t.Fatalf("the row the body uses is not in the prompt:\n%s", instructions)
	}
	if strings.Contains(instructions, "diffraction") {
		t.Fatal("a row the body never uses was sent anyway")
	}
}

func TestALongBodyIsSentInPiecesAndComesBackWhole(t *testing.T) {
	var b strings.Builder
	for i := range 40 {
		b.WriteString(strings.Repeat("текст ", 80))
		b.WriteString(string(rune('А' + i%30)))
		b.WriteString("\n\n")
	}
	body := b.String()

	f := &fake{}
	res, err := engine(f).Translate(context.Background(), Job{Lang: "en", Body: body}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Chunks < 2 {
		t.Fatalf("a body of %d characters went in one call", len(body))
	}
	if len(f.calls) != res.Chunks {
		t.Fatalf("%d chunks took %d calls", res.Chunks, len(f.calls))
	}
	// The fake echoes its input, so the blocks have to come back in order and
	// unchanged.
	if got, want := Blocks(res.Body), Blocks(body); len(got) != len(want) {
		t.Fatalf("%d blocks went in and %d came out", len(want), len(got))
	}
}

func TestAChunkThatMangledTheMathematicsIsAskedAgain(t *testing.T) {
	// A page whose algebra was quietly retyped is worse than a page that was
	// never translated, because it looks finished.
	f := &fake{answer: func(i int, r api.Request) string {
		if i == 0 {
			return "The area is $a^{2}$ here."
		}
		return "The area is $a^2$ here."
	}}
	res, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "Площадь равна $a^2$ здесь."}, nil, Options{Retries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("a mangled formula took %d calls", len(f.calls))
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("the retry succeeded and the run still warned: %v", res.Warnings)
	}
}

func TestAChunkThatKeepsManglingTheMathematicsIsReported(t *testing.T) {
	// The retry is bounded, and what survives it has to be visible rather than
	// written out as though it were fine.
	f := &fake{answer: func(int, api.Request) string { return "The area is $b^2$ here." }}
	res, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "Площадь равна $a^2$ здесь."}, nil, Options{Retries: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("expected the one retry the options allowed, got %d calls", len(f.calls))
	}
	if len(res.Warnings) == 0 {
		t.Fatal("a formula that came back rewritten was not reported")
	}
}

func TestARetryIsNotSpentOnAChunkThatCameBackClean(t *testing.T) {
	f := &fake{}
	if _, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "Площадь равна $a^2$."}, nil, Options{Retries: 3}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("a clean chunk took %d calls", len(f.calls))
	}
}

func TestAFencedReplyIsUnwrapped(t *testing.T) {
	// Models fence a whole reply as a code block however plainly they are told
	// not to, and a corpus of Markdown pages wrapped in backticks renders as
	// listings.
	f := &fake{answer: func(int, api.Request) string {
		return "```markdown\n# Heading\n\nSome prose.\n```"
	}}
	res, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "# Заголовок"}, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(res.Body, "```") {
		t.Fatalf("the fence survived: %q", res.Body)
	}
	if !strings.Contains(res.Body, "# Heading") {
		t.Fatalf("unwrapping ate the content: %q", res.Body)
	}
}

func TestACodeBlockInsideThePageIsLeftAlone(t *testing.T) {
	page := "Some prose.\n\n```\nlisting\n```"
	if got := clean(page); got != page {
		t.Fatalf("a fence that is part of the document was stripped: %q", got)
	}
}

func TestVerifyCountsSpansBothWays(t *testing.T) {
	if c := Verify("Дано $a$ и $b$.", "Given $a$ and $b$."); len(c) != 0 {
		t.Fatalf("a clean translation was complained about: %v", c)
	}
	if c := Verify("Дано $a$ и $b$.", "Given $a$."); len(c) == 0 {
		t.Fatal("a dropped formula was not noticed")
	}
	if c := Verify("Дано $a$.", "Given $a$ and $b$."); len(c) == 0 {
		t.Fatal("an invented formula was not noticed")
	}
}

func TestVerifyNoticesADisplayTurnedIntoAnInline(t *testing.T) {
	// A display that came back inline is a rendering change, not a translation,
	// and it is exactly the kind of thing that survives a proofread by somebody
	// who does not read the source.
	if c := Verify("$$a = b$$", "$a = b$"); len(c) == 0 {
		t.Fatal("a display collapsed to an inline was not noticed")
	}
}

func TestVerifyNoticesAnUnclosedSpan(t *testing.T) {
	if c := Verify("Дано $a$.", "Given $a."); len(c) == 0 {
		t.Fatal("a span left open was not noticed")
	}
}

func TestAChunkIsToldItIsAChunk(t *testing.T) {
	// Without this it writes an opening sentence for a piece that starts in the
	// middle and rounds off a piece that does not end.
	var b strings.Builder
	for range 40 {
		b.WriteString(strings.Repeat("текст ", 80))
		b.WriteString("\n\n")
	}
	f := &fake{}
	if _, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: b.String()}, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) < 2 {
		t.Fatalf("expected several chunks, got %d", len(f.calls))
	}
	for i, call := range f.calls {
		if !strings.Contains(call.Instructions, "part") {
			t.Fatalf("call %d was not told it is one part of a longer text", i+1)
		}
	}
}

func TestASingleChunkIsNotToldItIsAPartOfAnything(t *testing.T) {
	f := &fake{}
	if _, err := engine(f).Translate(context.Background(),
		Job{Lang: "en", Body: "Короткий текст."}, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.calls[0].Instructions, "part 1 of 1") {
		t.Fatal("a whole page was told it is part of something longer")
	}
}

func TestTheTargetLanguageIsNamedRatherThanCoded(t *testing.T) {
	f := &fake{}
	if _, err := engine(f).Translate(context.Background(),
		Job{Lang: "vi", Body: "Текст."}, nil, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.calls[0].Instructions, "Vietnamese") {
		t.Fatalf("the prompt asks for vi rather than Vietnamese:\n%s", f.calls[0].Instructions)
	}
}
