package corpus

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Corpus is a checkout of tamnd/kvant.
type Corpus struct {
	Root string
}

// Open points at a corpus checkout and checks that it looks like one.
func Open(root string) (*Corpus, error) {
	if root == "" {
		root = os.Getenv("KVANT_CORPUS")
	}
	if root == "" {
		return nil, fmt.Errorf("no corpus path given and KVANT_CORPUS is not set")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filepath.Join(abs, "content"))
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%s does not look like a corpus, it has no content directory", abs)
	}
	return &Corpus{Root: abs}, nil
}

// Report is what a validation run found. Files is everything it read, Problems
// is everything wrong with them, and an empty Problems is the only passing
// result.
type Report struct {
	Pages     int
	Articles  int
	Problems  int
	Solutions int
	Issues    []string
	Findings  []Finding
}

// Finding is one thing wrong, anchored at the file it is wrong in.
type Finding struct {
	Path string
	Err  error
}

// OK reports whether the corpus passed.
func (r *Report) OK() bool { return len(r.Findings) == 0 }

func (r *Report) add(path string, err error) {
	r.Findings = append(r.Findings, Finding{Path: path, Err: err})
}

// String is the one line summary the CLI prints.
func (r *Report) String() string {
	return fmt.Sprintf("%d issues, %d pages, %d articles, %d problems, %d solutions, %d findings",
		len(r.Issues), r.Pages, r.Articles, r.Problems, r.Solutions, len(r.Findings))
}

// Validate reads every file under content, checks its front matter and its
// hash, and then checks the things that are only visible across files: that an
// issue's pages run from one without a gap, and that an article's page range
// lies inside the pages that exist.
//
// It reads the whole tree even after it finds something wrong, because the
// useful output of a validation run is the list and not the first line of it.
func (c *Corpus) Validate() (*Report, error) {
	rep := &Report{}
	contentDir := filepath.Join(c.Root, "content")

	pagesByIssue := map[string][]int{}
	articlesByIssue := map[string][]*ArticleFront{}

	err := filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(c.Root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		switch kind(rel) {
		case "page":
			var f PageFront
			if _, err := Load(path, &f); err != nil {
				rep.add(rel, err)
				return nil
			}
			if err := f.Validate(); err != nil {
				rep.add(rel, err)
				return nil
			}
			rep.Pages++
			pagesByIssue[f.Issue] = append(pagesByIssue[f.Issue], f.PageIndex)
			if want := fmt.Sprintf("%04d.md", f.PageIndex); filepath.Base(rel) != want {
				rep.add(rel, fmt.Errorf("page_index %d wants the file to be called %s", f.PageIndex, want))
			}
		case "article":
			f := &ArticleFront{}
			if _, err := Load(path, f); err != nil {
				rep.add(rel, err)
				return nil
			}
			if err := f.Validate(); err != nil {
				rep.add(rel, err)
				return nil
			}
			rep.Articles++
			articlesByIssue[f.Issue] = append(articlesByIssue[f.Issue], f)
		case "solution":
			var f SolutionFront
			if _, err := Load(path, &f); err != nil {
				rep.add(rel, err)
				return nil
			}
			if err := f.Validate(); err != nil {
				rep.add(rel, err)
				return nil
			}
			rep.Solutions++
		case "problem":
			var f ProblemFront
			if _, err := Load(path, &f); err != nil {
				rep.add(rel, err)
				return nil
			}
			if err := f.Validate(); err != nil {
				rep.add(rel, err)
				return nil
			}
			rep.Problems++
		case "issue":
			// The issue file carries the masthead and the printed TOC. It is
			// checked as an article, since it has the same head.
			f := &ArticleFront{}
			if _, err := Load(path, f); err != nil {
				rep.add(rel, err)
			}
		}
		return nil
	})
	if err != nil {
		return rep, err
	}

	for issue, indexes := range pagesByIssue {
		rep.Issues = append(rep.Issues, issue)
		sort.Ints(indexes)
		if indexes[0] != 1 {
			rep.add(issue, fmt.Errorf("pages start at %d and not at 1", indexes[0]))
		}
		for i := 1; i < len(indexes); i++ {
			if indexes[i] == indexes[i-1] {
				rep.add(issue, fmt.Errorf("page %d appears twice", indexes[i]))
				continue
			}
			if indexes[i] != indexes[i-1]+1 {
				rep.add(issue, fmt.Errorf("pages jump from %d to %d", indexes[i-1], indexes[i]))
			}
		}
		last := indexes[len(indexes)-1]
		for _, a := range articlesByIssue[issue] {
			if a.PageLast > last {
				rep.add(a.ID, fmt.Errorf("article runs to page %d but the issue has %d pages", a.PageLast, last))
			}
			if !slices.Contains(indexes, a.PageFirst) {
				rep.add(a.ID, fmt.Errorf("article starts on page %d, which has no page file", a.PageFirst))
			}
		}
	}
	for issue, arts := range articlesByIssue {
		if _, ok := pagesByIssue[issue]; !ok {
			for _, a := range arts {
				rep.add(a.ID, fmt.Errorf("article is in %s, which has no page files at all", issue))
			}
		}
	}
	sort.Strings(rep.Issues)
	return rep, nil
}

// kind decides what schema a path under content holds, from the path alone.
// The layout is the contract, so a file in the wrong place is not guessed at.
func kind(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "content" {
		return ""
	}
	if parts[1] == "solutions" {
		return "solution"
	}
	for i, p := range parts {
		switch p {
		case "pages":
			return "page"
		case "articles":
			return "article"
		case "problems":
			return "problem"
		}
		if i == len(parts)-1 && p == "issue.md" {
			return "issue"
		}
	}
	return ""
}
