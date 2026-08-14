package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClient(delay time.Duration) *Client {
	c := NewClient()
	c.Delay = delay
	return c
}

func TestGetReturnsTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	resp, err := testClient(0).Get(t.Context(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "hello" {
		t.Errorf("body is %q", resp.Body)
	}
	if resp.Redirected() {
		t.Error("nothing redirected but Redirected says otherwise")
	}
}

func TestRedirectIsVisible(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/here", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("here"))
	})
	mux.HandleFunc("/gone", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/here", http.StatusMovedPermanently)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := testClient(0).Get(t.Context(), srv.URL+"/gone")
	if err != nil {
		t.Fatal(err)
	}
	// A page sweep uses this to find the end of an issue, so a redirect that
	// looks like a plain 200 would be a silent bug.
	if !resp.Redirected() {
		t.Error("a 301 did not show up as a redirect")
	}
	if !strings.HasSuffix(resp.FinalURL, "/here") {
		t.Errorf("final url is %q", resp.FinalURL)
	}
}

func TestRefusalsAreNotRetryable(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, ErrRateLimited},
		{http.StatusForbidden, ErrForbidden},
		{http.StatusNotFound, ErrNotFound},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
		}))
		_, err := testClient(0).Get(t.Context(), srv.URL)
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d gave %v, want %v", tc.status, err, tc.want)
		}
	}
	// 429 and 403 mean stop. A 404 is just a page that is not there.
	if !Fatal(ErrRateLimited) || !Fatal(ErrForbidden) {
		t.Error("Fatal should be true for 429 and 403")
	}
	if Fatal(ErrNotFound) {
		t.Error("a missing page should not stop the crawl")
	}
}

func TestOtherStatusesCarryTheCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := testClient(0).Get(t.Context(), srv.URL)
	var se *StatusError
	if !errors.As(err, &se) {
		t.Fatalf("want a StatusError, got %v", err)
	}
	if se.Status != 500 {
		t.Errorf("status is %d", se.Status)
	}
}

func TestOneRequestAtATimePerHost(t *testing.T) {
	var mu sync.Mutex
	var times []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	const delay = 60 * time.Millisecond
	c := testClient(delay)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := c.Get(t.Context(), srv.URL); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if len(times) != 4 {
		t.Fatalf("server saw %d requests", len(times))
	}
	// Four goroutines all aiming at one host still have to queue. Without the
	// per host lock they would all decide the gap had passed and fire at once.
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < delay/2 {
			t.Errorf("requests %d and %d were %v apart, delay is %v", i-1, i, gap, delay)
		}
	}
}

func TestWaitingRespectsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := testClient(time.Hour)
	if _, err := c.Get(t.Context(), srv.URL); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, srv.URL); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("second request gave %v, want a deadline", err)
	}
}

func TestWeSayWhoWeAre(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.UserAgent()
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if _, err := testClient(0).Get(t.Context(), srv.URL); err != nil {
		t.Fatal(err)
	}
	ua := <-got
	if !strings.Contains(ua, "kvant-solver") || !strings.Contains(ua, "github.com/tamnd") {
		t.Errorf("user agent is %q, it should name us and say where to complain", ua)
	}
	if strings.Contains(strings.ToLower(ua), "googlebot") {
		t.Error("we are not Googlebot")
	}
}
