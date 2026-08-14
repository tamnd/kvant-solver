package pdfsrc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/charmap"
)

// Source is one PDF on disk.
type Source struct {
	Path string
	Run  Runner
}

// Open checks the file is readable and returns a Source that shells out for
// real. A Source literal with a FakeRunner is how tests avoid poppler.
func Open(path string) (*Source, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	return &Source{Path: path, Run: ExecRunner{}}, nil
}

// Info is what pdfinfo reports.
type Info struct {
	Pages      int
	Title      string
	Producer   string
	Creator    string
	PDFVersion string
	WidthPt    float64
	HeightPt   float64
	Encrypted  bool
}

// Info runs pdfinfo and reads its key and value output.
func (s *Source) Info(ctx context.Context) (Info, error) {
	out, err := s.Run.Run(ctx, "pdfinfo", s.Path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "Pages":
			info.Pages, _ = strconv.Atoi(val)
		case "Title":
			info.Title = val
		case "Producer":
			info.Producer = val
		case "Creator":
			info.Creator = val
		case "PDF version":
			info.PDFVersion = val
		case "Encrypted":
			info.Encrypted = val != "no"
		case "Page size":
			// "595.28 x 841.89 pts (A4)"
			f := strings.Fields(val)
			if len(f) >= 3 && f[1] == "x" {
				info.WidthPt, _ = strconv.ParseFloat(f[0], 64)
				info.HeightPt, _ = strconv.ParseFloat(f[2], 64)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return info, err
	}
	if info.Pages == 0 {
		return info, fmt.Errorf("pdfinfo %s: no page count in the output", filepath.Base(s.Path))
	}
	return info, nil
}

// Text extracts the native text layer for a page range, inclusive at both
// ends. A range of 0 to 0 means the whole file. Layout keeps the columns where
// they were printed, which is what a two column magazine needs if the text is
// to be read in the order it was set.
func (s *Source) Text(ctx context.Context, first, last int, layout bool) (string, error) {
	var args []string
	if layout {
		args = append(args, "-layout")
	}
	if first > 0 {
		args = append(args, "-f", strconv.Itoa(first))
	}
	if last > 0 {
		args = append(args, "-l", strconv.Itoa(last))
	}
	args = append(args, s.Path, "-")
	out, err := s.Run.Run(ctx, "pdftotext", args...)
	return string(out), err
}

// Font is one row of pdffonts.
type Font struct {
	Name     string
	Type     string
	Encoding string
	Embedded bool
	Subset   bool
}

// Fonts lists the fonts the file uses.
func (s *Source) Fonts(ctx context.Context) ([]Font, error) {
	out, err := s.Run.Run(ctx, "pdffonts", s.Path)
	if err != nil {
		return nil, err
	}
	var fonts []Font
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "name") || strings.HasPrefix(line, "---") || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Fields(line)
		// The trailing columns are emb, sub, uni and a two field object id.
		if len(f) < 7 {
			continue
		}
		n := len(f)
		fonts = append(fonts, Font{
			Name:     f[0],
			Type:     strings.Join(f[1:n-6], " "),
			Encoding: f[n-6],
			Embedded: f[n-5] == "yes",
			Subset:   f[n-4] == "yes",
		})
	}
	return fonts, sc.Err()
}

// Image is one row of pdfimages -list.
type Image struct {
	Page   int
	Num    int
	Type   string
	Width  int
	Height int
	Color  string
	Comp   int
	BPC    int
	Enc    string
	XPPI   int
	YPPI   int
}

// Images lists the images embedded in a page range.
func (s *Source) Images(ctx context.Context, first, last int) ([]Image, error) {
	args := []string{"-list"}
	if first > 0 {
		args = append(args, "-f", strconv.Itoa(first))
	}
	if last > 0 {
		args = append(args, "-l", strconv.Itoa(last))
	}
	args = append(args, s.Path)
	out, err := s.Run.Run(ctx, "pdfimages", args...)
	if err != nil {
		return nil, err
	}
	var images []Image
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// page num type width height color comp bpc enc interp object id x-ppi y-ppi size ratio
		if len(f) < 14 {
			continue
		}
		page, err := strconv.Atoi(f[0])
		if err != nil {
			continue // the header row, or a separator
		}
		img := Image{Page: page, Type: f[2], Color: f[5], Enc: f[8]}
		img.Num, _ = strconv.Atoi(f[1])
		img.Width, _ = strconv.Atoi(f[3])
		img.Height, _ = strconv.Atoi(f[4])
		img.Comp, _ = strconv.Atoi(f[6])
		img.BPC, _ = strconv.Atoi(f[7])
		// The object id column is two fields wide, "14 0", so the two
		// resolutions land at 12 and 13 rather than at 11 and 12.
		img.XPPI, _ = strconv.Atoi(f[12])
		img.YPPI, _ = strconv.Atoi(f[13])
		images = append(images, img)
	}
	return images, sc.Err()
}

