// Package textguard decides how each page of the magazine is going to be read.
//
// There are three ways to get the text of a Kvant page. The mirror's full issue
// PDFs from the late 2000s on carry a real text layer, and those pages can be
// read with pdftotext and no model at all. kvant.digital carries the
// publisher's own text for some articles, mostly recent ones, and those can be
// taken as they are. Everything else, which is most of fifty years, is a scan
// of a printed page and has to go through a vision model.
//
// The difference between the three is money and days, so which one a page takes
// is measured rather than assumed, and it is written down before a single model
// call is made. That way the cost of a decade can be argued about while it is
// still an argument.
package textguard

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/pdfsrc"
)

// The three paths, as they are written into the page manifest.
const (
	Native    = "native"
	Publisher = "publisher"
	Vision    = "vision"
)

// What a born digital issue looks like when it is measured.
const (
	// SamplePages is how many body pages are read to decide whether a file has
	// a text layer worth having.
	SamplePages = 8

	// MinLayerRunes is the fewest characters a sampled body page can hold and
	// still count as text. A set page of Kvant carries a few thousand; a scan
	// wrapped in a PDF carries nothing, and a scan with somebody else's OCR
	// behind it carries a few dozen per page of running heads and noise.
	MinLayerRunes = 500

	// MinCyrillic is the share of the letters that have to be Russian. It is
	// what catches a text layer that is there and is mojibake, which is worse
	// than one that is missing because it looks like success.
	MinCyrillic = 0.5
)

// What a complete page looks like inside a born digital issue.
const (
	// MinPageShare is how much of the issue's typical page a page has to carry
	// in its text layer to be read natively. A page whose text is well under
	// that is a page whose mathematics was set as pictures, and reading it
	// natively would produce a paragraph with the formulas silently missing,
	// which is the one failure nobody downstream can see.
	MinPageShare = 0.35

	// MaxImageShare is how much of a page can be covered by embedded images
	// before it is treated as a picture with a caption rather than as text.
	MaxImageShare = 0.5
)

// Guard measures issues and decides paths.
type Guard struct {
	Cache *fetch.Cache
	Run   pdfsrc.Runner

	// Log is where progress goes.
	Log func(format string, args ...any)
}

// New returns a guard that shells out to poppler for real.
func New(cache *fetch.Cache) *Guard {
	return &Guard{Cache: cache, Run: pdfsrc.ExecRunner{}, Log: func(string, ...any) {}}
}

// Issue decides the path for every sheet of one issue, writes the decisions
// into the page manifest, and returns the counts for the corpus manifest.
//
// The rows are the issue's table of contents, which is what says whether the
// publisher carries the text of the article a page belongs to. Passing none is
// fine and means no page can take the publisher path.
func (g *Guard) Issue(ctx context.Context, iss *manifest.Issue, rows []manifest.Row) (manifest.IssuePaths, error) {
	out := manifest.IssuePaths{Key: iss.Key, Year: iss.Year}

	idx, err := g.Cache.ReadIndex(iss.Key)
	if err != nil {
		return out, err
	}
	out.Sheet = len(idx.Sheets)

	// The PDF is measured whether or not the scan has been downloaded. Whether
	// a year is born digital is a question about the mirror's files, and it is
	// worth being able to answer it before committing to twenty gigabytes of
	// JPEG.
	native, err := g.native(ctx, idx)
	if err != nil {
		return out, err
	}
	if native != nil {
		out.PDF = &native.Layer
	}
	if len(idx.Sheets) == 0 {
		out.Note = "no scan yet, run kvant fetch pages"
		return out, nil
	}
	pub := publisherPages(rows)

	for i := range idx.Sheets {
		s := &idx.Sheets[i]
		path, why := decide(s, native, pub)
		s.Path, s.Why = path, why
		switch path {
		case Native:
			out.Native++
		case Publisher:
			out.Publisher++
		case Vision:
			out.Vision++
		default:
			out.Undecided++
		}
	}
	if out.Note == "" {
		out.Note = note(native, out)
	}
	if err := g.Cache.WriteIndex(idx); err != nil {
		return out, err
	}
	g.Log("%s: %d native, %d publisher, %d vision", iss.Key, out.Native, out.Publisher, out.Vision)
	return out, nil
}

