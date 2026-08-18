package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
)

// checkFixture is a corpus of pages written by the given lanes, keyed by sheet,
// with a cache holding a picture of every sheet named in staged.
func checkFixture(t *testing.T, lanes map[int]string, staged []int) (*corpus.Corpus, *fetch.Cache, *fetch.Index, corpus.IssueKey) {
	t.Helper()
	c := emptyCorpus(t)
	key := corpus.IssueKey{Year: 2010, Number: "1"}
	cache, err := fetch.OpenCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	idx := &fetch.Index{Issue: key.String(), Year: key.Year}
	for index, lane := range lanes {
		front := &corpus.PageFront{
			Issue: key.String(), Year: key.Year, Number: key.Number,
			PageIndex: index, PageLabel: "5",
			Provenance: corpus.Provenance{Lang: "ru", Source: "kvant_mccme", Extraction: lane},
		}
		path := c.PagePath("ru", corpus.PageID{Issue: key, Index: index})
		if err := corpus.Save(path, front, "Страница, прочитанная из файла.\n"); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range staged {
		sum, n, err := cache.Put(strings.NewReader(strings.Repeat("j", index+1)))
		if err != nil {
			t.Fatal(err)
		}
		idx.Sheets = append(idx.Sheets, fetch.Sheet{
			Ord: index - 1, File: "page.jpg", Page: index,
			Blob: fetch.Blob{SHA256: sum, Bytes: n},
		})
	}
	return c, cache, idx, key
}

func TestOnlyThePagesThisLaneWroteAreCheckedAgainstTheScan(t *testing.T) {
	// A page a model read off the picture is not evidence about the text layer,
	// and comparing it against a second reading of the same picture would
	// measure the vision lane against itself.
	c, cache, idx, key := checkFixture(t,
		map[int]string{3: corpus.ExtractionNative, 4: corpus.ExtractionVision, 5: corpus.ExtractionNative},
		[]int{3, 4, 5})
	got, err := nativePages(c, cache, idx, "ru", key)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{3, 5}; !slices.Equal(got, want) {
		t.Fatalf("got sheets %v, want %v", got, want)
	}
}

func TestAPageWhoseSheetWasNeverDownloadedHasNothingToBeCheckedAgainst(t *testing.T) {
	// The 2007 range was fetched as PDFs, and most of those issues have no
	// pictures in the cache at all. Those pages are left out rather than
	// reported as failures, because nothing about them failed.
	c, cache, idx, key := checkFixture(t,
		map[int]string{3: corpus.ExtractionNative, 5: corpus.ExtractionNative},
		[]int{3})
	got, err := nativePages(c, cache, idx, "ru", key)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{3}; !slices.Equal(got, want) {
		t.Fatalf("got sheets %v, want %v", got, want)
	}
}

func TestTheSampleIsTakenRightThroughTheIssue(t *testing.T) {
	// The front is prose, the middle is the problem set, the back is the
	// answers, and each of them puts a different amount of mathematics through
	// the text layer. A sample out of one of them measures that one.
	pages := []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	got := spread(pages, 4)
	if len(got) != 4 {
		t.Fatalf("got %d pages, want 4: %v", len(got), got)
	}
	if got[0] != 3 {
		t.Errorf("the sample starts at sheet %d, want the first page", got[0])
	}
	if got[len(got)-1] < 10 {
		t.Errorf("the sample ends at sheet %d, which is not near the back", got[len(got)-1])
	}
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("the sample repeats or goes backwards: %v", got)
		}
	}
}

func TestAskingForMorePagesThanThereAreTakesThemAll(t *testing.T) {
	pages := []int{3, 4, 5}
	if got := spread(pages, 6); !slices.Equal(got, pages) {
		t.Fatalf("got %v, want all three", got)
	}
	if got := spread(pages, 0); !slices.Equal(got, pages) {
		t.Fatalf("got %v, want all three", got)
	}
}
