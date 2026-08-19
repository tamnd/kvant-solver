package publish

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// WriteRefused writes the report of the formulas a build could not typeset.
//
// Rule 7 already refuses a page whose math spans do not parse, and it caught a
// hundred and thirty nine of them in 1970 to 1989 alone. This is a different
// and much shorter list: the spans that got past the rule at reading time and
// that KaTeX still will not take. Nothing else in the project puts every
// formula in the corpus through a TeX implementation, so nothing else can know
// them.
//
// Each row is enough to fix the page without opening the site: the file, the
// line, what KaTeX objected to and the TeX the model wrote. The date is passed
// in rather than read off the clock so that a test can assert the whole report.
func WriteRefused(w io.Writer, list []Refused, date string) error {
	var out strings.Builder

	out.WriteString("# Formulas the site could not typeset\n\n")
	out.WriteString("Every math span in the corpus that KaTeX refuses, found by building the site.\n")
	fmt.Fprintf(&out, "Generated %s by kvant publish --report.\n\n", date)

	if len(list) == 0 {
		out.WriteString("Nothing. Every formula in the corpus parses.\n")
		_, err := io.WriteString(w, out.String())
		return err
	}

	pages := map[string]bool{}
	for _, r := range list {
		pages[r.Where] = true
	}
	fmt.Fprintf(&out, "%s over %s.\n\n", count(len(list), "formula", "formulas"),
		count(len(pages), "file", "files"))

	out.WriteString("## By year\n\n")
	out.WriteString("| year | formulas | files |\n|---|---|---|\n")
	for _, y := range byYear(list) {
		fmt.Fprintf(&out, "| %s | %d | %d |\n", y.year, y.formulas, y.files)
	}

	out.WriteString("\n## The formulas\n\n")
	out.WriteString("| file | line | what KaTeX said | the TeX |\n|---|---|---|---|\n")
	sorted := append([]Refused(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Where != sorted[j].Where {
			return sorted[i].Where < sorted[j].Where
		}
		return sorted[i].Line < sorted[j].Line
	})
	for _, r := range sorted {
		fmt.Fprintf(&out, "| %s | %d | %s | `%s` |\n",
			cell(r.Where), r.Line, cell(reason(r.Err)), cell(r.TeX))
	}

	_, err := io.WriteString(w, out.String())
	return err
}

type yearRow struct {
	year     string
	formulas int
	files    int
}

func byYear(list []Refused) []yearRow {
	formulas := map[string]int{}
	files := map[string]map[string]bool{}
	for _, r := range list {
		year, _, _ := strings.Cut(r.Where, "/")
		formulas[year]++
		if files[year] == nil {
			files[year] = map[string]bool{}
		}
		files[year][r.Where] = true
	}
	out := make([]yearRow, 0, len(formulas))
	for year, n := range formulas {
		out = append(out, yearRow{year: year, formulas: n, files: len(files[year])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].year < out[j].year })
	return out
}

// reason keeps the first line of what KaTeX said. It points at the character it
// stopped on with a run of combining marks, which is unreadable in a table and
// says nothing the TeX column does not.
func reason(err error) string {
	if err == nil {
		return "refused without saying why"
	}
	text, _, _ := strings.Cut(err.Error(), " at position ")
	return strings.TrimSpace(text)
}

// cell makes a string safe to put in a Markdown table.
func cell(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	text = strings.ReplaceAll(text, `|`, `\|`)
	// Counted in runes: a formula can carry Cyrillic inside \text and cutting
	// one in half would put a broken character in the report.
	if rs := []rune(text); len(rs) > 160 {
		text = string(rs[:157]) + "..."
	}
	return text
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// verb agrees with a count that count already formatted.
func verb(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
