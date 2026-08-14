// Package source fetches pages from the three archives that hold Kvant. It
// does the network part only. Every parser in the subpackages takes a reader,
// so the parsers can be tested without a server.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// UserAgent says who we are and where to complain. The old crawler sent a
// Googlebot string, which was a lie and is not repeated here.
const UserAgent = "kvant-solver/0.1 (+https://github.com/tamnd/kvant-solver)"

// DefaultDelay is the gap between two requests to the same host. These are
// small volunteer run servers holding fifty years of a magazine, and there is
// no hurry.
const DefaultDelay = time.Second

// MaxBody caps a single response. The largest HTML page in the archive is
// under a megabyte, so anything past this is a redirect loop into something
// we did not mean to download.
const MaxBody = 32 << 20

// Sentinel errors. The first two mean stop, not retry. A crawler that retries
// through a 429 is the reason hosts start blocking whole subnets.
var (
	ErrRateLimited = errors.New("the server asked us to slow down")
	ErrForbidden   = errors.New("the server refused us")
	ErrNotFound    = errors.New("not found")
	ErrDisallowed  = errors.New("robots.txt says no")
)

// StatusError is any other status we did not expect.
type StatusError struct {
	URL    string
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: http %d", e.URL, e.Status)
}

// Response is one fetched page.
type Response struct {
	RequestedURL string
	FinalURL     string
	Status       int
	Body         []byte
}

// Redirected reports whether the server sent us somewhere else. This matters
// more than it looks: the kvant.digital page viewer answers a request for a
// page past the end of an issue with a redirect rather than a 404, so a sweep
// that only watches for 404 walks off the end of the issue and keeps going.
func (r *Response) Redirected() bool { return r.FinalURL != r.RequestedURL }

// Client is an HTTP client that is deliberately slow. It serialises requests
// per host and waits between them.
type Client struct {
	HTTP      *http.Client
	Delay     time.Duration
	UserAgent string

	// IgnoreRobots skips the robots.txt check. It exists for tests against a
	// local server and is not a flag on the command.
	IgnoreRobots bool

	mu    sync.Mutex
	hosts map[string]*hostState
}

type hostState struct {
	mu   sync.Mutex
	last time.Time

	// override is a Crawl-delay this host asked for, when it is longer than
	// ours. It is read and written under the client lock.
	override time.Duration

	// robots is fetched once per host, on the first request to it. It has a
	// lock of its own because fetching it takes the request lock below, and
	// one mutex cannot do both jobs.
	robotsMu   sync.Mutex
	robots     *Robots
	robotsDone bool
}

// host returns the state for a host, creating it on first use.
func (c *Client) host(name string) *hostState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hosts == nil {
		c.hosts = map[string]*hostState{}
	}
	h := c.hosts[name]
	if h == nil {
		h = &hostState{}
		c.hosts[name] = h
	}
	return h
}

func (c *Client) agent() string {
	if c.UserAgent == "" {
		return UserAgent
	}
	return c.UserAgent
}

// delay is the gap this client asks for. Zero means no gap, which is only
// ever used by tests against a local server.
func (c *Client) delay() time.Duration { return c.Delay }

// NewClient returns a client with the default manners.
func NewClient() *Client {
	return &Client{
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		Delay:     DefaultDelay,
		UserAgent: UserAgent,
	}
}

// Get fetches one URL, waiting first if this host was hit recently.
func (c *Client) Get(ctx context.Context, rawURL string) (*Response, error) {
	return c.do(ctx, http.MethodGet, rawURL, true)
}

// Head asks whether a URL exists without pulling it down. This is how the
// probe finds out which years have a full issue PDF behind them without
// downloading fifty megabytes to answer a yes or no question.
func (c *Client) Head(ctx context.Context, rawURL string) (*Response, error) {
	return c.do(ctx, http.MethodHead, rawURL, true)
}

func (c *Client) do(ctx context.Context, method, rawURL string, checkRobots bool) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if checkRobots && !c.IgnoreRobots {
		robots, err := c.robotsFor(ctx, u)
		if err != nil {
			return nil, err
		}
		if !robots.Allowed(u.EscapedPath()) {
			return nil, fmt.Errorf("%s: %w", rawURL, ErrDisallowed)
		}
	}
	if err := c.wait(ctx, u.Host); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.agent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru,en;q=0.8")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("%s: %w", rawURL, ErrRateLimited)
	case http.StatusForbidden:
		return nil, fmt.Errorf("%s: %w", rawURL, ErrForbidden)
	case http.StatusNotFound, http.StatusGone:
		return nil, fmt.Errorf("%s: %w", rawURL, ErrNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &StatusError{URL: rawURL, Status: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rawURL, err)
	}
	return &Response{
		RequestedURL: rawURL,
		FinalURL:     resp.Request.URL.String(),
		Status:       resp.StatusCode,
		Body:         body,
	}, nil
}

// wait holds the host lock for the whole gap, so two goroutines aiming at the
// same host queue up instead of both deciding the gap has passed.
func (c *Client) wait(ctx context.Context, host string) error {
	h := c.host(host)

	c.mu.Lock()
	want := max(c.delay(), h.override)
	c.mu.Unlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	if gap := want - time.Since(h.last); gap > 0 && !h.last.IsZero() {
		t := time.NewTimer(gap)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	h.last = time.Now()
	return nil
}

// Fatal reports whether an error means give up on this source entirely rather
// than move on to the next URL.
func Fatal(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrForbidden)
}
