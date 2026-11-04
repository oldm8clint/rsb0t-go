package questplan

import (
	"strings"
	"testing"
)

// buyOutTerms is a 100,000-essence want over the three stalls as the price page showed them on 13 Oct 2026 (Seers Market 298,249 at 100,
// Tradevault 1,213,889, Sil Stuff 262,411 with no price) with eleven million coins in hand.
func buyOutTerms() MarketTerms {
	return MarketTerms{Order: PipEssenceMarkets, Item: "Rune essence", Want: 100000, Coins: 11_000_000,
		Fallback: PipEssenceUnitFallback, Ceiling: PipEssenceUnitCeiling,
		Listings: []MarketStock{
			{Market: "Seers Market", Item: "Rune essence", Stock: 298249, LastSold: 100},
			{Market: "Tradevault", Item: "Rune essence", Stock: 1213889, LastSold: 100},
			{Market: "Sil Stuff", Item: "Rune essence", Stock: 262411},
		}}
}

// The first stall takes the lot, at most lotMax: a hundred-thousand want is bought in lots, not in one trade.
func TestPickBuyOutLotsAreCapped(t *testing.T) {
	pick, why := PickBuyOut(buyOutTerms(), 20000)
	if why != "" || pick != (MarketPick{Market: "Seers Market", Unit: 100, UnitCap: 125, Qty: 20000, MaxTotal: 2_500_000}) {
		t.Fatalf("%+v %q", pick, why)
	}
	// The short want is the lot.
	tm := buyOutTerms()
	tm.Want = 1500
	if pick, _ := PickBuyOut(tm, 20000); pick.Qty != 1500 {
		t.Fatalf("%+v", pick)
	}
}

// A stall that lists less than the want is bought OUT (PickMarket would pass it over): the lot is what it lists.
func TestPickBuyOutTakesAShortStallWhole(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings[0].Stock = 13567 // Seers Market on 28 Sep
	pick, _ := PickBuyOut(tm, 20000)
	if pick.Market != "Seers Market" || pick.Qty != 13567 {
		t.Fatalf("the stall's 13,567 are the lot: %+v", pick)
	}
	// PickMarket, asked for the same, passes it over: that is the difference.
	short := buyOutTerms()
	short.Listings[0].Stock = 13567
	short.Want = 20000
	if p, _ := PickMarket(short); p.Market != "Tradevault" {
		t.Fatalf("PickMarket's rule is to pass a short stall over: %+v", p)
	}
	// Once it is sold out (the caller skips it), the next stall in the owner's order takes the rest.
	tm.Skip = map[string]bool{"Seers Market": true}
	if pick, _ := PickBuyOut(tm, 20000); pick.Market != "Tradevault" || pick.Qty != 20000 {
		t.Fatalf("%+v", pick)
	}
	tm.Skip["Tradevault"] = true
	if pick, _ := PickBuyOut(tm, 20000); pick.Market != "Sil Stuff" || pick.Unit != PipEssenceUnitFallback || pick.UnitCap != 125 {
		t.Fatalf("Sil Stuff lists stock with no price: the fallback, capped: %+v", pick)
	}
}

func TestPickBuyOutSpendsEveryCoin(t *testing.T) {
	tm := buyOutTerms()
	tm.Coins = 1_000_000 // 8,000 at the 125 cap
	pick, _ := PickBuyOut(tm, 20000)
	if pick.Qty != 8000 || pick.MaxTotal != 1_000_000 {
		t.Fatalf("%+v", pick)
	}
	tm.Coins = 100
	if pick, why := PickBuyOut(tm, 20000); pick.Market != "" || !strings.Contains(why, "do not pay for one") {
		t.Fatalf("%+v %q", pick, why)
	}
}

func TestPickBuyOutPassesOverWhatCannotSell(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings[0].LastSold = 400 // over the ceiling
	tm.Listings[1].Item = "Rune axe"
	pick, why := PickBuyOut(tm, 20000)
	if pick.Market != "Sil Stuff" {
		t.Fatalf("the first two cannot sell: %+v %q", pick, why)
	}
	tm.Skip = map[string]bool{"Sil Stuff": true}
	_, why = PickBuyOut(tm, 20000)
	for _, want := range []string{"Seers Market: last sold at 400 each, over the ceiling of 150", "Tradevault: its page lists no rune essence", "Sil Stuff: asked already"} {
		if !strings.Contains(why, want) {
			t.Errorf("%q lacks %q", why, want)
		}
	}
	// Nothing to buy, no lot size, a price that makes no sense.
	tm = buyOutTerms()
	tm.Want = 0
	if p, _ := PickBuyOut(tm, 20000); p.Market != "" {
		t.Fatalf("%+v", p)
	}
	if p, _ := PickBuyOut(buyOutTerms(), 0); p.Market != "" {
		t.Fatalf("%+v", p)
	}
	tm = buyOutTerms()
	tm.Fallback = 200
	if _, why := PickBuyOut(tm, 20000); !strings.Contains(why, "no sane price") {
		t.Fatal(why)
	}
	tm = buyOutTerms()
	tm.Order = nil
	if _, why := PickBuyOut(tm, 20000); !strings.Contains(why, "no stalls") {
		t.Fatal(why)
	}
}

// With no price page at all every stall is asked at the fallback for the whole lot.
func TestPickBuyOutWithoutAPage(t *testing.T) {
	tm := buyOutTerms()
	tm.Listings = nil
	pick, _ := PickBuyOut(tm, 20000)
	if pick.Market != "Seers Market" || pick.Unit != PipEssenceUnitFallback || pick.Qty != 20000 {
		t.Fatalf("%+v", pick)
	}
}
