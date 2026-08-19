package publish

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// The guard is the difference between publishing a transcription and
// republishing the magazine.
//
// Kvant is still being published. What this project has any business putting on
// a page is the text, which was read off the scans and written down here, and
// what it has no business putting anywhere is the scans themselves, the figures
// on them or the issue PDFs they came from. That is a decision about the work
// and not a detail of the build, so it is enforced on every byte written rather
// than left to whoever edits a template next.
//
// Copying a file is the obvious way to break it and it is not the only way. A
// page that never copies a scan but carries an img tag pointing at one has put
// the picture in front of the reader just the same, and the file it points at
// might be on somebody else's server. So the guard reads what goes out as well
// as counting it: an extension that is not text is refused, and so is a page
// that asks the browser to go and fetch something.

// allowedExt is what a static site of text is made of.
//
// The list is short on purpose and it is a whitelist rather than a blacklist,
// because the failure that matters is the format nobody thought of. SVG is not
// on it even though it is text, since an SVG is a picture and the question here
// is not what a file is encoded as.
var allowedExt = map[string]string{
	".html":  "a page",
	".css":   "the stylesheet",
	".woff2": "a vendored font",
	".json":  "the search index",
	".txt":   "a plain text file",
}

// fetchers are the elements that make a browser go and get something. An empty
// output cannot contain one, which is easy to check and easy to keep true.
var fetchers = []string{
	"<img", "<picture", "<source", "<video", "<audio",
	"<iframe", "<embed", "<object", "<script", "<canvas",
}

// scanExt is what a scan, a figure or a source PDF is called. A page that names
// one is pointing at it whether or not the build ever wrote it.
var scanExt = []string{
	".jpg", ".jpeg", ".png", ".pdf", ".webp", ".tif", ".tiff",
	".gif", ".bmp", ".djvu", ".ppm", ".svg",
}

// Guard reports what is wrong with a file about to be written to the site.
//
// It takes the bytes rather than a path to read later, so that the check
// happens before the write and not after it. A guard that runs over the built
// site can only tell you that you have already published the thing.
func Guard(name string, data []byte) error {
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := allowedExt[ext]; !ok {
		return fmt.Errorf("%s: the site is made of text and %s is not one of %s",
			name, quoteExt(ext), strings.Join(extList(), ", "))
	}
	if ext != ".html" {
		return nil
	}

	page := strings.ToLower(string(data))
	for _, tag := range fetchers {
		if strings.Contains(page, tag) {
			return fmt.Errorf("%s: %s> would send the reader off to fetch something", name, tag)
		}
	}
	// The extensions are looked for in what the page points at rather than in
	// the page, because a transcription contains the characters the magazine
	// printed and a body that quotes a filename is text like any other. Every
	// element that fetches on its own is already refused above, so a link and a
	// stylesheet URL are the two ways left to point at anything.
	for _, target := range references(page) {
		for _, bad := range scanExt {
			if strings.HasSuffix(target, bad) {
				return fmt.Errorf("%s: points at %s, and the scans are not ours to publish", name, target)
			}
		}
	}
	return nil
}

// references pulls out everything the page points at.
func references(page string) []string {
	var out []string
	for _, opener := range []string{`href="`, `href='`, "url("} {
		rest := page
		for {
			_, after, found := strings.Cut(rest, opener)
			if !found {
				break
			}
			closer := `"`
			switch opener {
			case `href='`:
				closer = `'`
			case "url(":
				closer = ")"
			}
			target, tail, closed := strings.Cut(after, closer)
			if !closed {
				break
			}
			out = append(out, strings.Trim(strings.TrimSpace(target), `"'`))
			rest = tail
		}
	}
	return out
}

func quoteExt(ext string) string {
	if ext == "" {
		return "a file with no extension"
	}
	return ext
}

func extList() []string {
	out := make([]string, 0, len(allowedExt))
	for ext := range allowedExt {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}
