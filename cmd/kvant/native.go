package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/katex"
	"github.com/tamnd/kvant-solver/lexicon"
	"github.com/tamnd/kvant-solver/native"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/pdfsrc"
)

// runNative reads an issue out of its own PDF and writes the pages that come
// out whole.
//
// This is the cheap lane and it is the one to try first from 2007 on, but it is
// not the lane that finishes an issue. It hands back a list of the sheets it
// would not read, and those go to kvant ocr in the ordinary way. A run that
// wrote every page it could parse would be cheaper still and would put pages
// with a fraction quietly missing from them into the corpus, which is the one
// outcome this project cannot recover from later.
func runNative(args []string) error {
	// The one subcommand, which reads pages this lane already wrote a second
	// time and compares them against the scan. It hangs off native rather than
	// off report because it does the reading itself, and everything under
	// report only ever writes up work that has already happened.
	if len(args) > 0 && args[0] == "check" {
		return runNativeCheck(args[1:])
	}
	fs := pflag.NewFlagSet("native", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	lang := fs.String("lang", corpus.DefaultLang, "the tree the pages are written into")
	checkTeX := fs.Bool("latex", true, "run every formula through KaTeX before accepting a page")
	dry := fs.Bool("dry-run", false, "measure the issue and write nothing")
	verbose := fs.Bool("v", false, "print a line per page rather than a line per issue")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name an --issue or a --year, and the native path starts at 2007")
	}
	if err := pdfsrc.Available(); err != nil {
		return err
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

	options := ocr.Options{}
	lex, err := lexicon.Open(c.Root)
	if err != nil {
		return err
	}
	options.Lexicon = lex
	if *checkTeX {
		renderer, err := katex.New()
		if err != nil {
			return err
		}
		options.LaTeX = renderer
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	read, sent, failed := 0, 0, 0
	for i := range issues {
		key := issues[i].Key
		idx, err := cache.ReadIndex(key)
		if err != nil {
			return err
		}
		if idx.PDF == nil || !idx.PDF.Have() {
			fmt.Printf("%s: no issue PDF, run kvant fetch pdf --issue %s\n", key, key)
			continue
		}
		run, err := readNative(ctx, c, cache, idx, *lang, options, *dry, *verbose)
		if err != nil {
			// One unreadable file must not cost the other hundred and thirty
			// three. The issue keeps every sheet it has, which is the same place
			// it was before this run, and kvant ocr will read it.
			if ctx.Err() != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "kvant: %s: %v\n", key, err)
			failed++
			continue
		}
		read += run.read
		sent += len(run.vision)
		fmt.Printf("%s: %d of %d pages read from the file, %d to a model, %d scripts put back\n",
			key, run.read, run.pages, len(run.vision), run.scripts)
		if run.stale > 0 {
			fmt.Printf("%s: %d pages an earlier native run wrote no longer pass, and were taken back out\n",
				key, run.stale)
		}
		if len(run.vision) > 0 {
			fmt.Printf("%s: kvant ocr --issue %s --sheets %s\n", key, key, sheetList(run.vision))
		}
		for _, why := range run.reasons() {
			fmt.Printf("  %s\n", why)
		}
	}
	fmt.Printf("%d pages read from their files, %d for a model\n", read, sent)
	if failed > 0 {
		fmt.Printf("%d issues the file itself would not open, and they go to a model whole\n", failed)
	}
	return nil
}

// nativeRun is what one issue came to.
type nativeRun struct {
	pages   int
	read    int
	scripts int
	// vision is the sheets a model still has to read, in scan order.
	vision []int
	// stale is the pages an earlier native run wrote that this one took back.
	stale int
	// why counts the rejections by which measurement made them, with one page's
	// wording kept as the example.
	//
	// By the rule and not by the sentence, because the sentence carries the
	// count that page measured and grouping on that prints forty rows that
	// differ in one number where there are two things going on.
	why map[native.Rule]*reason
}

type reason struct {
	n       int
	example string
}

// reasons is the rejection tally, the commonest first.
func (r nativeRun) reasons() []string {
	rules := make([]native.Rule, 0, len(r.why))
	for rule := range r.why {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if a, b := r.why[rules[i]].n, r.why[rules[j]].n; a != b {
			return a > b
		}
		return rules[i] < rules[j]
	})
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, fmt.Sprintf("%3d %-10s %s", r.why[rule].n, rule, r.why[rule].example))
	}
	return out
}

