package pdfsrc

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// This file reads the geometry of a page and not only its words.
//
// pdftotext on its own throws away everything that is not a character, and for
// this magazine that is most of the mathematics. A subscript comes back as a
// digit standing next to a letter, a superscript comes back as a letter
// standing after it, and ε_0^m arrives as "ε m 0" with nothing to say which
// was which. The positions are the difference: the subscript sits lower and is
// set smaller, and both facts are in the box pdftotext will print if it is
// asked for one. Asking for it is what makes a born digital page recoverable as
// mathematics rather than as a sentence with the formulas smeared into it.

// Box is where something was set on the page, in points from the top left.
type Box struct {
	XMin, YMin, XMax, YMax float64
}

// Width is how far the box runs across the page.
func (b Box) Width() float64 { return b.XMax - b.XMin }

// Height is how deep it runs, which for a word is the size the type was set
// in and is the whole of how a script is told from a letter on the line.
func (b Box) Height() float64 { return b.YMax - b.YMin }

// Base is the bottom of the box, which for a word of running text is close
// enough to the baseline to compare two words on one line.
func (b Box) Base() float64 { return b.YMax }

// Word is one word of the text layer with the box it was set in.
type Word struct {
	Box
	Text string
}

// Line is the words pdftotext found on one line of one block.
type Line struct {
	Box
	Words []Word
}

// Block is what pdftotext takes to be a paragraph. The blocks of a two column
// page come out column by column rather than across the gutter, which is the
// reading order the magazine was set in and the one a transcription needs.
type Block struct {
	Box
	Lines []Line
}

// LayoutPage is one page of the file.
type LayoutPage struct {
	Number        int
	Width, Height float64
	Blocks        []Block
}

// Layout runs pdftotext -bbox-layout over a page range and reads the result.
func (s *Source) Layout(ctx context.Context, first, last int) ([]LayoutPage, error) {
	args := []string{"-bbox-layout"}
	if first > 0 {
		args = append(args, "-f", strconv.Itoa(first))
	}
	if last > 0 {
		args = append(args, "-l", strconv.Itoa(last))
	}
	args = append(args, s.Path, "-")
	out, err := s.Run.Run(ctx, "pdftotext", args...)
	if err != nil {
		return nil, err
	}
	pages, err := ParseLayout(strings.NewReader(string(out)))
	if err != nil {
		return nil, err
	}
	// pdftotext numbers the pages it printed from one, so a range has to be
	// renumbered to say which pages of the file these are. Everything else in
	// this project is keyed on the file's own numbering.
	if first > 1 {
		for i := range pages {
			pages[i].Number += first - 1
		}
	}
	return pages, nil
}

// printable drops the control characters XML has no way to carry.
//
// A few of these files have a group separator or a shift out where a letter
// should be, left behind by an encoding that was already wrong when the issue
// went to press. The XML parser is right to refuse them and stopping the run
// over one is not: the character is not on the paper and no page needs it. It
// goes out here rather than in the reader above, because a byte at a time is
// how it can be dropped without touching the multi byte letters around it.
type printable struct{ r io.Reader }

func (p printable) Read(buf []byte) (int, error) {
	for {
		n, err := p.r.Read(buf)
		out := buf[:0]
		for _, b := range buf[:n] {
			if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
				continue
			}
			out = append(out, b)
		}
		// A read that was all control characters has to go round again. Handing
		// back nothing with no error is a reader some callers spin on.
		if len(out) > 0 || err != nil {
			return len(out), err
		}
	}
}

// ParseLayout reads the XHTML pdftotext -bbox-layout writes.
//
// It is read as a stream rather than unmarshalled into a tree, because the
// document is an XHTML page with a doctype and a head on it and only the four
// elements below matter. The parser is put in non strict mode for the same
// reason: the header is HTML and the body is XML, and a run of 68 pages should
// not fail over an entity in a title.
func ParseLayout(r io.Reader) ([]LayoutPage, error) {
	dec := xml.NewDecoder(printable{r})
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var (
		pages []LayoutPage
		page  *LayoutPage
		block *Block
		line  *Line
		word  *Word
		text  strings.Builder
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read the page layout: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "page":
				pages = append(pages, LayoutPage{
					Number: len(pages) + 1,
					Width:  attrFloat(t, "width"),
					Height: attrFloat(t, "height"),
				})
				page = &pages[len(pages)-1]
			case "block":
				if page == nil {
					continue
				}
				page.Blocks = append(page.Blocks, Block{Box: attrBox(t)})
				block = &page.Blocks[len(page.Blocks)-1]
			case "line":
				if block == nil {
					continue
				}
				block.Lines = append(block.Lines, Line{Box: attrBox(t)})
				line = &block.Lines[len(block.Lines)-1]
			case "word":
				if line == nil {
					continue
				}
				line.Words = append(line.Words, Word{Box: attrBox(t)})
				word = &line.Words[len(line.Words)-1]
				text.Reset()
			}
		case xml.CharData:
			if word != nil {
				text.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "page":
				page, block, line, word = nil, nil, nil, nil
			case "block":
				block, line, word = nil, nil, nil
			case "line":
				line, word = nil, nil
			case "word":
				if word != nil {
					word.Text = strings.TrimSpace(text.String())
					word = nil
				}
			}
		}
	}
	// A word with no text is a box around nothing, which the tool emits for a
	// glyph it has no character for, and carrying it would put an empty token
	// in the middle of a formula.
	for i := range pages {
		for j := range pages[i].Blocks {
			for k := range pages[i].Blocks[j].Lines {
				words := pages[i].Blocks[j].Lines[k].Words
				pages[i].Blocks[j].Lines[k].Words = slices.DeleteFunc(words,
					func(w Word) bool { return w.Text == "" })
			}
		}
	}
	return pages, nil
}

// BodyHeight is the height of a word of running text on this page, taken as the
// median over every word on it.
//
// The median and not the mean, because a page carries a title set in 24 point
// and a footnote set in 6, and an average of the three is a size nothing on the
// page is. It is what a subscript is judged small against.
func (p LayoutPage) BodyHeight() float64 {
	var heights []float64
	for _, block := range p.Blocks {
		for _, line := range block.Lines {
			for _, w := range line.Words {
				if h := w.Height(); h > 0 {
					heights = append(heights, h)
				}
			}
		}
	}
	if len(heights) == 0 {
		return 0
	}
	slices.Sort(heights)
	return heights[len(heights)/2]
}

// Base is the baseline of a line, taken as the median over its words so that a
// line which is half subscripts is still measured against its own text.
func (l Line) Base() float64 {
	if len(l.Words) == 0 {
		return 0
	}
	bases := make([]float64, 0, len(l.Words))
	for _, w := range l.Words {
		bases = append(bases, w.Base())
	}
	slices.Sort(bases)
	return bases[len(bases)/2]
}

func attrBox(e xml.StartElement) Box {
	return Box{
		XMin: attrFloat(e, "xMin"),
		YMin: attrFloat(e, "yMin"),
		XMax: attrFloat(e, "xMax"),
		YMax: attrFloat(e, "yMax"),
	}
}

func attrFloat(e xml.StartElement, name string) float64 {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			f, _ := strconv.ParseFloat(a.Value, 64)
			return f
		}
	}
	return 0
}
