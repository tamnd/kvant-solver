package native

// Folios says which page of the magazine each page of the file is.
//
// Most of them say so themselves and this hands back what they printed. The
// ones that do not are the pages the magazine leaves unnumbered on purpose: an
// article opening under a full width title, a plate, the page a picture fills.
// From 2007 on there are half a dozen of those in every issue, and each one is
// a page that would otherwise be read by a model for want of a number that the
// two pages either side of it settle between them.
//
// The inference is arithmetic and not a guess. A page with no number is given
// one only when the nearest numbered page behind it and the nearest ahead are
// exactly as far apart in numbers as they are in sheets, which is to say only
// when there is one page it can be. An issue with a folded insert, where the
// file has a page the numbering does not, fails that test and the page keeps
// its zero and goes to a model. That is the whole of the safety here: the run
// would rather read a page twice than file it as the page next to it.
//
// The zeros that come back are the covers, the pages before the numbering
// starts and the pages after it stops, and there is nothing to infer for those:
// a cover is not page zero, it is outside the count altogether.
func Folios(pages []Page) []int {
	out := make([]int, len(pages))
	for i, p := range pages {
		out[i] = p.Folio
	}
	// A page that has just been given a number is an anchor for the next one,
	// which is what carries a run of two or three unnumbered pages in a row.
	for i, folio := range out {
		if folio > 0 {
			continue
		}
		back, ok := nearest(out, i, -1)
		if !ok {
			continue
		}
		ahead, ok := nearest(out, i, 1)
		if !ok {
			continue
		}
		// The gap in sheets and the gap in numbers have to be the same gap.
		if out[ahead]-out[back] != ahead-back {
			continue
		}
		out[i] = out[back] + (i - back)
	}
	return out
}

// nearest walks one way for the closest page that printed a number.
func nearest(folios []int, from, step int) (int, bool) {
	for i := from + step; i >= 0 && i < len(folios); i += step {
		if folios[i] > 0 {
			return i, true
		}
	}
	return 0, false
}
