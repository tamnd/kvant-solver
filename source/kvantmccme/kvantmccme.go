// Package kvantmccme reads kvant.mccme.ru, the older MCCME mirror. It carries
// two things the newer site does not: an author index that goes back to 1970,
// and, from 2007 on, a born digital PDF of each whole issue whose text layer
// can be lifted out without OCR.
//
// The pages are Windows-1251 and the markup is 1990s table soup with no
// structure to hang a selector on. Articles are separated by a pair of br
// tags and nothing else, so the parser splits on that and reads each chunk.
package kvantmccme

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

// BaseURL is the mirror root.
const BaseURL = "https://kvant.mccme.ru"

// IssueURL is an issue's contents, sorted by author.
func IssueURL(year int, month string) string {
	return fmt.Sprintf("%s/%d/%s/index.htm", BaseURL, year, month)
}

// IssueByTitleURL is the same contents sorted by article title. The two
// orderings are generated separately, so comparing them catches rows that one
// of them dropped.
func IssueByTitleURL(year int, month string) string {
	return fmt.Sprintf("%s/%d/%s/index_n.htm", BaseURL, year, month)
}

// AuthorURL is one author's page in the index.
func AuthorURL(slug string) string {
	return fmt.Sprintf("%s/au/%s.htm", BaseURL, slug)
}

// There is no IssuePDFURL. The mirror names its PDFs four different ways over
// twenty years, including 2006-01s.pdf and 2024-05-06.pdf, so the URL comes off
// the archive page rather than out of a format string. See archive.go.

// FirstPDFYear is the first year the mirror has whole issue PDFs for.
const FirstPDFYear = 2005

// FirstNativeYear is the first year those PDFs are born digital rather than a
// scan. From here an issue's text comes out with pdftotext and never goes near
// a vision model.
const FirstNativeYear = 2007

// Entry is one row of an issue's contents on the mirror.
type Entry struct {
	Title   string
	Authors []Author

	// Pages are the printed page numbers the mirror has GIF scans for. The
	// mirror scanned page by page, so this is a real list and not a range.
	Pages []int

	// GIFURLs lines up with Pages.
	GIFURLs []string

	// AllPagesURL is the mirror's own page holding every scan of this item.
	AllPagesURL string

	// PDFURL is set on the rows that got their own PDF rather than a scan.
	PDFURL string
}

// Author is a contributor as the mirror names them.
type Author struct {
	Name string
	Slug string
}

// Contents is one issue on the mirror.
type Contents struct {
	Year    int
	Month   string
	Entries []Entry
}

// Titles returns the entry titles in order, which is what the two orderings
// get compared on.
func (c *Contents) Titles() []string {
	out := make([]string, 0, len(c.Entries))
	for _, e := range c.Entries {
		out = append(out, e.Title)
	}
	return out
}

var (
	reBreakPair = regexp.MustCompile(`(?i)<br\s*/?>\s*<br\s*/?>`)
	reAuthorRef = regexp.MustCompile(`/au/([a-z0-9_]+)\.htm$`)
	reGIFPage   = regexp.MustCompile(`/(\d{4})/(\d{2})/p(\d+)\.htm$`)
	reArticlePD = regexp.MustCompile(`/pdf/\d{4}/\d{2}/[^/]+\.pdf$`)
	reAllPages  = regexp.MustCompile(`/\d{4}/\d{2}/[a-z0-9_]+\.htm$`)
)

// Decode turns a Windows-1251 page into UTF-8. Every page on the mirror is in
// that encoding and none of them say so in a way Go's HTML parser acts on, so
// reading one as UTF-8 gives a page of replacement characters rather than an
// error, which is the kind of bug that gets committed.
func Decode(r io.Reader) (io.Reader, error) { return source.DecodeWindows1251(r) }

