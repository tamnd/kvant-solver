package catalog

import (
	"fmt"
	"strings"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source/kvantmccme"
)

// Probe is what each source turns out to hold, year by year. It is the report
// that says in advance how much of the archive can be had as text and how much
// has to go through a model, which is the difference between a plan and a hope.
//
// It reads the manifests and asks nothing of the network. Everything in it was
// written down by a sync, which is the only place a URL is allowed to come
// from, so the report cannot disagree with what a later fetch will do.
type Probe struct {
	Years []YearProbe
}

// YearProbe is one year.
type YearProbe struct {
	Year   int
	Issues int

	// Scan is how many issues kvant.digital holds the page images for. It has
	// every issue, so this is normally the whole year.
	Scan int

	// Mirror is how many issues the MCCME mirror has typeset contents for, and
	// PDF and DjVu how many it has a whole issue file for.
	Mirror int
	PDF    int
	DjVu   int

	// Native is how many of those PDFs are born digital rather than scans of
	// paper. These are the issues that never see a vision model.
	Native int

	// MathNet and FullText are how many issues mathnet.ru lists and how many it
	// claims article texts for.
	MathNet  int
	FullText int

	// TextRows and Rows are how many contents rows kvant.digital already holds
	// publisher text for. They stay at zero until a deep sync has run.
	TextRows int
	Rows     int
}

// Path is the extraction path a year takes: native for a born digital PDF,
// publisher where the site already holds the text of every row, vision for a
// page image and a model, and mixed for a year where some issues are born
// digital and the rest are not.
//
// Publisher text that covers only part of a year does not change the path. The
// corpus transcribes every printed page and assembles articles out of pages, so
// a year where a fifth of the rows come with text still has to go through the
// pages: the text is worth having as a check on the transcription and it is not
// a way out of doing it. The text rows column is there to say how much of that
// check exists.
func (y YearProbe) Path() string {
	switch {
	case y.Issues > 0 && y.Native == y.Issues:
		return "native"
	case y.Native > 0:
		return "mixed"
	case y.Rows > 0 && y.TextRows == y.Rows:
		return "publisher"
	default:
		return "vision"
	}
}

// ProbeSources reads the issue list into a per year report. Passing years
// limits it, and passing none reports the whole archive.
func ProbeSources(issues *manifest.Issues, years []int) *Probe {
	out := &Probe{}
	for _, year := range issues.YearList() {
		if len(years) > 0 && !hasYear(years, year) {
			continue
		}
		inYear := issues.Year(year)
		y := YearProbe{Year: year, Issues: len(inYear)}
		for _, iss := range inYear {
			if d := iss.Sources.Digital; d != nil {
				y.Scan++
				y.Rows += d.Rows
				y.TextRows += d.TextRows
			}
			if m := iss.Sources.MCCME; m != nil {
				if m.Rows > 0 || strings.HasSuffix(m.URL, "index.htm") {
					y.Mirror++
				}
				if m.DjVuURL != "" {
					y.DjVu++
				}
				if m.PDFURL != "" {
					y.PDF++
					if year >= kvantmccme.FirstNativeYear {
						y.Native++
					}
				}
			}
			if m := iss.Sources.MathNet; m != nil {
				y.MathNet++
				if m.FullText {
					y.FullText++
				}
			}
		}
		out.Years = append(out.Years, y)
	}
	return out
}

// String is the table the command prints. Every column is a count of issues out
// of the issues that year, because a year where one source has four issues of
// six is the interesting case and a yes or no would hide it.
func (p *Probe) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-6s %6s %6s %6s %6s %6s %6s %8s %11s %s\n",
		"year", "issues", "scan", "toc", "pdf", "native", "djvu", "mathnet", "text rows", "path")
	for _, y := range p.Years {
		fmt.Fprintf(&b, "%-6d %6d %6d %6d %6d %6d %6d %8d %5d/%-5d %s\n",
			y.Year, y.Issues, y.Scan, y.Mirror, y.PDF, y.Native, y.DjVu,
			y.MathNet, y.TextRows, y.Rows, y.Path())
	}
	fmt.Fprint(&b, p.total())
	return b.String()
}

func (p *Probe) total() string {
	var t YearProbe
	paths := map[string]int{}
	for _, y := range p.Years {
		t.Issues += y.Issues
		t.Scan += y.Scan
		t.Mirror += y.Mirror
		t.PDF += y.PDF
		t.Native += y.Native
		t.DjVu += y.DjVu
		t.MathNet += y.MathNet
		t.TextRows += y.TextRows
		t.Rows += y.Rows
		paths[y.Path()] += y.Issues
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-6s %6d %6d %6d %6d %6d %6d %8d %5d/%-5d\n",
		"all", t.Issues, t.Scan, t.Mirror, t.PDF, t.Native, t.DjVu,
		t.MathNet, t.TextRows, t.Rows)
	for _, name := range []string{"native", "publisher", "mixed", "vision"} {
		if n := paths[name]; n > 0 {
			fmt.Fprintf(&b, "%d issues take the %s path\n", n, name)
		}
	}
	return b.String()
}

func hasYear(years []int, year int) bool {
	for _, y := range years {
		if y == year {
			return true
		}
	}
	return false
}
