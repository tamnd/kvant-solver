package answerguard

import (
	"strings"
	"testing"
)

func TestRefusalsAreCaught(t *testing.T) {
	for _, text := range []string{
		"I'm sorry, I can't help with that.",
		"I am unable to transcribe this image.",
		"As an AI language model, I cannot read images.",
		"I apologize, but the image is not clear enough.",
	} {
		leaks := Check(text)
		if len(leaks) == 0 {
			t.Errorf("not caught: %q", text)
			continue
		}
		if leaks[0].Kind != "refusal" {
			t.Errorf("%q came back as %s, want refusal", text, leaks[0].Kind)
		}
	}
}

func TestNarrationIsCaughtEvenWhenTheRestLooksRight(t *testing.T) {
	// This is the dangerous one. The page under the first line may be a
	// perfectly good transcription, or the model may have summarised it, and
	// from the text alone there is no telling which.
	text := strings.Join([]string{
		"Here is the transcription of the image:",
		"",
		"A I.24  ALGEBRAIC STRUCTURES  § 4",
		"",
		"**Proposition 4.** — Let $G$ be a group.",
	}, "\n")
	leaks := Check(text)
	if len(leaks) != 1 || leaks[0].Kind != "meta" {
		t.Fatalf("narration not caught: %+v", leaks)
	}
	if leaks[0].Line != 1 {
		t.Fatalf("narration reported on line %d, want 1", leaks[0].Line)
	}
}

func TestClosingRemarksAreCaught(t *testing.T) {
	text := "A I.24\n\nSome real content here.\n\nLet me know if you need anything else!"
	leaks := Check(text)
	if len(leaks) != 1 {
		t.Fatalf("closing remark not caught: %+v", leaks)
	}
	if leaks[0].Line != 5 {
		t.Fatalf("reported on line %d, want 5", leaks[0].Line)
	}
}

func TestThePromptComingBackIsCaught(t *testing.T) {
	text := "Transcribe the complete text and all mathematical content from this image"
	leaks := Check(text)
	if len(leaks) == 0 || leaks[0].Kind != "prompt" {
		t.Fatalf("the prompt echoed back was not caught: %+v", leaks)
	}
}

func TestEmptyIsALeak(t *testing.T) {
	for _, text := range []string{"", "   ", "\n\n\t\n"} {
		leaks := Check(text)
		if len(leaks) != 1 || leaks[0].Kind != "empty" {
			t.Fatalf("%q came back as %+v", text, leaks)
		}
	}
}

func TestARealPagePassesClean(t *testing.T) {
	// A page that carries the words the checks look for. The magazine quotes
	// English, prints English names, and its book reviews carry English titles.
	// Anything here that fails is a false positive that would reject a good
	// page and cost two and a half minutes to read again.
	pages := []string{
		`⟦folio 14⟧

⟦rubric⟧ Задачник «Кванта»

М1234. Докажите, что для любого натурального $n$ выполняется
$$\sum_{k=1}^{n} k^3 = \left(\frac{n(n+1)}{2}\right)^2.$$

⟦figure⟧
Рис. 3`,
		`⟦folio 41⟧

⟦rubric⟧ Рецензии

Вышла книга M. Gardner, Mathematical Puzzles and Diversions, о которой мы
писали в прошлом номере. Автор пишет: "I cannot imagine a better way to
begin", и с этим трудно не согласиться.`,
	}
	for i, page := range pages {
		if leaks := Check(page); len(leaks) != 0 {
			t.Errorf("page %d was rejected: %+v", i+1, leaks)
		}
	}
}