// ParseContents reads one issue's contents page. The reader is the raw
// Windows-1251 bytes.
func ParseContents(r io.Reader, year int, month string) (*Contents, error) {
	utf8, err := Decode(r)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(utf8)
	if err != nil {
		return nil, err
	}
	base := IssueURL(year, month)

	body, err := doc.Find("body").Html()
	if err != nil {
		return nil, err
	}

	out := &Contents{Year: year, Month: month}
	for _, chunk := range reBreakPair.Split(entriesRegion(body), -1) {
		entry, ok := parseEntry(chunk, base)
		if ok {
			out.Entries = append(out.Entries, entry)
		}
	}
	if len(out.Entries) == 0 {
		return nil, fmt.Errorf("%d/%s: no entries, the markup has probably moved", year, month)
	}
	return out, nil
}

// entriesRegion drops the mirror's masthead.
//
// Entries are separated by a pair of breaks, and the masthead has a stack of
// four of them inside a table cell, so splitting the whole body cuts the
// masthead into pieces and hands back its bold lines as articles. It also puts
// no break pair between itself and the first entry, which then arrives glued to
// it and loses its title to the masthead. Everything from the sort selector on
// is contents, and every contents page the mirror has carries that selector.
func entriesRegion(body string) string {
	i := strings.Index(body, "сортировать")
	if i < 0 {
		return body
	}
	const end = "</div>"
	j := strings.Index(body[i:], end)
	if j < 0 {
		return body[i:]
	}
	// Past the closing tag, not up to it: a chunk that opens with a stray
	// closing tag closes the wrapper parseEntry puts around it, and the entry
	// then sits outside the element being read.
	return body[i+j+len(end):]
}

// parseEntry reads one chunk of the flat contents. A chunk without a bold
// title is page furniture, not an article.
func parseEntry(chunk string, base string) (Entry, bool) {
	chunk = strings.TrimSpace(chunk)
	if chunk == "" {
		return Entry{}, false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + chunk + "</div>"))
	if err != nil {
		return Entry{}, false
	}
	root := doc.Find("div").First()

	// The page furniture is laid out in tables and the entries are not, so the
	// footer that trails the last entry cannot contribute a title.
	root.Find("table, div, hr").Remove()

	// The mirror nests b inside b, so take the first one with text in it.
	var entry Entry
	root.Find("b").EachWithBreak(func(_ int, b *goquery.Selection) bool {
		if t := squeeze(b.Text()); t != "" {
			entry.Title = t
			return false
		}
		return true
	})
	if entry.Title == "" {
		return Entry{}, false
	}

	seenAuthor := map[string]bool{}
	root.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := resolve(base, a.AttrOr("href", ""))
		text := squeeze(a.Text())
		switch {
		case reAuthorRef.MatchString(href):
			slug := reAuthorRef.FindStringSubmatch(href)[1]
			if seenAuthor[slug] {
				return
			}
			seenAuthor[slug] = true
			name := squeeze(a.Find("i").Text())
			if name == "" {
				name = text
			}
			entry.Authors = append(entry.Authors, Author{
				Name: strings.TrimRight(name, " ,;"),
				Slug: slug,
			})
		case reGIFPage.MatchString(href):
			// The link text is the printed page number. A link whose text is
			// not a number is the "all pages" link wearing the same shape.
			n, err := strconv.Atoi(text)
			if err != nil {
				return
			}
			entry.Pages = append(entry.Pages, n)
			entry.GIFURLs = append(entry.GIFURLs, href)
		case reArticlePD.MatchString(href):
			entry.PDFURL = href
		case reAllPages.MatchString(href):
			entry.AllPagesURL = href
		}
	})
	return entry, true
}

// resolve turns the mirror's ./../../ style hrefs into absolute URLs. Some of
// them have newlines inside the attribute, which is why the whitespace goes
// first.
func resolve(base, href string) string {
	href = strings.Join(strings.Fields(href), "")
	if href == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return href
	}
	r, err := url.Parse(href)
	if err != nil {
		return href
	}
	return b.ResolveReference(r).String()
}

func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }
