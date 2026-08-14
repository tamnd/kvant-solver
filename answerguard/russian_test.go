package answerguard

import "testing"

// A real page. The prose is Russian and the formulas are Latin, which is what
// every page of this magazine looks like.
const page = `⟦folio 14⟧

## Как измерить высоту дерева

Возьмём прямоугольный треугольник и поставим его так, чтобы один катет
был вертикален. Тогда высота дерева равна $h = d\,\mathrm{tg}\,\alpha + h_0$,
где $d$ есть расстояние до дерева, а $h_0$ высота глаза наблюдателя над
землёй. Проверим это на примере: пусть $d = 20$ м и $\alpha = 40^\circ$.

$$h = 20\,\mathrm{tg}\,40^\circ + 1{,}6 \approx 18{,}4\ \text{м}.$$

Такой способ был известен ещё в древности, и он не требует никаких приборов,
кроме куска картона и линейки.`

func TestARussianPageIsNotComplainedAbout(t *testing.T) {
	if leak, found := Russian(page); found {
		t.Errorf("a page of Russian prose was called a translation: %s", leak.Detail)
	}
}

func TestAPageThatCameBackTranslatedIsCaught(t *testing.T) {
	// The same page, answered in the language the prompt was written in. Every
	// other rule in this package passes this: it is the right length, it has no
	// refusal in it, the delimiters balance and it reads as a transcription.
	english := `⟦folio 14⟧

## How to measure the height of a tree

Take a right triangle and hold it so that one leg is vertical. Then the height
of the tree is $h = d\,\mathrm{tg}\,\alpha + h_0$, where $d$ is the distance to
the tree and $h_0$ is the height of the observer's eye above the ground. Let us
check this with an example: suppose $d = 20$ m and $\alpha = 40^\circ$.

This method was known in antiquity and needs no instruments at all, only a
piece of cardboard and a ruler.`
	leak, found := Russian(english)
	if !found {
		t.Fatal("an English translation of a Russian page was accepted")
	}
	if leak.Kind != "language" {
		t.Errorf("kind is %q, want language", leak.Kind)
	}
}

func TestAPageWithNothingButAFigureIsNotJudged(t *testing.T) {
	// Under MinProse. There is no evidence here either way and a rule that
	// fires anyway costs a re-read of a page that was read correctly.
	if _, found := Russian("⟦figure⟧\nРис. 3\n\n$$S = \\pi r^2$$"); found {
		t.Error("a page with almost no prose on it was judged on its language")
	}
}

func TestAPageOfAlgebraIsNotMistakenForEnglish(t *testing.T) {
	// Latin letters outnumber Cyrillic ones badly here, and every one of them
	// is inside a formula. Counting them would reject a page that is fine.
	algebra := "Пусть $x_1, x_2, \\ldots, x_n$ корни многочлена " +
		"$$P(x) = a_n x^n + a_{n-1} x^{n-1} + \\cdots + a_1 x + a_0,$$ " +
		"тогда по теореме Виета сумма корней равна $-a_{n-1}/a_n$, " +
		"а их произведение равно $(-1)^n a_0 / a_n$ при любом натуральном $n$."
	if leak, found := Russian(algebra); found {
		t.Errorf("a page of algebra was called a translation: %s", leak.Detail)
	}
}

func TestProseKeepsTheWordsAndDropsTheFormulas(t *testing.T) {
	got := Prose(`Пусть $a+b$ и $$\frac{x}{y}$$ конец`)
	for _, want := range []string{"Пусть", "конец"} {
		if !contains(got, want) {
			t.Errorf("Prose dropped %q: %q", want, got)
		}
	}
	for _, gone := range []string{"frac", "a+b"} {
		if contains(got, gone) {
			t.Errorf("Prose kept the mathematics %q: %q", gone, got)
		}
	}
}

func TestAnUnclosedDelimiterStopsRatherThanRunsOn(t *testing.T) {
	// The math rule has already rejected this page. What matters here is that
	// the function returns rather than indexing past the end.
	if got := Prose("Начало $a+b конец без закрытия"); !contains(got, "Начало") {
		t.Errorf("Prose lost the text before an unclosed delimiter: %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
