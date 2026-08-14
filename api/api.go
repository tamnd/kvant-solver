// Package api is the model transport: one request shape, one response shape,
// and a client that speaks the OpenAI chat completions wire.
//
// There is only one wire here because there is only one thing on the other end.
// Every host in the fleet runs chatgpt-tool serve, which fronts a pool of
// ChatGPT web sessions and answers POST /v1/chat/completions. Nothing in this
// corpus calls a metered API, so there is no rate card, no second wire, and no
// credential file.
package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Completer is one model call. It is an interface so a test can answer without
// a server and so the router can wrap it.
type Completer interface {
	Complete(ctx context.Context, request Request) (Response, error)
}

// Request is one call to a model.
type Request struct {
	Model string
	// Instructions is the system message. It is the part that repeats across a
	// run, so it also becomes the prompt cache key.
	Instructions string
	Input        string
	// Image is a page image sent with the message, as a data URL. It is what
	// makes this wire usable for OCR as well as for prose, and it is a string
	// rather than a path because the encoding belongs to whoever read the file
	// and the transport should not be opening things.
	//
	// A request with an image may have an empty Input: the instruction is the
	// whole question and the picture is the whole subject.
	Image string

	// Temperature pins the sampling. Nil leaves it to the server, which is what
	// the browser lane needs because a browser session has no such control.
	//
	// It is a pointer because zero is the value that matters and zero is also
	// the empty value, so the two have to be tellable apart. Leaving it out
	// against vLLM means sampling at 1.0, and for reading a page that is not a
	// small difference: the same nine sheets of 1975 №1 came back with 1.3
	// Latin fragments welded into Russian words per page at temperature 0 and
	// 14.3 per page with it unset. A page has one correct transcription and
	// there is nothing to explore.
	Temperature *float64
}

// Response is what came back, plus enough about where it came from to write an
// honest report.
type Response struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	// Route names the host that served the call. A chapter assembled from two
	// hosts has to be able to say which one answered each page.
	Route   string        `json:"route,omitempty"`
	Text    string        `json:"text"`
	Usage   Usage         `json:"usage"`
	Elapsed time.Duration `json:"elapsed"`
}

// Usage is the token accounting the provider returned. InputTokens counts
// cached reads and cache writes as well. ReasoningTokens is a part of
// OutputTokens and must not be added to it.
//
// A ChatGPT web session bills nobody, so these numbers buy no cost estimate.
// They are here because they are the only measure of how much work a page took,
// and a page that comes back with a tenth of the usual output tokens is a page
// that got truncated.
type Usage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	ReasoningTokens   int `json:"reasoning_tokens"`
	TotalTokens       int `json:"total_tokens"`
}

// Normalized fills in totals a compatible proxy left out and stops malformed
// cache detail from producing a negative count of uncached input.
func (u Usage) Normalized() Usage {
	u.InputTokens = max(0, u.InputTokens)
	u.OutputTokens = max(0, u.OutputTokens)
	u.CachedInputTokens = min(max(0, u.CachedInputTokens), u.InputTokens)
	u.ReasoningTokens = min(max(0, u.ReasoningTokens), u.OutputTokens)
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	return u
}

// Add sums two accountings, for a report that covers a whole run.
func (u Usage) Add(other Usage) Usage {
	u.InputTokens += other.InputTokens
	u.CachedInputTokens += other.CachedInputTokens
	u.OutputTokens += other.OutputTokens
	u.ReasoningTokens += other.ReasoningTokens
	u.TotalTokens += other.TotalTokens
	return u
}

// parseRetryAfter reads the header, which a provider may write as a number of
// seconds or as an HTTP date.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(when))
	}
	return 0
}

func retryAfterSuffix(delay time.Duration) string {
	if delay <= 0 {
		return ""
	}
	return fmt.Sprintf(" (retry after %s)", delay)
}

// backoff doubles from a second to thirty, with jitter so a fleet of goroutines
// that all failed on the same dead tunnel do not all come back at once.
func backoff(attempt int) time.Duration {
	base := min(30*time.Second, time.Second<<min(attempt, 5))
	return base + time.Duration(rand.IntN(500))*time.Millisecond
}

// condense makes a provider message fit one log line and one table cell. A
// gateway that is down answers with an HTML error page, and a detail field
// holding twenty lines of markup turns doctor's table into something nobody can
// read.
func condense(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[:200] + "..."
	}
	return text
}
