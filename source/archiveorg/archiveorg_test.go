package archiveorg_test

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source/archiveorg"
)

// The text is Cyrillic on purpose. Every page of this magazine is, and the one
// mistake this format invites is reading its offsets as bytes, which only shows
// up when a character is not one byte wide.
var volumePages = []string{"первая страница\n", "вторая страница\n", "третья страница"}

// volumeText is the pages run together, which is how the archive stores them:
// one stream of characters with nothing in it to say where a page ends.
func volumeText() string { return strings.Join(volumePages, "") }

// spansOf is the index the archive would ship for those pages.
func spansOf(pages []string) []archiveorg.Span {
	var out []archiveorg.Span
	from := 0
	for _, page := range pages {
		to := from + len([]rune(page))
		out = append(out, archiveorg.Span{From: from, To: to})
		from = to
	}
	return out
}

func TestALeafIsCutOutByCharactersAndNotByBytes(t *testing.T) {
	// The whole reason Build exists as its own function. Cyrillic is two bytes
	// a character in UTF-8, so slicing this text by these numbers as bytes
	// returns the first half of the first word and no error at all.
	t.Parallel()
	v, err := archiveorg.Build(1975, volumeText(), spansOf(volumePages), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range volumePages {
		if got := v.Leaves[i].Text; got != want {
			t.Errorf("leaf %d is %q, want %q", i, got, want)
		}
	}
}

func TestAnIndexThatDoesNotCoverTheTextIsRefused(t *testing.T) {
	// This is what a byte offset file would look like if the archive ever
	// changed the unit, and it is the one failure that would otherwise be
	// silent, so it is an error rather than a shrug.
	t.Parallel()
	_, err := archiveorg.Build(1975, volumeText(), spansOf(volumePages[:2]), nil)
	if err == nil {
		t.Fatal("an index covering half the text was accepted")
	}
	if !strings.Contains(err.Error(), "characters") {
		t.Errorf("the error does not say what went wrong: %v", err)
	}
}

func TestLabelsRideAlongWithTheirLeaves(t *testing.T) {
	t.Parallel()
	v, err := archiveorg.Build(1975, volumeText(), spansOf(volumePages), []string{"", "12", "13"})
	if err != nil {
		t.Fatal(err)
	}
	if v.Leaves[1].Label != "12" || v.Leaves[0].Label != "" {
		t.Errorf("labels are %q and %q", v.Leaves[0].Label, v.Leaves[1].Label)
	}
}

func TestAYearTheArchiveDoesNotHoldIsRefusedBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	c := archiveorg.New()
	if _, err := c.Volume(t.Context(), 2015); err == nil {
		t.Fatal("2015 was accepted and the archive stops at 1992")
	}
}

func volume(t *testing.T, leaves int) *archiveorg.Volume {
	t.Helper()
	v := &archiveorg.Volume{Year: 1975}
	for i := range leaves {
		v.Leaves = append(v.Leaves, archiveorg.Leaf{Index: i})
	}
	return v
}

func issue(number string, sheets int) manifest.Issue {
	return manifest.Issue{Key: "kvant_1975_" + number, Year: 1975, Number: number, Sheets: sheets}
}

func TestTheNthLeafIsTheNthSheetOfTheYear(t *testing.T) {
	t.Parallel()
	got, err := archiveorg.Align(volume(t, 152), []manifest.Issue{issue("1", 84), issue("2", 68)})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got["kvant_1975_1"]); n != 84 {
		t.Errorf("the first issue got %d leaves", n)
	}
	if at := got["kvant_1975_2"][0].Index; at != 84 {
		t.Errorf("the second issue starts at leaf %d, want 84", at)
	}
}

func TestIssueTenDoesNotSortBeforeIssueTwo(t *testing.T) {
	// As strings it does, and then the whole second half of the year lands on
	// the wrong leaves and every page still reads as plausible Russian.
	t.Parallel()
	got, err := archiveorg.Align(volume(t, 30), []manifest.Issue{issue("10", 10), issue("2", 10), issue("1", 10)})
	if err != nil {
		t.Fatal(err)
	}
	if at := got["kvant_1975_2"][0].Index; at != 10 {
		t.Errorf("issue 2 starts at leaf %d, want 10", at)
	}
	if at := got["kvant_1975_10"][0].Index; at != 20 {
		t.Errorf("issue 10 starts at leaf %d, want 20", at)
	}
}

func TestADoubleNumberSortsOnTheFirstOfTheTwo(t *testing.T) {
	t.Parallel()
	got, err := archiveorg.Align(volume(t, 20), []manifest.Issue{issue("11-12", 10), issue("1", 10)})
	if err != nil {
		t.Fatal(err)
	}
	if at := got["kvant_1975_11-12"][0].Index; at != 10 {
		t.Errorf("the double number starts at leaf %d, want 10", at)
	}
}

func TestAYearWhoseSheetsDoNotAddUpIsRefused(t *testing.T) {
	// 1990 is this case: the volume is seven leaves short of the year, so the
	// two are not the same scan and lining them up would compare pages that
	// have nothing to do with each other.
	t.Parallel()
	_, err := archiveorg.Align(volume(t, 145), []manifest.Issue{issue("1", 84), issue("2", 68)})
	if err == nil {
		t.Fatal("a volume seven leaves short was aligned anyway")
	}
	if !strings.Contains(err.Error(), "not the same scan") {
		t.Errorf("the error does not say why: %v", err)
	}
}

func labelled(t *testing.T, first int, labels ...string) []archiveorg.Leaf {
	t.Helper()
	out := make([]archiveorg.Leaf, len(labels))
	for i, label := range labels {
		out[i] = archiveorg.Leaf{Index: first + i, Label: label}
	}
	return out
}

func TestVerifyAgreesWhenTheNumbersSitWhereTheyShould(t *testing.T) {
	// Two unnumbered covers, then page 1 on the third sheet.
	t.Parallel()
	checked, agreed := archiveorg.Verify(labelled(t, 84, "", "", "1", "2", "3", "4"))
	if checked != 4 || agreed != 4 {
		t.Errorf("checked %d and agreed %d, want 4 and 4", checked, agreed)
	}
}

func TestVerifyDisagreesWhenTheAlignmentSlips(t *testing.T) {
	// The failure this is for. One leaf out is invisible in the text, which
	// still reads as Russian from the right magazine, and shows up here.
	t.Parallel()
	checked, agreed := archiveorg.Verify(labelled(t, 84, "", "", "1", "2", "9", "10"))
	if checked != 4 {
		t.Fatalf("checked %d, want 4", checked)
	}
	if agreed != 2 {
		t.Errorf("agreed %d, want 2 of the 4", agreed)
	}
}

func TestVerifySaysNothingAboutAnIssueWithNoNumbersOnIt(t *testing.T) {
	t.Parallel()
	if checked, agreed := archiveorg.Verify(labelled(t, 0, "", "", "")); checked != 0 || agreed != 0 {
		t.Errorf("checked %d and agreed %d on an unnumbered issue", checked, agreed)
	}
}
