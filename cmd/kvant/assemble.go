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

		// Read before the clear, because the clear is what would destroy it.
		onDisk := keptOnDisk(c, *lang, key)
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
			had := onDisk[articleID(key, article.Slug)]
			if extraction != corpus.ExtractionPublisher && had.typed != "" {
				fmt.Printf("  kept_publisher_text %s the cache has none for it and what this issue already holds is the publisher's own\n", article.Slug)
				body, extraction = had.typed, corpus.ExtractionPublisher
			}
			if extraction == corpus.ExtractionPublisher {
				typed++
			}
			article.Body = body
			if err := writeArticle(c, *lang, key, &issues[i], article, extraction, had.tag); err != nil {
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
	return publisher.Titled(article.Title, text), corpus.ExtractionPublisher, ""
}

// kept is what an article already on disk carries that a fresh assembly has no
// way of working out for itself.
type kept struct {
	// typed is the publisher's own text, and is empty unless the article says
	// that is what it holds.
	typed string

	// tag is the identifier kvant tags assign gave the article.
	tag corpus.Tag
}

// keptOnDisk is what this issue's articles already carry, by article id.
//
// Assembly is a pure function of the pages and the contents, and these two
// fields are neither. The publisher's text comes out of a cache assemble is
// given the path of, so a run pointed at a different cache, or at one nobody
// has pulled into, finds nothing and falls back to the reading of the pages.
// For an article being built the first time that is the right answer. For one
// that already holds the publisher's own text it is the wrong one, because a
// model reading a scan cannot beat the text it is a reading of.
//
// Reassembling an issue to correct a byline used to replace the better source
// with the worse one and say nothing about it, which is how two articles in
// 1975 lost their ё, their formulas, and the markers saying which parts of them
// had not been read at all.
//
// The tag is there for the same reason and was a worse leak, because it hit
// every article rather than the forty nine the publisher has typed. A tag is
// assigned once and then cited, so it has to survive the article being built
// again. Rebuilding the front matter from scratch dropped all of them, and the
// standing workaround was to remember to run kvant tags assign afterwards and
// watch it report a few hundred files rewritten. Anybody who forgot published a
// corpus whose articles had no identifier at all.
//
// It is keyed by id rather than by filename because the ordinal is in the
// filename, and a row moving up the contents must not lose either field with
// it.
func keptOnDisk(c *corpus.Corpus, lang string, key corpus.IssueKey) map[string]kept {
	out := map[string]kept{}
	names, err := c.Articles(lang, key)
	if err != nil {
		return out
	}
	dir := filepath.Join(c.IssueDir(lang, key), "articles")
	for _, name := range names {
		front := &corpus.ArticleFront{}
		body, err := corpus.LoadUnchecked(filepath.Join(dir, name), front)
		if err != nil {
			continue
		}
		had := kept{tag: front.Tag}
		if front.Extraction == corpus.ExtractionPublisher {
			had.typed = body
		}
		out[front.ID] = had
	}
	return out
}

// articleID is the name an article goes by, or "" for one that cannot have a
// name, which is a row assemble is about to complain about anyway.
func articleID(key corpus.IssueKey, slug string) string {
	id, err := corpus.NewArticleID(key, slug)
	if err != nil {
		return ""
	}
	return id.String()
}

// writeArticle puts one assembled article in the corpus. The tag is whatever
// the article already carried, because assembly does not mint one.
func writeArticle(c *corpus.Corpus, lang string, key corpus.IssueKey, iss *manifest.Issue, article assemble.Article, extraction string, tag corpus.Tag) error {
	id, err := corpus.NewArticleID(key, article.Slug)
	if err != nil {
		return err
	}
	front := &corpus.ArticleFront{
		ID:         id.String(),
		Tag:        tag,
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
