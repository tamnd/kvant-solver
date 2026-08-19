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

// MaxDownload caps a streamed file. A page scan is half a megabyte and the
// largest full issue PDF on the mirror is under sixty, so this is a backstop
// against a server handing us something endless rather than a real limit.
const MaxDownload = 256 << 20

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

	// ContentType is what the server said it was sending. The page sweep
	// leans on it: a request for a sheet past the end of an issue is answered
	// with an HTML error page rather than a 404, and text/html where a JPEG
	// was asked for is the plainest possible way to notice that.
	ContentType string

	Body []byte
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

// Download streams a URL into w rather than into memory. It is the same
// request Get makes, with the same manners in front of it, and it exists
// because a page scan is half a megabyte and a full issue PDF is thirty, and
// reading either into a byte slice on the way to a file is a waste of both
// sides of the connection.
//
// The response it returns has no body. What the caller wants from it is the
// final URL, because a request for a sheet past the end of an issue answers
// with a redirect to an error page rather than a 404, and that redirect is how
// the page sweep learns it has reached the end.
func (c *Client) Download(ctx context.Context, rawURL string, w io.Writer) (*Response, int64, error) {
	resp, err := c.open(ctx, http.MethodGet, rawURL, true)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	n, err := io.Copy(w, io.LimitReader(resp.Body, MaxDownload))
	if err != nil {
		return nil, n, fmt.Errorf("%s: %w", rawURL, err)
	}
	return &Response{
		RequestedURL: rawURL,
		FinalURL:     resp.Request.URL.String(),
		Status:       resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
	}, n, nil
}

func (c *Client) do(ctx context.Context, method, rawURL string, checkRobots bool) (*Response, error) {
	resp, err := c.open(ctx, method, rawURL, checkRobots)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rawURL, err)
	}
	return &Response{
		RequestedURL: rawURL,
		FinalURL:     resp.Request.URL.String(),
		Status:       resp.StatusCode,
		ContentType:  resp.Header.Get("Content-Type"),
		Body:         body,
	}, nil
}

// open makes the request and hands back a live response with the status
// already judged. The caller closes the body.
func (c *Client) open(ctx context.Context, method, rawURL string, checkRobots bool) (*http.Response, error) {
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

	// A status we are not going on with closes the body here, because the
	// caller only closes what it is handed and it is handed nothing.
	if err := judge(rawURL, resp.StatusCode); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

// judge turns a status into the error that says what to do about it.
func judge(rawURL string, status int) error {
	switch status {
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s: %w", rawURL, ErrRateLimited)
	case http.StatusForbidden:
		return fmt.Errorf("%s: %w", rawURL, ErrForbidden)
	case http.StatusNotFound, http.StatusGone:
		return fmt.Errorf("%s: %w", rawURL, ErrNotFound)
	}
	if status < 200 || status > 299 {
		return &StatusError{URL: rawURL, Status: status}
	}
	return nil
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

// Tries is how many attempts Retry makes before it gives up.
const Tries = 4

// Retry runs f again when it fails for a reason that might not happen twice.
//
// A sweep over a source is hundreds of requests to one small volunteer run
// server, and over that many one of them times out. Losing the whole run to it
// means starting the hundreds again, which is worse for the server than the
// retry is.
//
// It retries a transport error and a 5xx, and nothing else. A 429 or a 403 is
// the server telling us to stop, and retrying through either is the reason
// hosts start blocking whole subnets. A 404 is an answer, not a failure. None
// of those come back here.
func (c *Client) Retry(ctx context.Context, f func() error) error {
	var err error
	for try := range Tries {
		if try > 0 {
			// Doubling from twice the ordinary gap, on top of the per-host gap
			// the client already waits out before every request.
			t := time.NewTimer(c.delay() * time.Duration(1<<try))
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if err = f(); err == nil {
			return nil
		}
		// Cancellation first, and as itself. A cancelled request fails as a
		// transport error, and reporting that is how a run somebody stopped on
		// purpose gets read afterwards as the source being down.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !retryable(err) {
			return err
		}
	}
	return fmt.Errorf("gave up after %d tries: %w", Tries, err)
}

// retryable says whether asking again could plausibly give a different answer.
func retryable(err error) bool {
	if Fatal(err) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrDisallowed) {
		return false
	}
	// A 4xx means the server understood us and said no, so asking again the
	// same way gets the same answer. A 5xx means it did not manage to answer,
	// which is exactly the case worth asking about again.
	var status *StatusError
	if errors.As(err, &status) {
		return status.Status >= 500
	}
	return true
}
