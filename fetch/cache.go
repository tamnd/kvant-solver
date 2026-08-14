// Package fetch downloads what the sources named and keeps it on disk.
//
// What it keeps is not the corpus. The corpus is text under version control
// and is meant to be read by people; this is twenty gigabytes of JPEG that any
// run can fetch again and that nobody should ever commit. So it lives in a
// cache directory of its own, outside the checkout, and everything in it is
// named by the hash of its own bytes.
//
// Naming a file by its hash buys two things worth having here. A page that has
// already been downloaded is recognised without asking the server about it,
// which is what makes a run resumable across the four thousand requests a
// decade takes. And a scan that the site quietly replaces shows up as a new
// hash against a page manifest that expected the old one, rather than as a
// transcription that stops matching its source for no visible reason.
package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Cache is the directory downloaded bytes live in.
type Cache struct{ Dir string }

// DefaultDir is where the cache goes when nobody says otherwise.
func DefaultDir() string {
	if dir := os.Getenv("KVANT_CACHE"); dir != "" {
		return dir
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return ".kvant-cache"
	}
	return filepath.Join(base, "kvant")
}

// OpenCache points at a cache directory, creating it if it is not there.
func OpenCache(dir string) (*Cache, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, "blobs"), 0o755); err != nil {
		return nil, err
	}
	return &Cache{Dir: abs}, nil
}

// Path is where the bytes with a given hash live. The first two hex characters
// become a directory, because a flat directory of forty thousand files is
// unpleasant on every filesystem and unusable on some.
func (c *Cache) Path(sum string) string {
	if len(sum) < 3 {
		return filepath.Join(c.Dir, "blobs", sum)
	}
	return filepath.Join(c.Dir, "blobs", sum[:2], sum[2:])
}

// Has reports whether the bytes with a given hash are already here.
func (c *Cache) Has(sum string) bool {
	if sum == "" {
		return false
	}
	_, err := os.Stat(c.Path(sum))
	return err == nil
}

// Put reads r into the cache and returns the hash of what it read.
func (c *Cache) Put(r io.Reader) (sum string, n int64, err error) {
	return c.PutFunc(func(w io.Writer) error {
		var cerr error
		n, cerr = io.Copy(w, r)
		return cerr
	})
}

// PutFunc lets the caller write the bytes rather than hand over a reader,
// which is what a streaming download wants: it writes into a writer and does
// not have a reader to give.
//
// The bytes go to a temporary file and are renamed into place once the hash is
// known, so a download that is interrupted leaves nothing behind that a later
// run could mistake for a complete file.
func (c *Cache) PutFunc(fill func(w io.Writer) error) (sum string, n int64, err error) {
	tmp, err := os.CreateTemp(c.Dir, ".put.*")
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	h := sha256.New()
	counted := &countingWriter{w: io.MultiWriter(tmp, h)}
	err = fill(counted)
	n = counted.n
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", n, err
	}
	sum = hex.EncodeToString(h.Sum(nil))

	dst := c.Path(sum)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", n, err
	}
	// Already here is not a failure. Two issues can share a blank insert page,
	// and the same bytes under the same name are the same file.
	if _, err := os.Stat(dst); err == nil {
		return sum, n, nil
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return "", n, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", n, err
	}
	return sum, n, nil
}

// countingWriter is how PutFunc knows how much was written when the caller
// does the writing.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// remove drops a blob. It is for the one case that needs it: a download that
// turned out to be the site's error page rather than the file that was asked
// for, which is how the end of an issue announces itself.
func (c *Cache) remove(sum string) error {
	err := os.Remove(c.Path(sum))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Open opens the bytes with a given hash.
func (c *Cache) Open(sum string) (*os.File, error) {
	f, err := os.Open(c.Path(sum))
	if err != nil {
		return nil, fmt.Errorf("blob %s: %w", sum, err)
	}
	return f, nil
}

// Size is how many bytes the cache is holding, and how many files. It is what
// the fetch command prints at the end of a run, because the honest answer to
// "how big is this going to get" is a measurement.
func (c *Cache) Size() (files int, bytes int64, err error) {
	root := filepath.Join(c.Dir, "blobs")
	err = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files++
			bytes += info.Size()
		}
		return nil
	})
	return files, bytes, err
}
