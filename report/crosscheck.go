package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/problems"
)

// MaxCrossExamples is how many worst statements the document shows. Enough to
// see what a bad reading looks like, few enough that this stays a page.
const MaxCrossExamples = 5

// CrosscheckMarkdown is the reports/problems-crosscheck.md document.
func CrosscheckMarkdown(x *problems.Crosscheck, generated time.Time) string {
	t := x.Tally()

	var out strings.Builder
	out.WriteString("# The joined pages against ours\n\n")
	out.WriteString("Recovering Задачник «Кванта» off the scans is a join. ")
	out.WriteString("A problem is cut out of one issue, its solution is cut out of another issue two to four numbers later, ")
	out.WriteString("and the two are put together on the strength of a number printed in a heading. ")
	out.WriteString("Nothing in that chain checks itself: a misread digit pairs a statement with somebody else's answer, ")
	out.WriteString("and the result looks exactly like a problem that worked.\n\n")
	out.WriteString("kvant.digital made the same pairing by hand from the same magazine. ")
	out.WriteString("This is the two put side by side, which is the only outside opinion available on whether the join is right.\n\n")
	fmt.Fprintf(&out, "Generated %s by kvant problems crosscheck.\n\n", generated.UTC().Format("2006-01-02"))

	fmt.Fprintf(&out, "%d problems compared, %d more whose number the site does not index.\n\n", t.Compared, t.Absent)
	if t.Compared == 0 {
		out.WriteString("Nothing to compare. Either the manifest is empty or the site carries none of the numbers in it.\n")
		return out.String()
	}

	out.WriteString("## Where each half was printed\n\n")
	out.WriteString("The rate is taken over the problems both sources name an issue for, because those are the only ones where there is anything to agree about. ")
	out.WriteString("A half only one side has is not a disagreement: the site indexes a fraction of the historical numbering, and this corpus has read a fraction of the years.\n\n")
	out.WriteString("| half | both name one | same | moved | agreement | only ours | only theirs | neither |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|\n")
	crossRow(&out, "posed", t.Posed)
	crossRow(&out, "solved", t.Solved)
	out.WriteString("\n")

	out.WriteString("## Where the two disagree\n\n")
	disagreements := x.Disagreements()
	if len(disagreements) == 0 {
		out.WriteString("Nowhere. Every problem both sources name an issue for, they name the same issue.\n\n")
	} else {
		out.WriteString("One of the two has these on the wrong page. ")
		out.WriteString("The site is not automatically right, and a disagreement is a page for a person to open rather than a correction to apply. ")
		out.WriteString("The exception is a problem the site has answered in an earlier issue than it was set in, ")
		out.WriteString("which the magazine could not have printed and which settles that line without anybody opening anything.\n\n")
		out.WriteString("| problem | half | ours | theirs | page | |\n|---|---|---|---|---|---|\n")
		for _, c := range disagreements {
			note := ""
			if c.TheirsRunsBackwards() {
				note = fmt.Sprintf("the site answers it in %s, before the issue it says set it", c.TheirSolved)
			}
			if c.Posed == problems.Moved {
				fmt.Fprintf(&out, "| %s | posed | %s | %s | %s | %s |\n", c.ID, c.OurPosed, c.TheirPosed, link(c.URL), note)
			}
			if c.Solved == problems.Moved {
				fmt.Fprintf(&out, "| %s | solved | %s | %s | %s | %s |\n", c.ID, c.OurSolved, c.TheirSolved, link(c.URL), note)
			}
		}
		out.WriteString("\n")
	}

	out.WriteString("## Pages to go and read\n\n")
	toRead := x.ToRead()
	if len(toRead) == 0 {
		out.WriteString("None. Every half the site indexes for these problems is one this corpus has already read.\n\n")
	} else {
		out.WriteString("The site names an issue for a half this corpus does not have. ")
		out.WriteString("This is the useful direction of the comparison: it turns a hole in the manifest into a numbered issue to put through the reading lane.\n\n")
		out.WriteString("| problem | half | the issue that carries it |\n|---|---|---|\n")
		for _, c := range toRead {
			if c.Posed == problems.OnlyIndexed {
				fmt.Fprintf(&out, "| %s | posed | %s |\n", c.ID, c.TheirPosed)
			}
			if c.Solved == problems.OnlyIndexed {
				fmt.Fprintf(&out, "| %s | solved | %s |\n", c.ID, c.TheirSolved)
			}
		}
		out.WriteString("\n")
	}

	out.WriteString("## The statements, word by word\n\n")
	if t.Statements == 0 {
		out.WriteString("No problem in this run has a statement on both sides, so there is nothing to compare the reading against.\n")
		return out.String()
	}
	out.WriteString("Every problem where the site carries the words as well as the reference, ")
	out.WriteString("compared the same way the publisher diff compares an article. ")
	out.WriteString("This is the reading measured against text a person typed off the same page, on the exact material the benchmark is built from.\n\n")
	fmt.Fprintf(&out, "%d statements, %d of their words, %.1f%% of them ours does not have, %.0f%% of ours theirs accounts for.\n\n",
		t.Statements, t.Text.Words, t.Text.Rate()*100, t.Text.Coverage()*100)

	worst := x.Worst(MaxCrossExamples)
	out.WriteString("### The furthest apart\n\n")
	for _, c := range worst {
		fmt.Fprintf(&out, "**%s**, %.1f%% of %d words. %s\n\n", c.ID, c.Condition.Rate()*100, c.Condition.Words, link(c.URL))
		if len(c.Examples) == 0 {
			out.WriteString("Every word the site has, we have too.\n\n")
			continue
		}
		out.WriteString("| the site | ours |\n|---|---|\n")
		for _, example := range c.Examples {
			fmt.Fprintf(&out, "| %s | %s |\n", word(example.Publisher), word(example.Vision))
		}
		out.WriteString("\n")
	}

	out.WriteString("## Every problem compared\n\n")
	out.WriteString("| problem | posed | solved | their words | differ |\n|---|---|---|---:|---:|\n")
	for _, c := range x.Checks {
		differ := ""
		if c.Condition.Words > 0 {
			differ = fmt.Sprintf("%.1f%%", c.Condition.Rate()*100)
		}
		fmt.Fprintf(&out, "| %s | %s | %s | %d | %s |\n",
			c.ID, c.Posed, c.Solved, c.Condition.Words, differ)
	}
	return out.String()
}

func crossRow(out *strings.Builder, half string, h problems.Halves) {
	rate := "n/a"
	if h.Checked() > 0 {
		rate = fmt.Sprintf("%.0f%%", h.Rate()*100)
	}
	fmt.Fprintf(out, "| %s | %d | %d | %d | %s | %d | %d | %d |\n",
		half, h.Checked(), h.Same, h.Moved, rate, h.OnlyRead, h.OnlyIndexed, h.Neither)
}

func link(url string) string {
	if url == "" {
		return ""
	}
	return fmt.Sprintf("[page](%s)", url)
}
