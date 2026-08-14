package source

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func parse(body string) *Robots {
	return ParseRobots(strings.NewReader(body), UserAgent)
}

func TestAllowedTakesTheMostSpecificRule(t *testing.T) {
	r := parse(`
User-agent: *
Disallow: /data/
Allow: /data/kvant_1975_1/
Disallow: /php/
`)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/issues/", true},
		{"/data/kvant_1976_5/jpg/0002.jpg", false},
		{"/data/kvant_1975_1/jpg/0002.jpg", true},
		{"/php/archive.phtml", false},
	} {
		if got := r.Allowed(tc.path); got != tc.want {
			t.Errorf("Allowed(%q) = %v", tc.path, got)
		}
	}
}

func TestNamedGroupWinsOverWildcard(t *testing.T) {
	body := `
User-agent: *
Disallow: /

User-agent: kvant-solver
Disallow: /admin/
`
	r := parse(body)
	if !r.Allowed("/issues/") {
		t.Error("the group naming us should be the one that applies")
	}
	if r.Allowed("/admin/") {
		t.Error("our own group still applies")
	}
	// A crawler that does not appear by name falls back to the wildcard, which
	// here is a closed door.
	other := ParseRobots(strings.NewReader(body), "someone-else/1.0")
	if other.Allowed("/issues/") {
		t.Error("an agent with no group of its own should get the wildcard")
	}
}

func TestUserAgentMatchesOnTheProductToken(t *testing.T) {
	// Our user agent is a whole sentence with a version and a URL in it. A
	// robots.txt names the token, so matching has to happen on the token.
	r := ParseRobots(strings.NewReader("User-agent: kvant-solver\nDisallow: /nope/\n"), UserAgent)
	if r.Allowed("/nope/") {
		t.Error("the group for kvant-solver did not match kvant-solver/0.1 (+url)")
	}
}

func TestWildcardsAndEndAnchor(t *testing.T) {
	r := parse(`
User-agent: *
Disallow: /*.pdf$
Disallow: /view/*/print
`)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/pdf/2015/2015-01.pdf", false},
		{"/pdf/2015/2015-01.pdf?token=abc", true}, // the query is not part of the path
		{"/pdfs/index.html", true},
		{"/view/kvant_1975_1/print", false},
		{"/view/kvant_1975_1/p3/", true},
	} {
		if got := r.Allowed(tc.path); got != tc.want {
			t.Errorf("Allowed(%q) = %v", tc.path, got)
		}
	}
}

func TestEmptyDisallowMeansEverythingIsAllowed(t *testing.T) {
	r := parse("User-agent: *\nDisallow:\n")
	if !r.Allowed("/anything") {
		t.Error("an empty Disallow is permission, not a bar on everything")
	}
}

func TestCommentsAndBlankLinesAreIgnored(t *testing.T) {
	r := parse("# nothing here\n\nUser-agent: *  # everyone\nDisallow: /private/ # keep out\n")
	if r.Allowed("/private/x") {
		t.Error("the rule with a comment after it was dropped")
	}
}

func TestCleanParamDropsTheParametersTheHostNamed(t *testing.T) {
	r := parse(`
User-agent: *
Clean-param: option_lang&sid /php/
Clean-param: ref
`)
	got := r.Clean("https://www.mathnet.ru/php/archive.phtml?jrnid=kvant&issue=1&option_lang=rus&sid=99")
	want := "https://www.mathnet.ru/php/archive.phtml?issue=1&jrnid=kvant"
	if got != want {
		t.Errorf("Clean gave %q", got)
	}
	// The rule with a path only applies under that path.
	got = r.Clean("https://www.mathnet.ru/other?option_lang=rus&ref=x")
	if !strings.Contains(got, "option_lang=rus") {
		t.Errorf("a rule scoped to /php/ was applied outside it: %q", got)
	}
	if strings.Contains(got, "ref=x") {
		t.Errorf("the unscoped rule was not applied: %q", got)
	}
}

func TestCrawlDelayLongerThanOursIsHonoured(t *testing.T) {
	r := parse("User-agent: *\nCrawl-delay: 2.5\n")
	if r.Delay != 2500*time.Millisecond {
		t.Errorf("delay is %v", r.Delay)
	}
}

func TestClientAsksForRobotsOnceAndObeysIt(t *testing.T) {
	var robotsHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		robotsHits.Add(1)
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /private/\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient()
	c.Delay = 0

	if _, err := c.Get(t.Context(), srv.URL+"/issues/"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(t.Context(), srv.URL+"/issues/1975/1/"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(t.Context(), srv.URL+"/private/x"); !errors.Is(err, ErrDisallowed) {
		t.Errorf("a disallowed path gave %v", err)
	}
	// Fetching robots.txt before every request would double the traffic to a
	// server we are already trying to be gentle with.
	if n := robotsHits.Load(); n != 1 {
		t.Errorf("robots.txt was fetched %d times", n)
	}
}

func TestNoRobotsFileMeansCrawlAway(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient()
	c.Delay = 0
	if _, err := c.Get(t.Context(), srv.URL+"/issues/"); err != nil {
		t.Fatal(err)
	}
}

func TestRobotsRefusalStopsTheRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient()
	c.Delay = 0
	_, err := c.Get(t.Context(), srv.URL+"/issues/")
	if !Fatal(err) {
		t.Errorf("a 429 on robots.txt gave %v, which does not stop the run", err)
	}
}
