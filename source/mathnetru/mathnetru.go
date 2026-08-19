// Package mathnetru reads www.mathnet.ru, the Steklov Institute's journal
// archive. It carries Kvant only from 2005 on, so it is not a way to get the
// magazine. What it is good for is metadata: it lists every issue it holds
// with the printed number as the magazine itself wrote it, and it says which
// issues have full text behind them. That makes it a third opinion on the
// issue list, and a third opinion is the only way to notice that one of the
// other two sites has quietly dropped a double issue.
package mathnetru

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/kvant-solver/source"
)

// BaseURL is the site root.
const BaseURL = "https://www.mathnet.ru"

// JournalID is what mathnet calls Kvant in every query string.
const JournalID = "kvant"

// FirstYear is the earliest year mathnet holds. Everything before it has to
// come from one of the other two sources.
const FirstYear = 2005

// ContentsURL is the one page listing every year and every issue.
func ContentsURL() string {
	return fmt.Sprintf("%s/php/archive.phtml?jrnid=%s&wshow=contents&option_lang=rus", BaseURL, JournalID)
}

// IssueURL is the contents of one issue. The number in the query is not always
// the number on the cover: a double issue printed as 5-6 is issue=5 here.
func IssueURL(year, issue int) string {
	return fmt.Sprintf("%s/php/archive.phtml?jrnid=%s&wshow=issue&year=%d&volume=&issue=%d&option_lang=rus",
		BaseURL, JournalID, year, issue)
}

// IssueRef is one issue as the archive page lists it.
type IssueRef struct {
	Year int

	// Number is the number as printed on the cover, so it can read 5-6.
	// Query is the single number the site's own URL uses for that issue.
	// Keeping both is the point of this package: the pair is what lets an
	// issue list built from another source be checked against this one.
	Number string
	Query  int

	// FullText is the site's own claim that the articles in this issue have
	// text behind them and not just a scan.
	FullText bool

	URL      string
	CoverURL string
}

// Double reports whether the cover number covers more than one issue.
func (r IssueRef) Double() bool { return strings.Contains(r.Number, "-") }

var reIssueHref = regexp.MustCompile(`wshow=issue`)

// fullTextMark is the second line of the title attribute on an issue that has
// text. An issue with scans only carries the first line and nothing else, so
// the presence of this phrase is the whole test.
const fullTextMark = "Доступны полные тексты статей"

// ParseContents reads the archive page. The reader is the raw Windows-1251
// bytes as served.
func ParseContents(r io.Reader) ([]IssueRef, error) {
	utf8, err := source.DecodeWindows1251(r)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(utf8)
	if err != nil {
		return nil, err
	}

	var out []IssueRef
	seen := map[string]bool{}
	doc.Find("a.SLink[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if !reIssueHref.MatchString(href) {
			return
		}
		year, issue, ok := splitIssueQuery(href)
		if !ok {
			return
		}
		// The link text is the number the magazine printed. The image inside
		// the link contributes nothing to it, so this is just the label.
		number := strings.Join(strings.Fields(a.Text()), "")
		if number == "" {
			return
		}
		key := fmt.Sprintf("%d/%s", year, number)
		if seen[key] {
			return
		}
		seen[key] = true

		ref := IssueRef{
			Year:     year,
			Number:   number,
			Query:    issue,
			FullText: strings.Contains(a.AttrOr("title", ""), fullTextMark),
			URL:      IssueURL(year, issue),
			CoverURL: absolute(a.Find("img").AttrOr("src", "")),
		}
		out = append(out, ref)
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no issue links on the archive page, the markup has probably moved")
	}
	return out, nil
}

// PaperRef is one article as an issue page lists it.
//
// Only the bibliography is taken: what the article is called, who signed it,
// where it sits in the issue, and the permanent identifier mathnet gives it.
// The site does hold full text for a good part of this range and none of it is
// read here, because the archive already has the text from its own sources and
// what mathnet is worth is the identifier.
type PaperRef struct {
	// ID is the identifier in the site's own permanent link, as in kvant2822.
	// It never changes and it is the reason this manifest is worth keeping:
	// our tags are ours, and this is the one name for a Kvant article that
	// somebody outside this project would recognise.
	ID  string
	URL string

	Title   string
	Authors []string

	// PageFirst and PageLast are the printed page range. A one page piece has
	// them equal, and a piece with no range printed has them zero.
	PageFirst int
	PageLast  int

	// FullText is the site's claim that this article has text behind it.
	FullText bool
}

