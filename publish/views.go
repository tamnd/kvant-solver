package publish

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"unicode"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/rubric"
)

// The views are the ways into the archive that are not the shelf order.
//
// An issue index is the obvious arrangement and it is the one a reader who
// already knows what they want does not need. What the corpus is actually good
// for is the other cuts: everything Смородинский wrote across thirty years,
// every piece the Задачник ran, the problem the olympiad literature cites by
// number and nothing else. None of those can be built one issue at a time,
// which is why they are here and not in Site.issue: they need the whole walk to
// have finished first.

// doc is one thing the site published, remembered so that the views can be
// written once every issue has been walked.
//
// It holds what the file said, in the form the file said it. The views join on
// the rubric slug and the tag and never on the printed text, for the same
// reason the corpus does: the printed text moves between issues and the slug
// does not.
type doc struct {
	Lang string
	// Kind is article or problem.
	Kind string
	// Addr is the page, as an address from the site root.
	Addr string
	// Title is what to call it in a list.
	Title string
	// Key is the issue it belongs to. A problem belongs to the issue that
	// posed it, and has none when that is not known.
	Key     corpus.IssueKey
	Authors []string
	// Rubric is the slug, not the banner.
	Rubric string
	Tag    string
	Pages  string
}

// where is the issue a doc came out of, for the second column of a list.
func (d doc) where() string {
	if d.Key.Zero() {
		return ""
	}
	return fmt.Sprintf("%d · %s", d.Key.Year, d.Key.Number)
}

// row is a doc as a line in a list on the page at from.
func (d doc) row(from string) link {
	r := link{Href: href(from, d.Addr), Text: d.Title, Note: d.where(), Sub: strings.Join(d.Authors, ", ")}
	if d.Rubric != "" {
		r.Kicker = rubricTitle(d.Lang, d.Rubric)
	}
	return r
}

// listData is what listTmpl is filled with.
type listData struct {
	Kicker, Lede string
	Letters      bool
	Groups       []group
	Items        []link
}

type group struct {
	ID, Title string
	Items     []link
}

// views writes everything in one language that cuts across issues.
//
// The pages of each view are added to what the build knows before the first of
// them is written, so that the language switcher on a section page can go to
// the same section in another language when it has one.
func (s *Site) views(lang string) error {
	for _, d := range s.docs[lang] {
		if d.Rubric != "" {
			s.known[rubricURL(lang, d.Rubric)] = true
		}
		for _, who := range d.Authors {
			if rubric.Slug(who) != "" {
				s.known[authorURL(lang, who)] = true
			}
		}
	}
	if err := s.rubricViews(lang); err != nil {
		return err
	}
	if err := s.authorViews(lang); err != nil {
		return err
	}
	return s.problemIndex(lang)
}

// rubricViews writes the section index and a page for each section.
//
// Only the sections that have something in them get a page. The table in
// package rubric lists every standing section the magazine ever ran, and
// publishing an empty page for the ones this corpus has not reached yet would
// put twenty seven entries in front of a reader when nine of them are real.
func (s *Site) rubricViews(lang string) error {
	t := wordsFor(lang)
	inSection := map[string][]doc{}
	for _, d := range s.docs[lang] {
		if d.Kind == "article" && d.Rubric != "" {
			inSection[d.Rubric] = append(inSection[d.Rubric], d)
		}
	}
	index := rubricsURL(lang)
	var rows []link
	for slug, list := range inSection {
		addr := rubricURL(lang, slug)
		name := rubricTitle(lang, slug)
		items := make([]link, 0, len(list))
		for _, d := range list {
			r := d.row(addr)
			r.Kicker = ""
			items = append(items, r)
		}
		p := s.frame(lang, addr, name)
		p.Crumbs = s.crumbs(lang, addr, index, t.Rubrics)
		p.Data = listData{Kicker: t.Rubrics, Lede: t.Count(len(list), "article"), Items: items}
		if err := s.emit(addr, listTmpl, p); err != nil {
			return err
		}
		rows = append(rows, link{Href: href(index, addr), Text: name, Note: t.Count(len(list), "article")})
	}
	sort.Slice(rows, func(i, j int) bool { return alphabetical(rows[i].Text) < alphabetical(rows[j].Text) })
	p := s.frame(lang, index, t.Rubrics)
	p.Crumbs = s.crumbs(lang, index)
	p.Data = listData{Lede: t.RubricsLede, Items: rows}
	return s.emit(index, listTmpl, p)
}

