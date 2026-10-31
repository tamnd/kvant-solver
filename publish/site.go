package publish

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/katex"
)

// DefaultSiteURL is where the site lives, and where every page says its
// canonical copy is, whichever host it was served from.
const DefaultSiteURL = "https://kvant.tamnd.com/"

// Site builds the static site out of a corpus checkout.
//
// One build is every language at once. The languages link to each other page
// by page, and a reader who switches from an English article to the Russian it
// was translated from should land on that article and not on a home page, which
// only a build that can see both trees can arrange.
type Site struct {
	// Corpus is the checkout to read. Nothing here writes to it.
	Corpus *corpus.Corpus
	// Langs are the trees to publish. Empty means every tree the corpus has,
	// Russian first.
	Langs []string
	// Out is the directory to write. It is created if it is not there.
	Out string
	// SiteURL is where this build will be served from, with a trailing slash.
	// The pages link relatively and do not need it; the sitemap and the page
	// for a missing address do.
	SiteURL string
	// CanonicalURL is the copy search engines are told is the real one. It is
	// SiteURL unless a mirror is being built.
	CanonicalURL string
	// NoSheets leaves out the pages that show every sheet of an issue as it
	// was read. They are half the bytes of the site, and GitHub Pages stops at
	// a gigabyte.
	NoSheets bool
	// Workers is how many issues render at once. Zero means one per CPU.
	Workers int
	// MaxFiles and MaxBytes fail the build when the output is bigger than the
	// host takes. A deploy that the host truncates or refuses is found out
	// here, before anything is uploaded, rather than half way through. Zero is
	// no limit.
	MaxFiles int
	MaxBytes int64
	// Logf reports progress and may be nil.
	Logf func(string, ...any)
	// Strict fails the build if any formula could not be typeset. It is off by
	// default because there is always a tail of reading defects and the site is
	// worth publishing without waiting for the last of them, and it is here
	// because a run that wants to gate on the number should not have to parse
	// the output to find it.
	Strict bool

	badMath []Refused
	wrote   atomic.Int64
	bytes   atomic.Int64
	logMu   sync.Mutex
	mapMu   sync.Mutex
	pages   []string // every page address written, for the sitemap

	shelves map[string]*shelf
	// known is every page address the build will write, so that a page can
	// link to the same page in another language only when there is one.
	known map[string]bool
	// docs is everything published, so that the views which cut across issues
	// can be built once the walk is done.
	docs map[string][]doc
}

// Refused is one formula the build could not typeset, and the file it is in.
//
// The site build is the only thing in the project that parses every formula in
// the corpus, so it is the only thing that knows this. Rule 7 refuses a page
// whose math spans do not parse at reading time; these are the ones that got
// past it and are still not TeX, which is a different list and a shorter one.
type Refused struct {
	// Lang is the tree the file is in.
	Lang string
	// Where is the corpus file under content/<lang>, as a reader of the report
	// would look for it.
	Where string
	BadMath
}

// Stats is what a build did, which is what the command prints and what a test
// asserts on.
type Stats struct {
	Langs    int
	Issues   int
	Pages    int
	Articles int
	Problems int
	Files    int
	Bytes    int64
	// BadMath is how many formulas KaTeX refused. They are marked on the page
	// they are on, so this is the size of a reading defect and not the size of a
	// hole in the site.
	BadMath int
}

// shelf is what one language has on disk, read before anything is rendered so
// that every page can know what the others will be.
type shelf struct {
	lang     string
	issues   []corpus.IssueKey
	articles map[corpus.IssueKey][]string
	sheets   map[corpus.IssueKey][]int
	problems []corpus.ProblemID
	years    []int
	byYear   map[int][]corpus.IssueKey
}

