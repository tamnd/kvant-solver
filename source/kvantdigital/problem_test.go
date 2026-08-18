package kvantdigital

import (
	"os"
	"strings"
	"testing"
)

func problem(t *testing.T) *Problem {
	t.Helper()
	f, err := os.Open("testdata/problem_m301.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	p, err := ParseProblem(f)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProblemIdentity(t *testing.T) {
	p := problem(t)
	if p.ID != "M301" {
		t.Errorf("id is %q", p.ID)
	}
	// The site writes the Cyrillic М, which looks identical to the Latin one
	// and is a different character. Letting it into an identifier means M301
	// and М301 are two problems.
	if p.ID[0] != 'M' {
		t.Error("the identifier kept the Cyrillic letter")
	}
	if p.Label != "Задача М301" {
		t.Errorf("label is %q", p.Label)
	}
	if p.Subject != "math" || p.Number != 301 {
		t.Errorf("subject %q number %d", p.Subject, p.Number)
	}
	if p.URL != "https://www.kvant.digital/problems/m301/" {
		t.Errorf("url is %q", p.URL)
	}
}

func TestProblemLabelBothAlphabets(t *testing.T) {
	for _, tc := range []struct {
		in      string
		subject string
		number  int
	}{
		{"Задача М301", "math", 301},
		{"M301", "math", 301},
		{"Задача Ф1200", "physics", 1200},
		{"F1200", "physics", 1200},
	} {
		subject, number, ok := ParseProblemLabel(tc.in)
		if !ok || subject != tc.subject || number != tc.number {
			t.Errorf("%q gave %q %d %v", tc.in, subject, number, ok)
		}
	}
	if _, _, ok := ParseProblemLabel("Статья про эллипс"); ok {
		t.Error("a title with no problem number was read as one")
	}
}

func TestBothHalvesComeOffOnePage(t *testing.T) {
	p := problem(t)
	// This is the property the whole benchmark rests on. The magazine printed
	// M301 in issue 1 and its solution in issue 8 of the same year, and the
	// site puts both on one page.
	if p.Condition.Year != 1975 || p.Condition.Number != "1" {
		t.Errorf("condition is from %d number %q", p.Condition.Year, p.Condition.Number)
	}
	if !p.HasPublishedSolution() {
		t.Fatal("the published solution was not found")
	}
	if p.Solution.Year != 1975 || p.Solution.Number != "8" {
		t.Errorf("solution is from %d number %q", p.Solution.Year, p.Solution.Number)
	}
	if !strings.HasPrefix(p.Condition.Text, "На плоскости заданы") {
		t.Errorf("condition starts %.40q", p.Condition.Text)
	}
	if !strings.HasPrefix(p.Solution.Text, "Назовём") {
		t.Errorf("solution starts %.40q", p.Solution.Text)
	}
}

func TestFormulasArriveAsLatex(t *testing.T) {
	p := problem(t)
	// Text already carrying LaTeX is the reason this material never goes near
	// OCR. If the dollars stopped arriving the corpus would silently lose
	// every formula, so it is worth a test.
	if !strings.Contains(p.Condition.Text, "$2n$") {
		t.Errorf("condition text has no inline maths: %q", p.Condition.Text)
	}
	if !strings.Contains(p.Solution.Text, "$K_1C_1$") {
		t.Error("the subscripted formula in the solution did not survive")
	}
	// The site wraps every formula in a nowrap span with zero width joiners
	// around it. Those must not reach the text.
	if strings.ContainsAny(p.Condition.Text, "\u200b\u200c\u200d\u2060\ufeff\u00ad") {
		t.Error("zero width characters made it into the condition")
	}
	if strings.ContainsRune(p.Condition.Text, '\u00a0') {
		t.Error("non breaking spaces made it into the condition")
	}
}

func TestParagraphsAreKeptApart(t *testing.T) {
	p := problem(t)
	// The solution is an argument in several steps. Running the paragraphs
	// together would change what it says.
	if n := strings.Count(p.Solution.Text, "\n\n"); n < 1 {
		t.Errorf("the solution came back as one paragraph")
	}
}

func TestBylineAndFigures(t *testing.T) {
	p := problem(t)
	if !strings.Contains(p.Condition.Byline, "Охитин") {
		t.Errorf("byline is %q", p.Condition.Byline)
	}
	// The byline is worth keeping: a fair number of problems were proposed by
	// schoolchildren and the magazine printed their school and town.
	if !strings.Contains(p.Condition.Byline, "ученик 10 класса") {
		t.Errorf("byline is %q", p.Condition.Byline)
	}
	if len(p.Condition.Figures) != 1 {
		t.Fatalf("%d figures on the condition", len(p.Condition.Figures))
	}
	fig := p.Condition.Figures[0]
	if !strings.HasPrefix(fig.URL, "https://www.kvant.digital/media/common/") {
		t.Errorf("figure url is %q, the protocol relative src did not resolve", fig.URL)
	}
	if fig.Caption != "Рис. 1" {
		t.Errorf("caption is %q", fig.Caption)
	}
}

func TestMetadataNamesProposerAndSolver(t *testing.T) {
	p := problem(t)
	if len(p.Authors) != 1 || p.Authors[0].Slug != "ohitin_s_v" {
		t.Errorf("authors are %+v", p.Authors)
	}
	// The people who wrote the published solution are usually the editors, and
	// they are not the same list as the proposer, so the two are kept apart.
	if len(p.Solvers) != 2 {
		t.Fatalf("solvers are %+v", p.Solvers)
	}
	if p.Solvers[0].Name != "Тоом А. Л." {
		t.Errorf("first solver is %q", p.Solvers[0].Name)
	}
}

func TestRefsCarryTheIssueAndThePages(t *testing.T) {
	p := problem(t)
	if len(p.Refs) != 2 {
		t.Fatalf("%d refs", len(p.Refs))
	}
	cond, sol := p.Refs[0], p.Refs[1]
	if cond.Kind != KindCondition || cond.IssueKey != "kvant_1975_1" || cond.Pages != "41" {
		t.Errorf("condition ref is %+v", cond)
	}
	if sol.Kind != KindSolution || sol.IssueKey != "kvant_1975_8" {
		t.Errorf("solution ref is %+v", sol)
	}
	// A solution that runs over a page break is printed as a range, and the
	// range is kept as printed rather than reduced to its first page.
	if sol.Pages != "51—52" {
		t.Errorf("solution pages are %q", sol.Pages)
	}
}

func TestParseProblemRefusesAPageWithNoNumber(t *testing.T) {
	if _, err := ParseProblem(strings.NewReader("<html><body><h1>Ничего</h1></body></html>")); err == nil {
		t.Fatal("a page with no problem number should be an error")
	}
}

// untyped is the shape of the several hundred pages the site has indexed and
// not typed: the condition block is there with its issue and its page, and the
// words are only in the scan.
const untyped = `<html><body>
<h1><span class="mark--hlwords--flt">Задача Ф351</span></h1>
<div class="box--block"><h2><span class="no-copy">Условие задачи (1975,&nbsp;№&nbsp;8)</span></h2>
<div class="block--text"></div></div>
<div class="box--block"><h2><span class="no-copy">Метаданные</span></h2>
<dl class="object--meta"><dt>Номера</dt><dd>
<p><a href="//www.kvant.digital/view/kvant_1975_8/" data-url="&quot;kvant_1975_8&quot;">1975</a> № 8 <label>39</label> [условие]</p>
<p><a href="//www.kvant.digital/view/kvant_1976_2/" data-url="&quot;kvant_1976_2&quot;">1976</a> № 2 <label>44—45</label> [решение]</p>
</dd></dl></div>
</body></html>`

func TestAPageTheSiteHasNotTypedIsStillRead(t *testing.T) {
	p, err := ParseProblem(strings.NewReader(untyped))
	if err != nil {
		t.Fatalf("a page with no typed condition should still be read: %v", err)
	}
	if p.ID != "F351" {
		t.Errorf("id is %q", p.ID)
	}
	// The words are the part that is missing, and the references are the part
	// that is worth having: they say which issue printed each half, which is
	// what the cross check is taken on.
	if p.Condition.Text != "" {
		t.Errorf("condition text is %q, the page carries none", p.Condition.Text)
	}
	if p.Condition.Year != 1975 || p.Condition.Number != "8" {
		t.Errorf("condition is dated %d number %q", p.Condition.Year, p.Condition.Number)
	}
	if len(p.Refs) != 2 {
		t.Fatalf("%d refs", len(p.Refs))
	}
	if p.Refs[0].IssueKey != "kvant_1975_8" || p.Refs[0].Kind != KindCondition {
		t.Errorf("condition ref is %+v", p.Refs[0])
	}
	if p.Refs[1].IssueKey != "kvant_1976_2" || p.Refs[1].Kind != KindSolution {
		t.Errorf("solution ref is %+v", p.Refs[1])
	}
}
