package pricing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tamnd/kvant-solver/api"
)

func card() *Card {
	return &Card{
		Retrieved: "2026-08-18",
		Source:    "https://example.invalid/pricing",
		Currency:  "USD",
		Rates: []Rates{
			{Model: "gpt-5", Input: 1.25, CachedInput: 0.125, Output: 10},
			{Model: "gpt-5-mini", Input: 0.25, Output: 2},
			{Route: "gamingpc", Free: true, Note: "a card in the next room"},
			{Route: "server3", Model: "gpt-5", Free: true, Note: "a session somebody already pays for"},
		},
	}
}

func TestACachedReadIsChargedAtItsOwnRate(t *testing.T) {
	// Six hundred tokens at the full rate and four hundred at the discounted
	// one. Charging all thousand at the full rate is the mistake worth a test:
	// it is invisible in a small run and is most of the bill in a long one,
	// where nearly every call re-sends the same prompt.
	cost := card().Calculate("", "gpt-5", api.Usage{InputTokens: 1000, CachedInputTokens: 400, OutputTokens: 200})
	if !cost.Known {
		t.Fatal("gpt-5 is on the card and came back unpriced")
	}
	want := 600*1.25/1e6 + 400*0.125/1e6 + 200*10/1e6
	if !near(cost.Total, want) {
		t.Fatalf("total is %v, want %v", cost.Total, want)
	}
	if cost.Currency != "USD" {
		t.Fatalf("currency is %q, the row should have taken the card's", cost.Currency)
	}
}

func TestARowWithNoCachedRateChargesTheFullInputRate(t *testing.T) {
	// Overstating the bill is the safe direction for a number somebody is
	// budgeting against, and the alternative is charging a cache read nothing
	// because the row was silent.
	cost := card().Calculate("", "gpt-5-mini", api.Usage{InputTokens: 1000, CachedInputTokens: 900})
	if want := 1000 * 0.25 / 1e6; !near(cost.Total, want) {
		t.Fatalf("total is %v, want %v", cost.Total, want)
	}
}

func TestAFreeRouteAndAnUnpricedOneAreNotTheSameThing(t *testing.T) {
	// A subscription bills nothing per token and that is a price. A model
	// nobody wrote a row for has no price at all, and showing the two the same
	// way would report a run as free when what happened is nobody priced it.
	free := card().Calculate("gamingpc", "qwen3-vl", api.Usage{InputTokens: 9000, OutputTokens: 4000})
	if !free.Known || free.Total != 0 {
		t.Fatalf("the free route came out %+v", free)
	}
	unknown := card().Calculate("hetzner", "llama-9", api.Usage{InputTokens: 9000, OutputTokens: 4000})
	if unknown.Known {
		t.Fatalf("a model with no row was priced at %+v", unknown)
	}
	if got := unknown.String(); got != "-" {
		t.Fatalf("an unpriced call prints as %q, which reads as free", got)
	}
}

func TestTheMostSpecificRowWins(t *testing.T) {
	c := card()
	// The same model on two routes is two prices, which is the case the route
	// column exists for: one is a paid API and the other is a session somebody
	// is already paying a flat fee for.
	if rates, _ := c.For("server3", "gpt-5"); !rates.Free {
		t.Fatalf("the route row lost to the model row: %+v", rates)
	}
	if rates, _ := c.For("openai", "gpt-5"); rates.Input != 1.25 {
		t.Fatalf("the model row did not apply on an unnamed route: %+v", rates)
	}
	// A dated snapshot is the same model at the same price, and a row per
	// snapshot would be out of date the week it was written.
	if rates, ok := c.For("", "gpt-5-2026-04-01"); !ok || rates.Input != 1.25 {
		t.Fatalf("a dated snapshot of gpt-5 came out %+v", rates)
	}
	// The longer prefix, not the first one that matched.
	if rates, ok := c.For("", "gpt-5-mini-2026-04-01"); !ok || rates.Input != 0.25 {
		t.Fatalf("gpt-5-mini was priced as gpt-5: %+v", rates)
	}
	// And a prefix that is not a name boundary is a different model.
	if rates, ok := c.For("", "gpt-51"); ok {
		t.Fatalf("gpt-51 was priced as gpt-5: %+v", rates)
	}
}

func TestAnUnpricedCallIsCountedRatherThanQuietlyDropped(t *testing.T) {
	// The tokens are complete and the money is short, and the report has to say
	// so. A total that is quietly missing a route is worse than no total.
	a := New(card())
	a.Record("openai", "gpt-5", api.Usage{InputTokens: 1000, OutputTokens: 500})
	a.Record("hetzner", "llama-9", api.Usage{InputTokens: 2000, OutputTokens: 700})
	total := a.Total()
	if total.Calls != 2 || total.Unpriced != 1 {
		t.Fatalf("total came out %+v", total)
	}
	if total.Usage.InputTokens != 3000 || total.Usage.OutputTokens != 1200 {
		t.Fatalf("the tokens are not complete: %+v", total.Usage)
	}
	report := a.Report()
	for _, want := range []string{"llama-9", "1 of the 2 calls are not in the total", "2026-08-18"} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report does not say %q:\n%s", want, report)
		}
	}
}

