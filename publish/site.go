package publish

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/rubric"
)

// Site builds the static site out of a corpus checkout.
type Site struct {
	// Corpus is the checkout to read. Nothing here writes to it.
	Corpus *corpus.Corpus
	// Lang is the tree to publish. One language is one site.
	Lang string
	// Out is the directory to write. It is created if it is not there.
	Out string
	// Logf reports progress and may be nil.
	Logf func(string, ...any)
	// Strict fails the build if any formula could not be typeset. It is off by
	// default because there is always a tail of reading defects and the site is
	// worth publishing without waiting for the last of them, and it is here
	// because a run that wants to gate on the number should not have to parse
	// the output to find it.
	Strict bool

	render  *Renderer
	badMath int
	wrote   int
}

// Stats is what a build did, which is what the command prints and what a test
// asserts on.
type Stats struct {
	Issues   int
	Pages    int
	Articles int
	Files    int
	// BadMath is how many formulas KaTeX refused. They are marked on the page
	// they are on, so this is the size of a reading defect and not the size of a
	// hole in the site.
	BadMath int
}

// Build writes the whole site.
//
// It stops at the first refusal rather than carrying on and reporting at the
// end. A build that keeps going after the guard has spoken leaves a directory
// that is half publishable, and the next person to look at it has to work out
// which half.
func (s *Site) Build() (Stats, error) {
	var stats Stats
	if s.Lang == "" {
		s.Lang = corpus.DefaultLang
	}
	render, err := NewRenderer()
	if err != nil {
		return stats, err
	}
	s.render = render

	issues, err := s.issues()
	if err != nil {
		return stats, err
	}
	if len(issues) == 0 {
		return stats, fmt.Errorf("%s holds no %s issues to publish", s.Corpus.Root, s.Lang)
	}

	years := map[int][]corpus.IssueKey{}
	for _, key := range issues {
		one, err := s.issue(key)
		if err != nil {
			return stats, err
		}
		stats.Issues++
		stats.Pages += one.Pages
		stats.Articles += one.Articles
		years[key.Year] = append(years[key.Year], key)
	}

	if err := s.years(years); err != nil {
		return stats, err
	}
	if err := s.assets(); err != nil {
		return stats, err
	}
	stats.Files = s.wrote
	stats.BadMath = s.badMath
	if s.Strict && s.badMath > 0 {
		return stats, fmt.Errorf("%d formulas could not be typeset", s.badMath)
	}
	return stats, nil
}

// noteBadMath records the formulas one file lost.
//
// Every one is named in the log rather than counted silently, because the count
// on its own tells whoever reads it that something is wrong and nothing about
// where, and these are fixable: a page with bad TeX on it can be read again.
func (s *Site) noteBadMath(where string, bad []BadMath) {
	for _, b := range bad {
		s.logf("%s: %s", where, b)
	}
	s.badMath += len(bad)
}

func (s *Site) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// issues lists what the corpus actually holds, in order.
//
// The manifest is the list of issues that exist in the world and this is not
// that list. A site is built out of what has been read, so an issue with no
// directory is not a gap to be reported here, it is simply not published yet.
func (s *Site) issues() ([]corpus.IssueKey, error) {
	root := filepath.Join(s.Corpus.Root, "content", s.Lang)
	yearDirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var keys []corpus.IssueKey
	for _, yearDir := range yearDirs {
		year, err := strconv.Atoi(yearDir.Name())
		if err != nil || !yearDir.IsDir() {
			continue // problems/ lives here too, and it is not a year
		}
		numbers, err := os.ReadDir(filepath.Join(root, yearDir.Name()))
		if err != nil {
			return nil, err
		}
		for _, numberDir := range numbers {
			if !numberDir.IsDir() {
				continue
			}
			// The directory is zero padded and the key is not, and 1988 printed
			// a double issue called 11-12, so the number is trimmed rather than
			// parsed.
			key, err := corpus.NewIssueKey(year, strings.TrimLeft(numberDir.Name(), "0"))
			if err != nil {
				continue
			}
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Year != keys[j].Year {
			return keys[i].Year < keys[j].Year
		}
		return manifestOrder(keys[i].Number) < manifestOrder(keys[j].Number)
	})
	return keys, nil
}

