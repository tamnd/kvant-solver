package kvantdigital

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// IssueRef is one issue as it appears on the index page.
type IssueRef struct {
	Year   int
	Number string
	Key    string
	URL    string
}

// Issue is the header of one issue plus its table of contents.
type Issue struct {
	Year        int
	Number      string
	Key         string
	Title       string
	Description string
	ISSN        string
	PageCount   int
	CoverURL    string
	URL         string
	Rows        []TOCRow

	// Sheets is the scan manifest, read off the thumbnail strip on the same
	// page. See ParsePages.
	Sheets []Sheet
}

// TOCRow is one line of a printed table of contents. A row is not always an
// article: the contents list answer columns, chess pages, competition results
// and cover captions the same way.
type TOCRow struct {
	Rubric    string
	RubricSub string
	Title     string
	Authors   string
	Slug      string
	URL       string

	// Page is the number printed on the page. Sheet is the position of that
	// page in the scan. They differ, usually by one or two, because the covers
	// are scanned but not numbered. Both are kept because the scan URL needs
	// the sheet and a reader looking something up needs the printed number.
	Page  int
	Sheet int

	// HasText is true when the site already holds publisher supplied text for
	// this item, so it does not need to go through OCR.
	HasText bool
}

// PageOffset is Sheet minus Page, taken from the first row that has both. Add
// it to a printed page number to get the sheet to fetch.
func (i *Issue) PageOffset() (int, bool) {
	for _, r := range i.Rows {
		if r.Page > 0 && r.Sheet > 0 {
			return r.Sheet - r.Page, true
		}
	}
	return 0, false
}

// ParseIssuesIndex reads the one page that lists the whole archive.
func ParseIssuesIndex(r io.Reader) ([]IssueRef, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	var out []IssueRef
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		m := reIssueHref.FindStringSubmatch(absolute(href))
		if m == nil {
			return
		}
		year, err := strconv.Atoi(m[1])
		if err != nil {
			return
		}
		key := IssueKey(year, m[2])
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, IssueRef{Year: year, Number: m[2], Key: key, URL: IssueURL(year, m[2])})
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no issue links on the index page, the markup has probably moved")
	}
	return out, nil
}

// ParseIssue reads one issue page. The year and number come from the caller
// because the page itself only says them in prose.
func ParseIssue(r io.Reader, year int, number string) (*Issue, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	key := IssueKey(year, number)
	iss := &Issue{
		Year:     year,
		Number:   number,
		Key:      key,
		URL:      IssueURL(year, number),
		CoverURL: CoverURL(key),
	}

	iss.Sheets = parseSheets(doc)
	iss.Title = strings.TrimSpace(doc.Find("title").First().Text())
	if i := strings.Index(iss.Title, " / "); i > 0 {
		iss.Title = strings.TrimSpace(iss.Title[:i])
	}
	readLinkedData(doc, iss)

	// The printed page count is worth having on its own: it is the number the
	// page sweep has to reach before it can call an issue complete.
	if m := rePageCount.FindStringSubmatch(iss.Description); m != nil {
		iss.PageCount, _ = strconv.Atoi(m[1])
	}

	rubric, sub := "", ""
	seen := map[string]bool{}
	doc.Find("ul.object--toc li").Each(func(_ int, li *goquery.Selection) {
		item := li.Find(".toc--item").First()
		if item.Length() == 0 {
			return
		}
		if item.HasClass("toc--section") {
			rubric = clean(item.Find(".toc--title").Text())
			sub = ""
			return
		}
		link := item.Find(".toc--title a").First()
		if link.Length() == 0 {
			return
		}
		href := absolute(link.AttrOr("href", ""))
		slug := ArticleSlug(href)
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true

		// The authors sit in an em inside the link, ahead of the title.
		text := link.Clone()
		authors := clean(text.Find("em").Text())
		text.Find("em").Remove()

		row := TOCRow{
			Rubric:    rubric,
			RubricSub: sub,
			Title:     clean(text.Text()),
			// Trailing commas go, trailing full stops stay: the last character
			// of "Бронштейн И. Н." belongs to the initial.
			Authors: strings.TrimRight(authors, " ,"),
			Slug:    slug,
			URL:     href,
		}
		if item.HasClass("toc--lev--1") {
			row.RubricSub = rubric
		}

		pageLink := item.Find(".toc--page a").First()
		row.Page, _ = strconv.Atoi(clean(pageLink.Text()))
		if m := reViewHref.FindStringSubmatch(absolute(pageLink.AttrOr("href", ""))); m != nil {
			row.Sheet, _ = strconv.Atoi(m[1])
		}

		// A glyph marks an item the site holds text for. The prop--noft class
		// on the same glyph means the opposite, so the class is the signal and
		// the glyph on its own is not.
		item.Find("ins").Each(func(_ int, ins *goquery.Selection) {
			if !ins.HasClass("prop--noft") && ins.Find("svg").Length() > 0 {
				row.HasText = true
			}
		})

		iss.Rows = append(iss.Rows, row)
	})
	if len(iss.Rows) == 0 {
		return nil, fmt.Errorf("%s: no contents rows, the markup has probably moved", key)
	}
	return iss, nil
}

// readLinkedData pulls the description and the ISSN out of the JSON-LD block.
// The description is the full bibliographic line, which is where the page
// count lives.
func readLinkedData(doc *goquery.Document, iss *Issue) {
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		var ld map[string]any
		if err := json.Unmarshal([]byte(s.Text()), &ld); err != nil {
			return
		}
		if ld["@type"] != "PublicationIssue" {
			return
		}
		if v, ok := ld["description"].(string); ok {
			iss.Description = v
		}
		if part, ok := ld["isPartOf"].(map[string]any); ok {
			if v, ok := part["issn"].(string); ok {
				iss.ISSN = v
			}
		}
	})
}

// clean squeezes whitespace and drops the invisible characters the site uses
// for typesetting. The site puts a non breaking space between every initial
// and every surname, and strings.Fields counts those as space, so those fold
// away on their own. The zero width joiners are the ones that matter: the
// contents write a problem range as М301<zwj>—<zwj>М305 so the browser will
// not break the line inside it, and if those survive into a title they end up
// in identifiers and in anything anyone tries to search for.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff', '\u00ad':
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
