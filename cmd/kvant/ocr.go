package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/prompt"
	"github.com/tamnd/kvant-solver/queue"
)

func runOCR(args []string) error {
	fs := pflag.NewFlagSet("ocr", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	queueDir := fs.String("queue", "", "where the job queue lives, cache/queue by default")
	work := fs.String("work", "", "where page images are staged, cache/work by default")
	images := fs.String("images", "", "read these images instead of staging from the cache")
	// The endpoint is the whole of the engine choice. chatgpt-tool serve and
	// vllm serve answer the same route, so the difference between the browser
	// lane and the card in the other room is a URL and a model name.
	endpoint := fs.String("api", envOr("KVANT_API", "http://127.0.0.1:8000/v1/chat/completions"), "an OpenAI shaped endpoint that takes an image")
	key := fs.String("api-key", os.Getenv("KVANT_API_KEY"), "bearer token for the endpoint, when it wants one")
	model := fs.String("model", envOr("KVANT_MODEL", "zai-org/GLM-OCR"), "the model to ask")
	// A document model wants one sentence and a general model wants the whole
	// page prompt, and sending the wrong one to either costs both speed and
	// accuracy. It defaults to short because the endpoint above defaults to the
	// card.
	short := fs.Bool("short-prompt", true, "send the one sentence prompt a document model wants")
	// The repair lane. A page the card cannot read is not read by asking the
	// card again, because the sampling is pinned and the answer is the same one;
	// it is read by something else.
	lane := fs.String("engine", "served", "served for an endpoint, cli for a local program")
	command := fs.String("cli", envOr("KVANT_CLI", "claude -p --allowedTools Read"), "the program the cli engine runs")
	only := fs.IntSlice("sheets", nil, "read only these sheets, which is how a repair pass is aimed")
	workers := fs.Int("workers", 1, "pages in flight at once, which the browser lane must leave at one")
	timeout := fs.Duration("timeout", ocr.DefaultPageTimeout, "how long one page may take")
	lang := fs.String("lang", corpus.DefaultLang, "the tree the pages are written into")
	checkTeX := fs.Bool("latex", true, "run every formula through KaTeX before accepting a page")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name an --issue, which for the first milestone is kvant_1975_1")
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
	if *checkTeX {
		// One renderer for the whole run. It is safe to share and loading the
		// script costs a tenth of a second, which is not worth paying per page.
		renderer, err := katex.New()
		if err != nil {
			return err
		}
		options.LaTeX = renderer
	}

	text, sum := prompt.OCRPage(), prompt.OCRPageSHA256()
	if *short {
		text, sum = prompt.OCRShort(), prompt.OCRShortSHA256()
	}

	client := &api.Client{
		URL:        *endpoint,
		APIKey:     *key,
		HTTPClient: &http.Client{Timeout: *timeout + time.Minute},
		UserAgent:  "kvant/" + version,
	}

	var engine ocr.Engine
	// The folio band is only read for the lane that needs it. A general model
	// answers the folio question in the page prompt, and reading a corner of
	// the same page a second time to check it would be one more call for less
	// evidence.
	var folio *ocr.Folioer
	switch *lane {
	case "served":
		engine = ocr.Served{Client: client, Model: *model, Prompt: text, Timeout: *timeout}
		if *short {
			folio = &ocr.Folioer{Engine: ocr.Served{
				Client:  client,
				Model:   *model,
				Prompt:  prompt.OCRBand(),
				Timeout: *timeout,
			}}
		}
	case "cli":
		fields := strings.Fields(*command)
		if len(fields) == 0 {
			return fmt.Errorf("--cli names no program")
		}
		// A general model gets the whole page prompt whatever --short-prompt
		// says, because --short-prompt is about the card. The one sentence
		// version exists to stop a recogniser drowning in instructions, and a
		// model that follows instructions should be given them: it is the only
		// lane that can answer the folio question by itself.
		// The default --model names the card's model, which did not read this
		// page, so it is only believed here when it was asked for.
		name := fields[0]
		if fs.Changed("model") {
			name = *model
		}
		engine = ocr.Command{
			Model:   name,
			Path:    fields[0],
			Args:    fields[1:],
			Prompt:  prompt.OCRPage(),
			Timeout: *timeout,
		}
		text, sum = prompt.OCRPage(), prompt.OCRPageSHA256()
	default:
		return fmt.Errorf("--engine is served or cli, not %q", *lane)
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
			Queue: jobs, Engine: engine, Corpus: c,
			Lang: *lang, Issue: key,
			Source: scanSource(&issues[i]), Scan: idx.Issue,
			Prompt: text, PromptSHA256: sum,
			Folio:   folio,
			Options: options, Workers: *workers,
			Logf: func(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) },
		}
		// A sheet is named on the command line because the last lane could not
		// read it, and the queue remembers that: three failures make a job dead,
		// a dead job is already somewhere, and Enqueue would add nothing. Naming
		// a sheet is the instruction to read it again, so the dead jobs for the
		// named ones go back to pending here. Sheets that already have a page
		// file are left alone, which is the same line Enqueue draws.
		if len(*only) > 0 {
			revived, err := revive(jobs, runner, c, *lang, key, sheets)
			if err != nil {
				return err
			}
			if revived > 0 {
				fmt.Printf("%s: %d dead sheets put back in the queue\n", issues[i].Key, revived)
			}
		}
		added, err := runner.Enqueue(sheets)
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d sheets, %d queued, %s at %d worker(s)\n",
			issues[i].Key, len(sheets), added, engine.Name(), max(1, *workers))

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

// revive puts the dead jobs for a narrowed work list back in the queue and says
// how many it moved.
//
// The page file is the guard. A sheet that already has one is skipped whatever
// the queue says about it, because the corpus is what has to be complete and
// re-reading a page that is already good is a call spent to overwrite something
// that was fine.
func revive(jobs *queue.Queue, runner *ocr.Runner, c *corpus.Corpus, lang string, key corpus.IssueKey, sheets []ocr.Sheet) (int, error) {
	moved := 0
	for _, sheet := range sheets {
		path := c.PagePath(lang, corpus.PageID{Issue: key, Index: sheet.Index})
		if _, err := os.Stat(path); err == nil {
			continue
		}
		ok, err := jobs.Reset(queue.StageOCR, runner.Target(sheet.Index))
		if err != nil {
			return moved, err
		}
		if ok {
			moved++
		}
	}
	return moved, nil
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

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
