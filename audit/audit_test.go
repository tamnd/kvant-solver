package audit_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/audit"
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

// clean is an issue that should pass: four sheets, two covers, one article over
// the two body pages, and a contents that lists it.
func clean(t *testing.T) audit.Input {
	t.Helper()
	k := key(t)
	pages := make([]audit.Page, 0, 4)
	for i := 1; i <= 4; i++ {
		body := "⟦folio none⟧\n\nОбложка."
		label := ""
		if i > 2 {
			label = fmt.Sprint(i - 2)
			body = fmt.Sprintf("⟦folio %d⟧\n\nТекст страницы с формулой $x^2+1$ и словами.", i-2)
		}
		pages = append(pages, audit.Page{
			Index: i,
			Front: corpus.PageFront{Issue: k.String(), PageIndex: i, PageLabel: label},
			Body:  body,
		})
	}
	return audit.Input{
		Key:    k,
		Sheets: 4,
		Rows:   []manifest.Row{{Title: "Об одной задаче Гаусса", Page: 1, Source: "kvant_mccme"}},
		Pages:  pages,
		Articles: []audit.Article{{
			Front: corpus.ArticleFront{
				ID: "kvant_1975_1/ob-odnoy-zadache-gaussa", Issue: k.String(),
				Title: "Об одной задаче Гаусса", PageFirst: 3, PageLast: 4,
			},
			Body: "Текст статьи.",
		}},
	}
}

func TestACleanIssuePasses(t *testing.T) {
	report := audit.Issue(clean(t))
	fails := 0
	for _, finding := range report.Findings {
		if finding.Level == audit.Fail {
			t.Errorf("unexpected failure: %s", finding)
			fails++
		}
	}
	if !report.OK() {
		t.Fatalf("the issue did not pass with %d failures", fails)
	}
	// The covers are orphans and that is expected, so they warn and do not fail.
	warns := 0
	for _, finding := range report.Findings {
		if finding.Rule == "orphan_page" {
			warns++
		}
	}
	if warns != 2 {
		t.Errorf("got %d orphan warnings, want the two covers", warns)
	}
}