func TestEachRouteIsBilledOnItsOwnRow(t *testing.T) {
	// A run that failed over onto a second host is the reason this is per route
	// and not one number. The second host is usually serving something else,
	// and a single total hides both facts.
	a := New(card())
	a.Record("openai", "gpt-5", api.Usage{InputTokens: 100000, OutputTokens: 20000})
	a.Record("server3", "gpt-5", api.Usage{InputTokens: 400000, OutputTokens: 90000})
	a.Record("openai", "gpt-5-mini", api.Usage{InputTokens: 1000, OutputTokens: 300})
	lines := a.Lines()
	if len(lines) != 3 {
		t.Fatalf("%d lines, want one per route and model", len(lines))
	}
	if lines[0].Route != "openai" || lines[0].Model != "gpt-5" {
		t.Fatalf("the dearest line is %s on %s, want the paid gpt-5 first", lines[0].Model, lines[0].Route)
	}
	// The busiest route by tokens is the one that costs nothing, which is the
	// whole point of reading this table rather than the token count.
	if total := a.Total(); total.Route != "mixed" || total.Model != "mixed" {
		t.Fatalf("the total names %s on %s rather than saying it is a mixture", total.Model, total.Route)
	}
}

func TestACallThatNamedNoRouteIsFiledAsDirect(t *testing.T) {
	a := New(nil)
	a.Record("", "gpt-5", api.Usage{InputTokens: 10, OutputTokens: 5})
	lines := a.Lines()
	if len(lines) != 1 || lines[0].Route != Direct {
		t.Fatalf("lines came out %+v", lines)
	}
	if lines[0].Unpriced != 1 {
		t.Fatal("a run with no rate card must report its calls as unpriced, not as free")
	}
}

// answering is a completer that returns what it is told to and can fail while
// still reporting tokens, which is what a provider does when it starts an answer
// and then gives up.
type answering struct {
	res api.Response
	err error
}

func (a answering) Complete(context.Context, api.Request) (api.Response, error) {
	return a.res, a.err
}

func TestAFailedCallThatBurnedTokensIsStillBilled(t *testing.T) {
	// The provider charges for these and a run that dropped them would report a
	// bill smaller than the one that turns up.
	a := New(card())
	m := &Meter{Client: answering{
		res: api.Response{Route: "openai", Model: "gpt-5", Usage: api.Usage{InputTokens: 800, OutputTokens: 4000}},
		err: errors.New("the stream ended early"),
	}, Account: a}
	if _, err := m.Complete(context.Background(), api.Request{Model: "gpt-5"}); err == nil {
		t.Fatal("the meter swallowed the error")
	}
	if total := a.Total(); total.Calls != 1 || total.Usage.OutputTokens != 4000 {
		t.Fatalf("the failed call was not booked: %+v", total)
	}
}

func TestTheMeterBooksWhatAnsweredAndNotWhatWasAskedFor(t *testing.T) {
	// A route that quietly serves something cheaper than the model asked for is
	// the reason the pool records what answered. Pricing it as the model that
	// was requested would bill a downgrade at the full rate and hide it.
	a := New(card())
	m := &Meter{Client: answering{res: api.Response{
		Route: "openai", Model: "gpt-5-mini", Usage: api.Usage{InputTokens: 1000, OutputTokens: 100},
	}}, Account: a}
	if _, err := m.Complete(context.Background(), api.Request{Model: "gpt-5"}); err != nil {
		t.Fatal(err)
	}
	lines := a.Lines()
	if len(lines) != 1 || lines[0].Model != "gpt-5-mini" {
		t.Fatalf("lines came out %+v", lines)
	}
	if want := 1000*0.25/1e6 + 100*2/1e6; !near(lines[0].Cost.Total, want) {
		t.Fatalf("cost is %v, want the mini rate %v", lines[0].Cost.Total, want)
	}
}

func TestAnEmptyResponseIsNotBookedAsACall(t *testing.T) {
	// A dead tunnel returns nothing at all, and a run against one would
	// otherwise report a hundred calls that never happened.
	a := New(card())
	m := &Meter{Client: answering{err: errors.New("connection refused")}, Account: a}
	if _, err := m.Complete(context.Background(), api.Request{Model: "gpt-5"}); err == nil {
		t.Fatal("the meter swallowed the error")
	}
	if total := a.Total(); total.Calls != 0 {
		t.Fatalf("a call that never reached a provider was booked: %+v", total)
	}
}

func TestTheAccountHoldsUpWithEveryLaneReportingAtOnce(t *testing.T) {
	// The pool runs as many calls at once as the routes will take, and they all
	// come back through here. Run with -race, which is what CI does.
	a := New(card())
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 50 {
				a.Record([]string{"openai", "server3"}[i%2], "gpt-5", api.Usage{InputTokens: 10, OutputTokens: 5})
			}
		})
	}
	wg.Wait()
	if total := a.Total(); total.Calls != 400 {
		t.Fatalf("%d calls booked, want 400", total.Calls)
	}
}

func near(a, b float64) bool { return a-b < 1e-12 && b-a < 1e-12 }
