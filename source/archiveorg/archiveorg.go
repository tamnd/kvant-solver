// Package archiveorg reads the Kvant year volumes on archive.org.
//
// The Internet Archive holds 1970 to 1992 as twenty three bound year volumes
// under a single item, each one scanned and run through ABBYY. That is a
// second reading of exactly the years this project reads with a model, made by
// somebody else, from a different set of paper, with a different engine, and it
// is free.
//
// It is not a better reading. The paper is Soviet newsprint and the OCR shows
// it: Latin letters land in Cyrillic words, ё is lost, and mathematics comes
// out as punctuation. Roughly three quarters of the words our own reading finds
// on a page are in the archive's text of the same page. Nothing here is meant
// to replace a page.
//
// What it is good for is disagreement. Two readings that were made
// independently and agree on a page make it very unlikely that either dropped a
// paragraph, and a page where they disagree badly is worth a person's time.
// That check costs a download per year and no model calls at all, which is the
// whole reason this package exists.
package archiveorg

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tamnd/kvant-solver/source"
)

// Item is the archive.org identifier holding every year volume.
//
// One item rather than one per year, which is worth knowing because it means
// the whole of 1970 to 1992 is described by a single metadata request and the
// per year files are told apart by their names alone.
const Item = "kvant-journal"

// FirstYear and LastYear are what the item covers. The magazine ran from 1970
// and still runs, so this is the Soviet run and the first two post Soviet
// years, and nothing after that.
const (
	FirstYear = 1970
	LastYear  = 1992
)

// Client fetches archive.org.
type Client struct {
	Fetcher *source.Client
}

// New returns a client with the default manners.
func New() *Client { return &Client{Fetcher: source.NewClient()} }

// FileURL is where one of a year's files lives.
func FileURL(year int, suffix string) string {
	return fmt.Sprintf("https://archive.org/download/%s/Kvant%d%s", Item, year, suffix)
}

// Leaf is one scanned side of paper as the archive holds it.
//
// Index is the archive's own numbering and starts at zero. Label is the printed
// page number that the OCR read off the page, which is empty on a cover, on a
// plate, and on any page where it read nothing it believed. It is not used for
// alignment, only for checking it: see Align.
type Leaf struct {
	Index int
	Label string
	Text  string
}

// Volume is one year as the archive holds it, in the order the leaves are
// bound.
type Volume struct {
	Year   int
	Leaves []Leaf
}

// Volume downloads one year.
//
// Three files, because the archive keeps the text and the page boundaries
// apart. The text is one run of characters with nothing in it to say where a
// page ends, the index says which characters belong to which leaf, and the page
// numbers are what the OCR read at the top and bottom of each one. Together
// they are a per page reading. The plain _djvu.txt file that looks like the
// obvious thing to fetch is the same text with no boundaries at all, which is
// why it is not fetched.
func (c *Client) Volume(ctx context.Context, year int) (*Volume, error) {
	if year < FirstYear || year > LastYear {
		return nil, fmt.Errorf("archive.org holds %d to %d, not %d", FirstYear, LastYear, year)
	}
	text, err := c.gzText(ctx, year, "_hocr_searchtext.txt.gz")
	if err != nil {
		return nil, err
	}
	spans, err := c.pageIndex(ctx, year)
	if err != nil {
		return nil, err
	}
	labels, err := c.pageNumbers(ctx, year)
	if err != nil {
		return nil, err
	}
	return Build(year, text, spans, labels)
}

// Span is the half open range of one leaf in the volume's text.
//
// The archive writes four numbers per leaf. The first two are offsets into the
// search text and the second two are offsets into the hOCR, which is the same
// text with a box round every word and forty times the size. Only the first
// pair is kept.
type Span struct{ From, To int }

// Build assembles a volume from the three files.
//
// It is separate from Volume so that the assembly can be tested without a
// server, which matters here more than usual: the one thing that can go
// silently wrong is the offsets being read as the wrong unit, and that produces
// text rather than an error.
func Build(year int, text string, spans []Span, labels []string) (*Volume, error) {
	if len(spans) == 0 {
		return nil, fmt.Errorf("the %d volume: the page index is empty", year)
	}
	// The offsets count characters and not bytes, which is the trap in this
	// format. Every page of this magazine is Cyrillic, so in UTF-8 the byte
	// length is nearly twice the character length, and slicing the file by
	// these numbers as if they were bytes lands halfway through the volume and
	// returns real Russian prose from the wrong page. It reads as an alignment
	// that is merely poor rather than as a bug.
	runes := []rune(text)
	if last := spans[len(spans)-1].To; last != len(runes) {
		return nil, fmt.Errorf("the %d volume: the index ends at %d and the text is %d characters", year, last, len(runes))
	}
	v := &Volume{Year: year, Leaves: make([]Leaf, len(spans))}
	for i, span := range spans {
		if span.From < 0 || span.To > len(runes) || span.From > span.To {
			return nil, fmt.Errorf("the %d volume: leaf %d spans %d to %d", year, i, span.From, span.To)
		}
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		v.Leaves[i] = Leaf{Index: i, Label: label, Text: string(runes[span.From:span.To])}
	}
	return v, nil
}

// gzText downloads and unpacks one of the gzipped files.
func (c *Client) gzText(ctx context.Context, year int, suffix string) (string, error) {
	u := FileURL(year, suffix)
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return "", err
	}
	zr, err := gzip.NewReader(strings.NewReader(string(resp.Body)))
	if err != nil {
		return "", fmt.Errorf("%s: %w", u, err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", fmt.Errorf("%s: %w", u, err)
	}
	return string(out), nil
}

// pageIndex reads the leaf offsets.
func (c *Client) pageIndex(ctx context.Context, year int) ([]Span, error) {
	raw, err := c.gzText(ctx, year, "_hocr_pageindex.json.gz")
	if err != nil {
		return nil, err
	}
	var rows [][]int
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, fmt.Errorf("the %d page index: %w", year, err)
	}
	spans := make([]Span, 0, len(rows))
	for i, row := range rows {
		if len(row) < 2 {
			return nil, fmt.Errorf("the %d page index: leaf %d has %d numbers", year, i, len(row))
		}
		spans = append(spans, Span{From: row[0], To: row[1]})
	}
	return spans, nil
}

// pageNumbers reads what the OCR made of the printed page numbers.
func (c *Client) pageNumbers(ctx context.Context, year int) ([]string, error) {
	u := FileURL(year, "_page_numbers.json")
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Pages []struct {
			LeafNum    int    `json:"leafNum"`
			PageNumber string `json:"pageNumber"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(resp.Body, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	out := make([]string, len(doc.Pages))
	for _, p := range doc.Pages {
		if p.LeafNum >= 0 && p.LeafNum < len(out) {
			out[p.LeafNum] = strings.TrimSpace(p.PageNumber)
		}
	}
	return out, nil
}
