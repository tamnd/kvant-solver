// Package catalog turns what the three archives say into the manifests under
// manifests/. It is the only package that knows all three sources at once, and
// it is where a disagreement between them becomes either a resolved fact or an
// entry in errata.yaml.
package catalog

import (
	"context"
	"fmt"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
	"github.com/tamnd/kvant-solver/source/kvantmccme"
	"github.com/tamnd/kvant-solver/source/mathnetru"
)

// The names the sources go by in errata and reports.
const (
	SourceDigital = "kvant.digital"
	SourceMCCME   = "kvant.mccme.ru"
	SourceMathNet = "mathnet.ru"
)

// Digital is the part of the kvant.digital client this package uses.
type Digital interface {
	IssuesIndex(ctx context.Context) ([]kvantdigital.IssueRef, error)
	Issue(ctx context.Context, year int, number string) (*kvantdigital.Issue, error)
	Personalia(ctx context.Context) ([]kvantdigital.Person, error)
}

// MCCME is the part of the MCCME mirror client this package uses.
type MCCME interface {
	Archive(ctx context.Context) (*kvantmccme.Archive, error)
	Contents(ctx context.Context, year int, month string) (*kvantmccme.Contents, error)
	ContentsByTitle(ctx context.Context, year int, month string) (*kvantmccme.Contents, error)
	Authors(ctx context.Context) ([]kvantmccme.Author, error)
}

// MathNet is the part of the mathnet.ru client this package uses.
type MathNet interface {
	Contents(ctx context.Context) ([]mathnetru.IssueRef, error)
}

// Catalog holds the three sources.
type Catalog struct {
	Digital Digital
	MCCME   MCCME
	MathNet MathNet

	// archive is the mirror's site map, fetched at most once per run. Every
	// mirror URL in the manifests comes from it.
	archive *kvantmccme.Archive
}

// Archive fetches the mirror's site map once and keeps it. A deep sync asks per
// issue and would otherwise refetch a page that does not change.
func (c *Catalog) Archive(ctx context.Context) (*kvantmccme.Archive, error) {
	if c.archive != nil {
		return c.archive, nil
	}
	a, err := c.MCCME.Archive(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", SourceMCCME, err)
	}
	c.archive = a
	return a, nil
}

// New wires the three real clients onto one fetcher, so the politeness of the
// whole run is decided in one place.
func New() *Catalog { return NewWith(source.NewClient()) }

// NewWith is New with a fetcher the caller has already set up, which is how the
// command passes its delay down.
func NewWith(fetcher *source.Client) *Catalog {
	return &Catalog{
		Digital: &kvantdigital.Client{Fetcher: fetcher},
		MCCME:   &kvantmccme.Client{Fetcher: fetcher},
		MathNet: &mathnetru.Client{Fetcher: fetcher},
	}
}

// SyncIssues builds the issue list. It is two requests: kvant.digital lists
// the whole archive on one page, and mathnet lists everything it holds on
// another. Nothing here is inferred, and no URL is written down that a source
// did not name, because a manifest full of plausible URLs is worse than a
// short one.
func (c *Catalog) SyncIssues(ctx context.Context, errata *manifest.Errata) (*manifest.Issues, error) {
	refs, err := c.Digital.IssuesIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", SourceDigital, err)
	}

	out := &manifest.Issues{}
	for _, ref := range refs {
		iss, err := manifest.NewIssue(ref.Year, ref.Number)
		if err != nil {
			// A number that is not a printed number is a change in the site,
			// not a fact about the magazine, and it is worth saying so out
			// loud rather than filing it under a made up key.
			errata.Add(manifest.Erratum{
				Issue:   ref.Key,
				Kind:    "number",
				Subject: ref.Number,
				Claims:  map[string]string{SourceDigital: err.Error()},
			})
			continue
		}
		iss.Sources.Digital = &manifest.Digital{URL: ref.URL}
		out.Add(iss)
	}
	if len(out.Issues) == 0 {
		return nil, fmt.Errorf("%s: the index gave no usable issues", SourceDigital)
	}

	if err := c.addMirror(ctx, out, errata); err != nil {
		return nil, err
	}

	mn, err := c.MathNet.Contents(ctx)
	if err != nil {
		// mathnet is a second opinion on twenty of the fifty years. Losing it
		// is worth reporting and is not worth failing the run for.
		errata.Add(manifest.Erratum{
			Kind: "source_unavailable",
			// Named in the subject as well as in the claims, because an entry
			// is matched on its issue, kind and subject, and two outages with
			// none of the three filled in are the same entry. The mirror going
			// down and then mathnet going down wrote one line, not two.
			Subject: SourceMathNet,
			Claims:  map[string]string{SourceMathNet: err.Error()},
		})
		out.Sort()
		return out, nil
	}
	for _, ref := range mn {
		iss, ok := findIssue(out, ref.Year, ref.Number)
		if !ok {
			errata.Add(manifest.Erratum{
				Issue:   fmt.Sprintf("kvant_%d_%s", ref.Year, ref.Number),
				Kind:    "missing_issue",
				Subject: ref.Number,
				Claims: map[string]string{
					SourceMathNet: "lists this issue",
					SourceDigital: "does not",
				},
			})
			continue
		}
		iss.Sources.MathNet = &manifest.MathNet{
			URL:      ref.URL,
			Number:   ref.Number,
			FullText: ref.FullText,
		}
		if iss.Number != ref.Number {
			// One source calling an issue 5 and the other calling it 5-6 is a
			// real finding: it means one of them split a double issue.
			errata.Add(manifest.Erratum{
				Issue:   iss.Key,
				Kind:    "number",
				Subject: iss.Number,
				Claims: map[string]string{
					SourceDigital: iss.Number,
					SourceMathNet: ref.Number,
				},
			})
		}
	}
	out.Sort()
	return out, nil
}

// findIssue matches an issue across sources. Matching is on the year and the
// first month the number covers, because that is the one thing the sources
// agree on: a double issue is 5-6 on one site and 5 on another, and both mean
// the same paper object.
func findIssue(m *manifest.Issues, year int, number string) (*manifest.Issue, bool) {
	first := manifest.FirstNumber(number)
	for i := range m.Issues {
		if m.Issues[i].Year == year && manifest.FirstNumber(m.Issues[i].Number) == first {
			return &m.Issues[i], true
		}
	}
	return nil, false
}
