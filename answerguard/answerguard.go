// Package answerguard catches the ways a model's answer is not what was asked
// for, before it reaches the corpus.
//
// A model handed a page image sometimes talks instead of transcribing. It
// apologises, it announces what it is about to do, it summarises, or it hands
// back the prompt. All of that is fluent English, none of it is on the page,
// and once it is written to a page file it looks exactly like text that was
// read off the page. Catching it costs a string search; not catching it costs a
// hand audit of 34000 pages.
//
// The name is answerguard and not textguard because textguard in this repo
// answers a different question, about the text layer of a PDF. This one is
// about what came back from a model.
package answerguard

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Leak is one thing found in an answer that should not be there.
type Leak struct {
	// Kind is refusal, no-image, meta, prompt, markup or empty.
	Kind string
	// Detail is the phrase that was found, as it appeared.
	Detail string
	// Line is where it was found, one based, 0 when the whole answer is at
	// fault.
	Line int
}

// refusals are the model declining. A page that opens with one of these has no
// transcription in it at all.
var refusals = []string{
	"i'm sorry",
	"i am sorry",
	"i apologize",
	"i apologise",
	"i cannot",
	"i can't",
	"i'm unable",
	"i am unable",
	"i'm not able",
	"unable to assist",
	"can't help with",
	"cannot help with",
	"as an ai",
	"as a language model",
	"i'm just an ai",
	"against my guidelines",
	// Narrow on purpose. A refusal says the request violates something; a
	// theorem says a map violates no relation, and the bare word rejected a
	// real page of chapter IV the first time this ran.
	"violates our",
	"violates my",
	"violates the content",
}

// noImage is the model answering politely to a message that arrived without its
// attachment.
//
// This is not a refusal and not narration. It is the upload having failed: the
// prompt got through, the page did not, and the model says so and asks for it.
// It is worth its own kind because the answer is not to ask a model again more
// loudly, it is that a browser upload did not finish, and the failures report
// should say which of the two happened.
//
// It cost three pages of the first live run. The answer is about a hundred and
// thirty characters, which is under the length rule, but the tool writes four
// lines of its own header above it, and with that on top it cleared the
// minimum and "I don't see an image attached" was written to the corpus as
// page 42 of an issue.
// The first person is load bearing. A transcription is in Russian, so an
// English sentence is already suspicious, but the fillers and the book reviews
// quote English now and then and the list must not fire on a quotation.
var noImage = []string{
	"i don't see an image",
	"i don't see the image",
	"i don't see any image",
	"i do not see an image",
	"i do not see the image",
	"i didn't receive an image",
	"i did not receive an image",
	"no image was attached",
	"there is no image attached",
	"please upload the",
	"please upload an image",
	"please attach the image",
	"upload the page image",
	"you want transcribed",
}

// metas are the model narrating. These are worse than refusals because the
// transcription usually follows and the page looks almost right.
var metas = []string{
	"here is the transcription",
	"here's the transcription",
	"here is the text",
	"here's the text",
	"here is the transcribed",
	"the image shows",
	"the image contains",
	"this image appears",
	"this page appears to be",
	"sure, here",
	"sure! here",
	"certainly, here",
	"certainly! here",
	"below is the transcription",
	"i have transcribed",
	"transcription of the image",
	"let me know if",
	"hope this helps",
	"note that i have",
}

// Deliberately not here: in summary, to summarize, and the like. A model that
// summarises a page says so at the top, and it is caught by the phrases above
// or by the length check. Mathematical prose says it too, and a page rejected
// for saying it costs three reads at 151 seconds each and then lands in the
// failures report as a defect that is not one.

// prompts are the model handing back its instructions rather than the page.
// This one is specific to how we ask, so it is kept apart from the general
// narration above.
var prompts = []string{
	"transcribe the complete text",
	"render all mathematical expressions as latex",
	"output only the raw transcribed content",
	"do not summarize, paraphrase",
}

