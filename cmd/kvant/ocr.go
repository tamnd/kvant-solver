package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/lexicon"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
)

func runOCR(args []string) error {
	fs := pflag.NewFlagSet("ocr", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	queueDir := fs.String("queue", "", "where the job queue lives, cache/queue by default")
	work := fs.String("work", "", "where page images are staged, cache/work by default")
	images := fs.String("images", "", "read these images instead of staging from the cache")
	engineFlags := addLaneFlags(fs, "served")
	only := fs.IntSlice("sheets", nil, "read only these sheets, which is how a repair pass is aimed")
	reread := fs.Bool("reread", false, "throw the named sheets' pages away and read them again")
	workers := fs.Int("workers", 1, "pages in flight at once, which the browser lane must leave at one")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the pages are written into")
	checkTeX := fs.Bool("latex", true, "run every formula through KaTeX before accepting a page")
	ledgerPath := fs.String("ledger", "", "where the cost ledger is appended, cache/ledger/ocr.jsonl by default")
	noLedger := fs.Bool("no-ledger", false, "do not record what the run cost")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name an --issue, which for the first milestone is kvant_1975_1")
	}
	// Rereading without naming sheets would be a year deleted by a typo. There
	// is no use for it either: a pass that wants to reread everything is a pass
	// that should be reading into an empty tree.
	if *reread && len(*only) == 0 {
		return fmt.Errorf("--reread needs --sheets, because it throws pages away")
	}

	issues, err := selected(f)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		return fmt.Errorf("nothing selected")
	}
	c, err := corpus.Open(*f.root)
	if err != nil {
		return err
	}
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	jobs, err := queue.Open(dirOr(*queueDir, cache.Dir, "queue"))
	if err != nil {
		return err
	}

	options := ocr.Options{}
	// The lexicon of the corpus being written into, so that rule 8 asks about
	// the words this archive uses. A corpus with none gets the rule as it was.
	lex, err := lexicon.Open(c.Root)
	if err != nil {
		return err
	}
	options.Lexicon = lex
	if *checkTeX {
		// One renderer for the whole run. It is safe to share and loading the
		// script costs a tenth of a second, which is not worth paying per page.
		renderer, err := katex.New()
		if err != nil {
			return err
		}
		options.LaTeX = renderer
	}

	reader, err := engineFlags.build()
	if err != nil {
		return err
	}

	// The ledger is on by default. A twenty year run is the thing whose cost
	// somebody asks about afterwards, and a run that was not recorded cannot
	// answer, so the flag turns it off rather than on.
	var ledger *ocr.Ledger
	if !*noLedger {
		ledger, err = ocr.OpenLedger(fileOr(*ledgerPath, cache.Dir, "ledger", "ocr.jsonl"))
		if err != nil {
			return err
		}
		defer func() { _ = ledger.Close() }()
	}

	// Ctrl-C stops the run and leaves the queue as it is, which is the whole
	// reason there is a queue. The next run picks the leases up when they
	// expire and reads the pages that are still missing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for i := range issues {
		key, err := corpus.ParseIssueKey(issues[i].Key)
		if err != nil {
			return err
		}
		idx, err := cache.ReadIndex(issues[i].Key)
		if err != nil {
			return err
		}
		sheets, err := sheetsFor(cache, idx, *images, dirOr(*work, cache.Dir, "work"))
		if err != nil {
			return err
		}
		if len(sheets) == 0 {
			return fmt.Errorf("%s has no downloaded sheets, run kvant fetch pages first", issues[i].Key)
		}
		// Kept before the narrowing, because what is missing is missing from the
		// issue and not from the pass. A repair pass over thirteen sheets that
		// reported the issue complete because it read its thirteen would be
		// worse than useless.
		total := len(sheets)
		if len(*only) > 0 {
			sheets = pick(sheets, *only)
			if len(sheets) == 0 {
				return fmt.Errorf("%s has none of the sheets named by --sheets", issues[i].Key)
			}
		}

		runner := &ocr.Runner{
			Queue: jobs, Engine: reader.Engine, Corpus: c,
			Lang: *lang, Issue: key,
			Source: scanSource(&issues[i]), Scan: idx.Issue,
			Prompt: reader.Prompt, PromptSHA256: reader.SHA256,
			Folio:   reader.Folio,
			Options: options, Workers: *workers, Ledger: ledger,
			Logf: func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) },
		}
		// The page file is what stops a sheet being read twice, everywhere in
		// this command, so rereading is throwing the page away and letting the
		// ordinary path do the rest. One guard rather than a flag threaded
		// through the runner and the queue and remembered at every call site.
		//
		// This exists because a page can be well formed and wrong. The shape
		// rules pass it, nothing downstream complains, and the only way anybody
		// finds out is a second reading of the same paper disagreeing with it.
		// When that happens the page has to go, and until now the only way to
		// make that happen was rm.
		if *reread {
			for _, sheet := range sheets {
				path := c.PagePath(*lang, corpus.PageID{Issue: key, Index: sheet.Index})
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
			fmt.Printf("%s: %d pages thrown away to be read again\n", issues[i].Key, len(sheets))
		}
		// A sheet is named on the command line because the last lane could not
		// read it, and the queue remembers that: three failures make a job dead,
		// a dead job is already somewhere, and Enqueue would add nothing. Naming
		// a sheet is the instruction to read it again, so the finished jobs for
		// the named ones go back to pending here. Sheets that already have a
		// page file are left alone, which is the same line Enqueue draws.
		//
		// Under --reread the job is done rather than dead, because the sheet was
		// read and the page it produced is what somebody wants gone. Deleting
		// that page above is not enough on its own: Enqueue skips an id the queue
		// already holds, in any state, so without this the flag threw ninety nine
		// pages away and read back none of them.
		if len(*only) > 0 {
			states := []queue.State{queue.Dead}
			if *reread {
				states = append(states, queue.Done)
			}
			finished, err := jobIDs(jobs, states...)
			if err != nil {
				return err
			}
			revived, err := revive(jobs, runner, c, *lang, key, sheets, finished)
			if err != nil {
				return err
			}
			if revived > 0 {
				fmt.Printf("%s: %d finished sheets put back in the queue\n", issues[i].Key, revived)
			}
		}
		added, err := runner.Enqueue(sheets)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d sheets, %d queued, %s at %d worker(s)\n",
			issues[i].Key, len(sheets), added, reader.Engine.Name(), max(1, *workers))

		summary, err := runner.Run(ctx)
		fmt.Printf("%s: %s\n", issues[i].Key, summary)
		if err != nil {
			return err
		}
		missing, err := c.Missing(*lang, key, total)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			fmt.Printf("%s: still missing %d pages, run kvant audit for the reasons\n", issues[i].Key, len(missing))
		}
	}
	return nil
}

