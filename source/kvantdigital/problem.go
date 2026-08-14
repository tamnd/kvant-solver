package kvantdigital

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Problem is one item of Задачник «Кванта» as the site serves it. The site
// does something no other source does: it puts the condition and the solution
// on one page, even though the magazine printed them two to four issues apart.
// That pairing is the ground truth the grader is built on, and it is the
// reason the solver is worth building for this material at all.
type Problem struct {
	// ID is the printed number with a Latin letter, so M301 and F1200. The
	// site writes the Cyrillic М and Ф, which look the same and sort
	// differently, and letting either into an identifier is a trap.
	ID      string
	Subject string
	Number  int

	// Label is the number exactly as the site prints it, Cyrillic letter and
	// all, kept so a page can be found again by searching for what it says.
	Label string

	URL string

	Condition Part
	Solution  *Part

	// Authors proposed the problem. Solvers wrote the published solution, and
	// are usually the editors rather than the proposer.
	Authors []Person
	Solvers []Person

	// Refs is what the metadata block says about where each half was printed.
	Refs []Ref
}

// HasPublishedSolution reports whether the magazine printed a solution. The
// ones that did not are the genuinely open problems and are the interesting
// half of the corpus for a solver.
func (p *Problem) HasPublishedSolution() bool { return p.Solution != nil }

// Part is one half of a problem: the condition or the published solution.
type Part struct {
	Year   int
	Number string

	// HTML is the block as served. The conversion to Markdown happens later,
	// and keeping the source here means a change in that conversion does not
	// need another fetch.
	HTML string

	// Text is the paragraphs, joined by blank lines, with the site's invisible
	// typesetting characters removed. Formulas are already LaTeX between
	// dollars, which is why this material never needs OCR.
	Text string

	// Byline is the line the magazine printed under a problem, which names the
	// person who proposed it and often their school and town.
	Byline string

	Figures []Figure

	// ViewURL opens the printed page this half appeared on.
	ViewURL string
}

// Figure is one illustration.
type Figure struct {
	URL     string
	Caption string
}

// Ref is one printed appearance of a problem, from the metadata block.
type Ref struct {
	IssueKey string
	Year     int
	Number   string

	// Pages is as printed, so it can read 51—52.
	Pages string

	// Kind is condition or solution.
	Kind string
}

// The two kinds of Ref.
const (
	KindCondition = "condition"
	KindSolution  = "solution"
)

var (
	reProblemLabel = regexp.MustCompile(`([МФMF])\s*(\d{1,5})`)
	reSection      = regexp.MustCompile(`\((\d{4}),\s*№\s*([0-9]{1,2}(?:[-–—][0-9]{1,2})?)\)`)
	reIssueKeyAttr = regexp.MustCompile(`kvant_\d{4}_\d{1,2}(?:-\d{1,2})?`)
)

// ParseProblemLabel reads М301 or F1200, in either alphabet, and returns the
// canonical Latin form.
func ParseProblemLabel(s string) (subject string, number int, ok bool) {
	m := reProblemLabel.FindStringSubmatch(clean(s))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n < 1 {
		return "", 0, false
	}
	switch m[1] {
	case "М", "M":
		return "math", n, true
	case "Ф", "F":
		return "physics", n, true
	}
	return "", 0, false
}

// ProblemID is the canonical identifier for a subject and number.
func ProblemID(subject string, number int) string {
	letter := "M"
	if subject == "physics" {
		letter = "F"
	}
	return fmt.Sprintf("%s%d", letter, number)
}

// ParseProblem reads one problem page.
func ParseProblem(r io.Reader) (*Problem, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}

	p := &Problem{Label: clean(doc.Find("h1 .mark--hlwords--flt").First().Text())}
	if p.Label == "" {
		p.Label = clean(doc.Find("h1").First().Text())
	}
	subject, number, ok := ParseProblemLabel(p.Label)
	if !ok {
		return nil, fmt.Errorf("no problem number on the page, the markup has probably moved")
	}
	p.Subject, p.Number = subject, number
	p.ID = ProblemID(subject, number)
	p.URL = ProblemURL(p.ID[:1], number)

	doc.Find(".box--block").Each(func(_ int, block *goquery.Selection) {
		heading := clean(block.Find("h2 .no-copy").First().Text())
		switch {
		case strings.HasPrefix(heading, "Условие"):
			p.Condition = readPart(block, heading)
		case strings.HasPrefix(heading, "Решение"):
			part := readPart(block, heading)
			p.Solution = &part
		case strings.HasPrefix(heading, "Метаданные"):
			readProblemMeta(block, p)
		}
	})
	if p.Condition.Text == "" {
		return nil, fmt.Errorf("%s: no condition text on the page", p.ID)
	}
	return p, nil
}

