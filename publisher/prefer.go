package publisher

import (
	"fmt"
	"strings"
)

// ShortestShare is how much of our own reading the publisher's text has to
// account for before it is taken as the article.
//
// The two are not measuring the same thing and never agree exactly. Our body is
// every word on the pages the article covers, which includes the running heads,
// the folio lines, the figure captions and whatever the neighbouring piece left
// at the top of the first page. The publisher's is the article's prose as
// somebody typed it. Two thirds is roughly where that difference stops
// explaining itself.
//
// Below the line is not a worse transcription, it is a different document: the
// first paragraph of the piece, a note saying where it was first printed, or an
// abstract. Taking one of those would replace a whole article with a fragment
// and stamp the result as coming from the better source, which is the one
// failure here that nothing downstream could see.
//
// Where exactly the line goes turned out not to matter much, which is the best
// thing that can be said about a threshold. Over the fifty one articles of 1975
// the site has typed text for, the twenty nine this refuses come in at a median
// of 35% and only one of them lands anywhere near the line. The two populations
// are a whole article and a paragraph of one, with very little in between to be
// wrong about.
const ShortestShare = 0.66

// Prefer chooses between the body assembled from our own pages and the body the
// publisher typed.
//
// The typed one wins where it is whole. It was set from the manuscript rather
// than read off a photograph of the print, its mathematics is TeX rather than a
// reconstruction of TeX, and it cost nothing. Preferring it is the whole point
// of fetching it.
//
// The second return is why it was refused, empty when it was taken. A refusal
// is worth printing rather than swallowing: it is either an article the site
// has only a fragment of, which somebody may want to finish, or this rule
// misjudging a real article, which is a thing to find out about early.
func Prefer(assembled, typed string) (string, string) {
	if typed == "" {
		return assembled, ""
	}
	if assembled == "" {
		// Nothing to check it against, and text is text. This is an issue whose
		// pages have not been read, which is the case the lane exists for.
		return typed, ""
	}
	count, _ := Compare(assembled, typed)
	if share := count.Kept(); share < ShortestShare {
		return assembled, fmt.Sprintf(
			"the publisher's text holds %.0f%% of the words our pages do, which is a fragment and not the article",
			share*100)
	}
	return typed, ""
}

// Titled puts the article's title at the top of the publisher's text.
//
// An article assembled from our own pages carries the printed title as a
// heading because the title is printed on the page and the reading transcribes
// what is printed. What the publisher typed is the prose alone, the title
// being the page's own field rather than the first line of the body. Taking
// that text unchanged would leave those articles the only ones in the corpus
// whose body does not name them, and every reader of bodies rather than front
// matter would see the difference without being told about it.
//
// A typed text that already opens with a heading is left alone, which is the
// same rule the page format has always had.
func Titled(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || strings.HasPrefix(body, "## ") {
		return body
	}
	// A leading figure or rubric marker belongs above the title, where it is on
	// the page. Anything else is prose and the title goes first.
	var lead []string
	rest := body
	for {
		line, tail, _ := strings.Cut(rest, "\n")
		if trimmed := strings.TrimSpace(line); !strings.HasPrefix(trimmed, "⟦") {
			break
		}
		lead = append(lead, strings.TrimSpace(line))
		rest = strings.TrimLeft(tail, "\n")
	}
	lead = append(lead, "## "+title)
	if rest == "" {
		return strings.Join(lead, "\n\n")
	}
	return strings.Join(lead, "\n\n") + "\n\n" + rest
}

// Kept is the share of the first reading's words the second has, which is
// Missing seen from the other end.
//
// It reads better than one minus a rate at the place it is used, where the
// question is how much of the article the publisher's text actually is rather
// than how much of it is absent.
func (c Count) Kept() float64 {
	if c.Words == 0 {
		return 1
	}
	return float64(c.Words-(c.Changed-c.Near)) / float64(c.Words)
}
