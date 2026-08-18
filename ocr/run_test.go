package ocr_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/ocr"
	"github.com/tamnd/kvant-solver/queue"
)

// page is what a good answer looks like: a folio line, a rubric, two columns,
// and enough Russian to clear the length and language rules.
func page(folio int) string {
	return fmt.Sprintf(`⟦folio %d⟧

⟦rubric⟧ Задачник «Кванта»

## Задачи М1234 и М1235

Условие первой задачи занимает несколько строк и содержит формулу $x^2+y^2=z^2$,
которую нужно разобрать внимательно, потому что она встречается дальше в решении.

⟦column⟧

Вторая задача о вписанной окружности треугольника. Пусть $r$ радиус вписанной
окружности, а $R$ радиус описанной. Докажите неравенство $R \ge 2r$ и укажите,
когда достигается равенство. Это классический результат, известный как
неравенство Эйлера, и школьнику полезно найти его доказательство самому.
`, folio)
}

// engine answers from a table, and records what it was asked, so a test can say
// how many times a page was read.
type engine struct {
	mu     sync.Mutex
	answer func(image string, attempt int) string
	calls  map[string]int
}

func (e *engine) Name() string { return "test-engine" }

func (e *engine) Read(_ context.Context, image string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.calls == nil {
		e.calls = map[string]int{}
	}
	e.calls[filepath.Base(image)]++
	return e.answer(filepath.Base(image), e.calls[filepath.Base(image)]), nil
}

func (e *engine) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls[name]
}

