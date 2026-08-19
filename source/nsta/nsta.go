// Package nsta reads the index of the English Quantum.
//
// Quantum ran from 1990 to 2001, published by the National Science Teachers
// Association with the Quantum Bureau of the Russian Academy of Sciences, and
// a good part of every issue was Kvant in translation. NSTA still hosts the
// cumulative index, one line per article, and that index is the only list of
// what was ever carried over into English.
//
// Only the bibliographic fields are taken: the title, who wrote it, which
// issue it was in, what page it started on and which department it ran under.
// The index also carries a one line description of each article, and that is
// somebody's writing rather than a fact about the magazine, so it is dropped
// here rather than copied into a manifest.
package nsta

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// IndexURL is the cumulative index, the whole run on one page.
const IndexURL = "https://www.nsta.org/quantum-magazine-math-and-science"

// Entry is one line of the index.
type Entry struct {
	Title string

	// Authors is the byline split into people. A good many lines have none,
	// because the departments that ran unsigned, the contest pages and the
	// front matter are indexed the same way an article is.
	Authors []string

	// Year and Months are the issue. Quantum was bimonthly and its cover date
	// is a pair of months, except for the two 1990 pilots, which were single
	// months and came out before the run proper started that September.
	Year   int
	Months string

	// Page is where the article starts. Department is the standing section it
	// ran under, which is worth keeping because it separates the translated
	// features from the contest and puzzle pages that were written in English.
	Page       int
	Department string
}

// Issue is the cover date, written the way the index writes it.
func (e Entry) Issue() string { return fmt.Sprintf("%s %d", e.Months, e.Year) }

// months are the pair a bimonthly cover date is made of. They are matched
// case insensitively and Mau is in the list because the index has one row that
// spells May that way, and a parser that drops a real article over somebody's
// typo is worse than a parser that knows about it.
var months = `Jan|Feb|Mar|Apr|May|Mau|Jun|Jul|Aug|Sep|Oct|Nov|Dec`

// reWhen matches the tail of an index line: the cover date and the page.
//
// It is this loose because the index is twelve years of hand keying and it is
// not uniform. The slash between the two months goes missing, the year is
// sometimes held off by a space, the p in front of the page number is
// sometimes absent, and the pilot issues carry one month rather than two.
var reWhen = regexp.MustCompile(
	`(?i)\b((?:` + months + `)(?:\s*/?\s*(?:` + months + `))?)\s*(\d{2})\b,?\s*p?\.?\s*(\d{1,3})\b`)

// reMonth is one month name on its own, used to take a cover date apart.
var reMonth = regexp.MustCompile(`(?i)` + months)

// reDepartment is the standing section, in brackets at the end of the line.
var reDepartment = regexp.MustCompile(`\(([^()]*)\)\s*\.?\s*$`)

// reDescription is the parenthesis after the title. It is somebody's prose and
// it is cut off and thrown away rather than parsed.
var reDescription = regexp.MustCompile(`^\s*\([^)]*\)\s*,?\s*`)

// ParseIndex reads the cumulative index page.
//
// Each entry is a bold title followed by the rest of the line as running text,
// and several entries share a paragraph, so the parse is driven off the bold
// runs rather than off the paragraphs. Taking one entry per paragraph looks
// right and quietly loses forty of them, most of the 1990 pilots among them.
func ParseIndex(r io.Reader) ([]Entry, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}

	var out []Entry
	seen := map[string]bool{}
	doc.Find("p, li").Each(func(_ int, block *goquery.Selection) {
		marks := block.Find("strong, b")
		marks.Each(func(i int, mark *goquery.Selection) {
			title := clean(mark.Text())
			title = strings.TrimSuffix(title, ",")
			if title == "" || strings.HasPrefix(strings.ToLower(title), "note") {
				return
			}
			rest, ok := after(block, mark, marks.Eq(i+1))
			if !ok {
				return
			}
			entry, ok := parseLine(title, rest)
			if !ok {
				return
			}
			// The same article is listed twice where it ran under two
			// departments. One row is enough for a mapping.
			key := entry.Title + "|" + entry.Issue() + "|" + strconv.Itoa(entry.Page)
			if seen[key] {
				return
			}
			seen[key] = true
			out = append(out, entry)
		})
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("the index page has no entries on it, so the page is not the index")
	}
	return out, nil
}

// after is the text between one bold title and the next one in the paragraph,
// which is that entry's line and nobody else's.
func after(block, mark, next *goquery.Selection) (string, bool) {
	_, rest, found := strings.Cut(block.Text(), mark.Text())
	if !found {
		return "", false
	}
	if next.Length() > 0 {
		if head, _, found := strings.Cut(rest, next.Text()); found {
			rest = head
		}
	}
	return clean(rest), true
}

// parseLine pulls the bibliography out of one index line. A line with no cover
// date on it is not an entry: the index carries a few standing notes about how
// to read it, and they are laid out the same way.
func parseLine(title, rest string) (Entry, bool) {
	when := reWhen.FindStringSubmatchIndex(rest)
	if when == nil {
		return Entry{}, false
	}
	year, err := strconv.Atoi(rest[when[4]:when[5]])
	if err != nil {
		return Entry{}, false
	}
	page, err := strconv.Atoi(rest[when[6]:when[7]])
	if err != nil {
		return Entry{}, false
	}
	entry := Entry{
		Title:  title,
		Year:   fullYear(year),
		Months: normalizeMonths(rest[when[2]:when[3]]),
		Page:   page,
	}
	entry.Authors = splitAuthors(reDescription.ReplaceAllString(rest[:when[0]], ""))
	if d := reDepartment.FindStringSubmatch(rest[when[1]:]); d != nil {
		entry.Department = clean(d[1])
	}
	return entry, true
}

// fullYear reads the two digit year. The run is 1990 to 2001 and nothing else,
// so there is no century to guess at.
func fullYear(yy int) int {
	if yy >= 90 {
		return 1900 + yy
	}
	return 2000 + yy
}

// normalizeMonths puts a cover date back into the form the index mostly uses,
// so that the rows the index typed loosely group with the rows it did not.
func normalizeMonths(s string) string {
	// Scanning for the month names rather than splitting on the separator,
	// because in a couple of rows there is no separator to split on.
	var parts []string
	for _, part := range reMonth.FindAllString(s, -1) {
		part = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		if part == "Mau" {
			part = "May"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// splitAuthors breaks a byline into people. Quantum wrote them out in English
// with and between the last two, and a byline is short enough that this is the
// whole of it.
func splitAuthors(byline string) []string {
	byline = clean(strings.Trim(clean(byline), " ,"))
	if byline == "" {
		return nil
	}
	byline = strings.ReplaceAll(byline, " and ", ",")
	var out []string
	for name := range strings.SplitSeq(byline, ",") {
		if name = clean(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// clean collapses the whitespace, including the non breaking spaces the page
// is full of.
func clean(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, " ", " ")), " ")
}
