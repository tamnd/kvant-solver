package translate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/glossary"
	"github.com/tamnd/kvant-solver/mathtex"
)

// Options is what a run is allowed to do.
type Options struct {
	Model string
	// Retries is how many times a chunk that came back with the mathematics
	// mangled is asked again. A retry is cheap and a page with a broken formula
	// is worth nothing, so this is not zero by default.
	Retries int
}

// Job is one file to translate.
type Job struct {
	// Key is the corpus path this came from, recorded in the stamp so that a
	// translated file says what it is a translation of.
	Key  string
	Lang string
	Body string
}

// Result is one translated file.
type Result struct {
	Body   string
	Chunks int
	// Terms is the glossary rows this file was shown, which is what the stamp
	// hashes.
	Terms []glossary.Term
	// Prompt is the fingerprint of the wording this was written under, which is
	// one of the things a later run checks before deciding the file is current.
	Prompt   string
	Warnings []string
	Usage    api.Usage
	Model    string
	Elapsed  time.Duration
}

// Engine translates.
type Engine struct {
	Client   api.Completer
	Prompts  Prompts
	Progress func(string)
}

// Translate turns one body into one language.
//
// The chunks go one at a time rather than concurrently. The magazine is not in
// a hurry, the fleet is shared with the reading, and a run that saturates the
// endpoint on one article is a run that starves twenty others.
func (e *Engine) Translate(ctx context.Context, job Job, g *glossary.Glossary, opts Options) (Result, error) {
	if !glossary.Known(job.Lang) {
		return Result{}, fmt.Errorf("%s is not a language this corpus translates into", job.Lang)
	}
	prompts := e.Prompts
	if prompts == nil {
		prompts = DefaultPrompts{}
	}
	start := time.Now()

	var terms []glossary.Term
	if g != nil {
		terms = g.Mentioned(job.Body, job.Lang)
	}

	hash := prompts.Hash(job.Lang)
	chunks := Chunks(job.Body)
	if len(chunks) == 0 {
		return Result{Terms: terms, Prompt: hash, Model: opts.Model}, nil
	}

	res := Result{Chunks: len(chunks), Terms: terms, Prompt: hash, Model: opts.Model}
	parts := make([]string, 0, len(chunks))

	for _, c := range chunks {
		e.report(fmt.Sprintf("chunk %d of %d, %d spans", c.Index, c.Of, c.Spans))
		instructions, input := prompts.Chunk(c, job.Lang, terms)

		var text string
		var complaints []string
		for attempt := 0; ; attempt++ {
			out, err := e.Client.Complete(ctx, api.Request{
				Model:        opts.Model,
				Instructions: instructions,
				Input:        input,
			})
			if err != nil {
				return res, fmt.Errorf("chunk %d of %d: %w", c.Index, c.Of, err)
			}
			res.Usage = add(res.Usage, out.Usage)
			if out.Model != "" {
				res.Model = out.Model
			}
			text = clean(out.Text)
			complaints = Verify(c.Body, text)
			if len(complaints) == 0 || attempt >= opts.Retries {
				break
			}
			e.report(fmt.Sprintf("chunk %d of %d: %s, asking again", c.Index, c.Of, complaints[0]))
		}
		for _, complaint := range complaints {
			res.Warnings = append(res.Warnings, fmt.Sprintf("chunk %d of %d: %s", c.Index, c.Of, complaint))
		}
		parts = append(parts, text)
	}

	res.Body = Join(parts)
	res.Elapsed = time.Since(start)
	return res, nil
}

// Verify checks what can be checked without a model.
//
// It is only about the mathematics, and that is on purpose. Whether the prose
// reads well is a judgement and needs a reader; whether the formulas came back
// the way they went in is a fact, it is the failure that actually happens, and
// it is the one nobody notices in a language they do not read. A page whose
// algebra was quietly retyped is worse than a page that was never translated,
// because it looks finished.
func Verify(src, out string) []string {
	before, _ := mathtex.Split(src)
	after, unclosed := mathtex.Split(out)

	var complaints []string
	if unclosed != nil {
		complaints = append(complaints, fmt.Sprintf(
			"the translation has a math span left open at line %d", unclosed.Line))
	}
	if len(before) != len(after) {
		complaints = append(complaints, fmt.Sprintf(
			"the source has %d math spans and the translation has %d", len(before), len(after)))
		return complaints
	}
	for i := range before {
		if before[i].Text == after[i].Text && before[i].Display == after[i].Display {
			continue
		}
		complaints = append(complaints, fmt.Sprintf(
			"math span %d was rewritten: %q became %q", i+1, before[i].Text, after[i].Text))
		// One report is enough to send the chunk back. Listing all forty spans of
		// a page of algebra buries the first one, which is usually the only one
		// anybody needs to look at.
		break
	}
	return complaints
}

// clean strips the wrapper a model puts round a reply however plainly it was
// told not to.
func clean(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	// A whole reply fenced as a code block is a wrapper. A fence that opens
	// partway down is part of the document and is left alone.
	rest := text[3:]
	if i := strings.Index(rest, "\n"); i >= 0 {
		rest = rest[i+1:]
	}
	if j := strings.LastIndex(rest, "```"); j >= 0 && strings.TrimSpace(rest[j+3:]) == "" {
		rest = rest[:j]
	} else {
		return text
	}
	return strings.TrimSpace(rest)
}

func (e *Engine) report(msg string) {
	if e.Progress != nil {
		e.Progress(msg)
	}
}

func add(a, b api.Usage) api.Usage {
	a.InputTokens += b.InputTokens
	a.CachedInputTokens += b.CachedInputTokens
	a.OutputTokens += b.OutputTokens
	a.ReasoningTokens += b.ReasoningTokens
	a.TotalTokens += b.TotalTokens
	return a
}
