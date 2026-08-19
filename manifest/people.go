package manifest

import (
	"cmp"
	"slices"
	"strings"

	"github.com/tamnd/kvant-solver/rubric"
)

// Personalia is every named contributor. The magazine ran for fifty years and
// the same person appears under a surname and initials, under a full name, and
// occasionally under a misprint, so this file is where the variants are
// gathered rather than being decided again in every stage.
type Personalia struct {
	Count  int      `yaml:"count"`
	People []Person `yaml:"people"`
}

// Person is one contributor.
type Person struct {
	Slug string `yaml:"slug"`
	Name string `yaml:"name"`
	URL  string `yaml:"url,omitempty"`

	// Items is how many contents rows name this person, which is the only
	// measure of prominence available before the text exists.
	Items int `yaml:"items,omitempty"`

	// Variants are the other spellings seen for the same person. They are kept
	// because a search for a name has to find all of them.
	Variants []string `yaml:"variants,omitempty"`
}

// Add records a person, merging with an existing row of the same slug.
func (p *Personalia) Add(person Person) {
	i := slices.IndexFunc(p.People, func(x Person) bool { return x.Slug == person.Slug })
	if i < 0 {
		p.People = append(p.People, person)
		return
	}
	cur := &p.People[i]
	cur.Items += person.Items
	if person.URL != "" {
		cur.URL = person.URL
	}
	if person.Name != "" && person.Name != cur.Name && !slices.Contains(cur.Variants, person.Name) {
		cur.Variants = append(cur.Variants, person.Name)
	}
	for _, v := range person.Variants {
		if v != cur.Name && !slices.Contains(cur.Variants, v) {
			cur.Variants = append(cur.Variants, v)
		}
	}
}

// Sort puts the file in a stable order.
func (p *Personalia) Sort() {
	for i := range p.People {
		slices.Sort(p.People[i].Variants)
	}
	slices.SortFunc(p.People, func(a, b Person) int { return cmp.Compare(a.Slug, b.Slug) })
	p.Count = len(p.People)
}

// Rubrics is the taxonomy of the standing sections the magazine ran, with the
// spellings actually seen rather than the ones it ought to have used. Квант
// changed the wording of its own rubric banners over fifty years, so the
// canonical name is a decision this file records and the variants are the
// evidence for it.
type Rubrics struct {
	Count   int      `yaml:"count"`
	Rubrics []Rubric `yaml:"rubrics"`
}

// Rubric is one standing section.
type Rubric struct {
	Name  string `yaml:"name"`
	Slug  string `yaml:"slug"`
	Count int    `yaml:"count"`

	// Known says the section is in the taxonomy the rubric package pins. A
	// rubric that is not is a section the magazine started and nobody has named
	// yet, and it is worth seeing that in the file rather than finding it in an
	// index six stages later.
	Known bool `yaml:"known"`

	// Group marks the archive's own contents buckets, Основные статьи and
	// Разное, which are the two largest entries here and are not sections of
	// the magazine at all.
	Group bool `yaml:"group,omitempty"`

	// First and Last are the years this rubric was seen in, which is how a
	// rubric that ran for three years in the 1970s is told from one that ran
	// throughout.
	First int `yaml:"first_year,omitempty"`
	Last  int `yaml:"last_year,omitempty"`

	Variants []Variant `yaml:"variants,omitempty"`
}

// Variant is one spelling of a rubric name and how often it appeared.
type Variant struct {
	Text  string `yaml:"text"`
	Count int    `yaml:"count"`
	First int    `yaml:"first_year,omitempty"`
	Last  int    `yaml:"last_year,omitempty"`
}

// From counts the rubrics afresh off the contents.
//
// A rubric is a summary of what the contents rows say and not a record of its
// own, so it is counted from them rather than added to as issues are read. The
// two go apart in three ways otherwise, and all three had happened by the time
// this was written. Resyncing a year counted it twice, because the old file was
// read back in and the year's rows were then observed onto it a second time.
// Nothing ever reconsidered Known or Group, so a section added to the taxonomy
// never reached a file already written. And an entry is matched on its slug, so
// changing how a slug is spelled did not update the old entries, it appended a
// second set beside them and left the file holding both.
func (r *Rubrics) From(toc *TOC) {
	r.Rubrics = nil
	for _, block := range toc.Issues {
		year := KeyYear(block.Key)
		for _, row := range block.Rows {
			r.observe(row.Rubric, year)
		}
	}
	r.Sort()
}

// observe records one sighting of a rubric banner in a given year.
func (r *Rubrics) observe(name string, year int) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	canon := rubric.Canonical(name)
	i := slices.IndexFunc(r.Rubrics, func(x Rubric) bool { return x.Slug == canon.Slug })
	if i < 0 {
		r.Rubrics = append(r.Rubrics, Rubric{
			Name:     name,
			Slug:     canon.Slug,
			Known:    canon.Known,
			Group:    canon.Group,
			Count:    1,
			First:    year,
			Last:     year,
			Variants: []Variant{{Text: name, Count: 1, First: year, Last: year}},
		})
		return
	}
	cur := &r.Rubrics[i]
	cur.Count++
	if year > 0 && (cur.First == 0 || year < cur.First) {
		cur.First = year
	}
	if year > cur.Last {
		cur.Last = year
	}
	j := slices.IndexFunc(cur.Variants, func(v Variant) bool { return v.Text == name })
	if j < 0 {
		cur.Variants = append(cur.Variants, Variant{Text: name, Count: 1, First: year, Last: year})
		return
	}
	v := &cur.Variants[j]
	v.Count++
	if year > 0 && (v.First == 0 || year < v.First) {
		v.First = year
	}
	if year > v.Last {
		v.Last = year
	}
}

// Sort puts the commonest rubric first, which is the order a person reading
// the file wants, and makes the name the commonest variant.
func (r *Rubrics) Sort() {
	for i := range r.Rubrics {
		slices.SortFunc(r.Rubrics[i].Variants, func(a, b Variant) int {
			if c := cmp.Compare(b.Count, a.Count); c != 0 {
				return c
			}
			return cmp.Compare(a.Text, b.Text)
		})
		if v := r.Rubrics[i].Variants; len(v) > 0 {
			r.Rubrics[i].Name = v[0].Text
		}
	}
	slices.SortFunc(r.Rubrics, func(a, b Rubric) int {
		if c := cmp.Compare(b.Count, a.Count); c != 0 {
			return c
		}
		return cmp.Compare(a.Slug, b.Slug)
	})
	r.Count = len(r.Rubrics)
}
