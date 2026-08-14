package kvantdigital

import (
	"fmt"
	"io"
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

// ParsePages reads the page list out of an issue's viewer page.
//
// This one request gives the whole scan manifest for an issue: every sheet,
// its file name, and the number printed on it. It replaces sweeping the scan
// directory until the server stops answering, which would be about twenty one
// thousand speculative requests for the archive and would still not say which
// sheet carries which printed page.
func ParsePages(r io.Reader, issueKey string) ([]Sheet, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}

	var out []Sheet
	seen := map[int]bool{}
	doc.Find("div.page-gfx").Each(func(_ int, s *goquery.Selection) {
		// The viewer opens with a spacer whose data-rel is the letter X, there
		// to make the two page spread land the right way round. It repeats the
		// first sheet, so taking it would double the cover.
		ord, err := strconv.Atoi(s.AttrOr("data-rel", ""))
		if err != nil || seen[ord] {
			return
		}
		src := absolute(s.Find("img").First().AttrOr("src", ""))
		file := scanFile(src)
		if file == "" {
			return
		}
		seen[ord] = true

		sheet := Sheet{
			Ord:      ord,
			Label:    clean(s.Find(".ph-no").First().Text()),
			File:     file,
			ImageURL: src,
		}
		// A label that is a bare number is the printed page number. Anything
		// else is a cover or an insert and has no number of its own.
		if n, err := strconv.Atoi(sheet.Label); err == nil {
			sheet.Page = n
		}
		out = append(out, sheet)
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no sheets in the page list, the markup has probably moved", issueKey)
	}
	return out, nil
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
