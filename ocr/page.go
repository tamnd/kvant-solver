package ocr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The prompt asks for four markers, and this is where they are read back.
//
// They are markers and not Markdown because Markdown has nothing for any of
// them. A printed folio is not a heading, a rubric banner is not a heading
// either, a column break is not a rule, and a figure that is not described in
// prose has no image to point at. Written as ⟦word⟧ they survive a Markdown
// renderer untouched, they are visible to anyone reading the file, and they are
// findable with a plain search, which matters more than elegance for something
// three stages depend on.

const (
	// FolioMarker carries the page number as printed, which is not the sheet
	// number and not the PDF page.
	FolioMarker = "⟦folio"
	// RubricMarker introduces a standing section heading. Assemble splits on it.
	RubricMarker = "⟦rubric⟧"
	// ColumnMarker is where the reading moved from the left column to the
	// right, which is the one thing about a scan that cannot be checked
	// afterwards from the text.
	ColumnMarker = "⟦column⟧"
	// FigureMarker stands where a figure sits. Its caption follows it.
	FigureMarker = "⟦figure⟧"
	// Illegible is what the prompt asks for in place of a guess.
	Illegible = "⟪illegible⟫"
)

// NoFolio is what a page with no printed number says.
const NoFolio = "⟦folio none⟧"

var folioLine = regexp.MustCompile(`⟦folio\s+([0-9]+)⟧`)

// Folio reads the printed page number off a transcription.
//
// found is false when the page said it carries no number, which is a real
// answer and not a failure: the cover, the first page of an article that starts
// on a full bleed illustration, and the advertisement pages all print none.
func Folio(text string) (number int, found bool) {
	match := folioLine.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// HasFolioLine says whether the page answered the folio question at all, either
// with a number or with none. A page that answered neither did not follow the
// prompt, and that is worth a rule of its own: the page map is built from these
// lines and a page missing one is a hole in it.
func HasFolioLine(text string) bool {
	return folioLine.MatchString(text) || strings.Contains(text, NoFolio)
}

// Rubrics returns the rubric banners on a page, in the order they appear and
// with the marker taken off.
//
// More than one is normal. The answers column ends and the chess page begins
// halfway down a page, and the whole point of transcribing a page rather than
// an article is that both of them survive.
func Rubrics(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, RubricMarker) {
			continue
		}
		heading := strings.TrimSpace(strings.TrimPrefix(trimmed, RubricMarker))
		if heading != "" {
			out = append(out, heading)
		}
	}
	return out
}

// Figures counts the figure markers.
func Figures(text string) int { return strings.Count(text, FigureMarker) }

// Columns counts the column breaks. Two columns is one break, which is what
// nearly every body page of this magazine has.
func Columns(text string) int { return strings.Count(text, ColumnMarker) }

// StripMarkers removes every marker line, leaving the text as a reader would
// see it. It is what the audit compares against, and what a translation is
// taken from, since a marker is about the page and not about the words.
func StripMarkers(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case folioLine.MatchString(trimmed), trimmed == NoFolio:
			continue
		case trimmed == ColumnMarker:
			continue
		case strings.HasPrefix(trimmed, RubricMarker):
			kept = append(kept, strings.TrimSpace(strings.TrimPrefix(trimmed, RubricMarker)))
		case trimmed == FigureMarker:
			continue
		default:
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// FolioNote is how a folio reads in a report or an error.
func FolioNote(text string) string {
	if n, ok := Folio(text); ok {
		return fmt.Sprintf("folio %d", n)
	}
	if strings.Contains(text, NoFolio) {
		return "no printed folio"
	}
	return "no folio line at all"
}
