package controlpanel

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// The Consumables tab: what was eaten and drunk is a cost with a coin value, priced as loot is (so a
// cake the market never traded still costs its cache value), a potion at a dose, and counted once
// (a user request).
func TestConsumablesAreCostsPricedLeniently(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"shark":              {Name: "Shark", Coins: 600, Trades: 40, Units: 9000},
		"strength potion(4)": {Name: "Strength potion(4)", Coins: 400, Trades: 40, Units: 900},
		"strength potion(3)": {Name: "Strength potion(3)", Coins: 300, Trades: 40, Units: 900},
		"jug of wine":        {Name: "Jug of wine", Coins: 50, Trades: 40, Units: 900},
	})
	tally := loot.NewTally(func() time.Time { return base })
	shark := loot.Item{ID: 385, Name: "Shark", Count: 1, Value: 150}
	cake := loot.Item{ID: 1891, Name: "Cake", Count: 1, Value: 50} // never traded: the cache cost stands
	for i := 0; i < 3; i++ {
		tally.Add(loot.Drop{Source: loot.Consumable(nil, 385, loot.ActionEat, "Shark"), Events: 1, Spent: []loot.Item{shark}})
	}
	tally.Add(loot.Drop{Source: loot.Consumable(nil, 1891, loot.ActionEat, "Cake"), Events: 1, Spent: []loot.Item{cake}})
	tally.Add(loot.Drop{Source: loot.Consumable(nil, 2440, loot.ActionDrink, "Strength potion(4)"), Events: 1,
		Spent: []loot.Item{{ID: 2440, Name: "Strength potion(4)", Count: 1, Value: 20}}})
	tally.Add(loot.Drop{Source: loot.Consumable(nil, 113, loot.ActionDrink, "Strength potion(3)"), Events: 1,
		Spent: []loot.Item{{ID: 113, Name: "Strength potion(3)", Count: 1, Value: 20}}})
	a.loot.record(lootReading("acct1", 1, tally.Snapshot()...), base)

	lifetime, _, _, _ := a.loot.body()
	rows := map[string]LootSourceView{}
	for _, s := range lifetime.Sources {
		if s.Source.Kind != loot.KindConsumable {
			t.Fatalf("a non-consumable source %+v on a consumables-only fleet", s.Source)
		}
		rows[s.Source.Name+"|"+s.Source.Key()] = s
		if len(s.Items) != 0 {
			t.Fatalf("%s looted %+v: a consumable is a cost, never loot", s.Source.Name, s.Items)
		}
	}
	shark3 := rows["Shark|"+loot.Consumable(nil, 385, loot.ActionEat, "Shark").Key()]
	if shark3.Events != 3 || !shark3.CostKnown || shark3.Cost != 1800 {
		t.Fatalf("shark row = %+v, want 3 eaten at 600", shark3)
	}
	cakeRow := rows["Cake|"+loot.Consumable(nil, 1891, loot.ActionEat, "Cake").Key()]
	// Cake is not on the market: high alchemy of its cache cost (60% of 50), as loot is priced.
	if !cakeRow.CostKnown || cakeRow.Cost != 30 {
		t.Fatalf("cake row = %+v, want 30 gp from its cache cost", cakeRow)
	}
	// Two drinks of one potion: a quarter of the (4) price and a third of the (3) price, 100 and 100.
	var potion int64
	for k, v := range rows {
		if v.Source.Name == "Strength potion" {
			potion += v.Cost
		} else if k == "" {
			t.Fatal("unreachable")
		}
	}
	if potion != 200 {
		t.Fatalf("two doses cost %d, want 100 each", potion)
	}
	if lifetime.Gross != 0 || lifetime.Cost != 1800+30+200 || lifetime.Value != -(1800+30+200) {
		t.Fatalf("gross %d cost %d net %d, want a pure cost of %d", lifetime.Gross, lifetime.Cost, lifetime.Value, 1800+30+200)
	}
	if lifetime.Events["eat"] != 4 || lifetime.Events["drink"] != 2 {
		t.Fatalf("events = %v", lifetime.Events)
	}
}
