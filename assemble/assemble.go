// Package assemble turns a read issue into articles.
//
// The pages are the ground truth and the articles are a view of them. That is
// the wrong way round from how the magazine was written and the right way round
// for a corpus made from scans: a page is a thing that exists and can be checked
// against the paper, and an article is a claim about which pages belong
// together. Keeping the pages means a wrong claim is repairable without reading
// anything again.
//
// The claim comes from two places that disagree. The printed contents of the
// issue gives a title, an author and a starting page, and it is the only source
// for the order. The pages themselves give the rubric banners and the article
// headings, which is the only evidence of where one piece actually stops. Where
// they conflict the page wins on boundaries and the contents wins on titles,
// because the contents was typed by the publisher from the same paper and the
// scan is the paper.
package assemble

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/rubric"
)

// Page is one transcribed page, as this package needs it.
type Page struct {
	Index int
	// Label is the printed number, empty when the page prints none.
	Label string
	Body  string
}

// Article is one assembled piece, ready to be written.
type Article struct {
	Slug string
	// RowSlug is the site's own slug for the contents row, kept because Slug is
	// it with the separators normalised and normalising does not go backwards.
	// The publisher's typed text is filed under the site's spelling, so finding
	// it later needs the spelling the site used.
	RowSlug   string
	Title     string
	Authors   []string
	Rubric    string
	RubricSub string
	// First and Last are scan indexes, not printed numbers, because that is
	// what a page file is named after.
	First int
	Last  int
	// Labels is the printed range, "12-15", for a reader and for a citation.
	Labels string
	// Pages is every scan index the article covers, in order.
	Pages []int
	Body  string
	// Ordinal is the position in the printed contents, one based. It goes in
	// the filename so that a directory listing is the contents in order.
	Ordinal int
	// Sources names where the row came from, carried through from the TOC so an
	// article assembled from a row only one mirror had can be told apart.
	Source string
}

// Note is something the assembly could not decide by itself. These are not
// errors: an issue with three of them is still assembled, and the three go in
// the report and, where they are a real disagreement between sources, into
// errata.yaml.
type Note struct {
	Kind    string
	Subject string
	Detail  string
}

// Result is one issue assembled.
type Result struct {
	Articles []Article
	Notes    []Note
	// Orphans are scan indexes no article claims. Some are expected, the covers
	// and the adverts, and the page files still hold them; a run of six in the
	// middle of an issue is a row the contents lost.
	Orphans []int
}

// Issue assembles one issue from its pages and its reconciled contents.
//
// The rows are taken in the order the contents prints them, which is the order
// the magazine ran, and never sorted by page: the contents of this magazine
// lists the answers column last and prints it in the middle, and sorting would
// silently reorder the issue.
func Issue(key corpus.IssueKey, rows []manifest.Row, pages []Page) Result {
	var result Result
	if len(pages) == 0 {
		result.Notes = append(result.Notes, Note{
			Kind:   "no_pages",
			Detail: fmt.Sprintf("%s has contents rows but no page files", key),
		})
		return result
	}

	sort.Slice(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })
	byIndex := make(map[int]Page, len(pages))
	for _, page := range pages {
		byIndex[page.Index] = page
	}
	offset, ok := labelOffset(pages)
	if !ok {
		result.Notes = append(result.Notes, Note{
			Kind:   "no_folios",
			Detail: fmt.Sprintf("%s prints no page numbers anywhere, so the contents cannot be placed", key),
		})
		return result
	}

	// Every row's start, in scan indexes, so that a row's end is the next row's
	// start minus one. Rows the contents gives no page for are dropped to a
	// note: an article with no start cannot be placed and guessing one would
	// move the article after it too.
	type placed struct {
		row   manifest.Row
		start int
	}
	var starts []placed
	for _, row := range rows {
		start, ok := startOf(row, offset, byIndex)
		if !ok {
			result.Notes = append(result.Notes, Note{
				Kind:    "unplaced",
				Subject: row.Title,
				Detail:  "the contents gives no page for this row, so it was not assembled",
			})
			continue
		}
		starts = append(starts, placed{row: row, start: start})
	}
	if len(starts) == 0 {
		return result
	}

	last := pages[len(pages)-1].Index
	claimed := map[int]bool{}
	for i, item := range starts {
		end := last
		// The end is the page before the next thing in the issue starts, and
		// next means next in the scan rather than next in the contents. The two
		// differ often enough to matter: this magazine lists its answers column
		// last and prints it in the middle, so the row that stops it is one
		// listed above it. Taking the nearest start over every row, in either
		// direction in the contents, is what keeps that column from swallowing
		// the rest of the issue.
		for j, other := range starts {
			if j == i {
				continue
			}
			if other.start > item.start && other.start-1 < end {
				end = other.start - 1
			}
		}
		article := build(key, item.row, item.start, end, byIndex, i+1)
		for _, index := range article.Pages {
			claimed[index] = true
		}
		result.Articles = append(result.Articles, article)
	}

	for _, page := range pages {
		if !claimed[page.Index] {
			result.Orphans = append(result.Orphans, page.Index)
		}
	}
	result.Notes = append(result.Notes, boundaryNotes(result.Articles, byIndex)...)
	return result
}

