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

// Issue returns the header, the contents and the scan manifest of one issue.
// That is everything this site knows about it, and it is one request: the
// thumbnail strip on the same page names every sheet.
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

// Article returns one item of a table of contents as its own page. The URL is
// the one the contents row gave, because a slug is not derivable from a title
// and building one would be a guess.
func (c *Client) Article(ctx context.Context, u, slug string) (*Article, error) {
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	a, err := ParseArticle(bytes.NewReader(resp.Body), slug)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	a.URL = u
	return a, nil
}

// Personalia returns every named contributor. The index paginates, and the
// first page says how many there are, so this is one request to find out and
// then one per remaining page.
func (c *Client) Personalia(ctx context.Context) ([]Person, error) {
	first, err := c.PersonaliaPage(ctx, 1)
	if err != nil {
		return nil, err
	}
	people := first.People
	for page := 2; page <= first.LastPage; page++ {
		next, err := c.PersonaliaPage(ctx, page)
		if err != nil {
			return people, err
		}
		people = append(people, next.People...)
	}
	return people, nil
}

// PersonaliaPage returns one page of the personalia index.
func (c *Client) PersonaliaPage(ctx context.Context, page int) (*PersonaliaPage, error) {
	u := PersonaliaPageURL(page)
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	p, err := ParsePersonalia(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	return p, nil
}

// Problem returns one problem with both halves. The subject is M or F.
func (c *Client) Problem(ctx context.Context, subject string, number int) (*Problem, error) {
	u := ProblemURL(subject, number)
	resp, err := c.Fetcher.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	p, err := ParseProblem(bytes.NewReader(resp.Body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	return p, nil
}
