package publish

import (
	"fmt"
	"html/template"
	"path"
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
	// Kind is article, sheet or problem.
	Kind string
	// Href is where the page went, from the site root.
	Href string
	// Title is what to call it in a list.
	Title string
	// Year and Number are the issue it belongs to, for the note in a list. A
	// problem belongs to the issue that posed it.
	Year   int
	Number string
	// Issue is the same thing as the corpus writes it, kept for the search
	// index so that a consumer can join back onto the corpus.
	Issue   string
	Authors []string
	// Rubric is the slug, not the banner.
	Rubric string
	Tag    string
	Pages  string
	// Text is the body with the markup taken off, for the search index.
	Text string
}

// where is the issue a doc came out of, for the second column of a list.
func (d doc) where() string {
	if d.Year == 0 {
		return ""
	}
	return fmt.Sprintf("%d, №%s", d.Year, d.Number)
}

// views writes everything that cuts across issues.
func (s *Site) views() error {
	if err := s.rubricViews(); err != nil {
		return err
	}
	if err := s.authorViews(); err != nil {
		return err
	}
	return s.tagViews()
}

// rubricViews writes the section index and a page for each section.
//
// Only the sections that have something in them get a page. The table in
// package rubric lists every standing section the magazine ever ran, and
// publishing an empty page for the ones this corpus has not reached yet would
// put twenty seven entries in front of a reader when nine of them are real.
func (s *Site) rubricViews() error {
	inSection := map[string][]indexEntry{}
	for _, d := range s.docs {
		if d.Kind != "article" || d.Rubric == "" {
			continue
		}
		inSection[d.Rubric] = append(inSection[d.Rubric], indexEntry{
			Href:  "../" + d.Href,
			Title: d.Title,
			Note:  d.where(),
		})
	}

	var index []indexEntry
	for slug, list := range inSection {
		name := rubric.BySlug(slug).Title
		if err := s.emit(path.Join("rubrics", slug+".html"), listTmpl, map[string]any{
			"Title": name,
			"Lede":  plural(len(list), "материал", "материала", "материалов") + " под этой рубрикой.",
			"Items": list,
			"Root":  "../",
		}); err != nil {
			return err
		}
		index = append(index, indexEntry{
			Href:  slug + ".html",
			Title: name,
			Note:  plural(len(list), "материал", "материала", "материалов"),
		})
	}
	sort.Slice(index, func(i, j int) bool {
		return alphabetical(index[i].Title) < alphabetical(index[j].Title)
	})

	return s.emit("rubrics/index.html", listTmpl, map[string]any{
		"Title": "Рубрики",
		"Lede":  "Постоянные разделы журнала. Рубрика опознаётся по слагу, а не по тому, как её набрали в конкретном номере.",
		"Items": index,
		"Root":  "../",
	})
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
func (s *Site) authorViews() error {
	type person struct {
		spellings map[string]int
		items     []indexEntry
	}
	people := map[string]*person{}
	for _, d := range s.docs {
		for _, name := range d.Authors {
			slug := rubric.Slug(name)
			if slug == "" {
				continue // a signature with no letters in it, which does happen
			}
			p := people[slug]
			if p == nil {
				p = &person{spellings: map[string]int{}}
				people[slug] = p
			}
			p.spellings[name]++
			p.items = append(p.items, indexEntry{
				Href:  "../" + d.Href,
				Title: d.Title,
				Note:  d.where(),
			})
		}
	}

	var index []indexEntry
	for slug, p := range people {
		name := commonest(p.spellings)
		lede := plural(len(p.items), "материал", "материала", "материалов") + "."
		if len(p.spellings) > 1 {
			lede += " Подписано также: " + strings.Join(others(p.spellings, name), ", ") + "."
		}
		if err := s.emit(path.Join("authors", slug+".html"), listTmpl, map[string]any{
			"Title": name,
			"Lede":  lede,
			"Items": p.items,
			"Root":  "../",
		}); err != nil {
			return err
		}
		index = append(index, indexEntry{
			Href:  slug + ".html",
			Title: name,
			Note:  plural(len(p.items), "материал", "материала", "материалов"),
		})
	}
	sort.Slice(index, func(i, j int) bool {
		return alphabetical(index[i].Title) < alphabetical(index[j].Title)
	})

	return s.emit("authors/index.html", listTmpl, map[string]any{
		"Title": "Авторы",
		"Lede":  "Имена так, как они напечатаны над статьями.",
		"Items": index,
		"Root":  "../",
	})
}

// tagViews writes a page for every tag and an index to reach them by.
//
// The index is split on the first character rather than being one list, because
// the corpus already carries five and a half thousand tags and one page of them
// is half a megabyte a reader would download to look at four lines of it.
func (s *Site) tagViews() error {
	shards := map[string][]indexEntry{}
	seen := map[string]string{}
	for _, d := range s.docs {
		if d.Tag == "" {
			continue
		}
		if had, ok := seen[d.Tag]; ok {
			return fmt.Errorf("tag %s is on both %s and %s, and a tag names one thing", d.Tag, had, d.Href)
		}
		seen[d.Tag] = d.Href
		if err := s.emit(path.Join("tags", d.Tag+".html"), tagTmpl, map[string]any{
			"Title": d.Title,
			"Tag":   d.Tag,
			"Href":  "../" + d.Href,
			"Note":  d.where(),
			"Root":  "../",
		}); err != nil {
			return err
		}
		first := d.Tag[:1]
		shards[first] = append(shards[first], indexEntry{
			Href:  d.Tag + ".html",
			Title: d.Tag,
			Note:  d.Title,
		})
	}

	var index []indexEntry
	for first, list := range shards {
		sort.Slice(list, func(i, j int) bool { return list[i].Title < list[j].Title })
		if err := s.emit(path.Join("tags", first+".html"), listTmpl, map[string]any{
			"Title": "Теги на " + first,
			"Items": list,
			"Root":  "../",
		}); err != nil {
			return err
		}
		index = append(index, indexEntry{
			Href:  first + ".html",
			Title: first,
			Note:  plural(len(list), "тег", "тега", "тегов"),
		})
	}
	sort.Slice(index, func(i, j int) bool { return index[i].Title < index[j].Title })

	return s.emit("tags/index.html", listTmpl, map[string]any{
		"Title": "Теги",
		"Lede":  "У каждого материала есть тег из четырёх знаков. Он закреплён навсегда и не меняется, когда материал переименован или перемещён.",
		"Items": index,
		"Root":  "../",
	})
}

// problems writes the Задачник, one page per problem plus an index.
//
// A problem is not filed under an issue in the corpus and it is not filed under
// one here either. It belongs to the issue that posed it and the issue that
// printed the solution, which are usually two to four apart, and its number is
// the name the olympiad literature already cites it by. So the number is the
// path, and the two issues are links off it.
func (s *Site) problems(published map[string]bool) (int, error) {
	ids, err := s.Corpus.Problems(s.Lang)
	if err != nil {
		return 0, err
	}

	var index []indexEntry
	for _, id := range ids {
		var front corpus.ProblemFront
		body, err := corpus.LoadUnchecked(s.Corpus.ProblemPath(s.Lang, id), &front)
		if err != nil {
			return 0, err
		}
		html, bad, err := s.render.Render(body)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", id, err)
		}
		source := path.Join("problems", strings.TrimPrefix(id.Path(), "problems/"))
		s.noteBadMath(source, bad)

		href := strings.TrimSuffix(id.Path(), ".md") + ".html"
		title := "Задача " + id.String()
		posed := issueLink(front.PosedIn, front.PosedPages, published, "../../")
		solved := issueLink(front.SolvedIn, front.SolvedPages, published, "../../")
		var tag map[string]string
		if front.Tag != "" {
			tag = map[string]string{
				"Href":  "../../tags/" + string(front.Tag) + ".html",
				"Title": string(front.Tag),
			}
		}
		if err := s.emit(href, problemTmpl, map[string]any{
			"Title":   title,
			"Authors": strings.Join(front.Authors, ", "),
			"Posed":   posed,
			"Solved":  solved,
			"Tag":     tag,
			"Body":    template.HTML(html), //nolint:gosec // escaped by the renderer, checked by Guard
			"Root":    "../../",
		}); err != nil {
			return 0, err
		}

		key, keyErr := corpus.ParseIssueKey(front.PosedIn)
		posedNote := front.PosedIn
		if posed != nil {
			posedNote = posed.Title
		}
		if front.SolvedIn == "" {
			posedNote += ", решение не напечатано"
		}
		index = append(index, indexEntry{
			Href:  strings.TrimPrefix(href, "problems/"),
			Title: id.String(),
			Note:  posedNote,
		})
		entry := doc{
			Kind:    "problem",
			Href:    href,
			Title:   title,
			Issue:   front.PosedIn,
			Authors: front.Authors,
			Tag:     string(front.Tag),
			Pages:   front.PosedPages,
			Text:    plain(body),
		}
		if keyErr == nil {
			entry.Year, entry.Number = key.Year, key.Number
		}
		s.docs = append(s.docs, entry)
	}

	return len(ids), s.emit("problems/index.html", listTmpl, map[string]any{
		"Title": "Задачник «Кванта»",
		"Lede":  "Задачи M и Ф под своими номерами. Условие и напечатанное решение выходили в разных номерах, поэтому задача стоит отдельно от них обоих.",
		"Items": index,
		"Root":  "../",
	})
}

// issueLink names an issue and links to it when the site has it.
//
// A problem posed in an issue nobody has read yet still gets a page, so the
// issue is named either way and only becomes a link when there is something on
// the other end. Writing the link regardless would leave the site pointing at a
// directory that is not there, which is exactly what CheckLinks is for.
func issueLink(key, pages string, published map[string]bool, root string) *indexEntry {
	if key == "" {
		return nil
	}
	parsed, err := corpus.ParseIssueKey(key)
	if err != nil {
		return &indexEntry{Title: key, Note: pages}
	}
	entry := indexEntry{Title: fmt.Sprintf("Квант %d, №%s", parsed.Year, parsed.Number), Note: pages}
	if published[key] {
		entry.Href = fmt.Sprintf("%s%d/%s/index.html", root, parsed.Year, pad(parsed.Number))
	}
	return &entry
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
