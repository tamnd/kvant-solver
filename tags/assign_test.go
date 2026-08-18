package tags_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/tags"
)

// corpusOf writes a corpus with the articles and problems named, one issue of
// 1975. The bodies are placeholders and are not text from the magazine.
func corpusOf(t *testing.T, articles []string, problems []string) *corpus.Corpus {
	t.Helper()
	root := t.TempDir()
	for i, slug := range articles {
		front := &corpus.ArticleFront{
			ID:         fmt.Sprintf("1975-1-%s", slug),
			Issue:      "kvant_1975_1",
			Year:       1975,
			Number:     "1",
			Title:      slug,
			PageFirst:  i*4 + 1,
			PageLast:   i*4 + 4,
			Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
		}
		path := filepath.Join(root, "content/ru/1975/01/articles", fmt.Sprintf("%02d_%s.md", i+1, slug))
		if err := corpus.Save(path, front, "Тело статьи.\n"); err != nil {
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
	c, err := corpus.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func tagOf(t *testing.T, c *corpus.Corpus, path string) corpus.Tag {
	t.Helper()
	var front corpus.ArticleFront
	if _, err := corpus.LoadUnchecked(filepath.Join(c.Root, path), &front); err != nil {
		t.Fatal(err)
	}
	return front.Tag
}

func TestAssignTagsEverythingAndWritesItDown(t *testing.T) {
	c := corpusOf(t, []string{"bronshteyn-ellips-d2e5763b", "metod-razmernostey-f319ab3d"}, []string{"M381", "F400"})
	store := open(t, c.Root)
	result, err := tags.Assign(c, "ru", store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Objects != 4 || result.Given != 4 || result.Written != 4 {
		t.Fatalf("first pass reports %s, want four of everything", result)
	}
	// The register and the front matter have to agree, because the front
	// matter is what somebody reading the file sees.
	tag := tagOf(t, c, "content/ru/1975/01/articles/01_bronshteyn-ellips-d2e5763b.md")
	if tag == "" {
		t.Fatal("the article carries no tag after assign")
	}
	if label, ok := store.Label(tag); !ok || label != "1975-1-bronshteyn-ellips-d2e5763b" {
		t.Errorf("%s names %q in the register, want the article identifier", tag, label)
	}

	problems, err := tags.Verify(c, "ru", store)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Errorf("a freshly tagged corpus does not verify: %v", problems)
	}
}

func TestAssignAgainChangesNothing(t *testing.T) {
	c := corpusOf(t, []string{"bronshteyn-ellips-d2e5763b"}, []string{"M381"})
	store := open(t, c.Root)
	if _, err := tags.Assign(c, "ru", store); err != nil {
		t.Fatal(err)
	}
	before := tagOf(t, c, "content/ru/1975/01/articles/01_bronshteyn-ellips-d2e5763b.md")

	// A fresh store, because this is what the next run of the command does.
	result, err := tags.Assign(c, "ru", open(t, c.Root))
	if err != nil {
		t.Fatal(err)
	}
	if result.Given != 0 || result.Written != 0 {
		t.Errorf("a second pass over an unchanged corpus reports %s", result)
	}
	if after := tagOf(t, c, "content/ru/1975/01/articles/01_bronshteyn-ellips-d2e5763b.md"); after != before {
		t.Errorf("the tag moved from %s to %s on a second pass", before, after)
	}
}

// This is the case the whole design is for. Assembling an issue writes its
// articles from the pages, so it throws the front matter away, tag and all. The
// register is not touched by that, and assign puts the tag back where it was.
func TestATagComesBackAfterTheFrontMatterIsRewritten(t *testing.T) {
	c := corpusOf(t, []string{"bronshteyn-ellips-d2e5763b"}, nil)
	store := open(t, c.Root)
	if _, err := tags.Assign(c, "ru", store); err != nil {
		t.Fatal(err)
	}
	path := "content/ru/1975/01/articles/01_bronshteyn-ellips-d2e5763b.md"
	before := tagOf(t, c, path)

	// What assemble does: the same file, written again from scratch.
	front := &corpus.ArticleFront{
		ID:         "1975-1-bronshteyn-ellips-d2e5763b",
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		Title:      "bronshteyn-ellips-d2e5763b",
		PageFirst:  1,
		PageLast:   4,
		Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
	}
	if err := corpus.Save(filepath.Join(c.Root, path), front, "Тело статьи, собранное заново.\n"); err != nil {
		t.Fatal(err)
	}

	// Which verify has to notice, otherwise a corpus can quietly lose its tags.
	problems, err := tags.Verify(c, "ru", open(t, c.Root))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("verify found %d problems after an article lost its tag, want one: %v", len(problems), problems)
	}

	if _, err := tags.Assign(c, "ru", open(t, c.Root)); err != nil {
		t.Fatal(err)
	}
	if after := tagOf(t, c, path); after != before {
		t.Errorf("the article came back with tag %q, had %s", after, before)
	}
}

func TestVerifyReportsAnObjectWithNoTag(t *testing.T) {
	c := corpusOf(t, []string{"bronshteyn-ellips-d2e5763b"}, nil)
	problems, err := tags.Verify(c, "ru", open(t, c.Root))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("verify found %d problems in an untagged corpus, want one: %v", len(problems), problems)
	}
	if !strings.Contains(problems[0].Detail, "no tag") {
		t.Errorf("the problem reads %q", problems[0])
	}
}

// A tag that names something no longer in the corpus is a citation that goes
// nowhere, and the fix is a recorded rename rather than a new tag.
func TestVerifyReportsATagThatNamesNothing(t *testing.T) {
	c := corpusOf(t, []string{"bronshteyn-ellips-d2e5763b"}, nil)
	store := open(t, c.Root)
	if _, err := tags.Assign(c, "ru", store); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Assign("1975-1-statya-kotoroy-net"); err != nil {
		t.Fatal(err)
	}
	problems, err := tags.Verify(c, "ru", store)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("verify found %d problems, want the one dangling tag: %v", len(problems), problems)
	}
	if problems[0].Label != "1975-1-statya-kotoroy-net" {
		t.Errorf("verify complained about %q", problems[0])
	}
}

