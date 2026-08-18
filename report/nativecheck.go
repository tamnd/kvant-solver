package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/publisher"
)

// CheckDrift is the share of the scan's words a page can be missing before it
// is worth a person opening.
//
// It is not a pass mark and nothing in the lane reads it. Two readings of the
// same page of this magazine disagree at a few per cent whatever happens: the
// scan carries a running head the file does not put in the text layer, the file
// carries a hyphenated break the reader on the picture joined up, and every
// formula is spelled by two different hands. Ten per cent is where that stops
// explaining it.
const CheckDrift = 0.10

// MaxCheckExamples is how many pages the document shows the words of.
const MaxCheckExamples = 5

// Check is one page read twice, once out of the publisher's own file and once
// by a model looking at the scan of the same sheet.
type Check struct {
	Issue string
	Year  int
	// Sheet is the page's place in the scan, which is what names its file in
	// the corpus.
	Sheet int

	publisher.Count
	Examples []publisher.Example

	// Unread is the vision lane failing on the sheet. It is kept rather than
	// dropped because a page nothing could check is a different thing from a
	// page that checked out, and a report that quietly left them out would be
	// reporting on whichever pages happened to be easy.
	Unread bool
}

// CheckTally is a set of checks added up.
type CheckTally struct {
	Pages  int
	Unread int
	publisher.Count
}

// TallyChecks adds up the checks, counting the words and not the rates, so
// that a page of dense formulas weighs what it holds rather than one page's
// worth.
func TallyChecks(checks []Check) CheckTally {
	var t CheckTally
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
	}
	return t
}

