package publish_test

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/publish"
)

// Kvant is still being published. The text was read off the scans and written
// down here and that is ours to show. The scans, the figures on them and the
// issue PDFs are not, and the whole point of putting that in a function is that
// it stops being a habit somebody has to remember.
func TestNothingButTextLeavesTheBuild(t *testing.T) {
	for _, test := range []struct {
		name string
		file string
		data string
		ok   bool
	}{
		{"a page", "1975/04/index.html", "<p>Текст</p>", true},
		{"the stylesheet", "assets/site.css", "body{}", true},
		{"a vendored font", "assets/fonts/KaTeX_Main-Regular.woff2", "\x00\x01", true},
		{"the search index", "search.json", "[]", true},

		{"a scan", "1975/04/pages/0011.jpg", "\xff\xd8\xff", false},
		{"an issue PDF", "1975/04/kvant_1975_4.pdf", "%PDF", false},
		{"a figure", "figures/fig1.png", "\x89PNG", false},
		{"something with no extension", "1975/04/Makefile", "all:", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := publish.Guard(test.file, []byte(test.data))
			if test.ok && err != nil {
				t.Errorf("the build refused %s: %v", test.file, err)
			}
			if !test.ok && err == nil {
				t.Errorf("the build wrote %s, want it refused", test.file)
			}
		})
	}
}

// Copying a scan is the obvious way to publish one and it is not the only way.
// A page that carries an img tag has put the picture in front of the reader
// just the same, and the file it points at is on somebody else's server.
func TestAPageMayNotSendTheReaderOffToFetchAPicture(t *testing.T) {
	for _, test := range []struct {
		name string
		page string
	}{
		{"an image", `<p>text</p><img src="/scans/0011.jpg">`},
		{"a picture element", `<picture><source srcset="a"></picture>`},
		{"an iframe", `<iframe src="https://elsewhere"></iframe>`},
		{"an object", `<object data="a"></object>`},
		{"an embed", `<embed src="a">`},
		{"a video", `<video></video>`},
		// The site reads with JavaScript off and that is a requirement rather
		// than a nicety, so a script tag is a defect wherever it came from.
		{"a script", `<script>fetch("/scan.jpg")</script>`},
		// Nothing is fetched here, but the reader is still being pointed at a
		// scan, and the guard is about what is published and not about what is
		// copied.
		{"a bare link to a scan", `<p><a href="/cache/blobs/0011.jpg">лист</a></p>`},
		{"a link to a source PDF", `<p><a href="kvant_1975_4.pdf">PDF</a></p>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := publish.Guard("page.html", []byte(test.page)); err == nil {
				t.Errorf("the build wrote %q, want it refused", test.page)
			}
		})
	}
}

// An angle bracket the magazine printed is text, and the renderer escapes it,
// so it must not read as a tag on the way out. Otherwise the guard would refuse
// perfectly good pages and somebody would turn it off.
func TestAnEscapedTagIsNotATag(t *testing.T) {
	page := `<p>Сравните &lt;img src=&quot;a.jpg&quot;&gt; и это.</p>`
	if err := publish.Guard("page.html", []byte(page)); err != nil {
		t.Errorf("the build refused a page of printed text: %v", err)
	}
}

// The refusal has to name the file and say what is wrong with it, because the
// person reading it is looking at a build that stopped.
func TestTheRefusalSaysWhichFileAndWhy(t *testing.T) {
	err := publish.Guard("1975/04/pages/0011.jpg", []byte("\xff\xd8"))
	if err == nil {
		t.Fatal("the build wrote a scan")
	}
	for _, want := range []string{"0011.jpg", "text"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal said %q, want %s in it", err, want)
		}
	}
}
