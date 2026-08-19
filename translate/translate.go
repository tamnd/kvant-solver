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
	// Title is the article's title, which lives in the front matter rather than
	// the body and so is not carried by Body. It is empty for the schemas that
	// have no title, and an empty one costs no call.
	Title string
}

// Result is one translated file.
type Result struct {
	Body string
	// Title is the translated title, empty when the job carried none.
	Title  string
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

	res := Result{Terms: terms, Prompt: prompts.Hash(job.Lang), Model: opts.Model}

	// The title goes first and on its own. It is the field a reader sees before
	// anything else, and leaving it in Russian on a page whose every sentence is
	// English is the one place where an unfinished translation looks finished.
	if job.Title != "" {
		e.report("title")
		instructions, input := prompts.Title(job.Title, job.Lang, terms)
		text, err := e.ask(ctx, "title", instructions, input, job.Title, opts, &res)
		if err != nil {
			return res, err
		}
		res.Title = text
	}

	chunks := Chunks(job.Body)
	if len(chunks) == 0 {
		return res, nil
	}
	res.Chunks = len(chunks)
	parts := make([]string, 0, len(chunks))

	for _, c := range chunks {
		e.report(fmt.Sprintf("chunk %d of %d, %d spans", c.Index, c.Of, c.Spans))
		instructions, input := prompts.Chunk(c, job.Lang, terms)
		text, err := e.ask(ctx, fmt.Sprintf("chunk %d of %d", c.Index, c.Of),
			instructions, input, c.Body, opts, &res)
		if err != nil {
			return res, err
		}
		parts = append(parts, text)
	}

	res.Body = Join(parts)
	res.Elapsed = time.Since(start)
	return res, nil
}

// ask sends one piece and asks for it again if the mathematics came back
// changed, giving up after opts.Retries and recording what it gave up on.
//
// What survives a retry is still returned rather than dropped. A chunk with one
// suspect formula and a warning against it is worth more than a hole in the
// page, and the warning is what the audit reads.
func (e *Engine) ask(ctx context.Context, what, instructions, input, src string,
	opts Options, res *Result) (string, error) {
	var text string
	var complaints []string
	for attempt := 0; ; attempt++ {
		out, err := e.Client.Complete(ctx, api.Request{
			Model:        opts.Model,
			Instructions: instructions,
			Input:        input,
		})
		if err != nil {
			return "", fmt.Errorf("%s: %w", what, err)
		}
		res.Usage = add(res.Usage, out.Usage)
		if out.Model != "" {
			res.Model = out.Model
		}
		text = clean(out.Text)
		complaints = Verify(src, text)
		if len(complaints) == 0 || attempt >= opts.Retries {
			break
		}
		e.report(fmt.Sprintf("%s: %s, asking again", what, complaints[0]))
	}
	for _, complaint := range complaints {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", what, complaint))
	}
	return text, nil
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
		if before[i].Display == after[i].Display &&
			maskProse(before[i].Text) == maskProse(after[i].Text) {
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

// proseCommand is the LaTeX that sets words inside a formula rather than
// mathematics. \operatorname is deliberately not here: it names a function, and
// tg and sin are notation rather than words to be carried into another
// language.
var proseCommand = map[string]bool{
	`\text`: true, `\textrm`: true, `\textit`: true,
	`\textbf`: true, `\textsf`: true, `\mbox`: true,
}

// maskProse blanks the argument of \text and its relatives, leaving everything
// else in the span alone.
//
// Rule 2 holds the mathematics of a span to exactly what it was, and the
// argument of \text is not mathematics. It is the words that happen to be set
// inside a formula, and a unit written out as a Russian word has to come back
// translated the same way the sentence around it does. Comparing spans byte for
// byte cannot tell that apart from retyped algebra, so it called every such
// span a rewrite, spent the chunk's one retry asking again for a chunk that was
// already right, and then filed a warning about the correct answer. Masking the
// prose still compares the algebra either side of it exactly, which is the part
// the rule was written for.
func maskProse(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); {
		if rs[i] != '\\' {
			b.WriteRune(rs[i])
			i++
			continue
		}
		j := i + 1
		for j < len(rs) && isLetter(rs[j]) {
			j++
		}
		// A backslash that names nothing is an escape, and the character it
		// escapes goes through with it so that a literal \{ is not read as the
		// start of an argument.
		if j == i+1 {
			b.WriteRune(rs[i])
			if j < len(rs) {
				b.WriteRune(rs[j])
				j++
			}
			i = j
			continue
		}
		name := string(rs[i:j])
		if !proseCommand[name] || j >= len(rs) || rs[j] != '{' {
			b.WriteString(name)
			i = j
			continue
		}
		b.WriteString(name)
		b.WriteString("{}")
		i = skipGroup(rs, j)
	}
	return b.String()
}

// skipGroup returns the index just past the brace group starting at open. An
// unbalanced group runs to the end, which leaves the span comparing unequal to
// anything else and so is reported rather than passed.
func skipGroup(rs []rune, open int) int {
	depth := 0
	for i := open; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return len(rs)
}

func isLetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
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
