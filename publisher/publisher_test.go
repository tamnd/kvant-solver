package publisher_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/publisher"
)

// The markup the site actually serves, cut down to the shapes that matter: a
// paragraph, a formula in a span, a figure with a caption, and the non breaking
// spaces it sets between initials and a surname.
const article = `<div class="block--text copy--target">
<div class="mark--hlwords--text">
<p>Пусть тело движется по окружности радиуса <span class="tex">R</span> с угловой
скоростью <span class="tex">&omega;</span>, тогда <span class="tex">v = &omega;R</span>.</p>
<figure><img src="//www.kvant.digital/data/kvant_1975_1/fig/0012_1.png" alt="Рис. 1"><figcaption>Рис. 1</figcaption></figure>
<p>Об этом писал <em>И.&nbsp;Н.&nbsp;Бронштейн</em> в первом номере.</p>
</div>
</div>`

func TestMarkdownKeepsTheMathematicsAsMathematics(t *testing.T) {
	got, err := publisher.Markdown(article)
	if err != nil {
		t.Fatal(err)
	}
	// The formula spans come out delimited, and the Greek inside them comes out
	// as TeX. A page where ω is loose in the prose is a page the audit cannot
	// tell from a page where the formula was lost.
	if !strings.Contains(got, "$v = \\omega R$") {
		t.Errorf("the formula came out as %q", got)
	}
	if strings.Contains(got, "ω") {
		t.Errorf("a Greek letter is loose in the text:\n%s", got)
	}
}

func TestMarkdownWritesFiguresTheWayAPageDoes(t *testing.T) {
	got, err := publisher.Markdown(article)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "⟦figure⟧\nРис. 1") {
		t.Errorf("the figure came out as:\n%s", got)
	}
}

// A non breaking space is whitespace, and a corpus that keeps it is one where
// searching for the name misses.
func TestMarkdownNormalisesTheSpacesInAName(t *testing.T) {
	got, err := publisher.Markdown(article)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "*И. Н. Бронштейн*") {
		t.Errorf("the byline came out as:\n%s", got)
	}
}

// A formula set as a picture is the one thing this path cannot carry, so it is
// marked where it stood rather than dropped. A dropped formula reads as prose
// that happens to be missing an equation, which is the failure nothing
// downstream can see.
func TestAFormulaSetAsAPictureIsMarked(t *testing.T) {
	got, err := publisher.Markdown(`<p>Отсюда <img src="/f/1.png" alt="формула"> для любого n.</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "⟪illegible⟫") {
		t.Errorf("the picture came out as %q", got)
	}
}

func TestMarkdownWritesListsAndTables(t *testing.T) {
	got, err := publisher.Markdown(`<ol><li>первое</li><li>второе</li></ol>
<table><tr><th>n</th><th>2n</th></tr><tr><td>1</td><td>2</td></tr></table>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1. первое", "2. второе", "| n | 2n |", "| --- | --- |", "| 1 | 2 |"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is not in:\n%s", want, got)
		}
	}
}

// Two readings of the same paragraph, differing in the ways two honest
// readings do: the typography of a dash, the case of a heading, ё against е,
// and the transcription's own folio line, which the publisher's file has no
// equivalent of and must not be charged for.
func TestComparePassesOverTypography(t *testing.T) {
	pub := "Эллипс — замечательная кривая. Всё о ней знал Аполлоний."
	ours := "⟦folio 4⟧\n\n## ЭЛЛИПС\n\nЭллипс - замечательная кривая. Все о ней знал Аполлоний."
	count, _ := publisher.Compare(pub, ours)
	if count.Changed != 0 {
		t.Errorf("%d of %d words differ, want none", count.Changed, count.Words)
	}
}

// Two houses print the same formula with different macros and different braces.
// Counting that as disagreement would make the rate a report on TeX habits, in
// a magazine where a good part of every page is formulae.
func TestTheSameFormulaSpelledTwoWays(t *testing.T) {
	pub := "Тогда $|p|\\le\\sqrt[4]q$ и площадь равна $\\dfrac13{\\smile}ABD$."
	ours := "Тогда $|p| \\leqslant \\sqrt[4]{q}$ и площадь равна $\\frac{1}{3} \\smile ABD$."
	count, examples := publisher.Compare(pub, ours)
	if count.Changed != 0 {
		t.Errorf("%d of %d words differ, want none, the mathematics is the same: %+v",
			count.Changed, count.Words, examples)
	}
}

// A word the archive has and the page does not is the archive being wrong, and
// that is the rate. A word the page has and the archive does not is our article
// holding more of the page than theirs does, and that is the coverage. The
// first measurement got this backwards and scored ninety per cent against text
// that was word for word right, because three pieces shared a page.
func TestOurExtraWordsAreCoverageAndNotError(t *testing.T) {
	pub := "Задача первая про окружность."
	ours := "Хвост предыдущей статьи. Задача первая про окружность. И головоломка после неё."
	count, examples := publisher.Compare(pub, ours)
	if count.Changed != 0 {
		t.Errorf("%d of the archive's %d words counted as wrong, want none", count.Changed, count.Words)
	}
	if len(examples) != 0 {
		t.Errorf("%d examples came back, want none, the archive said nothing we contradict", len(examples))
	}
	if got := count.Coverage(); got > 0.5 {
		t.Errorf("coverage is %.2f, want it to show that most of our page is not theirs", got)
	}
}

