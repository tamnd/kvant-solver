package mathnetru

import (
	"bytes"
	"context"
	"fmt"

	"github.com/tamnd/kvant-solver/source"
)

// Client fetches and parses mathnet.ru.
type Client struct {
	Fetcher *source.Client
}

// New returns a client with the default manners.
func New() *Client { return &Client{Fetcher: source.NewClient()} }

// Contents returns every Kvant issue mathnet holds, which is 2005 on. One
// request.
func (c *Client) Contents(ctx context.Context) ([]IssueRef, error) {
	u := ContentsURL()
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	refs, err := ParseContents(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	return refs, nil
}
