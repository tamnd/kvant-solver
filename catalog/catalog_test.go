package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
	"github.com/tamnd/kvant-solver/source/kvantmccme"
	"github.com/tamnd/kvant-solver/source/mathnetru"
)

type fakeDigital struct {
	refs   []kvantdigital.IssueRef
	issue  *kvantdigital.Issue
	people []kvantdigital.Person
	err    error
}

func (f *fakeDigital) IssuesIndex(context.Context) ([]kvantdigital.IssueRef, error) {
	return f.refs, f.err
}

func (f *fakeDigital) Issue(context.Context, int, string) (*kvantdigital.Issue, error) {
	return f.issue, f.err
}

func (f *fakeDigital) Personalia(context.Context) ([]kvantdigital.Person, error) {
	return f.people, f.err
}

type fakeMathNet struct {
	refs []mathnetru.IssueRef
	err  error
}

func (f *fakeMathNet) Contents(context.Context) ([]mathnetru.IssueRef, error) {
	return f.refs, f.err
}

type fakeMCCME struct {
	refs     []kvantmccme.ArchiveRef
	contents map[int]*kvantmccme.Contents
	byTitle  map[int]*kvantmccme.Contents
	authors  []kvantmccme.Author
	err      error

	calls         int
	contentsCalls int
}

func (f *fakeMCCME) Archive(context.Context) (*kvantmccme.Archive, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &kvantmccme.Archive{Refs: f.refs}, nil
}

func (f *fakeMCCME) Contents(_ context.Context, year int, _ string) (*kvantmccme.Contents, error) {
	f.contentsCalls++
	if c, ok := f.contents[year]; ok {
		return c, nil
	}
	return nil, source.ErrNotFound
}

func (f *fakeMCCME) ContentsByTitle(ctx context.Context, year int, month string) (*kvantmccme.Contents, error) {
	if c, ok := f.byTitle[year]; ok {
		return c, nil
	}
	if f.byTitle != nil {
		return nil, source.ErrNotFound
	}
	return f.Contents(ctx, year, month)
}

func (f *fakeMCCME) Authors(context.Context) ([]kvantmccme.Author, error) {
	return f.authors, nil
}

func ref(year int, number string) kvantdigital.IssueRef {
	return kvantdigital.IssueRef{
		Year:   year,
		Number: number,
		Key:    kvantdigital.IssueKey(year, number),
		URL:    kvantdigital.IssueURL(year, number),
	}
}

func TestSyncIssuesBuildsTheList(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{
			ref(1975, "1"), ref(2024, "5-6"), ref(1975, "2"),
		}},
		MCCME: &fakeMCCME{refs: []kvantmccme.ArchiveRef{
			{Year: 1975, Number: "1", Month: "01", URL: kvantmccme.IssueURL(1975, "01")},
		}},
		MathNet: &fakeMathNet{refs: []mathnetru.IssueRef{
			{Year: 2024, Number: "5-6", Query: 5, FullText: true, URL: mathnetru.IssueURL(2024, 5)},
		}},
	}
	errata := &manifest.Errata{}
	issues, err := c.SyncIssues(t.Context(), errata)
	if err != nil {
		t.Fatal(err)
	}
	if issues.Count != 3 || issues.Years != 2 {
		t.Errorf("%d issues over %d years", issues.Count, issues.Years)
	}
	if issues.Issues[0].Key != "kvant_1975_1" {
		t.Errorf("first issue is %q", issues.Issues[0].Key)
	}
	double, ok := issues.Get("kvant_2024_5-6")
	if !ok {
		t.Fatal("the double issue went missing")
	}
	if double.Sources.MathNet == nil || !double.Sources.MathNet.FullText {
		t.Error("mathnet did not get recorded on the double issue")
	}
	// The mirror lists 1975 and stops long before 2024, so the newer issue
	// gets no mirror URL. Nothing here invents one.
	if double.Sources.MCCME != nil {
		t.Error("sync wrote an MCCME URL the mirror had never named")
	}
	first, _ := issues.Get("kvant_1975_1")
	if first.Sources.MCCME == nil || first.Sources.MCCME.URL == "" {
		t.Error("the mirror lists 1975 number 1 and it did not get written down")
	}
	if len(errata.Entries) != 0 {
		t.Errorf("errata: %+v", errata.Entries)
	}
}

