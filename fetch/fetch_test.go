package fetch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/source"
	"github.com/tamnd/kvant-solver/source/kvantdigital"
)

// The issue page in the shape the sweep reads it: a thumbnail strip naming
// four sheets, and one contents row, since a page with no rows does not parse.
const issuePage = `<html><head><title>Квант, 1975, № 1</title></head><body>
<div class="swiper--thumbs"><div class="swiper-wrapper">
<div class="swiper-slide"><a href="/view/kvant_1975_1/p0/"><figure><img src="/data/kvant_1975_1/jpg/0000.jpg" alt="Обложка"></figure></a></div>
<div class="swiper-slide"><a href="/view/kvant_1975_1/p1/"><figure><img src="/data/kvant_1975_1/jpg/0001.jpg" alt="Стр. 1"></figure></a></div>
<div class="swiper-slide"><a href="/view/kvant_1975_1/p2/"><figure><img src="/data/kvant_1975_1/jpg/0002.jpg" alt="Стр. 2"></figure></a></div>
<div class="swiper-slide"><a href="/view/kvant_1975_1/p3/"><figure><img src="/data/kvant_1975_1/jpg/0003.jpg" alt="Стр. 3"></figure></a></div>
</div></div>
<ul class="object--toc"><li><div class="toc--item toc--lev--1"><div class="toc--title">
<a href="/issues/1975/1/bronshteyn-ellips-d2e5763b/"><span><em>Бронштейн&nbsp;И.&nbsp;Н.</em> Эллипс</span></a>
</div><span class="toc--page"><a href="/view/kvant_1975_1/p2/">1</a></span></div></li></ul>
</body></html>`

// server stands in for kvant.digital. It serves the four sheets the strip
// names, and answers anything past them the way the real site does, with a
// redirect to an error page and a 200.
func server(t *testing.T) (*httptest.Server, *int) {
	return serverWith(t, nil)
}

// serverWith is the same server with everything it serves behind one wrapper,
// which is how the retry tests make a single request fail once.
func serverWith(t *testing.T, wrap func(http.HandlerFunc) http.HandlerFunc) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/issues/1975/1/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, issuePage)
	})
	mux.HandleFunc("/error", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body>нет такой страницы</body></html>")
	})
	sheets := func(w http.ResponseWriter, r *http.Request) {
		file := strings.TrimSuffix(r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:], ".jpg")
		switch file {
		case "0000", "0001", "0002", "0003":
			hits++
			w.Header().Set("Content-Type", "image/jpeg")
			// The bytes differ per sheet, or every sheet would hash the same
			// and the test would prove nothing about content addressing.
			fmt.Fprintf(w, "not really a jpeg, sheet %s", file)
		default:
			http.Redirect(w, r, "/error", http.StatusMovedPermanently)
		}
	}
	mux.HandleFunc("/data/kvant_1975_1/jpg/", sheets)
	var h http.Handler = mux
	if wrap != nil {
		h = wrap(mux.ServeHTTP)
	}
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv, &hits
}

// flaky wraps a handler so that the first call for one sheet dies with the
// connection half open, the way a tired server does.
func flaky(t *testing.T, file string, h http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	dropped := false
	return func(w http.ResponseWriter, r *http.Request) {
		if !dropped && strings.Contains(r.URL.Path, file) {
			dropped = true
			c, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = c.Close()
			return
		}
		h(w, r)
	}
}

func fetcher(t *testing.T, srv *httptest.Server) *Fetcher {
	t.Helper()
	cache, err := OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := source.NewClient()
	client.Delay = 0
	client.IgnoreRobots = true
	// The requests keep the URLs the fetcher built, down to the host, and only
	// the connection goes to the test server. Rewriting the URL instead would
	// make every response look like a redirect, and a redirect is exactly the
	// signal the sweep reads to find the end of an issue.
	client.HTTP = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, base.Host)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // the test server signs its own
	}}

	f := New(cache, client)
	f.Digital = &kvantdigital.Client{Fetcher: client}
	f.Backoff = 0
	f.Log = func(format string, args ...any) { t.Logf(format, args...) }
	return f
}

func issue() *manifest.Issue {
	return &manifest.Issue{Key: "kvant_1975_1", Year: 1975, Number: "1", Dir: "1975/01"}
}

func TestPagesBringsDownTheWholeScan(t *testing.T) {
	srv, hits := server(t)
	f := fetcher(t, srv)

	idx, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Sheets) != 4 {
		t.Fatalf("got %d sheets, the strip names 4", len(idx.Sheets))
	}
	if idx.Fetched() != 4 {
		t.Errorf("%d of %d sheets have their bytes", idx.Fetched(), len(idx.Sheets))
	}
	// The probe asked for one sheet past the end and got the error page. It
	// must not have kept it.
	if *hits != 4 {
		t.Errorf("%d sheets were served", *hits)
	}
	for _, s := range idx.Sheets {
		if !f.Cache.Has(s.SHA256) {
			t.Errorf("sheet %s hashed to %s and is not in the cache", s.File, s.SHA256)
		}
	}
	// The printed page numbers come off the same strip and are what tells a
	// cover from page one.
	if cover, _ := idx.Get("0000"); cover.Page != 0 {
		t.Errorf("the cover is numbered %d", cover.Page)
	}
	if one, _ := idx.Get("0001"); one.Page != 1 {
		t.Errorf("sheet 0001 is page %d", one.Page)
	}
}

