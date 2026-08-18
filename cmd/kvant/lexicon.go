package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/lexicon"
)

func runLexicon(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("lexicon needs a subcommand, which is build or check")
	}
	switch args[0] {
	case "build":
		return runLexiconBuild(args[1:])
	case "check":
		return runLexiconCheck(args[1:])
	default:
		return fmt.Errorf("unknown lexicon subcommand %q", args[0])
	}
}

// runLexiconBuild writes the Russian word forms of the corpus to
// manifests/lexicon.txt.
//
// It is a committed file rather than something rule 8 works out as it goes,
// because rule 8 has to give the same answer twice. A lexicon built from
// whatever was on disk at the time would make a page pass on the day 1981 was
// read and fail on the day 1982 landed, and kvant audit would disagree with
// itself for reasons nobody could see in a diff. Building it is therefore a
// deliberate act with a commit attached, and rereading a year after a rebuild
// is the same kind of decision as rereading it after a prompt change.
func runLexiconBuild(args []string) error {
	fs := pflag.NewFlagSet("lexicon build", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to read")
	min := fs.Int("min", lexicon.MinCount, "how often a form must appear before it is a word")
	dry := fs.Bool("dry-run", false, "print the counts and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	count, err := lexicon.Collect(c.Root, *lang)
	if err != nil {
		return err
	}
	forms := lexicon.Forms(count, *min)

	fmt.Printf("%d distinct forms in the corpus, %d of them seen at least %d times\n",
		len(count), len(forms), *min)
	if len(count) == 0 {
		return fmt.Errorf("no pages under %s, so there is nothing to build a lexicon from", c.Root)
	}
	if *dry {
		fmt.Println("dry run, nothing written")
		return nil
	}
	path := lexicon.Path(c.Root)
	if err := lexicon.Write(path, forms); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}

// runLexiconCheck asks the lexicon about words, which is how a person finds out
// why a page is still failing rule 8.
//
// The interesting answer is the negative one. A word the lexicon refuses is a
// word the page will be counted for, and seeing that Оlimпилад is refused while
// однako is placed is the whole explanation of why one page died and another
// did not.
func runLexiconCheck(args []string) error {
	fs := pflag.NewFlagSet("lexicon check", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	words := fs.Args()
	if len(words) == 0 {
		return fmt.Errorf("say which words to check, as arguments")
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	lex, err := lexicon.Open(c.Root)
	if err != nil {
		return err
	}
	if lex == nil {
		return fmt.Errorf("%s has no lexicon, run kvant lexicon build", lexicon.Path(c.Root))
	}

	sort.Strings(words)
	for _, word := range words {
		switch {
		case lex.Has(word):
			fmt.Printf("%-24s a Russian form the corpus already uses\n", word)
		case lex.Resolves(word):
			fmt.Printf("%-24s Russian spelled in two alphabets, so rule 8 lets it pass\n", word)
		default:
			fmt.Printf("%-24s nothing the corpus uses, so rule 8 counts it\n", word)
		}
	}
	return nil
}
