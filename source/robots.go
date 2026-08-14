package source

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Robots is the part of a robots.txt that applies to us. Only the group that
// matches our user agent is kept, so Allowed does not have to think about
// which group it is in.
type Robots struct {
	// rules are sorted longest pattern first, which is the precedence the
	// standard asks for.
	rules []rule

	// Delay is a Crawl-delay if the host asked for one. A host that asks for
	// longer than our own default gets what it asked for.
	Delay time.Duration

	// clean is the Yandex Clean-param extension. Both Russian hosts in this
	// project use Yandex as their main search engine and write it, and it is
	// the only machine readable statement they make about which query
	// parameters do not change the page.
	clean []cleanRule
}

type rule struct {
	pattern string
	allow   bool
}

type cleanRule struct {
	params []string
	prefix string
}

// AllowAll is what a host with no robots.txt gets.
func AllowAll() *Robots { return &Robots{} }

// ParseRobots reads a robots.txt and keeps the group for the given agent. A
// named group wins over the wildcard group, and if the file names us more than
// once the groups merge, which is what every real parser does with the
// duplicate User-agent lines these files tend to accumulate.
func ParseRobots(r io.Reader, agent string) *Robots {
	agent = strings.ToLower(agentToken(agent))

	var (
		out       = &Robots{}
		named     []rule
		wildcard  []rule
		namedSeen bool

		namedDelay, wildDelay time.Duration

		// inNamed and inWild track which groups the current line belongs to.
		// A group can have several User-agent lines, and a rule line ends the
		// run of agent lines.
		inNamed, inWild, afterRule bool
	)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch key {
		case "user-agent":
			if afterRule {
				inNamed, inWild, afterRule = false, false, false
			}
			switch strings.ToLower(value) {
			case agent:
				inNamed, namedSeen = true, true
			case "*":
				inWild = true
			}
		case "allow", "disallow":
			afterRule = true
			if value == "" && key == "disallow" {
				// An empty Disallow is the way a file says everything is
				// permitted. There is nothing to record.
				continue
			}
			r := rule{pattern: value, allow: key == "allow"}
			if inNamed {
				named = append(named, r)
			}
			if inWild {
				wildcard = append(wildcard, r)
			}
		case "crawl-delay":
			afterRule = true
			secs, err := strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
			d := time.Duration(secs * float64(time.Second))
			if inNamed {
				namedDelay = d
			}
			if inWild {
				wildDelay = d
			}
		case "clean-param":
			// Clean-param: p0[&p1&p2..] [path prefix]
			fields := strings.Fields(value)
			if len(fields) == 0 {
				continue
			}
			cr := cleanRule{params: strings.Split(fields[0], "&")}
			if len(fields) > 1 {
				cr.prefix = fields[1]
			}
			out.clean = append(out.clean, cr)
		}
	}

	if namedSeen {
		out.rules, out.Delay = named, namedDelay
	} else {
		out.rules, out.Delay = wildcard, wildDelay
	}
	// Longest pattern first. Where two patterns are the same length, Allow
	// goes ahead of Disallow, which is how the standard breaks a tie.
	sort.SliceStable(out.rules, func(i, j int) bool {
		if len(out.rules[i].pattern) != len(out.rules[j].pattern) {
			return len(out.rules[i].pattern) > len(out.rules[j].pattern)
		}
		return out.rules[i].allow && !out.rules[j].allow
	})
	return out
}

// Allowed reports whether we may fetch a path. The most specific rule wins,
// and a path no rule mentions is allowed.
func (r *Robots) Allowed(path string) bool {
	if r == nil {
		return true
	}
	if path == "" {
		path = "/"
	}
	for _, rl := range r.rules {
		if matchPattern(rl.pattern, path) {
			return rl.allow
		}
	}
	return true
}

// Clean drops the query parameters the host has said do not change the page.
// It is how two URLs that name the same page end up with the same string, so
// the fetch queue does not download one page twice under two names. It never
// rewrites the request itself: a parameter this project passes on purpose,
// such as the language on mathnet, is still sent.
func (r *Robots) Clean(rawURL string) string {
	if r == nil || len(r.clean) == 0 {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if len(q) == 0 {
		return rawURL
	}
	for _, cr := range r.clean {
		if cr.prefix != "" && !matchPattern(cr.prefix, u.EscapedPath()) {
			continue
		}
		for _, p := range cr.params {
			q.Del(p)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// matchPattern implements the two wildcards robots.txt has: * for any run of
// characters and $ for end of path. Everything else is a literal prefix.
func matchPattern(pattern, path string) bool {
	if pattern == "" {
		return false
	}
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")

	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == 0 {
			if !strings.HasPrefix(path, part) {
				return false
			}
			path = path[len(part):]
			continue
		}
		j := strings.Index(path, part)
		if j < 0 {
			return false
		}
		path = path[j+len(part):]
	}
	if anchored {
		// Everything before the last wildcard matched. The pattern ends at the
		// end of the path only if there is nothing left over, or if the last
		// segment was a wildcard that can swallow the rest.
		if last := parts[len(parts)-1]; last != "" {
			return path == ""
		}
	}
	return true
}

// agentToken takes the product token out of a user agent string. robots.txt
// files are written against the token, not against the whole line with the
// version and the URL in it.
func agentToken(agent string) string {
	agent = strings.TrimSpace(agent)
	if i := strings.IndexAny(agent, "/ "); i > 0 {
		agent = agent[:i]
	}
	return agent
}

// robotsFor returns the robots.txt for a host, fetching it once. A host that
// answers 404 is treated as allowing everything, which is what the standard
// says. A host that answers 429 or 403 to robots.txt is telling us to go away
// and that error is passed back rather than swallowed.
func (c *Client) robotsFor(ctx context.Context, u *url.URL) (*Robots, error) {
	h := c.host(u.Host)

	h.robotsMu.Lock()
	defer h.robotsMu.Unlock()
	if h.robotsDone {
		return h.robots, nil
	}

	robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
	resp, err := c.do(ctx, "GET", robotsURL, false)
	switch {
	case err == nil:
		h.robots = ParseRobots(strings.NewReader(string(resp.Body)), c.agent())
	case errors.Is(err, ErrNotFound):
		h.robots = AllowAll()
	case Fatal(err):
		return nil, err
	default:
		// A timeout or a 500 on robots.txt is not permission to crawl, but it
		// is also not a reason to give up on the whole run. Treat it as a
		// closed door for now and try again on the next run.
		return nil, err
	}
	h.robotsDone = true

	// A host that asks for a longer gap than our own gets it. A host that asks
	// for a shorter one does not, because our default is already polite and
	// the point is not to go faster.
	if h.robots.Delay > c.delay() {
		c.mu.Lock()
		h.override = h.robots.Delay
		c.mu.Unlock()
	}
	return h.robots, nil
}
