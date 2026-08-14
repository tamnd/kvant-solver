package kvantdigital

import (
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/PuerkitoBio/goquery"
)

// Person is one named contributor from the personalia index. The site holds
// authors, but also the people a piece is about, translators, and the people
// who only ever appear as the source of a problem, which is why the index is
// called personalia and not authors.
type Person struct {
	Slug string
	Name string
	URL  string

	// Items is the number the index prints next to a name. It is missing for
	// people with a single item, so a zero here means one and not none.
	Items int
}

// Count is Items with the site's own shorthand undone.
func (p Person) Count() int {
	if p.Items == 0 {
		return 1
	}
	return p.Items
}

// PersonaliaPage is one page of the index.
type PersonaliaPage struct {
	People []Person

	// LastPage is the highest page number the paginator names, so the caller
	// knows how many more requests it has to make without walking until it
	// gets an empty page.
	LastPage int
}

// PersonaliaPageURL is one page of the personalia index.
func PersonaliaPageURL(page int) string {
	if page <= 1 {
		return PersonaliaIndexURL()
	}
	return fmt.Sprintf("%s?page=%d", PersonaliaIndexURL(), page)
}

var rePageQuery = regexp.MustCompile(`[?&]page=(\d+)`)

// ParsePersonalia reads one page of the personalia index.
func ParsePersonalia(r io.Reader) (*PersonaliaPage, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, err
	}
	out := &PersonaliaPage{LastPage: 1}
	seen := map[string]bool{}

	doc.Find(".data--index--personalia").Each(func(_ int, item *goquery.Selection) {
		link := item.Find("a[href]").First()
		slug := PersonSlug(absolute(link.AttrOr("href", "")))
		if slug == "" || seen[slug] {
			return
		}
		seen[slug] = true
		p := Person{
			Slug: slug,
			Name: clean(link.Find("span").First().Text()),
			URL:  PersonaliaURL(slug),
		}
		// The count sits in a marker next to the name and is absent for a
		// person with one item.
		if n, err := strconv.Atoi(clean(item.Find("ins.marker--itemtotal").First().Text())); err == nil {
			p.Items = n
		}
		out.People = append(out.People, p)
	})
	if len(out.People) == 0 {
		return nil, fmt.Errorf("no people on the personalia page, the markup has probably moved")
	}

	doc.Find(".wrap--paginator a[href]").Each(func(_ int, a *goquery.Selection) {
		m := rePageQuery.FindStringSubmatch(a.AttrOr("href", ""))
		if m == nil {
			return
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > out.LastPage {
			out.LastPage = n
		}
	})
	return out, nil
}
