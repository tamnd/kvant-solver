package ocr_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/ocr"
)

func TestALedgerReadsBackWhatItWrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting", "ocr.jsonl")
	ledger, err := ocr.OpenLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(1975, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := ledger.Append(ocr.Entry{
		TS: when, Target: "kvant_1975_1_p0007", Issue: "kvant_1975_1", Year: 1975,
		Engine: "glm-ocr", Seconds: 1.36, OK: true,
		Usage: api.Usage{InputTokens: 1200, OutputTokens: 800, TotalTokens: 2000},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Append(ocr.Entry{
		TS: when, Target: "kvant_1975_1_p0008", Issue: "kvant_1975_1", Year: 1975,
		Engine: "glm-ocr", Seconds: 2, Reason: "rule 3, the page is too short",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	back, err := ocr.ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Fatalf("read %d entries, want the two that were written", len(back))
	}
	switch {
	case !back[0].TS.Equal(when):
		t.Errorf("the time came back as %s, want %s", back[0].TS, when)
	case back[0].Usage.TotalTokens != 2000:
		t.Errorf("tokens came back as %d, want 2000", back[0].Usage.TotalTokens)
	case !back[0].OK || back[1].OK:
		t.Error("the accepted and the rejected page came back the same way")
	case back[1].Reason == "":
		t.Error("the rejected page came back with no reason")
	}
}

// A machine that dies mid write costs the report its last line and nothing
// else, so a damaged tail must not take the whole file with it.
func TestAHalfWrittenLineDoesNotSpoilTheLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr.jsonl")
	ledger, err := ocr.OpenLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Append(ocr.Entry{Target: "kvant_1975_1_p0001", Year: 1975, OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"target":"kvant_1975_1_p00`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	back, err := ocr.ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || back[0].Target != "kvant_1975_1_p0001" {
		t.Fatalf("read %+v, want the one whole line", back)
	}
}

// A ledger that has never been written to is a run that has not happened, and
// the report says nothing rather than failing.
func TestAMissingLedgerIsNotAnError(t *testing.T) {
	back, err := ocr.ReadLedger(filepath.Join(t.TempDir(), "nothing.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 0 {
		t.Fatalf("read %d entries out of a file that does not exist", len(back))
	}
}

// metered is the engine wrapped in the interface a served lane implements, so a
// test can check the tokens travel from the answer to the ledger line.
type metered struct{ *engine }

func (m metered) ReadMetered(ctx context.Context, image string) (string, api.Usage, error) {
	text, err := m.engine.Read(ctx, image)
	return text, api.Usage{InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500}, err
}

// The point of the ledger is that a page which was read three times and thrown
// away three times cost three times what a page that worked cost, so every
// attempt is a line and not every page.
func TestTheLedgerHoldsALineForEveryAttempt(t *testing.T) {
	runner, _, images := setup(t, func(image string, _ int) string {
		if sheetOf(image) == 2 {
			return "I'm sorry, I can't help with that."
		}
		return page(sheetOf(image))
	})
	runner.Engine = metered{runner.Engine.(*engine)}
	path := filepath.Join(t.TempDir(), "ocr.jsonl")
	ledger, err := ocr.OpenLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	runner.Ledger = ledger
	if _, err := runner.Enqueue(sheets(t, images)); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := runner.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}

	back, err := ocr.ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	// Two pages read once each and one page refused three times.
	if len(back) != 5 {
		t.Fatalf("the ledger holds %d lines, want five", len(back))
	}
	tries, tokens := 0, 0
	for _, entry := range back {
		tokens += entry.Usage.TotalTokens
		if entry.Target != "kvant_1975_1_p0002" {
			continue
		}
		tries++
		if entry.OK {
			t.Error("the refused page is recorded as read")
		}
		if !strings.Contains(entry.Reason, "1") {
			t.Errorf("the refusal is recorded as %q, want the rule that caught it", entry.Reason)
		}
	}
	if tries != 3 {
		t.Errorf("page two has %d lines, want the three attempts it cost", tries)
	}
	if tokens != 5*1500 {
		t.Errorf("the ledger counts %d tokens, want 1500 for each of the five attempts", tokens)
	}
	if back[0].Year != 1975 || back[0].Issue != "kvant_1975_1" {
		t.Errorf("a line does not say which issue it is from: %+v", back[0])
	}
}
