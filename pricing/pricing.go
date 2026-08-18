// Package pricing turns the tokens a run reported into money.
//
// The rates are not a table in this repo. They live in manifests/pricing.yaml
// beside the rest of the corpus metadata, each row carrying where it was copied
// from and the day it was copied, and a model with no row costs an unknown
// amount rather than nothing. A price list compiled into a program is out of
// date within a quarter and goes on printing authoritative looking totals
// afterwards, which is worse than printing none at all.
//
// The file looks like this:
//
//	retrieved: "2026-08-18"
//	source: https://developers.openai.com/api/docs/pricing
//	currency: USD
//	rates:
//	  - model: gpt-5
//	    input: 1.25
//	    cached_input: 0.125
//	    output: 10
//	  - route: gamingpc
//	    free: true
//	    note: a card in the next room, which bills electricity and not tokens
//
// Free is not the same as unpriced and the difference is the reason this is a
// field. Three of the four lanes this project runs on bill nothing per token: a
// browser session is a subscription, a card in the next room is electricity, and
// a local program is neither. Their totals are zero and known. A model nobody
// has written a row for has a total that is simply not known, and a report that
// showed the two the same way would be claiming a run was free when what
// happened is that nobody priced it.
package pricing

import (
	"fmt"
	"strings"

	"github.com/tamnd/kvant-solver/api"
)

const tokensPerMillion = 1e6

// Card is manifests/pricing.yaml.
//
// Retrieved and Source are on the card and may be repeated on any row that came
// from somewhere else. They are printed with every total, because a number of
// dollars with no date on it invites being quoted a year later.
type Card struct {
	Retrieved string  `yaml:"retrieved,omitempty"`
	Source    string  `yaml:"source,omitempty"`
	Currency  string  `yaml:"currency,omitempty"`
	Rates     []Rates `yaml:"rates"`
}

// Rates is what a million tokens cost.
type Rates struct {
	// Model is matched against what the provider said it answered on, not
	// against what was asked for. A route that quietly serves something cheaper
	// than the model requested should show up as the cheaper one.
	Model string `yaml:"model,omitempty"`
	// Route ties a row to one endpoint. The same model behind two endpoints is
	// two prices often enough that this has to be sayable: one may be a paid
	// API and the other a subscription that bills nothing per token.
	Route    string  `yaml:"route,omitempty"`
	Currency string  `yaml:"currency,omitempty"`
	Input    float64 `yaml:"input,omitempty"`
	// CachedInput is what a cached read costs. A row that leaves it out is
	// charged the full input rate for cached reads, which overstates the bill
	// for every provider that discounts them, and that is the safer direction
	// for a number somebody is budgeting against.
	CachedInput float64 `yaml:"cached_input,omitempty"`
	Output      float64 `yaml:"output,omitempty"`
	// Free says this route bills nothing per token, which is a price and not a
	// missing one.
	Free      bool   `yaml:"free,omitempty"`
	Source    string `yaml:"source,omitempty"`
	Retrieved string `yaml:"retrieved,omitempty"`
	Note      string `yaml:"note,omitempty"`
}

// Cost is what one call, or a run of them, came to.
type Cost struct {
	// Known is false where no row matched. Everything else is then zero and
	// means nothing, and a caller that adds it into a total without looking at
	// this field is publishing a total that is short by however many calls
	// nobody priced.
	Known       bool    `yaml:"known"`
	Currency    string  `yaml:"currency,omitempty"`
	Input       float64 `yaml:"input,omitempty"`
	CachedInput float64 `yaml:"cached_input,omitempty"`
	Output      float64 `yaml:"output,omitempty"`
	Total       float64 `yaml:"total,omitempty"`
}

