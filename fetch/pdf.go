package fetch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
)

// PDF downloads the full issue file the MCCME mirror has for an issue, where
// it has one. It returns a nil blob for an issue the mirror has no PDF of,
// which is most of the archive before 2005.
//
// This is the file the native path is built on. From 2007 the mirror's PDFs are
// born digital, so those issues can be read without a model at all, and
// textguard is what checks that claim year by year rather than taking it.
func (f *Fetcher) PDF(ctx context.Context, iss *manifest.Issue) (*Blob, error) {
	if iss.Sources.MCCME == nil || iss.Sources.MCCME.PDFURL == "" {
		return nil, nil
	}
	idx, err := f.Cache.ReadIndex(iss.Key)
	if err != nil {
		return nil, err
	}
	idx.Issue, idx.Year = iss.Key, iss.Year

	u := iss.Sources.MCCME.PDFURL
	if idx.PDF != nil && idx.PDF.URL == u && f.Cache.Has(idx.PDF.SHA256) {
		return idx.PDF, nil
	}

	sum, n, resp, err := f.download(ctx, u)
	if err != nil {
		if errors.Is(err, source.ErrNotFound) {
			f.Log("%s: the mirror lists a PDF at %s and does not have it", iss.Key, u)
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", iss.Key, err)
	}
	if !isPDF(resp.ContentType) {
		if rerr := f.Cache.remove(sum); rerr != nil {
			return nil, rerr
		}
		return nil, fmt.Errorf("%s: %s came back as %s and not a PDF", iss.Key, u, resp.ContentType)
	}

	idx.PDF = &Blob{URL: u, SHA256: sum, Bytes: n}
	if err := f.Cache.WriteIndex(idx); err != nil {
		return nil, err
	}
	f.Log("%s: %d KB of PDF", iss.Key, n/1024)
	return idx.PDF, nil
}

// ArticlePDF downloads the publisher's own PDF of one item.
//
// The link is a signed one, good for two hours, and it is read off the article
// page at the moment it is used. Nothing stores one. A stored download link is
// a field that is correct in testing, correct for the rest of the afternoon,
// and then quietly wrong for as long as the manifest lives, and the failure it
// produces is a 403 four thousand requests into a run rather than an error
// anybody would connect to the decision that caused it.
func (f *Fetcher) ArticlePDF(ctx context.Context, articleURL, slug string) (*Blob, error) {
	a, err := f.Digital.Article(ctx, articleURL, slug)
	if err != nil {
		return nil, err
	}
	if a.DownloadURL == "" {
		return nil, nil
	}
	sum, n, resp, err := f.download(ctx, a.DownloadURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", slug, err)
	}
	if !isPDF(resp.ContentType) {
		if rerr := f.Cache.remove(sum); rerr != nil {
			return nil, rerr
		}
		return nil, fmt.Errorf("%s: the download link came back as %s, the token has probably expired",
			slug, resp.ContentType)
	}
	// The URL is not recorded. It carries the token, and a token in a manifest
	// is both a stale value and a credential nobody meant to publish.
	return &Blob{URL: kvantdigital.BaseURL + "/rpc/dl/", SHA256: sum, Bytes: n}, nil
}

func isPDF(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/pdf")
}