// markup is the provider's own formatting, wrapped around an answer that is
// otherwise fine.
//
// This is a different failure from the ones above and it is the nastier one,
// because there is no English sentence to search for and every rule that
// compares a translation with its English can pass it. It happened: a retranslation
// of the appendix on the Nullstellensatz came back inside
//
//	:::writing{variant="document" id="58321"}
//	...
//	:::
//
// and reached the corpus. The math spans matched, the tags matched, the heading
// tree matched, the block count matched because the fence lines had no blank
// line around them and so joined the paragraphs either side of them, and all
// seven translation rules passed the file. It was found by reading the diff.
//
// A ::: line is a directive fence, which the Markdown this corpus is written in
// does not use anywhere. The private use area is where a provider hides its
// own citation anchors, and 【 】 is where another one puts them. None of the
// three can be part of a printed page, so all three are refused wherever they
// turn up rather than only at the start of an answer.
var markup = []struct {
	what string
	re   *regexp.Regexp
}{
	{"a directive fence, which is the provider's markup and not Markdown this corpus uses", regexp.MustCompile(`(?m)^\s*:::`)},
	{"a citation anchor", regexp.MustCompile(`【[^】]*】|oai_citation|contentReference`)},
	{"a private use character, which is a provider's own marker", regexp.MustCompile(`[\x{e000}-\x{f8ff}]`)},
}

// Check reads an answer and reports everything wrong with it.
//
// Every leak is reported rather than the first, because a page that both
// apologises and narrates is a different failure from one that only narrates,
// and the retry that follows is chosen from what was found.
func Check(text string) []Leak {
	var leaks []Leak
	if strings.TrimSpace(text) == "" {
		return []Leak{{Kind: "empty", Detail: "the answer is empty"}}
	}
	for i, line := range strings.Split(text, "\n") {
		for _, m := range markup {
			if found := m.re.FindString(line); found != "" {
				leaks = append(leaks, Leak{Kind: "markup",
					Detail: m.what + ", " + strconv.Quote(strings.TrimSpace(found)), Line: i + 1})
				break
			}
		}
	}
	// One leak per line, worst kind first. A line that both apologises and
	// narrates is one bad line, and reporting it twice makes the failures
	// report count problems it does not have.
	kinds := []struct {
		kind    string
		phrases []string
	}{
		{"refusal", refusals},
		{"no-image", noImage},
		{"prompt", prompts},
		{"meta", metas},
	}
	for i, line := range strings.Split(text, "\n") {
		if hasCyrillic(line) {
			continue
		}
		lower := straighten(strings.ToLower(line))
		if found, kind, ok := first(lower, kinds); ok {
			leaks = append(leaks, Leak{Kind: kind, Detail: found, Line: i + 1})
		}
	}
	return leaks
}