// Render writes one page as a PNG under dir and returns the path it wrote.
// This is how a page of a born digital issue becomes an image for a model to
// read when its text layer turns out not to be enough.
func (s *Source) Render(ctx context.Context, page, dpi int, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	prefix := filepath.Join(dir, fmt.Sprintf("p%04d", page))
	_, err := s.Run.Run(ctx, "pdftoppm",
		"-png", "-r", strconv.Itoa(dpi),
		"-f", strconv.Itoa(page), "-l", strconv.Itoa(page),
		"-singlefile", s.Path, prefix)
	if err != nil {
		return "", err
	}
	return prefix + ".png", nil
}

// PageText is what the text layer of one page holds.
type PageText struct {
	Page  int
	Runes int

	// Cyrillic is the share of the letters that are Russian. It is the measure
	// that separates a real Russian text layer from a mojibake one and from a
	// page that carries nothing but a page number and a formula rendered as a
	// picture.
	Cyrillic float64
}

// Measure reads the text layer of one page and counts what is in it.
func (s *Source) Measure(ctx context.Context, page int) (PageText, error) {
	txt, err := s.Text(ctx, page, page, false)
	if err != nil {
		return PageText{}, err
	}
	return MeasureText(page, txt), nil
}

// CP1251 names the one repair this package knows how to make to a text layer.
// It goes into the manifest so that whatever reads a page later applies the
// same repair the measurement was made on.
const CP1251 = "cp1251"

// RecodeCP1251 repairs a page of text that came out of pdftotext as Western
// European letters where Russian was meant.
//
// Some of the mirror's own PDFs, the ones distilled on Windows, embed their
// fonts with a custom encoding and no ToUnicode map. pdftotext has nothing to
// look the glyphs up in, so it hands back the raw bytes as if they were
// Latin-1, and КВАНТ arrives as ÊÂÀÍÒ. The bytes are the right bytes and only
// the table is wrong: writing each character back out as the byte it stands for
// and reading the result as CP1251 gives the Russian back exactly, with no
// guessing and no model.
//
// Characters above U+00FF are left exactly where they are, because these pages
// are mixed: the mathematics on them is set in Symbol and comes through as
// private use glyphs, and the arithmetic below would have nothing to say about
// those. It reports whether it changed anything at all, which is false for text
// that was pure ASCII and had nothing to repair.
func RecodeCP1251(txt string) (string, bool) {
	var b strings.Builder
	b.Grow(len(txt))
	raw := make([]byte, 0, len(txt))
	changed := false

	flush := func() {
		if len(raw) == 0 {
			return
		}
		out, err := charmap.Windows1251.NewDecoder().Bytes(raw)
		if err != nil {
			b.Write(raw)
		} else {
			b.Write(out)
		}
		raw = raw[:0]
	}
	for _, r := range txt {
		if r > 0xFF {
			flush()
			b.WriteRune(r)
			continue
		}
		if r > 0x7F {
			changed = true
		}
		raw = append(raw, byte(r))
	}
	flush()
	return b.String(), changed
}

// MeasureText counts a string that has already been extracted. It is separate
// from Measure so that the counting can be tested without a PDF.
func MeasureText(page int, txt string) PageText {
	out := PageText{Page: page}
	letters, cyrillic := 0, 0
	for _, r := range txt {
		if !unicode.IsSpace(r) {
			out.Runes++
		}
		if unicode.IsLetter(r) {
			letters++
			if unicode.Is(unicode.Cyrillic, r) {
				cyrillic++
			}
		}
	}
	if letters > 0 {
		out.Cyrillic = float64(cyrillic) / float64(letters)
	}
	return out
}

// SpreadPages picks n pages spread evenly through the middle half of a file,
// or every page there is when the file is shorter than n.
//
// The middle half is deliberate. The first pages of an issue are the cover and
// the contents, and the last are the answers column and the colophon, and none
// of those look like the body they are being taken to stand for.
func SpreadPages(pages, n int) []int {
	if pages <= 0 || n <= 0 {
		return nil
	}
	if pages <= n {
		out := make([]int, pages)
		for i := range out {
			out[i] = i + 1
		}
		return out
	}
	lo, hi := max(pages/4, 1), pages*3/4
	out := make([]int, 0, n)
	for i := range n {
		p := lo
		if n > 1 {
			p = lo + (hi-lo)*i/(n-1)
		}
		out = append(out, p)
	}
	return out
}
