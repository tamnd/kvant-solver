// Package corpus holds the model every other package reads and writes through:
// identifiers, front matter, and the hashing that makes staleness decidable
// without a model call.
package corpus

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FirstYear is the year Kvant began. Nothing in the corpus predates it.
const FirstYear = 1970

// IssueKey identifies one issue of the magazine. Printed numbers are not always
// a single integer: two months are often printed as one issue, and the corpus
// keeps that as the publisher wrote it, so 5-6 stays 5-6 and is never split or
// rounded to 5.
type IssueKey struct {
	Year   int
	Number string
}

var (
	issueKeyRe = regexp.MustCompile(`^kvant_(\d{4})_(\d{1,2}(?:-\d{1,2})?)$`)
	numberRe   = regexp.MustCompile(`^\d{1,2}(?:-\d{1,2})?$`)
)

// NewIssueKey checks a year and a printed number and returns the key for them.
func NewIssueKey(year int, number string) (IssueKey, error) {
	if year < FirstYear {
		return IssueKey{}, fmt.Errorf("year %d is before the first issue in %d", year, FirstYear)
	}
	if !numberRe.MatchString(number) {
		return IssueKey{}, fmt.Errorf("issue number %q is not a printed number", number)
	}
	return IssueKey{Year: year, Number: number}, nil
}

// ParseIssueKey reads the kvant_1975_1 form.
func ParseIssueKey(s string) (IssueKey, error) {
	m := issueKeyRe.FindStringSubmatch(s)
	if m == nil {
		return IssueKey{}, fmt.Errorf("issue key %q is not of the form kvant_1975_1", s)
	}
	year, err := strconv.Atoi(m[1])
	if err != nil {
		return IssueKey{}, err
	}
	return NewIssueKey(year, m[2])
}

// String is the canonical form, and is what appears in front matter.
func (k IssueKey) String() string {
	return fmt.Sprintf("kvant_%d_%s", k.Year, k.Number)
}

// Dir is where the issue lives under a language root, as 1975/01 or 1976/05-06.
// The number is padded so that a directory listing sorts the way a shelf does.
func (k IssueKey) Dir() string {
	parts := strings.Split(k.Number, "-")
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return fmt.Sprintf("%d/%s", k.Year, strings.Join(parts, "-"))
}

// Zero reports whether the key was never set.
func (k IssueKey) Zero() bool { return k.Year == 0 && k.Number == "" }

// PageID identifies one printed page by its position in the scan of its issue.
// The scan index is not the printed page label: an issue opens on a cover that
// carries no number, and some pages are unnumbered, so the two are tracked
// separately and reconciled by the page map.
type PageID struct {
	Issue IssueKey
	Index int
}

var pageIDRe = regexp.MustCompile(`^(kvant_\d{4}_\d{1,2}(?:-\d{1,2})?)_p(\d{4})$`)

// ParsePageID reads the kvant_1975_1_p0007 form.
func ParsePageID(s string) (PageID, error) {
	m := pageIDRe.FindStringSubmatch(s)
	if m == nil {
		return PageID{}, fmt.Errorf("page id %q is not of the form kvant_1975_1_p0007", s)
	}
	key, err := ParseIssueKey(m[1])
	if err != nil {
		return PageID{}, err
	}
	index, err := strconv.Atoi(m[2])
	if err != nil {
		return PageID{}, err
	}
	if index < 1 {
		return PageID{}, fmt.Errorf("page index %d is not positive", index)
	}
	return PageID{Issue: key, Index: index}, nil
}

// String is the canonical form.
func (p PageID) String() string {
	return fmt.Sprintf("%s_p%04d", p.Issue, p.Index)
}

// Filename is the page file within its issue directory.
func (p PageID) Filename() string { return fmt.Sprintf("%04d.md", p.Index) }

// Subject is what a problem is about. The magazine numbers the two series
// separately and has done since 1970.
type Subject string

// The two problem series of Задачник «Кванта».
const (
	Math    Subject = "math"
	Physics Subject = "physics"
)

// ProblemID is an M or F number as printed. These are already permanent
// identifiers outside this corpus, since the problems are cited by number
// across the Russian olympiad literature, so they are kept as the primary key
// and a tag is added only for internal cross referencing.
type ProblemID struct {
	Subject Subject
	Number  int
}

var problemIDRe = regexp.MustCompile(`^([MFmf])(\d{1,5})$`)

// ParseProblemID reads M1234 and F567, in either case.
func ParseProblemID(s string) (ProblemID, error) {
	m := problemIDRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ProblemID{}, fmt.Errorf("problem id %q is not of the form M1234 or F567", s)
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return ProblemID{}, err
	}
	if n < 1 {
		return ProblemID{}, fmt.Errorf("problem number %d is not positive", n)
	}
	subject := Math
	if strings.EqualFold(m[1], "F") {
		subject = Physics
	}
	return ProblemID{Subject: subject, Number: n}, nil
}

// String is the canonical form, always upper case.
func (p ProblemID) String() string {
	letter := "M"
	if p.Subject == Physics {
		letter = "F"
	}
	return fmt.Sprintf("%s%d", letter, p.Number)
}

// Path is the file for this problem under a language root.
func (p ProblemID) Path() string {
	dir := "m"
	if p.Subject == Physics {
		dir = "f"
	}
	return fmt.Sprintf("problems/%s/%04d.md", dir, p.Number)
}

// Tag is a permanent four character identifier for a citable object. The idea
// and the shape are the Stacks Project's. A tag is assigned once, is never
// reused for anything else, and is never edited: when a label changes the tag
// follows the object and the old label goes to tags/aliases. That is what lets
// a citation survive a reextraction or a resplit.
type Tag string

var tagRe = regexp.MustCompile(`^[0-9A-Z]{4}$`)

// ParseTag checks the shape.
func ParseTag(s string) (Tag, error) {
	if !tagRe.MatchString(s) {
		return "", fmt.Errorf("tag %q is not four characters of 0-9 and A-Z", s)
	}
	return Tag(s), nil
}

// Valid reports whether the tag has the right shape.
func (t Tag) Valid() bool { return tagRe.MatchString(string(t)) }

// ArticleID identifies an article by the issue it appeared in and the slug the
// publisher gave it. The publisher also hangs an eight hex digit suffix off
// that slug. It is recorded in front matter as source_id and kept out of the
// identifier, because it is theirs and may move under us.
type ArticleID struct {
	Issue IssueKey
	Slug  string
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:[_-][a-z0-9]+)*$`)

// NewArticleID checks the slug and returns the identifier.
func NewArticleID(key IssueKey, slug string) (ArticleID, error) {
	if key.Zero() {
		return ArticleID{}, fmt.Errorf("article %q has no issue", slug)
	}
	if !slugRe.MatchString(slug) {
		return ArticleID{}, fmt.Errorf("article slug %q is not lower case latin, digits and separators", slug)
	}
	return ArticleID{Issue: key, Slug: slug}, nil
}

// String is the canonical form, as 1975-1-bronshteyn-ellips.
func (a ArticleID) String() string {
	return fmt.Sprintf("%d-%s-%s", a.Issue.Year, a.Issue.Number, a.Slug)
}
