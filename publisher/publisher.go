// Package publisher turns the text the archive already holds into corpus
// Markdown.
//
// kvant.digital carries the publisher's own text for some of the archive, about
// 2300 contents rows of the 11000, and where it exists it is free: no card, no
// tokens, no waiting. It is not a transcription of the scan, it is the
// magazine's own file, so on the rows it covers it should be better than
// anything a model produces from a JPEG.
//
// Should is not is, which is why nothing here is trusted on that argument. The
// text is converted, kept beside the vision reading of the same pages, and the
// two are diffed in diff.go. What comes out of that measurement decides whether
// a page is allowed to take this path, and the measurement is per year, because
// the archive's text for 1970 and its text for 2003 were typed decades apart by
// different people.
//
// The conversion is markup to markup and never text to text. The mathematics on
// these pages is in span.tex elements, and flattening the HTML to its text
// content would leave a formula as a run of Unicode with no delimiters around
// it, which is exactly the shape the audit cannot tell from prose. So the spans
// come out as $...$ and the Greek inside them comes out as TeX.
package publisher

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/tamnd/kvant-solver/mathtex"
	"github.com/tamnd/kvant-solver/ocr"
)

// Markdown converts one article's publisher HTML.
//
// The result follows the same conventions as a page the vision lane writes, so
// that the two are comparable and so that nothing downstream has to know which
// path a body took. Figures are a ⟦figure⟧ line and their caption, headings are
// ATX, and the mathematics is in dollars.
func Markdown(fragment string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return "", err
	}
	var out []string
	body := doc.Find("body").First()
	for _, node := range body.Nodes {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			out = append(out, blocks(child)...)
		}
	}
	text := strings.Join(out, "\n\n")
	text, _, _ = mathtex.Repair(text)
	return strings.TrimSpace(text) + "\n", nil
}

// blocks turns one node into the paragraphs it stands for. A div is not a
// paragraph and the site nests three of them around the text, so a node this
// does not recognise is descended into rather than being flattened.
func blocks(n *html.Node) []string {
	if n.Type == html.TextNode {
		if line := squeeze(n.Data); line != "" {
			return []string{line}
		}
		return nil
	}
	if n.Type != html.ElementNode {
		return nil
	}
	sel := &goquery.Selection{Nodes: []*html.Node{n}}
	switch n.Data {
	case "p":
		if line := squeeze(inline(sel)); line != "" {
			return []string{line}
		}
		return nil
	case "h1", "h2", "h3", "h4", "h5", "h6":
		if line := squeeze(inline(sel)); line != "" {
			return []string{strings.Repeat("#", heading(n.Data)) + " " + line}
		}
		return nil
	case "figure":
		return []string{figure(sel)}
	case "blockquote":
		var out []string
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			for _, block := range blocks(child) {
				out = append(out, "> "+block)
			}
		}
		return out
	case "ul", "ol":
		var out []string
		i := 0
		sel.ChildrenFiltered("li").Each(func(_ int, li *goquery.Selection) {
			i++
			mark := "- "
			if n.Data == "ol" {
				mark = itoa(i) + ". "
			}
			if line := squeeze(inline(li)); line != "" {
				out = append(out, mark+line)
			}
		})
		if len(out) == 0 {
			return nil
		}
		// One list is one block, so that a diff of the body does not see a
		// paragraph break between two items.
		return []string{strings.Join(out, "\n")}
	case "table":
		return []string{table(sel)}
	case "br", "hr":
		return nil
	case "script", "style":
		return nil
	}
	var out []string
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		out = append(out, blocks(child)...)
	}
	return out
}

// inline flattens the run of text inside a block, keeping the markup that
// carries meaning and dropping the markup that carries layout.
func inline(sel *goquery.Selection) string {
	var b strings.Builder
	for _, n := range sel.Nodes {
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			writeInline(&b, child)
		}
	}
	return b.String()
}

func writeInline(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
	default:
		return
	}
	sel := &goquery.Selection{Nodes: []*html.Node{n}}
	switch {
	case n.Data == "br":
		b.WriteString("\n")
		return
	case n.Data == "img":
		// An image inside a paragraph is a formula set as a picture, which is
		// the one thing this path cannot carry. It is marked rather than
		// dropped, so that the diff counts it and the page can be sent to the
		// lane that can read it.
		b.WriteString(" " + ocr.Illegible + " ")
		return
	case hasClass(n, "tex"):
		if tex := squeeze(sel.Text()); tex != "" {
			b.WriteString("$" + tex + "$")
		}
		return
	case n.Data == "sup", n.Data == "sub":
		mark := "^"
		if n.Data == "sub" {
			mark = "_"
		}
		if s := squeeze(sel.Text()); s != "" {
			b.WriteString("$" + mark + "{" + s + "}$")
		}
		return
	case n.Data == "em", n.Data == "i":
		b.WriteString("*" + strings.TrimSpace(inline(sel)) + "*")
		return
	case n.Data == "strong", n.Data == "b":
		b.WriteString("**" + strings.TrimSpace(inline(sel)) + "**")
		return
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		writeInline(b, child)
	}
}

// figure is a picture and its caption, written the way a transcribed page
// writes them. The image itself is not carried: the corpus holds text, and the
// figures are downloaded separately and by file name.
func figure(sel *goquery.Selection) string {
	caption := squeeze(sel.Find("figcaption").First().Text())
	if caption == "" {
		caption = squeeze(sel.Find("img").First().AttrOr("alt", ""))
	}
	if caption == "" {
		return ocr.FigureMarker
	}
	return ocr.FigureMarker + "\n" + caption
}

// table is a Markdown table, with the first row as the header because that is
// what Markdown requires and what these tables are.
func table(sel *goquery.Selection) string {
	var rows [][]string
	sel.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		var cells []string
		tr.Find("th,td").Each(func(_ int, td *goquery.Selection) {
			cells = append(cells, squeeze(inline(td)))
		})
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	})
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}
	var b strings.Builder
	for i, row := range rows {
		for len(row) < width {
			row = append(row, "")
		}
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
		if i == 0 {
			b.WriteString("|" + strings.Repeat(" --- |", width) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// squeeze puts a run of HTML whitespace back to single spaces, keeping the line
// breaks a <br> asked for. The site sets non breaking spaces between initials
// and their surname, and those are whitespace here: a corpus that keeps them is
// a corpus where a search for the name misses.
func squeeze(s string) string {
	// The zero width characters go first, and separately, because they are not
	// whitespace and nothing downstream would take them out. The site glues a
	// formula to the punctuation after it with zero width joiners, several in a
	// row on some pages, and left in they make every formula in the corpus a
	// word nothing else matches.
	s = invisible.Replace(s)
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, strings.Join(strings.Fields(line), " "))
	}
	joined := strings.Join(out, "\n")
	for strings.Contains(joined, "\n\n") {
		joined = strings.ReplaceAll(joined, "\n\n", "\n")
	}
	return strings.TrimSpace(joined)
}

func hasClass(n *html.Node, want string) bool {
	for _, attr := range n.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, class := range strings.Fields(attr.Val) {
			if class == want {
				return true
			}
		}
	}
	return false
}

func heading(tag string) int {
	// The article's own title is an h1 on the page and is not part of the body,
	// so anything inside the text block starts a level down.
	level := int(tag[1] - '0')
	return min(max(level, 2), 6)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
