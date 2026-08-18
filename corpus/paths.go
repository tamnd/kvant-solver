package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The layout, written once so that no other package builds a path by hand:
//
//	content/<lang>/<year>/<number>/issue.md
//	content/<lang>/<year>/<number>/pages/NNNN.md
//	content/<lang>/<year>/<number>/articles/NN_slug.md
//	content/<lang>/<year>/<number>/problems/M1234.md
//	content/<lang>/solutions/M1234.md
//
// Language is a directory and not a suffix because a translation is a whole
// tree, and because ru and en then diff against each other file by file.

// DefaultLang is the language a page is read into. Everything else in the
// corpus is derived from it.
const DefaultLang = "ru"

// IssueDir is the directory one issue lives in.
func (c *Corpus) IssueDir(lang string, key IssueKey) string {
	return filepath.Join(c.Root, "content", lang, filepath.FromSlash(key.Dir()))
}

// PagePath is where one page file goes.
func (c *Corpus) PagePath(lang string, id PageID) string {
	return filepath.Join(c.IssueDir(lang, id.Issue), "pages", id.Filename())
}

// HasPage says whether a page has been read into the corpus.
//
// The corpus is the answer to what is finished, not the queue. A page that
// three lanes could not read and a fourth could is done, and the three dead
// jobs behind it are history rather than work.
func (c *Corpus) HasPage(lang string, id PageID) bool {
	_, err := os.Stat(c.PagePath(lang, id))
	return err == nil
}

// ArticlePath is where one article file goes. The ordinal is the article's
// position in the printed contents, and it is in the filename so that a
// directory listing is the table of contents in order. A slug is not enough for
// that: the magazine prints its sections in an order that is not alphabetical
// and does not repeat between issues.
func (c *Corpus) ArticlePath(lang string, id ArticleID, ordinal int) string {
	return filepath.Join(c.IssueDir(lang, id.Issue), "articles",
		fmt.Sprintf("%02d_%s.md", ordinal, id.Slug))
}

// IssuePath is where the masthead and printed contents of an issue go.
func (c *Corpus) IssuePath(lang string, key IssueKey) string {
	return filepath.Join(c.IssueDir(lang, key), "issue.md")
}

// Pages lists the page indexes an issue has on disk, in order.
//
// A missing directory is not an error and comes back empty. Asking what has
// been read so far is the first thing every run does, and on the first run the
// answer is nothing.
func (c *Corpus) Pages(lang string, key IssueKey) ([]int, error) {
	entries, err := os.ReadDir(filepath.Join(c.IssueDir(lang, key), "pages"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var indexes []int
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(name, ".md"))
		if err != nil || n <= 0 {
			continue
		}
		indexes = append(indexes, n)
	}
	sort.Ints(indexes)
	return indexes, nil
}

// Articles lists the article files an issue has on disk, in the order a
// directory listing gives them, which is the printed order because the ordinal
// is the start of the name.
//
// Names rather than ids, because the caller that wants this wants to count them
// and, when the count is wrong, to say which ones are there. A missing
// directory is an issue that has not been assembled and comes back empty.
func (c *Corpus) Articles(lang string, key IssueKey) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(c.IssueDir(lang, key), "articles"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// Missing is the gap between what an issue should have and what it has, which
// is what the runner queues and what the audit reports.
func (c *Corpus) Missing(lang string, key IssueKey, sheets int) ([]int, error) {
	have, err := c.Pages(lang, key)
	if err != nil {
		return nil, err
	}
	present := make(map[int]bool, len(have))
	for _, index := range have {
		present[index] = true
	}
	var missing []int
	for index := 1; index <= sheets; index++ {
		if !present[index] {
			missing = append(missing, index)
		}
	}
	return missing, nil
}
