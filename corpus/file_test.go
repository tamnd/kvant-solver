package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func samplePage() *PageFront {
	return &PageFront{
		Issue:     "kvant_1975_1",
		Year:      1975,
		Number:    "1",
		PageIndex: 7,
		PageLabel: "5",
		Rubrics:   []string{"math-circle"},
		Provenance: Provenance{
			Lang:            "ru",
			Source:          "kvant.digital",
			SourceScan:      "data/kvant_1975_1/jpg/0007.jpg",
			Extraction:      ExtractionVision,
			ExtractionModel: "test-model",
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0007.md")
	body := "Line one of the body.\n\nLine two, with $x^2 + y^2 = z^2$ in it.\n"

	if err := Save(path, samplePage(), body); err != nil {
		t.Fatal(err)
	}

	var got PageFront
	gotBody, err := Load(path, &got)
	if err != nil {
		t.Fatal(err)
	}
	if gotBody != body {
		t.Errorf("body came back as %q", gotBody)
	}
	if got.PageIndex != 7 || got.Issue != "kvant_1975_1" || got.Lang != "ru" {
		t.Errorf("front matter came back as %+v", got)
	}
	if got.ContentSHA256 != HashBody(body) {
		t.Error("Save did not record the hash of the body it wrote")
	}
}

func TestSaveIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0007.md")
	if err := Save(path, samplePage(), "Body.\n"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "0007.md" {
		t.Errorf("directory holds %d entries after one Save", len(entries))
	}
}

func TestLoadRejectsAHashThatDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0007.md")
	if err := Save(path, samplePage(), "Body as written.\n"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), "Body as written.", "Body as edited by hand.", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	var got PageFront
	if _, err := Load(path, &got); err == nil {
		t.Fatal("Load accepted a body that does not match its recorded hash")
	}
	if _, err := LoadUnchecked(path, &got); err != nil {
		t.Fatalf("LoadUnchecked should still read it: %v", err)
	}
}

func TestSaveRefusesInvalidFrontMatter(t *testing.T) {
	dir := t.TempDir()
	page := samplePage()
	page.Year = 1976 // does not match the issue key
	if err := Save(filepath.Join(dir, "0007.md"), page, "Body.\n"); err == nil {
		t.Fatal("Save wrote a file whose year contradicts its issue")
	}
	if _, err := os.Stat(filepath.Join(dir, "0007.md")); !os.IsNotExist(err) {
		t.Error("Save left a file behind after refusing to write it")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "0007.md")
	head := "---\nissue: kvant_1975_1\nyear: 1975\nnumber: \"1\"\npage_index: 7\nlang: ru\ncontent_sha256: " + HashBody("Body.\n") + "\ntypo_field: yes\n---\n\nBody.\n"
	if err := os.WriteFile(path, []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	var got PageFront
	if _, err := Load(path, &got); err == nil {
		t.Fatal("Load accepted a field nothing reads, which is how a typo becomes silent data loss")
	}
}

func TestSplit(t *testing.T) {
	if _, _, err := Split([]byte("no front matter here\n")); err != ErrNoFrontMatter {
		t.Errorf("want ErrNoFrontMatter, got %v", err)
	}
	if _, _, err := Split([]byte("---\nissue: x\n")); err == nil {
		t.Error("an unclosed front matter block should be an error")
	}
	front, body, err := Split([]byte("---\nissue: x\n---\n\nBody.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if front != "issue: x" || body != "Body.\n" {
		t.Errorf("split gave front %q body %q", front, body)
	}
}

func TestTranslationStaleness(t *testing.T) {
	tr := Translated{
		TranslatedFrom:  "content/ru/1975/01/pages/0007.md",
		SourceSHA256:    "aaa",
		GlossaryTerms:   "bbb",
		GlossaryVersion: 3,
	}
	if stale, why := tr.Stale("aaa", "bbb", "ccc", "ccc"); stale {
		t.Errorf("fresh file reported stale: %s", why)
	}
	if stale, _ := tr.Stale("zzz", "bbb", "ccc", "ccc"); !stale {
		t.Error("a changed source should be stale")
	}
	if stale, _ := tr.Stale("aaa", "zzz", "ccc", "ccc"); !stale {
		t.Error("moved glossary terms should be stale")
	}
	if stale, _ := tr.Stale("aaa", "bbb", "zzz", "ccc"); !stale {
		t.Error("an edited prompt should be stale")
	}
	// A file written before a field existed records nothing for it, and that
	// is not a reason to call it stale.
	old := Translated{TranslatedFrom: "x"}
	if stale, why := old.Stale("aaa", "bbb", "ccc", ""); stale {
		t.Errorf("a file from before these fields existed reported stale: %s", why)
	}
}
