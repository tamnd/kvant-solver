package kvantmccme

import (
	"strings"
	"testing"
)

func TestLetterURLUsesTheWindows1251Byte(t *testing.T) {
	got, ok := LetterURL('А')
	if !ok || got != "https://kvant.mccme.ru/let/192.htm" {
		t.Errorf("А gives %q %v", got, ok)
	}
	got, _ = LetterURL('Б')
	if got != "https://kvant.mccme.ru/let/193.htm" {
		t.Errorf("Б gives %q", got)
	}
	// A letter with no place in the encoding has no page, and asking for one
	// would be a request for a file that is not there.
	if _, ok := LetterURL('Ж'); !ok {
		t.Error("Ж is a Cyrillic letter and should have a page")
	}
	if _, ok := LetterURL('日'); ok {
		t.Error("a letter outside the encoding should not produce a URL")
	}
	if len(Letters) != 28 {
		t.Errorf("the index has %d letters", len(Letters))
	}
}

func TestParseAuthorIndex(t *testing.T) {
	authors, err := ParseAuthorIndex(fixture(t, "authors_a.htm"))
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 12 {
		t.Fatalf("%d authors", len(authors))
	}
	if authors[2].Name != "Абакумов Е." || authors[2].Slug != "abakumov_e" {
		t.Errorf("third author is %+v", authors[2])
	}
	if got := AuthorURL(authors[2].Slug); got != "https://kvant.mccme.ru/au/abakumov_e.htm" {
		t.Errorf("author url is %q", got)
	}
}

func TestTheSamePersonAppearsTwice(t *testing.T) {
	authors, err := ParseAuthorIndex(fixture(t, "authors_a.htm"))
	if err != nil {
		t.Fatal(err)
	}
	// Абрикосов (мл А..) and Абрикосов(мл А..) are one person typed twice, and
	// Адельсон--Вельский is the same as Адельсон-Вельский with a double hyphen.
	// The parser does not merge them, because merging is a decision that
	// belongs in the manifest with the evidence next to it, but the fixture
	// keeps them so the day someone adds merging there is a case to test.
	var abrikosov int
	for _, a := range authors {
		if strings.HasPrefix(a.Name, "Абрикосов") {
			abrikosov++
		}
	}
	if abrikosov != 3 {
		t.Errorf("%d spellings of Абрикосов, the fixture has three", abrikosov)
	}
}

func TestParseAuthorIndexRefusesAPageWithNoAuthors(t *testing.T) {
	if _, err := ParseAuthorIndex(strings.NewReader("<html><body>nothing</body></html>")); err == nil {
		t.Fatal("a page with no author links should be an error")
	}
}