// Build writes the whole site.
//
// It stops at the first refusal rather than carrying on and reporting at the
// end. A build that keeps going after the guard has spoken leaves a directory
// that is half publishable, and the next person to look at it has to work out
// which half.
func (s *Site) Build() (Stats, error) {
	var stats Stats
	if err := s.prepare(); err != nil {
		return stats, err
	}
	stats.Langs = len(s.Langs)
	for _, lang := range s.Langs {
		sh := s.shelves[lang]
		stats.Issues += len(sh.issues)
		for _, names := range sh.articles {
			stats.Articles += len(names)
		}
		for _, sheets := range sh.sheets {
			stats.Pages += len(sheets)
		}
		stats.Problems += len(sh.problems)
	}

	if err := s.render(); err != nil {
		return stats, err
	}
	for _, lang := range s.Langs {
		if err := s.years(lang); err != nil {
			return stats, err
		}
		if err := s.home(lang); err != nil {
			return stats, err
		}
		if err := s.views(lang); err != nil {
			return stats, err
		}
		if err := s.searchPage(lang); err != nil {
			return stats, err
		}
	}
	if err := s.tagViews(); err != nil {
		return stats, err
	}
	if err := s.landing(); err != nil {
		return stats, err
	}
	if err := s.notFound(); err != nil {
		return stats, err
	}
	if err := s.assets(); err != nil {
		return stats, err
	}
	if err := s.sitemap(); err != nil {
		return stats, err
	}
	stats.Files = int(s.wrote.Load())
	stats.Bytes = s.bytes.Load()
	stats.BadMath = len(s.badMath)

	// The links are checked last, over the finished directory, because that is
	// the only moment the answer is knowable: half a site has dead links in it
	// by construction.
	broken, err := CheckLinks(s.Out)
	if err != nil {
		return stats, err
	}
	if len(broken) > 0 {
		for _, b := range broken {
			s.logf("broken link: %s", b)
		}
		return stats, fmt.Errorf("%s on the built site %s nowhere, starting with %w",
			count(len(broken), "reference", "references"), verb(len(broken), "goes", "go"), broken[0])
	}

	if s.MaxFiles > 0 && stats.Files > s.MaxFiles {
		return stats, fmt.Errorf("the site is %d files and the host takes %d", stats.Files, s.MaxFiles)
	}
	if s.MaxBytes > 0 && stats.Bytes > s.MaxBytes {
		return stats, fmt.Errorf("the site is %s and the host takes %s", size(stats.Bytes), size(s.MaxBytes))
	}
	if s.Strict && len(s.badMath) > 0 {
		return stats, fmt.Errorf("%d formulas could not be typeset", len(s.badMath))
	}
	return stats, nil
}

// prepare reads what every language has on disk.
func (s *Site) prepare() error {
	if s.SiteURL == "" {
		s.SiteURL = DefaultSiteURL
	}
	if !strings.HasSuffix(s.SiteURL, "/") {
		s.SiteURL += "/"
	}
	if s.CanonicalURL == "" {
		s.CanonicalURL = s.SiteURL
	}
	if !strings.HasSuffix(s.CanonicalURL, "/") {
		s.CanonicalURL += "/"
	}
	if len(s.Langs) == 0 {
		langs, err := Langs(s.Corpus)
		if err != nil {
			return err
		}
		s.Langs = langs
	}
	s.shelves = map[string]*shelf{}
	s.known = map[string]bool{"": true, tagsURL(): true}
	s.docs = map[string][]doc{}
	for _, lang := range s.Langs {
		sh, err := s.shelf(lang)
		if err != nil {
			return err
		}
		if len(sh.issues) == 0 {
			return fmt.Errorf("%s holds no %s issues to publish", s.Corpus.Root, lang)
		}
		s.shelves[lang] = sh
		for _, addr := range []string{langURL(lang), searchURL(lang), rubricsURL(lang), authorsURL(lang), problemsURL(lang)} {
			s.known[addr] = true
		}
		for _, year := range sh.years {
			s.known[yearURL(lang, year)] = true
		}
		for _, key := range sh.issues {
			s.known[issueURL(lang, key)] = true
			if !s.NoSheets && len(sh.sheets[key]) > 0 {
				s.known[sheetsURL(lang, key)] = true
			}
			for _, name := range sh.articles[key] {
				s.known[articleURL(lang, key, name)] = true
			}
		}
		for _, id := range sh.problems {
			s.known[problemURL(lang, id)] = true
		}
	}
	return nil
}

// Langs is every language tree the corpus has, the language the magazine was
// printed in first and the translations after it in order.
func Langs(c *corpus.Corpus) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(c.Root, "content"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == "solutions" || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i] == corpus.DefaultLang) != (out[j] == corpus.DefaultLang) {
			return out[i] == corpus.DefaultLang
		}
		return out[i] < out[j]
	})
	return out, nil
}

