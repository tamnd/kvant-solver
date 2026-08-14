package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantmccme"
)

// The two mirror orderings, named separately because that is the whole point
// of reading both.
const (
	SourceMCCMEAuthor = "kvant.mccme.ru by author"
	SourceMCCMETitle  = "kvant.mccme.ru by title"
)

// addMirror folds the mirror's site map into the issue list. It is one request
// for the whole archive, so it runs on every sync rather than only on a deep
// one, and every URL it writes is one the mirror published.
//
// The mirror is the smaller of the two collections and holds nothing that
// kvant.digital does not, so an issue it lists and kvant.digital does not is a
// finding rather than a new row.
func (c *Catalog) addMirror(ctx context.Context, out *manifest.Issues, errata *manifest.Errata) error {
	archive, err := c.Archive(ctx)
	if err != nil {
		// The mirror carries the typeset text for 1970 to 2003, which is a
		// third of the archive, but the run can still write what it has.
		errata.Add(manifest.Erratum{
			Kind:   "source_unavailable",
			Claims: map[string]string{SourceMCCME: err.Error()},
		})
		return nil
	}
	for _, ref := range archive.Refs {
		iss, ok := findIssue(out, ref.Year, ref.Number)
		if !ok {
			errata.Add(unmatched(out, ref))
			continue
		}
		// The number is deliberately not compared here. All the mirror gives is
		// a file name, and 2016-05u.pdf holds the double issue 5-6, so a
		// disagreement with it would be a fact about a file rather than about
		// the magazine. mathnet prints its numbers in words, and that is where
		// the number check belongs.
		iss.Sources.MCCME = mirrorSource(ref)
	}
	return nil
}

// unmatched decides what a mirror issue with no counterpart actually means.
//
// A year the mirror has and kvant.digital does not is a missing issue. A year
// they both have but number differently is not: 1993 came out as four double
// issues, kvant.digital calls them 1-2, 3-4, 9-10 and 11-12, and the mirror
// files them as 01, 02, 05 and 06. Calling that three missing issues would be
// wrong about the magazine and would hide the numbering question underneath it.
func unmatched(out *manifest.Issues, ref kvantmccme.ArchiveRef) manifest.Erratum {
	key := fmt.Sprintf("kvant_%d_%s", ref.Year, ref.Number)
	inYear := out.Year(ref.Year)
	if len(inYear) == 0 {
		return manifest.Erratum{
			Issue:   key,
			Kind:    "missing_issue",
			Subject: ref.Number,
			Claims: map[string]string{
				SourceMCCME:   "lists this issue",
				SourceDigital: "does not have this year at all",
			},
		}
	}
	numbers := make([]string, 0, len(inYear))
	for _, iss := range inYear {
		numbers = append(numbers, iss.Number)
	}
	return manifest.Erratum{
		Issue:   key,
		Kind:    "number",
		Subject: ref.Number,
		Claims: map[string]string{
			SourceMCCME:   fmt.Sprintf("files this issue under %s", ref.Number),
			SourceDigital: fmt.Sprintf("numbers the year %s", strings.Join(numbers, ", ")),
		},
	}
}

func mirrorSource(ref kvantmccme.ArchiveRef) *manifest.MCCME {
	return &manifest.MCCME{
		URL:        ref.URL,
		ByTitleURL: ref.ByTitleURL,
		PDFURL:     ref.PDFURL,
		DjVuURL:    ref.DjVuURL,
	}
}