// authorViews writes the author index and a page for each name.
//
// The names are the ones printed over the articles, transliterated the same way
// article filenames are. That means two people who sign identically are one
// entry here, and one person who signed two ways is two entries when the
// spellings differ by more than case and punctuation. Resolving that properly
// is what the publisher's own personalia index is for and it is not this: an
// index built out of what the pages say is one that can be checked against the
// pages, which is worth more here than a fuzzy match against a list from
// somewhere else.
func (s *Site) authorViews(lang string) error {
	t := wordsFor(lang)
	type person struct {
		slug      string
		spellings map[string]int
		docs      []doc
	}
	people := map[string]*person{}
	for _, d := range s.docs[lang] {
		for _, name := range d.Authors {
			slug := rubric.Slug(name)
			if slug == "" {
				continue // a signature with no letters in it, which does happen
			}
			p := people[slug]
			if p == nil {
				p = &person{slug: slug, spellings: map[string]int{}}
				people[slug] = p
			}
			p.spellings[name]++
			p.docs = append(p.docs, d)
		}
	}

	index := authorsURL(lang)
	letters := map[string][]link{}
	for _, p := range people {
		addr := authorsURL(lang) + p.slug
		name := commonest(p.spellings)
		lede := t.Count(len(p.docs), "article") + "."
		if len(p.spellings) > 1 {
			lede += " " + t.SignedAlso + ": " + strings.Join(others(p.spellings, name), ", ") + "."
		}
		items := make([]link, 0, len(p.docs))
		for _, d := range p.docs {
			items = append(items, d.row(addr))
		}
		pg := s.frame(lang, addr, name)
		pg.Crumbs = s.crumbs(lang, addr, index, t.Authors)
		pg.Data = listData{Kicker: t.Authors, Lede: lede, Items: items}
		if err := s.emit(addr, listTmpl, pg); err != nil {
			return err
		}
		first := initial(surname(name))
		letters[first] = append(letters[first], link{Href: href(index, addr), Text: name, Note: t.Count(len(p.docs), "article")})
	}

	var groups []group
	for first, rows := range letters {
		sort.Slice(rows, func(i, j int) bool {
			a, b := alphabetical(surname(rows[i].Text)), alphabetical(surname(rows[j].Text))
			if a != b {
				return a < b
			}
			return rows[i].Text < rows[j].Text
		})
		groups = append(groups, group{ID: fmt.Sprintf("l-%x", first), Title: first, Items: rows})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Title < groups[j].Title })
	p := s.frame(lang, index, t.Authors)
	p.Crumbs = s.crumbs(lang, index)
	p.Data = listData{Lede: t.AuthorsLede + " " + t.Count(len(people), "author") + ".", Letters: len(groups) > 1, Groups: groups}
	return s.emit(index, listTmpl, p)
}

// surname is the part of a printed name an index files it under. The magazine
// prints initials first, «А. Н. Колмогоров», so that is the last word.
func surname(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return name
	}
	return fields[len(fields)-1]
}

// initial is the letter a name is filed under.
func initial(name string) string {
	for _, r := range alphabetical(name) {
		return string(unicode.ToUpper(r))
	}
	return "#"
}