// sheetsFor is the work list for one issue.
//
// The cache stores blobs by hash and nothing else, so the images have to be
// named before anything can read them: an OCR endpoint decides how to decode a
// picture from the media type, and the media type here comes from the file
// extension. Staging is hard links where the filesystem allows it, so a year of
// sheets costs its bytes once.
func sheetsFor(cache *fetch.Cache, idx *fetch.Index, images, work string) ([]ocr.Sheet, error) {
	if images != "" {
		return ocr.Sheets(images, nil)
	}
	dir := filepath.Join(work, idx.Issue)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sheets := make([]ocr.Sheet, 0, len(idx.Sheets))
	for i := range idx.Sheets {
		sheet := &idx.Sheets[i]
		if !sheet.Have() || !cache.Has(sheet.SHA256) {
			continue
		}
		// The page index is the sheet's place in the scan and not its place in
		// what happened to download. Counting the loop would renumber every page
		// after a sheet the fetch missed, and the numbers are in filenames and
		// in the contents rows that point at them, so they have to mean the same
		// thing on a partial run as on a whole one.
		index := sheet.Ord + 1
		ext := filepath.Ext(sheet.File)
		if ext == "" {
			ext = ".jpg"
		}
		path := filepath.Join(dir, fmt.Sprintf("%04d%s", index, ext))
		if err := link(cache.Path(sheet.SHA256), path); err != nil {
			return nil, err
		}
		sheets = append(sheets, ocr.Sheet{
			Image: path,
			Index: index,
			// A sheet the manifest gives no printed number to is a cover or an
			// insert. Rules 1 and 5 do not run on those, because a cover is
			// three words of title and no folio and would fail both.
			Expect: ocr.Expect{
				Issue: idx.Issue,
				Sheet: index,
				Folio: sheet.Page,
				Cover: sheet.Page == 0,
			},
		})
	}
	return sheets, nil
}