func readNative(ctx context.Context, c *corpus.Corpus, cache *fetch.Cache, idx *fetch.Index,
	lang string, options ocr.Options, dry, verbose bool) (nativeRun, error) {
	run := nativeRun{why: map[native.Rule]*reason{}}
	key, err := corpus.ParseIssueKey(idx.Issue)
	if err != nil {
		return run, err
	}
	src, err := pdfsrc.Open(cache.Path(idx.PDF.SHA256))
	if err != nil {
		return run, err
	}
	pages, err := src.Layout(ctx, 0, 0)
	if err != nil {
		return run, err
	}
	run.pages = len(pages)

	// The whole issue is read before any of it is judged, because the pages the
	// magazine leaves unnumbered are placed by the pages either side of them and
	// there is no way to know what those are one page at a time.
	read := make([]native.Page, len(pages))
	for i, layout := range pages {
		if err := ctx.Err(); err != nil {
			return run, err
		}
		read[i] = native.Read(layout)
		run.scripts += read[i].Scripts
	}
	folios := native.Folios(read)

	sheets := sheetsByFolio(idx)
	for i, layout := range pages {
		if err := ctx.Err(); err != nil {
			return run, err
		}
		page := read[i]

		// The printed number is what ties a page of the file to a sheet of the
		// scan. The two are usually the same count in the same order, and usually
		// is not good enough to file a page under: an issue with a folded insert
		// has more sheets than pages, and everything downstream is keyed on the
		// scan.
		sheet, ok := sheets[folios[i]]
		if !ok {
			// The sheet is not named, because nothing here knows which sheet it
			// is. That is right rather than a gap: kvant ocr reads every sheet
			// with no page file, so the covers and the unnumbered pages are
			// already on its list without this run putting them there.
			run.reject(0, native.RuleUnnumbered,
				"the page prints no number the scan carries, which is a cover or an insert")
			continue
		}
		expect := native.Expect{
			Folio:    sheet.Page,
			Cover:    sheet.Page == 0,
			Inferred: page.Folio == 0,
		}
		if v := native.Judge(page, expect); !v.OK {
			dropped, err := dropStale(c, lang, key, sheet, dry)
			if err != nil {
				return run, err
			}
			run.stale += dropped
			run.reject(sheet.Ord+1, v.Rule, v.Why)
			if verbose {
				fmt.Printf("  page %d sheet %d: %s\n", layout.Number, sheet.Ord+1, v.Why)
			}
			continue
		}
		// The same nine rules the vision lane's pages pass. The corpus has one
		// standard and not one per lane, and a page that came free is not a page
		// that is held to less.
		want := ocr.Expect{Issue: idx.Issue, Sheet: sheet.Ord + 1, Folio: sheet.Page, Cover: sheet.Page == 0}
		problems := ocr.Validate(page.Body, want, options)
		if len(problems) > 0 {
			dropped, err := dropStale(c, lang, key, sheet, dry)
			if err != nil {
				return run, err
			}
			run.stale += dropped
			// The nine rules have their own names and they go in the same tally
			// under them, so a run says whether it was the geometry or the text
			// that stopped a page without anyone having to know which list a
			// word like latex or script came off.
			run.reject(sheet.Ord+1, native.Rule(problems[0].Rule), problems[0].String())
			if verbose {
				fmt.Printf("  page %d sheet %d: %s\n", layout.Number, sheet.Ord+1, problems[0])
			}
			continue
		}
		if !dry {
			if err := writeNative(c, lang, key, idx, sheet, page); err != nil {
				return run, err
			}
		}
		run.read++
		if verbose {
			fmt.Printf("  page %d sheet %d: %d words, %d scripts put back\n",
				layout.Number, sheet.Ord+1, page.Words, page.Scripts)
		}
	}
	return run, nil
}

