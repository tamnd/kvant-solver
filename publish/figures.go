package publish

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/figures"
)

// The figures are the one thing on the site that is not text. They are cut
// from the scans by kvant figures, served from their own host and listed per
// issue in the corpus, and an article shows each one at the anchor it was cut
// for: in place of its ⟦figure⟧ mark, or above its caption.
//
// A placed figure travels through the renderer as a sentinel, ⟦img FILE WxH⟧,
// because a body cannot carry markup: the renderer escapes it, which is what
// keeps an img tag a model wrote off the page. The sentinel names a file under
// figures.Base and nothing else, so it cannot point anywhere but there.

// figureFile is what a sentinel may name: a crop path the cutter writes.
const figureFile = `[0-9]{4}/[0-9-]+/[0-9]{4}-[0-9]+\.jpg`

var (
	figureSentinel = regexp.MustCompile(`⟦img (` + figureFile + `) ([0-9]+)x([0-9]+)⟧`)
	figurePara     = regexp.MustCompile(`<p>((?:⟦img [^⟧]+⟧\s*)+)</p>`)
	// ownFigure is the img tag figureMarkup writes, which the guard lets through.
	ownFigure = regexp.MustCompile(`<img src="` + regexp.QuoteMeta(figures.Base) + `/` + figureFile + `" width="[0-9]+" height="[0-9]+" loading="lazy" alt="">`)
)

// figureMarkup turns the sentinels Render left into the figures.
func figureMarkup(html string) string {
	html = figurePara.ReplaceAllString(html, `<figure class="scan">$1</figure>`)
	return figureSentinel.ReplaceAllString(html,
		`<img src="`+figures.Base+`/$1" width="$2" height="$3" loading="lazy" alt="">`)
}

// manifests caches each issue's figure list for the build.
type manifests struct {
	mu   sync.Mutex
	byID map[string]*figures.Manifest
}

func (s *Site) manifest(key corpus.IssueKey) *figures.Manifest {
	s.figs.mu.Lock()
	defer s.figs.mu.Unlock()
	if s.figs.byID == nil {
		s.figs.byID = map[string]*figures.Manifest{}
	}
	id := key.String()
	if m, ok := s.figs.byID[id]; ok {
		return m
	}
	m, err := figures.Read(s.Corpus.Root, id)
	if err != nil {
		m = &figures.Manifest{Issue: id} // a broken manifest costs the pictures, not the build
	}
	s.figs.byID[id] = m
	return m
}

// placeFigures puts an article's figures into its body at their anchors.
func (s *Site) placeFigures(lang string, key corpus.IssueKey, front corpus.ArticleFront, body string) string {
	m := s.manifest(key)
	if len(m.Figures) == 0 {
		return body
	}
	anchors := figures.Anchors(body)
	if len(anchors) == 0 {
		return body
	}
	var placed [][]figures.Entry
	if lang == corpus.DefaultLang || front.TranslatedFrom == "" {
		placed = placeAt(m, front, anchors)
	} else {
		// A translation's captions are in another language and cannot be
		// looked up. Its anchors are the original's, in the same order, so the
		// original is resolved and its figures dealt out by position, or by
		// figure number when the translation merged or dropped an anchor.
		var src corpus.ArticleFront
		srcBody, err := corpus.LoadUnchecked(filepath.Join(s.Corpus.Root, front.TranslatedFrom), &src)
		if err != nil {
			return body
		}
		from := figures.Anchors(srcBody)
		got := placeAt(m, src, from)
		placed = make([][]figures.Entry, len(anchors))
		if len(from) == len(anchors) {
			copy(placed, got)
		} else {
			used := make([]bool, len(from))
			for i, a := range anchors {
				for j, b := range from {
					if !used[j] && a.Number != "" && a.Number == b.Number {
						placed[i], used[j] = got[j], true
						break
					}
				}
			}
		}
	}
	return insert(body, anchors, placed)
}

// placeAt finds each anchor's crops: those cut for its key from the pages the
// article was assembled from, the page after its last included, since a page
// cut between two articles is listed under the first.
func placeAt(m *figures.Manifest, front corpus.ArticleFront, anchors []figures.Anchor) [][]figures.Entry {
	used := make([]bool, len(m.Figures))
	out := make([][]figures.Entry, len(anchors))
	for i, a := range anchors {
		if a.Key == "" {
			continue
		}
		for j, e := range m.Figures {
			if used[j] || e.Key != a.Key {
				continue
			}
			if front.PageFirst > 0 && (e.Page < front.PageFirst || e.Page > front.PageLast+1) {
				continue
			}
			out[i] = append(out[i], e)
			used[j] = true
		}
	}
	return out
}

// insert writes the sentinels: over a mark, or above a bare caption.
func insert(body string, anchors []figures.Anchor, placed [][]figures.Entry) string {
	lines := strings.Split(body, "\n")
	at := map[int]string{}
	mark := map[int]bool{}
	for i, a := range anchors {
		if i >= len(placed) || len(placed[i]) == 0 {
			continue
		}
		var b strings.Builder
		for k, e := range placed[i] {
			if k > 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "⟦img %s %dx%d⟧", e.File, e.Width, e.Height)
		}
		at[a.Line], mark[a.Line] = b.String(), a.Marker
	}
	if len(at) == 0 {
		return body
	}
	out := make([]string, 0, len(lines)+2*len(at))
	for i, line := range lines {
		s, ok := at[i]
		switch {
		case !ok:
			out = append(out, line)
		case mark[i]:
			out = append(out, "", s, "")
		default:
			out = append(out, "", s, "", line)
		}
	}
	return strings.Join(out, "\n")
}