// Of is what a usage costs at these rates.
func (r Rates) Of(u api.Usage) Cost {
	u = u.Normalized()
	if r.Free {
		return Cost{Known: true, Currency: r.Currency}
	}
	// Cached reads are billed at their own rate and the rest of the input at
	// the full one. InputTokens counts the cached ones, which is why this
	// subtracts rather than adds.
	cost := Cost{
		Known:       true,
		Currency:    r.Currency,
		Input:       money(u.InputTokens-u.CachedInputTokens, r.Input),
		CachedInput: money(u.CachedInputTokens, r.cachedRate()),
		Output:      money(u.OutputTokens, r.Output),
	}
	cost.Total = cost.Input + cost.CachedInput + cost.Output
	return cost
}

// cachedRate falls back to the full input rate, which overcharges a cache read
// on every provider that discounts one. A row that meant free had to say so.
func (r Rates) cachedRate() float64 {
	if r.CachedInput > 0 {
		return r.CachedInput
	}
	return r.Input
}

func money(tokens int, perMillion float64) float64 {
	return float64(max(tokens, 0)) * perMillion / tokensPerMillion
}

// Add sums two costs.
//
// An unknown cost adds nothing and does not make the total unknown, because one
// unpriced call among two hundred should not blank out the bill. What it must
// not do is disappear, and the count of calls nobody could price is kept beside
// the total by the Account rather than in here.
func (c Cost) Add(other Cost) Cost {
	if !other.Known {
		return c
	}
	if !c.Known {
		return other
	}
	if c.Currency != other.Currency {
		// Two currencies in one column is a number nobody can read, and
		// converting them would need a rate this program has no business
		// guessing at.
		c.Currency = "mixed"
	}
	c.Input += other.Input
	c.CachedInput += other.CachedInput
	c.Output += other.Output
	c.Total += other.Total
	return c
}

// String is the money as it appears in a table, or a dash where nobody set a
// price. A dash rather than 0.00, which would read as free.
func (c Cost) String() string {
	if !c.Known {
		return "-"
	}
	if c.Currency == "" {
		return fmt.Sprintf("%.4f", c.Total)
	}
	return fmt.Sprintf("%.4f %s", c.Total, c.Currency)
}

// For finds the rates for a call that a route answered on a model.
//
// Four passes, most specific first: the row naming both, then the row naming
// only this route, then the row naming exactly this model, then the row whose
// model is the longest prefix of it. The prefix pass is what makes gpt-5 cover
// gpt-5-2026-04-01 without a row per dated snapshot, and it matches on a
// boundary so that gpt-5 does not price gpt-51.
func (c *Card) For(route, model string) (Rates, bool) {
	if c == nil {
		return Rates{}, false
	}
	route, model = fold(route), fold(model)
	for _, want := range []func(Rates) bool{
		func(r Rates) bool { return r.Route != "" && fold(r.Route) == route && fold(r.Model) == model },
		func(r Rates) bool { return r.Route != "" && fold(r.Route) == route && r.Model == "" },
		func(r Rates) bool { return r.Route == "" && r.Model != "" && fold(r.Model) == model },
	} {
		for _, row := range c.Rates {
			if want(row) {
				return c.fill(row), true
			}
		}
	}
	var best Rates
	longest := 0
	for _, row := range c.Rates {
		name := fold(row.Model)
		if row.Route != "" || name == "" || len(name) <= longest {
			continue
		}
		if strings.HasPrefix(model, name+"-") || strings.HasPrefix(model, name+":") {
			best, longest = row, len(name)
		}
	}
	if longest == 0 {
		return Rates{}, false
	}
	return c.fill(best), true
}

// fill puts the card's defaults on a row that did not say.
func (c *Card) fill(r Rates) Rates {
	if r.Currency == "" {
		r.Currency = c.Currency
	}
	if r.Source == "" {
		r.Source = c.Source
	}
	if r.Retrieved == "" {
		r.Retrieved = c.Retrieved
	}
	return r
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Calculate is what one call cost, or an unknown cost where the card has no row
// for it.
func (c *Card) Calculate(route, model string, u api.Usage) Cost {
	rates, ok := c.For(route, model)
	if !ok {
		return Cost{}
	}
	return rates.Of(u)
}
