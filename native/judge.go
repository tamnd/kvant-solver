package native

import (
	"fmt"

	"github.com/tamnd/kvant-solver/ocr"
)

// MaxMathShare is how much of a page can be mathematics and still be read out
// of the text layer.
//
// The formulas that survive the trip are the ones set on the line: a variable
// with a subscript, a power, a unit, an inequality. Those are what a page of
// prose about mathematics carries, and they are perhaps a twentieth of its
// words. A page over this share is a page whose formulas are the point of it,
// and on those the odds of one of them being stacked somewhere the line test
// did not catch stop being small.
const MaxMathShare = 0.12

// Verdict is what the native lane decided about one page.
type Verdict struct {
	// OK is the page being written from the text layer.
	OK bool
	// Rule is which measurement decided it, so that a run over a year can say
	// it sent four hundred pages away for two reasons rather than printing four
	// hundred sentences that differ only in a number.
	Rule Rule
	// Why is that measurement in words, in the same voice as the path decisions
	// in the page manifest, because it goes to the same reader.
	Why string
}

// Rule names one of the measurements a page has to pass.
type Rule string

// The seven of them, in the order Judge applies them.
const (
	RuleCover      Rule = "cover"      // the sheet is a cover or an insert
	RuleUnnumbered Rule = "unnumbered" // the page prints no number
	RuleMisaligned Rule = "misaligned" // the number it prints is not the one expected
	RuleStacked    Rule = "stacked"    // a formula is set on more than one line
	RuleSoup       Rule = "soup"       // a banner or a formula arrived as loose letters
	RuleMathShare  Rule = "math"       // the page is formulas rather than about them
	RuleShort      Rule = "short"      // the text layer holds almost nothing
)

// Expect is what the page manifest already says about a sheet.
type Expect struct {
	// Folio is the number the manifest says is printed on it, and 0 for a
	// sheet the magazine did not number.
	Folio int
	// Cover is a cover or an insert.
	Cover bool
	// Inferred is a page that printed no number and was placed by the pages
	// around it. See Folios. Nothing on the page says which page it is, and the
	// two pages either side of it do, which is enough to file it.
	Inferred bool
}

// Judge decides whether a reconstructed page goes into the corpus or to a
// model.
//
// Every no is a page that costs a model call, so none of them is a hunch. The
// order is the order of how much they say: a page that is not the page it was
// taken for is wrong about everything, and a page whose mathematics is stacked
// is missing something no reader downstream could see was missing.
//
// One thing here cannot see, and kvant native check is what found it. Matter
// the publisher set as a picture has no text under it at all, so it does not
// arrive broken, it does not arrive. Page 56 of the first issue of 2010 carries
// a half page advertisement for the МИФИ correspondence school as a JPEG, the
// text layer has not a word of it, and every measurement below reads the page as
// a clean three hundred and fifty words because the three hundred and fifty
// words it does have are clean. Nothing on the page says the rest is missing.
// The only witness is the photograph, which is why the check exists and why it
// is run rather than reasoned about.
//
// It is not guarded against here. The obvious guard is a page whose images are
// large and whose text does not cover it, and on the one issue there is evidence
// for that costs three good pages to catch the one bad one. Three false
// rejections is a defensible price and a threshold picked off a single page is
// not a defensible way to arrive at it, so this waits for a second issue that
// has both a scan and a file.
func Judge(p Page, expect Expect) Verdict {
	switch {
	case expect.Cover:
		return no(RuleCover, "a cover, which these files carry as a picture and not as text")
	case p.Folio == 0 && !expect.Inferred:
		return no(RuleUnnumbered, "the page prints no number here, so nothing on it says it is the page it was taken for")
	case p.Folio > 0 && expect.Folio > 0 && p.Folio != expect.Folio:
		return no(RuleMisaligned, fmt.Sprintf("the text layer prints %d where the scan has page %d, so the file and the scan are not aligned",
			p.Folio, expect.Folio))
	case p.Soup > 0:
		return no(RuleSoup, fmt.Sprintf("%d blocks came out as loose letters, which is a banner or a formula the text layer could not hold together",
			p.Soup))
	case p.Stacked > 0:
		return no(RuleStacked, fmt.Sprintf("%d formulas are set on more than one line, and a text layer holds the characters of a fraction and not its rule",
			p.Stacked))
	case p.MathShare() > MaxMathShare:
		return no(RuleMathShare, fmt.Sprintf("%.0f%% of the words are mathematics, which is a page of formulas rather than a page about them",
			p.MathShare()*100))
	case len(p.Body) < ocr.MinChars:
		return no(RuleShort, fmt.Sprintf("the text layer holds %d characters, which is a page number and not a page", len(p.Body)))
	}
	return Verdict{OK: true, Why: fmt.Sprintf("%d words of native text, %d of them mathematics, %d scripts put back",
		p.Words, p.Math, p.Scripts)}
}

func no(rule Rule, why string) Verdict { return Verdict{Rule: rule, Why: why} }
