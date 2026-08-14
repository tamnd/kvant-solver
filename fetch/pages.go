package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
)

// Fetcher downloads what the sources named into a cache.
type Fetcher struct {
	Cache   *Cache
	Client  *source.Client
	Digital *kvantdigital.Client

	// Probe asks for sheets past the end of the thumbnail strip, in case the
	// strip is short. See Pages.
	Probe bool

	// Retries is how many times a sheet is asked for again after the transfer
	// broke, and Backoff is how long the first wait is. A sweep of a decade is
	// four thousand requests to a volunteer run server and some of them will
	// end in a reset that means nothing at all; giving up on the issue for one
	// of those is how a decade never finishes.
	Retries int
	Backoff time.Duration

	// Log is where progress goes. A fetch of a decade runs for hours and a run
	// that prints nothing is a run nobody can tell apart from a hung one.
	Log func(format string, args ...any)
}

// New returns a fetcher on a cache and a client.
func New(cache *Cache, client *source.Client) *Fetcher {
	return &Fetcher{
		Cache:   cache,
		Client:  client,
		Digital: &kvantdigital.Client{Fetcher: client},
		Probe:   true,
		Retries: DefaultRetries,
		Backoff: DefaultBackoff,
		Log:     func(string, ...any) {},
	}
}

// What a broken transfer costs before the sweep gives up on a sheet. The waits
// grow, so five tries at five seconds is a minute and a quarter of patience. A
// sweep of 1975 met a host that went away for about that long, twice, and three
// tries at three seconds was not enough to sit it out.
const (
	DefaultRetries = 5
	DefaultBackoff = 5 * time.Second
)

// MaxProbe caps how far past the last named sheet the probe will look. It is
// small on purpose: the probe is a check that the thumbnail strip was complete,
// not a search.
const MaxProbe = 4

