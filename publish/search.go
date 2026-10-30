package publish

import (
	"encoding/json"
	"strings"
)

// The search index is built here and shipped as a file, one per language, and
// searched in the browser.
//
// Search on a static site is either somebody else's server, which means
// handing the corpus to a third party and putting a network call in front of a
// reader who only wanted to read a 1975 article about the Coriolis force, or it
// is an index the browser downloads and searches itself. The second is the
// right shape and the index is the hard half of it: knowing every document,
// its title, who wrote it and which section it ran in. That half is done here,
// once, at build time.
//
// The index holds no body text. The bodies were the bulk of it, twenty
// megabytes for one language, which is a download nobody should pay to look
// up an article by its title or its author, and what a reader searching an
// archive of a magazine types is overwhelmingly one of those two.

// Record is one document in the index.
//
// The field names are one letter because there are thousands of records and
// the names are repeated in every one.
type Record struct {
	// T is the title and H the page, from the site root.
	T string `json:"t"`
	H string `json:"h"`
	// A is the authors, R the section as this language names it.
	A string `json:"a,omitempty"`
	R string `json:"r,omitempty"`
	// I is the issue, as a reader would write it: 1975 · 4.
	I string `json:"i,omitempty"`
	// G is the tag.
	G string `json:"g,omitempty"`
	// K is the kind: a for an article, p for a problem.
	K string `json:"k"`
}

// searchPage writes the search page of one language and its index.
//
// The index carries no build date. Two builds of one corpus have to come out
// byte for byte the same, because that is what makes a rebuild reviewable as a
// diff, and a timestamp is the usual way that gets lost.
func (s *Site) searchPage(lang string) error {
	docs := s.docs[lang]
	records := make([]Record, 0, len(docs))
	for _, d := range docs {
		r := Record{T: d.Title, H: d.Addr, A: strings.Join(d.Authors, ", "), I: d.where(), G: d.Tag, K: d.Kind[:1]}
		if d.Rubric != "" {
			r.R = rubricTitle(lang, d.Rubric)
		}
		records = append(records, r)
	}
	data, err := json.Marshal(struct {
		Count   int      `json:"count"`
		Records []Record `json:"records"`
	}{Count: len(records), Records: records})
	if err != nil {
		return err
	}
	if err := s.write(searchIndexURL(lang), data); err != nil {
		return err
	}

	t := wordsFor(lang)
	addr := searchURL(lang)
	p := s.frame(lang, addr, t.Search)
	p.Crumbs = s.crumbs(lang, addr)
	p.Data = struct{ Index string }{href(addr, searchIndexURL(lang))}
	return s.emit(addr, searchTmpl, p)
}
