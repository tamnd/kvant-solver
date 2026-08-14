package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/catalog"
	"github.com/tamnd/kvant-solver/manifest"
)

func runSources(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("sources needs a subcommand, which is probe")
	}
	switch args[0] {
	case "probe":
		return runSourcesProbe(args[1:])
	default:
		return fmt.Errorf("unknown sources subcommand %q", args[0])
	}
}

// runSourcesProbe asks one sample issue a year what each source holds. It is
// the report that says in advance how much of the archive can be had as text
// and how much has to go through a model, which is the difference between a
// plan and a hope.
func runSourcesProbe(args []string) error {
	fs := pflag.NewFlagSet("sources probe", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	years := fs.IntSlice("year", nil, "limit to these years, repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := manifest.Open(*root)
	if err != nil {
		return err
	}
	issues := &manifest.Issues{}
	if err := store.Read(manifest.IssuesFile, issues); err != nil {
		if errors.Is(err, manifest.ErrMissing) {
			return fmt.Errorf("no issue list yet, run kvant issues sync first")
		}
		return err
	}
	fmt.Print(catalog.ProbeSources(issues, *years))
	return nil
}

func runPeople(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("people needs a subcommand, which is sync")
	}
	switch args[0] {
	case "sync":
		return runPeopleSync(args[1:])
	default:
		return fmt.Errorf("unknown people subcommand %q", args[0])
	}
}

// runPeopleSync rebuilds personalia.yaml. The two sites are kept apart rather
// than merged: the mirror writes one initial where kvant.digital writes two, so
// joining them on the name would fuse two people who share a surname.
func runPeopleSync(args []string) error {
	fs := pflag.NewFlagSet("people sync", pflag.ContinueOnError)
	f := addSyncFlags(fs)
	mirror := fs.Bool("mirror", true, "also read the MCCME author index")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := manifest.Open(*f.root)
	if err != nil {
		return err
	}
	c := catalogFor(f)
	people, err := c.SyncPersonalia(context.Background(), *mirror)
	if err != nil {
		return err
	}
	if err := store.Write(manifest.PersonaliaFile, people,
		"Everyone the archive credits. A slug with an mccme prefix comes from the mirror and has not been matched to a kvant.digital person."); err != nil {
		return err
	}
	fmt.Printf("personalia: %d people\n", people.Count)
	return nil
}
