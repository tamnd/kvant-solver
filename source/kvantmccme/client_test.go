package kvantmccme

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tamnd/kvant-solver/source"
)

func testClient() *Client {
	f := source.NewClient()
	f.Delay = 0
	// A test server answers every path, robots.txt included, so leaving the
	// check on would have this assert something about robots by accident.
	f.IgnoreRobots = true
	return &Client{Fetcher: f}
}

func TestAPageThatLeavesTheMirrorSaysTheMirrorIsGone(t *testing.T) {
	// The successor site, which answers with a page of its own rather than a
	// 404. That is what makes this worth detecting: the request succeeds.
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>somebody else's front page</body></html>"))
	}))
	defer elsewhere.Close()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/", http.StatusMovedPermanently)
	}))
	defer mirror.Close()

	_, err := testClient().get(t.Context(), mirror.URL+"/1972/01/")
	if !errors.Is(err, ErrRetired) {
		t.Fatalf("a redirect off the host gave %v", err)
	}
}

func TestARedirectWithinTheMirrorIsOrdinary(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/1972/01/index.htm", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>the contents</body></html>"))
	})
	mux.HandleFunc("/1972/01/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/1972/01/index.htm", http.StatusMovedPermanently)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The mirror has always redirected a directory to its index page, so
	// treating any redirect as the site being gone would break every request.
	resp, err := testClient().get(t.Context(), srv.URL+"/1972/01/")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Body) == 0 {
		t.Error("the page came back empty")
	}
}

func TestMovedOff(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{
		{"https://kvant.mccme.ru/1972/01/", "https://kvant.digital/", true},
		{"https://kvant.mccme.ru/1972/01/", "https://kvant.mccme.ru/1972/01/index.htm", false},
		{"https://kvant.mccme.ru/x", "HTTPS://KVANT.MCCME.RU/x", false},
		{"://not a url", "https://kvant.digital/", false},
	} {
		if got := movedOff(tc.from, tc.to); got != tc.want {
			t.Errorf("%s -> %s gave %v", tc.from, tc.to, got)
		}
	}
}
