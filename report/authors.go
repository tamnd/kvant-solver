package report

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/tamnd/kvant-solver/corpus"
)

// The classes of thing that can be wrong with a byline.
//
// Each one is a different failure of the read and wants a different fix, which
// is why they are counted apart. A page whose byline came back as noise was
// misread; a byline that is initials and no surname was read correctly off a
// page that does not carry the surname, and there is nowhere else to get one.
const (
	// NotAName is a byline with no Russian letter in it at all. These are
	// decoder noise: abc, e, xnyxzk.
	NotAName = "not-a-name"

	// LatinTail is a real name with noise stuck on the end of it, as in
	// Абрамович В. С.l. The tail is nearly always a short pattern repeating,
	// which is the runaway rule showing up in a field too short to trip it.
	LatinTail = "latin-tail"

	// NoSurname is initials and nothing else. The magazine does sign some
	// pieces that way, so this is not a misread, but it is not a byline anybody
	// can look up either. The printed contents were checked against all of
	// these and print the same bare initials, so there is nothing to recover.
	NoSurname = "no-surname"

	// LooksMisread is a surname seen once or twice that is one letter from a
	// surname seen many times. This one is a suggestion and not a finding: it
	// is right often enough to be worth printing and wrong often enough that
	// nothing should act on it without opening the page.
	LooksMisread = "looks-misread"
)

// Byline is one distinct spelling that came off the corpus, with everywhere it
// appears.
type Byline struct {
	Name  string
	Class string
	// Note says what else is known, which for LooksMisread is the name it
	// probably should have been and how often each was seen.
	Note  string
	Files []string
	Count int
}

// AuthorCounts is the size of the thing the defects were found in, so that a
// count of defects can be read against it.
type AuthorCounts struct {
	Articles int
	Mentions int
	Distinct int
}

// Authors reads every article byline in the corpus and returns the ones that
// are not names.
//
// It reads the files rather than manifests/toc.yaml on purpose. The contents
// are keyed off the printed index and the articles are what the model made of
// the page, so the two disagree exactly where the read went wrong, and this
// report is about the read.
//
// Front matter is loaded unchecked, because a file whose hash has drifted still
// has a byline and the drift is a complaint the audit already makes.
func Authors(c *corpus.Corpus, lang string) ([]Byline, AuthorCounts, error) {
	var counts AuthorCounts
	seen := map[string][]string{}

	dir := filepath.Join(c.Root, "content", lang)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(c.Root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if !strings.Contains(rel, "/articles/") {
			return nil
		}
		f := &corpus.ArticleFront{}
		if _, err := corpus.LoadUnchecked(path, f); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		counts.Articles++
		for _, name := range f.Authors {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			counts.Mentions++
			seen[name] = append(seen[name], rel)
		}
		return nil
	})
	if err != nil {
		return nil, counts, err
	}
	counts.Distinct = len(seen)

	out := make([]Byline, 0, 16)
	for name, files := range seen {
		if class := classify(name); class != "" {
			sort.Strings(files)
			out = append(out, Byline{Name: name, Class: class, Files: files, Count: len(files)})
		}
	}
	out = append(out, misreadings(seen)...)

	sort.Slice(out, func(a, b int) bool {
		if out[a].Class != out[b].Class {
			return classRank(out[a].Class) < classRank(out[b].Class)
		}
		return out[a].Name < out[b].Name
	})
	return out, counts, nil
}

// classify names what is wrong with one byline, or returns an empty string when
// it looks like a name.
func classify(name string) string {
	var cyrillic, latin bool
	for _, r := range name {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic = true
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			latin = true
		}
	}
	switch {
	case !cyrillic:
		return NotAName
	case latin:
		return LatinTail
	case surname(name) == "":
		return NoSurname
	}
	return ""
}

// surname is the first run of Russian letters long enough to be a family name.
// Two letters is not one: В. and Ф. are initials, and Ли is not a name the
// magazine ever printed.
func surname(name string) string {
	var word []rune
	for _, r := range name {
		if unicode.Is(unicode.Cyrillic, r) {
			word = append(word, r)
			continue
		}
		if len(word) >= 3 {
			return string(word)
		}
		word = word[:0]
	}
	if len(word) >= 3 {
		return string(word)
	}
	return ""
}

