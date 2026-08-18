package pricing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tamnd/kvant-solver/api"
)

// Line is one route's share of a run, on one model.
//
// Route and model together rather than either alone, because a run that fails
// over onto a second host is the case this exists for and the second host is
// often serving something else. A total per route would hide that, and a total
// per model would put two hosts on one row and make the cheap one look
// expensive.
type Line struct {
	Route string    `yaml:"route"`
	Model string    `yaml:"model,omitempty"`
	Calls int       `yaml:"calls"`
	Usage api.Usage `yaml:"usage"`
	Cost  Cost      `yaml:"cost"`
	// Unpriced is how many of these calls the card had no row for. It is on
	// the line and not worked out afterwards because the answer stops being
	// recoverable the moment the usage is added up.
	Unpriced int `yaml:"unpriced,omitempty"`
}

// Direct is the route a call came back on when nothing named one, which is what
// a run against a single endpoint with no pool in front of it looks like.
const Direct = "direct"

// Account totals the calls of a run per route.
//
// The zero value works and prices nothing, which is the honest state for a run
// with no rate card: the tokens are still counted and the money column says it
// does not know. It is safe for concurrent use because the pool runs several
// lanes at once and they all report through here.
type Account struct {
	mu    sync.Mutex
	card  *Card
	lines map[string]*Line
}

// New is an account that prices against a card. A nil card counts tokens and
// leaves the money unknown.
func New(card *Card) *Account { return &Account{card: card} }

// Card is what this account is pricing against, or nil.
func (a *Account) Card() *Card {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.card
}

// Record adds one call.
func (a *Account) Record(route, model string, u api.Usage) {
	if a == nil {
		return
	}
	if strings.TrimSpace(route) == "" {
		route = Direct
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lines == nil {
		a.lines = map[string]*Line{}
	}
	key := route + "\x00" + model
	line, ok := a.lines[key]
	if !ok {
		line = &Line{Route: route, Model: model}
		a.lines[key] = line
	}
	line.Calls++
	line.Usage = line.Usage.Add(u)
	cost := a.card.Calculate(route, model, u)
	if !cost.Known {
		line.Unpriced++
	}
	line.Cost = line.Cost.Add(cost)
}

// Lines is every route and model this run touched, dearest first and then by
// name, so that the row worth reading is the first one.
func (a *Account) Lines() []Line {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Line, 0, len(a.lines))
	for _, line := range a.lines {
		out = append(out, *line)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cost.Total != out[j].Cost.Total {
			return out[i].Cost.Total > out[j].Cost.Total
		}
		if out[i].Route != out[j].Route {
			return out[i].Route < out[j].Route
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// Total is every line added up. Route and model read mixed where more than one
// answered, rather than naming whichever sorted first.
func (a *Account) Total() Line {
	var total Line
	for _, line := range a.Lines() {
		if total.Calls == 0 {
			total.Route, total.Model = line.Route, line.Model
		} else {
			if total.Route != line.Route {
				total.Route = "mixed"
			}
			if total.Model != line.Model {
				total.Model = "mixed"
			}
		}
		total.Calls += line.Calls
		total.Unpriced += line.Unpriced
		total.Usage = total.Usage.Add(line.Usage)
		total.Cost = total.Cost.Add(line.Cost)
	}
	return total
}

// Report is the per route accounting, as a section of a scorecard.
func (a *Account) Report() string {
	lines := a.Lines()
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## What it cost\n\n")
	b.WriteString("| route | model | calls | input | cached | output | cost |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, line := range append(lines, a.Total()) {
		u := line.Usage.Normalized()
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %s |\n",
			line.Route, dash(line.Model), line.Calls,
			u.InputTokens, u.CachedInputTokens, u.OutputTokens, line.Cost)
	}
	b.WriteString("\n")
	total := a.Total()
	if a.Card() == nil {
		b.WriteString("There is no manifests/pricing.yaml in this corpus, so this run is counted " +
			"in tokens and not in money.\n\n")
	} else if total.Unpriced > 0 {
		fmt.Fprintf(&b, "%d of the %d calls are not in the total, because manifests/pricing.yaml "+
			"has no row for what answered them. The tokens above are complete and the money is "+
			"short by whatever those calls came to.\n\n", total.Unpriced, total.Calls)
	}
	if card := a.Card(); card != nil && (card.Retrieved != "" || card.Source != "") {
		b.WriteString("Priced against manifests/pricing.yaml")
		if card.Retrieved != "" {
			fmt.Fprintf(&b, ", copied on %s", card.Retrieved)
		}
		if card.Source != "" {
			fmt.Fprintf(&b, ", from %s", card.Source)
		}
		b.WriteString(". Rates move without notice, so a total is only as good as that date.\n")
	}
	return b.String()
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// Meter is a completer that records what every call through it cost.
//
// It wraps rather than living inside the solve engine because every lane in this
// program that talks to a model goes through a Completer: solving, grading,
// translating and the repair pass. Accounting inside one of them would count one
// of them.
type Meter struct {
	Client  api.Completer
	Account *Account
}

// Complete passes the call through and books it.
//
// A failed call is booked too when the provider still reported tokens. Those are
// billed the same as any other and a run that dropped them would report a bill
// smaller than the one that arrives. The route and the model come off the
// response rather than the request, because the point of the pool is that the
// caller does not know which host will answer, and a route that serves something
// other than the model asked for should be visible here.
func (m *Meter) Complete(ctx context.Context, r api.Request) (api.Response, error) {
	res, err := m.Client.Complete(ctx, r)
	u := res.Usage.Normalized()
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		model := res.Model
		if strings.TrimSpace(model) == "" {
			model = r.Model
		}
		m.Account.Record(res.Route, model, res.Usage)
	}
	return res, err
}
