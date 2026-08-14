package assemble_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/assemble"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
)

func key(t *testing.T) corpus.IssueKey {
	t.Helper()
	k, err := corpus.NewIssueKey(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// issue is a scan with two unnumbered covers at the front, which is what every
// issue of this magazine looks like: sheet 3 prints page 1.
func issue(t *testing.T, sheets int) []assemble.Page {
	t.Helper()
	pages := make([]assemble.Page, 0, sheets)
	for i := 1; i <= sheets; i++ {
		page := assemble.Page{Index: i, Body: fmt.Sprintf("Текст страницы %d, достаточно длинный, чтобы что-то значить.", i)}
		if i > 2 {
			page.Label = fmt.Sprint(i - 2)
		}
		pages = append(pages, page)
	}
	return pages
}

func TestAnIssueSplitsAtTheNextArticle(t *testing.T) {
	rows := []manifest.Row{
		{Title: "Об одной задаче Гаусса", Authors: "В. Тихомиров", Page: 2, Rubric: "Математический кружок", Source: "kvant_mccme"},
		{Title: "Задачи М381-М385", Page: 5, Rubric: "Задачник «Кванта»", Source: "kvant_mccme"},
		{Title: "Шахматная страничка", Page: 8, Source: "kvant_mccme"},
	}
	got := assemble.Issue(key(t), rows, issue(t, 12))
	if len(got.Articles) != 3 {
		t.Fatalf("assembled %d articles, want 3", len(got.Articles))
	}
	want := []struct {
		slug   string
		first  int
		last   int
		labels string
	}{
		{"ob-odnoy-zadache-gaussa", 4, 6, "2-4"},
		{"zadachi-m381-m385", 7, 9, "5-7"},
		{"shahmatnaya-stranichka", 10, 12, "8-10"},
	}
	for i, w := range want {
		article := got.Articles[i]
		switch {
		case article.Slug != w.slug:
			t.Errorf("article %d is %q, want %q", i, article.Slug, w.slug)
		case article.First != w.first || article.Last != w.last:
			t.Errorf("%s covers sheets %d-%d, want %d-%d", w.slug, article.First, article.Last, w.first, w.last)
		case article.Labels != w.labels:
			t.Errorf("%s prints pages %q, want %q", w.slug, article.Labels, w.labels)
		}
	}
}

// The covers carry no article, and that is expected rather than a defect. The
// page files still hold them.
func TestTheCoversAreOrphans(t *testing.T) {
	rows := []manifest.Row{{Title: "Об одной задаче Гаусса", Page: 1, Source: "kvant_mccme"}}
	got := assemble.Issue(key(t), rows, issue(t, 6))
	if len(got.Orphans) != 2 || got.Orphans[0] != 1 || got.Orphans[1] != 2 {
		t.Fatalf("orphans are %v, want the two cover sheets", got.Orphans)
	}
}

// The rubric goes in as a slug, so that the index joins on one name however the
// banner was printed that month.
func TestTheRubricIsCanonicalised(t *testing.T) {
	rows := []manifest.Row{{Title: "Задачи", Page: 1, Rubric: "ЗАДАЧНИК «КВАНТА»", Source: "kvant_mccme"}}
	got := assemble.Issue(key(t), rows, issue(t, 4))
	if len(got.Articles) != 1 {
		t.Fatalf("assembled %d articles, want 1", len(got.Articles))
	}
	if got.Articles[0].Rubric != "zadachnik-kvanta" {
		t.Fatalf("rubric is %q, want zadachnik-kvanta", got.Articles[0].Rubric)
	}
}

// An article interrupted by a full page advert is not contiguous. The contents
// gives the real page list, and a range would claim the advert as part of the
// article.
func TestAnInterruptedArticleKeepsItsGaps(t *testing.T) {
	rows := []manifest.Row{
		{Title: "Длинная статья", Page: 1, Pages: []int{1, 2, 4}, Source: "kvant_mccme"},
		{Title: "Следующая", Page: 5, Source: "kvant_mccme"},
	}
	got := assemble.Issue(key(t), rows, issue(t, 9))
	first := got.Articles[0]
	if len(first.Pages) != 3 || first.Pages[2] != 6 {
		t.Fatalf("pages are %v, want sheets 3, 4 and 6", first.Pages)
	}
	if first.Labels != "1, 2, 4" {
		t.Fatalf("labels are %q, want the list and not a range", first.Labels)
	}
	if strings.Contains(first.Body, "страницы 5,") {
		t.Error("the advert page was pulled into the article body")
	}
}

// A rubric banner partway through an article is the pages disagreeing with the
// contents about where the article stops, and it is the one thing that cannot
// be checked once the pages have been merged away.
func TestABannerInsideAnArticleIsReported(t *testing.T) {
	pages := issue(t, 8)
	pages[5].Body = "⟦rubric⟧ Шахматная страничка\n\n" + pages[5].Body
	rows := []manifest.Row{{Title: "Длинная статья", Page: 1, Source: "kvant_mccme"}}
	got := assemble.Issue(key(t), rows, pages)
	found := false
	for _, note := range got.Notes {
		if note.Kind == "banner_inside" {
			found = true
			if !strings.Contains(note.Detail, "Шахматная") {
				t.Errorf("the note does not name the banner: %s", note.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("notes are %+v, want one about the banner on sheet 6", got.Notes)
	}
}

// A row with no page cannot be placed, and guessing one would move every
// article after it. It is reported and skipped.
func TestARowWithNoPageIsReportedNotGuessed(t *testing.T) {
	rows := []manifest.Row{
		{Title: "Первая", Page: 1, Source: "kvant_mccme"},
		{Title: "Без страницы", Source: "mathnet_ru"},
	}
	got := assemble.Issue(key(t), rows, issue(t, 6))
	if len(got.Articles) != 1 {
		t.Fatalf("assembled %d articles, want only the placed one", len(got.Articles))
	}
	found := false
	for _, note := range got.Notes {
		if note.Kind == "unplaced" && note.Subject == "Без страницы" {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes are %+v, want one about the unplaced row", got.Notes)
	}
}

// The order is the printed contents and never the page order. This magazine
// lists the answers column last and prints it in the middle, and sorting by
// page would silently reorder the issue.
func TestTheOrderIsTheContentsAndNotThePages(t *testing.T) {
	rows := []manifest.Row{
		{Title: "Первая статья", Page: 1, Source: "kvant_mccme"},
		{Title: "Вторая статья", Page: 6, Source: "kvant_mccme"},
		{Title: "Ответы и решения", Page: 4, Rubric: "Ответы, указания, решения", Source: "kvant_mccme"},
	}
	got := assemble.Issue(key(t), rows, issue(t, 10))
	if len(got.Articles) != 3 {
		t.Fatalf("assembled %d articles, want 3", len(got.Articles))
	}
	if got.Articles[2].Title != "Ответы и решения" {
		t.Fatalf("the third article is %q, want the answers column the contents lists last", got.Articles[2].Title)
	}
	if got.Articles[2].Ordinal != 3 {
		t.Errorf("the answers column has ordinal %d, want 3", got.Articles[2].Ordinal)
	}
	// It is listed last and printed in the middle, so it must not run to the end
	// of the issue: the article that follows it in print stops it.
	if got.Articles[2].Last >= got.Articles[1].First {
		t.Errorf("the answers column runs to sheet %d, past the article printed after it", got.Articles[2].Last)
	}
}

// A row the contents gives no printed number for is placed by its sheet, and
// the sheet is the mirror's numbering, which counts from zero. The cover
// captions and the untitled fillers are all of this shape.
func TestASheetNumberIsTranslatedOutOfTheMirrorsNumbering(t *testing.T) {
	rows := []manifest.Row{{Title: "Обложка", Sheet: 1, Source: "kvant_digital"}}
	got := assemble.Issue(key(t), rows, issue(t, 4))
	if got.Articles[0].First != 2 {
		t.Fatalf("the row was placed at page %d, want 2, which is the mirror's sheet 1", got.Articles[0].First)
	}
}

// The printed number beats the sheet where the row has both, and this is the
// case that made it matter. A row printed on page 2 of 1975 №1 carries sheet 3,
// and sheet 3 in the mirror's numbering is the page file 0004. Believing the
// sheet as written put the article one page early and cost it its last page,
// which happened to eleven pages of the first issue assembled.
func TestThePrintedNumberBeatsTheSheet(t *testing.T) {
	rows := []manifest.Row{{Title: "Эллипс", Page: 2, Sheet: 3, Source: "kvant_digital"}}
	got := assemble.Issue(key(t), rows, issue(t, 6))
	if got.Articles[0].First != 4 {
		t.Fatalf("the row was placed at page %d, want the page that prints 2", got.Articles[0].First)
	}
}

// An issue that came back with no folios anywhere cannot be placed at all, and
// saying so is better than assembling something that looks right.
func TestAnIssueWithNoFoliosIsRefused(t *testing.T) {
	pages := []assemble.Page{{Index: 1, Body: "Текст"}, {Index: 2, Body: "Текст"}}
	rows := []manifest.Row{{Title: "Статья", Page: 1, Source: "kvant_mccme"}}
	got := assemble.Issue(key(t), rows, pages)
	if len(got.Articles) != 0 {
		t.Fatal("articles were assembled from pages that print no numbers")
	}
	if len(got.Notes) != 1 || got.Notes[0].Kind != "no_folios" {
		t.Fatalf("notes are %+v, want one saying the issue prints no numbers", got.Notes)
	}
}

func TestAuthorsAreSplit(t *testing.T) {
	rows := []manifest.Row{{Title: "Статья", Page: 1, Authors: "В. Тихомиров, А. Колмогоров", Source: "kvant_mccme"}}
	got := assemble.Issue(key(t), rows, issue(t, 4))
	authors := got.Articles[0].Authors
	if len(authors) != 2 || authors[0] != "В. Тихомиров" || authors[1] != "А. Колмогоров" {
		t.Fatalf("authors are %q, want the two names", authors)
	}
}

// The publisher's slug is used where the mirror gave one, because joining on it
// later is free.
func TestThePublisherSlugWins(t *testing.T) {
	rows := []manifest.Row{{Title: "Об одной задаче Гаусса", Page: 1, Slug: "ob-odnoy-zadache", Source: "kvant_mccme"}}
	got := assemble.Issue(key(t), rows, issue(t, 4))
	if got.Articles[0].Slug != "ob-odnoy-zadache" {
		t.Fatalf("slug is %q, want the publisher's", got.Articles[0].Slug)
	}
}
