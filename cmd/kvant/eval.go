package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/problems"
)

// The named sets. A set is a preset rather than a pair of numbers on the
// command line so that "the smoke set" means the same thing in a commit
// message, in a scorecard and six months later.
var evalSets = map[string]struct {
	perCell int
	why     string
}{
	"smoke": {
		perCell: 2,
		why: "Two problems per decade, subject and rubric. Small enough to run on every change, " +
			"which is the only thing it is for: it answers whether the pipeline runs, not whether it is any good.",
	},
	"decade-stratified": {
		perCell: 25,
		why: "Twenty five problems per decade, subject and rubric, drawn evenly across the numbering. " +
			"This is the set the scorecard is computed from, so it is weighted by decade rather than by how much of each decade happens to be read. " +
			"The rubric is in the cell because the magazine never graded a problem and the section it printed it in is the nearest thing it said.",
	},
}

func runEval(args []string) error {
	fs := pflag.NewFlagSet("eval", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	name := fs.String("set", "smoke", "which set to draw, smoke or decade-stratified")
	write := fs.Bool("write", false, "write the set into the corpus")
	members := fs.Bool("members", false, "print every problem in the set")
	if err := fs.Parse(args); err != nil {
		return err
	}

	preset, ok := evalSets[*name]
	if !ok {
		names := make([]string, 0, len(evalSets))
		for n := range evalSets {
			names = append(names, n)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown set %q, try one of %v", *name, names)
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	store, err := manifest.Open(c.Root)
	if err != nil {
		return err
	}
	m := &problems.Manifest{}
	if err := store.Read(problems.ManifestFile, m); err != nil {
		return fmt.Errorf("%w, run kvant problems build first", err)
	}

	set := problems.Stratify(m, preset.perCell)
	set.Name = *name
	set.Why = preset.why
	if set.Size == 0 {
		return fmt.Errorf("no problem in %s has both a statement and a printed solution, so there is nothing to grade against",
			store.Path(problems.ManifestFile))
	}

	fmt.Println(set.Describe())
	if *members {
		for _, id := range set.Members {
			fmt.Printf("  %s\n", id)
		}
	}
	if !*write {
		return nil
	}
	if err := store.Write(problems.SetFile(*name), set, preset.why); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", store.Path(problems.SetFile(*name)))
	return nil
}
