package native

import (
	"strings"
	"testing"
)

// full is a page that passes everything, so that each test below changes the
// one thing it is about.
func full() Page {
	return Page{Folio: 64, Body: strings.Repeat("слово ", 200), Words: 700, Math: 40, Scripts: 12}
}

func TestAPageOfProseAboutMathematicsIsReadFromTheFile(t *testing.T) {
	v := Judge(full(), Expect{Folio: 64})
	if !v.OK {
		t.Fatalf("rejected: %s", v.Why)
	}
	if !strings.Contains(v.Why, "12 scripts put back") {
		t.Errorf("the yes says %q, and it should say what was measured", v.Why)
	}
}

func TestAPageWithAFractionOnItGoesToAModel(t *testing.T) {
	// The one that matters. The characters of the fraction are all in the file
	// and the rule between them is not, so a page written from the text layer
	// here is wrong in a way nothing downstream could see.
	p := full()
	p.Stacked = 3
	v := Judge(p, Expect{Folio: 64})
	if v.OK {
		t.Fatal("a page with stacked formulas was written from its text layer")
	}
	if !strings.Contains(v.Why, "3 formulas") {
		t.Errorf("the reason is %q and does not say how many", v.Why)
	}
}

func TestAPageThatIsMostlyFormulasGoesToAModel(t *testing.T) {
	// Nothing here is stacked, so no single measurement objects. What objects is
	// the share: at a quarter of the words the page is a page of formulas, and
	// the odds of one of them being set in a way the line test cannot see stop
	// being small.
	p := full()
	p.Math = 300
	if v := Judge(p, Expect{Folio: 64}); v.OK {
		t.Fatalf("a page that is %.0f%% mathematics was taken as prose", p.MathShare()*100)
	}
}

func TestAPageThatDisagreesWithTheManifestIsNotTheePageItWasTakenFor(t *testing.T) {
	p := full()
	p.Folio = 62
	v := Judge(p, Expect{Folio: 64})
	if v.OK {
		t.Fatal("the file and the scan are two pages apart and the page was written anyway")
	}
	if !strings.Contains(v.Why, "62") || !strings.Contains(v.Why, "64") {
		t.Errorf("the reason is %q and should carry both numbers", v.Why)
	}
}

func TestAPageWithNoPrintedNumberProvesNothingAboutItself(t *testing.T) {
	p := full()
	p.Folio = 0
	if v := Judge(p, Expect{Folio: 64}); v.OK {
		t.Fatal("a page that prints no number was accepted as page 64")
	}
}

func TestACoverIsAPictureAndNotAPage(t *testing.T) {
	if v := Judge(full(), Expect{Folio: 64, Cover: true}); v.OK {
		t.Fatal("a cover was read out of a text layer it does not have")
	}
}

func TestAPageHoldingOnlyItsNumberIsNotAPage(t *testing.T) {
	p := full()
	p.Body = "⟦folio 64⟧\n"
	p.Words = 1
	p.Math = 0
	if v := Judge(p, Expect{Folio: 64}); v.OK {
		t.Fatal("an empty page was written into the corpus")
	}
}

func TestAPageTheManifestSaysNothingAboutIsStillJudged(t *testing.T) {
	// The manifest does not carry a number for every sheet, and a missing one is
	// not a reason to distrust the page that does print its own.
	if v := Judge(full(), Expect{}); !v.OK {
		t.Fatalf("rejected with no folio expected: %s", v.Why)
	}
}
