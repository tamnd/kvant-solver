package manifest

import "slices"

// PathsFile is where the extraction plan lives.
const PathsFile = "paths.yaml"

// Paths is what textguard decided, per issue, counted per path. The page by
// page decision lives in the page manifest next to the scan, because it is
// about bytes in a cache and it changes whenever the cache does. What is
// committed to the corpus is this: how many pages of each issue take each path,
// and what was measured about the issue PDF to arrive at that.
//
// It is the file the cost of the project is read off. A decade of vision pages
// is money and days, and this says how many there are before any of it is
// spent.
type Paths struct {
	Issues []IssuePaths `yaml:"issues"`
	Totals PathCount    `yaml:"totals"`
}

// IssuePaths is one issue's plan.
type IssuePaths struct {
	Key   string `yaml:"key"`
	Year  int    `yaml:"year"`
	Sheet int    `yaml:"sheets"`

	PathCount `yaml:",inline"`

	// PDF is what the full issue file from the mirror turned out to be, where
	// there is one. It is kept because "2007 on is born digital" is a claim
	// about measurements, and this is the measurement.
	PDF *Layer `yaml:"pdf,omitempty"`

	// Note is why this issue came out the way it did, where the counts alone do
	// not say it.
	Note string `yaml:"note,omitempty"`
}

// PathCount is how many pages take each path.
type PathCount struct {
	Native    int `yaml:"native"`
	Publisher int `yaml:"publisher"`
	Vision    int `yaml:"vision"`

	// Undecided is a sheet textguard could not place, which in practice means a
	// sheet that has not been downloaded yet.
	Undecided int `yaml:"undecided,omitempty"`
}

// Layer is what the text layer of one issue PDF measured.
type Layer struct {
	Pages    int  `yaml:"pages"`
	Born     bool `yaml:"born_digital"`
	Fonts    int  `yaml:"fonts"`
	Embedded int  `yaml:"fonts_embedded"`
	Subset   int  `yaml:"fonts_subset"`

	// Runes is the median count of non space characters over the sampled body
	// pages, and Cyrillic is the share of the letters on those pages that are
	// Russian. A scan wrapped in a PDF has neither.
	Runes    int     `yaml:"runes_per_page"`
	Cyrillic float64 `yaml:"cyrillic"`

	// Recode is the encoding the text layer has to be read back through, empty
	// when pdftotext got it right on its own. Some of the mirror's files embed
	// their fonts with no ToUnicode map, and their Russian comes out of
	// pdftotext as the Latin-1 characters standing for the CP1251 bytes.
	Recode string `yaml:"recode,omitempty"`

	// Offset is how far the file's page numbering runs ahead of the printed
	// numbering, measured from the numbers printed on the pages themselves. The
	// covers and the inside front matter are why it is rarely zero.
	Offset int `yaml:"page_offset"`

	Why string `yaml:"why,omitempty"`
}

// Add adds another count into this one.
func (c *PathCount) Add(other PathCount) {
	c.Native += other.Native
	c.Publisher += other.Publisher
	c.Vision += other.Vision
	c.Undecided += other.Undecided
}

// Total is how many pages the count covers.
func (c PathCount) Total() int { return c.Native + c.Publisher + c.Vision + c.Undecided }

// Set adds an issue or replaces the one already there.
func (p *Paths) Set(iss IssuePaths) {
	if i := slices.IndexFunc(p.Issues, func(x IssuePaths) bool { return x.Key == iss.Key }); i >= 0 {
		p.Issues[i] = iss
		return
	}
	p.Issues = append(p.Issues, iss)
}

// Get returns one issue's plan.
func (p *Paths) Get(key string) (*IssuePaths, bool) {
	if i := slices.IndexFunc(p.Issues, func(x IssuePaths) bool { return x.Key == key }); i >= 0 {
		return &p.Issues[i], true
	}
	return nil, false
}

// Sort puts the issues in shelf order and adds the counts up again.
func (p *Paths) Sort() {
	slices.SortFunc(p.Issues, func(a, b IssuePaths) int { return compareKeys(a.Key, b.Key) })
	p.Totals = PathCount{}
	for _, iss := range p.Issues {
		p.Totals.Add(iss.PathCount)
	}
}

// Year returns the plans for one year.
func (p *Paths) Year(year int) []IssuePaths {
	var out []IssuePaths
	for _, iss := range p.Issues {
		if iss.Year == year {
			out = append(out, iss)
		}
	}
	return out
}

// YearList returns the years present, oldest first.
func (p *Paths) YearList() []int {
	var out []int
	seen := map[int]bool{}
	for _, iss := range p.Issues {
		if !seen[iss.Year] {
			seen[iss.Year] = true
			out = append(out, iss.Year)
		}
	}
	slices.Sort(out)
	return out
}

// Count adds up the issues that satisfy a test.
func (p *Paths) Count(keep func(IssuePaths) bool) PathCount {
	var out PathCount
	for _, iss := range p.Issues {
		if keep == nil || keep(iss) {
			out.Add(iss.PathCount)
		}
	}
	return out
}

// Born is how many issues of a year were measured to have a born digital PDF,
// and how many were looked at. The pair is the point: three of twelve is not a
// year anybody should call native.
func (p *Paths) Born(year int) (born, measured int) {
	for _, iss := range p.Issues {
		if iss.Year != year || iss.PDF == nil {
			continue
		}
		measured++
		if iss.PDF.Born {
			born++
		}
	}
	return born, measured
}
