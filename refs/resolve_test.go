package refs_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/refs"
)

// article is what the fixture needs to know to write one file.
type article struct {
	issue string
	year  int
	num   string
	slug  string
	pages string
	body  string
}

// fixture writes a corpus with an issue list, a printed contents and the
// articles named, then tags it so that resolution has something to point at.
//
// The contents covers issues the corpus has no articles for on purpose. That is
// the normal state of this project for years at a time, and it is the case the
// pending status exists for.
func fixture(t *testing.T, articles []article, problems []string, toc map[string][]manifest.Row) *corpus.Corpus {
	t.Helper()
	root := t.TempDir()

	for i, a := range articles {
		front := &corpus.ArticleFront{
			ID:         fmt.Sprintf("%d-%s-%s", a.year, a.num, a.slug),
			Issue:      a.issue,
			Year:       a.year,
			Number:     a.num,
			Title:      a.slug,
			PageLabels: a.pages,
			Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
		}
		if first := refs.Labels(a.pages); len(first) > 0 {
			front.PageFirst, front.PageLast = first[0], first[len(first)-1]
		}
		dir := filepath.Join(root, "content/ru", fmt.Sprint(a.year), fmt.Sprintf("%02s", a.num), "articles")
		path := filepath.Join(dir, fmt.Sprintf("%02d_%s.md", i+1, a.slug))
		body := a.body
		if body == "" {
			body = "Тело статьи.\n"
		}
		if err := corpus.Save(path, front, body); err != nil {
			t.Fatal(err)
		}
	}

	for _, number := range problems {
		id, err := corpus.ParseProblemID(number)
		if err != nil {
			t.Fatal(err)
		}
		front := &corpus.ProblemFront{
			ID:         id.String(),
			Subject:    id.Subject,
			PosedIn:    "kvant_1975_1",
			Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
		}
		if err := corpus.Save(filepath.Join(root, "content/ru", id.Path()), front, "Условие задачи.\n"); err != nil {
			t.Fatal(err)
		}
	}

	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	issues := &manifest.Issues{}
	contents := &manifest.TOC{}
	for key, rows := range toc {
		var year int
		var num string
		if _, err := fmt.Sscanf(key, "kvant_%d_%s", &year, &num); err != nil {
			t.Fatalf("%s is not an issue key: %v", key, err)
		}
		issues.Issues = append(issues.Issues, manifest.Issue{Key: key, Year: year, Number: num, Dir: key})
		contents.Set(key, rows)
	}
	issues.Count = len(issues.Issues)
	if err := store.Write(manifest.IssuesFile, issues); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(manifest.TOCFile, contents); err != nil {
		t.Fatal(err)
	}

	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func rows(pairs ...any) []manifest.Row {
	var out []manifest.Row
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, manifest.Row{Page: pairs[i].(int), Title: pairs[i+1].(string), Source: "fixture"})
	}
	return out
}

// build tags the corpus and resolves the years asked for, which is what the
// command does.
func build(t *testing.T, c *corpus.Corpus, years ...int) map[int]*refs.Graph {
	t.Helper()
	idx, err := refs.Load(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	graphs, err := refs.Build(c, "ru", idx, years)
	if err != nil {
		t.Fatal(err)
	}
	return graphs
}

func only(t *testing.T, graphs map[int]*refs.Graph, year int) refs.Ref {
	t.Helper()
	graph, ok := graphs[year]
	if !ok {
		t.Fatalf("no graph for %d", year)
	}
	if len(graph.Refs) != 1 {
		t.Fatalf("the graph has %d references, want 1: %+v", len(graph.Refs), graph.Refs)
	}
	return graph.Refs[0]
}

func TestACitationWithAPageBecomesALinkToOneArticle(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1975, № 2, с. 14\n"},
		{issue: "kvant_1975_2", year: 1975, num: "2", slug: "parabola", pages: "12-18"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1975_2": rows(12, "Парабола"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Linked {
		t.Fatalf("status is %s, want %s: %+v", ref.Status, refs.Linked, ref)
	}
	if ref.ToLabel != "1975-2-parabola" {
		t.Errorf("it points at %q, want the article covering page 14", ref.ToLabel)
	}
	if ref.To == "" {
		t.Error("a linked reference has no tag, which is the whole point of one")
	}
}

// The magazine prints the year only when it does not mean the volume in the
// reader's hands.
func TestACitationWithNoYearMeansTheYearOfTheArticleCiting(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант» № 2, с. 14\n"},
		{issue: "kvant_1975_2", year: 1975, num: "2", slug: "parabola", pages: "12-18"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1975_2": rows(12, "Парабола"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Linked || ref.ToLabel != "1975-2-parabola" {
		t.Errorf("got %s pointing at %q, want a link to the 1975 article", ref.Status, ref.ToLabel)
	}
}

// This is the case the four statuses exist for. A pointer into a year nobody
// has read is a good pointer, and filing it with the broken ones would say the
// matching is worse than it is for as long as the corpus is incomplete.
func TestAPointerIntoAnUnreadIssueIsPendingAndCarriesTheTitle(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1974, № 3, с. 20\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1974_3": rows(18, "Что то другое", 20, "Теорема Пифагора", 25, "Ещё одна"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Pending {
		t.Fatalf("status is %s, want %s: %+v", ref.Status, refs.Pending, ref)
	}
	if ref.Title != "Теорема Пифагора" {
		t.Errorf("title is %q, want the contents line covering page 20", ref.Title)
	}
	if ref.To != "" {
		t.Errorf("it points at %s, and nothing has been read to point at", ref.To)
	}
}

