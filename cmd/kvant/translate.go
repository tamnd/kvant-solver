package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/glossary"
	"github.com/tamnd/kvant-solver/translate"
)

func runTranslate(args []string) error {
	fs := pflag.NewFlagSet("translate", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	from := fs.String("from", "ru", "the language to translate out of")
	lang := fs.String("lang", "en", "the language to translate into")
	year := fs.Int("year", 0, "restrict to one year, 0 for the whole corpus")
	issue := fs.String("issue", "", "restrict to one issue, for example kvant_1975_8")
	model := fs.String("model", "gpt-5", "which model to translate with")
	endpoint := fs.String("endpoint", "http://127.0.0.1:8077/v1/chat/completions", "the chatgpt-tool serve endpoint")
	apiKey := fs.String("key", os.Getenv("KVANT_API_KEY"), "API key for the endpoint")
	retries := fs.Int("retries", 1, "how many times a chunk that mangled the mathematics is asked again")
	timeout := fs.Duration("timeout", 10*time.Minute, "per call timeout")
	run := fs.String("run", "", "a name for this run, recorded in every file it writes")
	limit := fs.Int("limit", 0, "stop after this many files, 0 for all of them")
	audit := fs.Bool("audit", false, "report what is stale and write nothing")
	force := fs.Bool("force", false, "retranslate files the staleness check says are current")
	write := fs.Bool("write", false, "write the translations into the corpus")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !glossary.Known(*lang) {
		return fmt.Errorf("%s is not one of %s", *lang, strings.Join(glossary.Languages, ", "))
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}

	// A missing glossary is not an error. The first run into a new language has
	// nothing to be held to yet, and refusing to start until somebody writes one
	// would mean the glossary has to be guessed before any text has been read.
	g, err := glossary.Load(c.Root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if g == nil {
		fmt.Printf("no %s, translating without one\n", glossary.Path(c.Root))
	}

	sources, err := sourceFiles(c, *from, *year, *issue)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("nothing to translate under content/%s", *from)
	}

	engine := &translate.Engine{
		Client: &api.Client{
			URL:        *endpoint,
			APIKey:     *apiKey,
			HTTPClient: &http.Client{Timeout: *timeout + time.Minute},
			UserAgent:  "kvant/" + version,
		},
		Progress: func(line string) { fmt.Println("  " + line) },
	}

	report := &translate.Audit{Lang: *lang}
	ctx := context.Background()
	done := 0

	for _, src := range sources {
		rel, err := filepath.Rel(c.Root, src)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)

		front, ok := frontFor(key)
		if !ok {
			continue
		}
		body, err := corpus.LoadUnchecked(src, front)
		if err != nil {
			fmt.Printf("%s skipped: %v\n", key, err)
			continue
		}

		target := targetPath(c, key, *from, *lang)
		state := stateOf(target, front, body, g, *lang)
		report.Add(strings.TrimPrefix(target, c.Root+string(filepath.Separator)), state)
		if !*force && !state.Stale() && !state.Untranslated {
			continue
		}
		if *audit {
			continue
		}
		if *limit > 0 && done >= *limit {
			continue
		}

		fmt.Printf("%s: %s\n", key, state.Reason())
		res, err := engine.Translate(ctx,
			translate.Job{Key: key, Lang: *lang, Body: body, Title: titleOf(front)},
			g, translate.Options{Model: *model, Retries: *retries})
		if err != nil {
			fmt.Printf("  failed: %v\n", err)
			continue
		}
		for _, w := range res.Warnings {
			fmt.Printf("  %s\n", w)
		}
		done++

		if !*write {
			continue
		}
		if err := writeTranslation(target, front, res, key, *lang, *run, g, body); err != nil {
			return err
		}
	}

	fmt.Println()
	fmt.Print(report.Report())

	if *audit && *write {
		return writeAuditReport(c, sources, *from, scopeOf(*year, *issue), g)
	}
	return nil
}

