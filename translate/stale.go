package translate

import (
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/glossary"
)

// Staleness is the three tests, kept apart so the audit can say which one
// fired rather than just that something did.
type Staleness struct {
	// SourceMoved is the Russian this file was translated from no longer
	// having the content it had. Whatever else is true, this translation is of
	// a document that does not exist any more.
	SourceMoved bool
	// GlossaryMoved is the version having been bumped since. On its own this
	// says nothing about this file.
	GlossaryMoved bool
	// TermsMoved is the rows this file would be shown today differing from the
	// ones it was shown. This is the one that decides.
	TermsMoved bool
	// PromptMoved is the wording of the instruction having been edited since.
	// Unlike a version bump this is stale on its own, because the register is
	// the thing the translation is being held to and changing it means every
	// file was held to something else.
	PromptMoved bool
	// Untranslated is a file with no translation stamp at all, which is not
	// stale so much as absent, and the two want different work.
	Untranslated bool
}

// Stale reports whether the file has to be done again.
//
// The composition is the point of having three tests rather than one. A moved
// source is stale on its own. A glossary bump is not: it is a screen, and it is
// cheap, because if the version has not moved then no row can have moved and
// there is nothing to recompute. When the version has moved, what decides is
// whether a row this particular file was shown is different today, which is why
// the terms hash is over the rows shown and not over the whole glossary.
//
// Without that last distinction the pipeline is unusable. Adding one word about
// optics would bump the version, every one of the twenty thousand translated
// files would go stale, and the corpus would have to be retranslated to record
// a term that appears in four articles.
func (s Staleness) Stale() bool {
	if s.Untranslated {
		return false
	}
	return s.SourceMoved || s.PromptMoved || (s.GlossaryMoved && s.TermsMoved)
}

// Reason is what to print in the audit.
func (s Staleness) Reason() string {
	switch {
	case s.Untranslated:
		return "not translated yet"
	case s.SourceMoved && s.TermsMoved:
		return "the Russian has changed and so have the glossary rows this file uses"
	case s.SourceMoved:
		return "the Russian has changed since this was translated"
	case s.PromptMoved:
		return "the translation instruction has been edited since this was written"
	case s.GlossaryMoved && s.TermsMoved:
		return "a glossary row this file uses has changed"
	case s.GlossaryMoved:
		return "the glossary moved but no row this file uses did, so this is still current"
	default:
		return "current"
	}
}

// Pair is one translated file and the Russian it came from.
//
// It is a struct rather than a row of arguments because two of the fields are
// the same type and mean opposite things, and getting Source and Target the
// wrong way round would compile and would quietly report the whole corpus as
// current.
type Pair struct {
	// Stamp is the translation record on the translated file.
	Stamp corpus.Translated
	// Target is the provenance of the translated file, which is where the
	// prompt hash lives.
	Target corpus.Provenance
	// Source is the provenance of the Russian, and SourceBody is the Russian
	// itself as it stands now.
	Source     corpus.Provenance
	SourceBody string
	Lang       string
}

// Check decides staleness by reading files, and makes no model call.
//
// That is the whole reason the hashes are written. Deciding this with a model
// would cost as much as the translation did and would give a different answer
// on a different day.
//
// A nil Prompts means the wording this corpus ships with.
func Check(p Pair, g *glossary.Glossary, prompts Prompts) Staleness {
	if strings.TrimSpace(p.Stamp.TranslatedFrom) == "" && p.Stamp.SourceSHA256 == "" {
		return Staleness{Untranslated: true}
	}
	if prompts == nil {
		prompts = DefaultPrompts{}
	}
	var s Staleness
	s.SourceMoved = p.Stamp.SourceSHA256 != p.Source.ContentSHA256
	// A file written before the prompt was ever hashed says nothing about it,
	// and treating silence as a mismatch would restale every page from the first
	// runs. It gets left alone until something else sends it back.
	s.PromptMoved = p.Target.PromptSHA256 != "" && p.Target.PromptSHA256 != prompts.Hash(p.Lang)
	if g == nil {
		return s
	}
	s.GlossaryMoved = p.Stamp.GlossaryVersion != g.Version
	// The rows are recomputed against the body as it is now, so a term that
	// became relevant because the Russian was corrected counts as a change even
	// though the glossary row itself did not move.
	s.TermsMoved = p.Stamp.GlossaryTerms != glossary.TermsHash(g.Mentioned(p.SourceBody, p.Lang), p.Lang)
	return s
}