// reject records a page the native lane would not write and puts its sheet on
// the list for a model.
func (r *nativeRun) reject(sheet int, rule native.Rule, why string) {
	if r.why[rule] == nil {
		r.why[rule] = &reason{example: why}
	}
	r.why[rule].n++
	if sheet > 0 {
		r.vision = append(r.vision, sheet)
	}
}

// dropStale removes a page an earlier native run wrote and this one rejects.
//
// The rules here get tuned and the years get read again, and a page that was
// good enough for last month's rules and is not good enough for this month's
// has to go, because kvant ocr leaves alone any sheet that already has a file.
// Left where it is, it is a page nothing will ever look at again and which this
// run has just decided is wrong.
//
// Only a file this lane wrote is touched. A page a model read is somebody
// else's work and costs money to make, and the native lane has no business
// deciding it is stale.
func dropStale(c *corpus.Corpus, lang string, key corpus.IssueKey, sheet fetch.Sheet, dry bool) (int, error) {
	path := c.PagePath(lang, corpus.PageID{Issue: key, Index: sheet.Ord + 1})
	front := &corpus.PageFront{}
	if _, err := corpus.Load(path, front); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if front.Extraction != corpus.ExtractionNative {
		return 0, nil
	}
	if dry {
		return 1, nil
	}
	if err := os.Remove(path); err != nil {
		return 0, err
	}
	return 1, nil
}

func writeNative(c *corpus.Corpus, lang string, key corpus.IssueKey, idx *fetch.Index,
	sheet fetch.Sheet, page native.Page) error {
	id := corpus.PageID{Issue: key, Index: sheet.Ord + 1}
	front := &corpus.PageFront{
		Issue:     key.String(),
		Year:      key.Year,
		Number:    key.Number,
		PageIndex: id.Index,
		// The label is what the page prints, and a page that prints nothing gets
		// an empty one however sure the run is of where it sits. That is what the
		// vision lane writes for the same page, and a label this lane invented
		// would be the one field in the corpus that says a thing the paper does
		// not.
		PageLabel: label(page.Folio),
		Rubrics:   ocr.Rubrics(page.Body),
		Provenance: corpus.Provenance{
			Lang:       lang,
			Source:     "kvant_mccme",
			SourceID:   idx.PDF.URL,
			SourceScan: idx.Issue,
			Extraction: corpus.ExtractionNative,
			// No model made this page and no prompt shaped it, so both of those
			// fields stay empty. That is the difference the corpus is recording:
			// a native page is the publisher's own typesetting and there is
			// nothing between it and the file it came out of.
		},
	}
	return corpus.Save(c.PagePath(lang, id), front, page.Body)
}

func label(folio int) string {
	if folio == 0 {
		return ""
	}
	return fmt.Sprint(folio)
}

// sheetsByFolio indexes the scan by the number printed on each sheet.
//
// This is the map between the file and the scan, and it is built out of the
// printed numbers rather than out of the counts because the two counts are not
// always the same. An issue with an insert has a sheet the PDF does not, and
// from that sheet on a run that trusted the ordinals would file every page of
// the issue one slot late.
//
// Sheets the magazine did not number are not in the map at all. A page of the
// file that prints no number cannot be tied to one of them, and the native lane
// says so and lets a model read it.
func sheetsByFolio(idx *fetch.Index) map[int]fetch.Sheet {
	out := make(map[int]fetch.Sheet, len(idx.Sheets))
	for _, sheet := range idx.Sheets {
		if sheet.Page == 0 {
			continue
		}
		// The first sheet carrying a number wins. A duplicate is a defect in the
		// scan, and 1975 issue 8 has one, so it is not hypothetical.
		if _, seen := out[sheet.Page]; !seen {
			out[sheet.Page] = sheet
		}
	}
	return out
}

// sheetList writes the sheets for the --sheets flag of kvant ocr, so that the
// repair pass is a line somebody can paste.
func sheetList(sheets []int) string {
	sort.Ints(sheets)
	out := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		out = append(out, fmt.Sprint(sheet))
	}
	return strings.Join(out, ",")
}
