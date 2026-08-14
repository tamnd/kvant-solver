package kvantdigital

import (
	"bytes"
	"context"
	"fmt"

	"github.com/tamnd/kvant-solver/source"
)

// Client fetches and parses kvant.digital.
type Client struct {
	Fetcher *source.Client
}

// New returns a client with the default manners.
func New() *Client { return &Client{Fetcher: source.NewClient()} }

// IssuesIndex returns every issue the archive knows about. This is one
// request. The site lists every year and every issue on a single page, so
// there is no year by year crawl and no reason to write one.
func (c *Client) IssuesIndex(ctx context.Context) ([]IssueRef, error) {
	resp, err := c.Fetcher.Get(ctx, IssuesIndexURL())
	if err != nil {
		return nil, err
	}
	refs, err := ParseIssuesIndex(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", IssuesIndexURL(), err)
	}
	return refs, nil
}

// Issue returns the header and contents of one issue.
func (c *Client) Issue(ctx context.Context, year int, number string) (*Issue, error) {
	u := IssueURL(year, number)
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	iss, err := ParseIssue(bytes.NewReader(resp.Body), year, number)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	return iss, nil
}

// Pages returns the scan manifest for one issue: every sheet, its file name,
// and the number printed on it. One request per issue, no probing.
func (c *Client) Pages(ctx context.Context, issueKey string) ([]Sheet, error) {
	u := ViewURL(issueKey, 0)
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	sheets, err := ParsePages(bytes.NewReader(resp.Body), issueKey)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	return sheets, nil
}
