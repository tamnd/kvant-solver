package ocr

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// FolioBand is the fraction of the page height the band falls back to when the
// foot cannot be found by looking, which is a full bleed illustration that runs
// off the bottom of the sheet.
//
// It is a fallback and not the method, and the difference was paid for. A fixed
// band was the first attempt and it cannot work, because the foot margin of
// these scans moves: across the 84 sheets of 1975 №1 the printed number sits
// anywhere from 2.7 to 6.5 per cent up from the bottom edge. Four per cent cuts
// through the digits on the low sheets, so 51 comes back as 31 and 17 as 13,
// and every one of those is a page the folio rule then rejects for disagreeing
// with the manifest. Widening it to five and a half swallows the last line of
// the column instead, and the number the model reads first is a footnote.
// Measured over all 84: fixed four per cent got 74 right with 2 wrong, fixed
// five and a half got 75 with 5 wrong, and finding the line got 81 with none
// wrong.
const FolioBand = 0.04

// Where the foot is looked for, all as fractions of the page height so that a
// scan at a different resolution reads the same.
const (
	// footLook is how far up from the bottom edge the search goes. Past this is
	// body text on every sheet of the run.
	footLook = 0.12
	// footInk is how many dark pixels a row needs before it counts as a row of
	// something. One or two is dust on the platen, and these are scans of
	// twenty year old paper.
	footInk = 2
	// footPad is kept either side of the line, because a crop flush against the
	// ink reads worse than one with white around it.
	footPad = 0.005
	// footTall is the tallest a line of type can be. Anything deeper is a
	// picture reaching the foot, not a page number.
	footTall = 0.035
	// footHigh is how far up the line may start and still be the folio.
	footHigh = 0.09
)

// Folioer reads the printed page number off the foot of a sheet.
//
// It is here because the fast lane does not give one. A document model is a
// recogniser: GLM-OCR drops running heads and folios as furniture, and asking
// for them in the prompt does not work, it made the model nineteen times slower
// and still returned no folio on any of nine sheets. Two sentences of
// instruction are two sentences it was not trained to obey.
//
// So the number is read the same way anything else is read, by looking at the
// part of the picture it is printed on. A 1200 by 74 band is a fraction of the
// tokens of a page, and on the 4090 nine sheets came back in a second, all nine
// correct. That is 0.11 seconds a page on top of 1.36, for the one piece of
// evidence three later stages are built on.
//
// The alternative was to take the number from the issue manifest, and it is
// worth writing down why that is not the same thing. Rule 5 exists to catch a
// sheet transcribed out of order or a scan with a page missing from the middle.
// A folio copied from the manifest agrees with the manifest by construction, so
// the rule would pass on exactly the runs it was written to fail.
type Folioer struct {
	Engine Engine
	// Band overrides FolioBand, for a decade whose folios sit somewhere else.
	Band float64
}

// Read returns the number printed at the foot of a page, and whether there was
// one. A page with no number is the normal answer for a cover, not a failure.
func (f *Folioer) Read(ctx context.Context, image string) (int, bool, error) {
	if f == nil || f.Engine == nil {
		return 0, false, nil
	}
	crop, err := os.CreateTemp("", "kvant-folio-*.jpg")
	if err != nil {
		return 0, false, err
	}
	crop.Close()
	defer os.Remove(crop.Name())

	if err := CropFoot(image, crop.Name(), f.band()); err != nil {
		return 0, false, err
	}
	answer, err := f.Engine.Read(ctx, crop.Name())
	if err != nil {
		return 0, false, err
	}
	number, ok := ParseFolio(answer)
	return number, ok, nil
}

func (f *Folioer) band() float64 {
	if f.Band > 0 && f.Band < 1 {
		return f.Band
	}
	return FolioBand
}

// CropFoot writes the strip of a page the printed number is on as its own JPEG.
//
// It is the standard library and nothing else. A crop is a sub image and a JPEG
// is an encoder, so the one dependency this could have picked up, a resizer, is
// avoided by not resizing: the band was read correctly at its own size and
// doubling it only cost time.
func CropFoot(from, to string, band float64) error {
	file, err := os.Open(from)
	if err != nil {
		return err
	}
	defer file.Close()
	full, err := jpeg.Decode(file)
	if err != nil {
		return fmt.Errorf("%s: %w", from, err)
	}

	sub, ok := full.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return fmt.Errorf("%s: a %T cannot be cropped", from, full)
	}

	out, err := os.Create(to)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := jpeg.Encode(out, sub.SubImage(FootBand(full, band)), &jpeg.Options{Quality: 95}); err != nil {
		return err
	}
	return out.Close()
}