// tagViews writes the thirty six pages the tags are filed on and the page to
// look one up from.
//
// A tag names one thing in every language, so the tags are not under any of
// them. A row goes to the Russian, which is the text the magazine printed and
// the one every translation was made from, and names the translations beside
// it.
func (s *Site) tagViews() error {
	lang := s.Langs[0]
	t := wordsFor(lang)
	type entry struct {
		tag, title, note, addr string
		alts                   []alternate
	}
	byTag := map[string]*entry{}
	for _, l := range s.Langs {
		for _, d := range s.docs[l] {
			if d.Tag == "" {
				continue
			}
			e := byTag[d.Tag]
			if e == nil {
				e = &entry{tag: d.Tag, title: d.Title, note: d.where(), addr: d.Addr}
				byTag[d.Tag] = e
				continue
			}
			if l == s.Langs[0] {
				return fmt.Errorf("tag %s is on both %s and %s, and a tag names one thing", d.Tag, e.addr, d.Addr)
			}
			e.alts = append(e.alts, alternate{Lang: l, Href: d.Addr})
		}
	}
	shards := map[string][]*entry{}
	for _, e := range byTag {
		first := strings.ToUpper(e.tag[:1])
		shards[first] = append(shards[first], e)
	}

	index := tagsURL()
	var rows []link
	for first, list := range shards {
		addr := tagShard(first)
		sort.Slice(list, func(i, j int) bool { return list[i].tag < list[j].tag })
		type tagRow struct {
			ID, Go, Text, Note string
			Alts               []alternate
		}
		var out []tagRow
		for _, e := range list {
			r := tagRow{ID: e.tag, Go: href(addr, e.addr), Text: e.title, Note: e.note}
			for _, a := range e.alts {
				r.Alts = append(r.Alts, alternate{Lang: a.Lang, Href: href(addr, a.Href)})
			}
			out = append(out, r)
		}
		p := s.frame(lang, addr, t.TagsOn(first))
		p.Crumbs = []link{{Href: href(addr, langURL(lang)), Text: t.Brand}, {Href: href(addr, index), Text: t.Tags}}
		p.Data = struct {
			IndexHref string
			Tags      []tagRow
		}{href(addr, index), out}
		if err := s.emit(addr, tagsTmpl, p); err != nil {
			return err
		}
		rows = append(rows, link{Href: href(index, addr), Text: first, Note: t.Count(len(list), "tag")})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Text < rows[j].Text })
	p := s.frame(lang, index, t.Tags)
	p.Crumbs = s.crumbs(lang, index)
	p.Data = struct {
		Base  string
		Items []link
	}{href(index, tagsURL()), rows}
	return s.emit(index, tagIndexTmpl, p)
}

// problems writes a run of the Задачник, one page per problem.
//
// A problem is not filed under an issue in the corpus and it is not filed under
// one here either. It belongs to the issue that posed it and the issue that
// printed the solution, which are usually two to four apart, and its number is
// the name the olympiad literature already cites it by. So the number is the
// path, and the two issues are links off it.
func (s *Site) problems(r *Renderer, lang string, ids []corpus.ProblemID) result {
	var res result
	t := wordsFor(lang)
	for _, id := range ids {
		var front corpus.ProblemFront
		body, err := corpus.LoadUnchecked(s.Corpus.ProblemPath(lang, id), &front)
		if err != nil {
			res.err = err
			return res
		}
		html, bad, err := r.Render(body)
		if err != nil {
			res.err = fmt.Errorf("%s/%s: %w", lang, id.Path(), err)
			return res
		}
		s.noteBadMath(&res.badMath, lang, id.Path(), bad)

		addr := problemURL(lang, id)
		var chips []link
		if posed := s.issueChip(lang, addr, t.Posed, front.PosedIn, front.PosedPages, front.PosedArticle); posed != nil {
			chips = append(chips, *posed)
		}
		if solved := s.issueChip(lang, addr, t.Solved, front.SolvedIn, front.SolvedPages, front.SolvedArticle); solved != nil {
			chips = append(chips, *solved)
		} else {
			chips = append(chips, link{Text: t.NoSolution})
		}
		if front.Tag != "" {
			chips = append(chips, link{Href: href(addr, tagURL(string(front.Tag))), Text: t.Tag + " " + string(front.Tag), Kicker: "tag"})
		}
		title := t.ProblemTitle(id.String())
		p := s.frame(lang, addr, title)
		p.Crumbs = s.crumbs(lang, addr, problemsURL(lang), t.Problems)
		p.Data = struct {
			IndexHref, Authors string
			Chips              []link
			Body               template.HTML
		}{href(addr, problemsURL(lang)), strings.Join(front.Authors, ", "), chips,
			template.HTML(html)} //nolint:gosec // escaped by the renderer, checked by Guard
		if err := s.emit(addr, problemTmpl, p); err != nil {
			res.err = err
			return res
		}
		d := doc{Lang: lang, Kind: "problem", Addr: addr, Title: title, Authors: front.Authors, Tag: string(front.Tag), Pages: front.PosedPages}
		if key, err := corpus.ParseIssueKey(front.PosedIn); err == nil {
			d.Key = key
		}
		res.docs = append(res.docs, d)
	}
	return res
}

