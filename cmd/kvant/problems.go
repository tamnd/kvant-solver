package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/problems"
)

func runProblems(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("problems needs a subcommand, which is build, list or crosscheck")
	}
	switch args[0] {
	case "build":
		return runProblemsBuild(args[1:])
	case "list":
		return runProblemsList(args[1:])
	case "crosscheck":
		return runProblemsCrosscheck(args[1:])
	default:
		return fmt.Errorf("unknown problems subcommand %q", args[0])
	}
}

// runProblemsBuild recovers Задачник «Кванта» out of the assembled articles.
//
// It always reads the whole corpus, with no year flags, because the pairing is
// archive wide: a problem posed in 1975 issue 3 is answered in issue 7, and a
// build restricted to one year would file every problem it found as unanswered
// and every solution as orphaned. There is no useful partial answer here, only
// a wrong one.
func runProblemsBuild(args []string) error {
	fs := pflag.NewFlagSet("problems build", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	lang := fs.String("lang", corpus.DefaultLang, "the tree to read")
	write := fs.Bool("write", true, "write the problem files as well as the manifest")
	dry := fs.Bool("dry-run", false, "print the counts and write nothing")
	gaps := fs.String("gaps", "", "write every gap to this file, one line each, grouped by reason")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := corpus.Open(*root)
	if err != nil {
		return err
	}
	articles, err := loadProblemArticles(c, *lang)
	if err != nil {
		return err
	}
	zadachnik := 0
	for _, a := range articles {
		if !a.Spill {
			zadachnik++
		}
	}
	if zadachnik == 0 {
		return fmt.Errorf("no Задачник articles under %s, run kvant assemble first", c.Root)
	}

	res := problems.Build(articles)
	counts := res.Manifest.Count()
	credits := problems.CountCredits(res.Credits)
	fmt.Printf("%d Задачник articles, %s\n", zadachnik, counts)
	if credits.Lists > 0 {
		fmt.Printf("%s\n", credits)
	}
	if len(res.Gaps) > 0 {
		fmt.Printf("%d gaps, the first few:\n", len(res.Gaps))
		for _, g := range res.Gaps[:min(len(res.Gaps), 10)] {
			fmt.Printf("  %s in %s: %s\n", g.ID, g.Issue, g.Reason)
		}
	}
	if *gaps != "" {
		if err := writeGaps(*gaps, res.Gaps); err != nil {
			return err
		}
		fmt.Printf("wrote %d gaps to %s\n", len(res.Gaps), *gaps)
	}
	if *dry {
		fmt.Println("dry run, nothing written")
		return nil
	}

	store, err := manifest.Open(c.Root)
	if err != nil {
		return err
	}
	note := []string{
		"Задачник «Кванта», recovered from the assembled articles.",
		counts.String(),
	}
	if credits.Lists > 0 {
		note = append(note, credits.String())
	}
	if err := store.Write(problems.ManifestFile, res.Manifest, note...); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", store.Path(problems.ManifestFile))

	if !*write {
		return nil
	}
	written := 0
	for _, doc := range res.Documents {
		id, err := corpus.ParseProblemID(doc.Front.ID)
		if err != nil {
			return err
		}
		path := c.ProblemPath(*lang, id)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		front := doc.Front
		if err := corpus.Save(path, &front, doc.Body()); err != nil {
			return err
		}
		written++
	}
	fmt.Printf("wrote %d problem files under %s\n",
		written, filepath.Join(c.Root, "content", *lang, "problems"))
	return nil
}

// runProblemsList prints what the manifest holds, which is how the gap list is
// read without opening the YAML.
func runProblemsList(args []string) error {
	fs := pflag.NewFlagSet("problems list", pflag.ContinueOnError)
	root := fs.String("corpus", os.Getenv("KVANT_CORPUS"), "path to a tamnd/kvant checkout")
	subject := fs.String("subject", "", "math or physics, both by default")
	open := fs.Bool("open", false, "only the problems the magazine never answered")
	if err := fs.Parse(args); err != nil {
		return err
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
		return err
	}

	shown := 0
	for _, e := range m.Entries {
		if *subject != "" && string(e.Subject) != *subject {
			continue
		}
		if *open && e.Solvable() {
			continue
		}
		posed, solved := "?", "unanswered"
		if e.Posed != nil {
			posed = e.Posed.Issue
		}
		if e.Solved != nil {
			solved = e.Solved.Issue
		}
		fmt.Printf("%-8s %-8s posed %-16s solved %s\n", e.ID, e.Subject, posed, solved)
		shown++
	}
	fmt.Printf("%d of %d problems\n", shown, len(m.Entries))
	return nil
}

// loadProblemArticles reads every Задачник article in the corpus, and the
// other articles marked as spill.
//
// The rubric is what selects them rather than the title, because the title is
// checked afterwards to decide which half an article is, and a run that
// selected on the title would silently drop any heading the reading lane
// mangled. Selecting on the rubric and then failing to classify leaves a
// countable difference instead.
func loadProblemArticles(c *corpus.Corpus, lang string) ([]problems.Article, error) {
	dirs, err := filepath.Glob(filepath.Join(c.Root, "content", lang, "*", "*", "articles"))
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)

	var out []problems.Article
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			path := filepath.Join(dir, name)
			front := &corpus.ArticleFront{}
			// Unchecked for the same reason the audit reads pages unchecked: a
			// body that no longer matches its hash is somebody's edit, and this
			// should report on what is on disk rather than refuse to look.
			body, err := corpus.LoadUnchecked(path, front)
			if err != nil {
				return nil, err
			}
			// The rest of the issue comes along marked, because the last
			// problems of a spread are often assembled onto the first page of
			// the article that follows it.
			spill := front.Rubric != problems.Rubric && !strings.Contains(front.RubricSub, "Задачник")
			rel, err := filepath.Rel(c.Root, path)
			if err != nil {
				rel = path
			}
			out = append(out, problems.Article{
				Path:  filepath.ToSlash(rel),
				Front: *front,
				Body:  body,
				Spill: spill,
			})
		}
	}
	return out, nil
}

// writeGaps puts the whole gap list where it can be read, since the summary
// shows ten of what is usually hundreds. Grouping by reason first is what makes
// it useful: an unread year, a misread number and a reprint call for different
// work, and a list in issue order hides which of them dominates.
func writeGaps(path string, gaps []problems.Gap) error {
	byReason := map[string][]problems.Gap{}
	var reasons []string
	for _, g := range gaps {
		key := g.Reason
		if i := strings.Index(key, ", the "); i > 0 && strings.HasPrefix(key, "printed again") {
			key = key[:i]
		}
		if _, ok := byReason[key]; !ok {
			reasons = append(reasons, key)
		}
		byReason[key] = append(byReason[key], g)
	}
	sort.SliceStable(reasons, func(i, j int) bool { return len(byReason[reasons[i]]) > len(byReason[reasons[j]]) })
	var b strings.Builder
	fmt.Fprintf(&b, "# Задачник gaps\n\n%d gaps.\n", len(gaps))
	for _, r := range reasons {
		fmt.Fprintf(&b, "\n## %s (%d)\n\n", r, len(byReason[r]))
		for _, g := range byReason[r] {
			fmt.Fprintf(&b, "- %s %s in %s: %s\n", g.ID, g.Kind, g.Issue, g.Reason)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
