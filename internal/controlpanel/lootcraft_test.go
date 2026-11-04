package controlpanel

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// The Crafting card and the new Fletching rows as /api/loot delivers them (a user request: "we
// dont have a loot tracker panel for crafting and pearl bolt tips dont show in the fletching
// section either"). internal/loot's tests prove the sources; these prove the aggregator keeps
// Crafting a card of its own, counts its events under "craft", and nets a two-input recipe of BOTH
// inputs.

func cards(t *testing.T, p LootPeriod) map[string][]LootSourceView {
	t.Helper()
	out := map[string][]LootSourceView{}
	for _, s := range p.Sources {
		out[s.Source.GroupKey()] = append(out[s.Source.GroupKey()], s)
	}
	return out
}

// TestCraftingIsACardOfItsOwnNetOfTheUncutGems: gems cut, crushed and flax spun land on one
// Crafting card (two rows), apart from Fletching, and the card is net of the uncut gems and flax.
func TestCraftingIsACardOfItsOwnNetOfTheUncutGems(t *testing.T) {
	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"uncut sapphire":   {Name: "Uncut sapphire", Coins: 800, Trades: 20, Units: 9000},
		"sapphire":         {Name: "Sapphire", Coins: 1000, Trades: 20, Units: 9000},
		"flax":             {Name: "Flax", Coins: 10, Trades: 20, Units: 9000},
		"bow string":       {Name: "Bow string", Coins: 60, Trades: 20, Units: 9000},
		"oyster pearls":    {Name: "Oyster pearls", Coins: 500, Trades: 9, Units: 900},
		"pearl bolttips":   {Name: "Pearl bolttips", Coins: 30, Trades: 9, Units: 9000},
		"bolts":            {Name: "Bolts", Coins: 10, Trades: 9, Units: 9000},
		"pearl bolts":      {Name: "Pearl bolts", Coins: 70, Trades: 9, Units: 9000},
		"crushed gemstone": {Name: "Crushed gemstone", Coins: 1, Trades: 9, Units: 900},
	})
	var drops []loot.Drop
	sink := func(d loot.Drop) { drops = append(drops, d) }
	// 100 uncut sapphires: 97 cut, 3 smashed. 50 flax spun. 10 Oyster pearls into 240 tips, 200
	// of them onto 200 Bolts.
	loot.Made(sink, nil, loot.Crafting(nil, 1623, "Uncut sapphire"), 100,
		loot.Product(1607, "Sapphire", 97), loot.Product(1633, "Crushed gemstone", 3))
	loot.Made(sink, nil, loot.Crafting(nil, 1779, "Flax"), 50, loot.Product(1777, "Bow string", 50))
	loot.Made(sink, nil, loot.Fletching(nil, 413, "Oyster pearls"), 10, loot.Product(46, "Pearl bolttips", 240))
	loot.Made(sink, nil, loot.Fletching(nil, 46, "Pearl bolttips"), 200, loot.Product(880, "Pearl bolts", 200))
	var totals []loot.Totals
	for _, d := range drops {
		totals = append(totals, loot.Totals{Source: d.Source, Events: int64(d.Events), Items: d.Items})
	}
	a.loot.record(lootReading("main", 1, totals...), base)

	lifetime, _, _, _ := a.loot.body()
	c := cards(t, lifetime)
	craft := c[loot.Crafting(nil, 1779, "Flax").GroupKey()]
	fletch := c[loot.Fletching(nil, 46, "Pearl bolttips").GroupKey()]
	if len(craft) != 2 || len(fletch) != 2 {
		t.Fatalf("got %d Crafting and %d Fletching rows, want 2 and 2: %v", len(craft), len(fletch), c)
	}
	if lifetime.Events[loot.ActionCraft] != 150 {
		t.Errorf("Events[craft] = %d, want 150 (100 gems + 50 flax)", lifetime.Events[loot.ActionCraft])
	}
	for _, s := range craft {
		switch s.Source.Name {
		case "Uncut sapphire":
			if !s.CostKnown || s.Cost != 80000 {
				t.Errorf("gem row cost = %d known %v, want 100 x 800", s.Cost, s.CostKnown)
			}
		case "Flax":
			if !s.CostKnown || s.Cost != 500 {
				t.Errorf("flax row cost = %d known %v, want 50 x 10", s.Cost, s.CostKnown)
			}
		}
	}
	for _, s := range fletch {
		if s.Source.Name != "Pearl bolttips" {
			continue
		}
		// 200 tips at 30 and 200 Bolts at 10: the Bolts are a real cost (the main account buys them).
		if !s.CostKnown || s.Cost != 8000 || s.CostItem != "Pearl bolttips + Bolts" {
			t.Errorf("pearl bolt row = cost %d known %v item %q, want 8000 for tips + Bolts", s.Cost, s.CostKnown, s.CostItem)
		}
	}
}

// TestAStrungBowRowIsGrossBecauseTheUnstrungBowHasNoPriceOfItsOwn: the unstrung and the strung bow
// share a display name, so the price source cannot price the input; the row says gross.
func TestAStrungBowRowIsGrossBecauseTheUnstrungBowHasNoPriceOfItsOwn(t *testing.T) {
	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"yew longbow": {Name: "Yew longbow", Coins: 900, Trades: 20, Units: 900},
		"bow string":  {Name: "Bow string", Coins: 60, Trades: 20, Units: 9000},
	})
	a.loot.record(lootReading("main", 1, loot.Totals{
		Source: loot.Stringing(66, "Yew longbow"), Events: 10,
		Items: []loot.Item{{ID: 855, Name: "Yew longbow", Count: 10}},
	}), base)
	lifetime, _, _, _ := a.loot.body()
	if len(lifetime.Sources) != 1 {
		t.Fatalf("got %d sources", len(lifetime.Sources))
	}
	s := lifetime.Sources[0]
	if s.CostKnown || s.Cost != 0 || s.CostItem != "Yew longbow (u) + Bow string" {
		t.Errorf("stringing row = %+v, want an unknown cost over Yew longbow (u) + Bow string", s)
	}
	if lifetime.Gross != 9000 || lifetime.Cost != 0 {
		t.Errorf("gross %d cost %d, want 9000 and nothing subtracted", lifetime.Gross, lifetime.Cost)
	}
}

// TestThePrayerCardCostsTheBonesBuried: burying spends the bones, priced from the same PriceBook
// (a user request: "the Prayer panel must count the bone prices").
func TestThePrayerCardCostsTheBonesBuried(t *testing.T) {
	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"big bones": {Name: "Big bones", Coins: 300, Trades: 20, Units: 9000},
	})
	a.loot.record(lootReading("burier", 1, loot.Totals{Source: loot.Bury(nil, 532, "Big bones"), Events: 100, XP: 1500}), base)
	lifetime, _, _, _ := a.loot.body()
	if len(lifetime.Sources) != 1 {
		t.Fatalf("got %d sources", len(lifetime.Sources))
	}
	s := lifetime.Sources[0]
	if !s.CostKnown || s.Cost != 30000 || s.XP != 1500 || lifetime.Value != -30000 {
		t.Errorf("prayer row cost %d known %v xp %d, value %d; want 100 Big bones at 300 against 1500 XP", s.Cost, s.CostKnown, s.XP, lifetime.Value)
	}
}
