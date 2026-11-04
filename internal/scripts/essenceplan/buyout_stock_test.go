package essenceplan

import (
	"strings"
	"testing"
	"time"
)

// The buy-out looks for no more than the stalls list, and asks the mule for no more coins than that costs at the worst price: the
// stack is the reach, the page's stock is the bill. (Review, 14 Oct 2026: the whole stack must not go on the road for stalls with
// nothing on them.)
func TestPlanBuyOutIsCappedByWhatTheStallsList(t *testing.T) {
	terms := BuyOutTerms{Want: 99900, MuleKnown: true, MuleCoins: 11_229_284, Floor: BuyOutDefaultFloor, OnMarketWorld: true}
	p, why := PlanBuyOut(terms)
	if why != "" || p.Ask != 11_219_284 || p.Qty != 99900 {
		t.Fatalf("unknown stock: the whole stack as before: %+v %q", p, why)
	}
	terms.Listed = 60_000
	p, why = PlanBuyOut(terms)
	if why != "" || p.Qty != 60_000 || p.Ask != 60_000*MarketUnitCeiling || p.MaxCoins != p.Ask {
		t.Fatalf("60,000 listed: %+v %q", p, why)
	}
	// Listed more than the want changes nothing.
	terms.Listed = 400_000
	if p, why = PlanBuyOut(terms); why != "" || p.Qty != 99900 || p.Ask != 11_219_284 {
		t.Fatalf("%+v %q", p, why)
	}
	// Under the least worth a trip: no buy-out, and the page's figure is said.
	terms.Listed = 800
	if _, why = PlanBuyOut(terms); !strings.Contains(why, "the stalls list only 800 essence in all, under the 1000 worth a trip") {
		t.Fatalf("%q", why)
	}
	// A want smaller than the minimum is still worth it when the stalls list all of it.
	small := BuyOutTerms{Want: 300, Listed: 300, MuleKnown: true, MuleCoins: 1_000_000, OnMarketWorld: true}
	if p, why := PlanBuyOut(small); why != "" || p.Qty != 300 {
		t.Fatalf("%+v %q", p, why)
	}
	// Trips follow the capped want: 60,000 at the ceiling is 9,000,000 coins, three trips of three million.
	terms.Listed, terms.Trip = 60_000, 3_000_000
	if p, why = PlanBuyOut(terms); why != "" || p.Trips != 3 || p.Ask != 3_000_000 {
		t.Fatalf("%+v %q", p, why)
	}
}

func TestBuyOutShort(t *testing.T) {
	for _, c := range []struct {
		bought, want int
		short        bool
	}{
		{0, 99900, true}, {999, 99900, true}, {1000, 99900, false}, {7000, 99900, false},
		{299, 300, true}, {300, 300, false}, // a want under the minimum is met by buying all of it
		{0, 0, false}, // nothing was wanted: nothing came up short
	} {
		if got := BuyOutShort(c.bought, c.want); got != c.short {
			t.Errorf("bought %d of %d: short %v, want %v", c.bought, c.want, got, c.short)
		}
	}
	if BuyOutNoStockWait < time.Hour || BuyOutNoStockWait > 6*time.Hour || MaxDryRefills != 1 {
		t.Errorf("a few hours and one refill: %v %d", BuyOutNoStockWait, MaxDryRefills)
	}
}