func TestFencesAreStrippedNotRejected(t *testing.T) {
	text := "```markdown\n⟦folio 24⟧\n\n⟦rubric⟧ Задачник «Кванта»\n```"
	got := Strip(text)
	if strings.Contains(got, "```") {
		t.Fatalf("the fence survived:\n%s", got)
	}
	if !strings.HasPrefix(got, "⟦folio 24⟧") {
		t.Fatalf("stripping ate the content:\n%s", got)
	}
	// A fence inside the page, around a diagram, is content and stays.
	inner := "⟦folio 24⟧\n\n```diagram\nA --> B\n```\n\nmore text"
	if Strip(inner) != inner {
		t.Fatalf("an inner fence was stripped:\n%s", Strip(inner))
	}
}

func TestNormaliseFixesWhatIsUnambiguouslyWrong(t *testing.T) {
	got := Normalise(`Отсюда $h = d\tan\alpha$ и $\cot\beta = 1/\tan\beta$.   `)
	for _, want := range []string{`\mathrm{tg}`, `\mathrm{ctg}`} {
		if !strings.Contains(got, want) {
			t.Errorf("normalise did not produce %q: %s", want, got)
		}
	}
	if strings.Contains(got, `\tan`) || strings.Contains(got, `\cot`) {
		t.Errorf("an Anglicised function name survived: %s", got)
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("trailing space survived: %q", got)
	}
}

func TestNormaliseLeavesMathematicsAlone(t *testing.T) {
	// The minus sign, the two dash lengths, the quotation marks and the decimal
	// comma all mean something here. A normaliser that flattened any of them
	// would be changing what the page says.
	text := `$-1$ and $x - y$, the range 1–3, the rule — and "quotes", and $3{,}14$`
	if got := Normalise(text); got != text {
		t.Fatalf("normalise changed mathematics:\n%s\n%s", text, got)
	}
}

func TestCleanAgreesWithCheck(t *testing.T) {
	if !Clean("⟦folio 24⟧\n\n⟦rubric⟧ Задачник «Кванта»") {
		t.Error("a good page is not clean")
	}
	if Clean("I'm sorry, I can't do that") {
		t.Error("a refusal is clean")
	}
}

// The three answers below are verbatim from the first live batch on server3, 10
// August 2026. Every one of them was written into the corpus as a page of
// a page of an issue, because the apostrophe was the typographic one and no phrase here
// was spelled with it.
func TestAnAnswerFromAModelThatNeverGotThePage(t *testing.T) {
	answers := []string{
		"I don’t see an image attached to this message. Please upload the page image, and I’ll transcribe it exactly according to your specifications.",
		"I don’t see the image attached. Please upload the page image, and I’ll transcribe it exactly according to your specifications.",
		"Please upload the image page you want transcribed.",
	}
	for _, answer := range answers {
		leaks := Check(answer)
		if len(leaks) == 0 {
			t.Fatalf("no leak found in:\n%s", answer)
		}
		if leaks[0].Kind != "no-image" {
			t.Errorf("kind = %q, want no-image, in:\n%s", leaks[0].Kind, answer)
		}
	}
}

// The apostrophe fix is not only about the new phrases. Every refusal in the
// list was spelled with the ASCII one and a model writes the other.
func TestARefusalWithATypographicApostropheIsStillARefusal(t *testing.T) {
	for _, answer := range []string{
		"I’m sorry, I can’t transcribe this page.",
		"I’m unable to help with that.",
		"I can’t help with copyrighted material.",
	} {
		leaks := Check(answer)
		if len(leaks) == 0 || leaks[0].Kind != "refusal" {
			t.Errorf("no refusal found in %q: %+v", answer, leaks)
		}
	}
}

// Mathematics that talks about images must survive, and so must a page that
// quotes an English sentence, which the book reviews do.
func TestTheWordImageInMathematicsIsNotALeak(t *testing.T) {
	for _, page := range []string{
		"⟦folio 24⟧\n\nThe image of $f$ is a subgroup of $H$.",
		"Let $N$ be the inverse image of the identity element under $f$.",
		"We do not see any reason to distinguish the two images here.",
		"The image is not attached to any particular choice of basis, as we show below.",
	} {
		if leaks := Check(page); len(leaks) > 0 && leaks[0].Kind == "no-image" {
			t.Errorf("mathematics read as a failed upload: %q in %q", leaks[0].Detail, page)
		}
	}
}

