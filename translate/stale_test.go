package translate

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/glossary"
)

const body = "Через проводник идёт ток, и производная заряда по времени постоянна."

func gloss() *glossary.Glossary {
	return &glossary.Glossary{Version: 3, Terms: []glossary.Term{
		{RU: "ток", EN: "current"},
		{RU: "производная", EN: "derivative"},
		{RU: "дифракция", EN: "diffraction"},
	}}
}

func source() corpus.Provenance {
	return corpus.Provenance{Lang: "ru", ContentSHA256: "aaaa"}
}

func fresh(g *glossary.Glossary) corpus.Translated {
	return Stamp("content/ru/1975/08/pages/0012.md", source(), body, g, "en", "test-model", "run-1")
}

// pair is one translated file as it would be read back off disk, with the
// prompt hash the engine would have written into its provenance.
func pair(stamp corpus.Translated, src corpus.Provenance, sourceBody string) Pair {
	return Pair{
		Stamp:      stamp,
		Target:     corpus.Provenance{Lang: "en", PromptSHA256: DefaultPrompts{}.Hash("en")},
		Source:     src,
		SourceBody: sourceBody,
		Lang:       "en",
	}
}

func TestAFreshTranslationIsNotStale(t *testing.T) {
	g := gloss()
	if s := Check(pair(fresh(g), source(), body), g, nil); s.Stale() {
		t.Fatalf("a translation stamped a moment ago is stale: %s", s.Reason())
	}
}

func TestAChangedSourceIsStaleOnItsOwn(t *testing.T) {
	g := gloss()
	moved := source()
	moved.ContentSHA256 = "bbbb"
	s := Check(pair(fresh(g), moved, body), g, nil)
	if !s.SourceMoved || !s.Stale() {
		t.Fatalf("a translation of a document that changed is not stale: %+v", s)
	}
}

func TestAddingAnUnrelatedTermDoesNotInvalidateTheCorpus(t *testing.T) {
	// This is the test the whole design is for. Without the third check, adding
	// one word to the glossary means retranslating every file in the archive.
	g := gloss()
	stamp := fresh(g)

	g.Version = 4
	g.Terms = append(g.Terms, glossary.Term{RU: "интерференция", EN: "interference"})

	s := Check(pair(stamp, source(), body), g, nil)
	if !s.GlossaryMoved {
		t.Fatal("the glossary version moved and the check did not notice")
	}
	if s.TermsMoved {
		t.Fatal("a term this file never uses was counted as a change to it")
	}
	if s.Stale() {
		t.Fatalf("a version bump on its own made the file stale: %s", s.Reason())
	}
}

func TestChangingATermTheFileUsesMakesItStale(t *testing.T) {
	g := gloss()
	stamp := fresh(g)

	g.Version = 4
	for i := range g.Terms {
		if g.Terms[i].RU == "ток" {
			g.Terms[i].EN = "electric current"
		}
	}

	s := Check(pair(stamp, source(), body), g, nil)
	if !s.GlossaryMoved || !s.TermsMoved || !s.Stale() {
		t.Fatalf("a changed term the file uses did not make it stale: %+v", s)
	}
}

func TestATermThatBecameRelevantMakesTheFileStale(t *testing.T) {
	// The rows are recomputed against the Russian as it stands now, so a term
	// that was in the glossary all along but only appears after the page was
	// reread counts as a change to this file.
	g := gloss()
	stamp := fresh(g)

	g.Version = 4
	reread := body + " Дифракция здесь ни при чём."

	s := Check(pair(stamp, source(), reread), g, nil)
	if !s.TermsMoved {
		t.Fatal("a term that entered the body was not counted")
	}
	if !s.Stale() {
		t.Fatalf("the file is not stale: %s", s.Reason())
	}
}

func TestAFileWithNoStampIsMissingRatherThanStale(t *testing.T) {
	// The two want different work: one has to be translated, the other has to be
	// translated again, and lumping them together hides how much of the corpus
	// was never done at all.
	g := gloss()
	s := Check(pair(corpus.Translated{}, source(), body), g, nil)
	if !s.Untranslated {
		t.Fatal("a file with no translation stamp was not reported as untranslated")
	}
	if s.Stale() {
		t.Fatal("a file that was never translated was reported as stale")
	}
}

func TestEachLanguageGoesStaleOnItsOwnColumn(t *testing.T) {
	// English and Vietnamese are shown different columns of the same rows, so
	// fixing the Vietnamese must not send every English file back through.
	g := gloss()
	english := fresh(g)

	g.Version = 4
	for i := range g.Terms {
		if g.Terms[i].RU == "ток" {
			g.Terms[i].VI = "dòng điện"
		}
	}
	if s := Check(pair(english, source(), body), g, nil); s.Stale() {
		t.Fatalf("an English file went stale over a Vietnamese edit: %s", s.Reason())
	}
}