func TestADoubleIssueSplitByOneSourceIsAnErratum(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{ref(2024, "5-6")}},
		MCCME:   &fakeMCCME{},
		MathNet: &fakeMathNet{refs: []mathnetru.IssueRef{
			{Year: 2024, Number: "5", Query: 5, URL: mathnetru.IssueURL(2024, 5)},
		}},
	}
	errata := &manifest.Errata{}
	issues, err := c.SyncIssues(t.Context(), errata)
	if err != nil {
		t.Fatal(err)
	}
	// The two still match on the paper object, so the row is filled in.
	iss, _ := issues.Get("kvant_2024_5-6")
	if iss.Sources.MathNet == nil {
		t.Fatal("the issue was not matched across the two sources")
	}
	// And the disagreement is written down rather than averaged away.
	if len(errata.Open()) != 1 {
		t.Fatalf("errata: %+v", errata.Entries)
	}
	e := errata.Entries[0]
	if e.Kind != "number" || e.Claims[SourceDigital] != "5-6" || e.Claims[SourceMathNet] != "5" {
		t.Errorf("erratum is %+v", e)
	}
}

func TestAnIssueOnlyMathNetHasIsAnErratum(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{ref(2024, "1")}},
		MCCME:   &fakeMCCME{},
		MathNet: &fakeMathNet{refs: []mathnetru.IssueRef{
			{Year: 2024, Number: "1", Query: 1},
			{Year: 2026, Number: "1", Query: 1},
		}},
	}
	errata := &manifest.Errata{}
	if _, err := c.SyncIssues(t.Context(), errata); err != nil {
		t.Fatal(err)
	}
	if len(errata.Entries) != 1 || errata.Entries[0].Kind != "missing_issue" {
		t.Fatalf("errata: %+v", errata.Entries)
	}
}

func TestLosingMathNetDoesNotFailTheSync(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{ref(1975, "1")}},
		MCCME:   &fakeMCCME{},
		MathNet: &fakeMathNet{err: errors.New("timeout")},
	}
	errata := &manifest.Errata{}
	issues, err := c.SyncIssues(t.Context(), errata)
	if err != nil {
		t.Fatal(err)
	}
	// mathnet is a second opinion on twenty of the fifty years. The list is
	// still worth having without it, as long as its absence is recorded.
	if issues.Count != 1 {
		t.Errorf("%d issues", issues.Count)
	}
	if len(errata.Entries) != 1 || errata.Entries[0].Kind != "source_unavailable" {
		t.Errorf("errata: %+v", errata.Entries)
	}
}

func TestLosingKvantDigitalDoesFailTheSync(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{err: source.ErrForbidden},
		MCCME:   &fakeMCCME{},
		MathNet: &fakeMathNet{},
	}
	// It is the only source with every issue. A run that cannot reach it has
	// nothing to write and should say so rather than write a short file.
	if _, err := c.SyncIssues(t.Context(), &manifest.Errata{}); err == nil {
		t.Fatal("the sync should fail when the one complete source is gone")
	}
}

