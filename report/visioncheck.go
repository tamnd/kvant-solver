package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/publisher"
)

// VisionDrift is the share of the archive's words a page can be missing before
// it is worth a person opening.
//
// It is set high on purpose. The archive's reading of these years is ABBYY on
// Soviet newsprint and a quarter of what it says is not words at all, so a
// perfectly good page routinely fails to contain a third of it. What this line
// is for is the other tail: a page where our reading stopped halfway, or wrote
// one column of two, does not miss a third of the archive's words, it misses
// most of them.
const VisionDrift = 0.60

// MaxVisionExamples is how many pages the document lists.
const MaxVisionExamples = 40

// VisionCheck is one page of ours held against the archive's reading of the
// same sheet.
//
// Missing counts from the archive's side: the share of the words it found that
// our page does not have in any spelling. That direction is the one that
// matters. A word we have and it does not is almost always its OCR failing,
// which is not news, whereas a run of words it found and we did not is our page
// having stopped early or skipped a column, and that is a defect nothing else
// in this project can see.
//
// The first run of this found something worse than a lost column. Fifteen pages
// of 1975's fourth issue missed nearly every word the archive read, and the
// reason was that they held a different issue of the magazine: ten of them are
// the same sheet of the first issue of 1976, transcribed under 1975's front
// matter. The scan on disk was right, the archive's reading of it was right, and
// the page we wrote from it was another issue's page. Those pages are well
// formed, plausible Russian, and carry the folio the sheet actually prints,
// because the two issues both have that folio. Nothing in the reading lane can
// see that and no rule about the shape of a page ever will.
type VisionCheck struct {
	Issue string
	Year  int
	Sheet int

	publisher.Count

	// Unread is the sheet having no page in the corpus at all. Kept rather than
	// dropped, because a report that quietly left out the pages nobody could
	// read would be reporting on whichever pages happened to be easy.
	Unread bool
}

// VisionTally is a set of those added up.
type VisionTally struct {
	Pages  int
	Unread int
	Over   int
	publisher.Count
}

// TallyVision adds the checks up by words rather than by pages, so that a dense
// page weighs what it holds.
func TallyVision(checks []VisionCheck) VisionTally {
	var t VisionTally
	for _, c := range checks {
		if c.Unread {
			t.Unread++
			continue
		}
		t.Pages++
		t.Words += c.Words
		t.Changed += c.Changed
		t.Ours += c.Ours
		t.Extra += c.Extra
		t.Near += c.Near
		if c.Missing() > VisionDrift {
			t.Over++
		}
	}
	return t
}

