package manifest

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// Issues is the list of every issue of the magazine, with what each source
// holds for it. It is the first thing any run reads and the only thing that
// says how much work there is.
type Issues struct {
	Years  int     `yaml:"years"`
	Count  int     `yaml:"count"`
	Issues []Issue `yaml:"issues"`
}

// Issue is one issue of the magazine.
type Issue struct {
	Key    string `yaml:"key"`
	Year   int    `yaml:"year"`
	Number string `yaml:"number"`
	Dir    string `yaml:"dir"`

	Title string `yaml:"title,omitempty"`

	// Pages is the page count the publisher printed in the bibliographic line.
	// Sheets is how many surfaces the scan has, which is larger: the four
	// cover surfaces are scanned and are not numbered, and inserts are
	// scanned and are not numbered either.
	Pages  int `yaml:"pages,omitempty"`
	Sheets int `yaml:"sheets,omitempty"`

	Sources Sources `yaml:"sources"`
}

// Sources is what each of the three archives holds for one issue.
type Sources struct {
	Digital *Digital `yaml:"kvant_digital,omitempty"`
	MCCME   *MCCME   `yaml:"kvant_mccme,omitempty"`
	MathNet *MathNet `yaml:"mathnet_ru,omitempty"`
}

// Digital is kvant.digital, the one source that has every page of every issue
// as a scan.
type Digital struct {
	URL string `yaml:"url"`

	// Rows is how many lines the printed table of contents has. TextRows is
	// how many of them the site already holds publisher text for, and those
	// are exactly the ones that do not need to go through OCR.
	Rows     int `yaml:"toc_rows,omitempty"`
	TextRows int `yaml:"text_rows,omitempty"`
}

// MCCME is the older MCCME mirror. It has a second, independent table of
// contents, which is the only way to notice that a row went missing from the
// first one.
type MCCME struct {
	URL        string `yaml:"url"`
	ByTitleURL string `yaml:"url_by_title"`

	// PDFURL is the whole issue as one file. The mirror has these from 2005,
	// and from 2007 they are born digital, so those years never see an OCR
	// call.
	PDFURL string `yaml:"pdf_url,omitempty"`

	// DjVuURL covers the gap the PDFs leave: the second half of 2003 and all
	// of 2004 exist on the mirror only as a DjVu scan.
	DjVuURL string `yaml:"djvu_url,omitempty"`

	// Rows and TitleRows are how many contents rows each of the two orderings
	// gave. They are kept apart on purpose: the mirror generates the two pages
	// separately, and a row in one and not the other is the fault this whole
	// second reading exists to catch.
	Rows      int `yaml:"toc_rows,omitempty"`
	TitleRows int `yaml:"toc_rows_by_title,omitempty"`
}

// MathNet is mathnet.ru, which holds 2005 on.
type MathNet struct {
	URL string `yaml:"url"`

	// Number is the issue number as mathnet prints it. It is here rather than
	// taken on trust because a disagreement with the number the other two
	// sources use is a real finding about a double issue.
	Number   string `yaml:"number"`
	FullText bool   `yaml:"full_text"`
}

// Add appends an issue, or merges into the one already there. Sync calls this
// once per source, so the second and third source fill in fields rather than
// replacing a row.
func (m *Issues) Add(iss Issue) {
	if i := m.index(iss.Key); i >= 0 {
		m.Issues[i].merge(iss)
		return
	}
	m.Issues = append(m.Issues, iss)
}

// Get returns an issue by key.
func (m *Issues) Get(key string) (*Issue, bool) {
	if i := m.index(key); i >= 0 {
		return &m.Issues[i], true
	}
	return nil, false
}

func (m *Issues) index(key string) int {
	return slices.IndexFunc(m.Issues, func(i Issue) bool { return i.Key == key })
}

func (i *Issue) merge(other Issue) {
	if other.Title != "" {
		i.Title = other.Title
	}
	if other.Pages > 0 {
		i.Pages = other.Pages
	}
	if other.Sheets > 0 {
		i.Sheets = other.Sheets
	}
	if other.Sources.Digital != nil {
		i.Sources.Digital = other.Sources.Digital
	}
	if other.Sources.MCCME != nil {
		i.Sources.MCCME = other.Sources.MCCME
	}
	if other.Sources.MathNet != nil {
		i.Sources.MathNet = other.Sources.MathNet
	}
}

// Sort puts the list in shelf order and refreshes the two counts. Every write
// goes through it, so the file has one canonical order and a resync of
// unchanged data produces no diff.
func (m *Issues) Sort() {
	slices.SortFunc(m.Issues, func(a, b Issue) int {
		if c := cmp.Compare(a.Year, b.Year); c != 0 {
			return c
		}
		return cmp.Compare(FirstNumber(a.Number), FirstNumber(b.Number))
	})
	m.Count = len(m.Issues)
	years := map[int]bool{}
	for _, i := range m.Issues {
		years[i.Year] = true
	}
	m.Years = len(years)
}

// Year returns the issues of one year.
func (m *Issues) Year(year int) []Issue {
	var out []Issue
	for _, i := range m.Issues {
		if i.Year == year {
			out = append(out, i)
		}
	}
	return out
}

// YearList returns the years present, oldest first.
func (m *Issues) YearList() []int {
	var out []int
	seen := map[int]bool{}
	for _, i := range m.Issues {
		if !seen[i.Year] {
			seen[i.Year] = true
			out = append(out, i.Year)
		}
	}
	slices.Sort(out)
	return out
}

// NewIssue builds a row from a year and a printed number, filling in the key
// and the directory the way the corpus writes them.
func NewIssue(year int, number string) (Issue, error) {
	key, err := corpus.NewIssueKey(year, number)
	if err != nil {
		return Issue{}, err
	}
	return Issue{
		Key:    key.String(),
		Year:   year,
		Number: number,
		Dir:    key.Dir(),
	}, nil
}

// FirstNumber is the first month a printed number covers, so that 5-6 sorts
// between 4 and 7 rather than lexically after 4 and before 5.
func FirstNumber(number string) int {
	head, _, _ := strings.Cut(number, "-")
	n, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil {
		return 0
	}
	return n
}

// Month is the two digit month an issue is filed under on the MCCME mirror,
// which names its directories by the first month of the issue.
func Month(number string) string { return fmt.Sprintf("%02d", FirstNumber(number)) }
