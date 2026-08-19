package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
)

// onlyArticle is the single article the fixture issue assembles into, with the
// body and the front matter it currently has on disk.
func onlyArticle(t *testing.T, root string) (string, string, corpus.ArticleFront) {
	t.Helper()
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	key := corpus.IssueKey{Year: 1975, Number: "1"}
	names, err := c.Articles("ru", key)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 {
		t.Fatalf("the fixture assembled into %d articles, want 1", len(names))
	}
	path := filepath.Join(c.IssueDir("ru", key), "articles", names[0])
	var front corpus.ArticleFront
	body, err := corpus.LoadUnchecked(path, &front)
	if err != nil {
		t.Fatal(err)
	}
	return path, body, front
}

func TestAReassembleKeepsThePublishersTextWhenTheCacheHasNone(t *testing.T) {
	root := resplitFixture(t, []manifest.Row{
		{Title: "Равенства из спичек", Authors: "Иванов И. И.", Page: 1, Slug: "ravenstva"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}

	// Stand in for an article the publisher lane has already filled in. The
	// text is a placeholder and is not from the magazine.
	path, _, front := onlyArticle(t, root)
	front.Extraction = corpus.ExtractionPublisher
	typed := "Текст, который набрал издатель, со всеми ё и формулами на месте.\n"
	if err := corpus.Save(path, &front, typed); err != nil {
		t.Fatal(err)
	}

	// The same command again, with no publisher cache to read from, which is
	// what happens whenever assemble is pointed at a cache nobody has pulled
	// into. It used to overwrite the line above with a reading of the pages
	// and print nothing about having done it.
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	_, body, after := onlyArticle(t, root)
	if after.Extraction != corpus.ExtractionPublisher {
		t.Errorf("the article now says it was read by %q", after.Extraction)
	}
	if !strings.Contains(body, "набрал издатель") {
		t.Errorf("the publisher's text was replaced by:\n%s", body)
	}
}

func TestAReassembleStillCorrectsTheFrontMatterItKeepsTheTextOf(t *testing.T) {
	root := resplitFixture(t, []manifest.Row{
		{Title: "Равенства из спичек", Authors: "Иванов И. И.Cnk", Page: 1, Slug: "ravenstva"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	path, _, front := onlyArticle(t, root)
	front.Extraction = corpus.ExtractionPublisher
	if err := corpus.Save(path, &front, "Текст издателя.\n"); err != nil {
		t.Fatal(err)
	}

	// Keeping the body must not mean keeping the byline with it. Correcting a
	// byline is the whole reason anyone reassembles an issue they have already
	// built, so an article whose text is held back still has to pick the fix up.
	writeTOC(t, root, []manifest.Row{
		{Title: "Равенства из спичек", Authors: "Иванов И. И.", Page: 1, Slug: "ravenstva"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	_, body, after := onlyArticle(t, root)
	if len(after.Authors) != 1 || after.Authors[0] != "Иванов И. И." {
		t.Errorf("the byline is %v", after.Authors)
	}
	if !strings.Contains(body, "Текст издателя") {
		t.Errorf("the publisher's text went away:\n%s", body)
	}
}

// A tag is assigned once and then cited, so it has to survive the article being
// built again. Assembly rebuilds the front matter from scratch and has no way
// to mint one, so every reassemble used to drop the tag off every article it
// touched and the standing workaround was to remember to run tags assign after.
func TestAReassembleKeepsTheTagTheArticleWasGiven(t *testing.T) {
	root := resplitFixture(t, []manifest.Row{
		{Title: "Равенства из спичек", Authors: "Иванов И. И.Cnk", Page: 1, Slug: "ravenstva"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	path, body, front := onlyArticle(t, root)
	front.Tag = "8IO5"
	if err := corpus.Save(path, &front, body); err != nil {
		t.Fatal(err)
	}

	// Correcting a byline is the usual reason to reassemble, and it must not
	// cost the article its identifier.
	writeTOC(t, root, []manifest.Row{
		{Title: "Равенства из спичек", Authors: "Иванов И. И.", Page: 1, Slug: "ravenstva"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	_, _, after := onlyArticle(t, root)
	if after.Tag != "8IO5" {
		t.Errorf("the tag is now %q", after.Tag)
	}
	if len(after.Authors) != 1 || after.Authors[0] != "Иванов И. И." {
		t.Errorf("the byline is %v", after.Authors)
	}
}