// A row says where it starts and the next one says where it ends, so page 22
// belongs to the row that starts at 20.
func TestThePendingTitleIsTheRowThatCoversThePage(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1974, № 3, с. 22\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1974_3": rows(18, "Что то другое", 20, "Теорема Пифагора", 25, "Ещё одна"),
	})
	tagAll(t, c)

	if ref := only(t, build(t, c, 1975), 1975); ref.Title != "Теорема Пифагора" {
		t.Errorf("title is %q, want the row that starts at 20", ref.Title)
	}
}

func TestAnIssueWithNoPageResolvesAsFarAsTheIssue(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "об этом писал «Квант», 1974, № 3\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1974_3": rows(18, "Что то другое"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Issue {
		t.Fatalf("status is %s, want %s", ref.Status, refs.Issue)
	}
	if ref.Issue != "kvant_1974_3" {
		t.Errorf("it names %q, want kvant_1974_3", ref.Issue)
	}
}

func TestAnIssueTheArchiveDoesNotHaveIsUnresolved(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1969, № 3, с. 20\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Unresolved {
		t.Fatalf("status is %s, want %s", ref.Status, refs.Unresolved)
	}
	if !strings.Contains(ref.Why, "1969") {
		t.Errorf("the reason is %q, and it should say which issue is missing", ref.Why)
	}
}

// The magazine is monthly, so a number past twelve came off the page wrong.
// That is worth telling apart from a gap in the collection, because it is the
// one of the two somebody can fix.
func TestANumberPastTwelveIsNotAnIssueNumber(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1974, №911, 12\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Unresolved {
		t.Fatalf("status is %s, want %s", ref.Status, refs.Unresolved)
	}
	if !strings.Contains(ref.Why, "not an issue number") {
		t.Errorf("the reason is %q, want it to say the number is not an issue number", ref.Why)
	}
}

func TestAProblemInTheCorpusBecomesALink(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "это задача М381\n"},
	}, []string{"M381"}, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Linked {
		t.Fatalf("status is %s, want %s: %+v", ref.Status, refs.Linked, ref)
	}
	if ref.ToLabel != "M381" || ref.To == "" {
		t.Errorf("it points at %q with tag %q, want the problem and a tag", ref.ToLabel, ref.To)
	}
}

// The problems are extracted in a later milestone, so today every problem
// citation lands here, and that has to read as waiting rather than as broken.
func TestAProblemNotExtractedYetIsPending(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "это задача М381\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
	})
	tagAll(t, c)

	ref := only(t, build(t, c, 1975), 1975)
	if ref.Status != refs.Pending {
		t.Errorf("status is %s, want %s: %+v", ref.Status, refs.Pending, ref)
	}
	if ref.Problem != "M381" {
		t.Errorf("problem is %q, want M381 recorded even though nothing has it", ref.Problem)
	}
}