// CheckMarkdown is the reports/native-check.md document.
func CheckMarkdown(checks []Check, generated time.Time) string {
	t := TallyChecks(checks)

	var out strings.Builder
	out.WriteString("# The text layer against the scan\n\n")
	out.WriteString("From 2007 the magazine's own PDFs carry a text layer, and pages whose mathematics survives it are written straight into the corpus without a model ever seeing them. ")
	out.WriteString("That lane checks itself: the nine rules a vision page has to pass are run over the native text as well, and they pass. ")
	out.WriteString("But they are being run on text the file produced, so what they establish is that the file is consistent with itself. ")
	out.WriteString("A font with a broken encoding, a column read in the wrong order, a page of the file that is not the page of the paper it was filed as, all of those are consistent with themselves and all of them are wrong.\n\n")
	out.WriteString("So the picture is the witness. A sample of the pages this lane wrote is read again by a model looking at the scan of the same sheet, and the two are compared word for word. ")
	out.WriteString("The two records are independent: one is the publisher's typesetting and the other is a photograph of the paper it was printed on. ")
	out.WriteString("Where they agree there is nothing left for either to be wrong about.\n\n")
	fmt.Fprintf(&out, "Generated %s by kvant native check.\n\n", generated.UTC().Format("2006-01-02"))

	if t.Pages == 0 {
		out.WriteString("No page was read twice, so there is nothing here yet.\n")
		return out.String()
	}
	fmt.Fprintf(&out, "%d pages compared, %d words of them. The two readings put %.1f%% of those words differently, and %.1f%% of them are words the file does not have in any spelling.\n\n",
		t.Pages, t.Words, t.Rate()*100, t.Missing()*100)
	if t.Unread > 0 {
		fmt.Fprintf(&out, "%d more the vision lane could not read. Those are left out of the rates rather than counted either way, because a reading that did not happen is not evidence about the one that did.\n\n", t.Unread)
	}

	out.WriteString("## Which of the three numbers to argue with\n\n")
	out.WriteString("**Missing** is the one. A word the scan has and the file does not have at all is the text layer having dropped something, and that is exactly the failure the native lane cannot see on its own.\n\n")
	out.WriteString("One thing inflates it and there is no arithmetic here that takes it out. A figure with numbers in it, a coordinate grid or a row of cards, is a picture in the file, and the native lane writes the figure marker where it stood while the model looking at the photograph reads the numbers off and types them. ")
	out.WriteString("Those numbers then count as words the file is missing. On sheet 40 of the sixth issue of 2017 they are eighty one of the hundred and twelve words scored missing, and not one word of the article's prose is gone. ")
	out.WriteString("A page that ranks badly here and turns out to be full of diagrams is usually this, and the way to tell is to look at it, which is what the list at the end is for.\n\n")
	out.WriteString("**Differ** is the two figures added together and it is the weaker of them. Most of what it counts is the model on the picture having misread a letter in a word the file has right, кабитации for кавитации, and charging the text layer for those reports the second reading's errors as the first one's. ")
	out.WriteString("It is kept because the split between the two is a heuristic on how far apart two spellings are, and a heuristic should be shown its working rather than trusted quietly.\n\n")
	fmt.Fprintf(&out, "**Accounted for** is not an error rate at all. A word the file has and the reading of the picture does not is usually the model skipping a running head, a figure caption or a column it did not go back for, and on these pages it runs at %.0f%%. It says more about the second reading than about the first.\n\n", t.Coverage()*100)

	out.WriteString("## By issue\n\n")
	out.WriteString("| issue | pages | words | differ | missing | accounted for | unread |\n|---|---:|---:|---:|---:|---:|---:|\n")
	for _, i := range checksByIssue(checks) {
		fmt.Fprintf(&out, "| %s | %d | %d | %.1f%% | %.1f%% | %.0f%% | %d |\n",
			i.Issue, i.Pages, i.Words, i.Rate()*100, i.Missing()*100, i.Coverage()*100, i.Unread)
	}
	out.WriteString("\n")

	out.WriteString("## Pages to go and look at\n\n")
	worst := worstChecks(checks, MaxCheckExamples)
	if len(worst) == 0 {
		fmt.Fprintf(&out, "None. Every page compared is within %.0f%% of the scan, which is the range two honest readings of this magazine sit in.\n\n", CheckDrift*100)
	} else {
		fmt.Fprintf(&out, "These are the pages missing more than %.0f%% of what the scan reads on them, worst first. ", CheckDrift*100)
		out.WriteString("Neither reading is automatically the right one, so this is a list of sheets for somebody to open next to the file and not a list of corrections to apply.\n\n")
		for _, c := range worst {
			fmt.Fprintf(&out, "**%s sheet %d**, %.1f%% missing of %d words.\n\n", c.Issue, c.Sheet, c.Missing()*100, c.Words)
			if len(c.Examples) == 0 {
				out.WriteString("Every word the scan has, the file has too.\n\n")
				continue
			}
			out.WriteString("| the scan | the file |\n|---|---|\n")
			for _, example := range c.Examples {
				fmt.Fprintf(&out, "| %s | %s |\n", word(example.Publisher), word(example.Vision))
			}
			out.WriteString("\n")
		}
	}

	out.WriteString("## Every page compared\n\n")
	out.WriteString("| issue | sheet | the scan's words | differ | missing | accounted for |\n|---|---:|---:|---:|---:|---:|\n")
	for _, c := range sortedChecks(checks) {
		if c.Unread {
			fmt.Fprintf(&out, "| %s | %d | | the scan would not read | | |\n", c.Issue, c.Sheet)
			continue
		}
		fmt.Fprintf(&out, "| %s | %d | %d | %.1f%% | %.1f%% | %.0f%% |\n",
			c.Issue, c.Sheet, c.Words, c.Rate()*100, c.Missing()*100, c.Coverage()*100)
	}
	return out.String()
}

// issueTally is one issue's row.
type issueTally struct {
	Issue string
	CheckTally
}

func checksByIssue(checks []Check) []issueTally {
	index := map[string]*issueTally{}
	var order []string
	for _, c := range checks {
		row, ok := index[c.Issue]
		if !ok {
			row = &issueTally{Issue: c.Issue}
			index[c.Issue] = row
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
	}
	sort.Strings(order)
	out := make([]issueTally, 0, len(order))
	for _, key := range order {
		out = append(out, *index[key])
	}
	return out
}

// worstChecks is the pages over the drift line, worst first, at most n of them.
//
// Ranked on the words that are missing rather than on the words that differ,
// because a page the model misspelled its way through is not a page anybody
// needs to open.
func worstChecks(checks []Check, n int) []Check {
	var out []Check
	for _, c := range checks {
		if !c.Unread && c.Missing() > CheckDrift {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Missing() > out[j].Missing() })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func sortedChecks(checks []Check) []Check {
	out := append([]Check(nil), checks...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Issue != out[j].Issue {
			return out[i].Issue < out[j].Issue
		}
		return out[i].Sheet < out[j].Sheet
	})
	return out
}