// Stamp is the record a fresh translation writes.
//
// It is one function rather than six assignments at the call site, because a
// stamp with one field left out is a file that quietly never goes stale, and
// that failure is invisible until somebody trusts the audit.
//
// The prompt hash is not here because it does not live here: it goes in
// Provenance, next to the content hash, where every other fingerprint of how a
// file was made already is.
func Stamp(sourceKey string, source corpus.Provenance, sourceBody string,
	g *glossary.Glossary, lang, model, run string) corpus.Translated {
	t := corpus.Translated{
		TranslatedFrom:   sourceKey,
		SourceSHA256:     source.ContentSHA256,
		TranslationModel: model,
		TranslationRun:   run,
	}
	if g != nil {
		t.GlossaryVersion = g.Version
		t.GlossaryTerms = glossary.TermsHash(g.Mentioned(sourceBody, lang), lang)
	}
	return t
}

// Audit is one pass over a translated tree.
type Audit struct {
	Lang    string
	Checked int
	Stale   []Finding
	Missing []string
	Current int
}

// Finding is one file that has to be done again.
type Finding struct {
	Path   string
	Reason string
}

// Add records one file.
func (a *Audit) Add(path string, s Staleness) {
	a.Checked++
	switch {
	case s.Untranslated:
		a.Missing = append(a.Missing, path)
	case s.Stale():
		a.Stale = append(a.Stale, Finding{Path: path, Reason: s.Reason()})
	default:
		a.Current++
	}
}

// Clean is the exit condition M10 asks for: a year translated with zero stale
// files.
func (a *Audit) Clean() bool { return len(a.Stale) == 0 }

// method is how staleness is decided, and it is the same sentence whichever
// language is being reported on, so a combined report says it once at the top
// rather than four times over.
const method = "Staleness is decided by reading the files and makes no model call. " +
	"A file is stale when the Russian it was translated from has changed, " +
	"or when a glossary row it was actually shown has changed. " +
	"A glossary version bump on its own does not make a file stale, " +
	"because otherwise adding one term would invalidate the whole corpus.\n"

// Report is one language on its own, which is what the command prints at the
// end of a run.
func (a *Audit) Report() string {
	var b strings.Builder
	b.WriteString("# Translation audit: " + a.Lang + "\n\n")
	if a.Checked == 0 {
		b.WriteString("Nothing has been translated into this language yet.\n")
		return b.String()
	}
	b.WriteString(a.counts() + "\n")
	b.WriteString(method)
	b.WriteString(a.lists("##"))
	return b.String()
}

// Combined is reports/translation-audit.md, which covers every language rather
// than whichever one the command happened to be run for last.
//
// The file used to be written from the single run's audit, under a path that
// did not mention the language. Translating into Vietnamese therefore replaced
// the English report with Vietnamese numbers under a heading that still said
// English, and there was nothing in the file to say so. Staleness costs no model
// call, so working out all four languages is as cheap as working out one, and a
// reader comparing coverage wants them side by side anyway.
// scope says what was walked, because a run restricted to one year produces a
// report that looks exactly like a run over everything.
func Combined(scope string, audits []*Audit) string {
	var b strings.Builder
	b.WriteString("# Translation audit\n\n")
	b.WriteString("Covering " + scope + ".\n\n")
	b.WriteString(method)
	for _, a := range audits {
		b.WriteString("\n## " + Language(a.Lang) + "\n\n")
		if a.Checked == 0 {
			b.WriteString("Nothing has been translated into this language yet.\n")
			continue
		}
		b.WriteString(a.counts())
		b.WriteString(a.lists("###"))
	}
	return b.String()
}

func (a *Audit) counts() string {
	return plural(a.Checked, "file") + " checked, " + itoa(a.Current) +
		" current, " + itoa(len(a.Stale)) + " stale, " + itoa(len(a.Missing)) + " not translated yet.\n"
}

// lists is the two enumerations, under headings of whatever depth the document
// around them is using.
func (a *Audit) lists(heading string) string {
	var b strings.Builder
	if len(a.Stale) > 0 {
		b.WriteString("\n" + heading + " Stale\n\n")
		for _, f := range a.Stale {
			b.WriteString("- `" + f.Path + "`: " + f.Reason + "\n")
		}
	}
	if len(a.Missing) > 0 {
		b.WriteString("\n" + heading + " Not translated yet\n\n")
		shown := a.Missing
		if len(shown) > missingShown {
			shown = shown[:missingShown]
		}
		for _, p := range shown {
			b.WriteString("- `" + p + "`\n")
		}
		if rest := len(a.Missing) - len(shown); rest > 0 {
			b.WriteString("\nand " + itoa(rest) + " more, not listed.\n")
		}
	}
	return b.String()
}

// missingShown is how many untranslated paths one language lists before the
// report gives the count instead. A language nobody has started on has every
// file in the corpus under this heading, and nobody reads five thousand paths to
// learn that none of them exist. The stale list is deliberately not capped: it
// is the one somebody has to act on, and M10 is finished when it is empty.
const missingShown = 50

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
