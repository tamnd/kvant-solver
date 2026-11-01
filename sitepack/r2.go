package sitepack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Bucket is an R2 bucket reached through the Cloudflare API with a token, the
// same way the figures are uploaded, so a deploy needs no S3 keys.
type Bucket struct {
	Account, Name, Token string
	Client               *http.Client
}

// ErrNotFound is an object the bucket does not have.
var ErrNotFound = errors.New("not found")

func (b *Bucket) url(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/r2/buckets/%s/objects/%s",
		b.Account, b.Name, strings.Join(parts, "/"))
}

func (b *Bucket) do(ctx context.Context, method, key, ctype string, body []byte) ([]byte, error) {
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
		}
		req, err := http.NewRequestWithContext(ctx, method, b.url(key), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+b.Token)
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		switch {
		case err != nil:
			last = err
		case resp.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case resp.StatusCode/100 == 2:
			return data, nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			last = fmt.Errorf("%s %s: %s", method, key, resp.Status)
		default:
			return nil, fmt.Errorf("%s %s: %s: %.200s", method, key, resp.Status, data)
		}
	}
	return nil, last
}

// Get reads an object.
func (b *Bucket) Get(ctx context.Context, key string) ([]byte, error) {
	return b.do(ctx, http.MethodGet, key, "", nil)
}

// Put writes an object.
func (b *Bucket) Put(ctx context.Context, key, ctype string, data []byte) error {
	_, err := b.do(ctx, http.MethodPut, key, ctype, data)
	return err
}

// Delete removes an object.
func (b *Bucket) Delete(ctx context.Context, key string) error {
	_, err := b.do(ctx, http.MethodDelete, key, "", nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// Manifest reads the manifest of the deploy that is live, or nil when there
// has not been one.
func (b *Bucket) Manifest(ctx context.Context) (*Manifest, error) {
	data, err := b.Get(ctx, ManifestKey)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m := &Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestKey, err)
	}
	return m, nil
}
