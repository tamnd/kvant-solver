package source

import (
	"bytes"
	"io"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// DecodeWindows1251 converts a page served in Windows-1251 to UTF-8. Both of
// the older sites, kvant.mccme.ru and mathnet.ru, still serve cp1251 and say
// so only in a meta tag, so the encoding has to be applied by hand rather than
// left to the HTML parser. Reading those pages as UTF-8 does not fail, it
// quietly yields a page of replacement characters, which is a much worse
// outcome than an error.
//
// The whole page is read into memory. These are contents pages of a few tens
// of kilobytes, and the parsers behind this want a seekable reader anyway.
func DecodeWindows1251(r io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(transform.NewReader(r, charmap.Windows1251.NewDecoder()))
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}
