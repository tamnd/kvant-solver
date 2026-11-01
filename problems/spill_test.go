package problems

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

func issueArticle(path, title, rubric, body string, spill bool) Article {
	return Article{
		Path: path,
		Front: corpus.ArticleFront{
			Issue: "kvant_1971_11", Year: 1971, Number: "11",
			Title: title, Rubric: rubric,
		},
		Body:  body,
		Spill: spill,
	}
}

func TestProblemsOnTheNextArticlesPageAreFound(t *testing.T) {
	// 1971 №11: the Задачи spread ends on the page that opens «Найдите
	// ошибку», and assembly gave that whole page to the puzzle. Ф120—Ф122 were
	// in the corpus under the wrong title and the gap list called them unread.
	res := Build([]Article{
		issueArticle("content/ru/1971/11/articles/06_zadachi.md", "Задачи М111—М112; Ф118—Ф122", Rubric,
			"⟦folio 24⟧\n\nМ111. Первая.\n\nМ112. Вторая.\n\nФ118. Третья.\n\nФ119. Свет дойдет", false),
		issueArticle("content/ru/1971/11/articles/07_naydite.md", "Найдите ошибку", "smes",
			"⟦folio none⟧\n\nдо экрана?\n\nФ120. Вода в цилиндре.\n\nФ121. Полусферы.\n\nФ122. Шарик в стакане.\n\nНайдите ошибку\n\nМ5. Не задача, а пункт рассуждения.", true),
	})
	have := map[string]Entry{}
	for _, e := range res.Manifest.Entries {
		have[e.ID] = e
	}
	for _, id := range []string{"F120", "F121", "F122"} {
		e, ok := have[id]
		if !ok || e.Posed == nil {
			t.Fatalf("%s not recovered from the next article: %+v", id, res.Manifest.Entries)
		}
	}
	if _, ok := have["M5"]; ok {
		t.Fatal("a number nobody promised was taken from an ordinary article")
	}
	for _, d := range res.Documents {
		if d.Front.ID == "F122" && strings.Contains(d.Question, "Найдите") {
			t.Fatalf("F122 ran into the next article: %q", d.Question)
		}
	}
	for _, g := range res.Gaps {
		if strings.HasPrefix(g.Reason, "the heading lists it") {
			t.Fatalf("still reported missing: %+v", g)
		}
	}
}

func TestABracketedHeadingIsStillTheZadachnik(t *testing.T) {
	if kind, ok := KindOf("[Задачи М11—М15; Ф12—Ф17]"); !ok || kind != Posed {
		t.Fatalf("KindOf(bracketed) = %q, %v", kind, ok)
	}
	if kind, ok := KindOf("[Решения задач М1—М5]"); !ok || kind != Solved {
		t.Fatalf("KindOf(bracketed solutions) = %q, %v", kind, ok)
	}
}

func TestIssuesSortByNumber(t *testing.T) {
	if !(issueOrder("3") < issueOrder("11") && issueOrder("5-6") < issueOrder("7")) {
		t.Fatal("issue labels sort as text")
	}
}
