package ocr_test

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

func body(t *testing.T) ocr.Expect {
	t.Helper()
	return ocr.Expect{Issue: "kvant_1975_1", Sheet: 4, Folio: 2}
}

// The run this rule was written for. Every other rule passed these pages: they
// were the right length, in Russian, with balanced mathematics and the correct
// folio, and the words themselves were the only thing wrong with them.
func TestAPageWithLatinWeldedIntoRussianWordsIsRejected(t *testing.T) {
	text := page(2) + strings.Repeat(
		"\nПри dvижении тележки надо найти Funcцию, которая описывает teхнику "+
			"и её svойства, а затем проверить otvет по таблице.\n", 1)

	problems := ocr.Validate(text, body(t), ocr.Options{})
	if len(problems) == 0 {
		t.Fatal("a page with five welded words was accepted")
	}
	if problems[0].Rule != ocr.RuleScript {
		t.Fatalf("rejected by %s, want %s", problems[0].Rule, ocr.RuleScript)
	}
	// The message names one, because the first thing anyone does with this
	// report is go and look at the page.
	if !strings.Contains(problems[0].Detail, "dvижении") {
		t.Errorf("the reason is %q, want it to quote the first bad word", problems[0].Detail)
	}
}

// A good read of these scans still produces the odd one honestly, so the rule
// has an allowance. Rejecting on the first would send back most of the corpus
// over a single misread capital.
func TestAPageWithOneMisreadLetterIsAccepted(t *testing.T) {
	text := page(2) + "\nЗадача была предложена на олимпиаде MГУ в прошлом году.\n"
	for _, problem := range ocr.Validate(text, body(t), ocr.Options{}) {
		if problem.Rule == ocr.RuleScript {
			t.Fatalf("one misread letter was rejected: %s", problem.Detail)
		}
	}
}

// The mathematics is Latin by definition and it touches the Russian around it.
// Counting words inside a math span would report every page in the corpus, so
// the spans come out before the words are looked at.
func TestMathematicsIsNotAScriptProblem(t *testing.T) {
	text := page(2) + "\nОбозначим $\\alpha$ и $R_{max}$, тогда $S = \\pi R^2$ " +
		"и $v_{кон}$ выражается через $t$, где $x_{нач}$ задано условием.\n"
	for _, problem := range ocr.Validate(text, body(t), ocr.Options{}) {
		if problem.Rule == ocr.RuleScript {
			t.Fatalf("a page whose only Latin is mathematics was rejected: %s", problem.Detail)
		}
	}
}

// Latin next to Russian is not Latin inside it. This magazine prints variable
// names, instrument names and foreign surnames in its prose on most pages.
func TestALatinWordAmongRussianOnesIsFine(t *testing.T) {
	text := page(2) + "\nПрибор Nikon и таблица log были использованы в опыте, " +
		"о котором писал Maxwell в своей известной работе.\n"
	for _, problem := range ocr.Validate(text, body(t), ocr.Options{}) {
		if problem.Rule == ocr.RuleScript {
			t.Fatalf("a Latin word standing on its own was rejected: %s", problem.Detail)
		}
	}
}

// The page that made the rule general. The Latin only version read a Han
// character in the middle of a Russian word as ordinary text, and a run that is
// loose enough to reach for one alphabet reaches for whichever is nearest.
func TestAWordCarryingACharacterFromAThirdScriptIsRejected(t *testing.T) {
	text := page(2) + "\nПрибор показал длину в с怠митрах, а масса в gраммах " +
		"была измерена дважды, поэтому таблица пере一считана и проβерена, " +
		"а затем сноβа сверена с образцом.\n"

	problems := ocr.Validate(text, body(t), ocr.Options{})
	found := false
	for _, problem := range problems {
		if problem.Rule == ocr.RuleScript {
			found = true
		}
	}
	if !found {
		t.Fatal("a page with Han and Greek welded into Russian words was accepted")
	}
}

// Rule 9 exists because of three pages that went into the corpus and should
// not have. A model decoding greedily fell into a loop and emitted the same
// syllable for two thousand nine hundred letters, and every other rule passed
// it: the page is long rather than short, there is no mathematics left to
// unbalance, the folio is right, and a loop in Cyrillic never leaves the
// alphabet for rule 8 to notice.
func TestARunawayDecoderIsCaught(t *testing.T) {
	cases := []struct {
		name string
		word string
		want bool
	}{
		{"a syllable repeated to the end of the context", strings.Repeat("медыси", 400), false},
		{"two letters repeated", strings.Repeat("зы", 800), false},
		{"the longest real word in the corpus", "центростремительнымускорением", true},
		{"a misread with the spaces eaten", "некоторыхfundamentальныхпроблемах", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text := page(2) + "\n\n" + c.word + " и дальше обычный текст страницы.\n"
			problems := ocr.Validate(text, body(t), ocr.Options{})
			runaway := false
			for _, p := range problems {
				if p.Rule == ocr.RuleRunaway {
					runaway = true
				}
			}
			if runaway == c.want {
				t.Fatalf("rule 9 said %v, want %v, on %d letters", runaway, !c.want, len([]rune(c.word)))
			}
		})
	}
}

