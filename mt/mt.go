// Package mt is a client for a plain sentence translation service: text goes
// in, text comes back, and there is no prompt, no glossary and no notion of
// mathematics on the other side.
//
// It is what makes a first pass over the whole archive possible in hours
// rather than months. What it gives back is a draft and is stamped as one, and
// everything that keeps a draft usable (the formulas, the labels, the Markdown)
// is the harness in package translate and not this client. This one only
// speaks the wire, keeps its distance from the service and waits out a refusal.
//
// The service is not named here. Its address comes from --mt-url or
// KVANT_MT_URL, and that URL carries whatever fixed query parameters the
// service wants; this client adds the language pair and posts the text.
package mt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client talks to the service.
type Client struct {
	// URL is the endpoint with its fixed query parameters. sl, tl and q are
	// added per call.
	URL        string
	HTTPClient *http.Client
	UserAgent  string
	// Gap is the least time between two calls. The service is shared and free,
	// and a client that hammers it is a client that gets refused for a day.
	Gap time.Duration
	// Retries is how many times a refused or failed call is tried again, with
	// the wait doubling each time from Backoff.
	Retries int
	Backoff time.Duration

	mu   sync.Mutex
	last time.Time
}

// ErrRefused is a call the service declined to answer, after every retry.
var ErrRefused = errors.New("the translation service refused the call")

// Translate turns texts from one language into another, one answer per text,
// in order.
func (c *Client) Translate(ctx context.Context, from, to string, texts []string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if c.URL == "" {
		return nil, errors.New("no translation service: set --mt-url or KVANT_MT_URL")
	}
	backoff := c.Backoff
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		out, wait, err := c.once(ctx, from, to, texts)
		if err == nil {
			return out, nil
		}
		lastErr = err
		if wait > backoff {
			backoff = wait
		}
	}
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, from, to string, texts []string) ([]string, time.Duration, error) {
	c.pace(ctx)

	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	q.Set("sl", from)
	q.Set("tl", to)
	u.RawQuery = q.Encode()

	form := url.Values{}
	for _, t := range texts {
		form.Add("q", t)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, retryAfter(resp), fmt.Errorf("%w: status %d", ErrRefused, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("status %d: %.200s", resp.StatusCode, body)
	}
	out, err := Decode(body)
	if err != nil {
		return nil, 0, err
	}
	if len(out) != len(texts) {
		return nil, 0, fmt.Errorf("sent %d texts and got %d back", len(texts), len(out))
	}
	return out, 0, nil
}

// pace holds a call back until Gap has passed since the last one.
func (c *Client) pace(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.Gap - time.Since(c.last); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	c.last = time.Now()
}

func retryAfter(resp *http.Response) time.Duration {
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 0
}

// Decode reads an answer. The service gives a JSON array with one entry per
// text: a string when the source language was named, or a [text, language]
// pair when it was guessed. A single text sometimes comes back bare.
func Decode(body []byte) ([]string, error) {
	var single string
	if err := json.Unmarshal(body, &single); err == nil {
		return []string{single}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("not an answer: %.200s", body)
	}
	out := make([]string, 0, len(items))
	for _, raw := range items {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out = append(out, s)
			continue
		}
		var pair []json.RawMessage
		if err := json.Unmarshal(raw, &pair); err != nil || len(pair) == 0 {
			return nil, fmt.Errorf("an entry that is not text: %.200s", raw)
		}
		if err := json.Unmarshal(pair[0], &s); err != nil {
			return nil, fmt.Errorf("an entry that is not text: %.200s", raw)
		}
		out = append(out, s)
	}
	return out, nil
}
