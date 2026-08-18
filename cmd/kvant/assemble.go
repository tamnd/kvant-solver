package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/assemble"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/publisher"
)

func runAssemble(args []string) error {
	fs := pflag.NewFlagSet("assemble", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	lang := fs.String("lang", corpus.DefaultLang, "the tree being assembled")
	clean := fs.Bool("clean", true, "remove articles left by an earlier run before writing")
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
	// The text the publisher typed, where kvant publisher pull has fetched it.
	// An empty store is the normal state of most of the archive and costs
	// nothing here, every lookup simply misses.
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	text := publisher.Store{Dir: filepath.Join(cache.Dir, "publisher")}

	for i := range issues {
		key, err := corpus.ParseIssueKey(issues[i].Key)
		if err != nil {
			return err
		}
		pages, read, err := readPages(c, *lang, key)
		if err != nil {
			return err
		}
		rows, _ := toc.Get(issues[i].Key)
		result := assemble.Issue(key, rows, pages)

		if *clean {
			if err := clearArticles(c, *lang, key); err != nil {
				return err
			}
		}
		typed := 0
		for _, article := range result.Articles {
			body, extraction, why := preferTyped(text, key, article, read)
			if why != "" {
				fmt.Printf("  publisher_fragment %s %s\n", article.Slug, why)
			}
			if extraction == corpus.ExtractionPublisher {
				typed++
			}
			article.Body = body
			if err := writeArticle(c, *lang, key, &issues[i], article, extraction); err != nil {
				return err
			}
		}
		fmt.Printf("%s: %d pages, %d articles, %d of them the publisher's own text, %d orphans, %d notes\n",
			issues[i].Key, len(pages), len(result.Articles), typed, len(result.Orphans), len(result.Notes))
		for _, note := range result.Notes {
			fmt.Printf("  %s %s %s\n", note.Kind, note.Subject, note.Detail)
		}
	}
	return nil
}

// readPages loads the transcribed pages of one issue.
//
// The bodies are loaded unchecked, which is deliberate. A page whose content
// hash no longer matches its front matter is a page somebody edited by hand,
// and the right thing to do with an edited page is assemble it and let the
// corpus validator be the one that complains, rather than refusing to build the
// issue at all.
//
// It also returns how each page was read, so that an article can say the same
// thing about itself as the pages it is made of.
func readPages(c *corpus.Corpus, lang string, key corpus.IssueKey) ([]assemble.Page, map[int]string, error) {
	indexes, err := c.Pages(lang, key)
	if err != nil {
		return nil, nil, err
	}
	pages := make([]assemble.Page, 0, len(indexes))
	read := make(map[int]string, len(indexes))
	for _, index := range indexes {
		id := corpus.PageID{Issue: key, Index: index}
		front := &corpus.PageFront{}
		body, err := corpus.LoadUnchecked(c.PagePath(lang, id), front)
		if err != nil {
			return nil, nil, err
		}
		pages = append(pages, assemble.Page{Index: index, Label: front.PageLabel, Body: body})
		read[index] = front.Extraction
	}
	return pages, read, nil
}

// preferTyped decides which body an article gets and what it says it is.
//
// An article the publisher typed is not read off anything, so it is not a
// vision reading or a native one whatever its pages were, and the extraction it
// records is its own. The pages underneath keep saying how they were read,
// which is the point of pages being the ground truth: the article can change
// its mind about where its text comes from without any of that being lost.
func preferTyped(store publisher.Store, key corpus.IssueKey, article assemble.Article, read map[int]string) (body, extraction, why string) {
	if article.RowSlug == "" {
		return article.Body, extractionOf(article, read), ""
	}
	text, ok, err := store.Get(key.String(), article.RowSlug)
	if err != nil || !ok {
		// A store that will not read is a cache problem and not a corpus
		// problem, and the assembled body is right there. Failing the whole
		// issue over it would make the better source the fragile one.
		return article.Body, extractionOf(article, read), ""
	}
	chosen, why := publisher.Prefer(article.Body, text)
	if why != "" || chosen != text {
		return article.Body, extractionOf(article, read), why
	}
	return text, corpus.ExtractionPublisher, ""
}

// writeArticle puts one assembled article in the corpus.
func writeArticle(c *corpus.Corpus, lang string, key corpus.IssueKey, iss *manifest.Issue, article assemble.Article, extraction string) error {
	id, err := corpus.NewArticleID(key, article.Slug)
	if err != nil {
		return err
	}
	front := &corpus.ArticleFront{
		ID:         id.String(),
		Issue:      key.String(),
		Year:       key.Year,
		Number:     key.Number,
		Title:      article.Title,
		Authors:    article.Authors,
		Rubric:     article.Rubric,
		RubricSub:  article.RubricSub,
		PageFirst:  article.First,
		PageLast:   article.Last,
		PageLabels: article.Labels,
		Provenance: corpus.Provenance{
			Lang:       lang,
			Source:     scanSource(iss),
			Extraction: extraction,
		},
	}
	return corpus.Save(c.ArticlePath(lang, id, article.Ordinal), front, article.Body)
}

// extractionOf is how the article was read, which is how its pages were read.
//
// An issue can mix the three paths, and an article that spans a native page and
// a scanned one is honestly neither. Those say nothing rather than picking the
// majority, because the page files are still there and can be asked.
func extractionOf(article assemble.Article, read map[int]string) string {
	first := ""
	for i, index := range article.Pages {
		kind := read[index]
		if i == 0 {
			first = kind
			continue
		}
		if kind != first {
			return ""
		}
	}
	return first
}

// clearArticles removes what an earlier assembly wrote.
//
// Assembly is a pure function of the pages and the contents, so it is rerun
// whenever either changes, and a rerun after a row was renamed would otherwise
// leave the old file next to the new one. The pages are never touched: those
// cost a model call each and this command only ever reads them.
func clearArticles(c *corpus.Corpus, lang string, key corpus.IssueKey) error {
	dir := filepath.Join(c.IssueDir(lang, key), "articles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