// issueChip names an issue and links to it when the site has it.
//
// A problem posed in an issue nobody has read yet still gets a page, so the
// issue is named either way and only becomes a link when there is something on
// the other end. Writing the link regardless would leave the site pointing at a
// directory that is not there, which is exactly what CheckLinks is for.
//
// The article the half was cut from is the better target when the site has it,
// since that is where the words are; the issue is the fallback.
func (s *Site) issueChip(lang, from, what, key, pages, article string) *link {
	if key == "" {
		return nil
	}
	t := wordsFor(lang)
	parsed, err := corpus.ParseIssueKey(key)
	if err != nil {
		return &link{Text: what + ": " + key}
	}
	text := what + ": " + t.IssueTitle(parsed.Year, parsed.Number)
	if pages != "" {
		text += ", " + t.PageLabel(pages)
	}
	chip := &link{Text: text}
	if article != "" {
		for _, l := range []string{lang, corpus.DefaultLang} {
			if addr := articleURL(l, parsed, article); s.known[addr] {
				chip.Href = href(from, addr)
				return chip
			}
		}
	}
	for _, l := range []string{lang, corpus.DefaultLang} {
		if addr := issueURL(l, parsed); s.known[addr] {
			chip.Href = href(from, addr)
			break
		}
	}
	return chip
}

// problemIndex writes the list of the problems in one language.
func (s *Site) problemIndex(lang string) error {
	t := wordsFor(lang)
	index := problemsURL(lang)
	bySubject := map[string][]link{}
	for _, d := range s.docs[lang] {
		if d.Kind != "problem" {
			continue
		}
		id := strings.TrimPrefix(d.Addr, index)
		note := d.where()
		bySubject[id[:1]] = append(bySubject[id[:1]], link{Href: href(index, d.Addr), Text: id, Note: note, Sub: strings.Join(d.Authors, ", ")})
	}
	var groups []group
	for _, subject := range []string{"M", "F"} {
		if rows := bySubject[subject]; len(rows) > 0 {
			groups = append(groups, group{ID: strings.ToLower(subject), Title: subject, Items: rows})
		}
	}
	p := s.frame(lang, index, t.Problems)
	p.Crumbs = s.crumbs(lang, index)
	p.Data = listData{Lede: t.ProblemsLede, Letters: len(groups) > 1, Groups: groups}
	return s.emit(index, listTmpl, p)
}

// alphabetical is a title with the punctuation in front of it taken off.
//
// The magazine puts guillemets round its own name, so «Квант» улыбается starts
// with a character that sorts before every letter in the alphabet and would
// stand at the top of the section index instead of under К where a reader would
// look for it.
func alphabetical(title string) string {
	return strings.TrimLeftFunc(title, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// commonest is the spelling of a name that appears most often, and the earlier
// one alphabetically when two appear as often, so that a build is repeatable.
func commonest(counts map[string]int) string {
	best, most := "", 0
	for name, n := range counts {
		if n > most || (n == most && name < best) {
			best, most = name, n
		}
	}
	return best
}

// others is every spelling but the one being shown, in order.
func others(counts map[string]int, shown string) []string {
	var out []string
	for name := range counts {
		if name != shown {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
