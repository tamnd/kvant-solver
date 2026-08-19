package translate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/tamnd/kvant-solver/glossary"
)

// Prompts is every call the engine makes.
type Prompts interface {
	Chunk(c Chunk, lang string, terms []glossary.Term) (instructions, input string)
	// Title translates an article's title, which is front matter rather than
	// body and so never reaches Chunk.
	Title(title, lang string, terms []glossary.Term) (instructions, input string)
	// Hash fingerprints the wording, so that a file can say which version of the
	// instruction produced it.
	Hash(lang string) string
}

// DefaultPrompts is the wording this corpus uses.
type DefaultPrompts struct{}

var _ Prompts = DefaultPrompts{}

// languages are what to call each target in the instruction. A model told to
// translate into vi does noticeably worse than one told to translate into
// Vietnamese, and the codes are what the rest of the pipeline speaks.
var languages = map[string]string{
	"en": "English",
	"vi": "Vietnamese",
	"zh": "Chinese, in simplified characters",
	"ja": "Japanese",
}

// Language is what to call a target language in a prompt.
func Language(lang string) string {
	if name, ok := languages[lang]; ok {
		return name
	}
	return lang
}

// register is what the translation has to sound like.
//
// Квант was written by working mathematicians and physicists for school pupils,
// and the register is specific: direct, unhurried, and completely unafraid of
// the reader. It does not talk down and it does not perform. A translation that
// turns it into either a textbook or a popular science article has lost the
// thing that made the magazine worth keeping, even where every sentence is
// defensible on its own.
const register = `Квант was a Soviet monthly of mathematics and physics for senior school pupils,
written by people who did the subject for a living. Keep that register: plain
declarative sentences, no padding, no enthusiasm added on top, and no talking
down to the reader. Where the Russian is terse, be terse.

Translate everything, including headings, captions and the text inside tables.
Do not summarise, do not explain, and do not add a note of your own anywhere.

Leave the mathematics exactly as it is. Every $...$ and $$...$$ span is copied
character for character, in the same order, with nothing added and nothing
dropped. Translate the words around them, including any Russian word inside a
\text{} or \mbox{}. Do not renumber equations and do not convert the notation to
whatever is usual where the target language is spoken.

Keep the Markdown as it stands: the same headings, the same lists, the same
blank lines between the same blocks, the same figure references. Russian
quotation marks «» become the ones the target language uses.

Reply with the translation and nothing else. No preface, no closing remark, no
note about what you did.`

// Chunk is one piece of a body.
func (DefaultPrompts) Chunk(c Chunk, lang string, terms []glossary.Term) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Translate the following from Russian into %s.\n\n%s", Language(lang), register)
	if len(terms) > 0 {
		b.WriteString("\n\nUse these renderings for these terms. " +
			"They are fixed across the whole archive, so a better word here is still the wrong word:\n\n")
		b.WriteString(Table(terms, lang))
	}
	if c.Of > 1 {
		// A chunk is translated without sight of the others, so it has to be told
		// that, or it writes an opening sentence for a piece that starts in the
		// middle and rounds off a piece that does not end.
		fmt.Fprintf(&b, "\n\nThis is part %d of %d of a longer text. "+
			"Translate only what is here. Do not introduce it and do not conclude it.", c.Index, c.Of)
	}
	return b.String(), c.Body
}

// titleRegister is the part of the instruction that is only about titles.
//
// A title is held to different things than a paragraph. It has no sentence
// around it to carry the sense, so a model given the body wording alone tends
// to unfold it into a description of what the article is about, which is a
// caption and not a title.
const titleRegister = `A title is a line and not a sentence. Keep it short, keep the order the
magazine chose wherever the target language allows it, and do not add a
subtitle, a gloss or an explanation that was not in the Russian.

Leave any $...$ span exactly as it is and translate the words around it.

Reply with the translated title on one line and nothing else. Do not put
quotation marks round it unless the Russian had them, and do not add a note
about what you did.`

// Title translates one article title.
//
// It is its own call rather than a line pushed onto the front of the first
// chunk. A title handed over with a page of prose comes back folded into the
// first paragraph often enough to matter, and pulling it back out again means
// guessing where it ended.
func (DefaultPrompts) Title(title, lang string, terms []glossary.Term) (string, string) {
	var b strings.Builder
	fmt.Fprintf(&b, "Translate this title of an article in Квант from Russian into %s.\n\n%s",
		Language(lang), titleRegister)
	if len(terms) > 0 {
		b.WriteString("\n\nUse these renderings for these terms. " +
			"They are fixed across the whole archive, so a better word here is still the wrong word:\n\n")
		b.WriteString(Table(terms, lang))
	}
	return b.String(), title
}

// Hash fingerprints the instruction a translation was written under.
//
// Only the part that is the same for every chunk of every file goes in: the two
// registers, and which language it was asked for. The glossary rows are left out
// because they have their own hash and their own test, and the part of a chunk
// prompt, because a body that split into three pieces one day and four the next
// has not changed what it is being asked for.
//
// An edit to the register does restale the whole corpus for that language, and
// that is the right answer. The register is the thing the translation is being
// held to, so changing it means every file was held to something else.
func (DefaultPrompts) Hash(lang string) string {
	sum := sha256.Sum256([]byte(register + "\x00" + titleRegister + "\x00" + Language(lang)))
	return hex.EncodeToString(sum[:])
}

// Table renders the glossary rows for a prompt.
func Table(terms []glossary.Term, lang string) string {
	var b strings.Builder
	for _, t := range terms {
		b.WriteString(t.RU)
		b.WriteString(" = ")
		b.WriteString(t.In(lang))
		if t.Note != "" {
			b.WriteString(" (")
			b.WriteString(t.Note)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return b.String()
}
