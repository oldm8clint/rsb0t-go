package essenceplan

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
)

func TestBuyOutConstantsAgreeWithTheirOwners(t *testing.T) {
	if BuyOutPriceHint != questplan.PipEssenceUnitFallback {
		t.Errorf("the price hint %d is not the market source's fallback %d", BuyOutPriceHint, questplan.PipEssenceUnitFallback)
	}
	if MarketUnitCeiling != questplan.PipEssenceUnitCeiling {
		t.Errorf("the ceiling %d is not the market source's %d", MarketUnitCeiling, questplan.PipEssenceUnitCeiling)
	}
	if BuyOutDefaultFloor != runecraftplan.DefaultMuleCoinFloor {
		t.Errorf("the buy-out's floor %d is not the campaign setting's default %d", BuyOutDefaultFloor, runecraftplan.DefaultMuleCoinFloor)
	}
	// A lot is under every stall's per-trade limit even at the ceiling (Seers Market takes about 5,000,000).
	if BuyOutLot*MarketUnitCeiling > 5_000_000 {
		t.Errorf("a lot of %d at %d is %d coins, over a stall's trade limit", BuyOutLot, MarketUnitCeiling, BuyOutLot*MarketUnitCeiling)
	}
}

// The live case of 14 Oct 2026: the mule held 11,229,284 coins and the campaign lacks 100,000 essence. The owner: "request mules entire
// cash stack and spend it on everything instead of just running out".
func TestPlanBuyOutAsksTheWholeStack(t *testing.T) {
	p, why := PlanBuyOut(BuyOutTerms{Want: 100000, MuleKnown: true, MuleCoins: 11_229_284, Floor: BuyOutDefaultFloor, OnMarketWorld: true})
	if why != "" {
		t.Fatal(why)
	}
	if p.Ask != 11_219_284 || p.Whole != 11_219_284 || p.Trips != 1 {
		t.Fatalf("the whole stack less the floor in one trip: %+v", p)
	}
	// 11.2M buys 112,000 at the stalls' 100: the want caps what is looked for.
	if p.Qty != 100000 || p.MaxCoins != p.Ask {
		t.Fatalf("%+v", p)
	}
}

// A small want asks only for what it needs at the worst price, not for the whole stack: the stack is the reach, not the bill.
func TestPlanBuyOutAsksOnlyForTheWant(t *testing.T) {
	p, why := PlanBuyOut(BuyOutTerms{Want: 8000, MuleKnown: true, MuleCoins: 11_229_284, Floor: BuyOutDefaultFloor, OnMarketWorld: true})
	if why != "" || p.Ask != 8000*MarketUnitCeiling || p.Qty != 8000 {
		t.Fatalf("%+v %q", p, why)
	}
	// A poorer mule is asked for what it has beyond the floor, and the quantity follows the coins.
	p, why = PlanBuyOut(BuyOutTerms{Want: 100000, MuleKnown: true, MuleCoins: 773_226, Floor: BuyOutDefaultFloor, OnMarketWorld: true})
	if why != "" || p.Ask != 763_226 || p.Qty != 7632 {
		t.Fatalf("773,226 coins less the floor: %+v %q", p, why)
	}
}

func TestPlanBuyOutTrips(t *testing.T) {
	terms := BuyOutTerms{Want: 100000, MuleKnown: true, MuleCoins: 11_229_284, Floor: BuyOutDefaultFloor, OnMarketWorld: true, Trip: 3_000_000}
	p, why := PlanBuyOut(terms)
	if why != "" || p.Ask != 3_000_000 || p.Qty != 30000 || p.Trips != 4 {
		t.Fatalf("a 3,000,000 trip: %+v %q", p, why)
	}
	// The second trip: what was asked is counted against the (laggy) published stack, and what is held is counted in the funds.
	terms.Asked, terms.Held, terms.Want = 3_000_000, 250_000, 70_000
	p, why = PlanBuyOut(terms)
	if why != "" || p.Ask != 3_000_000 || p.MaxCoins != 3_250_000 || p.Qty != 32500 {
		t.Fatalf("%+v %q", p, why)
	}
	// The last trip asks for what is left of the stack, not a whole trip.
	terms.Asked, terms.Want = 10_000_000, 12_000
	p, why = PlanBuyOut(terms)
	if why != "" || p.Ask != 1_219_284 {
		t.Fatalf("%+v %q", p, why)
	}
}

func TestPlanBuyOutRefusals(t *testing.T) {
	ok := BuyOutTerms{Want: 100000, MuleKnown: true, MuleCoins: 11_229_284, Floor: BuyOutDefaultFloor, OnMarketWorld: true}
	for name, c := range map[string]struct {
		mod func(t *BuyOutTerms)
		why string
	}{
		"nothing short":        {func(t *BuyOutTerms) { t.Want = 0 }, "no essence is short"},
		"another world":        {func(t *BuyOutTerms) { t.OnMarketWorld = false }, "do not stand on this world"},
		"the mule unpublished": {func(t *BuyOutTerms) { t.MuleKnown = false }, "not published"},
		"the mule under floor": {func(t *BuyOutTerms) { t.MuleCoins = 9000 }, "not above its 10000 floor"},
		"all of it asked":      {func(t *BuyOutTerms) { t.Asked = 11_219_284 }, "not above its"},
		"too few to go":        {func(t *BuyOutTerms) { t.MuleCoins = 50_000 }, "under the 1000 worth a trip"},
	} {
		x := ok
		c.mod(&x)
		if _, why := PlanBuyOut(x); why == "" || !strings.Contains(why, c.why) {
			t.Errorf("%s: %q lacks %q", name, why, c.why)
		}
	}
	// A want under the minimum is still worth a trip when the coins buy all of it.
	if p, why := PlanBuyOut(BuyOutTerms{Want: 300, MuleKnown: true, MuleCoins: 1_000_000, Floor: 0, OnMarketWorld: true}); why != "" || p.Qty != 300 {
		t.Fatalf("%+v %q", p, why)
	}
	// Coins already in hand count: a leftover and a small ask together buy the lot.
	if p, why := PlanBuyOut(BuyOutTerms{Want: 5000, MuleKnown: true, MuleCoins: 40_000, Floor: BuyOutDefaultFloor, Held: 480_000, OnMarketWorld: true}); why != "" || p.Qty != 5000 {
		t.Fatalf("%+v %q", p, why)
	}
}
