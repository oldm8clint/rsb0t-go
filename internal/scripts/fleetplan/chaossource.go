package fleetplan

import (
	"regexp"
	"strconv"
	"strings"
)

// marketQuoteRe reads the Seers stall's own quote line, as the fleet heard it live on 2026-09-21
// (woodcutrunner/market.go): "Seers Market: 1 X rune axe = 150,000gp (150,000ea). Give me a
// moment." Public chat comes back sentence-cased, so the match ignores case.
var marketQuoteRe = regexp.MustCompile(`(?i)(\d[\d,]*)\s*x\s+([a-z' ]+?)\s*=\s*(\d[\d,]*)\s*gp\s*\(\s*(\d[\d,]*)\s*ea\s*\)`)

// MarketQuote is one parsed stall quote.
type MarketQuote struct {
	Qty   int
	Item  string
	Total int
	Unit  int
}

// ParseMarketQuote reads a stall quote for `item` out of one chat line. ok is false for a line
// that is not a quote, or quotes a different item.
func ParseMarketQuote(line, item string) (MarketQuote, bool) {
	m := marketQuoteRe.FindStringSubmatch(line)
	if m == nil {
		return MarketQuote{}, false
	}
	got := strings.TrimSpace(m[2])
	if item != "" && !strings.EqualFold(got, strings.TrimSpace(item)) &&
		!strings.EqualFold(strings.TrimSuffix(got, "s"), strings.TrimSpace(item)) {
		return MarketQuote{}, false
	}
	num := func(s string) int {
		n, _ := strconv.Atoi(strings.ReplaceAll(s, ",", ""))
		return n
	}
	q := MarketQuote{Qty: num(m[1]), Item: got, Total: num(m[3]), Unit: num(m[4])}
	if q.Qty <= 0 || q.Unit <= 0 {
		return MarketQuote{}, false
	}
	return q, true
}

// RequoteDecision is whether to retry a market order at the stall's quoted unit price.
type RequoteDecision struct {
	Retry bool
	Unit  int
	Why   string
}

// DecideRequote answers "the trade failed at our price; the stall quoted another one -- try
// again at theirs?". Only when a quote for exactly this order was heard, it differs from what was
// offered, and it is at or under the cap. Never more than once per order, which is the caller's
// counter: a stall that keeps moving its price is not one to chase.
func DecideRequote(offeredUnit int, q MarketQuote, heard bool, qty, unitCap int) RequoteDecision {
	switch {
	case !heard:
		return RequoteDecision{Why: "the stall quoted no price"}
	case q.Qty != qty:
		return RequoteDecision{Why: "the stall quoted a different quantity (" + strconv.Itoa(q.Qty) + ")"}
	case q.Unit == offeredUnit:
		return RequoteDecision{Why: "the stall quoted the price already offered"}
	case unitCap > 0 && q.Unit > unitCap:
		return RequoteDecision{Why: "the stall quoted " + strconv.Itoa(q.Unit) + " each, over the cap of " + strconv.Itoa(unitCap)}
	}
	return RequoteDecision{Retry: true, Unit: q.Unit, Why: "retrying at the stall's own quote of " + strconv.Itoa(q.Unit) + " each"}
}
