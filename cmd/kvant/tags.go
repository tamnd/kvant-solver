package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/tags"
)

func runTags(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tags needs a subcommand, which is assign, verify, list, resolve or rename")
	}
	switch args[0] {
	case "assign":
		return runTagsAssign(args[1:])
	case "verify":
		return runTagsVerify(args[1:])
	case "list":
		return runTagsList(args[1:])
	case "resolve":
		return runTagsResolve(args[1:])
	case "rename":
		return runTagsRename(args[1:])
	default:
		return fmt.Errorf("unknown tags subcommand %q", args[0])
	}
}

// runTagsAssign gives a tag to everything in the corpus that has none, and
// writes every tag into the front matter of the file it names.
//
// It is safe to run on a corpus that is already tagged and it is meant to be
// run that way. Assembling an issue rewrites its articles from the pages, which
// throws the front matter tag away, and this is what puts it back. The register
// is not touched by that, so the tag that comes back is the tag the object had.
func runTagsAssign(args []string) error {
	fs := pflag.NewFlagSet("tags assign", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree being tagged")
	dry := fs.Bool("dry-run", false, "say what would be tagged and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, store, err := openTags(*root)
	if err != nil {
		return err
	}
	if *dry {
		objects, err := tags.Objects(c, *lang)
		if err != nil {
			return err
		}
		untagged := 0
		for _, object := range objects {
			if _, ok := store.Tag(object.Label); !ok {
				untagged++
				fmt.Printf("  %s %s\n", object.Kind, object.Label)
			}
		}
		fmt.Printf("%d objects, %d of them with no tag\n", len(objects), untagged)
		return nil
	}
	result, err := tags.Assign(c, *lang, store)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

// runTagsVerify is the exit criterion of the milestone. It exits non zero when
// the register and the corpus disagree, so CI can run it.
func runTagsVerify(args []string) error {
	fs := pflag.NewFlagSet("tags verify", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree being checked")
	limit := fs.Int("limit", 40, "print this many problems, 0 for all of them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, store, err := openTags(*root)
	if err != nil {
		return err
	}
	problems, err := tags.Verify(c, *lang, store)
	if err != nil {
		return err
	}
	for i, problem := range problems {
		if *limit > 0 && i >= *limit {
			fmt.Printf("and %d more\n", len(problems)-i)
			break
		}
		fmt.Println(" ", problem)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d tags do not check out over %d in the register", len(problems), store.Len())
	}
	fmt.Printf("%d tags, all of them naming one object that carries them\n", store.Len())
	return nil
}

func runTagsList(args []string) error {
	fs := pflag.NewFlagSet("tags list", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	byTag := fs.Bool("by-tag", false, "sort by tag instead of by the order they were assigned in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, store, err := openTags(*root)
	if err != nil {
		return err
	}
	entries := store.Entries()
	if *byTag {
		sort.Slice(entries, func(i, j int) bool { return entries[i].Tag < entries[j].Tag })
	}
	for _, entry := range entries {
		fmt.Printf("%s\t%s\n", entry.Tag, entry.Label)
	}
	return nil
}

// runTagsResolve is what a citation does: it is handed something written down
// at some point in the past, a tag or a label or a label an object used to
// have, and has to say which object that is now.
func runTagsResolve(args []string) error {
	fs := pflag.NewFlagSet("tags resolve", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("give a tag or a label to resolve")
	}
	_, store, err := openTags(*root)
	if err != nil {
		return err
	}
	missing := 0
	for _, what := range rest {
		tag, label, ok := store.Resolve(what)
		if !ok {
			fmt.Printf("%s\tnothing\n", what)
			missing++
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", what, tag, label)
	}
	if missing > 0 {
		return fmt.Errorf("%d of %d resolved to nothing", missing, len(rest))
	}
	return nil
}

// runTagsRename records that an object is now called something else.
//
// This is the only way an entry in the register is ever edited, and it edits
// the label and never the tag. Anything that cites the old label goes on
// working because the old label lands in the aliases file.
func runTagsRename(args []string) error {
	fs := pflag.NewFlagSet("tags rename", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("rename takes the old label and the new one")
	}
	_, store, err := openTags(*root)
	if err != nil {
		return err
	}
	if err := store.Rename(rest[0], rest[1]); err != nil {
		return err
	}
	if err := store.Save(); err != nil {
		return err
	}
	tag, _ := store.Tag(rest[1])
	fmt.Printf("%s is now %s and keeps tag %s\n", rest[0], rest[1], tag)
	return nil
}

func openTags(root string) (*corpus.Corpus, *tags.Store, error) {
	c, err := corpus.Open(root)
	if err != nil {
		return nil, nil, err
	}
	store, err := tags.Open(c.Root)
	if err != nil {
		return nil, nil, err
	}
	return c, store, nil
}
