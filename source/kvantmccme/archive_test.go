package kvantmccme

import (
	"os"
	"testing"
)

func archive(t *testing.T) *Archive {
	t.Helper()
	f, err := os.Open("testdata/archive_index.htm")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	a, err := ParseArchive(f)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestArchiveReadsTheFourLayouts(t *testing.T) {
	a := archive(t)
	for _, want := range []struct {
		year   int
		number string
		url    string
	}{
		{1975, "1", BaseURL + "/1975/01/index.htm"},
		{2003, "5", BaseURL + "/djvu/2003_05.djvu"},
		{2005, "1", BaseURL + "/pdf/2005-01.pdf"},
		{2012, "1", BaseURL + "/2012/01/"},
		{2024, "5-6", BaseURL + "/pdf/2024/2024-05-06.pdf"},
	} {
		ref, ok := a.Get(want.year, want.number)
		if !ok {
			t.Errorf("%d number %s is not in the archive", want.year, want.number)
			continue
		}
		got := ref.URL + ref.PDFURL + ref.DjVuURL
		if got != want.url {
			t.Errorf("%d number %s is at %q, want %q", want.year, want.number, got, want.url)
		}
	}
}

func TestADoubleIssueIsWrittenFourWays(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"05-06", "5-6"},
		{"11-12", "11-12"},
		{"056", "5-6"},
		{"56", "5-6"},
		{"1112", "11-12"},
		// And these are single issues that look like doubles if you squint.
		{"12", "12"},
		{"01", "1"},
		{"11", "11"},
		{"05", "5"},
	} {
		if got := ArchiveNumber(tc.in); got != tc.want {
			t.Errorf("ArchiveNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecemberIsNotIssuesOneAndTwo(t *testing.T) {
	// 2018-12.pdf is the December issue. Reading a run together pair out of it
	// would file the last issue of the year as the first.
	a := archive(t)
	if _, ok := a.Get(2018, "12"); !ok {
		t.Error("2018 number 12 went missing")
	}
	if _, ok := a.Get(2018, "1-2"); ok {
		t.Error("December came out as a double issue of January and February")
	}
}

func TestADoubledSlashDoesNotLoseAnIssue(t *testing.T) {
	// pdf/2018//2018-11.pdf is on the page as written.
	a := archive(t)
	ref, ok := a.Get(2018, "11")
	if !ok {
		t.Fatal("2018 number 11 went missing")
	}
	if ref.PDFURL != BaseURL+"/pdf/2018/2018-11.pdf" {
		t.Errorf("pdf url is %q", ref.PDFURL)
	}
}

func TestTheMirrorsOwnVersionLetterIsNotPartOfTheNumber(t *testing.T) {
	a := archive(t)
	// 2006-01s.pdf, 2015-56s.pdf and 2016-05u.pdf all carry a letter that is
	// the mirror's own versioning and says nothing about the issue.
	for _, want := range []struct {
		year   int
		number string
	}{{2006, "1"}, {2015, "5-6"}, {2016, "5"}} {
		if _, ok := a.Get(want.year, want.number); !ok {
			t.Errorf("%d number %s went missing", want.year, want.number)
		}
	}
}

func TestAContentsPageBringsItsSecondOrdering(t *testing.T) {
	a := archive(t)
	ref, _ := a.Get(1975, "1")
	if ref.ByTitleURL != BaseURL+"/1975/01/index_n.htm" {
		t.Errorf("by title url is %q", ref.ByTitleURL)
	}
	// A year that is only a PDF has no contents page and so no second
	// ordering to compare against.
	pdf, _ := a.Get(2005, "1")
	if pdf.URL != "" || pdf.ByTitleURL != "" {
		t.Errorf("a PDF only issue claims contents pages %q and %q", pdf.URL, pdf.ByTitleURL)
	}
}

func TestArchiveSkipsWhatIsNotAnIssue(t *testing.T) {
	a := archive(t)
	for _, r := range a.Refs {
		if r.Year < 1970 || r.Year > 2100 {
			t.Errorf("%+v is not an issue", r)
		}
		if r.Month == "" {
			t.Errorf("%+v has no month", r)
		}
	}
	// The link inside the HTML comment at the top of the page is not a link.
	if _, ok := a.Get(1970, "1"); ok {
		t.Error("a link inside a comment was followed")
	}
}

func TestMonthOf(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"1", "01"}, {"12", "12"}, {"5-6", "05"}, {"11-12", "11"}, {"", ""},
	} {
		if got := MonthOf(tc.in); got != tc.want {
			t.Errorf("MonthOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