// The audit has to ask the same question of pages written before the rule
// existed, which is what LongestWord is for.
func TestLongestWordFindsTheRun(t *testing.T) {
	n, word := ocr.LongestWord("Обычная строка, а затем " + strings.Repeat("зы", 50) + " и конец.")
	if n != 100 {
		t.Fatalf("the longest run is %d letters, want 100", n)
	}
	if !strings.HasPrefix(word, "зызы") {
		t.Fatalf("got %q, want the repeated run", clipTest(word))
	}
}

// The exemption to rule 1, and the four things it has to keep apart.
//
// The first case is a real page: 1975 issue 5 sheet 31 is one plate of
// epicycloids, and the pipeline prompt run against the actual image returns
// exactly this, 42 characters of it. The other three are what a short page
// looks like when it is short because something went wrong.
func TestAFullPageFigureIsNotAShortPage(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		short bool
	}{
		{
			"the plate as the prompt asks for it",
			"⟦folio 2⟧\n\n⟦figure⟧\nРис. 9. Эпициклоиды.\n",
			false,
		},
		{
			// 1975 issue 5 sheet 35, which is two plates and the rule and
			// signature line the magazine sets at the foot of every sheet.
			"two plates and the printer's furniture",
			"⟦folio 2⟧\n\n⟦figure⟧\nРис. 3.\n\n⟦column⟧\n\n⟦figure⟧\nРис. 4.\n\n---\n3 «Квант» № 5\n",
			false,
		},
		{
			"a model that stopped in the prose",
			"⟦folio 2⟧\n\nРассмотрим окружность радиуса $R$, катящуюся без скольжения по\n",
			true,
		},
		{
			"a model that stopped at the marker",
			"⟦folio 2⟧\n\n⟦figure⟧\n",
			true,
		},
		{
			"a plate with no folio line to say which page it is",
			"⟦figure⟧\nРис. 9. Эпициклоиды.\n",
			true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			short := false
			for _, problem := range ocr.Validate(c.text, body(t), ocr.Options{}) {
				if problem.Rule == ocr.RuleShort {
					short = true
				}
			}
			if short != c.short {
				t.Fatalf("rule 1 said short=%v, want %v, on %d characters",
					short, c.short, len([]rune(c.text)))
			}
		})
	}
}

// A page carrying a figure is not a plate. The exemption is for the sheet the
// figure takes over, and a column of text next to one is a page that has to
// clear the floor like any other, so a truncated read of it still fails.
func TestAFigureInAPageOfTextDoesNotExemptIt(t *testing.T) {
	text := "⟦folio 2⟧\n\n⟦figure⟧\nРис. 9. Эпициклоиды.\n\n" +
		"Точка на ободе катящегося колеса описывает кривую, и первые\n"
	if ocr.Plate(text) {
		t.Fatal("a page with prose beside the figure was called a plate")
	}
	short := false
	for _, problem := range ocr.Validate(text, body(t), ocr.Options{}) {
		if problem.Rule == ocr.RuleShort {
			short = true
		}
	}
	if !short {
		t.Fatal("a truncated page was accepted because it happened to carry a figure")
	}
}

func clipTest(s string) string {
	if r := []rune(s); len(r) > 20 {
		return string(r[:20]) + "..."
	}
	return s
}

// Rule 8 was rejecting the chess pages, and it was wrong about every one of
// them. Шахматная страничка prints its moves the Russian way, a Cyrillic piece
// letter against a Latin square, so a correct read of one board mixes two
// alphabets twenty times over.
func TestChessNotationIsNotAMisread(t *testing.T) {
	text := page(2) + "\nБелые: Крg1, Фd3, Лf1, Сc4, Кe5. Чёрные: Крh8, Фa5, Лd8.\n" +
		"Решение: 1. Фh7+ Крf8 2. Фh8+ Крe7 3. Лf7+ Крd6 4. Кc4+ Крc5.\n"
	for _, problem := range ocr.Validate(text, body(t), ocr.Options{}) {
		if problem.Rule == ocr.RuleScript {
			t.Fatalf("a chess page was rejected: %s", problem.Detail)
		}
	}
}

// And the exemption has to stay the size of a chess move. The run that made
// rule 8 necessary is still the thing it is for.
func TestChessNotationDoesNotHideAWeldedWord(t *testing.T) {
	text := page(2) + "\nБелые: Крg1, Фd3. При dvижении тележки надо найти Funcцию, " +
		"которая описывает teхнику и её svойства, а затем проверить otvет.\n"
	found := false
	for _, problem := range ocr.Validate(text, body(t), ocr.Options{}) {
		if problem.Rule == ocr.RuleScript {
			found = true
		}
	}
	if !found {
		t.Fatal("a page with five welded words was accepted because it also had chess on it")
	}
}
