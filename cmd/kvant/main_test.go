package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
)

// fixture writes a corpus of one issue, two pages and one article. The bodies
// are invented placeholders and are not text from the magazine.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for i, body := range []string{
		"First page of the fixture issue.\n",
		"Second page of the fixture issue.\n",
	} {
		front := &corpus.PageFront{
			Issue:      "kvant_1975_1",
			Year:       1975,
			Number:     "1",
			PageIndex:  i + 1,
			Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
		}
		if err := corpus.Save(filepath.Join(root, "content/ru/1975/01/pages", front.Filename()), front, body); err != nil {
			t.Fatal(err)
		}
	}
	article := &corpus.ArticleFront{
		ID:         "1975-1-fixture-article",
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		Title:      "Fixture article",
		PageFirst:  1,
		PageLast:   2,
		Tag:        "0A3F",
		Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
	}
	if err := corpus.Save(filepath.Join(root, "content/ru/1975/01/articles/01_fixture-article.md"), article, "Assembled body.\n"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCorpusValidateOnAFixture(t *testing.T) {
	root := fixture(t)
	if err := run([]string{"corpus", "validate", "--corpus", root, "--quiet"}); err != nil {
		t.Fatalf("fixture corpus did not validate: %v", err)
	}
}

func TestCorpusValidateFailsOnABrokenCorpus(t *testing.T) {
	root := fixture(t)
	front := &corpus.PageFront{
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		PageIndex:  9,
		Provenance: corpus.Provenance{Lang: "ru", Source: "fixture", Extraction: corpus.ExtractionVision},
	}
	if err := corpus.Save(filepath.Join(root, "content/ru/1975/01/pages/0009.md"), front, "A page with a hole in front of it.\n"); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"corpus", "validate", "--corpus", root, "--quiet"})
	if err == nil {
		t.Fatal("validate passed on a corpus with a gap in its pages")
	}
	if !strings.Contains(err.Error(), "does not validate") {
		t.Errorf("error reads %q", err)
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := run([]string{"nonsense"}); err == nil {
		t.Fatal("an unknown command should be an error")
	}
	if err := run([]string{"corpus"}); err == nil {
		t.Fatal("corpus with no subcommand should be an error")
	}
}

func TestHelpAndVersionDoNotFail(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"--help"}, {"version"}} {
		if err := run(args); err != nil {
			t.Errorf("run(%v): %v", args, err)
		}
	}
}