// VisionMarkdown is the reports/vision-check.md document.
func VisionMarkdown(checks []VisionCheck, generated time.Time) string {
	t := TallyVision(checks)

	var out strings.Builder
	out.WriteString("# The model's reading against the archive's\n\n")
	out.WriteString("Every page of 1970 to 1989 in this corpus was read by a model looking at a photograph of the paper. ")
	out.WriteString("That reading passes nine rules before it is written, but the rules are about the shape of what came back: they can tell that a formula never closes or that two alphabets are mixed in one word, and they cannot tell that the second column of a page was never read at all. ")
	out.WriteString("A page that quietly stops halfway is well formed and wrong, and nothing inside the lane can see it.\n\n")
	out.WriteString("The Internet Archive scanned the same twenty three years as bound volumes and ran them through ABBYY. ")
	out.WriteString("That is an independent reading of the same paper by a different engine, and lining it up against ours costs a download per year and no model calls. ")
	out.WriteString("It is not a better reading and it is not used to correct anything. It is a witness.\n\n")
	fmt.Fprintf(&out, "Generated %s by kvant vision check.\n\n", generated.UTC().Format("2006-01-02"))

	if t.Pages == 0 {
		out.WriteString("No page has been checked yet, so there is nothing here.\n")
		return out.String()
	}
	fmt.Fprintf(&out, "%d pages checked against %d words the archive read. Our pages do not have %.0f%% of those words in any spelling.\n\n",
		t.Pages, t.Words, t.Missing()*100)
	if t.Unread > 0 {
		fmt.Fprintf(&out, "%d more sheets have no page in the corpus. Those are counted separately and left out of the rate, because a reading that never happened is not evidence about the ones that did.\n\n", t.Unread)
	}

	out.WriteString("## How to read that number\n\n")
	out.WriteString("Not as a score. A quarter of what ABBYY produced on this paper is not words at all, and every one of those counts here as something we are missing. ")
	out.WriteString("A page that is entirely correct still fails to contain a third of the archive's output, and that is the normal state of a good page rather than a problem with it.\n\n")
	out.WriteString("What the number is for is its own tail. A page where the model stopped halfway, or read one column of two, or transcribed a caption and called it a page, does not miss a third of the archive's words. It misses most of them. ")
	fmt.Fprintf(&out, "The line here is %.0f%%, and the pages over it are listed at the end for somebody to open.\n\n", VisionDrift*100)
	out.WriteString("The comparison runs from the archive's side on purpose. A word we have and it does not is its OCR failing, which is not news. A run of words it found and we did not is our page having lost something, which is the whole point.\n\n")

	fmt.Fprintf(&out, "%d of the %d pages are over the line.\n\n", t.Over, t.Pages)

	out.WriteString("## By issue\n\n")
	out.WriteString("| issue | pages | words | missing | over the line | not read |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, row := range visionByIssue(checks) {
		fmt.Fprintf(&out, "| %s | %d | %d | %.0f%% | %d | %d |\n",
			row.issue, row.Pages, row.Words, row.Missing()*100, row.Over, row.Unread)
	}
	out.WriteString("\n")

	worst := worstVision(checks, MaxVisionExamples)
	if len(worst) == 0 {
		out.WriteString("## The pages to open\n\nNone. No page is over the line.\n")
		return out.String()
	}
	out.WriteString("## The pages to open\n\n")
	out.WriteString("Worst first, on the share of the archive's words that are absent from ours. A page here is not a page that is wrong, it is a page where two readings disagree enough that only looking at the scan will settle it.\n\n")
	out.WriteString("The first run of this list settled one of them the hard way. Fifteen pages of the fourth issue of 1975 were near the top, and opening the scans showed the scans were fine: the pages we had written from them belonged to the first issue of 1976. ")
	out.WriteString("They read as good Russian, they carry the folio the sheet prints, and they had passed every rule the reading lane has. That is the kind of thing this list is for.\n\n")
	out.WriteString("| issue | sheet | archive words | missing |\n")
	out.WriteString("| --- | --- | --- | --- |\n")
	for _, c := range worst {
		fmt.Fprintf(&out, "| %s | %d | %d | %.0f%% |\n", c.Issue, c.Sheet, c.Words, c.Missing()*100)
	}
	return out.String()
}

type visionRow struct {
	issue string
	VisionTally
}

func visionByIssue(checks []VisionCheck) []visionRow {
	at := map[string]*visionRow{}
	var order []string
	for _, c := range checks {
		row, ok := at[c.Issue]
		if !ok {
			row = &visionRow{issue: c.Issue}
			at[c.Issue] = row
			order = append(order, c.Issue)
		}
		if c.Unread {
			row.Unread++
			continue
		}
		row.Pages++
		row.Words += c.Words
		row.Changed += c.Changed
		row.Ours += c.Ours
		row.Extra += c.Extra
		row.Near += c.Near
		if c.Missing() > VisionDrift {
			row.Over++
		}
	}
	sort.Strings(order)
	out := make([]visionRow, 0, len(order))
	for _, key := range order {
		out = append(out, *at[key])
	}
	return out
}

// worstVision is the pages over the line, worst first, at most n of them.
//
// A page the archive read almost nothing on is dropped rather than ranked. Its
// rate is arithmetic on a handful of words and says nothing about our reading,
// and letting those to the top of the list would fill it with covers.
func worstVision(checks []VisionCheck, n int) []VisionCheck {
	var over []VisionCheck
	for _, c := range checks {
		if !c.Unread && c.Words >= 20 && c.Missing() > VisionDrift {
			over = append(over, c)
		}
	}
	sort.Slice(over, func(i, j int) bool {
		if a, b := over[i].Missing(), over[j].Missing(); a != b {
			return a > b
		}
		if over[i].Issue != over[j].Issue {
			return over[i].Issue < over[j].Issue
		}
		return over[i].Sheet < over[j].Sheet
	})
	if len(over) > n {
		over = over[:n]
	}
	return over
}
