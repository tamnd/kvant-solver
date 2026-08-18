package corpus

import (
	"path/filepath"
	"strings"
	"testing"
)

// buildFixture writes the smallest corpus that is still a corpus: one issue of
// two pages, one article assembled out of them, and one problem posed in that
// issue and solved in a later one. The bodies are invented placeholders and are
// not text from the magazine.
func buildFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	pages := []struct {
		index int
		label string
		body  string
	}{
		{1, "1", "## Rubric banner\n\nFirst page of the fixture issue, standing in for a scanned page.\n"},
		{2, "2", "Second page of the fixture issue, where the fixture article ends.\n"},
	}
	for _, p := range pages {
		front := &PageFront{
			Issue:     "kvant_1975_1",
			Year:      1975,
			Number:    "1",
			PageIndex: p.index,
			PageLabel: p.label,
			Rubrics:   []string{"math-circle"},
			Articles:  []string{"1975-1-fixture-article"},
			Provenance: Provenance{
				Lang:       "ru",
				Source:     "fixture",
				Extraction: ExtractionVision,
			},
		}
		path := filepath.Join(root, "content/ru/1975/01/pages", front.Filename())
		if err := Save(path, front, p.body); err != nil {
			t.Fatal(err)
		}
	}

	article := &ArticleFront{
		ID:        "1975-1-fixture-article",
		Issue:     "kvant_1975_1",
		Year:      1975,
		Number:    "1",
		Title:     "Fixture article",
		Authors:   []string{"A. Author"},
		Rubric:    "math-circle",
		PageFirst: 1,
		PageLast:  2,
		Tag:       "0A3F",
		Provenance: Provenance{
			Lang:       "ru",
			Source:     "fixture",
			Extraction: ExtractionVision,
		},
	}
	if err := Save(filepath.Join(root, "content/ru/1975/01/articles/01_fixture-article.md"), article, "Assembled body of the fixture article.\n"); err != nil {
		t.Fatal(err)
	}

	problem := &ProblemFront{
		ID:                   "M1234",
		Subject:              Math,
		Tag:                  "0A3G",
		PosedIn:              "kvant_1975_1",
		PosedPages:           "56",
		SolvedIn:             "kvant_1975_5",
		SolvedPages:          "58-59",
		HasPublishedSolution: true,
		Provenance: Provenance{
			Lang:       "ru",
			Source:     "fixture",
			Extraction: ExtractionVision,
		},
	}
	id, err := problem.ProblemID()
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(root, "content/ru", id.Path()), problem, "Condition of the fixture problem.\n\n## Published solution\n\nSolution as the fixture prints it.\n"); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestValidateAcceptsAGoodCorpus(t *testing.T) {
	root := buildFixture(t)
	c, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() {
		for _, f := range rep.Findings {
			t.Errorf("%s: %v", f.Path, f.Err)
		}
		t.Fatal("fixture corpus did not validate")
	}
	if rep.Pages != 2 || rep.Articles != 1 || rep.Problems != 1 {
		t.Errorf("counted %s", rep)
	}
	if len(rep.Issues) != 1 || rep.Issues[0] != "kvant_1975_1" {
		t.Errorf("issues came back as %v", rep.Issues)
	}
}

func TestValidateCatchesAGapInThePages(t *testing.T) {
	root := buildFixture(t)
	front := &PageFront{
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		PageIndex:  9,
		Provenance: Provenance{Lang: "ru", Source: "fixture", Extraction: ExtractionVision},
	}
	if err := Save(filepath.Join(root, "content/ru/1975/01/pages/0009.md"), front, "A page with nothing before it.\n"); err != nil {
		t.Fatal(err)
	}
	c, _ := Open(root)
	rep, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("a gap between page 2 and page 9 passed validation")
	}
	if !strings.Contains(joinFindings(rep), "jump from 2 to 9") {
		t.Errorf("findings do not name the gap: %s", joinFindings(rep))
	}
}

