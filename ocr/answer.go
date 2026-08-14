package ocr

import (
	"regexp"
	"strings"
)

// toolHeader is the block ocr-batch writes above every answer: the source path,
// the model slug it happened to draw from the pool, the time and the elapsed
// seconds, fenced in the same three dashes a Markdown front matter uses.
var toolHeader = regexp.MustCompile(`(?s)\A---\r?\n.*?\r?\n---\r?\n`)

// StripToolHeader removes that block.
//
// It has to go for two reasons. It is not part of the page, so leaving it in
// makes every file open with four lines of machinery that no reader wants and
// every later stage has to skip. And its source line carries the absolute path
// on the rented box, /root/kvant-ocr/in/..., which is somebody's home directory
// and has no business in a public corpus.
//
// It also makes the length rule useless. A refusal of a hundred and thirty
// characters comes to two hundred and fifty seven with the header on top, which
// is over the minimum, so without this the pipeline writes "I don't see an
// image attached" to the corpus as though it were a page of an issue.
func StripToolHeader(text string) string {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	// Only the tool's own header, which has these keys. A page that really
	// begins with a horizontal rule keeps it.
	match := toolHeader.FindString(trimmed)
	if match == "" || !strings.Contains(match, "\nsource:") || !strings.Contains(match, "\nelapsed:") {
		return text
	}
	return strings.TrimLeft(trimmed[len(match):], " \t\r\n")
}
