package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/lexicon"
	"github.com/tamnd/kvant-solver/manifest"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/publisher"
	"github.com/tamnd/kvant-solver/queue"
	"github.com/tamnd/kvant-solver/report"
)

// runNativeCheck reads a sample of the pages the native lane wrote through the
// vision lane as well, and says how far the two readings agree.
//
// The native lane is the one place in this project where nothing outside ever
// looked at the result. A page a model read off a picture is checked by nine
// rules and, where it matters, by a second model. A page lifted out of a text
// layer passes the same nine rules on text the file itself produced, which
// establishes that the file is consistent with itself and not that it says what
// the paper says. A broken font encoding, a column taken in the wrong order, a
// page filed as a sheet it is not: all three are self consistent and all three
// are wrong.
//
// So the picture is the witness. The scan and the file are two independent
// records of the same paper, and where they agree word for word there is
// nothing left for either to be wrong about.
func runNativeCheck(args []string) error {
	fs := pflag.NewFlagSet("native check", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	lang := fs.String("lang", corpus.DefaultLang, "the tree the pages were written into")
	sample := fs.Int("sample", 6, "pages an issue to read a second time")
	engineFlags := addLaneFlags(fs, "served")
	work := fs.String("work", "", "where page images are staged, cache/work by default")
	// The report names sheets for somebody to look at, and the reading that
	// named them is thrown away when the run ends. This is how they get to see
	// it: the second reading lands in a tree shaped like the corpus, so the two
	// pages can be put side by side in a diff.
	keep := fs.String("keep", "", "keep the second reading here, as a corpus, so the two can be diffed")
	out := fs.String("out", "", "write here instead of corpus/reports/native-check.md")
	stdout := fs.Bool("stdout", false, "print the document instead of writing it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name an --issue or a --year")
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
	reader, err := engineFlags.build()
	if err != nil {
		return err
	}

	options := ocr.Options{}
	lex, err := lexicon.Open(c.Root)
	if err != nil {
		return err
	}
	options.Lexicon = lex
	renderer, err := katex.New()
	if err != nil {
		return err
	}
	options.LaTeX = renderer

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var checks []report.Check
	for i := range issues {
		key, err := corpus.ParseIssueKey(issues[i].Key)
		if err != nil {
			return err
		}
		idx, err := cache.ReadIndex(issues[i].Key)
		if err != nil {
			return err
		}
		written, err := nativePages(c, cache, idx, *lang, key)
		if err != nil {
			return err
		}
		if len(written) == 0 {
			fmt.Printf("%s: no native pages whose sheet is in the cache\n", issues[i].Key)
			continue
		}
		want := spread(written, *sample)
		fmt.Printf("%s: %d native pages, reading sheets %s again through %s\n",
			issues[i].Key, len(written), sheetList(want), reader.Engine.Name())

		seen, err := readAgain(ctx, cache, idx, &issues[i], key, *lang, want, reader, options,
			dirOr(*work, cache.Dir, "work"), *keep)
		if err != nil {
			return err
		}
		for _, index := range want {
			scan, ok := seen[index]
			if !ok {
				// A sheet the vision lane could not read is not evidence against
				// the native page. It is said out loud and left out of the rates,
				// because counting it either way would be inventing a result.
				fmt.Printf("  sheet %d: the second reading failed, which settles nothing either way\n", index)
				checks = append(checks, report.Check{Issue: issues[i].Key, Year: key.Year, Sheet: index, Unread: true})
				continue
			}
			file, err := pageBody(c, *lang, key, index)
			if err != nil {
				return err
			}
			// The scan goes first, so the rate is the share of the paper's words
			// the file does not have. That is the direction this check exists for.
			count, examples := publisher.Compare(scan, file)
			check := report.Check{
				Issue: issues[i].Key, Year: key.Year, Sheet: index,
				Count: count, Examples: examples,
			}
			checks = append(checks, check)
			fmt.Printf("  sheet %d: %.1f%% of the scan's %d words are not in the file\n",
				index, check.Rate()*100, count.Words)
		}
	}
	if len(checks) == 0 {
		return fmt.Errorf("no page was read twice, so there is nothing to compare")
	}

	md := report.CheckMarkdown(checks, time.Now())
	if *stdout {
		fmt.Print(md)
		return nil
	}
	path := *out
	if path == "" {
		if *f.root == "" {
			return fmt.Errorf("no corpus, so nowhere to write, pass --corpus or --out")
		}
		path = filepath.Join(*f.root, "reports", "native-check.md")
	}
	if err := writeReport(path, md); err != nil {
		return err
	}
	fmt.Printf("%d pages compared, written to %s\n", len(checks), path)
	return nil
}

// nativePages is the pages of an issue the native lane wrote and the cache can
// still show a picture of.
//
// Both halves are needed. A page a model read is not this lane's work and
// proves nothing about it, and a sheet whose image was never downloaded has no
// second record to be compared against.
func nativePages(c *corpus.Corpus, cache *fetch.Cache, idx *fetch.Index, lang string, key corpus.IssueKey) ([]int, error) {
	indexes, err := c.Pages(lang, key)
	if err != nil {
		return nil, err
	}
	staged := make(map[int]bool, len(idx.Sheets))
	for _, sheet := range idx.Sheets {
		if sheet.Have() && cache.Has(sheet.SHA256) {
			staged[sheet.Ord+1] = true
		}
	}
	var out []int
	for _, index := range indexes {
		if !staged[index] {
			continue
		}
		front := &corpus.PageFront{}
		if _, err := corpus.Load(c.PagePath(lang, corpus.PageID{Issue: key, Index: index}), front); err != nil {
			return nil, err
		}
		if front.Extraction != corpus.ExtractionNative {
			continue
		}
		out = append(out, index)
	}
	return out, nil
}

// spread picks n of the pages, evenly through the issue.
//
// Evenly and not at random, because an issue is not one thing. The front is
// prose about a physicist, the middle is the problem set, the back is the
// chess column and the answers, and each of those puts a different amount of
// mathematics through the text layer. A sample drawn out of one of them would
// measure that one and get quoted as the issue.
func spread(pages []int, n int) []int {
	if n <= 0 || n >= len(pages) {
		return pages
	}
	out := make([]int, 0, n)
	for i := range n {
		out = append(out, pages[i*len(pages)/n])
	}
	return out
}

// readAgain runs the vision lane over the named sheets and hands back what it
// read, keyed by sheet.
//
// It reads into a corpus of its own and a queue of its own, both of them thrown
// away at the end. This is a measurement and not a repair: writing into the
// real corpus would overwrite the pages being measured with the readings they
// are being measured against, which is the one way to build a check that can
// never fail. The separate queue matters for the same reason, since a job the
// ordinary lane already finished for this sheet would otherwise be found done
// and the run would compare a page against itself.
func readAgain(ctx context.Context, cache *fetch.Cache, idx *fetch.Index,
	iss *manifest.Issue, key corpus.IssueKey, lang string, want []int,
	reader lane, options ocr.Options, work, keep string) (map[int]string, error) {
	dir := keep
	if dir == "" {
		temp, err := os.MkdirTemp("", "kvant-check-")
		if err != nil {
			return nil, err
		}
		dir = temp
		defer func() { _ = os.RemoveAll(temp) }()
	}
	if err := os.MkdirAll(filepath.Join(dir, "content"), 0o755); err != nil {
		return nil, err
	}
	scratch, err := corpus.Open(dir)
	if err != nil {
		return nil, err
	}
	jobs, err := queue.Open(filepath.Join(dir, "queue"))
	if err != nil {
		return nil, err
	}

	sheets, err := sheetsFor(cache, idx, "", work)
	if err != nil {
		return nil, err
	}
	sheets = pick(sheets, want)
	if len(sheets) == 0 {
		return nil, fmt.Errorf("%s has none of the sheets to be checked", idx.Issue)
	}
	runner := &ocr.Runner{
		Queue: jobs, Engine: reader.Engine, Corpus: scratch,
		Lang: lang, Issue: key,
		Source: scanSource(iss), Scan: idx.Issue,
		Prompt: reader.Prompt, PromptSHA256: reader.SHA256,
		Folio:   reader.Folio,
		Options: options, Workers: 1,
		Logf: func(format string, args ...any) { fmt.Printf("    "+format+"\n", args...) },
	}
	if _, err := runner.Enqueue(sheets); err != nil {
		return nil, err
	}
	// The summary is dropped on purpose. What the second reading cost and how
	// many of its own rules it broke is the vision lane's business, and the only
	// thing this run wants from it is the pages that came out.
	if _, err := runner.Run(ctx); err != nil {
		return nil, err
	}

	out := make(map[int]string, len(want))
	for _, index := range want {
		text, err := pageBody(scratch, lang, key, index)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out[index] = text
	}
	return out, nil
}

// pageBody is the text of one page, without its front matter.
func pageBody(c *corpus.Corpus, lang string, key corpus.IssueKey, index int) (string, error) {
	return corpus.Load(c.PagePath(lang, corpus.PageID{Issue: key, Index: index}), &corpus.PageFront{})
}
