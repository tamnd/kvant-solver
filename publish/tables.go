package publish

import (
	"strings"
)

// mendTables repairs the delimiter row of a pipe table so the parser sees one.
//
// A translation pass reads «---» as punctuation and hands back «—», and a
// table whose header starts with an empty corner cell often comes back with
// one delimiter cell fewer than it has columns. Either way the parser does not
// recognise the table and the reader gets a paragraph of pipes. The row under
// a header is the only thing touched, and only when it holds nothing but
// pipes, colons, dashes and spaces.
func mendTables(body string) string {
	lines := strings.Split(body, "\n")
	changed := false
	for i := 1; i < len(lines); i++ {
		header := strings.TrimSpace(lines[i-1])
		if !strings.HasPrefix(header, "|") || !delimiterish(lines[i]) {
			continue
		}
		want := len(cells(header))
		have := cells(strings.TrimSpace(lines[i]))
		out := make([]string, 0, want)
		for k := 0; k < want; k++ {
			cell := "---"
			if k < len(have) {
				c := strings.TrimSpace(have[k])
				left, right := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":") && len(c) > 1
				cell = map[[2]bool]string{{false, false}: "---", {true, false}: ":---", {false, true}: "---:", {true, true}: ":---:"}[[2]bool{left, right}]
			}
			out = append(out, " "+cell+" ")
		}
		mended := "|" + strings.Join(out, "|") + "|"
		if mended != lines[i] {
			lines[i] = mended
			changed = true
		}
	}
	if !changed {
		return body
	}
	return strings.Join(lines, "\n")
}

// delimiterish says whether a line could be the delimiter row of a table.
func delimiterish(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.ContainsAny(line, "-—–−") {
		return false
	}
	for _, r := range line {
		switch r {
		case '|', ':', '-', '—', '–', '−', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// cells splits a pipe row into its cells, without the outer pipes.
func cells(row string) []string {
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	return strings.Split(row, "|")
}