func TestSyncIssueFillsInTheDetail(t *testing.T) {
	c := &Catalog{Digital: &fakeDigital{
		issue: &kvantdigital.Issue{
			Year: 1975, Number: "1", Key: "kvant_1975_1",
			Title:     "Квант. 1975. № 1",
			PageCount: 80,
			URL:       kvantdigital.IssueURL(1975, "1"),
			Rows: []kvantdigital.TOCRow{
				{Rubric: "Задачник «Кванта»", Title: "Задачи М301—М305", Page: 41, Sheet: 42, HasText: true},
				{Rubric: "Квант для младших школьников", Title: "Равенства из спичек", Page: 12, Sheet: 13},
			},
			Sheets: make([]kvantdigital.Sheet, 84),
		},
	}}

	iss, err := manifest.NewIssue(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	toc := &manifest.TOC{}
	rubrics := &manifest.Rubrics{}
	if err := c.SyncIssue(t.Context(), &iss, toc, rubrics); err != nil {
		t.Fatal(err)
	}
	if iss.Pages != 80 {
		t.Errorf("printed page count is %d", iss.Pages)
	}
	// An 80 page issue is 84 surfaces once the covers are counted, and the
	// scan has all of them. Confusing the two numbers means the last four
	// sheets of every issue go unfetched.
	if iss.Sheets != 84 {
		t.Errorf("sheet count is %d", iss.Sheets)
	}
	if iss.Sources.Digital.Rows != 2 || iss.Sources.Digital.TextRows != 1 {
		t.Errorf("digital source is %+v", iss.Sources.Digital)
	}
	rows, ok := toc.Get("kvant_1975_1")
	if !ok || len(rows) != 2 {
		t.Fatalf("toc has %d rows", len(rows))
	}
	if rubrics.Count != 0 {
		t.Error("Observe should not set the count, Sort does")
	}
	rubrics.Sort()
	if rubrics.Count != 2 {
		t.Errorf("%d rubrics", rubrics.Count)
	}
}

func TestADeepRunWithoutTheMirrorKeepsTheMirrorsPageRanges(t *testing.T) {
	c := &Catalog{Digital: &fakeDigital{
		issue: &kvantdigital.Issue{
			Year: 1975, Number: "1", Key: "kvant_1975_1",
			PageCount: 80,
			URL:       kvantdigital.IssueURL(1975, "1"),
			Rows: []kvantdigital.TOCRow{
				{Title: "Задачи М301—М305", Page: 41},
				{Title: "Равенства из спичек", Page: 12},
			},
			Sheets: make([]kvantdigital.Sheet, 84),
		},
	}}

	// This is the state an earlier mirrored run leaves behind. Pages is the one
	// field on a row that kvant.digital cannot fill in, so a plain deep run
	// setting its own rows over these used to throw every range away.
	toc := &manifest.TOC{}
	toc.Set("kvant_1975_1", []manifest.Row{
		{Title: "Задачи М301—М305", Page: 41, Pages: []int{41, 42, 43}, Source: SourceDigital},
		{Title: "Равенства из спичек", Page: 12, Pages: []int{12, 13}, Source: SourceDigital},
	})

	iss, err := manifest.NewIssue(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SyncIssue(t.Context(), &iss, toc, nil); err != nil {
		t.Fatal(err)
	}
	rows, _ := toc.Get("kvant_1975_1")
	if len(rows) != 2 {
		t.Fatalf("toc has %d rows", len(rows))
	}
	if !slices.Equal(rows[0].Pages, []int{41, 42, 43}) {
		t.Errorf("first row covers %v", rows[0].Pages)
	}
	if !slices.Equal(rows[1].Pages, []int{12, 13}) {
		t.Errorf("second row covers %v", rows[1].Pages)
	}
	// The rest of the row is this run's answer, not the old one.
	if rows[0].Page != 41 {
		t.Errorf("first row starts at %d", rows[0].Page)
	}
}

func TestAnIssueNobodyHasReadBeforeHasNoRangesToKeep(t *testing.T) {
	c := &Catalog{Digital: &fakeDigital{
		issue: &kvantdigital.Issue{
			Year: 1975, Number: "1", Key: "kvant_1975_1",
			URL:    kvantdigital.IssueURL(1975, "1"),
			Rows:   []kvantdigital.TOCRow{{Title: "Равенства из спичек", Page: 12}},
			Sheets: make([]kvantdigital.Sheet, 84),
		},
	}}
	iss, err := manifest.NewIssue(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	toc := &manifest.TOC{}
	if err := c.SyncIssue(t.Context(), &iss, toc, nil); err != nil {
		t.Fatal(err)
	}
	rows, ok := toc.Get("kvant_1975_1")
	if !ok || len(rows) != 1 {
		t.Fatalf("toc has %d rows", len(rows))
	}
	if rows[0].Pages != nil {
		t.Errorf("a range appeared out of nowhere: %v", rows[0].Pages)
	}
}

func TestTheMirrorIsPartOfASync(t *testing.T) {
	mirror := &fakeMCCME{refs: []kvantmccme.ArchiveRef{
		{Year: 2004, Number: "1", Month: "01", DjVuURL: "https://kvant.mccme.ru/djvu/2004_01.djvu"},
		{Year: 2011, Number: "1", Month: "01", URL: "https://kvant.mccme.ru/2011/01/"},
	}}
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{ref(2004, "1")}},
		MCCME:   mirror,
		MathNet: &fakeMathNet{},
	}
	errata := &manifest.Errata{}
	issues, err := c.SyncIssues(t.Context(), errata)
	if err != nil {
		t.Fatal(err)
	}
	// 2003 and 2004 are on the mirror only as DjVu, and that is the one place
	// a whole issue file exists for those years.
	iss, _ := issues.Get("kvant_2004_1")
	if iss.Sources.MCCME == nil || iss.Sources.MCCME.DjVuURL == "" {
		t.Fatalf("2004 came out as %+v", iss.Sources.MCCME)
	}
	// An issue the mirror lists and kvant.digital does not is a finding, not a
	// new row: kvant.digital is the complete collection.
	if len(errata.Entries) != 1 || errata.Entries[0].Kind != "missing_issue" {
		t.Fatalf("errata: %+v", errata.Entries)
	}
	if errata.Entries[0].Issue != "kvant_2011_1" {
		t.Errorf("erratum is about %q", errata.Entries[0].Issue)
	}
	if issues.Count != 1 {
		t.Errorf("%d issues", issues.Count)
	}
	// The archive page is one request and a deep sync asks per issue, so it is
	// fetched once and kept.
	if _, err := c.Archive(t.Context()); err != nil {
		t.Fatal(err)
	}
	if mirror.calls != 1 {
		t.Errorf("the archive page was fetched %d times", mirror.calls)
	}
}

func TestTheMirrorNumberingOf1993IsNotThreeMissingIssues(t *testing.T) {
	// 1993 came out as four double issues. kvant.digital numbers them the way
	// they were printed and the mirror files them as 01, 02, 05 and 06.
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{
			ref(1993, "1-2"), ref(1993, "3-4"), ref(1993, "9-10"), ref(1993, "11-12"),
		}},
		MCCME: &fakeMCCME{refs: []kvantmccme.ArchiveRef{
			{Year: 1993, Number: "1", Month: "01", URL: kvantmccme.IssueURL(1993, "01")},
			{Year: 1993, Number: "2", Month: "02", URL: kvantmccme.IssueURL(1993, "02")},
			{Year: 1993, Number: "5", Month: "05", URL: kvantmccme.IssueURL(1993, "05")},
			{Year: 1993, Number: "6", Month: "06", URL: kvantmccme.IssueURL(1993, "06")},
		}},
		MathNet: &fakeMathNet{},
	}
	errata := &manifest.Errata{}
	issues, err := c.SyncIssues(t.Context(), errata)
	if err != nil {
		t.Fatal(err)
	}
	if issues.Count != 4 {
		t.Errorf("%d issues in 1993", issues.Count)
	}
	if len(errata.Entries) != 3 {
		t.Fatalf("errata: %+v", errata.Entries)
	}
	for _, e := range errata.Entries {
		// The magazine is not missing three issues that year. It is the
		// numbering the two sites disagree about, and saying otherwise would be
		// a false claim about the archive.
		if e.Kind != "number" {
			t.Errorf("erratum is %+v", e)
		}
		if !strings.Contains(e.Claims[SourceDigital], "1-2, 3-4, 9-10, 11-12") {
			t.Errorf("erratum does not say how the year is numbered: %+v", e)
		}
	}
}

