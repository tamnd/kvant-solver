package archiveorg

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/manifest"
)

// Align says which leaf of the year volume is which sheet of which issue.
//
// The two scans turn out to be the same scan. Somebody bound twelve issues into
// a year volume and photographed every surface in order, covers and inserts
// included, and that is exactly what kvant.digital holds issue by issue. So the
// nth leaf of the volume is the nth sheet of the year, and the whole of the
// alignment is a running total of sheet counts.
//
// That is a claim about somebody else's scanning, not a fact about the format,
// so it is checked rather than assumed. The check is the sheet total: if the
// year's issues do not add up to the number of leaves, the two are not the same
// set of paper and nothing here is safe to line up. Over 1970 to 1992 that
// holds for every year but 1990, which is seven leaves short and is refused.
//
// The printed page numbers are deliberately not used to do the alignment, only
// to check it afterwards. They are OCR of a number in the corner of a page
// printed on newsprint, they are missing on every cover and wrong often enough
// to matter, and an alignment built on them would fail worst exactly where the
// scan is worst, which is where it is most needed. See Verify.
func Align(v *Volume, issues []manifest.Issue) (map[string][]Leaf, error) {
	year := inYear(issues, v.Year)
	if len(year) == 0 {
		return nil, fmt.Errorf("the %d volume: no issues to align", v.Year)
	}
	total := 0
	for _, iss := range year {
		if iss.Sheets <= 0 {
			return nil, fmt.Errorf("%s: the manifest gives no sheet count", iss.Key)
		}
		total += iss.Sheets
	}
	if total != len(v.Leaves) {
		return nil, fmt.Errorf(
			"the %d volume has %d leaves and the year's issues have %d sheets, so they are not the same scan",
			v.Year, len(v.Leaves), total)
	}
	out := make(map[string][]Leaf, len(year))
	at := 0
	for _, iss := range year {
		out[iss.Key] = v.Leaves[at : at+iss.Sheets]
		at += iss.Sheets
	}
	return out, nil
}

// Verify checks an alignment against the page numbers the OCR read off the
// paper.
//
// It returns how many leaves carried a number this could check and how many of
// those sat where that number says they should. A sheet's own printed number is
// not in the manifest, but its position is: the magazine numbers from 1 and the
// cover surfaces come first, so a leaf whose printed number is n should be the
// (n + covers)th sheet of its issue. Covers is not a constant, so this works
// out the offset that the most leaves in the issue agree on and then counts how
// many agree with it.
//
// A perfect score is not expected and would be suspicious. Half is normal,
// because covers, plates and adverts carry no number and the OCR misreads
// plenty of the ones that exist. What this is for is the other end: an
// alignment that is off by a leaf scores near zero, and that is the failure
// worth catching, because it produces a comparison that looks merely
// disappointing rather than broken.
//
// Note what it does not say. It compares the archive's numbers against the
// archive's own leaf positions, so a perfect score means the archive is
// internally consistent and nothing more. It cannot say that those leaves are
// the issue we think they are, and it cannot say a word about our pages. The
// first run of this found 1975's fourth issue scoring 63 of 63 while fifteen of
// its sixty eight pages held text from a different issue of the magazine, which
// is a defect on our side that a number in the corner of somebody else's scan
// was never going to see.
func Verify(leaves []Leaf) (checked, agreed int) {
	votes := map[int]int{}
	type numbered struct {
		at, printed int
	}
	var seen []numbered
	for _, leaf := range leaves {
		n, err := strconv.Atoi(strings.TrimSpace(leaf.Label))
		if err != nil || n <= 0 {
			continue
		}
		seen = append(seen, numbered{at: leaf.Index, printed: n})
	}
	if len(seen) == 0 {
		return 0, 0
	}
	// The offset is relative to the first leaf of the issue, so that this says
	// the same thing whichever issue of the year it is handed.
	first := leaves[0].Index
	for _, s := range seen {
		votes[(s.at-first)-s.printed]++
	}
	best, bestVotes := 0, -1
	for offset, count := range votes {
		if count > bestVotes || (count == bestVotes && offset < best) {
			best, bestVotes = offset, count
		}
	}
	return len(seen), bestVotes
}

// inYear is the year's issues in the order they were published, which is the
// order they were bound in.
//
// Sorting by the number as an integer and not as a string matters: as strings
// issue 10 sorts before issue 2 and the whole second half of the year lands on
// the wrong leaves. A double number such as 1988's "11-12" sorts on the first
// of the two, which is where it belongs.
func inYear(issues []manifest.Issue, year int) []manifest.Issue {
	var out []manifest.Issue
	for _, iss := range issues {
		if iss.Year == year {
			out = append(out, iss)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return issueOrdinal(out[i]) < issueOrdinal(out[j]) })
	return out
}

func issueOrdinal(iss manifest.Issue) int {
	head, _, _ := strings.Cut(iss.Number, "-")
	n, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil {
		return 0
	}
	return n
}