func TestLabelsExpandsThePageFieldEveryWayItIsWritten(t *testing.T) {
	cases := []struct {
		field string
		want  []int
	}{
		{"12", []int{12}},
		{"12-15", []int{12, 13, 14, 15}},
		{"12, 14, 16", []int{12, 14, 16}},
		{"12-13, 16", []int{12, 13, 16}},
		{"", nil},
		{"not a page", nil},
	}
	for _, c := range cases {
		got := refs.Labels(c.field)
		if len(got) != len(c.want) {
			t.Errorf("%q gave %v, want %v", c.field, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q gave %v, want %v", c.field, got, c.want)
				break
			}
		}
	}
}

// Running the pass again over an unchanged corpus has to give the same graph,
// because the file is committed and a rerun that shuffles it makes every diff
// unreadable.
func TestBuildingTwiceGivesTheSameGraph(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1975, № 2, с. 14 и задачу М381\n"},
		{issue: "kvant_1975_2", year: 1975, num: "2", slug: "parabola", pages: "12-18",
			body: "см. «Квант», 1975, № 1, с. 3\n"},
	}, []string{"M381"}, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1975_2": rows(12, "Парабола"),
	})
	tagAll(t, c)

	first, second := build(t, c, 1975), build(t, c, 1975)
	if fmt.Sprint(first[1975]) != fmt.Sprint(second[1975]) {
		t.Errorf("two passes over one corpus disagree:\n%v\n%v", first[1975], second[1975])
	}
	if first[1975].Count != 3 {
		t.Errorf("found %d references, want 3", first[1975].Count)
	}
}

func TestTheReportSaysWhatDidNotResolveAndAtWhatRate(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1969, № 3, с. 20, и «Квант», 1975, № 2, с. 14\n"},
		{issue: "kvant_1975_2", year: 1975, num: "2", slug: "parabola", pages: "12-18"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1975_2": rows(12, "Парабола"),
	})
	tagAll(t, c)

	graphs := build(t, c, 1975)
	rate, ok := refs.OK(graphs)
	if ok {
		t.Errorf("one unresolved in two is a rate of %.2f, which is over the threshold", rate)
	}
	md := refs.Report(graphs, fixedTime())
	for _, want := range []string{"1969", "unresolved", "## The list", "1975"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report does not mention %q:\n%s", want, md)
		}
	}
}

// Pending is not a failure, so a corpus whose every citation points into an
// unread year still passes.
func TestPendingDoesNotCountAgainstTheThreshold(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1974, № 3, с. 20\n"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1974_3": rows(20, "Теорема Пифагора"),
	})
	tagAll(t, c)

	rate, ok := refs.OK(build(t, c, 1975))
	if !ok || rate != 0 {
		t.Errorf("rate is %.2f and ok is %v, want a corpus of pending references to pass", rate, ok)
	}
}

func TestTheGraphSurvivesARoundTripThroughTheManifest(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5",
			body: "см. «Квант», 1975, № 2, с. 14\n"},
		{issue: "kvant_1975_2", year: 1975, num: "2", slug: "parabola", pages: "12-18"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1975_2": rows(12, "Парабола"),
	})
	tagAll(t, c)

	graphs := build(t, c, 1975)
	store, err := refs.Store(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := refs.Save(store, graphs[1975]); err != nil {
		t.Fatal(err)
	}
	back, err := refs.ReadAll(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Fatalf("read %d graphs back, want 1", len(back))
	}
	if fmt.Sprint(back[1975].Refs) != fmt.Sprint(graphs[1975].Refs) {
		t.Errorf("the graph changed on the way through YAML:\n%v\n%v", back[1975].Refs, graphs[1975].Refs)
	}
}

func TestYearsIsWhatTheCorpusHasArticlesFor(t *testing.T) {
	c := fixture(t, []article{
		{issue: "kvant_1975_1", year: 1975, num: "1", slug: "ellips", pages: "2-5"},
		{issue: "kvant_1977_4", year: 1977, num: "4", slug: "parabola", pages: "12-18"},
	}, nil, map[string][]manifest.Row{
		"kvant_1975_1": rows(2, "Эллипс"),
		"kvant_1977_4": rows(12, "Парабола"),
	})
	got, err := refs.Years(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 1975 || got[1] != 1977 {
		t.Errorf("got %v, want 1975 and 1977 in order", got)
	}
}