// The rule the milestone is written around. Seventy nine pages of eighty looks
// exactly like eighty until something counts them.
func TestAMissingPageFailsTheIssue(t *testing.T) {
	in := clean(t)
	in.Pages = in.Pages[:3]
	report := audit.Issue(in)
	if report.OK() {
		t.Fatal("an issue missing a page passed")
	}
	found := false
	for _, finding := range report.Findings {
		if finding.Rule == "missing_pages" {
			found = true
			if !strings.Contains(finding.Detail, "1 of 4") {
				t.Errorf("the detail does not say how many are missing: %s", finding.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("findings are %v, want one about the missing sheet", report.Findings)
	}
}

// An article the contents lists and the corpus does not have is the failure no
// page rule can see: eighty good pages with one article lost is eighty good
// pages.
func TestAnArticleTheContentsListsMustExist(t *testing.T) {
	in := clean(t)
	in.Rows = append(in.Rows, manifest.Row{Title: "Шахматная страничка", Page: 4, Source: "kvant_mccme"})
	report := audit.Issue(in)
	if report.OK() {
		t.Fatal("an issue missing an article from its own contents passed")
	}
	for _, finding := range report.Findings {
		if finding.Rule == "missing_article" && strings.Contains(finding.Detail, "Шахматная") {
			return
		}
	}
	t.Fatalf("findings are %v, want one naming the lost article", report.Findings)
}

// The two mirrors type the same title with different punctuation, so the
// comparison folds it away. Otherwise the audit fails every issue.
func TestATitleMatchesThroughPunctuation(t *testing.T) {
	in := clean(t)
	in.Rows[0].Title = "ОБ ОДНОЙ ЗАДАЧЕ ГАУССА!"
	if !audit.Issue(in).OK() {
		t.Fatal("the same title typed differently was read as a missing article")
	}
}

// A page that answered neither a number nor none leaves a hole in the page map,
// and assemble places every article with that map.
func TestAPageWithNoFolioLineFails(t *testing.T) {
	in := clean(t)
	in.Pages[2].Body = "Текст страницы без всякой пометки."
	report := audit.Issue(in)
	for _, finding := range report.Findings {
		if finding.Rule == "no_folio" {
			return
		}
	}
	t.Fatalf("findings are %v, want one about the missing folio line", report.Findings)
}

// Two pages printing the same number is a misread digit, and every article
// placed by that number lands in the wrong place.
func TestTwoPagesCannotPrintTheSameNumber(t *testing.T) {
	in := clean(t)
	in.Pages[3].Front.PageLabel = "1"
	report := audit.Issue(in)
	if report.OK() {
		t.Fatal("two pages printing page 1 passed")
	}
	for _, finding := range report.Findings {
		if finding.Rule == "duplicate_folio" {
			return
		}
	}
	t.Fatalf("findings are %v, want one about the repeated page number", report.Findings)
}

// A formula that does not compile is a page the site cannot render, and the
// only cheap moment to find out is now.
func TestABrokenFormulaFails(t *testing.T) {
	in := clean(t)
	in.Pages[2].Body = "⟦folio 1⟧\n\nФормула $\\frac{1}{$ не разбирается."
	in.LaTeX = checker{}
	report := audit.Issue(in)
	for _, finding := range report.Findings {
		if finding.Rule == "latex" || finding.Rule == "unclosed_math" {
			return
		}
	}
	t.Fatalf("findings are %v, want one about the formula", report.Findings)
}

// An article claiming a page that was never read is a broken link in the corpus.
func TestAnArticleCannotClaimAPageThatIsNotThere(t *testing.T) {
	in := clean(t)
	in.Articles[0].Front.PageLast = 9
	report := audit.Issue(in)
	if report.OK() {
		t.Fatal("an article claiming a page that does not exist passed")
	}
	for _, finding := range report.Findings {
		if finding.Rule == "missing_source_page" {
			return
		}
	}
	t.Fatalf("findings are %v, want one about the page the article claims", report.Findings)
}

// An issue whose length nobody knows cannot be declared complete, and saying so
// is better than passing it.
func TestAnIssueWithNoSheetCountWarns(t *testing.T) {
	in := clean(t)
	in.Sheets = 0
	report := audit.Issue(in)
	for _, finding := range report.Findings {
		if finding.Rule == "sheet_count" {
			return
		}
	}
	t.Fatalf("findings are %v, want one about the unknown sheet count", report.Findings)
}

// A page read by an older prompt is not wrong, it is stale, and the difference
// between the two prompts is usually why a rule started firing.
func TestAStalePromptWarnsAndDoesNotFail(t *testing.T) {
	in := clean(t)
	in.Prompts = []string{strings.Repeat("b", 64)}
	in.Pages[2].Front.PromptSHA256 = strings.Repeat("a", 64)
	report := audit.Issue(in)
	if !report.OK() {
		t.Fatal("a stale page failed the issue rather than warning")
	}
	for _, finding := range report.Findings {
		if finding.Rule == "stale_prompt" {
			return
		}
	}
	t.Fatalf("findings are %v, want one about the older prompt", report.Findings)
}

func TestTheSummaryCountsByRule(t *testing.T) {
	report := audit.Issue(clean(t))
	if got := report.Counts(); !strings.Contains(got, "orphan_page 2") {
		t.Fatalf("counts are %q, want the two orphan covers in them", got)
	}
}

// checker refuses anything with an unbalanced brace, which is enough to stand
// in for KaTeX without pulling a JavaScript engine into this test.
type checker struct{}

func (checker) Check(fragment string, _ bool) error {
	if strings.Count(fragment, "{") != strings.Count(fragment, "}") {
		return fmt.Errorf("unbalanced braces")
	}
	return nil
}

// The rule that does not trust the front matter, because the front matter and
// the path were written from the same value and agreed with each other while
// both named the wrong issue.
func TestAPageNamingAYearTheIssuePredatesIsReported(t *testing.T) {
	in := clean(t)
	in.Pages[0].Body = "⟦folio 1⟧\n\nНапечатано в 1983 году, оглавление за год.\n"
	report := audit.Issue(in)
	var found *audit.Finding
	for i, finding := range report.Findings {
		if finding.Rule == "anachronism" {
			found = &report.Findings[i]
		}
	}
	if found == nil {
		t.Fatalf("a 1975 page dated 1983 passed: %s", report)
	}
	if found.Level != audit.Warn {
		t.Errorf("the finding is a %s, want a warning at the rate real pages trip it", found.Level)
	}
	if !strings.Contains(found.Detail, "1983") {
		t.Errorf("the finding does not say which year: %s", found.Detail)
	}
}

// A December issue prints next year's dates, and the magazine writes about the
// year 2000. Neither is a page in the wrong place, and a rule that says they
// are is a rule people learn to skip past.
func TestTheYearsAnIssueIsAllowedToNameArePassed(t *testing.T) {
	for _, body := range []string{
		"⟦folio 1⟧\n\nПриём заявок на олимпиаду 1976 года открыт.\n",
		"⟦folio 1⟧\n\nК 2000 году эта задача будет решена.\n",
		"⟦folio 1⟧\n\nВ 1917 году всё изменилось.\n",
		"⟦folio 1⟧\n\nЗадача М1983 о вписанной окружности.\n",
	} {
		in := clean(t)
		in.Pages[0].Body = body
		for _, finding := range audit.Issue(in).Findings {
			if finding.Rule == "anachronism" {
				t.Errorf("%q was called anachronistic: %s", body, finding.Detail)
			}
		}
	}
}