// decide is the whole rule, in one place, in order of preference. Native text
// is the publisher's own typesetting and costs nothing. Publisher text is
// somebody else's transcription and is free but unverified. Vision is correct
// and expensive, and is what everything falls back to.
func decide(s *fetch.Sheet, native *nativeIssue, pub map[int]bool) (path, why string) {
	if !s.Have() {
		return "", "not downloaded"
	}
	if native != nil {
		switch path, why := native.page(s.Page); path {
		case Native:
			return path, why
		case Vision:
			if pub[s.Page] {
				return Publisher, "the publisher carries this article and " + why
			}
			return Vision, why
		}
		// An empty path means the file has no page for this sheet, which the
		// covers never are, so the other two paths get their turn.
	}
	if s.Page > 0 && pub[s.Page] {
		return Publisher, "the publisher carries the text of this article"
	}
	switch {
	case s.Page == 0:
		return Vision, "a cover or an insert, which nothing carries the text of"
	case native != nil && !native.Layer.Born:
		return Vision, native.Layer.Why
	case native != nil:
		return Vision, fmt.Sprintf("the issue PDF has no page %d", s.Page)
	}
	return Vision, "no text layer and no publisher text"
}

// nativeIssue is a measured issue PDF and the per page counts taken off it.
type nativeIssue struct {
	Layer manifest.Layer

	// runes is the count of non space characters on each page of the file, in
	// file order, and median is what a page of this issue normally holds.
	runes  []int
	median int

	// images is the share of each page covered by embedded images.
	images []float64
}

// page decides one printed page number against the file.
func (n *nativeIssue) page(printed int) (path, why string) {
	if !n.Layer.Born || printed <= 0 {
		return "", ""
	}
	i := printed + n.Layer.Offset - 1
	if i < 0 || i >= len(n.runes) {
		return "", ""
	}
	if share := n.images[i]; share > MaxImageShare {
		return Vision, fmt.Sprintf("%.0f%% of the page is a picture", share*100)
	}
	if n.median > 0 && float64(n.runes[i]) < MinPageShare*float64(n.median) {
		return Vision, fmt.Sprintf("the text layer holds %d characters where this issue's pages hold %d",
			n.runes[i], n.median)
	}
	return Native, fmt.Sprintf("%d characters of native text", n.runes[i])
}

// native measures the issue PDF, if the issue has one in the cache. It returns
// nil for an issue with no PDF, which is most of the archive.
func (g *Guard) native(ctx context.Context, idx *fetch.Index) (*nativeIssue, error) {
	if idx.PDF == nil || !g.Cache.Has(idx.PDF.SHA256) {
		return nil, nil
	}
	src := &pdfsrc.Source{Path: g.Cache.Path(idx.PDF.SHA256), Run: g.Run}

	info, err := src.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", idx.Issue, err)
	}
	fonts, err := src.Fonts(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", idx.Issue, err)
	}
	// One call for the whole file rather than one per page. pdftotext separates
	// pages with a form feed, which is the only thing that says where a page
	// ended once the layout is gone.
	whole, err := src.Text(ctx, 0, 0, false)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", idx.Issue, err)
	}
	pages := splitPages(whole, info.Pages)
	pages, recoded := recode(pages)

	out := &nativeIssue{}
	out.runes = make([]int, len(pages))
	for i, txt := range pages {
		out.runes[i] = pdfsrc.MeasureText(i+1, txt).Runes
	}
	out.median = median(out.runes)

	images, err := src.Images(ctx, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", idx.Issue, err)
	}
	out.images = imageShare(images, len(pages), info.WidthPt, info.HeightPt)

	out.Layer = measure(info, fonts, pages)
	if recoded {
		out.Layer.Recode = pdfsrc.CP1251
		out.Layer.Why += ", once the text layer is read as " + pdfsrc.CP1251 + " rather than as what pdftotext made of it"
	}
	if out.Layer.Born {
		off, votes := offset(pages)
		out.Layer.Offset = off
		out.Layer.Why += ", " + folioNote(off, votes)
	}
	return out, nil
}

