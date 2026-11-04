package fleetplan

import (
	"testing"
)

// seersListings0922 are the example.invalid/market/inventory rows read 2026-09-22 (chaos, mind).
var seersListings0922 = []MarketListing{
	{Market: "Seers Market", Item: "Chaos rune", Stock: 732139, LastSold: 95, LastBought: 80},
	{Market: "Seers Market", Item: "Mind rune", Stock: 15, LastSold: 26},
	{Market: "Constructor", Item: "Chaos rune", Stock: 11167},
	{Market: "Edge Market", Item: "Chaos rune", Stock: 119},
}

func TestParseMarketQuote(t *testing.T) {
	cases := []struct {
		line, item string
		ok         bool
		qty, unit  int
	}{
		{"Seers Market: 150 X chaos rune = 14,250gp (95ea). Give me a moment.", "Chaos rune", true, 150, 95},
		{"Seers market: 150 x chaos rune = 14,250gp (95ea). give me a moment. ", "Chaos rune", true, 150, 95},
		{"Seers Market: 1 X rune axe = 150,000gp (150,000ea). Give me a moment.", "Rune axe", true, 1, 150000},
		{"Seers Market: 150 X chaos runes = 14,250gp (95ea).", "Chaos rune", true, 150, 95},
		{"Seers Market: 150 X death rune = 30,000gp (200ea).", "Chaos rune", false, 0, 0},
		{"Seers Market: Got your chaos rune. Trade me, rsu1.", "Chaos rune", false, 0, 0},
	}
	for _, c := range cases {
		q, ok := ParseMarketQuote(c.line, c.item)
		if ok != c.ok || (ok && (q.Qty != c.qty || q.Unit != c.unit)) {
			t.Errorf("%q: %+v %v", c.line, q, ok)
		}
	}
}

func TestDecideRequote(t *testing.T) {
	q := MarketQuote{Qty: 150, Item: "chaos rune", Total: 15000, Unit: 100}
	if d := DecideRequote(95, q, true, 150, 150); !d.Retry || d.Unit != 100 {
		t.Fatalf("a quote under the cap: %+v", d)
	}
	if d := DecideRequote(95, q, true, 150, 99); d.Retry {
		t.Fatal("retried over the cap")
	}
	if d := DecideRequote(100, q, true, 150, 150); d.Retry {
		t.Fatal("retried at the price already offered")
	}
	if d := DecideRequote(95, q, false, 150, 150); d.Retry {
		t.Fatal("retried without a quote")
	}
	if d := DecideRequote(95, q, true, 100, 150); d.Retry {
		t.Fatal("retried on a quote for a different quantity")
	}
}