// otherPrompts is the same wording with something changed in it.
type otherPrompts struct{ DefaultPrompts }

func (otherPrompts) Hash(string) string { return "a different instruction" }

func TestAnEditedInstructionIsStaleOnItsOwn(t *testing.T) {
	// The register is the thing the translation is being held to, so editing it
	// means every file was held to something else. Unlike a glossary bump this
	// does not need a second test to confirm it applies to this file, because it
	// applies to all of them.
	g := gloss()
	s := Check(pair(fresh(g), source(), body), g, otherPrompts{})
	if !s.PromptMoved {
		t.Fatal("an edited instruction was not noticed")
	}
	if !s.Stale() {
		t.Fatalf("an edited instruction did not make the file stale: %s", s.Reason())
	}
}

func TestAFileFromBeforeThePromptWasHashedIsLeftAlone(t *testing.T) {
	// Treating a missing hash as a mismatch would send every page from the first
	// runs back through for nothing.
	g := gloss()
	p := pair(fresh(g), source(), body)
	p.Target.PromptSHA256 = ""
	if s := Check(p, g, otherPrompts{}); s.PromptMoved || s.Stale() {
		t.Fatalf("a file that never recorded a prompt hash was called stale: %s", s.Reason())
	}
}

func TestTheInstructionHashIsPerLanguage(t *testing.T) {
	// The English and the Vietnamese are written under different instructions,
	// so they cannot share one fingerprint.
	p := DefaultPrompts{}
	if p.Hash("en") == p.Hash("vi") {
		t.Fatal("two languages hashed to the same instruction")
	}
}

func TestTheStampCarriesEverythingTheCheckNeeds(t *testing.T) {
	// A stamp with a field left out is a file that quietly never goes stale, and
	// that failure is invisible until somebody trusts the audit.
	g := gloss()
	stamp := fresh(g)
	switch {
	case stamp.TranslatedFrom == "":
		t.Fatal("the stamp does not say what it was translated from")
	case stamp.SourceSHA256 != "aaaa":
		t.Fatalf("the stamp recorded the source hash as %q", stamp.SourceSHA256)
	case stamp.TranslationModel == "":
		t.Fatal("the stamp does not say which model wrote it")
	case stamp.TranslationRun == "":
		t.Fatal("the stamp does not say which run wrote it")
	case stamp.GlossaryVersion != 3:
		t.Fatalf("the stamp recorded glossary version %d, want 3", stamp.GlossaryVersion)
	case stamp.GlossaryTerms == "":
		t.Fatal("the stamp does not carry the hash of the rows it was shown")
	}
}

func TestTheAuditSeparatesStaleFromNeverTranslated(t *testing.T) {
	a := &Audit{Lang: "en"}
	a.Add("a.md", Staleness{})
	a.Add("b.md", Staleness{SourceMoved: true})
	a.Add("c.md", Staleness{Untranslated: true})
	a.Add("d.md", Staleness{GlossaryMoved: true})

	if a.Checked != 4 {
		t.Fatalf("checked %d files, want 4", a.Checked)
	}
	if a.Current != 2 {
		t.Fatalf("%d current, want 2: a version bump with no row change is current", a.Current)
	}
	if len(a.Stale) != 1 || a.Stale[0].Path != "b.md" {
		t.Fatalf("stale is %v", a.Stale)
	}
	if len(a.Missing) != 1 || a.Missing[0] != "c.md" {
		t.Fatalf("missing is %v", a.Missing)
	}
	if a.Clean() {
		t.Fatal("an audit with a stale file reported clean")
	}
}

func TestAnAuditWithNothingStaleIsClean(t *testing.T) {
	a := &Audit{Lang: "en"}
	a.Add("a.md", Staleness{})
	a.Add("b.md", Staleness{Untranslated: true})
	if !a.Clean() {
		t.Fatal("an audit with nothing stale did not report clean")
	}
	if a.Report() == "" {
		t.Fatal("the report is empty")
	}
}

// The report is one file with one name, and it used to be written from whichever
// language the run was for, so translating into a second language replaced the
// first language's report while the heading still named the first.
func TestTheCombinedReportKeepsEveryLanguage(t *testing.T) {
	en := &Audit{Lang: "en"}
	en.Add("content/en/1970/01/pages/0003.md", Staleness{SourceMoved: true})
	vi := &Audit{Lang: "vi"}
	vi.Add("content/vi/1970/01/pages/0007.md", Staleness{GlossaryMoved: true, TermsMoved: true})

	report := Combined("1970", []*Audit{en, vi})
	for _, want := range []string{
		"## English", "## Vietnamese",
		"content/en/1970/01/pages/0003.md",
		"content/vi/1970/01/pages/0007.md",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("the combined report does not mention %q:\n%s", want, report)
		}
	}
	// A heading naming one language is what made the overwrite invisible, so the
	// combined report must not carry one at the top.
	if strings.Contains(report, "# Translation audit: ") {
		t.Fatalf("the combined report is headed as though it were one language:\n%s", report)
	}
}

