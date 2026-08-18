package refs

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Threshold is how many of the citations found may end up unresolved before the
// reference pass counts as failed.
//
// One in twenty. It is a number picked against what the extraction actually
// produces rather than against what would be nice, and the thing it is really
// measuring is the reading rather than the matching: a citation that resolves
// to nothing is usually a page whose folio came back wrong or a number the
// model dropped a digit from. Pending does not count against it. A pointer into
// a year nobody has read yet is a good pointer, and folding it in here would
// mean the report said the matching was broken for as long as the corpus was
// incomplete, which is exactly when nobody could tell the difference.
const Threshold = 0.05

// Report is the reports/refs-unresolved.md document.
func Report(graphs map[int]*Graph, generated time.Time) string {
	var out strings.Builder
	counts := Counts(graphs)
	total := 0
	for _, n := range counts {
		total += n
	}

	fmt.Fprintf(&out, "# Cross references that did not resolve\n\n")
	out.WriteString("Kvant cites itself in prose, so a reference is matched against the printed contents rather than parsed.\n")
	out.WriteString("This is every citation the matching could not place, and the rate it failed at.\n")
	fmt.Fprintf(&out, "Generated %s by kvant report refs.\n\n", generated.UTC().Format("2006-01-02"))

	if total == 0 {
		out.WriteString("No citations found, which means no articles were read in the range asked for.\n")
		return out.String()
	}

	rate := float64(counts[Unresolved]) / float64(total)
	fmt.Fprintf(&out, "%d citations over %d years. %.1f%% unresolved against a threshold of %.0f%%, which is %s.\n\n",
		total, len(graphs), rate*100, Threshold*100, verdict(rate))

	out.WriteString("| status | count | share | what it means |\n|---|---|---|---|\n")
	for _, status := range []Status{Linked, Issue, Pending, Unresolved} {
		n := counts[status]
		fmt.Fprintf(&out, "| %s | %d | %.1f%% | %s |\n", status, n, float64(n)/float64(total)*100, means(status))
	}
	out.WriteString("\n")

	fmt.Fprintf(&out, "## By year\n\n")
	out.WriteString("| year | citations | linked | issue | pending | unresolved |\n|---|---|---|---|---|---|\n")
	for _, year := range years(graphs) {
		per := Counts(map[int]*Graph{year: graphs[year]})
		fmt.Fprintf(&out, "| %d | %d | %d | %d | %d | %d |\n",
			year, graphs[year].Count, per[Linked], per[Issue], per[Pending], per[Unresolved])
	}
	out.WriteString("\n")

	if counts[Unresolved] == 0 {
		out.WriteString("## The list\n\nEmpty. Every citation found was placed.\n")
		return out.String()
	}

	out.WriteString("## Why they did not resolve\n\n")
	out.WriteString("| reason | count |\n|---|---|\n")
	for _, row := range byReason(graphs) {
		fmt.Fprintf(&out, "| %s | %d |\n", row.reason, row.count)
	}

	out.WriteString("\n## The list\n\n")
	for _, year := range years(graphs) {
		unresolved := pick(graphs[year], Unresolved)
		if len(unresolved) == 0 {
			continue
		}
		fmt.Fprintf(&out, "### %d\n\n", year)
		out.WriteString("| citing article | citation | why |\n|---|---|---|\n")
		for _, ref := range unresolved {
			fmt.Fprintf(&out, "| %s | %s | %s |\n", cell(ref.FromLabel), cell(ref.Cite), cell(ref.Why))
		}
		out.WriteString("\n")
	}
	return out.String()
}

// OK says whether the unresolved rate is inside the threshold, which is the
// exit criterion of the milestone.
func OK(graphs map[int]*Graph) (float64, bool) {
	counts := Counts(graphs)
	total := 0
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return 0, true
	}
	rate := float64(counts[Unresolved]) / float64(total)
	return rate, rate <= Threshold
}

func verdict(rate float64) string {
	if rate <= Threshold {
		return "inside it"
	}
	return "over it"
}

func means(status Status) string {
	switch status {
	case Linked:
		return "the citation came out as a tag pointing at one object"
	case Issue:
		return "the citation named an issue and no page, so an issue is as far as it goes"
	case Pending:
		return "the target is in the printed contents and not yet in the corpus"
	case Unresolved:
		return "nothing in the archive matches what the citation says"
	}
	return ""
}

func years(graphs map[int]*Graph) []int {
	out := make([]int, 0, len(graphs))
	for year := range graphs {
		out = append(out, year)
	}
	sort.Ints(out)
	return out
}

func pick(graph *Graph, status Status) []Ref {
	var out []Ref
	for _, ref := range graph.Refs {
		if ref.Status == status {
			out = append(out, ref)
		}
	}
	return out
}

type reasonRow struct {
	reason string
	count  int
}

// byReason groups the failures by what went wrong rather than listing them, so
// that a systematic fault shows up as one line with a large number next to it.
func byReason(graphs map[int]*Graph) []reasonRow {
	count := map[string]int{}
	for _, graph := range graphs {
		for _, ref := range graph.Refs {
			if ref.Status != Unresolved {
				continue
			}
			count[generalise(ref.Why)]++
		}
	}
	rows := make([]reasonRow, 0, len(count))
	for reason, n := range count {
		rows = append(rows, reasonRow{reason: reason, count: n})
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].count != rows[b].count {
			return rows[a].count > rows[b].count
		}
		return rows[a].reason < rows[b].reason
	})
	return rows
}

// generalise strips the issue key and the page number out of a reason, so that
// four hundred failures against four hundred different issues group into one
// line instead of four hundred.
func generalise(why string) string {
	switch {
	case strings.HasPrefix(why, "the archive has no issue"):
		return "the citation names an issue the archive does not have"
	case strings.HasPrefix(why, "nothing in the contents of"):
		return "the page cited is outside the contents of the issue"
	case strings.Contains(why, "is not an issue number"):
		return "the number read off the page is not an issue number"
	}
	if why == "" {
		return "no reason recorded"
	}
	return why
}

func cell(text string) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "|", "/")
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > 80 {
		return string([]rune(text)[:79]) + "…"
	}
	if text == "" {
		return "nothing recorded"
	}
	return text
}
