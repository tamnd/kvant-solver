package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/tags"
)

// resplitFixture writes a corpus with one issue of twelve sheets and the
// manifests assemble needs, so that the assemble command can be run against it
// for real. The page bodies are placeholders and are not text from the
// magazine.
//
// The first two sheets are the covers and carry no printed number, which is
// what every issue of this magazine looks like: sheet 3 prints page 1.
func resplitFixture(t *testing.T, rows []manifest.Row) string {
	t.Helper()
	root := t.TempDir()
	for sheet := 1; sheet <= 12; sheet++ {
		front := &corpus.PageFront{
			Issue:      "kvant_1975_1",
			Year:       1975,
			Number:     "1",
			PageIndex:  sheet,
			Provenance: corpus.Provenance{Lang: "ru", Source: "kvant_digital", Extraction: corpus.ExtractionVision},
		}
		if sheet > 2 {
			front.PageLabel = fmt.Sprint(sheet - 2)
		}
		body := fmt.Sprintf("Текст страницы %d, достаточно длинный, чтобы что-то значить.\n", sheet)
		if err := corpus.Save(filepath.Join(root, "content/ru/1975/01/pages", front.Filename()), front, body); err != nil {
			t.Fatal(err)
		}
	}
	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	issues := &manifest.Issues{
		Years: 1, Count: 1,
		Issues: []manifest.Issue{{
			Key: "kvant_1975_1", Year: 1975, Number: "1", Dir: "1975/01",
			Sheets:  12,
			Sources: manifest.Sources{Digital: &manifest.Digital{URL: "https://example.invalid/kvant_1975_1"}},
		}},
	}
	if err := store.Write(manifest.IssuesFile, issues); err != nil {
		t.Fatal(err)
	}
	writeTOC(t, root, rows)
	return root
}

func writeTOC(t *testing.T, root string, rows []manifest.Row) {
	t.Helper()
	store, err := manifest.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	toc := &manifest.TOC{}
	toc.Set("kvant_1975_1", rows)
	if err := store.Write(manifest.TOCFile, toc); err != nil {
		t.Fatal(err)
	}
}

// articleTags is every article in the fixture issue, by the label the register
// knows it as.
func articleTags(t *testing.T, root string) map[string]corpus.Tag {
	t.Helper()
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	names, err := c.Articles("ru", corpus.IssueKey{Year: 1975, Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]corpus.Tag{}
	for _, name := range names {
		var front corpus.ArticleFront
		path := filepath.Join(c.IssueDir("ru", corpus.IssueKey{Year: 1975, Number: "1"}), "articles", name)
		if _, err := corpus.LoadUnchecked(path, &front); err != nil {
			t.Fatal(err)
		}
		out[tags.Label(front.ID)] = front.Tag
	}
	return out
}

// TestATagSurvivesAForcedResplit is the test the milestone asks for by name.
//
// The whole claim of a tag is that it outlives the things that move. The most
// violent thing that happens to an article is a resplit: the contents are
// corrected, assemble throws every article in the issue away and builds a
// different set of them from the same pages, and the files that come back have
// different names, different page ranges and no tag in their front matter. An
// article that is still the same article has to come back with the same tag,
// and one that is genuinely new has to get a new one.
func TestATagSurvivesAForcedResplit(t *testing.T) {
	root := resplitFixture(t, []manifest.Row{
		{Title: "Об одной задаче Гаусса", Authors: "В. Тихомиров", Page: 1, Slug: "ob-odnoy-zadache-gaussa-11111111", Source: "kvant_digital"},
		{Title: "Задачник «Кванта»", Page: 4, Slug: "zadachnik-kvanta-22222222", Source: "kvant_digital"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"tags", "assign", "--corpus", root}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"tags", "verify", "--corpus", root}); err != nil {
		t.Fatalf("the corpus does not verify straight after assign: %v", err)
	}
	before := articleTags(t, root)
	if len(before) != 2 {
		t.Fatalf("the fixture assembled into %d articles, want 2", len(before))
	}

	// The resplit. The contents were wrong: the chess page at printed page 8
	// was inside the problems column and is its own item, so the second article
	// now ends where the third begins.
	writeTOC(t, root, []manifest.Row{
		{Title: "Об одной задаче Гаусса", Authors: "В. Тихомиров", Page: 1, Slug: "ob-odnoy-zadache-gaussa-11111111", Source: "kvant_digital"},
		{Title: "Задачник «Кванта»", Page: 4, Slug: "zadachnik-kvanta-22222222", Source: "kvant_digital"},
		{Title: "Шахматная страничка", Page: 8, Slug: "shahmatnaya-stranichka-33333333", Source: "kvant_digital"},
	})
	if err := run([]string{"assemble", "--corpus", root, "--issue", "kvant_1975_1"}); err != nil {
		t.Fatal(err)
	}

	// Before the tags are put back, the two articles that survived have lost
	// the copy in their front matter, and verify is the thing that has to say
	// so rather than let a corpus quietly shed its tags.
	if err := run([]string{"tags", "verify", "--corpus", root}); err == nil {
		t.Fatal("verify passed on a corpus whose articles were rewritten without their tags")
	}

	if err := run([]string{"tags", "assign", "--corpus", root}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"tags", "verify", "--corpus", root}); err != nil {
		t.Fatalf("the corpus does not verify after the resplit: %v", err)
	}

	after := articleTags(t, root)
	if len(after) != 3 {
		t.Fatalf("the resplit produced %d articles, want 3", len(after))
	}
	for label, tag := range before {
		got, ok := after[label]
		if !ok {
			t.Errorf("%s is not in the issue any more", label)
			continue
		}
		if got != tag {
			t.Errorf("%s came back as %s, had %s", label, got, tag)
		}
	}
	if after["1975-1-shahmatnaya-stranichka-33333333"] == "" {
		t.Error("the article the resplit created has no tag")
	}

	// And the register is still the single answer for each of them, which is
	// what a citation written before the resplit depends on.
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := tags.Open(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	for label, tag := range before {
		gotTag, gotLabel, ok := store.Resolve(string(tag))
		if !ok {
			t.Errorf("%s resolves to nothing after the resplit", tag)
			continue
		}
		if gotTag != tag || gotLabel != label {
			t.Errorf("%s resolves to %s (%s), want %s", tag, gotTag, gotLabel, label)
		}
	}
}

func TestTagsVerifyFailsOnAnUntaggedCorpus(t *testing.T) {
	root := fixture(t)
	if err := run([]string{"tags", "verify", "--corpus", root}); err == nil {
		t.Fatal("verify passed on a corpus with no register at all")
	}
}

func TestTagsResolveFindsAnObjectByEitherName(t *testing.T) {
	root := fixture(t)
	if err := run([]string{"tags", "assign", "--corpus", root}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"tags", "resolve", "--corpus", root, "1975-1-fixture-article"}); err != nil {
		t.Fatalf("the label of the only article resolved to nothing: %v", err)
	}
	if err := run([]string{"tags", "resolve", "--corpus", root, "1975-1-nothing-here"}); err == nil {
		t.Fatal("resolving something that was never tagged should be an error")
	}
}