// hasCyrillic is what makes the phrase lists usable on a Russian page.
//
// Every phrase above is English, and so is every sentence the magazine quotes
// from a foreign book. A review that prints Автор пишет: "I cannot imagine a
// better way to begin" is a page, not a refusal, and a rule that cannot tell
// the two apart either rejects the review or is written so narrowly that it
// misses the refusal.
//
// The line is the unit and Cyrillic is the tell. A model that declines writes a
// line of English and nothing else; a page that quotes English does it inside a
// Russian sentence. This is the one thing about reading a Russian corpus that
// makes these checks easier rather than harder, so it is used.
func hasCyrillic(line string) bool {
	for _, r := range line {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// straighten turns the typographic apostrophe into the ASCII one for matching
// only.
//
// A model writes I don’t and I’m sorry with U+2019, and every phrase above is
// spelled with U+0027, so none of them matched. That is not a hypothetical
// either: it is why "I don’t see an image attached" reached the corpus with no
// leak reported at all. The answer itself is untouched, because the apostrophe
// a page prints is the page's business.
var straightener = strings.NewReplacer("’", "'", "‘", "'", "＇", "'")

func straighten(text string) string { return straightener.Replace(text) }

func first(lower string, kinds []struct {
	kind    string
	phrases []string
}) (string, string, bool) {
	for _, group := range kinds {
		for _, phrase := range group.phrases {
			if strings.Contains(lower, phrase) {
				return phrase, group.kind, true
			}
		}
	}
	return "", "", false
}

// Clean says whether an answer is free of leaks.
func Clean(text string) bool { return len(Check(text)) == 0 }

// fences finds the code fence a model wraps an answer in when it was asked for
// Markdown and decided to be helpful.
var fences = regexp.MustCompile("(?s)\\A```[a-zA-Z]*\\n(.*)\\n```\\s*\\z")

// Strip removes the wrapping a model adds around an otherwise good answer.
//
// This is not the same as Check. A fenced answer is correct text in the wrong
// packaging and unwrapping it is safe; a narrated answer is text that may have
// been altered, and no amount of trimming makes that safe, so it is rejected
// rather than repaired.
func Strip(text string) string {
	trimmed := strings.TrimSpace(text)
	if match := fences.FindStringSubmatch(trimmed); match != nil {
		trimmed = strings.TrimSpace(match[1])
	}
	return trimmed
}

// Normalise fixes the typography a model substitutes for what the page prints.
//
// Only substitutions that are unambiguously wrong in this corpus are made. The
// minus sign, the two dash lengths and the quotation marks all carry meaning in
// mathematics and are left alone, and so does the decimal comma, which is what
// this magazine prints and what an English trained model wants to turn into a
// point.

var normalise = strings.NewReplacer(
	// The Russian function names, which the magazine prints and which a model
	// Anglicises about one page in ten however plainly the prompt says not to.
	// They are a substitution and not a rejection because the meaning survives
	// the mistake and a re-read is two and a half minutes.
	`\tan`, `\mathrm{tg}`,
	`\cot`, `\mathrm{ctg}`,
	`\arctan`, `\mathrm{arctg}`,
	`\operatorname{tg}`, `\mathrm{tg}`,
	`\operatorname{ctg}`, `\mathrm{ctg}`,
	`\operatorname{arctg}`, `\mathrm{arctg}`,
	// Non-breaking and thin spaces read as ordinary spaces everywhere they
	// appear on these pages, and they break every word match if kept.
	" ", " ",
	" ", " ",
	" ", " ",
	// Zero width characters carry nothing and hide differences in a diff. They
	// are written as escapes because a byte order mark in the middle of a Go
	// source file is a compile error, and a zero width space in one is
	// invisible to whoever reads it next.
	"\u200b", "",
	"\ufeff", "",
)

// displayBrackets is the other spelling of a display, \[ ... \], written out as
// the corpus writes one.
//
// The corpus has $$ and nothing else, and the prompt says which to use. It still comes back the other way often
// enough to be worth handling. That is a model being a model, and it is not
// worth a call to say so. The two spellings mean the same display, so the
// answer is turned into the corpus's spelling here rather than sent back.
// The row break of a matrix is \\, and \\[2pt] is a row break asking for space
// after it. Both start with a backslash that is not the delimiter's, so they are
// put aside before the delimiters are turned round and put back afterwards.
var (
	rowBreak     = strings.NewReplacer(`\\`, "\x00")
	rowBreakBack = strings.NewReplacer("\x00", `\\`)
	delimiters   = strings.NewReplacer(
		`\[`, "$$",
		`\]`, "$$",
		`\(`, "$",
		`\)`, "$",
	)
)

func displayBrackets(text string) string {
	return rowBreakBack.Replace(delimiters.Replace(rowBreak.Replace(text)))
}

// Normalise applies those substitutions and trims trailing space from every
// line, which is invisible in review and shows up in every later diff.
func Normalise(text string) string {
	text = displayBrackets(text)
	text = normalise.Replace(text)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return strings.Join(lines, "\n")
}