// SyncMirrorTOC reads one issue from the mirror twice, once in each of the two
// orderings it generates, and does two things with the answer.
//
// It fills in the page ranges. kvant.digital gives a contents row a single
// starting page, and the mirror gives the whole range, which is what the
// assembler later needs to know where an article stops.
//
// And it compares the two orderings against each other. They are generated
// separately from the same data, so a title in one and not in the other is a
// fault in the mirror rather than a fact about the magazine, and it goes to
// errata instead of into the manifest.
//
// An issue the mirror has no contents page for is not an error. It typeset
// 1970 to 2003 and has nothing but PDFs after that.
func (c *Catalog) SyncMirrorTOC(ctx context.Context, iss *manifest.Issue, toc *manifest.TOC, errata *manifest.Errata) error {
	archive, err := c.Archive(ctx)
	if err != nil {
		return err
	}
	ref, ok := archive.Get(iss.Year, iss.Number)
	if !ok {
		return nil
	}
	if iss.Sources.MCCME == nil {
		iss.Sources.MCCME = mirrorSource(ref)
	}
	if ref.URL == "" || ref.ByTitleURL == "" {
		return nil
	}

	month := manifest.Month(iss.Number)
	byAuthor, err := c.MCCME.Contents(ctx, iss.Year, month)
	if err != nil {
		if errors.Is(err, source.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("%s: %s: %w", SourceMCCME, iss.Key, err)
	}

	haveTitle := true
	byTitle, err := c.MCCME.ContentsByTitle(ctx, iss.Year, month)
	if err != nil {
		if !errors.Is(err, source.ErrNotFound) {
			return fmt.Errorf("%s: %s by title: %w", SourceMCCME, iss.Key, err)
		}
		haveTitle = false
		// One ordering answering and the other not is itself worth writing
		// down, and the run carries on with the ordering that answered.
		errata.Add(manifest.Erratum{
			Issue: iss.Key,
			Kind:  "toc_ordering",
			Claims: map[string]string{
				SourceMCCMEAuthor: fmt.Sprintf("%d rows", len(byAuthor.Entries)),
				SourceMCCMETitle:  "no such page",
			},
		})
		byTitle = &kvantmccme.Contents{Year: iss.Year, Month: month}
	}

	// A missing page is one erratum, not one per row on the page that answered.
	var diffs []titleDiff
	if haveTitle {
		diffs = diffTitles(byAuthor, byTitle)
	}
	for _, e := range diffs {
		errata.Add(manifest.Erratum{
			Issue:   iss.Key,
			Kind:    "toc_row",
			Subject: e.title,
			Claims: map[string]string{
				SourceMCCMEAuthor: has(e.inAuthor),
				SourceMCCMETitle:  has(e.inTitle),
			},
		})
	}

	if toc != nil {
		fillPages(toc, iss.Key, byAuthor)
	}
	iss.Sources.MCCME.Rows = len(byAuthor.Entries)
	iss.Sources.MCCME.TitleRows = len(byTitle.Entries)
	return nil
}

type titleDiff struct {
	title    string
	inAuthor bool
	inTitle  bool
}

// diffTitles reports the titles the two orderings disagree about, in the
// ordering of the author page so the errata file reads in issue order.
func diffTitles(byAuthor, byTitle *kvantmccme.Contents) []titleDiff {
	author := count(byAuthor)
	title := count(byTitle)

	var out []titleDiff
	seen := map[string]bool{}
	for _, e := range byAuthor.Entries {
		k := titleKey(e.Title)
		if seen[k] || author[k] == title[k] {
			continue
		}
		seen[k] = true
		out = append(out, titleDiff{title: e.Title, inAuthor: true, inTitle: title[k] > 0})
	}
	for _, e := range byTitle.Entries {
		k := titleKey(e.Title)
		if seen[k] || author[k] == title[k] {
			continue
		}
		seen[k] = true
		out = append(out, titleDiff{title: e.Title, inAuthor: author[k] > 0, inTitle: true})
	}
	return out
}

func count(c *kvantmccme.Contents) map[string]int {
	out := map[string]int{}
	for _, e := range c.Entries {
		out[titleKey(e.Title)]++
	}
	return out
}

// fillPages copies the mirror's page ranges onto the rows kvant.digital gave.
// Titles are matched after folding case, ё and punctuation, and a row the
// mirror does not have keeps the single page it came with rather than being
// dropped: kvant.digital is the more complete of the two.
func fillPages(toc *manifest.TOC, key string, mirror *kvantmccme.Contents) {
	rows, ok := toc.Get(key)
	if !ok {
		return
	}
	pages := map[string][]int{}
	for _, e := range mirror.Entries {
		if len(e.Pages) > 0 {
			pages[titleKey(e.Title)] = e.Pages
		}
	}
	for i := range rows {
		if p, ok := pages[titleKey(rows[i].Title)]; ok {
			rows[i].Pages = p
		}
	}
	toc.Set(key, rows)
}

// titleKey folds the differences that are typography rather than content. The
// two sites quote differently, one writes ё where the other writes е, and the
// mirror sometimes runs two spaces together.
func titleKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'ё':
			b.WriteRune('е')
		case '«', '»', '"', '\'', '.', ',':
			// dropped
		case '–', '—', '-':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func has(b bool) string {
	if b {
		return "has this row"
	}
	return "does not have this row"
}
