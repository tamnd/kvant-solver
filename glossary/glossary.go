// Package glossary is the terminology a translation is held to.
//
// It exists because a corpus this size cannot be translated in one call, so it
// is translated in thousands of them, and nothing carries context from one to
// the next. Without a fixed glossary «производная» comes back as derivative in
// one article and as differential coefficient in the next, both defensible, and
// the archive reads as though it had forty translators who never met. With one,
// every call that touches the word is shown the same row.
//
// The glossary is versioned and the version only moves when a term actually
// changes. That is what makes staleness decidable without a model call: a file
// records which version it was translated against and the hash of the rows it
// was actually shown, and both can be rechecked by reading two files.
package glossary

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Term is one row.
//
// Russian is the key because Russian is the source. Kvant was written in it,
// every other column is something this repository produced, and a row with no
// RU is a row about nothing.
type Term struct {
	RU string `yaml:"ru"`
	EN string `yaml:"en,omitempty"`
	VI string `yaml:"vi,omitempty"`
	ZH string `yaml:"zh,omitempty"`
	JA string `yaml:"ja,omitempty"`
	// Note is for the rows where the choice is not obvious and somebody will
	// otherwise change it back in six months.
	Note string `yaml:"note,omitempty"`
	// Quantum is the rendering the English Quantum used between 1990 and 2001,
	// where a mapping row exists. It is a check on our terminology and not a
	// source: it is recorded so that a disagreement is visible and deliberate
	// rather than silent.
	Quantum string `yaml:"quantum,omitempty"`
}

// In returns the rendering for one language, or the empty string if this row
// has not been given one yet.
func (t Term) In(lang string) string {
	switch lang {
	case "en":
		return t.EN
	case "vi":
		return t.VI
	case "zh":
		return t.ZH
	case "ja":
		return t.JA
	default:
		return ""
	}
}

// Set writes one rendering.
func (t *Term) Set(lang, value string) {
	switch lang {
	case "en":
		t.EN = value
	case "vi":
		t.VI = value
	case "zh":
		t.ZH = value
	case "ja":
		t.JA = value
	}
}

// Languages are the ones this corpus translates into, in the order M10 wants
// them. English and Vietnamese first, Chinese and Japanese behind them.
var Languages = []string{"en", "vi", "zh", "ja"}

// Known reports whether a language is one this corpus handles.
func Known(lang string) bool { return slices.Contains(Languages, lang) }

// Glossary is manifests/glossary.yaml.
type Glossary struct {
	Version int    `yaml:"version"`
	Terms   []Term `yaml:"terms"`
}

// Path is where the glossary lives in a corpus checkout.
func Path(root string) string { return filepath.Join(root, "manifests", "glossary.yaml") }

// Load reads the glossary out of a corpus.
func Load(root string) (*Glossary, error) {
	b, err := os.ReadFile(Path(root))
	if err != nil {
		return nil, err
	}
	return Parse(b, Path(root))
}

// Parse reads a glossary and refuses one that is malformed, because a glossary
// half read is worse than none: the terms that survive get applied and the ones
// that did not silently stop being enforced.
func Parse(b []byte, name string) (*Glossary, error) {
	var g Glossary
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &g, nil
}

// Validate reports what is wrong with the glossary itself.
func (g Glossary) Validate() error {
	if g.Version < 1 {
		return fmt.Errorf("version is %d, a saved glossary starts at 1", g.Version)
	}
	seen := map[string]bool{}
	for i, t := range g.Terms {
		if strings.TrimSpace(t.RU) == "" {
			return fmt.Errorf("term %d has no ru", i+1)
		}
		key := Key(t.RU)
		if seen[key] {
			// Two rows for one term means whichever is found first wins, and
			// which that is depends on sort order. That is the kind of bug that
			// shows up as one article in forty using the wrong word.
			return fmt.Errorf("%q appears twice", t.RU)
		}
		seen[key] = true
	}
	return nil
}

// Key normalises a term for lookup. Case and internal spacing are not part of
// the identity of a term.
func Key(term string) string {
	return strings.Join(strings.Fields(strings.ToLower(term)), " ")
}

// Sort puts the glossary in a stable order so that a rebuild that changed
// nothing produces no diff.
func (g *Glossary) Sort() {
	sort.SliceStable(g.Terms, func(i, j int) bool { return Key(g.Terms[i].RU) < Key(g.Terms[j].RU) })
}

// In indexes the glossary by term for one language, skipping the rows that have
// no rendering in it yet.
func (g Glossary) In(lang string) map[string]Term {
	out := map[string]Term{}
	for _, t := range g.Terms {
		if t.In(lang) != "" {
			out[Key(t.RU)] = t
		}
	}
	return out
}

// SameTerms reports whether two term lists say the same thing.
//
// This is what decides a version bump, so it compares the content of the rows
// and not the slice: a reordering is not a change, and bumping the version for
// one would invalidate every translated file in the corpus for nothing.
func SameTerms(a, b []Term) bool {
	if len(a) != len(b) {
		return false
	}
	index := func(terms []Term) map[string]Term {
		m := map[string]Term{}
		for _, t := range terms {
			m[Key(t.RU)] = t
		}
		return m
	}
	left, right := index(a), index(b)
	if len(left) != len(right) {
		return false
	}
	for key, l := range left {
		r, ok := right[key]
		if !ok || l != r {
			return false
		}
	}
	return true
}

// Save writes the glossary and bumps the version only if a term changed.
//
// The bump is the expensive part of the whole translation pipeline, because
// every file translated against the old version is now suspect and has to be
// rechecked. So it happens when the terminology moved and not when the file was
// rewritten, and a run that adds no new term leaves every translation alone.
func (g *Glossary) Save(path string) (version int, bumped bool, err error) {
	g.Sort()
	old, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		g.Version = 1
		bumped = true
	case err != nil:
		return 0, false, err
	default:
		previous, err := Parse(old, path)
		if err != nil {
			return 0, false, err
		}
		g.Version = previous.Version
		if !SameTerms(previous.Terms, g.Terms) {
			g.Version = previous.Version + 1
			bumped = true
		}
	}
	if err := g.Write(path); err != nil {
		return 0, false, err
	}
	return g.Version, bumped, nil
}

// Write puts the glossary on disk without touching the version.
func (g *Glossary) Write(path string) error {
	g.Sort()
	b, err := yaml.Marshal(g)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Written to a temporary file and renamed, so that an interrupted write
	// cannot leave a half glossary that Parse would then refuse and every
	// translation command would then fail on.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// TermsHash fingerprints the rows a file was actually shown.
//
// This is the third staleness test and the one that makes the other two
// bearable. Without it, any version bump invalidates every translated file in
// the corpus, so adding one word to the glossary means retranslating twenty
// thousand pages. With it, a file is only stale when a row it was shown has
// moved, and adding a term about optics leaves the number theory alone.
func TermsHash(terms []Term, lang string) string {
	rows := make([]string, 0, len(terms))
	for _, t := range terms {
		rows = append(rows, Key(t.RU)+"\x00"+t.In(lang))
	}
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:])
}