// misreadings looks for a surname that is one substituted letter from a much
// commoner one.
//
// Only a substitution, never an inserted or dropped letter, because Russian
// makes the feminine of a surname by adding one: Петров and Петрова are two
// people and Васильев and Васильева are two more. Only when the rare one is
// rare and the common one is common, because Штейнберг and Штернберг are both
// real and both wrote about ten pieces each, and nothing here can tell those
// apart. What the imbalance buys is the difference between a pair of authors
// and one author read wrong on one page out of thirty.
func misreadings(seen map[string][]string) []Byline {
	count := map[string]int{}
	files := map[string][]string{}
	for name, where := range seen {
		s := surname(name)
		if s == "" {
			continue
		}
		count[s] += len(where)
		files[s] = append(files[s], where...)
	}
	names := make([]string, 0, len(count))
	for s := range count {
		names = append(names, s)
	}
	sort.Strings(names)

	var out []Byline
	for _, rare := range names {
		r := count[rare]
		if r > maxRare {
			continue
		}
		best, most := "", 0
		for _, common := range names {
			c := count[common]
			if c < minCommon || c < r*imbalance || c <= most || !oneLetterApart(rare, common) {
				continue
			}
			best, most = common, c
		}
		if best == "" {
			continue
		}
		where := files[rare]
		sort.Strings(where)
		out = append(out, Byline{
			Name:  rare,
			Class: LooksMisread,
			Note:  fmt.Sprintf("%s appears %d times, %s appears %d", rare, r, best, most),
			Files: where,
			Count: r,
		})
	}
	return out
}

// The thresholds the imbalance test runs at. They are round numbers chosen
// against the corpus rather than derived from anything: at these values the
// list is short enough to read in a minute and most of what is on it is real.
const (
	maxRare   = 2
	minCommon = 8
	imbalance = 10
)

// oneLetterApart reports whether two words of the same length differ in exactly
// one position.
func oneLetterApart(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if len(x) != len(y) {
		return false
	}
	diff := 0
	for n := range x {
		if x[n] != y[n] {
			diff++
			if diff > 1 {
				return false
			}
		}
	}
	return diff == 1
}

func classRank(class string) int {
	switch class {
	case NotAName:
		return 0
	case LatinTail:
		return 1
	case NoSurname:
		return 2
	default:
		return 3
	}
}

// AuthorMarkdown writes reports/author-defects.md.
func AuthorMarkdown(bylines []Byline, counts AuthorCounts, now time.Time) string {
	var b strings.Builder
	b.WriteString("# Bylines that are not names\n\n")
	fmt.Fprintf(&b, "Generated %s by `kvant report authors`.\n\n", now.UTC().Format("2006-01-02"))

	fmt.Fprintf(&b, "%s articles carry %s author mentions between them, %s distinct spellings.\n",
		thousands(counts.Articles), thousands(counts.Mentions), thousands(counts.Distinct))
	if len(bylines) == 0 {
		b.WriteString("None of them looks wrong.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d of those spellings are listed below.\n\n", len(bylines))

	for _, section := range []struct{ class, title, blurb string }{
		{NotAName, "No Russian letter in it",
			"The byline is gone and something else is standing in it. " +
				"Look at what: it is the notation out of the article's own title, so `abc` heads " +
				"a piece about a2+b2=c2 and `xnyxzk` heads one about xn+yx=zk. " +
				"This is rule 3 in a field too short to trip it. These want the page read again."},
		{LatinTail, "A name with noise stuck on the end",
			"The same leak, caught earlier. The surname and initials came out right and the " +
				"notation from the title followed them into the field: Cnk, nxnx, 10n. " +
				"The tail can be cut without reading the page again, but the page is still worth " +
				"opening, because whatever leaked here leaked from somewhere."},
		{NoSurname, "Initials and no surname",
			"Not a misread. The magazine signs some pieces this way and the page has no surname on it to find. " +
				"`manifests/toc.yaml` is not the way out either: it was checked against every one of these and " +
				"it prints the same bare initials in almost all of them, so there is nothing there to recover. " +
				"Leave them as they are unless somebody identifies the writer from outside the magazine."},
		{LooksMisread, "One letter from a much commoner name",
			"A suggestion and not a finding. Nothing here should be changed without opening the page first."},
	} {
		rows := pick(bylines, section.class)
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", section.title)
		fmt.Fprintf(&b, "%s\n\n", section.blurb)
		b.WriteString("| byline | seen | where |\n|---|---|---|\n")
		for _, row := range rows {
			note := row.Name
			if row.Note != "" {
				note = row.Note
			}
			fmt.Fprintf(&b, "| %s | %d | %s |\n", escape(note), row.Count, where(row.Files))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// pick is the rows of one class, in the order Authors put them in.
func pick(bylines []Byline, class string) []Byline {
	var out []Byline
	for _, row := range bylines {
		if row.Class == class {
			out = append(out, row)
		}
	}
	return out
}

// where lists the files, and stops after a few. A name that appears on fourteen
// pages is a pattern rather than fourteen separate things to go and look at.
func where(files []string) string {
	const most = 3
	shown := files
	if len(shown) > most {
		shown = shown[:most]
	}
	out := strings.Join(shown, "<br>")
	if len(files) > most {
		out += fmt.Sprintf("<br>and %d more", len(files)-most)
	}
	return out
}

// escape keeps a pipe in a name from ending the table cell early.
func escape(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// thousands writes a number the way it is read.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for at := len(s) - 3; at > 0; at -= 3 {
		s = s[:at] + "," + s[at:]
	}
	return s
}