var rePaperHref = regexp.MustCompile(`^/rus/` + JournalID + `(\d+)$`)

// reDash splits a printed page range. The site writes it with an en dash and
// the decoder has already turned the entity into the character.
var reDash = regexp.MustCompile(`\s*[-–—]\s*`)

// ParseIssue reads the contents of one issue. The reader is the raw
// Windows-1251 bytes as served.
//
// The rows are found off the article links rather than off the table, because
// the page puts the running head, the cover navigation and the article list in
// tables that look alike, and the only thing unique to an article is the link.
func ParseIssue(r io.Reader) ([]PaperRef, error) {
	utf8, err := source.DecodeWindows1251(r)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(utf8)
	if err != nil {
		return nil, err
	}

	var out []PaperRef
	seen := map[string]bool{}
	doc.Find("a.SLink[href]").Each(func(_ int, a *goquery.Selection) {
		m := rePaperHref.FindStringSubmatch(a.AttrOr("href", ""))
		if m == nil {
			return
		}
		id := JournalID + m[1]
		if seen[id] {
			return
		}
		title := clean(a.Text())
		if title == "" {
			return
		}
		seen[id] = true

		cell := a.Parent()
		row := a.Closest("tr")
		ref := PaperRef{
			ID:      id,
			URL:     BaseURL + "/rus/" + id,
			Title:   title,
			Authors: byline(cell, title),
			// The marker is an image with the claim in its title attribute,
			// which is the same way the archive page says it.
			FullText: strings.Contains(row.Find("img").AttrOr("title", ""), "полный текст"),
		}
		ref.PageFirst, ref.PageLast = pages(row)
		out = append(out, ref)
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no article links on the issue page, the markup has probably moved")
	}
	return out, nil
}

// byline is the text left in the cell once the title link is taken out of it.
//
// The site writes the authors after a <br> with no markup of their own, so
// there is nothing to select and the remainder is the byline. Splitting on the
// comma is right here because mathnet writes initials before the surname, so a
// comma only ever separates two people.
func byline(cell *goquery.Selection, title string) []string {
	rest := clean(strings.TrimPrefix(clean(cell.Text()), title))
	var out []string
	for part := range strings.SplitSeq(rest, ",") {
		if name := clean(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// pages reads the printed range out of the last cell of the row.
//
// The cell and not the <nobr> inside it, because the site only reaches for a
// <nobr> when there is a dash to keep from wrapping. A one page article is a
// bare number in the cell, and reading the tag instead of the cell silently
// loses the page of every short piece in the issue.
func pages(row *goquery.Selection) (first, last int) {
	text := clean(row.Find("td").Last().Text())
	if text == "" {
		return 0, 0
	}
	part := reDash.Split(text, 2)
	first, err := strconv.Atoi(strings.TrimSpace(part[0]))
	if err != nil {
		return 0, 0
	}
	if len(part) == 1 {
		return first, first
	}
	last, err = strconv.Atoi(strings.TrimSpace(part[1]))
	if err != nil {
		return first, first
	}
	return first, last
}

// clean collapses the whitespace, including the non-breaking spaces the site
// holds initials together with.
func clean(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}

// Years returns the years present, newest first, in the order the page lists
// them.
func Years(refs []IssueRef) []int {
	var out []int
	seen := map[int]bool{}
	for _, r := range refs {
		if !seen[r.Year] {
			seen[r.Year] = true
			out = append(out, r.Year)
		}
	}
	return out
}

// splitIssueQuery pulls the year and the issue number out of an archive link.
func splitIssueQuery(href string) (year, issue int, ok bool) {
	u, err := url.Parse(href)
	if err != nil {
		return 0, 0, false
	}
	q := u.Query()
	if q.Get("jrnid") != JournalID {
		return 0, 0, false
	}
	year, err = strconv.Atoi(q.Get("year"))
	if err != nil {
		return 0, 0, false
	}
	issue, err = strconv.Atoi(q.Get("issue"))
	if err != nil {
		return 0, 0, false
	}
	return year, issue, true
}

func absolute(href string) string {
	href = strings.TrimSpace(href)
	switch {
	case href == "":
		return ""
	case strings.HasPrefix(href, "//"):
		return "https:" + href
	case strings.HasPrefix(href, "/"):
		return BaseURL + href
	default:
		return href
	}
}
