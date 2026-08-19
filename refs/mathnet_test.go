package refs_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/refs"
	"github.com/tamnd/kvant-solver/source/mathnetru"
)

// mathnetCorpus writes a corpus holding one issue of three articles. The bodies
// are invented placeholders and none of them is text from the magazine.
func mathnetCorpus(t *testing.T) *corpus.Corpus {
	t.Helper()
	root := t.TempDir()
	articles := []struct {
		id, title string
		first     int
	}{
		{"2008-3-gazovyie-tsentrifugi", "Из истории газовых центрифуг", 2},
		{"2008-3-nanotehnologii", "Нанотехнологии: когда размер имеет значение", 6},
		{"2008-3-urok", "Урок близился к завершению …", 37},
	}
	for n, a := range articles {
		front := &corpus.ArticleFront{
			ID:        a.id,
			Issue:     "kvant_2008_3",
			Year:      2008,
			Number:    "3",
			Title:     a.title,
			PageFirst: a.first,
			PageLast:  a.first + 1,
			Provenance: corpus.Provenance{
				Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionNative,
			},
		}
		path := filepath.Join(root, "content/ru/2008/03/articles",
			string(rune('a'+n))+"_fixture.md")
		if err := corpus.Save(path, front, "Body of the fixture article.\n"); err != nil {
			t.Fatal(err)
		}
	}
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	tagAll(t, c)
	return c
}

func paper(id, title string, first, last int) mathnetru.PaperRef {
	return mathnetru.PaperRef{
		ID: id, URL: mathnetru.BaseURL + "/rus/" + id,
		Title: title, PageFirst: first, PageLast: last, FullText: true,
	}
}

func issue2008() []mathnetru.IssueRef {
	return []mathnetru.IssueRef{{Year: 2008, Number: "3", Query: 3}}
}