// readPart reads the condition or solution block.
func readPart(block *goquery.Selection, heading string) Part {
	var part Part
	if m := reSection.FindStringSubmatch(heading); m != nil {
		part.Year, _ = strconv.Atoi(m[1])
		part.Number = m[2]
	}

	body := block.Find(".block--text .blurer--content.mark--hlwords--text").First()
	part.HTML, _ = body.Html()

	var paragraphs []string
	body.Find("p").Each(func(_ int, s *goquery.Selection) {
		if t := clean(s.Text()); t != "" {
			paragraphs = append(paragraphs, t)
		}
	})
	part.Text = strings.Join(paragraphs, "\n\n")
	part.Byline = clean(block.Find("p.meta--inscription").First().Text())

	block.Find("figure").Each(func(_ int, f *goquery.Selection) {
		src := absolute(f.Find("img").First().AttrOr("src", ""))
		if src == "" {
			return
		}
		part.Figures = append(part.Figures, Figure{
			URL:     src,
			Caption: clean(f.Find("figcaption").First().Text()),
		})
	})

	block.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		href := absolute(a.AttrOr("href", ""))
		if reViewHref.MatchString(href) {
			part.ViewURL = href
			return false
		}
		return true
	})
	return part
}

// readProblemMeta reads the definition list at the foot of the page. It is the
// only place that says which people proposed the problem and which wrote the
// published solution, and the only place that gives the page numbers for both
// halves in a form worth parsing.
func readProblemMeta(block *goquery.Selection, p *Problem) {
	block.Find("dl.object--meta dt").Each(func(_ int, dt *goquery.Selection) {
		key := clean(dt.Text())
		dd := dt.NextFiltered("dd")
		switch key {
		case "Условие":
			p.Authors = readMetaPeople(dd)
		case "Решение":
			p.Solvers = readMetaPeople(dd)
		case "Номера":
			dd.Find("p").Each(func(_ int, para *goquery.Selection) {
				if ref, ok := readMetaRef(para); ok {
					p.Refs = append(p.Refs, ref)
				}
			})
		}
	})
}

func readMetaPeople(dd *goquery.Selection) []Person {
	var out []Person
	dd.Find(".data--index--personalia a[href]").Each(func(_ int, a *goquery.Selection) {
		href := absolute(a.AttrOr("href", ""))
		slug := PersonSlug(href)
		if slug == "" {
			// The problem index hangs its people off /problems/personalia/
			// rather than /indices/personalia/, and it is the same person.
			slug = problemPersonSlug(href)
		}
		if slug == "" {
			return
		}
		out = append(out, Person{Slug: slug, Name: clean(a.Text()), URL: PersonaliaURL(slug)})
	})
	return out
}

var reProblemPersonHref = regexp.MustCompile(`/problems/personalia/([a-z0-9_]+)/$`)

func problemPersonSlug(href string) string {
	if m := reProblemPersonHref.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}

// readMetaRef reads one line of the Номера block, which reads as
// 1975. — № 1. — Стр. 41 [условие].
func readMetaRef(para *goquery.Selection) (Ref, bool) {
	var ref Ref
	para.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if m := reIssueKeyAttr.FindString(a.AttrOr("data-url", "")); m != "" {
			ref.IssueKey = m
		}
	})
	if ref.IssueKey == "" {
		return Ref{}, false
	}
	year, number, ok := SplitIssueKey(ref.IssueKey)
	if !ok {
		return Ref{}, false
	}
	ref.Year, ref.Number = year, number
	ref.Pages = clean(para.Find("label").First().Text())

	text := clean(para.Text())
	switch {
	case strings.Contains(text, "[условие]"):
		ref.Kind = KindCondition
	case strings.Contains(text, "[решение]"):
		ref.Kind = KindSolution
	}
	return ref, true
}
