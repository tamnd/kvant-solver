package source

import (
	"context"
	"errors"
	"fmt"
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
	// These tests are about the fetching, and a test server answers every path
	// including /robots.txt, so leaving the check on would have every one of
	// them assert something about robots.txt by accident. That check has its
	// own tests in robots_test.go.
	c.IgnoreRobots = true
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

// A sweep is hundreds of requests and one of them times out. That is the whole
// reason Retry exists, so the first thing to pin down is that the run survives
// it.
func TestATimeoutIsTriedAgain(t *testing.T) {
	tries := 0
	err := testClient(0).Retry(t.Context(), func() error {
		tries++
		if tries < 3 {
			return errors.New("dial tcp: i/o timeout")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tries != 3 {
		t.Errorf("gave up after %d tries", tries)
	}
}

// Retrying through a 429 or a 403 is how a crawler gets a subnet blocked, so
// both have to come straight back out. A 404 is an answer and asking again does
// not change it.
func TestRetryDoesNotArgueWithARefusal(t *testing.T) {
	for _, want := range []error{ErrRateLimited, ErrForbidden, ErrNotFound, ErrDisallowed} {
		tries := 0
		err := testClient(0).Retry(t.Context(), func() error {
			tries++
			return fmt.Errorf("http://example.invalid: %w", want)
		})
		if !errors.Is(err, want) {
			t.Errorf("%v came back as %v", want, err)
		}
		if tries != 1 {
			t.Errorf("%v was asked %d times", want, tries)
		}
	}
}

// A 4xx means the server understood and said no. A 5xx means it did not manage
// to answer at all, which is the case worth asking about again.
func TestRetryTellsAServerErrorFromABadRequest(t *testing.T) {
	for _, tc := range []struct{ status, want int }{{400, 1}, {418, 1}, {500, Tries}, {503, Tries}} {
		tries := 0
		err := testClient(0).Retry(t.Context(), func() error {
			tries++
			return &StatusError{URL: "http://example.invalid", Status: tc.status}
		})
		if err == nil {
			t.Errorf("http %d was allowed to pass", tc.status)
		}
		if tries != tc.want {
			t.Errorf("http %d was asked %d times, want %d", tc.status, tries, tc.want)
		}
	}
}

func TestRetryGivesUpAndSaysWhatTheLastFailureWas(t *testing.T) {
	sentinel := errors.New("the wire came loose")
	err := testClient(0).Retry(t.Context(), func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("the last failure was lost: %v", err)
	}
	if !strings.Contains(err.Error(), "gave up") {
		t.Errorf("the error does not say it gave up: %v", err)
	}
}

// A cancelled run stops now. Sitting out the backoff of a context that is
// already done is the slowest possible way to do nothing.
func TestRetryStopsWhenTheRunIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	tries := 0
	err := testClient(time.Hour).Retry(ctx, func() error {
		tries++
		cancel()
		return errors.New("dial tcp: i/o timeout")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelling gave %v", err)
	}
	if tries != 1 {
		t.Errorf("kept going for %d tries after cancellation", tries)
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
