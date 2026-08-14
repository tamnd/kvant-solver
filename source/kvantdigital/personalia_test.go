package kvantdigital

import (
	"os"
	"strings"
	"testing"
)

func personalia(t *testing.T) *PersonaliaPage {
	t.Helper()
	f, err := os.Open("testdata/personalia_index.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	p, err := ParsePersonalia(f)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePersonalia(t *testing.T) {
	p := personalia(t)
	if len(p.People) < 8 {
		t.Fatalf("%d people on the page", len(p.People))
	}
	first := p.People[0]
	if first.Slug != "a_b" {
		t.Errorf("first slug is %q", first.Slug)
	}
	// The index has entries that are initials only, because that is how the
	// magazine signed some pieces. They are people and they belong in the file.
	if first.Name != "А. Б." {
		t.Errorf("first name is %q", first.Name)
	}
	if first.URL != "https://www.kvant.digital/indices/personalia/a_b/" {
		t.Errorf("first url is %q", first.URL)
	}
}

func TestItemCountIsOneWhenTheSiteOmitsIt(t *testing.T) {
	p := personalia(t)
	var withCount, without *Person
	for i := range p.People {
		switch {
		case p.People[i].Items > 0 && withCount == nil:
			withCount = &p.People[i]
		case p.People[i].Items == 0 && without == nil:
			without = &p.People[i]
		}
	}
	if withCount == nil || without == nil {
		t.Fatal("the fixture needs one person with a count and one without")
	}
	if withCount.Count() != withCount.Items {
		t.Errorf("%s has %d items but Count says %d", withCount.Slug, withCount.Items, withCount.Count())
	}
	// The site prints no number next to a person with a single item, so a zero
	// means one. Reading it as none would drop them from every report.
	if without.Count() != 1 {
		t.Errorf("%s has no printed count and Count says %d", without.Slug, without.Count())
	}
}

func TestPaginatorSaysHowManyPagesThereAre(t *testing.T) {
	p := personalia(t)
	// Walking until an empty page comes back would be 44 requests either way,
	// but knowing the count up front means the sync can report progress and
	// can tell a short page from a broken one.
	if p.LastPage != 43 {
		t.Errorf("last page is %d", p.LastPage)
	}
}

func TestPersonaliaPageURL(t *testing.T) {
	if got := PersonaliaPageURL(1); got != "https://www.kvant.digital/indices/personalia/" {
		t.Errorf("page 1 is %q", got)
	}
	if got := PersonaliaPageURL(2); got != "https://www.kvant.digital/indices/personalia/?page=2" {
		t.Errorf("page 2 is %q", got)
	}
}

func TestParsePersonaliaRefusesAnEmptyPage(t *testing.T) {
	if _, err := ParsePersonalia(strings.NewReader("<html><body></body></html>")); err == nil {
		t.Fatal("a page with no people should be an error")
	}
}
