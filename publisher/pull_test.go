package publisher_test

import (
	"testing"

	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/publisher"
)

func toc(rows ...manifest.Row) *manifest.TOC {
	t := &manifest.TOC{}
	t.Set("kvant_1975_1", rows)
	return t
}

func row(slug string, hasText bool) manifest.Row {
	r := manifest.Row{Title: slug, Slug: slug, HasText: hasText}
	if slug != "" {
		r.URL = "https://www.kvant.digital/issues/1975/1/" + slug + "/"
	}
	return r
}

var issues = []manifest.Issue{{Key: "kvant_1975_1", Year: 1975}}

func TestOnlyTheRowsTheArchiveSaysItHasTypedAreTaken(t *testing.T) {
	t.Parallel()
	got := publisher.WithText(toc(row("ellips", true), row("krug", false), row("kub", true)), issues)
	if len(got) != 2 {
		t.Fatalf("%d rows taken, want the two with text", len(got))
	}
	for _, c := range got {
		if c.Slug == "krug" {
			t.Error("a row with no publisher text is in the list")
		}
	}
}

func TestARowWithNoPageOfItsOwnIsSkipped(t *testing.T) {
	// The contents prints lines the site has no page for, a section heading or
	// a continuation. has_text on one of those is the parser having found the
	// text of whatever page it landed on, and fetching it would file one
	// article's text under another article's name.
	t.Parallel()
	got := publisher.WithText(toc(row("", true), row("ellips", true)), issues)
	if len(got) != 1 || got[0].Slug != "ellips" {
		t.Fatalf("%d rows taken, want only the one with a page: %+v", len(got), got)
	}
}

func TestTheWholeContentsIsTakenAndNotASampleOfIt(t *testing.T) {
	// This is the difference from Candidates. A sample is right for measuring a
	// rate and wrong for text that is being kept, where the article left out is
	// a page somebody has to read for no reason.
	t.Parallel()
	var rows []manifest.Row
	for _, slug := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		rows = append(rows, row(slug, true))
	}
	if got := publisher.WithText(toc(rows...), issues); len(got) != len(rows) {
		t.Fatalf("%d of %d rows taken", len(got), len(rows))
	}
}

func TestAnIssueTheContentsDoesNotCoverIsNotAnError(t *testing.T) {
	// The contents is synced a year at a time and the issue list comes from the
	// manifest, so asking for a year that has not been synced is normal and has
	// to come back empty rather than blow up mid run.
	t.Parallel()
	got := publisher.WithText(toc(row("ellips", true)), []manifest.Issue{{Key: "kvant_1999_4", Year: 1999}})
	if len(got) != 0 {
		t.Fatalf("%d rows for an issue with no contents", len(got))
	}
}
