package tags

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// Object is one thing in the corpus that can be tagged, and where its tag is
// written down.
//
// Two places, always. The tags file is the register and the front matter is the
// copy, and the copy is what makes an article file readable on its own: a
// citation is checked against the register, but a person looking at the file
// should not have to go and find it.
type Object struct {
	// Label is the object's name in the tags file. For an article it is the
	// corpus identifier and for a problem it is the printed number.
	Label string
	// Path is the file whose front matter carries the tag.
	Path string
	// Kind is for the report, and for the ordering: articles before problems,
	// so that a first run over a corpus lays the file out in a way a person can
	// read.
	Kind string
	// Year and Issue order the walk. Assignment must not depend on the order
	// the filesystem hands directories back.
	Year  int
	Issue string
}

// Objects lists everything tagged in a corpus, in a fixed order.
//
// The order is fixed because assignment is not quite order independent. A
// derived tag is the same whatever order it is asked in, but the loser of a
// collision is whoever asked second, so two runs that walk the corpus
// differently would give a handful of objects different tags. Sorting here
// costs nothing and takes the question off the table.
func Objects(c *corpus.Corpus, lang string) ([]Object, error) {
	var out []Object
	articles, err := filepath.Glob(filepath.Join(c.Root, "content", lang, "*", "*", "articles", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range articles {
		var front corpus.ArticleFront
		if _, err := corpus.LoadUnchecked(path, &front); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if front.ID == "" {
			return nil, fmt.Errorf("%s: the article carries no id, so it cannot be tagged", path)
		}
		out = append(out, Object{
			Label: Label(front.ID),
			Path:  path, Kind: "article",
			Year: front.Year, Issue: front.Issue,
		})
	}
	problems, err := filepath.Glob(filepath.Join(c.Root, "content", lang, "problems", "*", "*.md"))
	if err != nil {
		return nil, err
	}
	for _, path := range problems {
		var front corpus.ProblemFront
		if _, err := corpus.LoadUnchecked(path, &front); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if front.ID == "" {
			return nil, fmt.Errorf("%s: the problem carries no id, so it cannot be tagged", path)
		}
		out = append(out, Object{
			Label: Label(front.ID),
			Path:  path, Kind: "problem",
			Issue: front.PosedIn,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

// Label is the name an object goes under in the register.
//
// It is the corpus identifier as it stands, publisher suffix and all. Dropping
// that suffix was the first thing tried, on the argument that it belongs to
// kvant.digital rather than to us and that a tag should not move when they
// regenerate a slug. The corpus says otherwise. Issue 10 of 1975 prints Задачи
// наших читателей twice, once on page 8 and once on page 23, and with the
// suffix off both files claim the label 1975-10-zadachi-nashih-chitateley. One
// tag naming two objects is the one failure a permanent name exists to rule
// out, and it beats a hypothetical rename, which is what the aliases file is
// there to absorb.
func Label(id string) string { return strings.TrimSpace(id) }

// Result is what one assign pass did.
type Result struct {
	Objects int
	Given   int
	Written int
}

func (r Result) String() string {
	return fmt.Sprintf("%d objects, %d newly tagged, %d files rewritten", r.Objects, r.Given, r.Written)
}

// Assign tags everything in the corpus that does not have a tag, and writes the
// tag into the front matter of everything that does.
//
// Both halves run every time and the second is not conditional on the first.
// An object can be in the register and have lost its front matter tag, because
// the front matter is regenerated whenever an issue is reassembled and the
// register is not. That is the case this whole design exists for: assemble
// throws the articles away and builds them again, and the tags have to come
// back afterwards.
func Assign(c *corpus.Corpus, lang string, store *Store) (Result, error) {
	objects, err := Objects(c, lang)
	if err != nil {
		return Result{}, err
	}
	if err := duplicates(objects); err != nil {
		return Result{}, err
	}
	result := Result{Objects: len(objects)}
	for _, object := range objects {
		_, had := store.Tag(object.Label)
		if !had {
			// The file may know something the register does not, and if it does
			// then the object already has a permanent name and this is not the
			// place to give it another one.
			carried, err := stamped(object)
			if err != nil {
				return result, err
			}
			store.Adopt(object.Label, carried)
		}
		tag, err := store.Assign(object.Label)
		if err != nil {
			return result, err
		}
		if !had {
			result.Given++
		}
		wrote, err := stamp(object, tag)
		if err != nil {
			return result, err
		}
		if wrote {
			result.Written++
		}
	}
	if err := store.Save(); err != nil {
		return result, err
	}
	return result, nil
}

// duplicates refuses to tag a corpus where two files claim the same label.
//
// Nothing downstream could recover from it. Both files would be stamped with
// one tag, the register would say the tag names one object, and a citation to
// it would be ambiguous forever, which is the one thing a permanent name is
// supposed to rule out. It is a bug in whatever wrote the files and it should
// be fixed there rather than papered over here.
func duplicates(objects []Object) error {
	seen := make(map[string]string, len(objects))
	for _, object := range objects {
		if had, ok := seen[object.Label]; ok {
			return fmt.Errorf("%q is the label of both %s and %s, so neither can be tagged", object.Label, had, object.Path)
		}
		seen[object.Label] = object.Path
	}
	return nil
}

// stamp writes a tag into a file's front matter, and says whether it had to.
func stamp(object Object, tag corpus.Tag) (bool, error) {
	switch object.Kind {
	case "article":
		var front corpus.ArticleFront
		body, err := corpus.LoadUnchecked(object.Path, &front)
		if err != nil {
			return false, err
		}
		if front.Tag == tag {
			return false, nil
		}
		front.Tag = tag
		return true, corpus.Save(object.Path, &front, body)
	case "problem":
		var front corpus.ProblemFront
		body, err := corpus.LoadUnchecked(object.Path, &front)
		if err != nil {
			return false, err
		}
		if front.Tag == tag {
			return false, nil
		}
		front.Tag = tag
		return true, corpus.Save(object.Path, &front, body)
	}
	return false, fmt.Errorf("%s: nothing knows how to tag a %q", object.Path, object.Kind)
}

// Problem is one thing wrong with the tags.
type Problem struct {
	Tag    corpus.Tag
	Label  string
	Detail string
}

func (p Problem) String() string {
	switch {
	case p.Tag != "" && p.Label != "":
		return fmt.Sprintf("%s (%s): %s", p.Tag, p.Label, p.Detail)
	case p.Tag != "":
		return fmt.Sprintf("%s: %s", p.Tag, p.Detail)
	case p.Label != "":
		return fmt.Sprintf("%s: %s", p.Label, p.Detail)
	}
	return p.Detail
}

// Verify checks the four things the corpus spec asks of the tags.
//
// Every tag resolves to exactly one object, every tagged object appears once,
// no tag is reused, and every alias points at a live tag. The first three are
// about the register against the corpus and the fourth is about the register
// against itself.
//
// A tagged object that is no longer in the corpus is a failure and not a
// warning, and it is the one that would be tempting to soften. An article that
// was split in two by a fix to the assembler leaves its old label behind, the
// register still points at it, and a citation to that tag now resolves to
// nothing. The right answer is a rename, recorded, so the citation follows the
// object. Letting it pass silently is how the register stops being true.
func Verify(c *corpus.Corpus, lang string, store *Store) ([]Problem, error) {
	objects, err := Objects(c, lang)
	if err != nil {
		return nil, err
	}
	var problems []Problem

	// The register against itself. Open already rejects a file with a repeated
	// tag or a repeated label, so a store that got this far cannot have one,
	// and this is here for a store that was built in memory.
	seenTag, seenLabel := map[corpus.Tag]string{}, map[string]corpus.Tag{}
	for _, entry := range store.Entries() {
		if had, ok := seenTag[entry.Tag]; ok {
			problems = append(problems, Problem{Tag: entry.Tag,
				Detail: fmt.Sprintf("names both %q and %q, and a tag is never reused", had, entry.Label)})
		}
		if had, ok := seenLabel[entry.Label]; ok {
			problems = append(problems, Problem{Label: entry.Label,
				Detail: fmt.Sprintf("has tags %s and %s, and an object appears once", had, entry.Tag)})
		}
		seenTag[entry.Tag], seenLabel[entry.Label] = entry.Label, entry.Tag
	}

	// The register against the corpus, both ways.
	inCorpus := make(map[string]Object, len(objects))
	for _, object := range objects {
		if had, ok := inCorpus[object.Label]; ok {
			problems = append(problems, Problem{Label: object.Label,
				Detail: fmt.Sprintf("is the label of both %s and %s, so one tag would name two objects", rel(c, had.Path), rel(c, object.Path))})
		}
		inCorpus[object.Label] = object
	}
	for _, entry := range store.Entries() {
		if _, ok := inCorpus[entry.Label]; !ok {
			problems = append(problems, Problem{Tag: entry.Tag, Label: entry.Label,
				Detail: "names nothing in the corpus, so a citation to it resolves to nothing, and if the object was renamed the rename has to be recorded"})
		}
	}
	for _, object := range objects {
		tag, ok := store.Tag(object.Label)
		if !ok {
			problems = append(problems, Problem{Label: object.Label,
				Detail: fmt.Sprintf("is in the corpus with no tag, run tags assign (%s)", rel(c, object.Path))})
			continue
		}
		// And the copy in the front matter, which is the part that a
		// reassemble destroys.
		stamped, err := stamped(object)
		if err != nil {
			return nil, err
		}
		if stamped != tag {
			problems = append(problems, Problem{Tag: tag, Label: object.Label,
				Detail: fmt.Sprintf("carries tag %q in its front matter, run tags assign (%s)", stamped, rel(c, object.Path))})
		}
	}

	// The aliases.
	for _, alias := range store.Aliases() {
		if _, ok := store.Label(alias.Tag); !ok {
			problems = append(problems, Problem{Tag: alias.Tag, Label: alias.Label,
				Detail: "is an alias for a tag that is not in the register"})
		}
		if _, ok := store.Tag(alias.Label); ok {
			problems = append(problems, Problem{Label: alias.Label,
				Detail: "is both a live label and an alias, so a lookup has two answers"})
		}
	}
	return problems, nil
}

func stamped(object Object) (corpus.Tag, error) {
	switch object.Kind {
	case "article":
		var front corpus.ArticleFront
		_, err := corpus.LoadUnchecked(object.Path, &front)
		return front.Tag, err
	case "problem":
		var front corpus.ProblemFront
		_, err := corpus.LoadUnchecked(object.Path, &front)
		return front.Tag, err
	}
	return "", fmt.Errorf("%s: nothing knows how to read the tag of a %q", object.Path, object.Kind)
}

func rel(c *corpus.Corpus, path string) string {
	if out, err := filepath.Rel(c.Root, path); err == nil {
		return out
	}
	return path
}
