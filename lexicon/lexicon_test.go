package lexicon_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tamnd/kvant-solver/lexicon"
)

// russian is the corpus vocabulary the tests resolve against. Real forms taken
// off pages of 1976, because the point of the lexicon is that it only places a
// word the archive already uses.
var russian = []string{
	"однако", "перпендикулярно", "перпендикуляр", "тетраэдра", "олимпиады",
	"математической", "физико", "периода", "пентамино", "центроид", "ординат",
	"параболы", "одесса", "случае", "наука", "килограмм", "кибернетики",
	"гипотенузы", "лабораториях", "динозавром", "вузы", "теорема", "которые",
}

func build() *lexicon.Lexicon { return lexicon.New(russian) }

func TestRussianSpelledInTwoAlphabetsIsPlaced(t *testing.T) {
	lex := build()
	for _, word := range []string{
		"однako",          // Latin o, d, n, a, k, o through the middle
		"Оdnako",          // and the same word opening with them
		"перpendикулярно", // the international spelling showing through
		"tetraэдра",       // a Greek loan the model reached for in Latin
		"олимпiadы",       // one syllable romanised
		"mатематической",  // a single leading Latin m
		"физico",          // c for к at the end
		"перiodа",         // two Latin runs in one word
		"pentамино",       // the Latin half first
		"центroid",        // the Latin half last
		"ordinат",         //
		"gипотенузы",      // g for г
		"kilogramм",       //
		"киберnetики",     //
		"laboratorиях",    //
		"dinozавром",      //
	} {
		if !lex.Resolves(word) {
			t.Errorf("Resolves(%q) = false, want true, it is a word the corpus uses", word)
		}
	}
}

func TestIdenticalLookingLettersArePlaced(t *testing.T) {
	lex := build()
	// Одеcca is «Одесса» with Latin c twice. A Russian face draws the two the
	// same and a reader cannot tell them apart either.
	for _, word := range []string{"Одеcca", "случae", "Teорема"} {
		if !lex.Resolves(word) {
			t.Errorf("Resolves(%q) = false, want true", word)
		}
	}
}

func TestGarbledWordsAreNotPlaced(t *testing.T) {
	lex := build()
	// These came off dead pages of 1976. None of them is a word in any
	// alphabet, and leaving them counted is the whole reason rule 8 exists.
	for _, word := range []string{
		"Оlimпилад",
		"MEMORIALНОГО",
		"COMПЛЕКСА",
		"непrivibuous",
		"перpendикularы",
		"Гельмогlua",
		"выpusкойших",
		"пiventor",
		"нагrajденый",
	} {
		if lex.Resolves(word) {
			t.Errorf("Resolves(%q) = true, want false, it is not a word", word)
		}
	}
}

func TestAWordTheCorpusDoesNotUseIsNotPlaced(t *testing.T) {
	lex := build()
	// эмigrантами reads back as «эмигрантами» cleanly, and that is not enough.
	// The lexicon has never seen the form, so this cannot tell a romanisation
	// from a misreading that happens to transliterate, and it says so.
	if lex.Resolves("эмigrантами") {
		t.Error("Resolves placed a form the corpus does not use, which is a guess")
	}
}

func TestShortFragmentsAreNotPlaced(t *testing.T) {
	lex := build()
	// Фh and Крd are chess moves with the file or rank missing. Three letters
	// sit close enough to a dozen real words that a hit would mean nothing.
	for _, word := range []string{"Фh", "Крd", "Фa", "Кc"} {
		if lex.Resolves(word) {
			t.Errorf("Resolves(%q) = true, want false, it is too short to place", word)
		}
	}
}

func TestAnAmbiguousWordIsRefused(t *testing.T) {
	// c is к and ц and с, so cом reads back as ком and цом and сом. Two of
	// those being words means this cannot say which was printed.
	lex := lexicon.New([]string{"ком", "сом"})
	if lex.Resolves("comом") {
		t.Error("Resolves picked one of two readings, which is a guess and not a recognition")
	}
}

func TestPlainCyrillicIsNotTheQuestion(t *testing.T) {
	lex := build()
	// Resolves is only ever asked about a word that already mixes alphabets, so
	// a clean Russian word is not its business. It should still not claim one
	// it has never seen.
	if lex.Resolves("несуществующее") {
		t.Error("Resolves claimed a form the lexicon does not hold")
	}
}

func TestCollectCountsOnlyTheCleanRussian(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "content", "ru", "1976", "01", "pages")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "Теорема о параболы и однako теорема снова.\n"
	if err := os.WriteFile(filepath.Join(dir, "0001.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := lexicon.Collect(root, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if count["теорема"] != 2 {
		t.Errorf("теорема counted %d times, want 2", count["теорема"])
	}
	// однako mixes alphabets, and letting it in would let a page vouch for its
	// own misreadings.
	if _, ok := count["однako"]; ok {
		t.Error("Collect took a mixed alphabet word into the lexicon")
	}
}

func TestFormsDropsTheOneOffs(t *testing.T) {
	count := map[string]int{"теорема": 9, "параболы": 2, "нrбкж": 1}
	forms := lexicon.Forms(count, 2)
	if len(forms) != 2 {
		t.Fatalf("Forms kept %v, want the two seen twice or more", forms)
	}
	// Sorted, because the file is committed and read in diffs.
	if forms[0] != "параболы" || forms[1] != "теорема" {
		t.Errorf("Forms returned %v, want it sorted", forms)
	}
}

func TestWriteAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifests", "lexicon.txt")
	if err := lexicon.Write(path, []string{"однако", "теорема"}); err != nil {
		t.Fatal(err)
	}
	lex, err := lexicon.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if lex.Len() != 2 {
		t.Errorf("loaded %d forms, want 2", lex.Len())
	}
	if !lex.Has("Однако") {
		t.Error("Has is case sensitive, and a word at the start of a sentence is the same word")
	}
}

func TestOpenTreatsAMissingLexiconAsNone(t *testing.T) {
	lex, err := lexicon.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open on a corpus with no lexicon returned %v, want it to say none", err)
	}
	if lex != nil {
		t.Error("Open invented a lexicon for a corpus that has none")
	}
}