// writeAuditReport writes reports/translation-audit.md, covering every language
// this corpus translates into rather than the one the command was asked for.
//
// The report is one file with one name, so writing it from the run's own audit
// meant each language overwrote the last. Every language is walked here instead,
// which costs nothing but file reads, and the run's --lang only decides which
// section a reader is most likely to be looking for.
func writeAuditReport(c *corpus.Corpus, sources []string, from, scope string,
	g *glossary.Glossary) error {
	audits := make([]*translate.Audit, 0, len(glossary.Languages))
	for _, lang := range glossary.Languages {
		a, err := auditLang(c, sources, from, lang, g)
		if err != nil {
			return err
		}
		audits = append(audits, a)
	}
	path := filepath.Join(c.Root, "reports", "translation-audit.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(translate.Combined(scope, audits)), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// scopeOf says in words what the run was restricted to, so that a report over
// one year is not read as a report over the archive.
func scopeOf(year int, issue string) string {
	switch {
	case issue != "":
		return issue
	case year > 0:
		return fmt.Sprintf("%d", year)
	default:
		return "the whole corpus"
	}
}

// auditLang is the staleness pass for one language, over the same source files
// the run itself walked. It makes no model call and writes nothing.
func auditLang(c *corpus.Corpus, sources []string, from, lang string,
	g *glossary.Glossary) (*translate.Audit, error) {
	a := &translate.Audit{Lang: lang}
	for _, src := range sources {
		rel, err := filepath.Rel(c.Root, src)
		if err != nil {
			return nil, err
		}
		key := filepath.ToSlash(rel)
		front, ok := frontFor(key)
		if !ok {
			continue
		}
		// A source that will not parse is skipped rather than counted missing.
		// It is a fault in the Russian, and reporting it as an untranslated
		// English file would send somebody to fix the wrong tree.
		body, err := corpus.LoadUnchecked(src, front)
		if err != nil {
			continue
		}
		target := targetPath(c, key, from, lang)
		a.Add(strings.TrimPrefix(target, c.Root+string(filepath.Separator)),
			stateOf(target, front, body, g, lang))
	}
	return a, nil
}

// stateOf reads the file already sitting at the target path and decides whether
// it has to be done again.
//
// A target that does not exist is untranslated rather than stale, and a target
// that will not parse is treated the same way: whatever is there cannot be
// trusted to say what it was made from, so the only safe answer is to make it
// again.
func stateOf(target string, front corpus.Front, body string, g *glossary.Glossary, lang string) translate.Staleness {
	existing, ok := frontFor(filepath.ToSlash(target))
	if !ok {
		return translate.Staleness{Untranslated: true}
	}
	if _, err := corpus.LoadUnchecked(target, existing); err != nil {
		return translate.Staleness{Untranslated: true}
	}
	return translate.Check(translate.Pair{
		Stamp:      translatedOf(existing),
		Target:     provenanceOf(existing),
		Source:     provenanceOf(front),
		SourceBody: body,
		Lang:       lang,
	}, g, nil)
}

// sourceFiles is every Markdown file under one language, in a stable order.
func sourceFiles(c *corpus.Corpus, lang string, year int, issue string) ([]string, error) {
	dir := filepath.Join(c.Root, "content", lang)
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(c.Root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if _, ok := frontFor(key); !ok {
			return nil
		}
		if year > 0 && !strings.Contains(key, fmt.Sprintf("/%d/", year)) {
			return nil
		}
		if issue != "" {
			parsed, err := corpus.ParseIssueKey(issue)
			if err != nil {
				return err
			}
			if !strings.Contains(key, "/"+parsed.Dir()+"/") {
				return nil
			}
		}
		out = append(out, path)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no content under %s", dir)
	}
	return out, err
}

// frontFor is the schema a path holds, decided from the path alone.
//
// issue.md is left out. It is an index of what is in the issue rather than
// prose, and the fields worth reading in another language are the article
// titles, which are translated with the articles themselves.
func frontFor(key string) (corpus.Front, bool) {
	switch {
	case strings.Contains(key, "/pages/"):
		return &corpus.PageFront{}, true
	case strings.Contains(key, "/articles/"):
		return &corpus.ArticleFront{}, true
	case strings.Contains(key, "/problems/"):
		return &corpus.ProblemFront{}, true
	default:
		return nil, false
	}
}

// targetPath is where the translation of one source file goes.
func targetPath(c *corpus.Corpus, key, from, lang string) string {
	rel := strings.Replace(key, "content/"+from+"/", "content/"+lang+"/", 1)
	return filepath.Join(c.Root, filepath.FromSlash(rel))
}

// provenanceOf and translatedOf reach the two embedded blocks that every schema
// carries. A type switch is the honest way to do this: the alternative is
// reflection over four structs that are deliberately not related by an
// interface, and a new schema should fail to compile here rather than be
// silently skipped.
func provenanceOf(front corpus.Front) corpus.Provenance {
	switch f := front.(type) {
	case *corpus.PageFront:
		return f.Provenance
	case *corpus.ArticleFront:
		return f.Provenance
	case *corpus.ProblemFront:
		return f.Provenance
	default:
		return corpus.Provenance{}
	}
}

// titleOf is the one piece of prose that lives in the front matter rather than
// the body. Only articles carry one, so everything else translates its body and
// nothing else.
func titleOf(front corpus.Front) string {
	if f, ok := front.(*corpus.ArticleFront); ok {
		return f.Title
	}
	return ""
}

func translatedOf(front corpus.Front) corpus.Translated {
	switch f := front.(type) {
	case *corpus.PageFront:
		return f.Translated
	case *corpus.ArticleFront:
		return f.Translated
	case *corpus.ProblemFront:
		return f.Translated
	default:
		return corpus.Translated{}
	}
}

// stampOn writes the translation provenance into the front matter, keeping
// every other field the source file carried.
//
// The front matter is the source file's, with the language changed and the
// translation block filled in. That is the point of Translated being embedded
// in the four schemas rather than being a fifth: a translated page is a page,
// with the same issue, the same page index and the same rubrics, and copying
// those across by hand is how they drift.
func stampOn(front corpus.Front, lang, prompt string, t corpus.Translated) {
	switch f := front.(type) {
	case *corpus.PageFront:
		f.Lang, f.PromptSHA256, f.Translated = lang, prompt, t
	case *corpus.ArticleFront:
		f.Lang, f.PromptSHA256, f.Translated = lang, prompt, t
	case *corpus.ProblemFront:
		f.Lang, f.PromptSHA256, f.Translated = lang, prompt, t
	}
}

func writeTranslation(target string, front corpus.Front, res translate.Result,
	key, lang, run string, g *glossary.Glossary, sourceBody string) error {
	stampOn(front, lang, res.Prompt,
		translate.Stamp(key, provenanceOf(front), sourceBody, g, lang, res.Model, run))
	// An empty title back from a job that carried one means the call returned
	// nothing usable, and the Russian title is better than no title at all.
	if f, ok := front.(*corpus.ArticleFront); ok && res.Title != "" {
		f.Title = res.Title
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return corpus.Save(target, front, res.Body)
}
