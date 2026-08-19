package report

import (
	"testing"

	"github.com/tamnd/kvant-solver/ocr"
)

// The class column names the rule and the column beside it says what the reader
// is looking at, so a rule with no case in meaning does not print an empty cell.
// It prints the default, and the default is the sentence for a page that has no
// history at all. Rule 9 went in without a case and sixty six pages the model
// had visibly repeated itself on came out as "no attempt was recorded", which
// says the queue lost them rather than that they need a different engine.
//
// Two lists have now been missed the same way, this one and AllRules, so the
// test walks AllRules rather than naming the rules again. A tenth rule fails
// here until somebody writes the sentence for it.
func TestEveryRuleIsExplained(t *testing.T) {
	fallback := meaning("")
	for _, rule := range ocr.AllRules {
		got := meaning(string(rule))
		if got == fallback {
			t.Errorf("%s has no case in meaning, so the report explains it as %q", rule, fallback)
		}
		if got == "" {
			t.Errorf("%s explains as the empty string", rule)
		}
	}
}