// recode repairs the pages of a file that came out of pdftotext as Latin-1
// letters where Russian was set, and says whether any page needed it.
//
// It is decided per page and not per file, because the files that need it are
// mixed: the cover and the contents of a 2021 issue come through as proper
// Unicode and the body pages behind them do not, and one good page must not
// speak for the sixty broken ones.
//
// A page that already reads as Russian is never touched, and a page that does
// not is only rewritten if the rewrite comes out as Russian. Anything that fails
// both tests, which is what a genuine scan looks like, is left as it was and
// goes to vision like the rest of the archive.
func recode(pages []string) ([]string, bool) {
	out := make([]string, len(pages))
	fixed := 0
	for i, txt := range pages {
		out[i] = txt
		if pdfsrc.MeasureText(i+1, txt).Cyrillic >= MinCyrillic {
			continue
		}
		got, changed := pdfsrc.RecodeCP1251(txt)
		if !changed {
			continue
		}
		if pdfsrc.MeasureText(i+1, got).Cyrillic >= MinCyrillic {
			out[i] = got
			fixed++
		}
	}
	if fixed == 0 {
		return pages, false
	}
	return out, true
}

// measure decides whether a file is born digital, on the sampled body pages
// rather than on the whole file. The first pages are the cover and the
// contents and the last are the answers column, and none of them look like the
// body they would be standing for.
func measure(info pdfsrc.Info, fonts []pdfsrc.Font, pages []string) manifest.Layer {
	out := manifest.Layer{Pages: info.Pages, Fonts: len(fonts)}
	for _, f := range fonts {
		if f.Embedded {
			out.Embedded++
		}
		if f.Subset {
			out.Subset++
		}
	}

	var runes []int
	var cyrillic float64
	sampled := pdfsrc.SpreadPages(len(pages), SamplePages)
	for _, p := range sampled {
		m := pdfsrc.MeasureText(p, pages[p-1])
		runes = append(runes, m.Runes)
		cyrillic += m.Cyrillic
	}
	out.Runes = median(runes)
	if len(sampled) > 0 {
		out.Cyrillic = cyrillic / float64(len(sampled))
	}

	switch {
	case out.Embedded == 0:
		out.Why = "no embedded fonts, so the file is a scan in a wrapper"
	case out.Runes < MinLayerRunes:
		out.Why = fmt.Sprintf("a body page carries %d characters, which is a page number and not a page", out.Runes)
	case out.Cyrillic < MinCyrillic:
		out.Why = fmt.Sprintf("only %.0f%% of the letters are Russian, so the text layer is mojibake", out.Cyrillic*100)
	default:
		out.Born = true
		out.Why = fmt.Sprintf("%d of %d fonts embedded, %d characters and %.0f%% Russian on a body page",
			out.Embedded, out.Fonts, out.Runes, out.Cyrillic*100)
	}
	return out
}

// offset is how far the file's page numbering runs ahead of the numbering
// printed on the pages, measured by reading the folios off the pages, and how
// many pages agreed on it.
//
// It matters because everything else in this project is keyed on the printed
// page number, and a file that opens with two cover surfaces answers a request
// for printed page 30 with page 32. Getting it wrong does not fail, it quietly
// transcribes the wrong page, so it is measured and not assumed, and a file
// whose folios do not read clearly is left numbered as it is rather than
// shifted on a guess.
func offset(pages []string) (off, votes int) {
	cast := map[int]int{}
	for i, txt := range pages {
		for _, n := range edgeNumbers(txt) {
			if n <= 0 || n > len(pages) {
				continue
			}
			cast[i+1-n]++
		}
	}
	second := 0
	for candidate, n := range cast {
		switch {
		// A tie goes to the smaller offset, so that a file that can be read two
		// ways does not depend on the order of a map.
		case n > votes || (n == votes && candidate < off):
			off, votes, second = candidate, n, votes
		case n > second:
			second = n
		}
	}
	if votes < minFolios(len(pages)) || votes < 2*second {
		return 0, 0
	}
	return off, votes
}

// minFolios is how many pages have to agree before the file is renumbered. A
// real issue gets thirty of its sixty eight pages voting the same way, so a
// quarter is a low bar for a file whose folios read at all and a high one for a
// file where they do not.
//
// The 2021 double issue is why it is a share and not a count. Its folios are
// set in Symbol and come out of pdftotext as punctuation, so nothing votes,
// and seven equation numbers scattered through the text agreed on an offset of
// fifty one and carried it.
func minFolios(pages int) int {
	return max(MinFolios, pages/4)
}