// FootBand is the strip of a page image the printed number is on.
//
// The folio is the last line of ink on a page, so it is found rather than
// assumed: walk up from the bottom edge to the first row carrying ink, then up
// again to the first row that carries none, and that is the line. Two things
// disqualify what is found. A band deeper than a line of type is a picture
// running off the foot of the sheet, and a band that starts high up is the last
// line of the column on a page that prints no number at all. Either way the
// fallback band is returned, which reads as none, which is the true answer for
// the colour inserts this issue has two of.
func FootBand(img image.Image, band float64) image.Rectangle {
	bounds := img.Bounds()
	height := bounds.Dy()
	limit := bounds.Max.Y - int(float64(height)*footLook)

	y := bounds.Max.Y - 1
	for y >= limit && !inked(img, bounds, y) {
		y--
	}
	if y >= limit {
		bottom := y
		for y >= limit && inked(img, bounds, y) {
			y--
		}
		top := y + 1
		if bottom-top <= int(float64(height)*footTall) && bounds.Max.Y-top <= int(float64(height)*footHigh) {
			pad := max(2, int(float64(height)*footPad))
			return image.Rect(bounds.Min.X, max(bounds.Min.Y, top-pad),
				bounds.Max.X, min(bounds.Max.Y, bottom+pad+1))
		}
	}

	tall := max(1, int(float64(height)*band))
	return image.Rect(bounds.Min.X, bounds.Max.Y-tall, bounds.Max.X, bounds.Max.Y)
}

// inked says whether a row of the image carries print.
func inked(img image.Image, bounds image.Rectangle, y int) bool {
	found := 0
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		if dark(img, x, y) {
			found++
			if found > footInk {
				return true
			}
		}
	}
	return false
}

// dark says whether one pixel is print rather than paper. The two fast paths
// are the two things a scan decodes to, and they matter: this runs over a
// quarter of a million pixels a page and the general path converts a colour per
// pixel to answer a question about brightness.
func dark(img image.Image, x, y int) bool {
	switch src := img.(type) {
	case *image.YCbCr:
		return src.Y[src.YOffset(x, y)] < 128
	case *image.Gray:
		return src.GrayAt(x, y).Y < 128
	}
	return color.GrayModel.Convert(img.At(x, y)).(color.Gray).Y < 128
}

// digits is the number in the band. The band holds the folio and, on some
// pages, a signature mark or the last word of a hyphenated line, so this takes
// the first thing that is a plausible page number rather than the first thing
// that is a number.
var digits = regexp.MustCompile(`\d+`)

// ParseFolio picks the printed number out of what came back from the band.
func ParseFolio(answer string) (int, bool) {
	for _, match := range digits.FindAllString(answer, -1) {
		number, err := strconv.Atoi(match)
		if err != nil {
			continue
		}
		// The magazine ran to 80 pages and never past 128, and a year is four
		// digits, so anything above this is a date or a problem number that
		// found its way into the band.
		if number >= 1 && number <= 128 {
			return number, true
		}
	}
	return asLetters(answer)
}

// asLetters is the one shape of wrong answer worth reading anyway.
//
// A band is a picture of two glyphs and nothing else, with no word around them
// to say they are a number, so a model reading it as text sometimes returns the
// letters they look like. Sheet 57 of 1975 №1 came back as SS, which is 55.
//
// It is deliberately narrow. The whole answer has to be one or two of the six
// characters that a printed digit is mistaken for, which the folio of a page is
// and a line of Russian is not. Anything looser would start inventing page
// numbers out of words, and a wrong number here is worse than none: none is
// caught by the folio rule as a page with no printed number, which is true of
// some pages, and a wrong one places the page somewhere it was never printed.
var asDigit = strings.NewReplacer("S", "5", "O", "0", "I", "1", "L", "1", "B", "8", "Z", "2")

var lettersOnly = regexp.MustCompile(`^[SOILBZ]{1,2}$`)

func asLetters(answer string) (int, bool) {
	clean := strings.ToUpper(strings.TrimSpace(answer))
	clean = strings.Trim(clean, "`\n\t ")
	clean = strings.TrimSpace(strings.TrimPrefix(clean, "MARKDOWN"))
	if !lettersOnly.MatchString(clean) {
		return 0, false
	}
	number, err := strconv.Atoi(asDigit.Replace(clean))
	if err != nil || number < 1 || number > 128 {
		return 0, false
	}
	return number, true
}

// MarkFolio puts the folio line at the top of a page that came back without
// one, which is every page the fast lane reads.
//
// A page that already answered is left alone. That matters: the general model
// does answer, and its answer is the page's own, so overwriting it with a
// second reading of one corner would be replacing evidence with less evidence.
func MarkFolio(text string, number int, printed bool) string {
	if HasFolioLine(text) {
		return text
	}
	line := NoFolio
	if printed {
		line = fmt.Sprintf("⟦folio %d⟧", number)
	}
	return line + "\n\n" + strings.TrimLeft(text, "\n")
}
