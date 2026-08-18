package native

import "testing"

func TestTheSymbolsThisMagazineSetsBecomeLaTeX(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"α", `\alpha`},
		{"≥", `\ge`},
		{"≠", `\ne`},
		{"π", `\pi`},
		{"Δ", `\Delta`},
		{"30°", `30^\circ`},
		{"−5", "-5"},
	} {
		if got := tex(c.in); got != c.want {
			t.Errorf("tex(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAMacroDoesNotSwallowTheLetterAfterIt(t *testing.T) {
	// \alphax is not a parse error. It is a different formula, and one nothing
	// downstream would flag.
	if got := tex("αx"); got != `\alpha x` {
		t.Fatalf("tex(%q) = %q", "αx", got)
	}
}

func TestADashIsPunctuationAndNotAMinusSign(t *testing.T) {
	// A table that called the dash mathematics would pull the Russian sentence
	// around it into a formula, and this magazine uses the dash for the verb to
	// be in nearly every other sentence.
	for _, in := range []string{"–", "—", "-"} {
		if got := tex(in); got != in {
			t.Errorf("tex(%q) = %q, and a dash in prose is not an operator", in, got)
		}
	}
}

func TestADollarFromTheFontCannotOpenAFormula(t *testing.T) {
	// The logo face leaves one behind. Left as it is, it closes the formula it
	// stands in and opens another over the prose after it, which is a page of
	// nonsense out of a single bad glyph.
	if got := tex("A$"); got != `A\$` {
		t.Fatalf("tex(%q) = %q", "A$", got)
	}
}

func TestASymbolTheTableDoesNotKnowIsLeftWhereItCanBeSeen(t *testing.T) {
	// Dropping it would make the page look complete and be wrong. Keeping it
	// makes the gap visible in the corpus and fixable here.
	if got := tex("⨀"); got != "⨀" {
		t.Fatalf("tex(%q) = %q, want it left alone", "⨀", got)
	}
}
