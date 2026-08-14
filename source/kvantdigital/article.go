package kvantdigital

import (
	"fmt"
	"io"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Article is one item from a table of contents, as its own page.
//
// The page is read for three things. Whether the publisher has supplied the
// text, which is the one question that decides whether this item ever needs a
// vision model. Which sheets of the scan it sits on, which the assembler needs
// and the contents row only half answers. And the download link, which is not
// a link at all but a signed one hour token, so it is read at the moment it is
// used and never written down.
type Article struct {
	Slug  string
	Title string
	URL   string

	// Text is the publisher's own text where there is any. It is kept as the
	// HTML the site serves rather than as flattened text, because the
	// mathematics in it is markup and flattening it throws the mathematics
	// away.
	Text      string
	HasText   bool
	TextRunes int

	// SheetFiles are the scan files this item runs over, in printed order, as
	// file names rather than URLs. They come off the page's own image sources.
	SheetFiles []string

	// DownloadURL is the signed PDF link. It carries a JWT that expires in two
	// hours, which is why nothing in this project stores one.
	DownloadURL string
}

// MinTextRunes is how much text a text block has to hold before it counts as
// the article.
//
// Every article page has a block--text, and on most of them it holds a line
// saying where the piece was first published rather than the piece itself. A
// 2020 reprint of a 1987 article carries about eighty characters of that and
// nothing else, so a parser that treats the block's presence as the answer
// marks the whole reprint era as needing no transcription.
const MinTextRunes = 400

// ParseArticle reads one article page.
func ParseArticle(r io.Reader, slug string) (*Article, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	a := &Article{Slug: slug}

	a.Title = clean(doc.Find("h1").First().Text())
	if a.Title == "" {
		return nil, fmt.Errorf("%s: no heading on the article page, the markup has probably moved", slug)
	}

	if block := doc.Find(".block--text.copy--target").First(); block.Length() > 0 {
		if runes := len([]rune(clean(block.Text()))); runes >= MinTextRunes {
			html, err := block.Html()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", slug, err)
			}
			a.Text = strings.TrimSpace(html)
			a.TextRunes = runes
			a.HasText = true
		}
	}

	seen := map[string]bool{}
	doc.Find(".swiper-slide img[src]").Each(func(_ int, s *goquery.Selection) {
		file := scanFile(absolute(s.AttrOr("src", "")))
		if file == "" || seen[file] {
			return
		}
		seen[file] = true
		a.SheetFiles = append(a.SheetFiles, file)
	})

	doc.Find(`a[href*="rpc/dl/"]`).Each(func(_ int, s *goquery.Selection) {
		if a.DownloadURL == "" {
			a.DownloadURL = absolute(s.AttrOr("href", ""))
		}
	})
	return a, nil
}
