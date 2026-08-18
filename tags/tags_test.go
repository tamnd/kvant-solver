package tags_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/tags"
)

func open(t *testing.T, root string) *tags.Store {
	t.Helper()
	store, err := tags.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func assign(t *testing.T, store *tags.Store, label string) corpus.Tag {
	t.Helper()
	tag, err := store.Assign(label)
	if err != nil {
		t.Fatal(err)
	}
	if !tag.Valid() {
		t.Fatalf("assign gave %q, which is not the shape of a tag", tag)
	}
	return tag
}

func TestATagSurvivesTheProcessThatMadeIt(t *testing.T) {
	root := t.TempDir()
	store := open(t, root)
	want := map[string]corpus.Tag{}
	for _, label := range []string{"1975-1-bronshteyn-ellips", "M381", "1980-7-kak-ustroen-lazer"} {
		want[label] = assign(t, store, label)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	again := open(t, root)
	if again.Len() != len(want) {
		t.Fatalf("read back %d tags, wrote %d", again.Len(), len(want))
	}
	for label, tag := range want {
		got, ok := again.Tag(label)
		if !ok {
			t.Errorf("%s lost its tag on the way through the file", label)
			continue
		}
		if got != tag {
			t.Errorf("%s came back as %s, was %s", label, got, tag)
		}
	}
}

// The tags file is where the truth lives, so it is worth looking at rather than
// only reading back through the same code that wrote it.
func TestTheFileIsOneLinePerObject(t *testing.T) {
	root := t.TempDir()
	store := open(t, root)
	tag := assign(t, store, "1975-1-bronshteyn-ellips")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, tags.Dir, tags.TagsFile))
	if err != nil {
		t.Fatal(err)
	}
	want := string(tag) + ",1975-1-bronshteyn-ellips"
	if !strings.Contains(string(body), want+"\n") {
		t.Errorf("the file does not have the line %q in it:\n%s", want, body)
	}
	if _, err := os.Stat(filepath.Join(root, tags.Dir, tags.AliasesFile)); err == nil {
		t.Error("a corpus that has never renamed anything should have no aliases file")
	}
}

// Assignment is derived from the label, so the same corpus tagged twice from
// scratch has to come out the same. This is what makes the register
// reproducible rather than a record of the order somebody ran things in.
func TestTaggingTheSameCorpusTwiceGivesTheSameTags(t *testing.T) {
	labels := []string{"1975-1-a", "1975-2-b", "1976-3-c", "M400", "M401"}
	first, second := open(t, t.TempDir()), open(t, t.TempDir())
	for _, label := range labels {
		if a, b := assign(t, first, label), assign(t, second, label); a != b {
			t.Errorf("%s got %s in one run and %s in the other", label, a, b)
		}
	}
}

// The order objects are asked about in only matters when two of them want the
// same tag, and the file is what settles it. A store that already knows a tag
// hands the same one back however many times it is asked.
func TestAskingTwiceDoesNotAssignTwice(t *testing.T) {
	store := open(t, t.TempDir())
	first := assign(t, store, "M381")
	second := assign(t, store, "M381")
	if first != second {
		t.Errorf("M381 got %s and then %s", first, second)
	}
	if store.Len() != 1 {
		t.Errorf("the register holds %d entries for one object", store.Len())
	}
}

// A collision is not hypothetical at this size, so the probe has to be exercised
// rather than trusted. Filling the register by hand with every tag one label
// would derive is not possible from outside the package, so this checks the
// property instead: several thousand labels, no tag used twice.
func TestNoTagIsUsedTwice(t *testing.T) {
	store := open(t, t.TempDir())
	seen := map[corpus.Tag]string{}
	for year := 1970; year < 1990; year++ {
		for number := 1; number <= 12; number++ {
			for ordinal := 1; ordinal <= 20; ordinal++ {
				label := articleLabel(year, number, ordinal)
				tag := assign(t, store, label)
				if had, ok := seen[tag]; ok {
					t.Fatalf("%s names both %s and %s", tag, had, label)
				}
				seen[tag] = label
			}
		}
	}
	if len(seen) != store.Len() {
		t.Fatalf("%d distinct tags over %d entries", len(seen), store.Len())
	}
}

func articleLabel(year, number, ordinal int) string {
	return fmt.Sprintf("%d-%d-article-%02d", year, number, ordinal)
}