// Pages downloads the whole scan of one issue and returns its page manifest.
//
// The sheet list comes off the issue page, which is one request and is also
// where the printed page numbers are. Nothing here builds a scan URL out of
// arithmetic on a page number: the file names are not the page numbers, the
// covers hang off their neighbours with a letter suffix, and a URL this project
// invented is a URL no source ever offered.
//
// After the named sheets, the sweep asks for a few more by number. The site
// answers a sheet past the end of an issue with a redirect to an error page
// rather than a 404, so anything that comes back as HTML, or comes back from
// somewhere other than where it was asked for, is the end. This costs one extra
// request per issue and normally finds nothing, which is the point: it is how
// the run learns that the strip was the whole scan rather than assuming it.
//
// It is resumable. A sheet whose bytes are already in the cache is not fetched
// again, so a run that stops half way through a decade picks up where it left
// off rather than starting the decade again.
func (f *Fetcher) Pages(ctx context.Context, iss *manifest.Issue) (*Index, error) {
	idx, err := f.Cache.ReadIndex(iss.Key)
	if err != nil {
		return nil, err
	}
	idx.Issue, idx.Year = iss.Key, iss.Year

	// The sheet list is one request and the whole issue hangs off it, so it gets
	// the same patience a sheet does. Losing an issue of a decade because the
	// host was unreachable for four seconds is not worth the one request saved.
	var page *kvantdigital.Issue
	err = f.retry(ctx, func() error {
		var perr error
		page, perr = f.Digital.Issue(ctx, iss.Year, iss.Number)
		return perr
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", iss.Key, err)
	}
	for _, s := range page.Sheets {
		idx.Set(Sheet{Ord: s.Ord, File: s.File, Page: s.Page})
	}

	got, skipped := 0, 0
	for i := range idx.Sheets {
		s := &idx.Sheets[i]
		if s.Have() && f.Cache.Has(s.SHA256) {
			skipped++
			continue
		}
		blob, err := f.sheet(ctx, iss.Key, s.File)
		if err != nil {
			// Whatever has been fetched is written down before the error goes
			// up, so an issue that fails on sheet sixty is sixty sheets closer
			// to done than it was.
			if werr := f.Cache.WriteIndex(idx); werr != nil {
				return idx, errors.Join(err, werr)
			}
			return idx, err
		}
		if blob == nil {
			// A named sheet that is not there is worth saying out loud. It is
			// not fatal: the rest of the issue is still worth having.
			f.Log("%s: sheet %s is listed and not there", iss.Key, s.File)
			continue
		}
		s.Blob = *blob
		got++
	}

	if f.Probe {
		found, err := f.probeUpward(ctx, idx)
		if err != nil {
			return idx, err
		}
		got += found
	}

	if err := f.Cache.WriteIndex(idx); err != nil {
		return idx, err
	}
	f.Log("%s: %d sheets, %d fetched, %d already here", iss.Key, len(idx.Sheets), got, skipped)
	return idx, nil
}

// probeUpward asks for sheets numbered past the last one the site named.
func (f *Fetcher) probeUpward(ctx context.Context, idx *Index) (int, error) {
	last := lastNumbered(idx)
	if last < 0 {
		return 0, nil
	}
	found := 0
	for n := last + 1; n <= last+MaxProbe; n++ {
		file := fmt.Sprintf("%04d", n)
		if _, ok := idx.Get(file); ok {
			continue
		}
		blob, err := f.sheet(ctx, idx.Issue, file)
		if err != nil {
			return found, err
		}
		if blob == nil {
			return found, nil
		}
		f.Log("%s: sheet %s is not in the thumbnail strip and exists", idx.Issue, file)
		idx.Set(Sheet{Ord: idx.LastOrd() + 1, File: file, Blob: *blob})
		found++
	}
	return found, nil
}

// sheet downloads one scan. A sheet that is not there comes back as a nil blob
// and no error, because walking off the end of an issue is the expected way for
// a sweep to finish rather than a failure.
func (f *Fetcher) sheet(ctx context.Context, issueKey, file string) (*Blob, error) {
	u := kvantdigital.ScanURL(issueKey, file)
	sum, n, resp, err := f.download(ctx, u)
	switch {
	case errors.Is(err, source.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	// The site redirects a request for a sheet past the end of an issue to an
	// error page and answers 200, so the status alone says nothing.
	if resp.Redirected() || !isImage(resp.ContentType) {
		if rerr := f.Cache.remove(sum); rerr != nil {
			return nil, rerr
		}
		return nil, nil
	}
	return &Blob{URL: u, SHA256: sum, Bytes: n}, nil
}

// download streams a URL into the cache and returns what it hashed to, asking
// again when the transfer breaks in a way that says nothing about the file.
func (f *Fetcher) download(ctx context.Context, u string) (sum string, n int64, resp *source.Response, err error) {
	err = f.retry(ctx, func() error {
		var gerr error
		sum, n, resp, gerr = f.get(ctx, u)
		return gerr
	})
	if err != nil {
		return "", 0, nil, err
	}
	return sum, n, resp, nil
}

// retry runs one request until it works, until the error turns out to be an
// answer rather than an accident, or until the waiting has gone on long enough.
func (f *Fetcher) retry(ctx context.Context, try func() error) error {
	for attempt := 0; ; attempt++ {
		err := try()
		if err == nil || attempt >= f.Retries || ctx.Err() != nil || !worthRetrying(err) {
			return err
		}
		// The error names the URL already, in every shape it comes in.
		f.Log("%v, asking again", err)
		if werr := f.pause(ctx, time.Duration(attempt+1)*f.Backoff); werr != nil {
			return werr
		}
	}
}

// get is one attempt. A transfer that breaks half way leaves nothing behind:
// the bytes go to a temporary file that is only named once the hash is known.
func (f *Fetcher) get(ctx context.Context, u string) (sum string, n int64, resp *source.Response, err error) {
	sum, n, err = f.Cache.PutFunc(func(w io.Writer) error {
		var derr error
		resp, _, derr = f.Client.Download(ctx, u, w)
		return derr
	})
	return sum, n, resp, err
}

// worthRetrying separates a connection that dropped from an answer. A 403 or a
// 429 is the server telling us to stop and asking again is the rudest possible
// response to it, and a 404 is an answer that will not change.
//
// A timeout is on the retrying side of the line. It usually means a transfer
// stalled rather than that the file is a problem, and whether the run itself is
// still wanted is a question for the context rather than for the error.
func worthRetrying(err error) bool {
	switch {
	case err == nil:
		return false
	case source.Fatal(err):
		return false
	case errors.Is(err, source.ErrNotFound), errors.Is(err, source.ErrDisallowed):
		return false
	}
	var status *source.StatusError
	if errors.As(err, &status) {
		// A server that is briefly unwell is worth waiting for. Anything else
		// it says on purpose is not.
		return status.Status >= 500
	}
	return true
}

func (f *Fetcher) pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// lastNumbered is the highest sheet whose file name is a plain number, which is
// where the probe starts. The cover backs are named after the sheet they sit
// behind, so they say nothing about how far the scan runs.
func lastNumbered(idx *Index) int {
	last := -1
	for _, s := range idx.Sheets {
		if n, err := strconv.Atoi(s.File); err == nil {
			last = max(last, n)
		}
	}
	return last
}

func isImage(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/")
}
