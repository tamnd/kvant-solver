package publish

import (
	"html/template"
	"strings"
)

// page is what the layout is filled with: the frame every page shares and,
// in Data, what is particular to one kind of page.
type page struct {
	Lang        string
	Title       string
	Description string
	T           words
	// Root is the way back to the site root from this page.
	Root       string
	Canonical  string
	Alternates []alternate
	Icon       template.URL
	HomeHref   string
	SearchHref string
	Nav        []link
	Langs      []langLink
	FootLangs  []langLink
	Side       *side
	Crumbs     []link
	Prev, Next *link
	Foot       string
	Data       any
}

// link is one thing a reader can follow. Every list on the site is a list of
// these, with the fields a particular list does not need left empty.
type link struct {
	Href    string
	Text    string
	Note    string
	Kicker  string
	Sub     string
	ID      string
	Current bool
}

// side is the contents beside a page.
type side struct {
	Href, Kicker, Title string
	Items               []link
	More                *link
}

type alternate struct{ Lang, Href string }

type langLink struct {
	Lang, Name, Href string
	Current          bool
}

type articleData struct {
	Rubric  *link
	Authors []link
	Chips   []link
	Notice  *link
	Body    template.HTML
}

type sheetView struct {
	ID, Text, Label, How string
	Body                 template.HTML
}

type yearCard struct {
	Href, Text, Note string
	Titles           []string
}

type yearTiles struct {
	Year   int
	Href   string
	Issues []link
}

type decade struct {
	Title string
	Years []yearTiles
}

// frame is the part of a page that is the same on every page of a language:
// the bar along the top, the languages, the canonical address.
func (s *Site) frame(lang, addr, title string) page {
	t := wordsFor(lang)
	p := page{
		Lang:       lang,
		Title:      title,
		T:          t,
		Root:       rootOf(addr),
		Canonical:  s.CanonicalURL + addr,
		Icon:       template.URL(icon), //nolint:gosec // a constant
		HomeHref:   href(addr, langURL(lang)),
		SearchHref: href(addr, searchURL(lang)),
	}
	if addr == "" {
		p.HomeHref = "./"
	}
	section := ""
	if rest, ok := strings.CutPrefix(addr, lang+"/"); ok {
		section, _, _ = strings.Cut(rest, "/")
	}
	for _, n := range []struct{ to, text, section string }{
		{langURL(lang), t.Archive, ""},
		{rubricsURL(lang), t.Rubrics, "rubrics"},
		{authorsURL(lang), t.Authors, "authors"},
		{problemsURL(lang), t.Problems, "problems"},
		{tagsURL(), t.Tags, "tags"},
	} {
		current := section == n.section && (n.section != "" || addr == langURL(lang))
		if n.section == "tags" {
			current = strings.HasPrefix(addr, tagsURL())
		}
		p.Nav = append(p.Nav, link{Href: href(addr, n.to), Text: n.text, Current: current})
	}

	// The same page in the other languages, where there is one, and that
	// language's home where there is not.
	for _, other := range s.Langs {
		target := swapLang(addr, lang, other)
		has := s.known[target]
		ll := langLink{Lang: other, Name: wordsFor(other).Name, Current: other == lang && addr != ""}
		if has {
			ll.Href = href(addr, target)
			if len(s.Langs) > 1 && target != addr || other == lang {
				p.Alternates = append(p.Alternates, alternate{Lang: other, Href: s.CanonicalURL + target})
			}
		} else {
			ll.Href = href(addr, langURL(other))
		}
		p.Langs = append(p.Langs, ll)
		p.FootLangs = append(p.FootLangs, langLink{Lang: other, Name: ll.Name, Href: href(addr, langURL(other))})
	}
	if len(p.Alternates) < 2 {
		p.Alternates = nil // a page in one language has no alternates to name
	}
	if len(s.Langs) < 2 {
		p.Langs, p.FootLangs = nil, nil
	}
	return p
}

// swapLang is an address in one language as the same address in another.
// Addresses outside every language, like the tags, are the same in all of them.
func swapLang(addr, from, to string) string {
	if rest, ok := strings.CutPrefix(addr, from+"/"); ok {
		return to + "/" + rest
	}
	return addr
}

// crumbs is the way up from a page: the language home, then each pair of
// address and title given.
func (s *Site) crumbs(lang, from string, pairs ...string) []link {
	out := []link{{Href: href(from, langURL(lang)), Text: wordsFor(lang).Brand}}
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, link{Href: href(from, pairs[i]), Text: pairs[i+1]})
	}
	return out
}
