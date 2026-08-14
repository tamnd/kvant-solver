package fetch

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/tamnd/kvant-solver/manifest"
)

// Index is the page manifest of one issue: every sheet the scan has, where its
// bytes are, and which of the three extraction paths it is going to take.
//
// It is written before any model is called, which is the whole point of it. A
// run that knows what each page costs before it starts can be priced, argued
// with and stopped; a run that finds out as it goes can only be watched.
type Index struct {
	Issue string `yaml:"issue"`
	Year  int    `yaml:"year"`

	// Sheets is the scan, in the order the sheets are bound.
	Sheets []Sheet `yaml:"sheets"`

	// PDF is the full issue file from the MCCME mirror where the mirror has
	// one. It is what makes the native path possible from 2007 on.
	PDF *Blob `yaml:"pdf,omitempty"`
}

// Sheet is one scanned surface.
type Sheet struct {
	// Ord is the position in the scan, counting from zero the way the site
	// numbers its viewer. File is the name the site gives the JPEG, which is
	// not the ordinal: inserts and cover backs carry a letter suffix.
	Ord  int    `yaml:"ord"`
	File string `yaml:"file"`

	// Page is the number printed on the sheet, or zero for a sheet the
	// magazine did not number, which the four cover surfaces never are.
	Page int `yaml:"page,omitempty"`

	Blob `yaml:",inline"`

	// Path is what textguard decided, and is empty until it has run.
	Path string `yaml:"path,omitempty"`

	// Why is the measurement behind Path, in a few words, so that a page that
	// was sent to a model can be argued with without rerunning anything.
	Why string `yaml:"why,omitempty"`
}

// Blob is a downloaded file: where it came from and what it hashed to.
type Blob struct {
	URL    string `yaml:"url,omitempty"`
	SHA256 string `yaml:"sha256,omitempty"`
	Bytes  int64  `yaml:"bytes,omitempty"`
}

// Have reports whether this sheet has been downloaded.
func (b Blob) Have() bool { return b.SHA256 != "" }

// IndexFile is what one issue's page manifest is called.
func IndexFile(issueKey string) string { return issueKey + ".yaml" }

// Indexes is where the page manifests live inside a cache.
func (c *Cache) Indexes() (*manifest.Store, error) {
	return manifest.At(filepath.Join(c.Dir, "pages"))
}

// ReadIndex loads the page manifest of one issue. A missing one is not an
// error: the first fetch of an issue is where it comes from.
func (c *Cache) ReadIndex(issueKey string) (*Index, error) {
	store, err := c.Indexes()
	if err != nil {
		return nil, err
	}
	idx := &Index{}
	if err := store.Read(IndexFile(issueKey), idx); err != nil {
		if isMissing(err) {
			return &Index{Issue: issueKey}, nil
		}
		return nil, err
	}
	return idx, nil
}

// WriteIndex saves the page manifest of one issue.
func (c *Cache) WriteIndex(idx *Index) error {
	store, err := c.Indexes()
	if err != nil {
		return err
	}
	idx.Sort()
	return store.Write(IndexFile(idx.Issue), idx,
		fmt.Sprintf("The scan of %s: every sheet, its bytes in the cache, and how it is to be read.", idx.Issue))
}

// Sort puts the sheets in binding order.
func (i *Index) Sort() {
	slices.SortFunc(i.Sheets, func(a, b Sheet) int {
		if a.Ord != b.Ord {
			return a.Ord - b.Ord
		}
		if a.File < b.File {
			return -1
		}
		if a.File > b.File {
			return 1
		}
		return 0
	})
}

// Get returns the sheet with a file name.
func (i *Index) Get(file string) (*Sheet, bool) {
	if n := slices.IndexFunc(i.Sheets, func(s Sheet) bool { return s.File == file }); n >= 0 {
		return &i.Sheets[n], true
	}
	return nil, false
}

// Set adds a sheet or updates the one already there, keeping whatever the
// caller did not say. A sweep that reruns must not throw away a path decision
// textguard made, and a textguard run must not throw away a download.
func (i *Index) Set(s Sheet) {
	old, ok := i.Get(s.File)
	if !ok {
		i.Sheets = append(i.Sheets, s)
		return
	}
	if s.Page == 0 {
		s.Page = old.Page
	}
	if !s.Have() {
		s.Blob = old.Blob
	}
	if s.Path == "" {
		s.Path, s.Why = old.Path, old.Why
	}
	*old = s
}

// Fetched is how many sheets have their bytes.
func (i *Index) Fetched() int {
	n := 0
	for _, s := range i.Sheets {
		if s.Have() {
			n++
		}
	}
	return n
}

// Bytes is how much the scan of this issue weighs.
func (i *Index) Bytes() int64 {
	var n int64
	for _, s := range i.Sheets {
		n += s.Bytes
	}
	return n
}

// LastOrd is the highest sheet ordinal in the manifest, which is where the
// upward probe starts looking for sheets nobody listed.
func (i *Index) LastOrd() int {
	n := -1
	for _, s := range i.Sheets {
		n = max(n, s.Ord)
	}
	return n
}

func isMissing(err error) bool { return errors.Is(err, manifest.ErrMissing) }