// pick narrows a work list to the sheets named on the command line.
//
// A repair pass is aimed and not swept. The pages that need one are the few the
// first pass could not read, they are named in the audit, and re-reading the
// whole issue to reach thirteen of them costs the other seventy one calls to a
// slower and more expensive lane.
func pick(sheets []ocr.Sheet, want []int) []ocr.Sheet {
	keep := make(map[int]bool, len(want))
	for _, index := range want {
		keep[index] = true
	}
	var out []ocr.Sheet
	for _, sheet := range sheets {
		if keep[sheet.Index] {
			out = append(out, sheet)
		}
	}
	return out
}

// revive puts the finished jobs for a narrowed work list back in the queue and
// says how many it moved.
//
// The page file is the guard. A sheet that already has one is skipped whatever
// the queue says about it, because the corpus is what has to be complete and
// re-reading a page that is already good is a call spent to overwrite something
// that was fine.
//
// The index comes from jobIDs. A job is filed under a hash of its target and
// its prompt, so the target alone does not name a file, and the first version
// of this passed the target and quietly reset nothing: the run printed its work
// list, moved zero pages and exited happy.
func revive(jobs *queue.Queue, runner *ocr.Runner, c *corpus.Corpus, lang string, key corpus.IssueKey, sheets []ocr.Sheet, finished map[string]string) (int, error) {
	moved := 0
	for _, sheet := range sheets {
		path := c.PagePath(lang, corpus.PageID{Issue: key, Index: sheet.Index})
		if _, err := os.Stat(path); err == nil {
			continue
		}
		id, ok := finished[runner.Target(sheet.Index)]
		if !ok {
			continue
		}
		reset, err := jobs.Restage(queue.StageOCR, id, runner.Meta(sheet))
		if err != nil {
			return moved, err
		}
		if reset {
			moved++
		}
	}
	return moved, nil
}

// jobIDs indexes the OCR jobs in the given states by the page they were
// reading.
//
// One pass over the directory for a whole run. The alternative is a search per
// page, and the dead list is the small one only until a decade has been through
// the queue.
//
// Which states get indexed is the whole of the difference between aiming a
// repair pass and rereading. A named sheet with no page file failed, so the
// job is dead and that is the only list worth walking. A named sheet under
// --reread had a page a moment ago, which means the job is done, and a done job
// is the one thing that both Add and the dead list ignore. That is not a corner
// case, it is the case: a page that is well formed and wrong is exactly the
// page somebody reaches for --reread to get rid of, and until now the flag
// deleted it, printed the count it had deleted, queued nothing and left a hole
// where the bad page had been.
func jobIDs(jobs *queue.Queue, states ...queue.State) (map[string]string, error) {
	out := map[string]string{}
	for _, state := range states {
		list, err := jobs.List(queue.StageOCR, state)
		if err != nil {
			return nil, err
		}
		for _, job := range list {
			out[job.Target] = job.ID
		}
	}
	return out, nil
}

// scanSource names the archive the sheets came from, which goes into the
// provenance of every page read from them. Only one of the three serves page
// images, so this is a lookup and not a decision, but it is written down
// because a page that cannot say where its picture came from is not evidence
// of anything.
func scanSource(iss *manifest.Issue) string {
	switch {
	case iss.Sources.Digital != nil:
		return "kvant_digital"
	case iss.Sources.MCCME != nil:
		return "kvant_mccme"
	case iss.Sources.MathNet != nil:
		return "mathnet_ru"
	}
	return ""
}

// link puts a named copy of a blob where the engine can read it, preferring a
// hard link and falling back to a copy on filesystems that will not.
func link(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return nil
	}
	if err := os.Link(from, to); err == nil {
		return nil
	}
	raw, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, raw, 0o644)
}

func dirOr(set, base, name string) string {
	if set != "" {
		return set
	}
	return filepath.Join(base, name)
}

// fileOr is dirOr for a path that ends in a file rather than a directory.
func fileOr(set, base string, parts ...string) string {
	if set != "" {
		return set
	}
	return filepath.Join(append([]string{base}, parts...)...)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
