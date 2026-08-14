package kvantdigital

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Sheet is one scanned sheet of an issue.
//
// A sheet is not the same thing as a printed page. The scan includes the four
// cover surfaces and any unnumbered insert, and the magazine numbers only what
// is between them, so an eighty page issue of Kvant is eighty four sheets.
type Sheet struct {
	// Ord is the sheet's position in the scan, counting from zero, and also
	// the anchor the contents link to, so the contents entry that points at pN
	// is pointing at the sheet with Ord N.
	Ord int

	// Label is what the viewer prints under the sheet. For a numbered page it
	// is the page number. For the covers it is a word.
	Label string

	// Page is the printed page number, or zero on a cover or an insert. This
	// is the number a reader looking something up will have.
	Page int

	// File is the name of the JPEG without its extension, which is not
	// derivable from either of the numbers above. Cover backs and inserts get
	// a letter suffix, so an issue whose last page is 0080 also has 0080_a and
	// 0080_b for the inside and outside of the back cover.
	File string

	ImageURL string
}

// Numbered reports whether the sheet carries a printed page number.
func (s Sheet) Numbered() bool { return s.Page > 0 }

// ParsePages reads the scan manifest out of an issue page.
//
// The whole manifest is on the issue page, in the thumbnail strip above the
// contents: every sheet, its file name, and the number printed on it. The
// viewer would say the same thing, but robots.txt disallows /view, so the
// thumbnails are both the allowed way to get this and the cheaper one, since
// the issue page has to be fetched anyway.
//
// It replaces sweeping the scan directory until the server stops answering,
// which would be about twenty one thousand speculative requests for the
// archive and would still not say which sheet carries which printed page.
func ParsePages(r io.Reader, issueKey string) ([]Sheet, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	sheets := parseSheets(doc)
	if len(sheets) == 0 {
		return nil, fmt.Errorf("%s: no sheets in the thumbnail strip, the markup has probably moved", issueKey)
	}
	return sheets, nil
}

// parseSheets walks the thumbnails. Every figure on the page carries data-ord
// zero, so the ordinal comes from the pN in the link the thumbnail wraps. That
// link is read and never followed.
func parseSheets(doc *goquery.Document) []Sheet {
	var out []Sheet
	seen := map[int]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		m := reViewHref.FindStringSubmatch(absolute(a.AttrOr("href", "")))
		if m == nil {
			return
		}
		ord, err := strconv.Atoi(m[1])
		if err != nil || seen[ord] {
			return
		}
		// The contents rows link into the viewer as well, one per article. They
		// have no thumbnail, and that is what tells the two apart.
		src := absolute(a.Find("img[src]").First().AttrOr("src", ""))
		file := scanFile(src)
		if file == "" {
			return
		}
		seen[ord] = true

		label := clean(a.Find("img").First().AttrOr("alt", ""))
		if label == "" {
			label = clean(a.Find("figcaption").First().Text())
		}
		out = append(out, Sheet{
			Ord:      ord,
			Label:    label,
			Page:     printedPage(label),
			File:     file,
			ImageURL: src,
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Ord < out[j].Ord })
	return out
}

// printedPage reads the number off a label. The site writes a numbered page as
// Стр. 1, with a non breaking space that clean has already folded, and writes a
// word for everything else: a cover surface or an insert has no number of its
// own and comes back as zero.
func printedPage(label string) int {
	rest, ok := strings.CutPrefix(label, "Стр.")
	if !ok {
		rest = label
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest))
	if err != nil {
		return 0
	}
	return n
}

// scanFile pulls 0080_a out of .../jpg/0080_a.jpg.
func scanFile(src string) string {
	i := strings.LastIndex(src, "/jpg/")
	if i < 0 {
		return ""
	}
	name := src[i+len("/jpg/"):]
	name = strings.TrimSuffix(name, ".jpg")
	if name == "" || strings.Contains(name, "/") {
		return ""
	}
	return name
}

// SheetForPage finds the sheet carrying a printed page number.
func SheetForPage(sheets []Sheet, page int) (Sheet, bool) {
	for _, s := range sheets {
		if s.Page == page {
			return s, true
		}
	}
	return Sheet{}, false
}
