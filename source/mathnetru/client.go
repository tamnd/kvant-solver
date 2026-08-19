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

// Issue returns the articles in one issue, with the bibliography and the
// permanent identifier for each. One request.
//
// The issue number wanted here is the one out of IssueRef.Query and not the
// number on the cover, because a double issue printed as 5-6 is issue=5 in the
// site's own URL.
func (c *Client) Issue(ctx context.Context, year, issue int) ([]PaperRef, error) {
	u := IssueURL(year, issue)
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	papers, err := ParseIssue(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	return papers, nil
}