// And the failure this is here to catch: an archive text that is not the
// article but a garbled reading of it. Every word it gets wrong is a word we
// cannot take from it.
func TestCompareCountsTheArchivesOwnWrongWords(t *testing.T) {
	pub := "Первое предложение статьи, второе предложение статьи."
	ours := "Первое предложение статьи, второе изложение статьи."
	count, examples := publisher.Compare(pub, ours)
	if count.Changed != 1 {
		t.Errorf("%d of %d words differ, want the one", count.Changed, count.Words)
	}
	if len(examples) == 0 {
		t.Fatal("no examples came back, so nobody can see what went wrong")
	}
	if examples[0].Publisher != "предложение" || examples[0].Vision != "изложение" {
		t.Errorf("the first example is %+v, want the word the two readings part on", examples[0])
	}
}

// A letter misread in a word the other reading has right is not the same defect
// as a word that is not there, and the two have to come out separately or the
// looser one gets quoted as the tighter.
func TestAMisreadLetterIsCountedApartFromAMissingWord(t *testing.T) {
	pub := "явление кавитации известно давно"
	ours := "явление кабитации известно давно"
	count, _ := publisher.Compare(pub, ours)
	if count.Changed != 1 {
		t.Fatalf("%d words differ, want the one", count.Changed)
	}
	if count.Near != 1 {
		t.Errorf("%d of them counted as a near miss, and кабитации is кавитации with a letter wrong", count.Near)
	}
	if got := count.Missing(); got != 0 {
		t.Errorf("missing is %.2f, want none, the word is there and it is spelled wrong", got)
	}
	if count.Rate() == 0 {
		t.Error("the rate hides it entirely, and the split is meant to show the working rather than bury it")
	}
}

func TestADifferentWordIsNotANearMiss(t *testing.T) {
	pub := "второй международный семинар"
	ours := "второй междисциплинарный семинар"
	count, _ := publisher.Compare(pub, ours)
	if count.Near != 0 {
		t.Fatalf("%d near misses, and международный is not международный misspelled", count.Near)
	}
	if got := count.Missing(); got == 0 {
		t.Error("missing is zero, and one of the two readings has a word the other does not")
	}
}

func TestAWordOneReadingDoesNotHaveAtAllIsMissing(t *testing.T) {
	// The failure the whole comparison exists to find: text gone, not text
	// spelled differently.
	pub := "первая строка и вторая строка"
	ours := "первая строка строка"
	count, _ := publisher.Compare(pub, ours)
	if count.Changed-count.Near != 2 {
		t.Fatalf("%d words missing of %d changed, want the two that are gone", count.Changed-count.Near, count.Changed)
	}
}

// The sample has to be the same articles every run, or the rate moves when
// nobody changed anything.
func TestTheSampleIsStableAndPerYear(t *testing.T) {
	var all []publisher.Candidate
	for _, year := range []int{1991, 1992} {
		for i := range 10 {
			all = append(all, publisher.Candidate{
				Year:  year,
				Issue: "kvant_" + itoa(year) + "_1",
				Slug:  "article-" + itoa(i),
			})
		}
	}
	first := publisher.Sample(all, 3)
	if len(first) != 6 {
		t.Fatalf("%d articles sampled over two years, want three each", len(first))
	}
	again := publisher.Sample(all, 3)
	for i := range first {
		if first[i].Slug != again[i].Slug || first[i].Year != again[i].Year {
			t.Fatalf("the sample moved between runs: %v then %v", first[i], again[i])
		}
	}
	// And adding a year does not reshuffle the years before it, which is what
	// makes a rate comparable to the one in the last report.
	for _, c := range publisher.Sample(append(all, publisher.Candidate{Year: 1993, Slug: "later"}), 3) {
		if c.Year != 1993 {
			continue
		}
		if c.Slug != "later" {
			t.Errorf("the new year sampled %q", c.Slug)
		}
	}
}

func TestYearsWeighByLengthAndNotByArticle(t *testing.T) {
	years := publisher.Years([]publisher.Diff{
		{Year: 1993, Count: publisher.Count{Words: 20, Changed: 10, Ours: 20}},
		{Year: 1993, Count: publisher.Count{Words: 980, Changed: 10, Ours: 980}},
	})
	if len(years) != 1 {
		t.Fatalf("%d years, want one", len(years))
	}
	if got := years[0].Rate(); got > 0.05 {
		t.Errorf("the year rate is %.3f, want the long article to carry it", got)
	}
	if years[0].Worst.Words != 20 {
		t.Error("the worst article of the year is not the one with the highest rate")
	}
}

func TestTheStoreKeepsTextOutOfTheCorpus(t *testing.T) {
	store := publisher.Store{Dir: t.TempDir()}
	if _, ok, err := store.Get("kvant_1975_1", "ellips"); err != nil || ok {
		t.Fatalf("an unfetched article came back %v, %v", ok, err)
	}
	if err := store.Put("kvant_1975_1", "ellips", "Эллипс.\n"); err != nil {
		t.Fatal(err)
	}
	text, ok, err := store.Get("kvant_1975_1", "ellips")
	if err != nil || !ok || text != "Эллипс.\n" {
		t.Fatalf("read back %q, %v, %v", text, ok, err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, "kvant_1975_1", "ellips.md")); err != nil {
		t.Error(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
