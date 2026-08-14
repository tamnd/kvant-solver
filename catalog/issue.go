package catalog

import (
	"context"
	"fmt"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source/kvantmccme"
)

// SyncIssue fills in one issue: its title, its page count, its scan manifest
// and its table of contents. One request to kvant.digital.
//
// The scan manifest is the part worth explaining. The file names are zero
// based, zero padded, and cover backs carry a letter suffix, so no arithmetic
// on a printed page number produces them. The thumbnail strip on the issue page
// names every one of them, which is why this is one request per issue rather
// than a sweep of forty.
func (c *Catalog) SyncIssue(ctx context.Context, iss *manifest.Issue, toc *manifest.TOC, rubrics *manifest.Rubrics) error {
	page, err := c.Digital.Issue(ctx, iss.Year, iss.Number)
	if err != nil {
		return fmt.Errorf("%s: %s: %w", SourceDigital, iss.Key, err)
	}
	iss.Title = page.Title
	iss.Pages = page.PageCount

	rows := make([]manifest.Row, 0, len(page.Rows))
	textRows := 0
	for _, r := range page.Rows {
		if r.HasText {
			textRows++
		}
		rows = append(rows, manifest.Row{
			Rubric:    r.Rubric,
			RubricSub: r.RubricSub,
			Title:     r.Title,
			Authors:   r.Authors,
			Page:      r.Page,
			Sheet:     r.Sheet,
			Slug:      r.Slug,
			URL:       r.URL,
			HasText:   r.HasText,
			Source:    SourceDigital,
		})
		if rubrics != nil {
			rubrics.Observe(r.Rubric, iss.Year)
		}
	}
	if toc != nil {
		toc.Set(iss.Key, rows)
	}
	iss.Sources.Digital = &manifest.Digital{
		URL:      page.URL,
		Rows:     len(rows),
		TextRows: textRows,
	}

	iss.Sheets = len(page.Sheets)
	return nil
}

// SyncPersonalia rebuilds the contributor list from kvant.digital, which is
// the better of the two: it carries full initials and a per person item count.
// The MCCME author list is folded in afterwards under its own slug, because a
// name it has and kvant.digital does not is a real gap rather than a duplicate.
func (c *Catalog) SyncPersonalia(ctx context.Context, withMirror bool) (*manifest.Personalia, error) {
	people, err := c.Digital.Personalia(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", SourceDigital, err)
	}
	out := &manifest.Personalia{}
	for _, p := range people {
		out.Add(manifest.Person{
			Slug:  p.Slug,
			Name:  p.Name,
			URL:   p.URL,
			Items: p.Count(),
		})
	}
	if withMirror {
		authors, err := c.MCCME.Authors(ctx)
		if err != nil {
			return out, fmt.Errorf("%s: %w", SourceMCCME, err)
		}
		for _, a := range authors {
			out.Add(manifest.Person{
				Slug: mirrorSlug(a),
				Name: a.Name,
				URL:  kvantmccme.AuthorURL(a.Slug),
			})
		}
	}
	out.Sort()
	return out, nil
}

// mirrorSlug keeps the mirror's people in a namespace of their own. The two
// sites slug the same person differently, kvant.digital writing two initials
// where the mirror writes one, and pretending otherwise would merge two people
// who share a surname and an initial.
func mirrorSlug(a kvantmccme.Author) string { return "mccme:" + a.Slug }