func TestTwoFilesWithOneLabelCannotBeTagged(t *testing.T) {
	c := corpusOf(t, []string{"ellips-aaaaaaaa", "ellips-aaaaaaaa"}, nil)
	if _, err := tags.Assign(c, "ru", open(t, c.Root)); err == nil {
		t.Fatal("two articles with the same label were tagged anyway")
	}
}

// A register can be lost. The front matter cannot, because it is in the file
// the object is, so it is the fallback and a tag there is adopted rather than
// replaced.
func TestATagInTheFrontMatterIsAdoptedNotReplaced(t *testing.T) {
	c := corpusOf(t, []string{"bronshteyn-ellips-d2e5763b"}, nil)
	path := "content/ru/1975/01/articles/01_bronshteyn-ellips-d2e5763b.md"
	var front corpus.ArticleFront
	body, err := corpus.LoadUnchecked(filepath.Join(c.Root, path), &front)
	if err != nil {
		t.Fatal(err)
	}
	front.Tag = "0A3F"
	if err := corpus.Save(filepath.Join(c.Root, path), &front, body); err != nil {
		t.Fatal(err)
	}

	store := open(t, c.Root)
	if _, err := tags.Assign(c, "ru", store); err != nil {
		t.Fatal(err)
	}
	if got := tagOf(t, c, path); got != "0A3F" {
		t.Errorf("the article was given tag %s, it already had 0A3F", got)
	}
	if label, ok := store.Label("0A3F"); !ok || label != "1975-1-bronshteyn-ellips-d2e5763b" {
		t.Errorf("0A3F names %q in the register", label)
	}
}
