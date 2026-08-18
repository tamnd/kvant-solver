package publisher

import (
	"github.com/tamnd/kvant-solver/manifest"
)

// WithText is every contents row the archive says it carries the text of.
//
// Candidates asks nearly the same question and asks it of an issue this corpus
// has already assembled, because a comparison needs a reading on each side.
// This asks it of the contents alone and takes the row whether or not a page
// near it has ever been read.
//
// The difference is what the two are for. A comparison is a measurement of a
// reading that has happened. This is the text somebody already typed, and the
// point of having it is the reading it saves, so waiting for that reading
// before fetching it has the order backwards.
func WithText(toc *manifest.TOC, issues []manifest.Issue) []Candidate {
	var out []Candidate
	for _, iss := range issues {
		rows, ok := toc.Get(iss.Key)
		if !ok {
			continue
		}
		for _, row := range rows {
			// A row with no slug is one the contents printed and the site does
			// not have a page for, a section heading or a continuation line.
			// has_text on one of those is the parser having found the text of
			// whatever page it landed on.
			if !row.HasText || row.Slug == "" || row.URL == "" {
				continue
			}
			out = append(out, Candidate{
				Issue: iss.Key,
				Year:  iss.Year,
				Slug:  row.Slug,
				Title: row.Title,
				URL:   row.URL,
			})
		}
	}
	return sorted(out)
}
