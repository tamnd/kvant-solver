package refs_test

import (
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/tags"
)

// tagAll gives every object in a fixture a permanent name, because a reference
// resolves to a tag and an untagged corpus can only ever produce pending ones.
func tagAll(t *testing.T, c *corpus.Corpus) {
	t.Helper()
	store, err := tags.Open(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tags.Assign(c, "ru", store); err != nil {
		t.Fatal(err)
	}
}

// fixedTime keeps the generated line out of the comparison, so that a test of
// the report is a test of the report and not of the clock.
func fixedTime() time.Time {
	return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
}