func TestValidateCatchesAnArticleOffTheEndOfItsIssue(t *testing.T) {
	root := buildFixture(t)
	article := &ArticleFront{
		ID:         "1975-1-runaway",
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		Title:      "Runaway",
		PageFirst:  1,
		PageLast:   80,
		Provenance: Provenance{Lang: "ru", Source: "fixture", Extraction: ExtractionVision},
	}
	if err := Save(filepath.Join(root, "content/ru/1975/01/articles/02_runaway.md"), article, "Body.\n"); err != nil {
		t.Fatal(err)
	}
	c, _ := Open(root)
	rep, _ := c.Validate()
	if rep.OK() {
		t.Fatal("an article running past the end of its issue passed validation")
	}
	if !strings.Contains(joinFindings(rep), "the issue has 2 pages") {
		t.Errorf("findings do not name the overrun: %s", joinFindings(rep))
	}
}

func TestOpenRejectsSomethingThatIsNotACorpus(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Error("a directory with no content tree should not open as a corpus")
	}
	t.Setenv("KVANT_CORPUS", "")
	if _, err := Open(""); err == nil {
		t.Error("an empty path with no KVANT_CORPUS set should be an error")
	}
}

func TestKind(t *testing.T) {
	cases := map[string]string{
		"content/ru/1975/01/pages/0007.md":         "page",
		"content/ru/1975/01/articles/01_ellips.md": "article",
		"content/ru/problems/m/1234.md":            "problem",
		"content/vi/problems/f/0567.md":            "problem",
		"content/solutions/ru/problems/m/1234.md":  "solution",
		"content/ru/1975/01/issue.md":              "issue",
		"reports/audit.md":                         "",
		"content/README.md":                        "",
	}
	for path, want := range cases {
		if got := kind(path); got != want {
			t.Errorf("kind(%q) = %q, want %q", path, got, want)
		}
	}
}

func joinFindings(rep *Report) string {
	var b strings.Builder
	for _, f := range rep.Findings {
		b.WriteString(f.Path)
		b.WriteString(": ")
		b.WriteString(f.Err.Error())
		b.WriteString("\n")
	}
	return b.String()
}

func TestAMissingPageIsAGapAndNotADefect(t *testing.T) {
	// The distinction the CI check runs on. Thousands of sheets were refused by
	// the reading rules and every one leaves a hole. A check that failed on
	// those would fail every day there has ever been.
	root := buildFixture(t)
	front := &PageFront{
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		PageIndex:  9,
		Provenance: Provenance{Lang: "ru", Source: "fixture", Extraction: ExtractionVision},
	}
	if err := Save(filepath.Join(root, "content/ru/1975/01/pages/0009.md"), front, "A page with nothing before it.\n"); err != nil {
		t.Fatal(err)
	}
	c, _ := Open(root)
	rep, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if rep.OK() {
		t.Fatal("a gap between page 2 and page 9 passed validation")
	}
	if !rep.Sound() {
		t.Error("a corpus whose only complaint is an unread page is not sound")
	}
	if rep.Gaps() != len(rep.Findings) {
		t.Errorf("%d of %d findings are gaps, want all of them", rep.Gaps(), len(rep.Findings))
	}
}

func TestAnArticleOffTheEndOfItsIssueIsNotAGap(t *testing.T) {
	// The other side of it. This is a file disagreeing with another file, which
	// no amount of reading will fix, so it has to fail the check that gaps pass.
	root := buildFixture(t)
	article := &ArticleFront{
		ID:         "1975-1-runaway",
		Issue:      "kvant_1975_1",
		Year:       1975,
		Number:     "1",
		Title:      "Runaway",
		PageFirst:  1,
		PageLast:   80,
		Provenance: Provenance{Lang: "ru", Source: "fixture", Extraction: ExtractionVision},
	}
	if err := Save(filepath.Join(root, "content/ru/1975/01/articles/02_runaway.md"), article, "Body.\n"); err != nil {
		t.Fatal(err)
	}
	c, _ := Open(root)
	rep, _ := c.Validate()
	if rep.Sound() {
		t.Error("an article running past its issue was treated as an unread page")
	}
}

func TestAGoodCorpusIsSoundAndHasNoGaps(t *testing.T) {
	root := buildFixture(t)
	c, _ := Open(root)
	rep, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Sound() || rep.Gaps() != 0 {
		t.Errorf("sound %v with %d gaps on a corpus with nothing wrong", rep.Sound(), rep.Gaps())
	}
}