func (s *Site) shelf(lang string) (*shelf, error) {
	sh := &shelf{
		lang:     lang,
		articles: map[corpus.IssueKey][]string{},
		sheets:   map[corpus.IssueKey][]int{},
		byYear:   map[int][]corpus.IssueKey{},
	}
	issues, err := s.issues(lang)
	if err != nil {
		return nil, err
	}
	for _, key := range issues {
		names, err := s.Corpus.Articles(lang, key)
		if err != nil {
			return nil, err
		}
		sheets, err := s.Corpus.Pages(lang, key)
		if err != nil {
			return nil, err
		}
		if len(names) == 0 && len(sheets) == 0 {
			continue // a directory with nothing read into it yet
		}
		sh.issues = append(sh.issues, key)
		sh.articles[key] = names
		sh.sheets[key] = sheets
		if len(sh.byYear[key.Year]) == 0 {
			sh.years = append(sh.years, key.Year)
		}
		sh.byYear[key.Year] = append(sh.byYear[key.Year], key)
	}
	sh.problems, err = s.Corpus.Problems(lang)
	if err != nil {
		return nil, err
	}
	return sh, nil
}

// noteBadMath records the formulas one file lost.
//
// Every one is named in the log rather than counted silently, because the count
// on its own tells whoever reads it that something is wrong and nothing about
// where, and these are fixable: a page with bad TeX on it can be read again.
func (s *Site) noteBadMath(out *[]Refused, lang, where string, bad []BadMath) {
	for _, b := range bad {
		s.logf("%s/%s: %s", lang, where, b)
		*out = append(*out, Refused{Lang: lang, Where: where, BadMath: b})
	}
}

// Refused lists the formulas the last build could not typeset.
//
// They are worth writing down rather than counting, because every one of them
// is a page that can be read again: the file, the line and the TeX the model
// produced are what somebody fixing it needs.
func (s *Site) Refused() []Refused { return s.badMath }

func (s *Site) logf(format string, args ...any) {
	if s.Logf != nil {
		s.logMu.Lock()
		s.Logf(format, args...)
		s.logMu.Unlock()
	}
}

