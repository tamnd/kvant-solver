// Package figures finds the pictures on a scanned sheet and cuts them out.
//
// The reading kept the words and marked where each picture stood, and that is
// all a transcription can do: an article about a pendulum is half a drawing.
// The pictures are on the same sheets the words were read from, so they can be
// cut out of them, and what has to be known is where each one is.
//
// The detector is plain image arithmetic and not a model. A sheet of Kvant is
// black type in columns with pictures between, and type has a size: once the
// height of a letter is known, ink that is not letter sized is either a rule or
// a picture. Pictures are what is left after the letters and the rules are
// taken away, grown until their pieces join.
package figures

import (
	"image"
	"image/color"
	"sort"
)

// A Figure is one picture on a sheet, in the sheet's pixels.
type Figure struct {
	Box image.Rectangle
}

// tuning, in multiples of the letter height
const (
	joinGap   = 1.6 // pieces of one drawing closer than this are one drawing
	minHeight = 3.0 // a figure is at least this tall and
	minWidth  = 4.0 // this wide; smaller is a formula or a drop cap
	minSide   = 5.0 // and at least this on its longer side
	pad       = 0.4 // margin left round a crop
)

// Detect returns the figures on a sheet in reading order: down the left
// column, then down the right one, with a picture spanning both columns read
// where it starts.
func Detect(img image.Image) []Figure {
	m := newMask(img)
	comps := m.components()
	lh := letterHeight(comps)
	if lh == 0 {
		return nil
	}
	var seeds []image.Rectangle
	var letters []image.Rectangle
	for _, c := range comps {
		switch {
		case isLetter(c, lh):
			letters = append(letters, c)
		case isRule(c, lh, m.bounds):
			// column rules and frames are left out: a rule joins whatever it
			// passes, which would make one figure of a page
		case c.Dx()*c.Dy() >= int(lh*lh):
			seeds = append(seeds, c)
		}
	}
	var out []Figure
	for _, g := range join(seeds, int(joinGap*lh)) {
		b := g.box
		if !figureLike(b, lh, letters) || bigType(g, lh) || m.reversed(b, lh) {
			continue
		}
		p := int(pad * lh)
		out = append(out, Figure{Box: b.Inset(-p).Intersect(m.bounds)})
	}
	order(out, m.bounds)
	return out
}

// mask is the ink of a sheet: dark, or coloured, or grey enough to be a tint.
type mask struct {
	bounds image.Rectangle
	w, h   int
	ink    []bool
}

func newMask(img image.Image) *mask {
	b := img.Bounds()
	m := &mask{bounds: image.Rect(0, 0, b.Dx(), b.Dy()), w: b.Dx(), h: b.Dy()}
	m.ink = make([]bool, m.w*m.h)
	// the scan's own edge is shadow and paper colour, not ink
	edgeX, edgeY := m.w/60, m.h/60
	for y := edgeY; y < m.h-edgeY; y++ {
		for x := edgeX; x < m.w-edgeX; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			r, g, bl = r>>8, g>>8, bl>>8
			lum := (299*r + 587*g + 114*bl) / 1000
			hi, lo := max(r, g, bl), min(r, g, bl)
			m.ink[y*m.w+x] = lum < 170 || (hi-lo > 60 && lum < 235)
		}
	}
	return m
}

// reversed is a block of type printed white on black: inked nearly all over,
// with letter shaped holes in it.
func (m *mask) reversed(b image.Rectangle, lh float64) bool {
	sub := &mask{bounds: image.Rect(0, 0, b.Dx(), b.Dy()), w: b.Dx(), h: b.Dy()}
	sub.ink = make([]bool, sub.w*sub.h)
	inked := 0
	for y := range sub.h {
		for x := range sub.w {
			on := m.ink[(b.Min.Y+y)*m.w+b.Min.X+x]
			if on {
				inked++
			}
			sub.ink[y*sub.w+x] = !on
		}
	}
	area := float64(sub.w * sub.h)
	if float64(inked) < 0.5*area {
		return false
	}
	var holes float64
	for _, c := range sub.components() {
		h := float64(c.Dy())
		if h >= 0.5*lh && h <= 1.6*lh && float64(c.Dx()) <= 2*h {
			holes += float64(c.Dx() * c.Dy())
		}
	}
	return holes > 0.08*area
}

// components are the bounding boxes of the ink's connected pieces.
func (m *mask) components() []image.Rectangle {
	seen := make([]bool, len(m.ink))
	var out []image.Rectangle
	stack := make([]int, 0, 1024)
	for i, on := range m.ink {
		if !on || seen[i] {
			continue
		}
		x0, y0, x1, y1 := m.w, m.h, 0, 0
		stack = append(stack[:0], i)
		seen[i] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := p%m.w, p/m.w
			x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= m.w || ny >= m.h {
						continue
					}
					q := ny*m.w + nx
					if m.ink[q] && !seen[q] {
						seen[q] = true
						stack = append(stack, q)
					}
				}
			}
		}
		out = append(out, image.Rect(x0, y0, x1+1, y1+1))
	}
	return out
}