// manifestOrder sorts 2 before 10, and puts the double issue where its first
// half belongs.
func manifestOrder(number string) int {
	first, _, _ := strings.Cut(number, "-")
	n, err := strconv.Atoi(first)
	if err != nil {
		return 0
	}
	return n
}

// issueBuild is the counts one issue contributed.
type issueBuild struct {
	Pages    int
	Articles int
}

// indexEntry is one thing a reader can reach, kept so that the year and archive
// pages can be written once everything under them is known.
type indexEntry struct {
	Href  string
	Title string
	Note  string
}

func (s *Site) issue(key corpus.IssueKey) (issueBuild, error) {
	var built issueBuild
	dir := path.Join(strconv.Itoa(key.Year), pad(key.Number))

	articles, err := s.Corpus.Articles(s.Lang, key)
	if err != nil {
		return built, err
	}
	var articleList []indexEntry
	for _, name := range articles {
		entry, err := s.article(key, dir, name)
		if err != nil {
			return built, err
		}
		articleList = append(articleList, entry)
		built.Articles++
	}

	indexes, err := s.Corpus.Pages(s.Lang, key)
	if err != nil {
		return built, err
	}
	var pageList []indexEntry
	for _, index := range indexes {
		entry, err := s.page(key, dir, index)
		if err != nil {
			return built, err
		}
		pageList = append(pageList, entry)
		built.Pages++
	}

	s.logf("%s: %d articles, %d pages", key, built.Articles, built.Pages)
	return built, s.emit(path.Join(dir, "index.html"), issueTmpl, map[string]any{
		"Title":    fmt.Sprintf("Квант %d, №%s", key.Year, key.Number),
		"Up":       "../",
		"Root":     "../../",
		"Articles": articleList,
		"Pages":    pageList,
	})
}

func (s *Site) article(key corpus.IssueKey, dir, name string) (indexEntry, error) {
	var front corpus.ArticleFront
	body, err := corpus.LoadUnchecked(
		filepath.Join(s.Corpus.IssueDir(s.Lang, key), "articles", name), &front)
	if err != nil {
		return indexEntry{}, err
	}
	html, bad, err := s.render.Render(body)
	if err != nil {
		return indexEntry{}, fmt.Errorf("%s: %w", name, err)
	}
	s.noteBadMath(path.Join(dir, "articles", name), bad)

	slug := strings.TrimSuffix(name, ".md")
	href := path.Join("articles", slug+".html")
	// The file holds the slug the assembler gave it and not the banner the
	// magazine printed, so the table is read the way round that matches.
	printed := rubric.BySlug(front.Rubric).Title
	return indexEntry{Href: href, Title: front.Title, Note: printed},
		s.emit(path.Join(dir, href), articleTmpl, map[string]any{
			"Title":   front.Title,
			"Authors": strings.Join(front.Authors, ", "),
			"Rubric":  printed,
			"Issue":   fmt.Sprintf("Квант %d, №%s", front.Year, front.Number),
			"Pages":   pageRange(front),
			"Tag":     string(front.Tag),
			"Body":    template.HTML(html), //nolint:gosec // escaped by the renderer, checked by Guard
			"Up":      "../",
			"Root":    "../../../",
		})
}

