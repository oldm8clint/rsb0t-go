package controlpanel

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// The Teleports tab: a cast has no items, only the runes it measurably spent, and the card is net of
// them like every other cost (a user request: "cost per teleport cast, count, coin value").
func TestTeleportCastsAreACostOnTheTeleportsTab(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"law rune":  {Name: "Law rune", Coins: 200, Trades: 30, Units: 90000},
		"fire rune": {Name: "Fire rune", Coins: 5, Trades: 30, Units: 90000},
		"air rune":  {Name: "Air rune", Coins: 4, Trades: 30, Units: 90000},
	})
	tally := loot.NewTally(func() time.Time { return base })
	varrock := []loot.Item{{ID: 554, Name: "Fire rune", Count: 1}, {ID: 556, Name: "Air rune", Count: 3}, {ID: 563, Name: "Law rune", Count: 1}}
	for i := 0; i < 4; i++ {
		tally.Add(loot.Drop{Source: loot.Teleport("Varrock Teleport"), Events: 1, Spent: varrock})
	}
	tally.Add(loot.Drop{Source: loot.Teleport("Ardougne Teleport"), Events: 1, Spent: []loot.Item{{ID: 555, Name: "Water rune", Count: 2}, {ID: 563, Name: "Law rune", Count: 2}}})
	a.loot.record(lootReading("acct1", 1, tally.Snapshot()...), base)

	lifetime, _, _, _ := a.loot.body()
	if len(lifetime.Sources) != 2 {
		t.Fatalf("got %d sources, want one row per spell", len(lifetime.Sources))
	}
	var v LootSourceView
	for _, s := range lifetime.Sources {
		if s.Source.Name == "Varrock Teleport" {
			v = s
		}
	}
	// Four casts, each a fire rune (5), three air runes (4 each) and a law rune (200): 217 a cast.
	if v.Events != 4 || !v.CostKnown || v.Cost != 4*217 || v.Source.Kind != loot.KindTeleport {
		t.Fatalf("Varrock row = events %d cost %d known %v kind %q, want 4 casts at 217", v.Events, v.Cost, v.CostKnown, v.Source.Kind)
	}
	if len(v.Items) != 0 {
		t.Fatalf("a cast looted %+v", v.Items)
	}
	// The Ardougne row has no Water rune price: unknown, never zero, and left out of the net.
	if lifetime.Cost != 4*217 {
		t.Fatalf("fleet cost = %d, want only the priceable Varrock casts", lifetime.Cost)
	}
	if lifetime.Value != -4*217 || lifetime.Gross != 0 {
		t.Fatalf("net %d gross %d, want a pure cost", lifetime.Value, lifetime.Gross)
	}
	if lifetime.Events["teleport"] != 5 {
		t.Fatalf("teleport events = %v", lifetime.Events)
	}
}