// setup builds a corpus, a queue and three page images on disk.
func setup(t *testing.T, answer func(image string, attempt int) string) (*ocr.Runner, *engine, string) {
	t.Helper()
	root := t.TempDir()
	images := filepath.Join(root, "images")
	if err := os.MkdirAll(filepath.Join(root, "corpus", "content"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		name := filepath.Join(images, fmt.Sprintf("%04d.jpg", i))
		if err := os.WriteFile(name, fmt.Appendf(nil, "not really a jpeg, page %d", i), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store, err := corpus.Open(filepath.Join(root, "corpus"))
	if err != nil {
		t.Fatal(err)
	}
	work, err := queue.Open(filepath.Join(root, "queue"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := corpus.NewIssueKey(1975, "1")
	if err != nil {
		t.Fatal(err)
	}
	eng := &engine{answer: answer}
	return &ocr.Runner{
		Queue:        work,
		Engine:       eng,
		Corpus:       store,
		Issue:        key,
		Source:       "kvant.digital",
		Scan:         "1975-01.pdf",
		PromptSHA256: strings.Repeat("a", 64),
		Workers:      2,
	}, eng, images
}

func sheets(t *testing.T, dir string) []ocr.Sheet {
	t.Helper()
	list, err := ocr.Sheets(dir, func(index int) ocr.Expect {
		return ocr.Expect{Issue: "kvant_1975_1", Folio: index}
	})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestAGoodRunWritesEveryPage(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		return page(sheetOf(image))
	})
	added, err := runner.Enqueue(sheets(t, images))
	if err != nil {
		t.Fatal(err)
	}
	if added != 3 {
		t.Fatalf("queued %d sheets, want 3", added)
	}
	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Read != 3 || summary.Rejected != 0 {
		t.Fatalf("got %s, want three pages read and nothing rejected", summary)
	}
	have, err := runner.Corpus.Pages("ru", runner.Issue)
	if err != nil {
		t.Fatal(err)
	}
	if len(have) != 3 {
		t.Fatalf("corpus holds pages %v, want three of them", have)
	}
}

// The page file has to carry what came off the page and what read it, because
// every later stage decides staleness from those two fields and never by
// reading the page again.
func TestThePageFileRecordsItsProvenance(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		return page(sheetOf(image))
	})
	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var front corpus.PageFront
	path := runner.Corpus.PagePath("ru", corpus.PageID{Issue: runner.Issue, Index: 2})
	body, err := corpus.Load(path, &front)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case front.PageLabel != "2":
		t.Errorf("page_label is %q, want the printed 2", front.PageLabel)
	case front.Extraction != corpus.ExtractionVision:
		t.Errorf("extraction is %q, want %q", front.Extraction, corpus.ExtractionVision)
	case front.ExtractionModel != "test-engine":
		t.Errorf("extraction_model is %q, want the engine that read it", front.ExtractionModel)
	case front.PromptSHA256 != strings.Repeat("a", 64):
		t.Errorf("prompt_sha256 is %q, want the prompt the runner was given", front.PromptSHA256)
	case len(front.Rubrics) != 1 || front.Rubrics[0] != "Задачник «Кванта»":
		t.Errorf("rubrics are %v, want the one banner the page prints", front.Rubrics)
	case !strings.Contains(body, "⟦column⟧"):
		t.Error("the markers were stripped out of the body, and assemble needs them")
	}
}

// A page that comes back wrong is read again, and the retry sees the same job
// rather than a new one.
func TestARejectedPageIsReadAgain(t *testing.T) {
	runner, eng, images := setup(t, func(image string, attempt int) string {
		if sheetOf(image) == 2 && attempt == 1 {
			return "I'm sorry, I can't help with that."
		}
		return page(sheetOf(image))
	})
	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The first pass fails page two and leaves it pending. Draining again is
	// what a second invocation of the command does.
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := eng.count("0002.jpg"); got != 2 {
		t.Fatalf("page two was read %d times, want two", got)
	}
	have, err := runner.Corpus.Pages("ru", runner.Issue)
	if err != nil {
		t.Fatal(err)
	}
	if len(have) != 3 {
		t.Fatalf("corpus holds pages %v, want all three after the retry", have)
	}
}

// The magazine numbers its displayed equations in the margin and a model
// transcribing that writes \eqno, which is what plain TeX calls it. KaTeX has
// never implemented it, so a correct reading came back rejected for an
// undefined control sequence: 233 pages of 1975 died that way, all of them
// right about the page. The lane rewrites the number on the way past.
func TestANumberedEquationIsNotAFailure(t *testing.T) {
	runner, eng, images := setup(t, func(image string, _ int) string {
		return page(sheetOf(image)) +
			"\nЧастота и длина волны связаны соотношением\n\n$$c=\\lambda\\nu \\eqno(1)$$\n"
	})
	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	summary, err := runner.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Rejected != 0 {
		t.Fatalf("got %s, want nothing rejected", summary)
	}
	if got := eng.count("0001.jpg"); got != 1 {
		t.Fatalf("page one was read %d times, want the first answer to have been taken", got)
	}
	path := runner.Corpus.PagePath("ru", corpus.PageID{Issue: runner.Issue, Index: 1})
	body, err := corpus.Load(path, &corpus.PageFront{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `\tag{(1)}`) {
		t.Errorf("the page reads %q, want the number as \\tag{(1)}", body)
	}
}

// Three failures kill the job and put it on the repair queue with the rule that
// killed it, because a fourth identical attempt buys nothing.
func TestADeadPageGoesToRepair(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		if sheetOf(image) == 2 {
			return "⟦folio 2⟧\n\n" + strings.Repeat("Текст страницы, который читается плохо ⟪illegible⟫ ", 10)
		}
		return page(sheetOf(image))
	})
	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := runner.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := runner.Queue.List(queue.StageOCR, queue.Pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d jobs are still pending after three attempts, want none", len(pending))
	}
	repairs, err := runner.Queue.List(queue.StageRepair, queue.Pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(repairs) != 1 {
		t.Fatalf("the repair queue holds %d jobs, want one", len(repairs))
	}
	job := repairs[0]
	if job.Target != "kvant_1975_1_p0002" {
		t.Errorf("the repair is for %q, want page two", job.Target)
	}
	if !strings.Contains(job.Meta["rules"], string(ocr.RuleIllegible)) {
		t.Errorf("the repair carries rules %q, want the illegible rule that killed it", job.Meta["rules"])
	}
	if job.Meta["dpi"] != "400" {
		t.Errorf("the repair asks for dpi %q, want a re-render at 400", job.Meta["dpi"])
	}
}

// A page already in the corpus is not queued again. This is what makes a second
// run of the whole year cheap rather than a second day and a half.
func TestPagesAlreadyReadAreNotQueued(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		return page(sheetOf(image))
	})
	list := sheets(t, images)
	if _, err := runner.Enqueue(list); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	added, err := runner.Enqueue(list)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Fatalf("queued %d sheets on the second pass, want none", added)
	}
}

// The service's own error page is not a transcription and must not be blamed on
// the scan. It also must never reach the corpus.
func TestAProviderErrorPageIsNotAPage(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		if sheetOf(image) == 1 {
			return "Something went wrong. Please try again later."
		}
		return page(sheetOf(image))
	})
	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	summary, _ := runner.Run(context.Background())
	if summary.Read != 2 {
		t.Fatalf("got %s, want the two real pages read", summary)
	}
	path := runner.Corpus.PagePath("ru", corpus.PageID{Issue: runner.Issue, Index: 1})
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the error page was written to the corpus")
	}
}

func TestSheetsOrdersByFilename(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"0010.jpg", "0002.jpg", "notes.txt", "0001.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, err := ocr.Sheets(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("read %d sheets, want the three images and not the text file", len(list))
	}
	for i, want := range []string{"0001.png", "0002.jpg", "0010.jpg"} {
		if filepath.Base(list[i].Image) != want {
			t.Errorf("sheet %d is %s, want %s", i+1, filepath.Base(list[i].Image), want)
		}
		if list[i].Index != i+1 {
			t.Errorf("sheet %s has index %d, want %d", want, list[i].Index, i+1)
		}
	}
}

func sheetOf(image string) int {
	var n int
	fmt.Sscanf(strings.TrimSuffix(image, filepath.Ext(image)), "%d", &n)
	return n
}
