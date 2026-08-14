// Package kvantdigital reads www.kvant.digital, the MCCME run archive that
// carries a scan of every page of every issue and a growing amount of
// publisher supplied text.
package kvantdigital

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BaseURL is the site root. Everything else is built from it.
const BaseURL = "https://www.kvant.digital"

// IssuesIndexURL is the one page that lists every year and every issue. The
// whole issue manifest comes from here, which is why there is no year by year
// crawl.
func IssuesIndexURL() string { return BaseURL + "/issues/" }

// IssueURL is the table of contents for one issue.
func IssueURL(year int, number string) string {
	return fmt.Sprintf("%s/issues/%d/%s/", BaseURL, year, number)
}

// IssueKey is the site's own name for an issue, and the same string this
// project uses as its issue identifier.
func IssueKey(year int, number string) string {
	return fmt.Sprintf("kvant_%d_%s", year, number)
}

// ViewURL opens one sheet of an issue in the page viewer. The sheet number is
// not the printed page number, see Sheet on TOCRow.
func ViewURL(issueKey string, sheet int) string {
	return fmt.Sprintf("%s/view/%s/p%d/", BaseURL, issueKey, sheet)
}

// ScanURL is the JPEG of one scanned sheet. The file name is not derived from
// the page number: it is zero padded, zero based, and inserts and cover backs
// carry a letter suffix, so it comes from the page list rather than from
// arithmetic. Asking for a sheet that does not exist gets a 301 to an error
// page rather than a 404.
func ScanURL(issueKey, file string) string {
	return fmt.Sprintf("%s/data/%s/jpg/%s.jpg", BaseURL, issueKey, file)
}

// CoverURL is the issue thumbnail.
func CoverURL(issueKey string) string {
	return fmt.Sprintf("%s/data/%s/thumb@hd.jpg", BaseURL, issueKey)
}

// PersonaliaIndexURL lists every named contributor.
func PersonaliaIndexURL() string { return BaseURL + "/indices/personalia/" }

// PersonaliaURL is one contributor.
func PersonaliaURL(slug string) string {
	return fmt.Sprintf("%s/indices/personalia/%s/", BaseURL, slug)
}

// ProblemURL is one problem from the M or F series. The site writes the
// subject letter in lower case.
func ProblemURL(subject string, number int) string {
	return fmt.Sprintf("%s/problems/%s%d/", BaseURL, strings.ToLower(subject), number)
}

var (
	reIssueHref     = regexp.MustCompile(`/issues/(\d{4})/([^/"]+)/$`)
	reArticleHref   = regexp.MustCompile(`/issues/\d{4}/[^/]+/([a-z0-9_-]+-[0-9a-f]{8})/$`)
	reViewHref      = regexp.MustCompile(`/view/[^/]+/p(\d+)/$`)
	rePersonHref    = regexp.MustCompile(`/indices/personalia/([a-z0-9_]+)/$`)
	rePageCount     = regexp.MustCompile(`(\d+)\s*с\.`)
	reIssueKeyParts = regexp.MustCompile(`^kvant_(\d{4})_(.+)$`)
)

// absolute turns the protocol relative and root relative hrefs the site emits
// into something that can be fetched.
func absolute(href string) string {
	href = strings.TrimSpace(href)
	switch {
	case href == "":
		return ""
	case strings.HasPrefix(href, "//"):
		return "https:" + href
	case strings.HasPrefix(href, "/"):
		return BaseURL + href
	default:
		return href
	}
}

// SplitIssueKey takes kvant_1976_5-6 apart.
func SplitIssueKey(key string) (year int, number string, ok bool) {
	m := reIssueKeyParts.FindStringSubmatch(key)
	if m == nil {
		return 0, "", false
	}
	year, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", false
	}
	return year, m[2], true
}

// ArticleSlug pulls the slug out of an article URL. The slug already ends in
// eight hex characters, so it is unique within the site and makes a usable
// stable identifier.
func ArticleSlug(href string) string {
	if m := reArticleHref.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}

// PersonSlug pulls the slug out of a personalia URL.
func PersonSlug(href string) string {
	if m := rePersonHref.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}
