// Package prompt holds the prompts the pipeline sends to a model.
//
// They are files rather than string literals so that a prompt can be read and
// edited as prose, and they are embedded rather than loaded from disk so that a
// binary carries the exact text it was built with. Every prompt has a hash, and
// that hash goes in the front matter of every page it produced: when the prompt
// changes, the pages it produced are detectably stale rather than silently
// mixed with pages produced by a different one.
package prompt

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed ocr_page_ru.md
var ocrPageRU string

// OCRPage is the prompt for reading one page of a scanned issue.
//
// It is a page prompt and not an article prompt, which is the whole design of
// this corpus in one file. The model is given a photograph of a printed page
// and asked for everything on it, including the material no table of contents
// mentions, because that material only survives if something asks for it.
//
// Three of the rules here are structural rather than typographic and assembly
// depends on all three: the folio line, the rubric line and the column break.
// Editing any of them puts every page produced under the old text back in the
// queue, so they are pinned by a test.
func OCRPage() string { return strings.TrimSpace(ocrPageRU) + "\n" }

// OCRPageSHA256 is the hash of the page prompt as embedded.
func OCRPageSHA256() string { return SHA256(OCRPage()) }

//go:embed ocr_page_short.md
var ocrPageShort string

// OCRShort is the prompt for a document model, and it is one sentence because
// a document model is not an instruction follower.
//
// This was measured rather than guessed. GLM-OCR read nine sheets at 81% word
// agreement in 1.36 seconds a page with the sentence below, and at 75% in 12.5
// seconds a page with OCRPage. Every rule the long prompt asks for is context
// that a 0.9B recognition model spends attention on and does not obey, so the
// structure it would have marked up is recovered afterwards instead, and the
// pages it produces are the ones the rules are hardest on.
func OCRShort() string { return strings.TrimSpace(ocrPageShort) + "\n" }

//go:embed ocr_band_ru.md
var ocrBandRU string

// OCRBand is what the folio band is asked, and it is four words because the
// band holds one number and there is nothing to explain about it. Asking a
// document model for the page number in words got no page number at all; asking
// it to transcribe a picture that contains only the page number got nine out of
// nine.
func OCRBand() string { return strings.TrimSpace(ocrBandRU) + "\n" }

// OCRShortSHA256 is the hash of the short prompt. It is a different hash from
// the long one on purpose: a page read by a document model and a page read by a
// general model are not the same page, and the front matter should say which.
func OCRShortSHA256() string { return SHA256(OCRShort()) }

// SHA256 is the hash written into front matter. It is over the exact text sent,
// so a prompt that gains a trailing newline in an edit is a different prompt,
// which is the honest answer.
func SHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
