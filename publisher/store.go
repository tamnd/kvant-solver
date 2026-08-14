package publisher

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
)

// Store is where the converted publisher text is kept.
//
// It is in the cache and not in the corpus, because it is evidence about the
// corpus rather than part of it. The corpus holds one reading of each page and
// this is a second reading of some of them, kept so the two can be compared
// without asking the site again, and thrown away with the rest of the cache
// when it stops being interesting.
type Store struct{ Dir string }

// Path is where one article's text goes.
func (s Store) Path(issue, slug string) string {
	return filepath.Join(s.Dir, issue, slug+".md")
}

// Put writes one article's converted text.
func (s Store) Put(issue, slug, text string) error {
	path := s.Path(issue, slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

// Get reads it back. A missing file is not an error: it means this article has
// not been fetched, which is the normal state of most of them.
func (s Store) Get(issue, slug string) (string, bool, error) {
	data, err := os.ReadFile(s.Path(issue, slug))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// Candidates is every article that can be compared: one the archive says it
// carries the text of, in an issue this corpus has assembled.
//
// Both halves are needed and neither is common. About a fifth of the contents
// rows in the archive carry publisher text, and an issue is only assembled once
// every page of it has been read, so the list is short until a decade is
// finished and that is the right way round. There is no point measuring the
// publisher text of a year we cannot check it against.
func Candidates(c *corpus.Corpus, lang string, toc *manifest.TOC, issues []manifest.Issue) ([]Candidate, error) {
	var out []Candidate
	for _, iss := range issues {
		rows, ok := toc.Get(iss.Key)
		if !ok {
			continue
		}
		key, err := corpus.ParseIssueKey(iss.Key)
		if err != nil {
			return nil, err
		}
		files, err := c.Articles(lang, key)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			continue
		}
		for _, row := range rows {
			if !row.HasText || row.Slug == "" || row.URL == "" {
				continue
			}
			file := match(files, row.Slug)
			if file == "" {
				continue
			}
			out = append(out, Candidate{
				Issue: iss.Key,
				Year:  iss.Year,
				Slug:  row.Slug,
				Title: row.Title,
				URL:   row.URL,
				File:  filepath.Join(c.IssueDir(lang, key), "articles", file),
			})
		}
	}
	return sorted(out), nil
}

// match finds the assembled article a contents row became.
//
// The row's slug and the article's differ in one way that matters: the site
// separates the words of a title with underscores and a filename here uses
// dashes throughout, because a corpus where two files differ only in the kind
// of separator is a corpus somebody will mis-copy. The hash at the end of the
// slug is the site's own and carries across both, which is what makes the match
// exact rather than a prefix guess.
func match(files []string, slug string) string {
	want := "_" + strings.ReplaceAll(slug, "_", "-") + ".md"
	for _, file := range files {
		if strings.HasSuffix(file, want) {
			return file
		}
	}
	return ""
}
