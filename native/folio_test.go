package native

import (
	"slices"
	"testing"
)

// folios builds the pages of a file out of the numbers they print, where a zero
// is a page that prints none.
func folios(printed ...int) []Page {
	out := make([]Page, len(printed))
	for i, n := range printed {
		out[i] = Page{Folio: n}
	}
	return out
}

func TestAPageBetweenTwoNumberedOnesTakesTheNumberLeftOver(t *testing.T) {
	// The commonest page in the archive from 2007 on: an article opens under a
	// full width title and the magazine leaves the number off. Nothing on it
	// says which page it is and the pages either side leave one possibility.
	got := Folios(folios(7, 8, 0, 10, 11))
	if want := []int{7, 8, 9, 10, 11}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestARunOfUnnumberedPagesIsCarriedThrough(t *testing.T) {
	// A plate and the page facing it, or an opening spread.
	got := Folios(folios(20, 0, 0, 0, 24))
	if want := []int{20, 21, 22, 23, 24}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAPageTheArithmeticDoesNotSettleKeepsItsZero(t *testing.T) {
	// One page of the file between page 8 and page 12 is three pages missing,
	// which means the file and the numbering have parted company somewhere in
	// here. Every one of those goes to a model rather than being filed next to
	// a page it might not belong beside.
	got := Folios(folios(8, 0, 12))
	if want := []int{8, 0, 12}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTheCoversAreNotInferredIntoTheCount(t *testing.T) {
	// A cover is not page zero, it is outside the numbering, and there is
	// nothing behind the first numbered page to reckon from.
	got := Folios(folios(0, 0, 1, 2, 0))
	if want := []int{0, 0, 1, 2, 0}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAnIssueThatPrintsEveryNumberIsLeftAlone(t *testing.T) {
	got := Folios(folios(1, 2, 3, 4))
	if want := []int{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAnInferredPageIsJudgedOnItsTextAndNotOnItsNumber(t *testing.T) {
	// Judge rejects a page with no number, because normally nothing says which
	// page it is. Once the pages around it have said so, the only question left
	// is whether the text came out whole.
	p := Page{Folio: 0, Words: 400, Body: string(make([]byte, 400))}
	if v := Judge(p, Expect{Folio: 9, Inferred: true}); !v.OK {
		t.Fatalf("an inferred page was rejected: %s", v.Why)
	}
	if v := Judge(p, Expect{Folio: 9}); v.OK || v.Rule != RuleUnnumbered {
		t.Fatalf("a page nothing places was accepted: %+v", v)
	}
}