// build cuts one article out of the pages.
func build(key corpus.IssueKey, row manifest.Row, start, end int, byIndex map[int]Page, ordinal int) Article {
	article := Article{
		Title:   strings.TrimSpace(row.Title),
		Authors: authors(row.Authors),
		First:   start,
		Last:    end,
		Ordinal: ordinal,
		Source:  row.Source,
	}
	if row.Rubric != "" {
		canonical := rubric.Canonical(row.Rubric)
		article.Rubric = canonical.Slug
	}
	article.RubricSub = strings.TrimSpace(row.RubricSub)
	article.Slug = slugOf(row, article.Title, key, ordinal)
	article.RowSlug = strings.TrimSpace(row.Slug)

	// Where the contents lists the exact pages, use them: an article split by a
	// full page advert is not contiguous and a range would claim the advert.
	if len(row.Pages) > 0 {
		article.Pages = withinScan(row.Pages, start, end, byIndex)
	}
	if len(article.Pages) == 0 {
		for index := start; index <= end; index++ {
			if _, ok := byIndex[index]; ok {
				article.Pages = append(article.Pages, index)
			}
		}
	}
	if len(article.Pages) > 0 {
		article.First = article.Pages[0]
		article.Last = article.Pages[len(article.Pages)-1]
	}

	var parts []string
	var labels []string
	for _, index := range article.Pages {
		page := byIndex[index]
		body := strings.TrimSpace(page.Body)
		if body != "" {
			parts = append(parts, body)
		}
		if page.Label != "" {
			labels = append(labels, page.Label)
		}
	}
	article.Body = strings.Join(parts, "\n\n")
	article.Labels = labelRange(labels)
	return article
}

// boundaryNotes reports where the pages disagree with the contents about where
// an article starts.
//
// A rubric banner in the middle of an article is the tell. The magazine prints
// a banner where a section begins, so a banner on a page the contents says is
// page three of something is either a row the contents lost or a start the
// contents put in the wrong place. Either way it is worth a person's minute,
// and it is exactly the kind of thing that is invisible once the pages have
// been merged and thrown away.
func boundaryNotes(articles []Article, byIndex map[int]Page) []Note {
	var notes []Note
	for _, article := range articles {
		for _, index := range article.Pages {
			if index == article.First {
				continue
			}
			banners := ocr.Rubrics(byIndex[index].Body)
			if len(banners) == 0 {
				continue
			}
			notes = append(notes, Note{
				Kind:    "banner_inside",
				Subject: article.Title,
				Detail: fmt.Sprintf("page %d prints the banner %q, which is a section starting where the contents says this article continues",
					index, banners[0]),
			})
		}
	}
	return notes
}

// startOf places a contents row in the scan.
//
// The printed number is asked first, and it is asked of the pages: the page
// that says it prints 18 is where the row that says page 18 begins, and that
// answer is checked against the paper rather than derived from anything. The
// row's own sheet comes second, and the offset last, because an offset measured
// over one issue is wrong on either side of an unnumbered insert.
//
// The order used to be the other way round, on the reasoning that a sheet is
// already in scan coordinates and needs no arithmetic. It is not in these scan
// coordinates. The mirror numbers the images of a scan from zero and a page
// file is numbered from one, so trusting the sheet put every row one page early
// and cost each article its last page: on 1975 №1 that orphaned eleven pages
// and left three articles fighting over one sheet.
func startOf(row manifest.Row, offset int, byIndex map[int]Page) (int, bool) {
	if row.Page > 0 {
		if index, ok := byLabel(row.Page, byIndex); ok {
			return index, true
		}
	}
	if row.Sheet > 0 {
		if index := row.Sheet + 1; hasIndex(byIndex, index) {
			return index, true
		}
	}
	if row.Page <= 0 {
		return 0, false
	}
	index := row.Page + offset
	if _, ok := byIndex[index]; !ok {
		return 0, false
	}
	return index, true
}