func concord(t *testing.T, c *corpus.Corpus, papers ...mathnetru.PaperRef) *refs.MathNet {
	t.Helper()
	idx, err := refs.LoadMathNet(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	return refs.BuildMathNet(issue2008(),
		map[string][]mathnetru.PaperRef{"kvant_2008_3": papers}, idx)
}

// row is the concordance row for one mathnet identifier.
func row(t *testing.T, m *refs.MathNet, id string) refs.MathNetPaper {
	t.Helper()
	for _, p := range m.Papers {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("%s is not in the concordance", id)
	return refs.MathNetPaper{}
}

// The title is what the two sides most reliably agree on, once the punctuation
// and the case the magazine sets it in are taken off. Our page numbering came
// out of a scan and it slips; the title does not.
func TestAnArticleIsFoundByItsTitle(t *testing.T) {
	c := mathnetCorpus(t)
	m := concord(t, c, paper("kvant2006", "НАНОТЕХНОЛОГИИ: когда размер имеет значение!", 99, 104))
	got := row(t, m, "kvant2006")
	if got.Status != refs.MathNetLinked || got.How != refs.ByTitle {
		t.Fatalf("%+v", got)
	}
	if got.To == "" || got.ToLabel != "2008-3-nanotehnologii" {
		t.Errorf("tag %q, label %q", got.To, got.ToLabel)
	}
	if m.Linked != 1 || m.Count != 1 {
		t.Errorf("linked %d of %d", m.Linked, m.Count)
	}
}

// Where the two sides wrote the title differently enough that folding it does
// not help, the printed page is what is left.
func TestAnArticleWhoseTitleDisagreesIsFoundByItsPrintedPage(t *testing.T) {
	c := mathnetCorpus(t)
	m := concord(t, c, paper("kvant2834", "Совершенно другое название", 37, 37))
	got := row(t, m, "kvant2834")
	if got.Status != refs.MathNetLinked || got.How != refs.ByPage {
		t.Fatalf("%+v", got)
	}
	if got.ToLabel != "2008-3-urok" {
		t.Errorf("label %q", got.ToLabel)
	}
}

// The title has to win. It reads as though the page were the stronger test,
// since both sides took it off the same printed issue, and it is not: a page
// number is one integer and it collides the moment either side's numbering
// slips by one.
func TestTheTitleIsPreferredToThePrintedPage(t *testing.T) {
	c := mathnetCorpus(t)
	m := concord(t, c, paper("kvant2822", "Из истории газовых центрифуг", 37, 40))
	got := row(t, m, "kvant2822")
	if got.How != refs.ByTitle || got.ToLabel != "2008-3-gazovyie-tsentrifugi" {
		t.Errorf("%+v", got)
	}
}

// The collision this ordering exists for, taken off 2008 issue 1. Our page
// numbering for that issue runs two ahead of mathnet's, so the obituary sits at
// their page 15 and our page 17, and their solutions column sits at their page
// 17. Matched a paper at a time with the page tried first, the solutions column
// takes the obituary out from under the title that really owned it and both
// papers end up claiming one article.
func TestAPageCollisionDoesNotStealAnArticleATitleAlreadyMatched(t *testing.T) {
	c := mathnetCorpus(t)
	m := concord(t, c,
		paper("kvant2834", "Урок близился к завершению …", 35, 35),
		paper("kvant2835", "Решения задач М2051-М2055", 37, 44),
	)
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	if got := row(t, m, "kvant2834"); got.How != refs.ByTitle || got.ToLabel != "2008-3-urok" {
		t.Errorf("the title match lost its article: %+v", got)
	}
	if got := row(t, m, "kvant2835"); got.Status != refs.MathNetUnmatched {
		t.Errorf("the solutions column was given an article anyway: %+v", got)
	}
}

// An issue nobody has read is not a failure of the matching, and counting it
// as one would make the linked rate a report on how much has been downloaded.
func TestAnArticleInAnIssueWeDoNotHaveIsNotAFailure(t *testing.T) {
	c := mathnetCorpus(t)
	idx, err := refs.LoadMathNet(c, "ru")
	if err != nil {
		t.Fatal(err)
	}
	m := refs.BuildMathNet(
		[]mathnetru.IssueRef{{Year: 2005, Number: "1", Query: 1}},
		map[string][]mathnetru.PaperRef{
			"kvant_2005_1": {paper("kvant100", "Статья из непрочитанного номера", 3, 7)},
		}, idx)
	got := row(t, m, "kvant100")
	if got.Status != refs.MathNetUnread {
		t.Errorf("status %q", got.Status)
	}
	if m.Linked != 0 {
		t.Errorf("linked %d", m.Linked)
	}
}

// An issue we hold in full where nothing lines up is the count worth watching,
// and it has to be told apart from an issue we never read.
func TestAnArticleWeShouldHaveAndCannotFindIsSaidToBeUnmatched(t *testing.T) {
	c := mathnetCorpus(t)
	m := concord(t, c, paper("kvant9999", "Статья которой у нас нет", 88, 90))
	if got := row(t, m, "kvant9999"); got.Status != refs.MathNetUnmatched {
		t.Errorf("status %q", got.Status)
	}
}

// An issue somebody has assembled three articles of, out of the twenty-nine
// mathnet lists, has not failed to match twenty-six times. Counting it that way
// would make the number a report on how much of the magazine has been
// assembled, which is what the coverage command is for.
func TestAnArticleInAnIssueOnlyPartlyAssembledIsNotAFailure(t *testing.T) {
	c := mathnetCorpus(t)
	papers := []mathnetru.PaperRef{paper("kvant2006", "Нанотехнологии: когда размер имеет значение", 6, 12)}
	for n := range 6 {
		papers = append(papers, paper(fmt.Sprintf("kvant90%d", n), fmt.Sprintf("Ненайденная статья %d", n), 50+n, 51+n))
	}
	m := concord(t, c, papers...)
	if got := row(t, m, "kvant900"); got.Status != refs.MathNetUnassembled {
		t.Errorf("status %q", got.Status)
	}
	if counts := m.Counts(); counts[refs.MathNetUnmatched] != 0 {
		t.Errorf("counts %v", counts)
	}
}

// A permanent link is exactly the kind of thing nobody rechecks once it is
// written down, so two of them landing on one file has to stop the build.
func TestTwoPapersClaimingOneArticleIsRefused(t *testing.T) {
	m := &refs.MathNet{Source: "x", Papers: []refs.MathNetPaper{
		{ID: "kvant1", Title: "a", Year: 2008, To: "0A3F"},
		{ID: "kvant2", Title: "b", Year: 2008, To: "0A3F"},
	}}
	if err := m.Check(); err == nil {
		t.Fatal("two papers were allowed to claim one article")
	}
}

func TestAConcordanceWithNoSourceIsRefused(t *testing.T) {
	m := &refs.MathNet{Papers: []refs.MathNetPaper{{ID: "kvant1", Title: "a", Year: 2008}}}
	if err := m.Check(); err == nil {
		t.Fatal("a concordance that does not say where it came from was accepted")
	}
}

// Mathnet starts at 2005 and a row from before it means the issue key was
// built wrong somewhere upstream.
func TestAPaperFromBeforeMathnetHoldsKvantIsRefused(t *testing.T) {
	m := &refs.MathNet{Source: "x", Papers: []refs.MathNetPaper{
		{ID: "kvant1", Title: "a", Year: 1975},
	}}
	if err := m.Check(); err == nil {
		t.Fatal("a paper from before the run was accepted")
	}
}

func TestAGoodConcordancePasses(t *testing.T) {
	c := mathnetCorpus(t)
	m := concord(t, c,
		paper("kvant2822", "Из истории газовых центрифуг", 2, 5),
		paper("kvant2006", "Нанотехнологии: когда размер имеет значение", 6, 12),
		paper("kvant2834", "Урок близился к завершению …", 37, 37),
	)
	if err := m.Check(); err != nil {
		t.Fatal(err)
	}
	if m.Linked != 3 {
		t.Errorf("linked %d of %d", m.Linked, m.Count)
	}
	if years := m.Years(); len(years) != 1 || years[0] != 2008 {
		t.Errorf("years %v", years)
	}
	if counts := m.Counts(); counts[refs.MathNetLinked] != 3 {
		t.Errorf("counts %v", counts)
	}
}