// The magazine prints tg where an English book prints tan, and a model trained
// mostly on English writes what it knows however plainly the prompt says not
// to. It is a substitution rather than a rejection because the formula still
// means the same thing and a re-read is two and a half minutes.
func TestTheRussianFunctionNamesAreRestored(t *testing.T) {
	cases := map[string]string{
		`$h = d\tan\alpha$`:          `$h = d\mathrm{tg}\alpha$`,
		`$\arctan x + \cot y$`:       `$\mathrm{arctg} x + \mathrm{ctg} y$`,
		`$\operatorname{tg}\varphi$`: `$\mathrm{tg}\varphi$`,
	}
	for text, want := range cases {
		if got := Normalise(text); got != want {
			t.Errorf("normalise(%q)\n got %q\nwant %q", text, got, want)
		}
	}
}

// The provider's own formatting, which is the leak with no English sentence in
// it. A retranslation of the appendix on the Nullstellensatz came back inside a
// :::writing fence and passed all seven translation rules, because the fence
// lines had no blank line around them and so joined the paragraphs either side.
func TestProviderMarkupIsRefused(t *testing.T) {
	cases := []struct {
		name, text string
		want       bool
	}{
		{"a directive fence", ":::writing{variant=\"document\" id=\"58321\"}\nCho A là một vành.\n:::", true},
		{"an indented fence", "Cho A là một vành.\n  ::: ", true},
		{"a citation anchor", "the ring A 【4:0†source】 is local", true},
		{"a private use character", "the ring A \ue203 is local", true}, // written as an escape, since it prints as nothing
		{"a section with three colons in the mathematics", "the map $A ::: B$ is one", false},
		{"ordinary prose", "Cho A là một vành giao hoán.", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var found bool
			for _, l := range Check(c.text) {
				if l.Kind == "markup" {
					found = true
				}
			}
			if found != c.want {
				t.Errorf("Check(%q) markup = %v, want %v: %v", c.text, found, c.want, Check(c.text))
			}
		})
	}
}

// A display written the other way is turned into the corpus's way rather than
// sent back for another call. Exercise 2 of § 1 came back with twenty of them.
func TestADisplayWrittenWithBracketsIsTurnedRound(t *testing.T) {
	got := Normalise("The equality\n\\[\nsrs(M)=s(M).\n\\]\nholds, and \\(s\\neq 0\\).\n")
	want := "The equality\n$$\nsrs(M)=s(M).\n$$\nholds, and $s\\neq 0$.\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	// The escapes that are not delimiters are left alone, and \\[ in a matrix
	// row is a line break rather than the start of a display.
	for _, keep := range []string{`\{x\}`, `\begin{pmatrix}1\\2\end{pmatrix}`, `a\,b`,
		`\begin{aligned}a&=b\\[2pt]c&=d\end{aligned}`} {
		if got := Normalise(keep); got != keep {
			t.Errorf("Normalise(%q) = %q", keep, got)
		}
	}
}

// The line is the unit, and a line with Russian on it is a line off the page.
func TestAnEnglishQuotationInARussianSentenceIsNotARefusal(t *testing.T) {
	page := `⟦folio 41⟧

⟦rubric⟧ Рецензии

Автор пишет: "I'm sorry to say that the problem remains open", и приводит
ссылку на работу 1968 года.`
	if leaks := Check(page); len(leaks) != 0 {
		t.Errorf("a quoted English sentence was read as a refusal: %+v", leaks)
	}
	// The same phrase on a line of its own, which is what a real refusal looks
	// like, still has to be caught.
	if leaks := Check("I'm sorry, I can't transcribe this image."); len(leaks) == 0 {
		t.Error("a refusal on its own line was accepted")
	}
}