func (s *Site) page(key corpus.IssueKey, dir string, index int) (indexEntry, error) {
	id := corpus.PageID{Issue: key, Index: index}
	var front corpus.PageFront
	body, err := corpus.LoadUnchecked(s.Corpus.PagePath(s.Lang, id), &front)
	if err != nil {
		return indexEntry{}, err
	}
	html, bad, err := s.render.Render(body)
	if err != nil {
		return indexEntry{}, fmt.Errorf("%s: %w", id, err)
	}
	s.noteBadMath(id.String(), bad)

	href := path.Join("pages", fmt.Sprintf("%04d.html", index))
	label := front.PageLabel
	if label == "" {
		label = "не пронумерована"
	}
	return indexEntry{Href: href, Title: fmt.Sprintf("Лист %d", index), Note: label},
		s.emit(path.Join(dir, href), pageTmpl, map[string]any{
			"Title": fmt.Sprintf("Квант %d, №%s, лист %d", key.Year, key.Number, index),
			"Label": front.PageLabel,
			"Sheet": index,
			// How a page was read is part of what it is. A page off a born
			// digital PDF and a page a model read off a scan are not the same
			// kind of evidence, and a reader comparing the two should be able to
			// see which is which without going to the repository.
			"Extraction": front.Extraction,
			"Model":      front.ExtractionModel,
			"Body":       template.HTML(html), //nolint:gosec // escaped by the renderer, checked by Guard
			"Up":         "../",
			"Root":       "../../../",
		})
}

func (s *Site) years(years map[int][]corpus.IssueKey) error {
	var all []indexEntry
	for year, keys := range years {
		var list []indexEntry
		for _, key := range keys {
			list = append(list, indexEntry{
				Href:  path.Join(pad(key.Number), "index.html"),
				Title: "№" + key.Number,
			})
		}
		if err := s.emit(path.Join(strconv.Itoa(year), "index.html"), listTmpl, map[string]any{
			"Title": fmt.Sprintf("Квант %d", year),
			"Items": list,
			"Up":    "../",
			"Root":  "../",
		}); err != nil {
			return err
		}
		all = append(all, indexEntry{
			Href:  path.Join(strconv.Itoa(year), "index.html"),
			Title: strconv.Itoa(year),
			Note:  plural(len(keys)),
		})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Title < all[j].Title })
	return s.emit("index.html", listTmpl, map[string]any{
		"Title": "Квант",
		"Items": all,
		"Root":  "",
	})
}

// assets copies the stylesheet and the vendored fonts.
//
// KaTeX's own stylesheet and fonts are the whole of what the browser needs to
// display what was typeset here, and they are copied out of the same embedded
// copy the checksum test covers, so the site cannot end up carrying a KaTeX
// nobody checked.
func (s *Site) assets() error {
	if err := s.write("assets/site.css", []byte(siteCSS)); err != nil {
		return err
	}
	return fs.WalkDir(katex.Assets(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(katex.Assets(), name)
		if err != nil {
			return err
		}
		return s.write(path.Join("assets", name), data)
	})
}

// emit fills a template and writes the result.
func (s *Site) emit(name string, tmpl *template.Template, data map[string]any) error {
	var out strings.Builder
	if err := tmpl.Execute(&out, data); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return s.write(name, []byte(out.String()))
}

// write is the only way anything reaches the output, so that the guard cannot
// be walked around by adding a view.
func (s *Site) write(name string, data []byte) error {
	if err := Guard(name, data); err != nil {
		return err
	}
	full := filepath.Join(s.Out, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return err
	}
	s.wrote++
	return nil
}

func pad(number string) string {
	if n, err := strconv.Atoi(number); err == nil {
		return fmt.Sprintf("%02d", n)
	}
	return number
}

func pageRange(front corpus.ArticleFront) string {
	if front.PageLabels != "" {
		return front.PageLabels
	}
	if front.PageFirst == front.PageLast {
		return strconv.Itoa(front.PageFirst)
	}
	return fmt.Sprintf("%d-%d", front.PageFirst, front.PageLast)
}

func plural(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d номер", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d номера", n)
	default:
		return fmt.Sprintf("%d номеров", n)
	}
}
