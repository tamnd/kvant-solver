package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/publisher"
)

// Trust is the disagreement rate above which the archive's own text is not
// worth taking.
//
// It is a threshold and not a verdict, and the verdict is per year, because the
// archive's text was typed over decades by different hands. A year under it is
// a year where the publisher path saves a vision pass on every article it
// covers. A year over it is a year where the text is a different text: an
// abridgement, a reprint, or the first two paragraphs and a link.
//
// Ten per cent of the words is deliberately loose. Two honest readings of the
// same page differ on where a formula stops being a formula, on the site's own
// notes in square brackets, and on the hyphenation of a word broken across a
// column, and that is a couple of per cent before anybody has made a mistake:
// the first three articles measured, in the first issue of 1975, came in at
// 2.6, 2.3 and nothing. What this has to separate is that from a text that is
// not the same text at all.
const Trust = 0.10

// DiffMarkdown is the reports/publisher-diff.md document.
func DiffMarkdown(years []publisher.Year, diffs []publisher.Diff, perYear int, generated time.Time) string {
	var out strings.Builder
	out.WriteString("# The archive's text against ours\n\n")
	out.WriteString("kvant.digital carries the publisher's own text for part of the archive. ")
	out.WriteString("Where it does, a page needs no model, so the question is how far that text can be trusted, ")
	out.WriteString("and this is the measurement rather than the opinion.\n\n")
	fmt.Fprintf(&out, "Up to %d articles a year, picked by hash so the sample does not move between runs, ", perYear)
	out.WriteString("each compared word by word against the same article assembled from our own reading.\n\n")
	fmt.Fprintf(&out, "Generated %s by kvant report diff.\n\n", generated.UTC().Format("2006-01-02"))

	out.WriteString("## The two numbers\n\n")
	out.WriteString("The rate is the share of the archive's own words that our reading of the same pages does not have. ")
	out.WriteString("That is the number the verdict is taken on, because it is the one that says whether their typing can stand in for a vision pass.\n\n")
	out.WriteString("Coverage is the share of our reading their article accounts for, and a low one is not a complaint about them. ")
	out.WriteString("Assembly cuts at the page and the archive cuts at the piece, so wherever three short items share a page our article is the whole page and theirs is a fifth of it. ")
	out.WriteString("Coverage is here so that an archive text that stops halfway through a long article shows up instead of scoring a clean rate on the half it did type.\n\n")

	if len(years) == 0 {
		out.WriteString("Nothing to compare. No year in the range has both an article the archive carries the text of and the same article read here.\n")
		return out.String()
	}

	out.WriteString("## By year\n\n")
	out.WriteString("| year | articles | their words | differ | rate | coverage | verdict |\n|---|---:|---:|---:|---:|---:|---|\n")
	for _, year := range years {
		fmt.Fprintf(&out, "| %d | %d | %d | %d | %.1f%% | %.0f%% | %s |\n",
			year.Year, year.Articles, year.Words, year.Changed,
			year.Rate()*100, year.Coverage()*100, verdict(year.Rate()))
	}

	out.WriteString("\n## Where they part\n\n")
	out.WriteString("The worst article of each year, and the first few words the archive has that we do not. ")
	out.WriteString("A rate with no examples under it is a number nobody can act on.\n\n")
	for _, year := range years {
		worst := year.Worst
		if worst.Slug == "" {
			continue
		}
		fmt.Fprintf(&out, "### %d, %s\n\n", year.Year, worst.Issue)
		fmt.Fprintf(&out, "%s, %.1f%% of %d words.\n\n", cell(worst.Title), worst.Rate()*100, worst.Words)
		if len(worst.Examples) == 0 {
			out.WriteString("Every word the archive has, we have too.\n\n")
			continue
		}
		out.WriteString("| the archive | ours |\n|---|---|\n")
		for _, example := range worst.Examples {
			fmt.Fprintf(&out, "| %s | %s |\n", word(example.Publisher), word(example.Vision))
		}
		out.WriteString("\n")
	}

	out.WriteString("## Every article compared\n\n")
	out.WriteString("| issue | article | their words | rate | coverage |\n|---|---|---:|---:|---:|\n")
	for _, d := range diffs {
		fmt.Fprintf(&out, "| %s | %s | %d | %.1f%% | %.0f%% |\n",
			d.Issue, cell(d.Title), d.Words, d.Rate()*100, d.Coverage()*100)
	}
	return out.String()
}

// verdict is the one word a person reading the table is after.
func verdict(rate float64) string {
	if rate <= Trust {
		return "take the archive's text"
	}
	return "read it ourselves"
}

// word is one side of a disagreement, and an empty one means the other reading
// had a word here and this one did not.
func word(s string) string {
	if s == "" {
		return "*nothing*"
	}
	return "`" + s + "`"
}
