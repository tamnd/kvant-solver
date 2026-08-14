package catalog

import (
	"slices"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source/kvantmccme"
)

func entry(title string, pages ...int) kvantmccme.Entry {
	return kvantmccme.Entry{Title: title, Pages: pages}
}

func mirrorFixture(t *testing.T, byAuthor, byTitle []kvantmccme.Entry) (*Catalog, *manifest.Issue, *manifest.TOC, *manifest.Errata) {
	t.Helper()
	iss, err := manifest.NewIssue(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	toc := &manifest.TOC{}
	toc.Set(iss.Key, []manifest.Row{
		{Title: "Равенства из спичек", Page: 12, Source: SourceDigital},
		{Title: "Задачник «Кванта»", Page: 41, Source: SourceDigital},
	})
	c := &Catalog{MCCME: &fakeMCCME{
		refs: []kvantmccme.ArchiveRef{{
			Year: 1975, Number: "1", Month: "01",
			URL:        kvantmccme.IssueURL(1975, "01"),
			ByTitleURL: kvantmccme.IssueByTitleURL(1975, "01"),
		}},
		contents: map[int]*kvantmccme.Contents{1975: {Year: 1975, Month: "01", Entries: byAuthor}},
		byTitle:  map[int]*kvantmccme.Contents{1975: {Year: 1975, Month: "01", Entries: byTitle}},
	}}
	return c, &iss, toc, &manifest.Errata{}
}

func TestTheMirrorFillsInPageRanges(t *testing.T) {
	rows := []kvantmccme.Entry{
		entry("Равенства из спичек", 12, 13, 14),
		entry("Задачник «Кванта»", 41, 42, 43, 44),
	}
	c, iss, toc, errata := mirrorFixture(t, rows, rows)
	if err := c.SyncMirrorTOC(t.Context(), iss, toc, errata); err != nil {
		t.Fatal(err)
	}
	got, _ := toc.Get(iss.Key)
	// kvant.digital gives a row one starting page. The assembler needs to know
	// where the article stops, and only the mirror says.
	if !slices.Equal(got[0].Pages, []int{12, 13, 14}) {
		t.Errorf("first row covers %v", got[0].Pages)
	}
	if !slices.Equal(got[1].Pages, []int{41, 42, 43, 44}) {
		t.Errorf("second row covers %v", got[1].Pages)
	}
	if len(errata.Entries) != 0 {
		t.Errorf("errata: %+v", errata.Entries)
	}
	if iss.Sources.MCCME.Rows != 2 || iss.Sources.MCCME.TitleRows != 2 {
		t.Errorf("mirror source is %+v", iss.Sources.MCCME)
	}
	// 1975 is long before the mirror started posting whole issue PDFs, and the
	// archive page named none, so none is written.
	if iss.Sources.MCCME.PDFURL != "" {
		t.Errorf("pdf url is %q", iss.Sources.MCCME.PDFURL)
	}
}

func TestARowInOneOrderingOnlyIsAnErratum(t *testing.T) {
	c, iss, toc, errata := mirrorFixture(t,
		[]kvantmccme.Entry{entry("Равенства из спичек", 12), entry("Задачник «Кванта»", 41)},
		[]kvantmccme.Entry{entry("Равенства из спичек", 12)},
	)
	if err := c.SyncMirrorTOC(t.Context(), iss, toc, errata); err != nil {
		t.Fatal(err)
	}
	if len(errata.Entries) != 1 {
		t.Fatalf("errata: %+v", errata.Entries)
	}
	e := errata.Entries[0]
	if e.Kind != "toc_row" || e.Subject != "Задачник «Кванта»" {
		t.Errorf("erratum is %+v", e)
	}
	if e.Claims[SourceMCCMEAuthor] == e.Claims[SourceMCCMETitle] {
		t.Errorf("both orderings claim %q", e.Claims[SourceMCCMEAuthor])
	}
	// The row still gets its page from the ordering that has it. A fault in one
	// generated page is not a reason to throw away the other.
	got, _ := toc.Get(iss.Key)
	if !slices.Equal(got[1].Pages, []int{41}) {
		t.Errorf("second row covers %v", got[1].Pages)
	}
}

func TestQuotesAndYoAreNotADisagreement(t *testing.T) {
	c, iss, toc, errata := mirrorFixture(t,
		[]kvantmccme.Entry{entry("Задачник «Кванта»", 41), entry("Ёж и черепаха", 5)},
		[]kvantmccme.Entry{entry("Задачник \"Кванта\"", 41), entry("Еж  и черепаха", 5)},
	)
	if err := c.SyncMirrorTOC(t.Context(), iss, toc, errata); err != nil {
		t.Fatal(err)
	}
	// The two pages are generated separately and quote differently. Reporting
	// that as a missing row would bury the real faults under a thousand fake
	// ones.
	if len(errata.Entries) != 0 {
		t.Errorf("errata: %+v", errata.Entries)
	}
}

func TestAYearTheMirrorDoesNotHoldIsNotAnError(t *testing.T) {
	iss, err := manifest.NewIssue(2019, "1")
	if err != nil {
		t.Fatal(err)
	}
	c := &Catalog{MCCME: &fakeMCCME{}}
	errata := &manifest.Errata{}
	if err := c.SyncMirrorTOC(t.Context(), &iss, nil, errata); err != nil {
		t.Fatal(err)
	}
	if iss.Sources.MCCME != nil {
		t.Errorf("mirror source is %+v", iss.Sources.MCCME)
	}
	if len(errata.Entries) != 0 {
		t.Errorf("errata: %+v", errata.Entries)
	}
}

func TestOneOrderingMissingIsWorthSaying(t *testing.T) {
	c, iss, toc, errata := mirrorFixture(t, []kvantmccme.Entry{entry("Равенства из спичек", 12)}, nil)
	c.MCCME.(*fakeMCCME).byTitle = map[int]*kvantmccme.Contents{}
	if err := c.SyncMirrorTOC(t.Context(), iss, toc, errata); err != nil {
		t.Fatal(err)
	}
	if len(errata.Entries) != 1 || errata.Entries[0].Kind != "toc_ordering" {
		t.Fatalf("errata: %+v", errata.Entries)
	}
	if iss.Sources.MCCME.Rows != 1 || iss.Sources.MCCME.TitleRows != 0 {
		t.Errorf("mirror source is %+v", iss.Sources.MCCME)
	}
}

func TestAPDFOnlyIssueHasNoOrderingsToCompare(t *testing.T) {
	iss, err := manifest.NewIssue(2010, "1")
	if err != nil {
		t.Fatal(err)
	}
	mirror := &fakeMCCME{refs: []kvantmccme.ArchiveRef{{
		Year: 2010, Number: "1", Month: "01",
		PDFURL: "https://kvant.mccme.ru/pdf/2010/2010-01.pdf",
	}}}
	c := &Catalog{MCCME: mirror}
	if err := c.SyncMirrorTOC(t.Context(), &iss, nil, &manifest.Errata{}); err != nil {
		t.Fatal(err)
	}
	if iss.Sources.MCCME.PDFURL == "" {
		t.Error("2010 has a born digital PDF and it did not get written down")
	}
	// From 2005 the mirror has PDFs instead of typeset contents, so there is
	// no contents page to ask for and no request worth making.
	if mirror.contentsCalls != 0 {
		t.Errorf("%d contents pages were fetched for a PDF only issue", mirror.contentsCalls)
	}
}