// MinFolios is the floor under that share, for a file short enough that a
// quarter of it is nothing.
const MinFolios = 3

// edgeNumbers is the standalone numbers at the top and bottom of a page, which
// is where a magazine prints the folio. It looks at three lines rather than
// one, because the folio usually sits under a running head.
func edgeNumbers(txt string) []int {
	var lines []string
	for _, line := range strings.Split(txt, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	const edge = 3
	if len(lines) > 2*edge {
		lines = append(lines[:edge:edge], lines[len(lines)-edge:]...)
	}

	var out []int
	for _, line := range lines {
		for _, field := range strings.Fields(line) {
			// Only a bare number counts. An equation number is in brackets and
			// a date has punctuation in it, and neither is a folio.
			if n, err := strconv.Atoi(field); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}

// folioNote says how the file's numbering was decided, because an issue whose
// pages came out shifted is a thing somebody will want to check.
func folioNote(off, votes int) string {
	if votes == 0 {
		return "the folios do not read, so the file is numbered as printed"
	}
	if off == 0 {
		return fmt.Sprintf("%d folios agree the file is numbered as printed", votes)
	}
	return fmt.Sprintf("%d folios put the printed numbering %d pages behind the file", votes, off)
}

// imageShare is how much of each page is covered by embedded images, as a
// share of the page area. A page of a born digital issue whose formulas were
// pasted in as pictures shows up here and nowhere else.
func imageShare(images []pdfsrc.Image, pages int, widthPt, heightPt float64) []float64 {
	out := make([]float64, pages)
	area := widthPt / 72 * heightPt / 72
	if area <= 0 {
		return out
	}
	for _, img := range images {
		if img.Page < 1 || img.Page > pages || img.XPPI <= 0 || img.YPPI <= 0 {
			continue
		}
		inches := float64(img.Width) / float64(img.XPPI) * float64(img.Height) / float64(img.YPPI)
		out[img.Page-1] += inches / area
	}
	for i, share := range out {
		// Images overlap, and a page cannot be more than covered.
		out[i] = min(share, 1)
	}
	return out
}

// splitPages cuts pdftotext output into pages on the form feeds it writes
// between them. It pads or trims to the page count pdfinfo gave, so that an
// index into the result is a page number whatever the file did.
func splitPages(text string, pages int) []string {
	out := strings.Split(text, "\f")
	// The output ends with a form feed, so the last piece is empty and is not a
	// page.
	if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if pages <= 0 {
		return out
	}
	for len(out) < pages {
		out = append(out, "")
	}
	return out[:pages]
}

// publisherPages is the printed pages covered by an article the publisher
// carries the text of.
//
// A row that names its pages is taken at its word. A row that names only where
// it starts covers everything up to where the next row starts, which is what
// the printed contents means and is close enough to price a decade with.
func publisherPages(rows []manifest.Row) map[int]bool {
	out := map[int]bool{}
	starts := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.Page > 0 {
			starts = append(starts, r.Page)
		}
	}
	slices.Sort(starts)

	for _, r := range rows {
		if !r.HasText {
			continue
		}
		if len(r.Pages) > 0 {
			for _, p := range r.Pages {
				out[p] = true
			}
			continue
		}
		if r.Page <= 0 {
			continue
		}
		end := r.Page
		if i := slices.IndexFunc(starts, func(p int) bool { return p > r.Page }); i >= 0 {
			end = starts[i] - 1
		}
		for p := r.Page; p <= end; p++ {
			out[p] = true
		}
	}
	return out
}

// note is the one line that goes in the corpus manifest next to the counts.
func note(native *nativeIssue, counts manifest.IssuePaths) string {
	switch {
	case native == nil && counts.Publisher > 0:
		return "scan only, with publisher text for part of it"
	case native == nil:
		return "scan only"
	case !native.Layer.Born:
		return "the mirror has a PDF and it is a scan: " + native.Layer.Why
	case counts.Vision > 0:
		return fmt.Sprintf("born digital, with %d pages the text layer does not cover", counts.Vision)
	default:
		return "born digital"
	}
}

func median(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Clone(xs)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}
