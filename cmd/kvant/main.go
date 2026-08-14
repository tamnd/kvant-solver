// Command kvant builds the Kvant corpus: it downloads the archive, transcribes
// every printed page, assembles the pages into articles, tags them, translates
// them, and solves the problems.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
)

// Set by the linker at release time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type command struct {
	name  string
	usage string
	short string
	run   func(args []string) error
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kvant:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	commands := []command{
		{"assemble", "assemble --issue K", "build articles out of the pages", runAssemble},
		{"audit", "audit --issue K", "say whether an issue is finished", runAudit},
		{"corpus", "corpus validate [--corpus DIR]", "check a corpus checkout", runCorpus},
		{"coverage", "coverage [--from Y --to Y]", "say what is finished and what is not", runCoverage},
		{"fetch", "fetch pages|pdf [--year Y]", "download the scans and the issue PDFs", runFetch},
		{"issues", "issues sync|list [--deep]", "build and print the issue list", runIssues},
		{"ocr", "ocr --issue K [--workers N]", "read the pages of an issue through a model", runOCR},
		{"people", "people sync", "build the contributor list", runPeople},
		{"repair", "repair [--from Y --to Y]", "read the dead pages through another lane", runRepair},
		{"publisher", "publisher [--from Y --to Y]", "download the text the archive already holds", runPublisher},
		{"report", "report failures|cost|diff", "write the failure list, what the reading cost, and how far the archive's text agrees", runReport},
		{"sources", "sources probe", "report what each source holds per year", runSources},
		{"textguard", "textguard --all|--year Y", "decide how each page is read and price it", runTextguard},
		{"version", "version", "print the version", runVersion},
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage(commands)
		return nil
	}
	for _, c := range commands {
		if c.name == args[0] {
			return c.run(args[1:])
		}
	}
	return fmt.Errorf("unknown command %q, try kvant help", args[0])
}

func usage(commands []command) {
	fmt.Println("kvant builds the Kvant corpus.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  kvant <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	sort.Slice(commands, func(i, j int) bool { return commands[i].name < commands[j].name })
	for _, c := range commands {
		fmt.Printf("  %-30s %s\n", c.usage, c.short)
	}
	fmt.Println()
	fmt.Println("The corpus path comes from --corpus or from KVANT_CORPUS.")
	fmt.Println("The downloaded scans live outside the corpus, under --cache or KVANT_CACHE.")
}

func runVersion([]string) error {
	fmt.Printf("kvant %s, commit %s, built %s\n", version, commit, date)
	return nil
}

func runCorpus(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("corpus needs a subcommand, which is validate")
	}
	switch args[0] {
	case "validate":
		return runCorpusValidate(args[1:])
	default:
		return fmt.Errorf("unknown corpus subcommand %q", args[0])
	}
}

func runCorpusValidate(args []string) error {
	fs := pflag.NewFlagSet("corpus validate", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	quiet := fs.Bool("quiet", false, "print the summary only")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	rep, err := c.Validate()
	if err != nil {
		return err
	}
	if !*quiet {
		for _, f := range rep.Findings {
			for line := range strings.SplitSeq(f.Err.Error(), "\n") {
				fmt.Printf("%s: %s\n", f.Path, line)
			}
		}
	}
	fmt.Println(rep)
	if !rep.OK() {
		return fmt.Errorf("corpus does not validate")
	}
	return nil
}
