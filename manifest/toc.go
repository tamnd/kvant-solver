package manifest

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// TOC is every printed table of contents, one block per issue. It is the plan
// the assemble stage works to: a row here is a thing that has to end up in the
// corpus, and a row that never does is a hole worth reporting.
type TOC struct {
	Issues []IssueTOC `yaml:"issues"`
}

// IssueTOC is the contents of one issue.
type IssueTOC struct {
	Key string `yaml:"key"`

	// Rows is the reconciled list. Where the two mirrors disagreed the
	// disagreement is in errata.yaml and the row here is the one that won.
	Rows []Row `yaml:"rows"`
}

// Row is one line of a printed table of contents. A row is not always an
// article: the contents list answer columns, chess pages, competition results
// and cover captions the same way, and all of them are content.
type Row struct {
	Rubric    string `yaml:"rubric,omitempty"`
	RubricSub string `yaml:"rubric_sub,omitempty"`
	Title     string `yaml:"title"`
	Authors   string `yaml:"authors,omitempty"`

	// Page is the number printed on the page. Sheet is where that page sits in
	// the scan. They differ because the covers are scanned and not numbered.
	//
	// Sheet is the mirror's own numbering, taken out of the viewer URL, and the
	// mirror counts its images from zero. A page file is numbered from one. Add
	// one to cross between them, and do not do it by eye: the two agree closely
	// enough that a run with the conversion missing looks nearly right and is
	// wrong by one page everywhere.
	Page  int `yaml:"page,omitempty"`
	Sheet int `yaml:"sheet,omitempty"`

	// Pages is the full list of pages the item occupies where a source gives
	// it, which the MCCME mirror does. An article interrupted by a full page
	// advert is not contiguous, so this is a list and not a range.
	Pages []int `yaml:"pages,omitempty"`

	Slug    string `yaml:"slug,omitempty"`
	URL     string `yaml:"url,omitempty"`
	HasText bool   `yaml:"has_text,omitempty"`

	// Source names where the row came from, so that a row present in only one
	// contents can be told from one both agree on.
	Source string `yaml:"source"`
}

// Set replaces the rows for one issue, or adds them.
func (t *TOC) Set(key string, rows []Row) {
	if i := slices.IndexFunc(t.Issues, func(b IssueTOC) bool { return b.Key == key }); i >= 0 {
		t.Issues[i].Rows = rows
		return
	}
	t.Issues = append(t.Issues, IssueTOC{Key: key, Rows: rows})
}

// Get returns the rows for one issue.
func (t *TOC) Get(key string) ([]Row, bool) {
	if i := slices.IndexFunc(t.Issues, func(b IssueTOC) bool { return b.Key == key }); i >= 0 {
		return t.Issues[i].Rows, true
	}
	return nil, false
}

// Rows counts every row in every issue.
func (t *TOC) Rows() int {
	n := 0
	for _, b := range t.Issues {
		n += len(b.Rows)
	}
	return n
}

// Sort puts the issues in the order issues.yaml uses and the rows in printed
// order, so a resync of unchanged data produces no diff.
func (t *TOC) Sort() {
	for i := range t.Issues {
		slices.SortStableFunc(t.Issues[i].Rows, func(a, b Row) int {
			if c := cmp.Compare(a.Page, b.Page); c != 0 {
				return c
			}
			return cmp.Compare(a.Title, b.Title)
		})
	}
	slices.SortFunc(t.Issues, func(a, b IssueTOC) int { return compareKeys(a.Key, b.Key) })
}

// compareKeys orders kvant_1975_1 before kvant_1975_2 and kvant_1976_1, and
// puts kvant_1976_5-6 where the shelf puts it.
func compareKeys(a, b string) int {
	ay, an := splitKey(a)
	by, bn := splitKey(b)
	if c := cmp.Compare(ay, by); c != 0 {
		return c
	}
	return cmp.Compare(FirstNumber(an), FirstNumber(bn))
}

// KeyYear is the year in an issue key, or zero if the string is not one.
func KeyYear(key string) int {
	year, _ := splitKey(key)
	n, err := strconv.Atoi(year)
	if err != nil {
		return 0
	}
	return n
}

func splitKey(key string) (year string, number string) {
	parts := strings.Split(key, "_")
	if len(parts) != 3 {
		return key, ""
	}
	return parts[1], parts[2]
}
