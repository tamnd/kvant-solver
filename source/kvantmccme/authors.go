package kvantmccme

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/tamnd/kvant-solver/source"
	"golang.org/x/text/encoding/charmap"
)

// Letters are the initial letters the author index is split by. The mirror has
// one page per letter and nothing else, so this list is the whole index.
var Letters = []rune("АБВГДЕЖЗИКЛМНОПРСТУФХЦЧШЩЭЮЯ")

// LetterURL is the author index page for one initial letter. The site names
// the file after the Windows-1251 byte of the letter, so А is 192 and Б is
// 193. It looks arbitrary and it is not: the pages were generated on a machine
// where that byte was the letter.
func LetterURL(letter rune) (string, bool) {
	b, ok := cp1251Byte(letter)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s/let/%d.htm", BaseURL, b), true
}

func cp1251Byte(r rune) (byte, bool) {
	s, err := charmap.Windows1251.NewEncoder().String(string(r))
	if err != nil || len(s) != 1 {
		return 0, false
	}
	return s[0], true
}

var reAuthorHref = regexp.MustCompile(`(?:^|/)au/([a-z0-9_-]+)\.htm$`)

// ParseAuthorIndex reads one letter page of the author index. The reader is
// the raw Windows-1251 bytes.
//
// The mirror's author list is not the same list as kvant.digital's personalia,
// and the difference is the point of having both. This one is a bare surname
// and one initial, so two different people with the same surname and initial
// share a page, and the same person appears twice where the site was fed a
// misprint. Both facts are worth knowing before either list is trusted.
func ParseAuthorIndex(r io.Reader) ([]Author, error) {
	utf8, err := source.DecodeWindows1251(r)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(utf8)
	if err != nil {
		return nil, err
	}

	var out []Author
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		m := reAuthorHref.FindStringSubmatch(strings.TrimSpace(a.AttrOr("href", "")))
		if m == nil || seen[m[1]] {
			return
		}
		name := squeeze(a.Text())
		if name == "" {
			return
		}
		seen[m[1]] = true
		out = append(out, Author{Name: name, Slug: m[1]})
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no authors on the page, the markup has probably moved")
	}
	return out, nil
}
