package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/audit"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/lexicon"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/prompt"
)

func runAudit(args []string) error {
	fs := pflag.NewFlagSet("audit", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	lang := fs.String("lang", corpus.DefaultLang, "the tree being audited")
	checkTeX := fs.Bool("latex", true, "put every formula through KaTeX")
	quiet := fs.Bool("quiet", false, "print the summary line only")
	warnings := fs.Bool("warnings", true, "print warnings as well as failures")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name an --issue or a --year")
	}

	issues, err := selected(f)
	if err != nil {
		return err
	}
	c, err := corpus.Open(*f.root)
	if err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	toc := &manifest.TOC{}
	if err := store.Read(manifest.TOCFile, toc); err != nil && !errors.Is(err, manifest.ErrMissing) {
		return err
	}

	// Both prompts are current, so a page read by either is not stale. What is
	// stale is a page read by something this build no longer carries.
	in := audit.Input{Prompts: []string{prompt.OCRPageSHA256(), prompt.OCRShortSHA256()}}
	// The same lexicon the reading lane used, so that the audit asks a page the
	// question it was accepted under rather than a stricter one.
	if in.Lexicon, err = lexicon.Open(c.Root); err != nil {
		return err
	}
	if *checkTeX {
		renderer, err := katex.New()
		if err != nil {
			return err
		}
		in.LaTeX = renderer
	}

	bad := 0
	for i := range issues {
		key, err := corpus.ParseIssueKey(issues[i].Key)
		if err != nil {
			return err
		}
		in.Key = key
		in.Sheets = issues[i].Sheets
		in.Rows, _ = toc.Get(issues[i].Key)
		if in.Pages, err = auditPages(c, *lang, key); err != nil {
			return err
		}
		if in.Articles, err = auditArticles(c, *lang, key); err != nil {
			return err
		}

		report := audit.Issue(in)
		if *quiet {
			fmt.Printf("%s: %d pages, %d articles, %s\n",
				report.Issue, report.Pages, report.Articles, report.Counts())
		} else {
			fmt.Print(printable(report, *warnings))
		}
		if !report.OK() {
			bad++
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d issues do not pass", bad, len(issues))
	}
	return nil
}

// printable is the report with the warnings filtered out when they were not
// asked for. A sweep over a whole year wants the failures and nothing else,
// because every issue has two covers and every cover is an orphan page.
func printable(report *audit.Report, warnings bool) string {
	if warnings {
		return report.String()
	}
	kept := report.Findings[:0]
	for _, finding := range report.Findings {
		if finding.Level == audit.Fail {
			kept = append(kept, finding)
		}
	}
	report.Findings = kept
	return report.String()
}

func auditPages(c *corpus.Corpus, lang string, key corpus.IssueKey) ([]audit.Page, error) {
	indexes, err := c.Pages(lang, key)
	if err != nil {
		return nil, err
	}
	pages := make([]audit.Page, 0, len(indexes))
	for _, index := range indexes {
		id := corpus.PageID{Issue: key, Index: index}
		front := &corpus.PageFront{}
		// Unchecked, because a body that no longer matches its hash is a page
		// somebody edited, and the audit should report on what is on disk
		// rather than refuse to look at it. The corpus validator is the command
		// that cares about the hash.
		body, err := corpus.LoadUnchecked(c.PagePath(lang, id), front)
		if err != nil {
			return nil, err
		}
		pages = append(pages, audit.Page{Index: index, Front: *front, Body: body})
	}
	return pages, nil
}

func auditArticles(c *corpus.Corpus, lang string, key corpus.IssueKey) ([]audit.Article, error) {
	dir := filepath.Join(c.IssueDir(lang, key), "articles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	articles := make([]audit.Article, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		front := &corpus.ArticleFront{}
		body, err := corpus.LoadUnchecked(path, front)
		if err != nil {
			return nil, err
		}
		articles = append(articles, audit.Article{Path: path, Front: *front, Body: body})
	}
	return articles, nil
}