func TestTheErrorPageIsNotKeptAsASheet(t *testing.T) {
	srv, _ := server(t)
	f := fetcher(t, srv)
	if _, err := f.Pages(context.Background(), issue()); err != nil {
		t.Fatal(err)
	}
	files, bytes, err := f.Cache.Size()
	if err != nil {
		t.Fatal(err)
	}
	if files != 4 {
		t.Errorf("the cache holds %d files and %d bytes, the issue has 4 sheets", files, bytes)
	}
}

func TestASecondRunFetchesNothing(t *testing.T) {
	srv, hits := server(t)
	f := fetcher(t, srv)
	if _, err := f.Pages(context.Background(), issue()); err != nil {
		t.Fatal(err)
	}
	before := *hits
	// This is what makes a decade resumable: the second run reads the issue
	// page again, because that is where the sheet list is, and asks for none
	// of the sheets.
	idx, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	if *hits != before {
		t.Errorf("%d sheets were fetched again", *hits-before)
	}
	if idx.Fetched() != 4 {
		t.Errorf("the second run has %d of 4 sheets", idx.Fetched())
	}
}

func TestABlobDeletedFromTheCacheComesBack(t *testing.T) {
	srv, hits := server(t)
	f := fetcher(t, srv)
	idx, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	// The manifest saying a sheet was fetched is not the same as the bytes
	// being there, and the bytes are what the next stage reads.
	if err := os.Remove(f.Cache.Path(idx.Sheets[1].SHA256)); err != nil {
		t.Fatal(err)
	}
	before := *hits
	if _, err := f.Pages(context.Background(), issue()); err != nil {
		t.Fatal(err)
	}
	if *hits != before+1 {
		t.Errorf("%d sheets were fetched, one was missing from the cache", *hits-before)
	}
}

func TestThePathDecisionSurvivesARefetch(t *testing.T) {
	srv, _ := server(t)
	f := fetcher(t, srv)
	idx, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	s, _ := idx.Get("0002")
	s.Path, s.Why = "vision", "no publisher text"
	if err := f.Cache.WriteIndex(idx); err != nil {
		t.Fatal(err)
	}
	again, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	back, _ := again.Get("0002")
	if back.Path != "vision" || back.Why != "no publisher text" {
		t.Errorf("the sweep overwrote the path decision with %q", back.Path)
	}
}

func TestAConnectionThatDropsIsAskedAgain(t *testing.T) {
	srv, _ := serverWith(t, func(h http.HandlerFunc) http.HandlerFunc { return flaky(t, "0002", h) })
	f := fetcher(t, srv)

	idx, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	// The sweep must come back with the whole issue. A reset in the middle of
	// sheet three says nothing about sheet three.
	if idx.Fetched() != 4 {
		t.Errorf("%d of 4 sheets came back after one dropped connection", idx.Fetched())
	}
}

func TestTheSheetListIsAskedForAgainToo(t *testing.T) {
	// The thumbnail strip is one request that the whole issue hangs off. A real
	// sweep of 1975 lost five issues to a host that was unreachable for a few
	// seconds, and every one of them died here rather than on a sheet.
	srv, _ := serverWith(t, func(h http.HandlerFunc) http.HandlerFunc { return flaky(t, "/issues/1975/1/", h) })
	f := fetcher(t, srv)

	idx, err := f.Pages(context.Background(), issue())
	if err != nil {
		t.Fatal(err)
	}
	if idx.Fetched() != 4 {
		t.Errorf("%d of 4 sheets came back after the issue page dropped once", idx.Fetched())
	}
}

func TestARefusalIsNotRetried(t *testing.T) {
	// Asking again after a 403 is how a polite crawler becomes a blocked one.
	if worthRetrying(fmt.Errorf("x: %w", source.ErrForbidden)) {
		t.Error("a refusal was treated as worth another go")
	}
	if worthRetrying(fmt.Errorf("x: %w", source.ErrRateLimited)) {
		t.Error("a rate limit was treated as worth another go")
	}
	if worthRetrying(&source.StatusError{URL: "x", Status: 418}) {
		t.Error("a teapot was treated as worth another go")
	}
	if !worthRetrying(&source.StatusError{URL: "x", Status: 503}) {
		t.Error("a server that is briefly unwell is worth waiting for")
	}
}

func TestTheSameBytesAreStoredOnce(t *testing.T) {
	cache, err := OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, n, err := cache.Put(strings.NewReader("a blank insert page"))
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := cache.Put(strings.NewReader("a blank insert page"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the same bytes hashed to %s and %s", first, second)
	}
	files, bytes, err := cache.Size()
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 || bytes != n {
		t.Errorf("two identical pages became %d files and %d bytes", files, bytes)
	}
}

func TestAnInterruptedPutLeavesNothing(t *testing.T) {
	cache, err := OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = cache.PutFunc(func(w io.Writer) error {
		if _, err := w.Write([]byte("half a page")); err != nil {
			return err
		}
		return errUnplugged
	})
	if !errors.Is(err, errUnplugged) {
		t.Fatalf("got %v", err)
	}
	files, _, err := cache.Size()
	if err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Errorf("a download that died half way left %d files behind", files)
	}
}

var errUnplugged = errors.New("the connection went away")
