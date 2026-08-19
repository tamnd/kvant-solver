package ocr_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

// A rule that can kill a page has to be a rule the report can name.
//
// The two halves of that live apart. Validate emits a Rule, and the failure
// report turns a stored reason back into a Rule by looking it up in AllRules,
// so a rule missing from that list is invisible to the report rather than
// merely uncounted. Rule 9 was left off when it was written and every page it
// killed came back classed "no rule", which the report glosses as the service
// failing rather than the page. Fifty eight pages of the Soviet decades sat
// under an outage that never happened, and nothing failed to say so.
//
// So the assertion is not that AllRules has nine entries, which the next rule
// makes stale. It is that a rule observed rejecting a page round trips through
// the report's own lookup.
func TestEveryRuleThatKillsAPageCanBeNamed(t *testing.T) {
	cases := []struct {
		name string
		rule ocr.Rule
		text string
	}{
		{
			"short", ocr.RuleShort,
			"⟦folio 2⟧\n\nСлишком мало.\n",
		},
		{
			"math", ocr.RuleMath,
			page(2) + "\n\nЗдесь формула $x^2+y^2 без закрывающего знака, и текст идёт дальше.\n",
		},
		{
			"script", ocr.RuleScript,
			page(2) + strings.Repeat(
				"\nПри dvижении тележки надо найти Funcцию, которая описывает teхнику "+
					"и её svойства, а затем проверить otvет по таблице.\n", 1),
		},
		{
			"runaway, by the length of one word", ocr.RuleRunaway,
			page(2) + "\n\n" + strings.Repeat("г", 900) + "\n",
		},
		{
			"runaway, by the length of the page", ocr.RuleRunaway,
			page(2) + "\n\n" + strings.Repeat("поверхности ", 1500) + "\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fired ocr.Rule
			for _, p := range ocr.Validate(c.text, body(t), ocr.Options{}) {
				if p.Rule == c.rule {
					fired = p.Rule
				}
			}
			if fired == "" {
				t.Fatalf("the page did not trip %s, so this case is not testing what it means to", c.rule)
			}
			if !slices.Contains(ocr.AllRules, c.rule) {
				t.Fatalf("%s rejected a page and is not in AllRules, so the failure report will class it \"no rule\"", c.rule)
			}
			// The report stores the sentence, not the rule, and reads the rule back
			// out of it. That round trip is where rule 9 was lost.
			reason := string(c.rule) + ": whatever the rule said about it"
			if got := ocr.ParseRules(reason); !slices.Contains(got, c.rule) {
				t.Errorf("ParseRules(%q) = %v, and %s is not in it", reason, got, c.rule)
			}
		})
	}
}
