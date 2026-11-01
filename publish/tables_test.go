package publish

import (
	"strings"
	"testing"
)

func TestATranslatedTableStillRenders(t *testing.T) {
	// The vi translation of 1970 №2 «Что такое график функции» came back with
	// em dashes for the delimiter and one cell short of the header.
	body := "| | Thứ Hai | Thứ Ba |\n| :— | :— |\n| Voloda | | |\n"
	got := mendTables(body)
	if !strings.Contains(got, "\n| :--- | :--- | --- |\n") {
		t.Fatalf("delimiter not mended: %q", got)
	}
	r, err := NewRenderer()
	if err != nil {
		t.Skip(err)
	}
	html, _, err := r.Render(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<table>") {
		t.Fatalf("no table rendered: %s", html)
	}
}

func TestAnOrdinaryRuleIsLeftAlone(t *testing.T) {
	body := "Текст.\n\n---\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"
	if got := mendTables(body); !strings.Contains(got, "\n---\n") {
		t.Fatalf("a thematic break was touched: %q", got)
	}
}
