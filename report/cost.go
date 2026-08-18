package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/ocr"
)

// Spend is what one year of reading took.
type Spend struct {
	Year int
	// Attempts is every page read, accepted or not. Pages is the ones that were
	// accepted, which is what ended up in the corpus. The gap between the two is
	// the waste, and it is the number worth watching: the first run of 1975 №1
	// at temperature 1.0 spent 83 attempts to keep one page.
	Attempts int
	Pages    int
	Seconds  float64
	Usage    api.Usage
	// Metered is how many attempts came with token counts. A local program
	// prints Markdown and exits with nothing to read the numbers off, so a year
	// read that way has a real cost in time and none in tokens, and a report
	// that did not separate the two would report it as free.
	Metered int
	Engines []string
}

// Waste is the attempts that bought nothing.
func (s Spend) Waste() int { return s.Attempts - s.Pages }

// Cost rolls a ledger up per year, in year order.
func Cost(entries []ocr.Entry) []Spend {
	byYear := map[int]*Spend{}
	engines := map[int]map[string]bool{}
	for _, entry := range entries {
		// The year comes off the page the line is about and not off the field it
		// was written with, which is what heals the lines a repair run mislabelled.
		year := entry.PageYear()
		spend := byYear[year]
		if spend == nil {
			spend = &Spend{Year: year}
			byYear[year] = spend
			engines[year] = map[string]bool{}
		}
		spend.Attempts++
		if entry.OK {
			spend.Pages++
		}
		spend.Seconds += entry.Seconds
		if entry.Usage.TotalTokens > 0 {
			spend.Metered++
			spend.Usage = spend.Usage.Add(entry.Usage)
		}
		if entry.Engine != "" {
			engines[year][entry.Engine] = true
		}
	}
	out := make([]Spend, 0, len(byYear))
	for year, spend := range byYear {
		for engine := range engines[year] {
			spend.Engines = append(spend.Engines, engine)
		}
		sort.Strings(spend.Engines)
		out = append(out, *spend)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Year < out[b].Year })
	return out
}

// Total adds the years up.
func Total(spends []Spend) Spend {
	all := Spend{}
	seen := map[string]bool{}
	for _, spend := range spends {
		all.Attempts += spend.Attempts
		all.Pages += spend.Pages
		all.Seconds += spend.Seconds
		all.Metered += spend.Metered
		all.Usage = all.Usage.Add(spend.Usage)
		for _, engine := range spend.Engines {
			if !seen[engine] {
				seen[engine] = true
				all.Engines = append(all.Engines, engine)
			}
		}
	}
	sort.Strings(all.Engines)
	return all
}

// Price is what a provider charges, in units of currency per million tokens.
//
// It is a flag and not a table in the repo on purpose. Three of the four lanes
// bill nothing per token: a browser session is a subscription, a card in the
// next room is electricity, and a local program is neither. Only one lane has a
// price at all, it changes without notice, and a stale table in the source
// would produce numbers that look authoritative and are wrong. Whoever wants
// money in the report passes today's rate.
type Price struct {
	Input  float64
	Output float64
	// Currency is only a label for the report. Nothing converts.
	Currency string
}

// Zero is a price nobody set, and it is what turns the money column off.
func (p Price) Zero() bool { return p.Input == 0 && p.Output == 0 }

// Of is what a usage costs. Cached input is charged at the full input rate,
// which overstates it for the providers that discount a cache read, and that is
// the safer direction for a number somebody is budgeting against.
func (p Price) Of(u api.Usage) float64 {
	return float64(u.InputTokens)*p.Input/1e6 + float64(u.OutputTokens)*p.Output/1e6
}

func (p Price) currency() string {
	if p.Currency == "" {
		return "USD"
	}
	return p.Currency
}

// CostTable is the per year report, printed at the end of a run or asked for
// later.
func CostTable(spends []Spend, price Price) string {
	// The year is printed as a string so that the total line, which is called
	// all, goes through exactly the same format as the years above it.
	const format = "%-6s  %9s  %9s  %10s  %12s  %12s  %10s"

	var out strings.Builder
	line := func(cells ...string) {
		if !price.Zero() {
			fmt.Fprintf(&out, format+"  %10s\n", toAny(cells)...)
			return
		}
		fmt.Fprintf(&out, format+"\n", toAny(cells[:7])...)
	}
	line("year", "pages", "attempts", "time", "input", "output", "a call", price.currency())

	row := func(name string, spend Spend) {
		line(name,
			fmt.Sprint(spend.Pages), fmt.Sprint(spend.Attempts), clock(spend.Seconds),
			fmt.Sprint(spend.Usage.InputTokens), fmt.Sprint(spend.Usage.OutputTokens),
			perCall(spend), fmt.Sprintf("%.2f", price.Of(spend.Usage)))
	}
	for _, spend := range spends {
		row(fmt.Sprint(spend.Year), spend)
	}
	if len(spends) > 1 {
		row("all", Total(spends))
	}
	return out.String()
}

// toAny is the one thing Printf will not do for a slice of strings.
func toAny(cells []string) []any {
	out := make([]any, len(cells))
	for i, cell := range cells {
		out[i] = cell
	}
	return out
}

// perCall is how long one call took on average.
//
// It is a call and not a page, and the difference matters when a run is planned
// from it. Six workers against one card give 19 seconds a call and 3 seconds a
// page, because the card is batching them; dividing this column by the workers
// is the throughput and this column is the latency. The time column beside it is
// the same warning, being the sum over the workers rather than the clock on the
// wall.
func perCall(spend Spend) string {
	if spend.Attempts == 0 {
		return "-"
	}
	return clock(spend.Seconds / float64(spend.Attempts))
}

// clock is a number of seconds somebody can read. Hours for a year of work,
// seconds for a page.
func clock(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.1f h", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.1f m", d.Minutes())
	default:
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
}

// CostMarkdown is the same numbers as a document, for the milestone's report
// directory.
func CostMarkdown(spends []Spend, price Price, generated time.Time) string {
	var out strings.Builder
	out.WriteString("# What the reading cost\n\n")
	fmt.Fprintf(&out, "One row per year, counting every attempt and not only the pages that were kept.\n")
	fmt.Fprintf(&out, "Generated %s by kvant report cost.\n\n", generated.UTC().Format("2006-01-02"))
	if len(spends) == 0 {
		out.WriteString("The ledger is empty, so nothing has been read on this machine.\n")
		return out.String()
	}

	out.WriteString("| year | pages | attempts | wasted | time | input tokens | output tokens | a call | engines |\n")
	out.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	write := func(name string, spend Spend) {
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %s | %d | %d | %s | %s |\n",
			name, spend.Pages, spend.Attempts, spend.Waste(), clock(spend.Seconds),
			spend.Usage.InputTokens, spend.Usage.OutputTokens, perCall(spend),
			strings.Join(spend.Engines, ", "))
	}
	for _, spend := range spends {
		write(fmt.Sprint(spend.Year), spend)
	}
	all := Total(spends)
	if len(spends) > 1 {
		write("all", all)
	}

	out.WriteString("\n")
	if !price.Zero() {
		fmt.Fprintf(&out, "At %.2f per million input tokens and %.2f per million output tokens, that is %.2f %s.\n\n",
			price.Input, price.Output, price.Of(all.Usage), price.currency())
	}
	if all.Metered < all.Attempts {
		fmt.Fprintf(&out, "%d of %d attempts came with no token counts at all. Those are the lanes that are a program on a box rather than an endpoint, and their cost is the time column and the electricity behind it.\n",
			all.Attempts-all.Metered, all.Attempts)
	}
	return out.String()
}
