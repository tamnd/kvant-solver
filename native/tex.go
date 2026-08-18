package native

import "strings"

// The characters the text layer of these files uses for mathematics, and what
// they are in LaTeX.
//
// The magazine is set in a Cyrillic TeX and the mirror's PDFs carry the glyphs
// with proper Unicode behind them, so a formula arrives as real characters
// rather than as the private use soup an old distiller produces. That makes the
// translation a table and not a guess, and a table is worth having because
// everything downstream of the corpus, the KaTeX check included, reads LaTeX
// and not Unicode.
//
// Only the characters that actually turn up are here. A symbol this table does
// not know is left exactly as it was printed, which is visible in the corpus
// and fixable, rather than dropped, which is not.
var texOf = map[rune]string{
	'α': `\alpha`, 'β': `\beta`, 'γ': `\gamma`, 'δ': `\delta`,
	'ε': `\varepsilon`, 'ζ': `\zeta`, 'η': `\eta`, 'θ': `\theta`,
	'ι': `\iota`, 'κ': `\kappa`, 'λ': `\lambda`, 'μ': `\mu`, 'µ': `\mu`,
	'ν': `\nu`, 'ξ': `\xi`, 'π': `\pi`, 'ρ': `\rho`, 'σ': `\sigma`,
	'τ': `\tau`, 'υ': `\upsilon`, 'φ': `\varphi`, 'ϕ': `\phi`, 'χ': `\chi`,
	'ψ': `\psi`, 'ω': `\omega`,
	'Γ': `\Gamma`, 'Δ': `\Delta`, '∆': `\Delta`, 'Θ': `\Theta`,
	'Λ': `\Lambda`, 'Ξ': `\Xi`, 'Π': `\Pi`, 'Σ': `\Sigma`,
	'Φ': `\Phi`, 'Ψ': `\Psi`, 'Ω': `\Omega`,

	'∞': `\infty`, '∈': `\in`, '∉': `\notin`, '∅': `\emptyset`,
	'∩': `\cap`, '∪': `\cup`, '⊂': `\subset`, '⊃': `\supset`,
	'∀': `\forall`, '∃': `\exists`, '∧': `\wedge`, '∨': `\vee`,
	'≤': `\le`, '≥': `\ge`, '≠': `\ne`, '≈': `\approx`, '≡': `\equiv`,
	'∼': `\sim`, '≅': `\cong`, '∝': `\propto`,
	'±': `\pm`, '∓': `\mp`, '×': `\times`, '÷': `\div`,
	'⋅': `\cdot`, '·': `\cdot`, '∙': `\cdot`,
	'√': `\sqrt`, '∑': `\sum`, '∏': `\prod`, '∫': `\int`, '∂': `\partial`,
	'∇': `\nabla`, '∠': `\angle`, '⊥': `\perp`, '∥': `\parallel`,
	'→': `\to`, '←': `\leftarrow`, '↔': `\leftrightarrow`,
	'⇒': `\Rightarrow`, '⇐': `\Leftarrow`, '⇔': `\Leftrightarrow`,
	'°': `^\circ`, '′': `'`, '″': `''`,
	// The minus sign, which is not the hyphen the keyboard has. The dashes are
	// deliberately absent: a dash in this magazine is nearly always punctuation
	// in a Russian sentence, and a table that called it mathematics would pull
	// the sentence around it into a formula.
	'−': `-`,
}

// tex writes one word of a formula as LaTeX.
func tex(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if out, ok := texOf[r]; ok {
			b.WriteString(out)
			// A macro needs a space after it or it swallows the next letter, and
			// \alphax is not a parse error, it is a different formula.
			if strings.HasPrefix(out, `\`) {
				b.WriteByte(' ')
			}
			continue
		}
		// A dollar that came out of the file is a glyph the font had no character
		// for, never the start of a formula. One left as it is closes the
		// formula it is standing in and opens another one over the prose after
		// it, which is a page of nonsense from a single bad glyph.
		if r == '$' {
			b.WriteString(`\$`)
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// isTeXMath reports whether a character is one the table above knows, which is
// the same thing as saying the word it sits in is mathematics.
func isTeXMath(r rune) bool {
	_, ok := texOf[r]
	return ok
}
