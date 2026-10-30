package publish

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The link checker runs over the finished directory, which is the one place the
// answer is knowable.
//
// Every href on this site is computed: from a page two levels down to a tag
// page one level down, from a problem to the issue that posed it if that issue
// was read and not otherwise. Each of those is a small sum over the depth of
// the file being written, and a small sum written in six places is a small sum
// that will be wrong in one of them. No amount of care in the templates catches
// that; walking the output and opening what it points at does.
//
// It is part of the build rather than a separate command because a site with a
// dead link in it is not finished, and the failure is always a bug here rather
// than a gap in the corpus. A page that has not been read yet is simply not
// linked to.

// Broken is one reference on the built site that goes nowhere.
type Broken struct {
	// Page is the file the reference is on, relative to the site root.
	Page string
	// Target is what it points at, as written.
	Target string
	// Why is what is wrong with it.
	Why string
}

func (b Broken) Error() string {
	return fmt.Sprintf("%s: %s: %s", b.Page, b.Target, b.Why)
}

// notOurs are files in the output that this project did not write.
//
// KaTeX's stylesheet names woff2, woff and ttf in one src list for every font
// it has, and only the woff2 is vendored, because every browser that has
// shipped since 2016 takes the first format it understands and the other two
// are a megabyte nobody would ever fetch. So the stylesheet points at files
// that are deliberately not there, and reporting that as a broken link would be
// reporting a decision as a defect.
//
// The page for a missing address is the other: it is served at whatever
// address was asked for, so its links are absolute from where the site is
// served and there is nothing relative to check them against.
var notOurs = map[string]bool{
	"assets/katex.min.css": true,
	"404.html":             true,
}

// offSite is the short list of places outside the site a page may point at:
// the repositories the site is built from.
var offSite = []string{"https://github.com/tamnd/"}

// meta is the elements that name an address for search engines rather than
// for readers. The canonical address and the same page in other languages are
// absolute on purpose, to the one copy of the site that is the real one, and
// they are not links a reader follows.
var meta = regexp.MustCompile(`<link rel="(?:canonical|alternate)"[^>]*>`)

// CheckLinks opens everything the built site points at.
//
// Only the site's own files are followed. There is nothing off site to check:
// the guard already refuses a page that would send the reader anywhere, so an
// absolute or external reference here is a bug and is reported as one rather
// than skipped.
func CheckLinks(root string) ([]Broken, error) {
	var broken []Broken
	exists := map[string]bool{}
	have := func(rel string) bool {
		if seen, ok := exists[rel]; ok {
			return seen
		}
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		exists[rel] = err == nil
		return err == nil
	}

	err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".html" && ext != ".css" {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		page := filepath.ToSlash(rel)
		if notOurs[page] {
			return nil
		}
		data, err := os.ReadFile(name) //nolint:gosec // the path came from walking root
		if err != nil {
			return err
		}
		for _, target := range references(meta.ReplaceAllString(string(data), "")) {
			if bad := resolve(page, target, have); bad != nil {
				broken = append(broken, *bad)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(broken, func(i, j int) bool {
		if broken[i].Page != broken[j].Page {
			return broken[i].Page < broken[j].Page
		}
		return broken[i].Target < broken[j].Target
	})
	return broken, nil
}

// resolve reports what is wrong with one reference, or nil if it lands.
func resolve(page, target string, have func(string) bool) *Broken {
	fail := func(why string) *Broken { return &Broken{Page: page, Target: target, Why: why} }

	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return fail("points at nothing")
	}
	// A reference to a place on the same page is not a file and is checked by
	// the id being there, which nothing here writes yet.
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "data:") {
		return nil
	}
	for _, ok := range offSite {
		if strings.HasPrefix(trimmed, ok) {
			return nil
		}
	}
	if strings.HasPrefix(trimmed, "//") || strings.Contains(before(trimmed, "/"), ":") {
		return fail("leaves the site, and this site is self contained")
	}
	if strings.HasPrefix(trimmed, "/") {
		// The site has to work unpacked into a subdirectory of somebody's
		// server, so every reference on it is relative. An absolute one would
		// work in exactly one deployment.
		return fail("is absolute, and the site must work from any directory")
	}
	clean := before(before(trimmed, "#"), "?")
	if clean == "" {
		return nil // a query or a fragment on the current page
	}
	full := path.Join(path.Dir(page), clean)
	if strings.HasPrefix(full, "..") {
		return fail("climbs out of the site root")
	}
	// An address is a file, a directory that has an index in it, or a page
	// written as name.html and linked to without the extension, which is how
	// both hosts serve it.
	switch {
	case strings.HasSuffix(clean, "/") || clean == "." || clean == "..":
		if !have(path.Join(full, "index.html")) {
			return fail("there is no " + path.Join(full, "index.html"))
		}
	case have(full) && !isDir(have, full):
	case have(full + ".html"):
	default:
		return fail("there is no " + full)
	}
	return nil
}

// isDir says whether an address that exists is a directory, which a link
// without a trailing slash does not reach on every host.
func isDir(have func(string) bool, full string) bool {
	return have(path.Join(full, "index.html")) && !strings.Contains(path.Base(full), ".")
}

// before is the part of a string up to the first separator, or all of it.
func before(s, sep string) string {
	head, _, _ := strings.Cut(s, sep)
	return head
}