// hasIndex says whether the scan has this page. It is a named helper because
// the map holds values and not pointers, so the comma ok form is the only way
// to tell a missing page from a page that is present and empty.
func hasIndex(byIndex map[int]Page, index int) bool {
	_, ok := byIndex[index]
	return ok
}

// byLabel finds the page that actually prints a number. This is the exact
// answer where it exists, and it exists for every numbered page of a normal
// issue.
func byLabel(printed int, byIndex map[int]Page) (int, bool) {
	want := strconv.Itoa(printed)
	found := 0
	for index, page := range byIndex {
		if page.Label != want {
			continue
		}
		if found != 0 {
			// Two pages claiming the same printed number is a misread digit.
			// Fall back to the offset rather than picking one at random.
			return 0, false
		}
		found = index
	}
	return found, found != 0
}

// labelOffset is scan index minus printed number, measured over the pages that
// print a number and taken as the most common answer.
//
// The median would be wrong here and the mean would be worse. An issue with an
// unnumbered insert has two offsets, one either side of it, and what we want is
// the one that holds for most of the issue, with the rest handled by matching
// the printed label directly.
func labelOffset(pages []Page) (int, bool) {
	counts := map[int]int{}
	for _, page := range pages {
		printed, err := strconv.Atoi(page.Label)
		if err != nil || printed <= 0 {
			continue
		}
		counts[page.Index-printed]++
	}
	best, seen := 0, 0
	for offset, count := range counts {
		if count > seen || (count == seen && offset < best) {
			best, seen = offset, count
		}
	}
	return best, seen > 0
}

// withinScan turns the contents' printed page list into scan indexes and drops
// anything outside the article's own span, which is what stops a mistyped page
// in the contents from claiming half the issue.
func withinScan(printed []int, start, end int, byIndex map[int]Page) []int {
	var out []int
	for _, number := range printed {
		index, ok := byLabel(number, byIndex)
		if !ok || index < start || index > end {
			continue
		}
		out = append(out, index)
	}
	sort.Ints(out)
	return out
}

// labelRange writes the printed pages as a citation. A contiguous run is a
// range, anything else is the list, because an article that runs 12, 13, 47 is
// one that jumped the adverts and a range would be a lie.
func labelRange(labels []string) string {
	if len(labels) == 0 {
		return ""
	}
	if len(labels) == 1 {
		return labels[0]
	}
	numbers := make([]int, 0, len(labels))
	for _, label := range labels {
		n, err := strconv.Atoi(label)
		if err != nil {
			return strings.Join(labels, ", ")
		}
		numbers = append(numbers, n)
	}
	for i := 1; i < len(numbers); i++ {
		if numbers[i] != numbers[i-1]+1 {
			return strings.Join(labels, ", ")
		}
	}
	return labels[0] + "-" + labels[len(labels)-1]
}

// authors splits the contents' author field, which is one string with the names
// separated however the mirror felt.
func authors(field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil
	}
	parts := strings.FieldsFunc(field, func(r rune) bool {
		return r == ',' || r == ';'
	})
	var out []string
	for _, part := range parts {
		if name := strings.Join(strings.Fields(part), " "); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// slugOf names the article file.
//
// The publisher's own slug is used where there is one, because it is what the
// mirror's URL uses and joining on it later is free. Otherwise the title is
// transliterated. A row with neither, which happens for the untitled fillers,
// falls back to the issue and the position, which is ugly and unique, and unique
// is the requirement.
func slugOf(row manifest.Row, title string, key corpus.IssueKey, ordinal int) string {
	if slug := strings.TrimSpace(row.Slug); slug != "" {
		return rubric.Slug(slug)
	}
	if slug := rubric.Slug(title); slug != "" {
		return clip(slug, 60)
	}
	return fmt.Sprintf("%d-%s-%02d", key.Year, key.Number, ordinal)
}

func clip(slug string, limit int) string {
	if len(slug) <= limit {
		return slug
	}
	cut := strings.LastIndex(slug[:limit], "-")
	if cut < limit/2 {
		cut = limit
	}
	return strings.Trim(slug[:cut], "-")
}
