package mt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBothShapesOfAnswerDecode(t *testing.T) {
	for body, want := range map[string]string{
		`["a","b"]`:               "a|b",
		`[["a","ru"],["b","ru"]]`: "a|b",
		`"a"`:                     "a",
	} {
		got, err := Decode([]byte(body))
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		s := ""
		for i, g := range got {
			if i > 0 {
				s += "|"
			}
			s += g
		}
		if s != want {
			t.Errorf("%s gave %q, want %q", body, s, want)
		}
	}
}

func TestTheLanguagePairAndTheTextsAreSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.URL.Query().Get("sl") != "ru" || r.URL.Query().Get("tl") != "vi" || r.URL.Query().Get("client") != "x" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if got := r.PostForm["q"]; len(got) != 2 || got[0] != "один" || got[1] != "два" {
			t.Errorf("texts %q", got)
		}
		_, _ = w.Write([]byte(`["một","hai"]`))
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL + "?client=x"}
	got, err := c.Translate(context.Background(), "ru", "vi", []string{"один", "два"})
	if err != nil || len(got) != 2 || got[1] != "hai" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestARefusalIsWaitedOutAndTriedAgain(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`["ok"]`))
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL, Retries: 2, Backoff: 1}
	got, err := c.Translate(context.Background(), "ru", "vi", []string{"x"})
	if err != nil || got[0] != "ok" || calls.Load() != 2 {
		t.Fatalf("%q %v after %d calls", got, err, calls.Load())
	}
}

func TestACountMismatchIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`["one"]`))
	}))
	defer srv.Close()
	c := &Client{URL: srv.URL}
	if _, err := c.Translate(context.Background(), "ru", "vi", []string{"a", "b"}); err == nil {
		t.Fatal("two texts and one answer passed")
	}
}
