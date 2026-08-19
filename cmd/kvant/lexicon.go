package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/lexicon"
)

// sha256File is what pins the header to a particular wordlist. The name of a
// download says nothing about which download it was.
func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

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
	wordlist := fs.String("wordlist", "", "a file of Russian word forms to add to the ones the corpus uses")
	source := fs.String("wordlist-source", "", "where that file came from, written into the header")
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
	mine := lexicon.Forms(count, *min)
	forms := mine

	fmt.Printf("%d distinct forms in the corpus, %d of them seen at least %d times\n",
		len(count), len(mine), *min)
	if len(count) == 0 {
		return fmt.Errorf("no pages under %s, so there is nothing to build a lexicon from", c.Root)
	}

	header := []string{
		"Russian word forms rule 8 recognises. Built by kvant lexicon build, do not edit.",
		fmt.Sprintf("%d forms from the %s pages of this corpus, seen at least %d times.", len(mine), *lang, *min),
	}
	if *wordlist != "" {
		theirs, err := lexicon.Wordlist(*wordlist)
		if err != nil {
			return err
		}
		sum, err := sha256File(*wordlist)
		if err != nil {
			return err
		}
		forms = lexicon.Merge(mine, theirs)
		fmt.Printf("%d usable forms from %s, %d forms in all\n", len(theirs), *wordlist, len(forms))
		where := *source
		if where == "" {
			where = filepath.Base(*wordlist)
		}
		header = append(header,
			fmt.Sprintf("%d forms from %s, sha256 %s,", len(theirs), where, sum),
			"kept where they are Cyrillic throughout and at least four letters, which is what a reading can be.",
		)
	}
	if *dry {
		fmt.Println("dry run, nothing written")
		return nil
	}
	path := lexicon.Path(c.Root)
	if err := lexicon.Write(path, forms, header...); err != nil {
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