func TestARenameKeepsTheTagAndLeavesTheOldName(t *testing.T) {
	root := t.TempDir()
	store := open(t, root)
	tag := assign(t, store, "1975-1-staroe-nazvanie")
	if err := store.Rename("1975-1-staroe-nazvanie", "1975-1-novoe-nazvanie"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	again := open(t, root)
	if label, ok := again.Label(tag); !ok || label != "1975-1-novoe-nazvanie" {
		t.Errorf("%s names %q, want the new label", tag, label)
	}
	if got, ok := again.Tag("1975-1-novoe-nazvanie"); !ok || got != tag {
		t.Errorf("the renamed object has tag %q, want %s", got, tag)
	}
	// This is the whole reason for the aliases file: something written down
	// before the rename still has to find the object.
	gotTag, gotLabel, ok := again.Resolve("1975-1-staroe-nazvanie")
	if !ok {
		t.Fatal("a citation to the old label resolves to nothing")
	}
	if gotTag != tag || gotLabel != "1975-1-novoe-nazvanie" {
		t.Errorf("the old label resolved to %s (%s)", gotTag, gotLabel)
	}
	if again.Len() != 1 {
		t.Errorf("a rename added an entry, the register holds %d", again.Len())
	}
}

func TestRenamingSomethingUntaggedIsAnError(t *testing.T) {
	store := open(t, t.TempDir())
	if err := store.Rename("nothing-here", "somewhere-else"); err == nil {
		t.Fatal("renaming an object with no tag should say so")
	}
}

func TestResolveTakesATagOrALabel(t *testing.T) {
	store := open(t, t.TempDir())
	tag := assign(t, store, "M381")
	for _, what := range []string{string(tag), "M381"} {
		gotTag, gotLabel, ok := store.Resolve(what)
		if !ok {
			t.Errorf("%q resolved to nothing", what)
			continue
		}
		if gotTag != tag || gotLabel != "M381" {
			t.Errorf("%q resolved to %s (%s)", what, gotTag, gotLabel)
		}
	}
	if _, _, ok := store.Resolve("M999"); ok {
		t.Error("a problem that was never tagged should not resolve")
	}
}

// The file is one line per object with a comma between the columns, so a label
// carrying either of those would produce a file that does not read back.
func TestALabelThatWouldBreakTheFileIsRefused(t *testing.T) {
	store := open(t, t.TempDir())
	for _, label := range []string{"", "   ", "has,comma", "has\nnewline"} {
		if _, err := store.Assign(label); err == nil {
			t.Errorf("%q was accepted as a label", label)
		}
	}
}

func TestABrokenFileIsRefusedRatherThanRead(t *testing.T) {
	for name, body := range map[string]string{
		"a repeated tag":       "0A3F,first\n0A3F,second\n",
		"a repeated label":     "0A3F,same\n1B4Z,same\n",
		"no comma":             "0A3F first\n",
		"a lower case tag":     "0a3f,first\n",
		"a short tag":          "0A3,first\n",
		"a tag naming nothing": "0A3F,\n",
	} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, tags.Dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, tags.Dir, tags.TagsFile), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := tags.Open(root); err == nil {
			t.Errorf("a tags file with %s was read without complaint", name)
		}
	}
}

func TestCommentsAndBlankLinesAreNotEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, tags.Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Permanent tags, one per object.\n\n0A3F,1975-1-ellips\n\n"
	if err := os.WriteFile(filepath.Join(root, tags.Dir, tags.TagsFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	store := open(t, root)
	if store.Len() != 1 {
		t.Fatalf("read %d entries out of one line of content", store.Len())
	}
}

func TestACorpusWithNoTagsYetIsNotAnError(t *testing.T) {
	store := open(t, t.TempDir())
	if store.Len() != 0 {
		t.Fatalf("an empty corpus came back with %d tags", store.Len())
	}
}

// The label is the identifier as it stands, because two items in one issue can
// share everything but the publisher's suffix.
func TestTheLabelIsTheIdentifierAsItStands(t *testing.T) {
	for _, id := range []string{
		"1975-10-zadachi-nashih-chitateley-71ebb3c1",
		"1975-10-zadachi-nashih-chitateley-ad7b807c",
		"M381",
	} {
		if got := tags.Label(id); got != id {
			t.Errorf("%s became %q", id, got)
		}
	}
	if got := tags.Label("  M381\n"); got != "M381" {
		t.Errorf("a label with whitespace around it became %q", got)
	}
}