// letterHeight is the commonest height among pieces shaped like letters,
// which on a page of text is the body type's.
func letterHeight(comps []image.Rectangle) float64 {
	count := map[int]int{}
	for _, c := range comps {
		h, w := c.Dy(), c.Dx()
		if h >= 8 && h <= 60 && w <= 2*h && w >= h/4 {
			count[h]++
		}
	}
	best, n := 0, 0
	for h, k := range count {
		// heights a pixel apart are the same letters; count the neighbours in
		k += count[h-1] + count[h+1]
		if k > n || (k == n && h < best) {
			best, n = h, k
		}
	}
	if n < 20 {
		return 0
	}
	return float64(best)
}

// isLetter is a piece of type: a letter, a few touching ones, punctuation.
func isLetter(c image.Rectangle, lh float64) bool {
	h, w := float64(c.Dy()), float64(c.Dx())
	return h <= 1.8*lh && w <= 6*lh
}

// isRule is a line ruled across the sheet: very thin and long.
func isRule(c image.Rectangle, lh float64, page image.Rectangle) bool {
	h, w := float64(c.Dy()), float64(c.Dx())
	thin := h < 0.5*lh || w < 0.5*lh
	long := w > 0.25*float64(page.Dx()) || h > 0.2*float64(page.Dy())
	return thin && long
}

// group is pieces joined into one box.
type group struct {
	box    image.Rectangle
	pieces []image.Rectangle
}

// join grows boxes into each other until no two are within gap.
func join(boxes []image.Rectangle, gap int) []group {
	out := make([]group, len(boxes))
	for i, b := range boxes {
		out[i] = group{box: b, pieces: []image.Rectangle{b}}
	}
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(out); i++ {
			for j := i + 1; j < len(out); j++ {
				if out[i].box.Inset(-gap).Overlaps(out[j].box) {
					out[i].box = out[i].box.Union(out[j].box)
					out[i].pieces = append(out[i].pieces, out[j].pieces...)
					out = append(out[:j], out[j+1:]...)
					merged = true
					j = i
				}
			}
		}
	}
	return out
}

// bigType is a line of type set large, a headline: several pieces each shaped
// like a letter, and the line no taller than its tallest letter much.
func bigType(g group, lh float64) bool {
	if len(g.pieces) < 2 {
		return false
	}
	tallest := 0
	for _, p := range g.pieces {
		h, w := p.Dy(), p.Dx()
		if float64(h) > 5*lh || w > 3*h {
			return false
		}
		tallest = max(tallest, h)
	}
	return g.box.Dy() < tallest*8/5
}

// figureLike rejects what is big and still not a picture: a formula, a drop
// cap, a headline set large, a frame round a block of type.
func figureLike(b image.Rectangle, lh float64, letters []image.Rectangle) bool {
	h, w := float64(b.Dy()), float64(b.Dx())
	if h < minHeight*lh || w < minWidth*lh || max(h, w) < minSide*lh {
		return false
	}
	// a headline: a strip of big letters, wide and short
	if h < 4.5*lh && w > 5*h {
		return false
	}
	// a box of type: lines of letters fill it. A drawing has a few labels.
	var area float64
	for _, l := range letters {
		if l.In(b) {
			area += float64(l.Dx() * l.Dy())
		}
	}
	return area < 0.22*w*h
}

// order sorts figures into the order a reader meets them.
func order(fs []Figure, page image.Rectangle) {
	mid := page.Dx() / 2
	col := func(b image.Rectangle) int {
		switch {
		case b.Dx() > page.Dx()*3/5:
			// across both columns: before the columns if it starts in the top
			// half, after them if in the bottom one
			if b.Min.Y < page.Dy()/2 {
				return 0
			}
			return 3
		case (b.Min.X+b.Max.X)/2 < mid:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(fs, func(i, j int) bool {
		ci, cj := col(fs[i].Box), col(fs[j].Box)
		if ci != cj {
			return ci < cj
		}
		return fs[i].Box.Min.Y < fs[j].Box.Min.Y
	})
}

// Outline draws the boxes on a copy of the sheet, which is how the detector is
// looked at.
func Outline(img image.Image, fs []Figure) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	red := color.RGBA{255, 0, 0, 255}
	for _, f := range fs {
		r := f.Box
		for t := range 4 {
			for x := r.Min.X; x < r.Max.X; x++ {
				out.Set(x, r.Min.Y+t, red)
				out.Set(x, r.Max.Y-1-t, red)
			}
			for y := r.Min.Y; y < r.Max.Y; y++ {
				out.Set(r.Min.X+t, y, red)
				out.Set(r.Max.X-1-t, y, red)
			}
		}
	}
	return out
}
