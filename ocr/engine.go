package ocr

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/api"
)

// Engine reads one page image and returns the Markdown for it.
//
// There is an interface here because the measurements say there has to be. Four
// lanes were timed on the same nine sheets of 1975 №1 and none of them wins
// outright:
//
//	browser session, one page at a time      148 s a page,  the reference
//	CLI model, six at once                   7.5 s a page,  97% of the words
//	GLM-OCR on a 4090, six at once           1.36 s a page, 81% of the words
//	Apple Vision on the laptop CPU           0.4 to 1.2 s,  93%, no mathematics
//
// Neither of the fast ones is the answer on its own. Vision runs the two columns
// of a page together and returns no mathematics at all, which on this magazine
// is most of the point. GLM-OCR keeps the columns and does write LaTeX, and it
// is systematically short: over the nine sheets it produced 3038 words against
// the reference 3240, and on one page it dropped a 55 word run out of the middle
// of a column, which is the failure nothing downstream can see. So speed is
// chosen per run and not per repo: a page that will be read by a person tomorrow
// can afford the slow lane, and 34000 of them cannot. docs/ocr-engines.md has
// the whole table and what it was measured on.
//
// Read returns the raw answer. Stripping, normalising and validating happen in
// the runner, once, so that every lane is held to the same standard.
type Engine interface {
	Read(ctx context.Context, image string) (string, error)
	// Name goes in the page front matter as extraction_model, so a file can say
	// what read it.
	Name() string
}

// Metered is an engine that also says what the page cost in tokens.
//
// It is a second interface rather than a wider Engine because half the lanes
// cannot answer the question. An endpoint returns a usage block; a program on
// the box prints a page of Markdown and exits, and there is nothing to read the
// number off. Folding it into Engine would mean every local lane returning a
// zero that the report cannot tell from a page that really used no tokens, so
// the runner asks with a type assertion and a lane that stays quiet is recorded
// as having said nothing.
type Metered interface {
	Engine
	ReadMetered(ctx context.Context, image string) (string, api.Usage, error)
}

// Served is an engine backed by anything that answers POST
// /v1/chat/completions with an image in the message.
//
// Two very different things are behind that one wire. One is chatgpt-tool serve
// on a rented box, which fronts a browser session; the other is vllm serve on
// the machine with the 4090, holding a document model such as GLM-OCR. The
// second is the reason this exists: a 0.9B document model reads a page in 1.36
// seconds on that card, keeps the column order, and does emit LaTeX, which is
// the combination neither of the other two lanes had.
//
// Prompt is short for a document model and long for a general one, and that is
// not a preference. Sending the whole page prompt to GLM-OCR made it read six
// points worse and nine times slower, because it is a recognition model and
// every sentence of instruction is context it did not want.
type Served struct {
	Client api.Completer
	Model  string
	// Prompt is the instruction, which for a document model is short and for a
	// general model is the whole of prompt.OCRPage.
	Prompt string
	// Timeout bounds one page. Zero means DefaultPageTimeout, which is sized for
	// the browser lane; a served model wants far less and should say so, because
	// a card that has wedged should be found in a minute rather than a quarter
	// of an hour.
	Timeout time.Duration

	// Sample leaves the sampling to the server instead of pinning it at zero.
	//
	// The default is the pin, and it was paid for. The first full run of 1975
	// №1 sent no temperature at all, so vLLM sampled at 1.0, and what came back
	// was Russian with Latin and occasionally Chinese fragments welded into the
	// middle of words: dvижении, Funcцию, teхнике. Over the issue that was 836
	// of them on 83 of 84 pages, and they are worse than an obvious failure
	// because they read as text and a rule looking for refusals or short pages
	// sees nothing wrong. The same nine sheets at temperature 0 had 1.3 a page
	// against 14.3. Set this only for a lane that has no such control.
	Sample bool
}

// zero is addressable so that the request can carry a pointer to it.
var zero = 0.0

func (s Served) temperature() *float64 {
	if s.Sample {
		return nil
	}
	return &zero
}

// Name is the model this engine is holding.
func (s Served) Name() string { return s.Model }

func (s Served) Read(ctx context.Context, image string) (string, error) {
	text, _, err := s.ReadMetered(ctx, image)
	return text, err
}

// ReadMetered is Read with the token accounting the server sent back. A server
// that sends none leaves it zero, which the ledger records as a page nobody
// counted rather than a page that was free.
func (s Served) ReadMetered(ctx context.Context, image string) (string, api.Usage, error) {
	if s.Client == nil {
		return "", api.Usage{}, fmt.Errorf("no client")
	}
	url, err := DataURL(image)
	if err != nil {
		return "", api.Usage{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = DefaultPageTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	response, err := s.Client.Complete(ctx, api.Request{
		Model:        s.Model,
		Instructions: s.Prompt,
		Image:        url,
		Temperature:  s.temperature(),
	})
	if err != nil {
		return "", api.Usage{}, fmt.Errorf("read %s: %w", filepath.Base(image), err)
	}
	return response.Text, response.Usage.Normalized(), nil
}

// DataURL reads a page image and encodes it for the wire.
//
// Base64 costs a third more bytes than the file, which over a whole year is
// real, but every alternative means the model's host being able to reach a URL
// we serve, and these are boxes behind a tunnel with no inbound route. A page
// image at 200 dpi is under a megabyte, so a third more of it is not what makes
// a run slow.
func DataURL(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	kind := mediaType(path)
	if kind == "" {
		return "", fmt.Errorf("%s: not a page image", filepath.Base(path))
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

// mediaType is by extension and not by sniffing, because the only images that
// reach here are the ones pdfsrc rendered and it names what it wrote.
func mediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	}
	return ""
}