func TestTheCombinedReportExplainsStalenessOnce(t *testing.T) {
	en := &Audit{Lang: "en"}
	en.Add("a.md", Staleness{})
	vi := &Audit{Lang: "vi"}
	vi.Add("b.md", Staleness{})

	report := Combined("1970", []*Audit{en, vi})
	if n := strings.Count(report, "makes no model call"); n != 1 {
		t.Fatalf("the method is stated %d times, want 1:\n%s", n, report)
	}
}

// An untouched language is a row of the coverage table, not an omission. Leaving
// it out reads as though the corpus only ever aimed at the ones that are done.
func TestALanguageWithNothingTranslatedStillGetsASection(t *testing.T) {
	en := &Audit{Lang: "en"}
	en.Add("a.md", Staleness{})

	report := Combined("1970", []*Audit{en, {Lang: "ja"}})
	if !strings.Contains(report, "## Japanese") {
		t.Fatalf("the empty language has no section:\n%s", report)
	}
	if !strings.Contains(report, "Nothing has been translated into this language yet") {
		t.Fatalf("the empty language does not say so:\n%s", report)
	}
}

// The lists sit one level down inside a combined report, so that a language's
// own heading still contains them.
func TestTheCombinedListsSitUnderTheirLanguage(t *testing.T) {
	en := &Audit{Lang: "en"}
	en.Add("a.md", Staleness{SourceMoved: true})
	en.Add("b.md", Staleness{Untranslated: true})

	report := Combined("1970", []*Audit{en})
	for _, want := range []string{"### Stale", "### Not translated yet"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the combined report is missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "\n## Stale") {
		t.Fatalf("a list escaped its language section:\n%s", report)
	}
}

// A report over one year and a report over the archive are otherwise identical
// documents, so the file has to say which one it is.
func TestTheCombinedReportSaysWhatItCovers(t *testing.T) {
	a := &Audit{Lang: "en"}
	a.Add("a.md", Staleness{})
	if !strings.Contains(Combined("1970", []*Audit{a}), "Covering 1970.") {
		t.Fatal("the combined report does not say what it covers")
	}
}

// A language nobody has started on has every file in the corpus under one
// heading. The count is what a reader wants, and the cut has to be visible.
func TestALongListOfUntranslatedFilesIsCutAndSaysSo(t *testing.T) {
	a := &Audit{Lang: "ja"}
	for i := range missingShown + 7 {
		a.Add("content/ja/1970/01/pages/"+itoa(i)+".md", Staleness{Untranslated: true})
	}
	report := Combined("1970", []*Audit{a})
	if n := strings.Count(report, "content/ja/"); n != missingShown {
		t.Fatalf("the report lists %d paths, want the cap of %d", n, missingShown)
	}
	if !strings.Contains(report, "and 7 more, not listed.") {
		t.Fatalf("the report cut the list without saying so:\n%s", report)
	}
	if !strings.Contains(report, itoa(missingShown+7)+" not translated yet") {
		t.Fatalf("the report does not give the full count:\n%s", report)
	}
}

// The stale list is the one somebody has to act on, and M10 is finished when it
// is empty, so cutting it would hide the work that is left.
func TestTheStaleListIsNeverCut(t *testing.T) {
	a := &Audit{Lang: "en"}
	n := missingShown + 20
	for i := range n {
		a.Add("content/en/1970/01/pages/"+itoa(i)+".md", Staleness{SourceMoved: true})
	}
	if got := strings.Count(Combined("1970", []*Audit{a}), "content/en/"); got != n {
		t.Fatalf("the report lists %d stale files, want all %d", got, n)
	}
}

// The single language report is what the command prints at the end of a run, and
// it keeps the headings it always had.
func TestTheSingleLanguageReportIsUnchanged(t *testing.T) {
	a := &Audit{Lang: "en"}
	a.Add("a.md", Staleness{SourceMoved: true})
	a.Add("b.md", Staleness{Untranslated: true})

	report := a.Report()
	for _, want := range []string{"# Translation audit: en", "## Stale", "## Not translated yet"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the single language report is missing %q:\n%s", want, report)
		}
	}
}

func TestTheReportSaysWhichTestFired(t *testing.T) {
	a := &Audit{Lang: "en"}
	a.Add("a.md", Staleness{SourceMoved: true})
	a.Add("b.md", Staleness{GlossaryMoved: true, TermsMoved: true})
	report := a.Report()
	for _, want := range []string{"the Russian has changed", "a glossary row this file uses has changed"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report does not say %q:\n%s", want, report)
		}
	}
}
