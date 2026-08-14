package ocr

import "strings"

import "testing"

// The page that started this. It arrived in an answer file, twice running, on
// the largest question of a solve run, and it was read as a judge that had
// forgotten to write a verdict.
const wentWrong = "Something went wrong. If this issue persists please contact " +
	"us through our help center at help.openai.com."

func TestTheServicesOwnErrorPageIsNotAnAnswer(t *testing.T) {
	for _, text := range []string{
		wentWrong,
		"\n" + wentWrong + "\n",
		"You've reached our limit of messages per hour. Please try again later.",
		"Too many concurrent requests. Please try again.",
		// Verbatim, and the whole of the file: it came back as page 113 of
		// Théories spectrales and the audit compared it with the page.
		"Hmm...something seems to have gone wrong.",
	} {
		if ProviderFailure(text) == "" {
			t.Errorf("read as an answer: %q", text)
		}
	}
}

// An answer is entitled to discuss what goes wrong. The test is the opening of
// the text and its length, so a proof that says something went wrong in the
// third case is a proof.
func TestAnAnswerThatTalksAboutFailingIsAnAnswer(t *testing.T) {
	for _, text := range []string{
		"Suppose the induction fails. Something went wrong at the base case, " +
			"and the argument below shows where.",
		wentWrong + " is what the service says when it fails, and the point of " +
			"this passage is that " + strings.Repeat("the argument continues. ", 40),
		"",
	} {
		if why := ProviderFailure(text); why != "" {
			t.Errorf("thrown away as an error page: %q\nbecause %s", first(text, 80), why)
		}
	}
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