func TestLosingTheMirrorDoesNotFailTheSync(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{refs: []kvantdigital.IssueRef{ref(1975, "1")}},
		MCCME:   &fakeMCCME{err: errors.New("timeout")},
		MathNet: &fakeMathNet{},
	}
	errata := &manifest.Errata{}
	issues, err := c.SyncIssues(t.Context(), errata)
	if err != nil {
		t.Fatal(err)
	}
	if issues.Count != 1 {
		t.Errorf("%d issues", issues.Count)
	}
	if len(errata.Entries) != 1 || errata.Entries[0].Kind != "source_unavailable" {
		t.Errorf("errata: %+v", errata.Entries)
	}
}

func TestProbeReportsAPathPerYear(t *testing.T) {
	issues := &manifest.Issues{}
	for _, y := range []struct {
		year     int
		number   string
		rows     int
		textRows int
		mccme    *manifest.MCCME
	}{
		{1975, "1", 24, 24, &manifest.MCCME{URL: "https://kvant.mccme.ru/1975/01/index.htm", Rows: 24}},
		{2004, "1", 18, 0, &manifest.MCCME{DjVuURL: "https://kvant.mccme.ru/djvu/2004_01.djvu"}},
		{2010, "1", 20, 0, &manifest.MCCME{PDFURL: "https://kvant.mccme.ru/pdf/2010/2010-01.pdf"}},
		{2011, "1", 18, 0, nil},
	} {
		iss, err := manifest.NewIssue(y.year, y.number)
		if err != nil {
			t.Fatal(err)
		}
		iss.Sources.Digital = &manifest.Digital{URL: "x", Rows: y.rows, TextRows: y.textRows}
		iss.Sources.MCCME = y.mccme
		issues.Add(iss)
	}
	issues.Sort()

	probe := ProbeSources(issues, nil)
	if len(probe.Years) != 4 {
		t.Fatalf("%d years", len(probe.Years))
	}
	// 1975 has the mirror's typeset text, 2010 has a born digital PDF, 2004
	// has a DjVu scan and nothing else, and 2011 is on neither mirror.
	want := []string{"publisher", "vision", "native", "vision"}
	for i, w := range want {
		if got := probe.Years[i].Path(); got != w {
			t.Errorf("%d takes the %s path, want %s", probe.Years[i].Year, got, w)
		}
	}
	if probe.Years[1].DjVu != 1 || probe.Years[2].Native != 1 || probe.Years[3].Mirror != 0 {
		t.Errorf("counts came out as %+v", probe.Years)
	}
	// A PDF from before the born digital years is a scan and does not save a
	// model call, so it must not be counted as native.
	if probe.Years[0].Native != 0 {
		t.Error("1975 was counted as native")
	}
	out := probe.String()
	if !strings.Contains(out, "native") || !strings.Contains(out, "2 issues take the vision path") {
		t.Errorf("the table reads:\n%s", out)
	}
}

func TestSyncPersonaliaKeepsTheTwoListsApart(t *testing.T) {
	c := &Catalog{
		Digital: &fakeDigital{people: []kvantdigital.Person{
			{Slug: "kolmogorov_a_n", Name: "Колмогоров А. Н.", Items: 12},
			{Slug: "abakumov_e_v", Name: "Абакумов Е. В."},
		}},
		MCCME: &fakeMCCME{authors: []kvantmccme.Author{
			{Slug: "abakumov_e", Name: "Абакумов Е."},
		}},
	}
	people, err := c.SyncPersonalia(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	if people.Count != 3 {
		t.Fatalf("%d people", people.Count)
	}
	// The mirror writes one initial where kvant.digital writes two, so two
	// different people can share a mirror slug. Merging on the name would join
	// people who are not the same person.
	var mirror int
	for _, p := range people.People {
		if strings.HasPrefix(p.Slug, "mccme:") {
			mirror++
		}
	}
	if mirror != 1 {
		t.Errorf("%d people came from the mirror", mirror)
	}
	// A person with no printed count has one item, not none.
	for _, p := range people.People {
		if p.Slug == "abakumov_e_v" && p.Items != 1 {
			t.Errorf("a person with no printed count came out with %d items", p.Items)
		}
	}
}
