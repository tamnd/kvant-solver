package publisher_test

import (
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/publisher"
)

// A body of the length these decisions are taken on, so a test is not measuring
// the behaviour of a rate over eight words.
func body(words ...string) string {
	var out []string
	for i := range 300 {
		out = append(out, words[i%len(words)])
	}
	return strings.Join(out, " ")
}

func TestTheTypedTextWinsWhereItIsWhole(t *testing.T) {
	// The whole reason for fetching it. Our reading of the same pages carries
	// the folio line and a running head the typist had no reason to type, and
	// that difference must not be enough to refuse it.
	t.Parallel()
	ours := "⟦folio 12⟧\n\n## ЭЛЛИПС\n\n" + body("эллипс", "замечательная", "кривая", "аполлоний")
	typed := body("эллипс", "замечательная", "кривая", "аполлоний")
	got, why := publisher.Prefer(ours, typed)
	if why != "" {
		t.Fatalf("refused: %s", why)
	}
	if got != typed {
		t.Error("the assembled body was kept over the publisher's own text")
	}
}

func TestAFragmentIsRefusedAndSaysWhy(t *testing.T) {
	// The failure this rule exists for. The site holds the opening paragraph
	// of some articles and nothing else, and taking one of those would replace
	// a whole article with a fragment and then stamp it as coming from the
	// better source, which is the one mistake nothing downstream could see.
	t.Parallel()
	ours := body("эллипс", "замечательная", "кривая", "аполлоний")
	typed := strings.Join(strings.Fields(ours)[:60], " ")
	got, why := publisher.Prefer(ours, typed)
	if why == "" {
		t.Fatal("a fifth of the article was taken as the article")
	}
	if got != ours {
		t.Error("the fragment was written anyway")
	}
	if !strings.Contains(why, "%") {
		t.Errorf("the reason does not say how short it is: %q", why)
	}
}

func TestAnIssueWithNoPagesReadTakesTheTypedTextOnTrust(t *testing.T) {
	// There is nothing to check it against and text is better than no article
	// at all. This is the case the lane exists for: a year nobody has read yet.
	t.Parallel()
	typed := body("эллипс", "кривая")
	got, why := publisher.Prefer("", typed)
	if got != typed || why != "" {
		t.Fatalf("got %d chars and %q, want the typed text", len(got), why)
	}
}

func TestNoTypedTextLeavesTheAssembledBodyAlone(t *testing.T) {
	t.Parallel()
	ours := body("эллипс", "кривая")
	if got, why := publisher.Prefer(ours, ""); got != ours || why != "" {
		t.Fatalf("got %d chars and %q, want ours untouched", len(got), why)
	}
}

func TestAMisspelledTypedTextIsStillTheArticle(t *testing.T) {
	// Kept counts words that are gone and not words that are spelled
	// differently, so an old orthography or a typo every few lines does not
	// look like a missing half.
	t.Parallel()
	ours := body("эллипс", "замечательная", "кривая", "аполлоний")
	typed := strings.ReplaceAll(ours, "замечательная", "замѣчательная")
	if got, why := publisher.Prefer(ours, typed); why != "" || got != typed {
		t.Fatalf("refused over spelling: %s", why)
	}
}

func TestTheTitleGoesOnTopOfTheTypedText(t *testing.T) {
	// Every article assembled from our own pages has its title as a heading,
	// because the title is printed on the page. The typed text is the prose
	// alone, and without this those articles would be the only ones in the
	// corpus whose body does not say what they are.
	t.Parallel()
	got := publisher.Titled("Дифракционная окраска насекомых", "Крылья бабочки переливаются.")
	want := "## Дифракционная окраска насекомых\n\nКрылья бабочки переливаются."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAMarkerAboveTheTitleStaysAboveIt(t *testing.T) {
	// A figure at the head of the article is above the title on the page, and
	// the body says where things are on the page.
	t.Parallel()
	got := publisher.Titled("Задачи", "⟦figure⟧\n\n1. Старый пират.")
	want := "⟦figure⟧\n\n## Задачи\n\n1. Старый пират."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestATypedTextThatAlreadyNamesItselfIsLeftAlone(t *testing.T) {
	t.Parallel()
	body := "## Задачи\n\n1. Старый пират."
	if got := publisher.Titled("Задачи", body); got != body {
		t.Errorf("the title was written twice: %q", got)
	}
}

func TestAnArticleWithNoTitleKeepsItsBody(t *testing.T) {
	t.Parallel()
	if got := publisher.Titled("", "Крылья бабочки."); got != "Крылья бабочки." {
		t.Errorf("got %q", got)
	}
}
