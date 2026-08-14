package kvantmccme

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ArchiveURL is the mirror's front page, which is also its whole site map: one
// link per issue for every year it holds, plus every PDF and DjVu it has. It is
// worth one request at the start of a run, because it is the difference between
// knowing what the mirror has and guessing at URLs.
func ArchiveURL() string { return BaseURL + "/index.htm" }

// ArchiveRef is one issue as the mirror lists it. Every field is a URL the page
// actually named, apart from ByTitleURL, which is the sibling of a contents
// page and is checked by fetching it rather than trusted.
type ArchiveRef struct {
	Year   int
	Number string
	Month  string

	// URL is the contents page for the years the mirror typeset itself, or the
	// issue's own page for 2012 and 2013, where it holds a PDF and a cover
	// instead.
	URL        string
	ByTitleURL string

	// PDFURL is the whole issue. From 2005 these are what the mirror has
	// instead of typeset text, and from 2007 they are born digital.
	PDFURL string

	// DjVuURL covers the gap: the second half of 2003 and all of 2004 exist
	// only as a DjVu scan.
	DjVuURL string
}

// Archive is what the mirror says it holds.
type Archive struct {
	Refs []ArchiveRef
}

// Get returns the issue with this year and number.
func (a *Archive) Get(year int, number string) (ArchiveRef, bool) {
	for _, r := range a.Refs {
		if r.Year == year && r.Number == number {
			return r, true
		}
	}
	return ArchiveRef{}, false
}

// The link shapes the mirror uses. The trailing single letter on a PDF name is
// the mirror's own versioning, 2006-01s.pdf and 2016-05u.pdf, and means nothing
// about the issue.
var (
	reArchiveContents = regexp.MustCompile(`^/?(\d{4})/(\d{2})/index\.htm$`)
	reArchiveDir      = regexp.MustCompile(`^/?(\d{4})/(\d{2,3})/$`)
	reArchivePDFFlat  = regexp.MustCompile(`^/?pdf/(\d{4})-(\d{2,4}(?:-\d{2})?)[a-z]?\.pdf$`)
	reArchivePDFYear  = regexp.MustCompile(`^/?pdf/(\d{4})/(\d{4})-(\d{2,4}(?:-\d{2})?)[a-z]?\.pdf$`)
	reArchiveDjVu     = regexp.MustCompile(`^/?djvu/(\d{4})_(\d{2})\.djvu$`)
	reSlashes         = regexp.MustCompile(`/{2,}`)
)

// ParseArchive reads the front page into one row per issue. The mirror grew
// over thirty years and names things four different ways, so this is a list of
// patterns rather than one: 1970 to 2003 are typeset contents pages, 2003 and
// 2004 are DjVu, 2005 to 2010 are flat PDFs, 2012 and 2013 are per issue
// directories, and 2014 on are PDFs under a year directory.
func ParseArchive(r io.Reader) (*Archive, error) {
	dec, err := Decode(r)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(dec)
	if err != nil {
		return nil, err
	}

	out := &Archive{}
	index := map[string]int{}
	at := func(year int, number string) *ArchiveRef {
		key := fmt.Sprintf("%d_%s", year, number)
		if i, ok := index[key]; ok {
			return &out.Refs[i]
		}
		out.Refs = append(out.Refs, ArchiveRef{
			Year:   year,
			Number: number,
			Month:  MonthOf(number),
		})
		index[key] = len(out.Refs) - 1
		return &out.Refs[len(out.Refs)-1]
	}

	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		href = strings.TrimSpace(href)
		if href == "" || strings.Contains(href, "://") {
			return
		}
		// pdf/2018//2018-11.pdf is on the page as written. The server serves it
		// either way, and a doubled slash is not a reason to lose an issue.
		href = reSlashes.ReplaceAllString(href, "/")
		switch {
		case reArchiveContents.MatchString(href):
			m := reArchiveContents.FindStringSubmatch(href)
			year, month := atoi(m[1]), m[2]
			ref := at(year, ArchiveNumber(month))
			ref.URL = BaseURL + "/" + strings.TrimPrefix(href, "/")
			ref.ByTitleURL = IssueByTitleURL(year, month)

		case reArchiveDir.MatchString(href):
			m := reArchiveDir.FindStringSubmatch(href)
			year := atoi(m[1])
			ref := at(year, ArchiveNumber(m[2]))
			if ref.URL == "" {
				ref.URL = BaseURL + "/" + strings.TrimPrefix(href, "/")
			}

		case reArchivePDFFlat.MatchString(href):
			m := reArchivePDFFlat.FindStringSubmatch(href)
			at(atoi(m[1]), ArchiveNumber(m[2])).PDFURL = BaseURL + "/" + strings.TrimPrefix(href, "/")

		case reArchivePDFYear.MatchString(href):
			m := reArchivePDFYear.FindStringSubmatch(href)
			if m[1] != m[2] {
				// A file under one year's directory naming another year is a
				// typo on the page, and following it would file an issue under
				// the wrong year.
				return
			}
			at(atoi(m[2]), ArchiveNumber(m[3])).PDFURL = BaseURL + "/" + strings.TrimPrefix(href, "/")

		case reArchiveDjVu.MatchString(href):
			m := reArchiveDjVu.FindStringSubmatch(href)
			at(atoi(m[1]), ArchiveNumber(m[2])).DjVuURL = BaseURL + "/" + strings.TrimPrefix(href, "/")
		}
	})
	if len(out.Refs) == 0 {
		return nil, fmt.Errorf("no issues on the archive page")
	}
	return out, nil
}

// ArchiveNumber turns the mirror's file naming into a printed issue number.
// A double issue is written four ways over the years and 5-6 is not one of
// them: 056 as a directory, 05-06 and 56 in a PDF name, and 1112 for the
// November and December of a recent year.
func ArchiveNumber(s string) string {
	if a, b, ok := strings.Cut(s, "-"); ok {
		return trimZero(a) + "-" + trimZero(b)
	}
	switch len(s) {
	case 4:
		// 1112 is November and December.
		if n, ok := pair(s[:2], s[2:]); ok {
			return n
		}
	case 3:
		// 056 is issues 5 and 6 under one cover.
		if n, ok := pair(s[1:2], s[2:]); ok {
			return n
		}
	case 2:
		// 56 is the same pair with the leading zero dropped. Anything up to 12
		// is read as the month it plainly is, so 12 stays December rather than
		// becoming issues 1 and 2.
		if atoi(s) > 12 {
			if n, ok := pair(s[:1], s[1:]); ok {
				return n
			}
		}
	}
	return trimZero(s)
}

// pair reads a run together number, and only accepts it if the two halves are
// consecutive months. That is what a double issue is, and it keeps 12 from
// being read as issues 1 and 2.
func pair(a, b string) (string, bool) {
	first, err := strconv.Atoi(a)
	if err != nil {
		return "", false
	}
	second, err := strconv.Atoi(b)
	if err != nil {
		return "", false
	}
	if first < 1 || second != first+1 || second > 12 {
		return "", false
	}
	return fmt.Sprintf("%d-%d", first, second), true
}

// MonthOf is the zero padded month a number starts in, which is how the rest
// of the corpus names a directory.
func MonthOf(number string) string {
	first, _, _ := strings.Cut(number, "-")
	n, err := strconv.Atoi(first)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%02d", n)
}

func trimZero(s string) string {
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0"
	}
	return s
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
