package ocr

import "strings"

// The chat service answers its own failures in the same place it answers
// questions. A dropped generation comes back as "Something went wrong", a
// throttled account comes back as a sentence about limits, and both arrive as
// the contents of the answer file with an exit status of nothing wrong.
//
// Read as an answer, that page is a page of OCR that lost the mathematics, or a
// judge with no verdict in it, or a translation of nothing. The caller then
// asks again, is told the answer could not be read, and files the exercise as
// failed for a reason that has nothing to do with the exercise. It is worth
// naming for what it is at the point it arrives.

// failures are the openings of the service's error pages. Matched against the
// front of the answer and not anywhere in it, because a solution is entitled to
// discuss what goes wrong, and a page that begins with an apology is not a
// solution that mentions one.
var failures = []string{
	"Something went wrong",
	// The same failure worded for a person rather than for a log. It came back
	// as the whole of a page of Théories spectrales, from a profile that had
	// just been rotated onto, and the audit read it as a page the model and the
	// extractor disagreed about.
	"Hmm...something seems to have gone wrong",
	"You've reached our limit",
	"You have reached our limit",
	"Too many concurrent requests",
	"Conversation not found",
	"An error occurred while processing your request",
	"Our systems have detected unusual activity",
	"The server had an error while processing your request",
	"There was an error generating a response",
}

// answerLimit is how long a text can be and still be an error page. The pages
// are a sentence or two, and the bound is what keeps a long answer that opens
// by quoting one of them from being thrown away.
const answerLimit = 600

// ProviderFailure names the service's own error page, and returns the empty
// string for anything that might be an answer.
func ProviderFailure(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || len(trimmed) > answerLimit {
		return ""
	}
	for _, opening := range failures {
		if strings.HasPrefix(trimmed, opening) {
			return condense(trimmed)
		}
	}
	return ""
}