// issues lists what the corpus actually holds, in order.
//
// The manifest is the list of issues that exist in the world and this is not
// that list. A site is built out of what has been read, so an issue with no
// directory is not a gap to be reported here, it is simply not published yet.
func (s *Site) issues(lang string) ([]corpus.IssueKey, error) {
	root := filepath.Join(s.Corpus.Root, "content", lang)
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

// job is one unit of rendering: an issue, or a run of problems.
type job struct {
	lang    string
	key     corpus.IssueKey
	problem []corpus.ProblemID
}

// result is what a job leaves for the views.
type result struct {
	docs    []doc
	badMath []Refused
	err     error
}

// render renders every issue and every problem of every language, on as many
// workers as there are CPUs.
//
// KaTeX runs in a JavaScript engine that only one goroutine may use at a time,
// so each worker owns a Renderer of its own. The results are put back in job
// order before anything reads them, which keeps two builds of one corpus byte
// for byte the same however the work happened to be scheduled.
func (s *Site) render() error {
	var jobs []job
	for _, lang := range s.Langs {
		sh := s.shelves[lang]
		for _, key := range sh.issues {
			jobs = append(jobs, job{lang: lang, key: key})
		}
		const batch = 50
		for i := 0; i < len(sh.problems); i += batch {
			jobs = append(jobs, job{lang: lang, problem: sh.problems[i:min(i+batch, len(sh.problems))]})
		}
	}

	workers := s.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	workers = max(1, min(workers, len(jobs)))
	results := make([]result, len(jobs))
	next := make(chan int)
	var failed atomic.Bool
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := NewRenderer()
			if err != nil {
				errs <- err
				failed.Store(true)
				for range next {
				}
				return
			}
			for i := range next {
				if failed.Load() {
					continue
				}
				j := jobs[i]
				if j.problem != nil {
					results[i] = s.problems(r, j.lang, j.problem)
				} else {
					results[i] = s.issue(r, j.lang, j.key)
				}
				if results[i].err != nil {
					failed.Store(true)
				}
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	for _, r := range results {
		if r.err != nil {
			return r.err
		}
	}
	for _, r := range results {
		s.badMath = append(s.badMath, r.badMath...)
		for _, d := range r.docs {
			s.docs[d.Lang] = append(s.docs[d.Lang], d)
		}
	}
	return nil
}

// articleView is one article as the contents of its issue lists it.
type articleView struct {
	name  string
	addr  string
	front corpus.ArticleFront
	body  string
}

// issue writes one issue: its contents, its articles and its sheets.
func (s *Site) issue(r *Renderer, lang string, key corpus.IssueKey) result {
	var res result
	sh := s.shelves[lang]
	t := wordsFor(lang)
	dir := s.Corpus.IssueDir(lang, key)
	where := key.Dir()

	var views []articleView
	for _, name := range sh.articles[key] {
		var front corpus.ArticleFront
		body, err := corpus.LoadUnchecked(filepath.Join(dir, "articles", name), &front)
		if err != nil {
			res.err = err
			return res
		}
		if lang != corpus.DefaultLang {
			s.fillFromSource(key, name, &front)
		}
		views = append(views, articleView{name: name, addr: articleURL(lang, key, name), front: front, body: body})
	}

	issueAddr := issueURL(lang, key)
	title := t.IssueTitle(key.Year, key.Number)
	sheets := sh.sheets[key]
	hasSheets := !s.NoSheets && len(sheets) > 0

	// The contents of the issue go beside every article in it, so a reader can
	// move through the issue the way they would turn its pages.
	contents := func(from, current string) *side {
		sd := &side{Href: href(from, issueAddr), Kicker: t.YearTitle(key.Year), Title: "№ " + key.Number}
		for _, v := range views {
			sd.Items = append(sd.Items, link{Href: href(from, v.addr), Text: v.front.Title, Current: v.addr == current})
		}
		if hasSheets {
			sd.More = &link{Href: href(from, sheetsURL(lang, key)), Text: t.AllPages + " →"}
		}
		return sd
	}

	for i, v := range views {
		html, bad, err := r.Render(v.body)
		if err != nil {
			res.err = fmt.Errorf("%s/%s/articles/%s: %w", lang, where, v.name, err)
			return res
		}
		s.noteBadMath(&res.badMath, lang, path.Join(where, "articles", v.name), bad)

		from := v.addr
		f := v.front
		data := articleData{Body: template.HTML(html)} //nolint:gosec // escaped by the renderer, checked by Guard
		if f.Rubric != "" {
			data.Rubric = &link{Href: href(from, rubricURL(lang, f.Rubric)), Text: rubricTitle(lang, f.Rubric)}
		}
		for _, who := range f.Authors {
			data.Authors = append(data.Authors, link{Href: href(from, authorURL(lang, who)), Text: who})
		}
		data.Chips = append(data.Chips, link{Href: href(from, issueAddr), Text: title})
		if pages := pageRange(f); pages != "" {
			chip := link{Text: t.PageLabel(pages)}
			if hasSheets && f.PageFirst > 0 {
				chip.Href = href(from, sheetURL(lang, key, f.PageFirst))
			}
			data.Chips = append(data.Chips, chip)
		}
		if f.Tag != "" {
			data.Chips = append(data.Chips, link{Href: href(from, tagURL(string(f.Tag))), Text: t.Tag + " " + string(f.Tag), Kicker: "tag"})
		}
		if f.TranslatedFrom != "" {
			note := t.Translation
			if f.TranslationModel != "" {
				note += " (" + f.TranslationModel + ")"
			}
			data.Notice = &link{Text: note + ". " + t.NotChecked}
			if f.TranslationQuality == corpus.QualityDraft {
				data.Notice = &link{Text: t.Draft, Kicker: "warn"}
			}
			if orig := articleURL(corpus.DefaultLang, key, v.name); s.known[orig] {
				data.Notice.Href = href(from, orig)
			}
		}

		p := s.frame(lang, from, f.Title)
		p.Description = strings.Join(append(append([]string{}, f.Authors...), title), " · ")
		p.Side = contents(from, from)
		p.Crumbs = s.crumbs(lang, from, yearURL(lang, key.Year), t.YearTitle(key.Year), issueAddr, title)
		if i > 0 {
			p.Prev = &link{Href: href(from, views[i-1].addr), Text: views[i-1].front.Title, Note: t.Prev}
		}
		if i+1 < len(views) {
			p.Next = &link{Href: href(from, views[i+1].addr), Text: views[i+1].front.Title, Note: t.Next}
		}
		p.Data = data
		if err := s.emit(from, articleTmpl, p); err != nil {
			res.err = err
			return res
		}
		res.docs = append(res.docs, doc{
			Lang: lang, Kind: "article", Addr: from, Title: f.Title, Key: key,
			Authors: f.Authors, Rubric: f.Rubric, Tag: string(f.Tag), Pages: pageRange(f),
		})
	}

	// The sheets.
	if hasSheets {
		addr := sheetsURL(lang, key)
		var list []sheetView
		sd := &side{Href: href(addr, issueAddr), Kicker: t.YearTitle(key.Year), Title: "№ " + key.Number}
		for _, index := range sheets {
			id := corpus.PageID{Issue: key, Index: index}
			var front corpus.PageFront
			body, err := corpus.LoadUnchecked(s.Corpus.PagePath(lang, id), &front)
			if err != nil {
				res.err = err
				return res
			}
			html, bad, err := r.Render(body)
			if err != nil {
				res.err = fmt.Errorf("%s/%s/pages/%s: %w", lang, where, id.Filename(), err)
				return res
			}
			s.noteBadMath(&res.badMath, lang, path.Join(where, "pages", id.Filename()), bad)
			label := t.Unnumbered
			if front.PageLabel != "" {
				label = t.PageLabel(front.PageLabel)
			}
			list = append(list, sheetView{
				ID:    sheetID(index),
				Text:  t.SheetTitle(index),
				Label: label,
				How:   provenance(t, front.Provenance, front.Translated),
				Body:  template.HTML(html), //nolint:gosec // escaped by the renderer, checked by Guard
			})
			sd.Items = append(sd.Items, link{Href: "#" + sheetID(index), Text: t.SheetTitle(index), Note: front.PageLabel})
		}
		p := s.frame(lang, addr, title+" · "+t.Pages)
		p.Side = sd
		p.Crumbs = s.crumbs(lang, addr, yearURL(lang, key.Year), t.YearTitle(key.Year), issueAddr, title)
		p.Data = struct {
			Issue, IssueHref string
			Sheets           []sheetView
		}{title, href(addr, issueAddr), list}
		if err := s.emit(addr, sheetsTmpl, p); err != nil {
			res.err = err
			return res
		}
	}

	// The issue itself.
	from := issueAddr
	var rows []link
	for _, v := range views {
		row := link{Href: href(from, v.addr), Text: v.front.Title, Note: pageRange(v.front), Sub: strings.Join(v.front.Authors, ", ")}
		if v.front.Rubric != "" {
			row.Kicker = rubricTitle(lang, v.front.Rubric)
		}
		rows = append(rows, row)
	}
	chips := []link{{Text: t.Count(len(views), "article")}, {Text: t.Count(len(sheets), "page")}}
	if hasSheets {
		chips = append(chips, link{Href: href(from, sheetsURL(lang, key)), Text: t.AllPages + " →"})
	}
	p := s.frame(lang, from, title)
	p.Crumbs = s.crumbs(lang, from, yearURL(lang, key.Year), t.YearTitle(key.Year))
	p.Side = s.yearSide(lang, from, key.Year, key)
	prev, next := neighbours(sh.issues, key)
	if prev != nil {
		p.Prev = &link{Href: href(from, issueURL(lang, *prev)), Text: t.IssueTitle(prev.Year, prev.Number), Note: t.PrevIssue}
	}
	if next != nil {
		p.Next = &link{Href: href(from, issueURL(lang, *next)), Text: t.IssueTitle(next.Year, next.Number), Note: t.NextIssue}
	}
	p.Data = struct {
		Year     int
		YearHref string
		Chips    []link
		Articles []link
	}{key.Year, href(from, yearURL(lang, key.Year)), chips, rows}
	if err := s.emit(from, issueTmpl, p); err != nil {
		res.err = err
		return res
	}
	s.logf("%s %s: %d articles, %d pages", lang, key, len(views), len(sheets))
	return res
}

// fillFromSource fills in what a translation's front matter left out from the
// Russian it was translated from. The translator carries the title and the
// body across and not always the names, which are not words to translate.
func (s *Site) fillFromSource(key corpus.IssueKey, name string, front *corpus.ArticleFront) {
	if len(front.Authors) > 0 && front.Rubric != "" && front.Tag != "" && front.PageFirst > 0 {
		return
	}
	var src corpus.ArticleFront
	if _, err := corpus.LoadUnchecked(filepath.Join(s.Corpus.IssueDir(corpus.DefaultLang, key), "articles", name), &src); err != nil {
		return
	}
	if len(front.Authors) == 0 {
		front.Authors = src.Authors
	}
	if front.Rubric == "" {
		front.Rubric = src.Rubric
	}
	if front.Tag == "" {
		front.Tag = src.Tag
	}
	if front.PageFirst == 0 {
		front.PageFirst, front.PageLast, front.PageLabels = src.PageFirst, src.PageLast, src.PageLabels
	}
}

// provenance is how a sheet came to be text, which is part of what it is. A
// sheet off a born digital PDF and a sheet a model read off a scan are not the
// same kind of evidence, and a reader comparing the two should be able to see
// which is which without going to the repository.
func provenance(t words, p corpus.Provenance, tr corpus.Translated) string {
	var parts []string
	switch p.Extraction {
	case corpus.ExtractionNative, corpus.ExtractionPublisher:
		parts = append(parts, t.NativeText)
	case corpus.ExtractionVision:
		how := t.Transcribed
		if p.ExtractionModel != "" {
			how += " (" + p.ExtractionModel + ")"
		}
		parts = append(parts, how)
	}
	if tr.TranslationQuality == corpus.QualityDraft {
		parts = append(parts, t.DraftShort)
	} else if tr.TranslatedFrom != "" {
		how := t.Translation
		if tr.TranslationModel != "" {
			how += " (" + tr.TranslationModel + ")"
		}
		parts = append(parts, how)
	}
	return strings.Join(parts, " · ")
}

// neighbours is the issue before and after one, across years.
func neighbours(keys []corpus.IssueKey, key corpus.IssueKey) (prev, next *corpus.IssueKey) {
	for i, k := range keys {
		if k != key {
			continue
		}
		if i > 0 {
			prev = &keys[i-1]
		}
		if i+1 < len(keys) {
			next = &keys[i+1]
		}
	}
	return prev, next
}

// yearSide is the issues of a year, beside an issue or a year.
func (s *Site) yearSide(lang, from string, year int, current corpus.IssueKey) *side {
	t := wordsFor(lang)
	sh := s.shelves[lang]
	sd := &side{Href: href(from, yearURL(lang, year)), Kicker: t.Year, Title: strconv.Itoa(year)}
	for _, key := range sh.byYear[year] {
		sd.Items = append(sd.Items, link{
			Href:    href(from, issueURL(lang, key)),
			Text:    "№ " + key.Number,
			Note:    t.Count(len(sh.articles[key]), "article"),
			Current: key == current,
		})
	}
	return sd
}

// years writes a page for every year a language has.
func (s *Site) years(lang string) error {
	t := wordsFor(lang)
	sh := s.shelves[lang]
	titles := map[corpus.IssueKey][]string{}
	for _, d := range s.docs[lang] {
		if d.Kind == "article" && len(titles[d.Key]) < 5 {
			titles[d.Key] = append(titles[d.Key], d.Title)
		}
	}
	for i, year := range sh.years {
		addr := yearURL(lang, year)
		var cards []yearCard
		articles := 0
		for _, key := range sh.byYear[year] {
			articles += len(sh.articles[key])
			cards = append(cards, yearCard{
				Href:   href(addr, issueURL(lang, key)),
				Text:   key.Number,
				Note:   t.Count(len(sh.articles[key]), "article") + " · " + t.Count(len(sh.sheets[key]), "page"),
				Titles: titles[key],
			})
		}
		p := s.frame(lang, addr, t.YearTitle(year))
		p.Crumbs = s.crumbs(lang, addr)
		if i > 0 {
			p.Prev = &link{Href: href(addr, yearURL(lang, sh.years[i-1])), Text: strconv.Itoa(sh.years[i-1]), Note: t.PrevYear}
		}
		if i+1 < len(sh.years) {
			p.Next = &link{Href: href(addr, yearURL(lang, sh.years[i+1])), Text: strconv.Itoa(sh.years[i+1]), Note: t.NextYear}
		}
		p.Data = struct {
			Lede   string
			Issues []yearCard
		}{t.Count(len(cards), "issue") + " · " + t.Count(articles, "article"), cards}
		if err := s.emit(addr, yearTmpl, p); err != nil {
			return err
		}
	}
	return nil
}

// home writes the archive in one language.
func (s *Site) home(lang string) error {
	t := wordsFor(lang)
	sh := s.shelves[lang]
	addr := langURL(lang)
	articles, sheets := 0, 0
	byDecade := map[int][]yearTiles{}
	var decades []int
	for _, year := range sh.years {
		yt := yearTiles{Year: year, Href: href(addr, yearURL(lang, year))}
		for _, key := range sh.byYear[year] {
			articles += len(sh.articles[key])
			sheets += len(sh.sheets[key])
			yt.Issues = append(yt.Issues, link{
				Href: href(addr, issueURL(lang, key)),
				Text: key.Number,
				Note: t.IssueTitle(key.Year, key.Number) + " · " + t.Count(len(sh.articles[key]), "article"),
			})
		}
		d := year / 10 * 10
		if _, ok := byDecade[d]; !ok {
			decades = append(decades, d)
		}
		byDecade[d] = append(byDecade[d], yt)
	}
	var ds []decade
	for _, d := range decades {
		ds = append(ds, decade{Title: strconv.Itoa(d) + "s", Years: byDecade[d]})
	}
	if lang == "ru" {
		for i := range ds {
			ds[i].Title = strings.TrimSuffix(ds[i].Title, "s") + "-е"
		}
	}
	stats := []link{
		{Text: strconv.Itoa(len(sh.issues)), Note: t.Count(len(sh.issues), "issue")},
		{Text: strconv.Itoa(articles), Note: t.Count(articles, "article")},
		{Text: strconv.Itoa(sheets), Note: t.Count(sheets, "page")},
	}
	for i := range stats {
		stats[i].Note = strings.TrimSpace(strings.TrimPrefix(stats[i].Note, stats[i].Text))
	}
	if len(sh.problems) > 0 {
		stats = append(stats, link{Text: strconv.Itoa(len(sh.problems)),
			Note: strings.TrimSpace(strings.TrimPrefix(t.Count(len(sh.problems), "problem"), strconv.Itoa(len(sh.problems))))})
	}
	quick := []link{
		{Href: href(addr, rubricsURL(lang)), Text: t.Rubrics, Note: t.RubricsLede},
		{Href: href(addr, authorsURL(lang)), Text: t.Authors, Note: t.AuthorsLede},
		{Href: href(addr, problemsURL(lang)), Text: t.Problems, Note: t.Count(len(sh.problems), "problem")},
		{Href: href(addr, tagsURL()), Text: t.Tags, Note: t.TagLookup},
	}
	if len(sh.problems) == 0 {
		quick[2].Note = t.ProblemsLede // "0 problems" reads as broken, not as not yet translated
	}
	p := s.frame(lang, addr, t.Brand+" · "+t.Tagline)
	p.Description = t.About
	p.Data = struct {
		Stats, Quick []link
		Decades      []decade
	}{stats, quick, ds}
	return s.emit(addr, homeTmpl, p)
}

// landing writes the root, which belongs to no language.
func (s *Site) landing() error {
	type card struct {
		Lang, Name, Brand, Tagline, About, Href, Counts string
	}
	var cards []card
	for _, lang := range s.Langs {
		t := wordsFor(lang)
		sh := s.shelves[lang]
		articles := 0
		for _, names := range sh.articles {
			articles += len(names)
		}
		cards = append(cards, card{
			Lang: lang, Name: t.Name, Brand: t.Brand, Tagline: t.Tagline, About: t.About,
			Href:   href("", langURL(lang)),
			Counts: t.Count(len(sh.issues), "issue") + " · " + t.Count(articles, "article"),
		})
	}
	p := s.frame(s.Langs[0], "", "Квант · Kvant")
	p.Lang = "mul"
	p.Nav = nil
	p.Description = wordsFor("en").About
	p.Data = struct{ Langs []card }{cards}
	return s.emit("", landingTmpl, p)
}

// notFound writes the page both hosts serve for an address that holds nothing.
//
// It is the one page whose address is not known when it is written, so it is
// the one page whose links are absolute, from wherever the site is served.
func (s *Site) notFound() error {
	base := "/"
	if i := strings.Index(strings.TrimPrefix(strings.TrimPrefix(s.SiteURL, "https://"), "http://"), "/"); i >= 0 {
		base = strings.TrimPrefix(strings.TrimPrefix(s.SiteURL, "https://"), "http://")[i:]
	}
	t := wordsFor("en")
	var items []link
	for _, lang := range s.Langs {
		w := wordsFor(lang)
		items = append(items, link{Href: base + langURL(lang), Text: w.Brand + " · " + w.Name, Sub: w.NotFoundLede})
	}
	p := page{
		Lang: "en", Title: t.NotFound, T: t, Root: base, Icon: template.URL(icon), //nolint:gosec // a constant
		HomeHref: base, SearchHref: base + searchURL("en"),
		Data: struct{ Items []link }{items},
	}
	return s.write("404.html", s.exec("404.html", notFoundTmpl, p))
}

// assets copies the stylesheet, the script and the vendored fonts.
//
// KaTeX's own stylesheet and fonts are the whole of what the browser needs to
// display what was typeset here, and they are copied out of the same embedded
// copy the checksum test covers, so the site cannot end up carrying a KaTeX
// nobody checked.
func (s *Site) assets() error {
	for name, body := range map[string]string{
		"assets/site.css": siteCSS,
		"assets/site.js":  siteJS,
		"assets/theme.js": themeJS,
		"robots.txt":      "User-agent: *\nAllow: /\nSitemap: " + s.SiteURL + "sitemap.xml\n",
		// Cloudflare Pages reads this and GitHub Pages serves it as a file
		// nobody links to. The assets are not fingerprinted, so they are
		// cached for a day rather than for ever.
		"_headers": "/*\n  X-Content-Type-Options: nosniff\n  Referrer-Policy: strict-origin-when-cross-origin\n" +
			"/assets/*\n  Cache-Control: public, max-age=86400, stale-while-revalidate=604800\n" +
			"/*/search.json\n  Cache-Control: public, max-age=3600\n",
	} {
		if err := s.write(name, []byte(body)); err != nil {
			return err
		}
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

// sitemap lists every page, from where this build is served.
func (s *Site) sitemap() error {
	addrs := append([]string(nil), s.pages...)
	sort.Strings(addrs)
	const per = 40000 // the protocol allows fifty thousand
	var maps []string
	for i := 0; i < len(addrs); i += per {
		var b bytes.Buffer
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
		for _, a := range addrs[i:min(i+per, len(addrs))] {
			b.WriteString("<url><loc>")
			template.HTMLEscape(&b, []byte(s.SiteURL+a))
			b.WriteString("</loc></url>\n")
		}
		b.WriteString("</urlset>\n")
		name := fmt.Sprintf("sitemap-%d.xml", len(maps)+1)
		maps = append(maps, name)
		if err := s.write(name, b.Bytes()); err != nil {
			return err
		}
	}
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" + `<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, m := range maps {
		b.WriteString("<sitemap><loc>" + s.SiteURL + m + "</loc></sitemap>\n")
	}
	b.WriteString("</sitemapindex>\n")
	return s.write("sitemap.xml", b.Bytes())
}

// emit fills a template for a page address and writes the result.
func (s *Site) emit(addr string, tmpl *template.Template, p page) error {
	data := s.exec(addr, tmpl, p)
	if data == nil {
		return fmt.Errorf("%s: could not fill %s", addr, tmpl.Name())
	}
	s.mapMu.Lock()
	s.pages = append(s.pages, addr)
	s.mapMu.Unlock()
	return s.write(file(addr), data)
}

func (s *Site) exec(name string, tmpl *template.Template, p page) []byte {
	var out bytes.Buffer
	if err := tmpl.Execute(&out, p); err != nil {
		s.logf("%s: %v", name, err)
		return nil
	}
	return out.Bytes()
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
	s.wrote.Add(1)
	s.bytes.Add(int64(len(data)))
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
	if front.PageFirst == 0 {
		return ""
	}
	if front.PageFirst == front.PageLast || front.PageLast == 0 {
		return strconv.Itoa(front.PageFirst)
	}
	return fmt.Sprintf("%d–%d", front.PageFirst, front.PageLast)
}

func size(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KiB", n>>10)
}

// plural counts something in Russian, which needs three forms rather than two:
// one номер, two номера, five номеров, and then eleven to fourteen go back to
// номеров whatever their last digit is.
func plural(n int, one, few, many string) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d %s", n, one)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d %s", n, few)
	default:
		return fmt.Sprintf("%d %s", n, many)
	}
}
