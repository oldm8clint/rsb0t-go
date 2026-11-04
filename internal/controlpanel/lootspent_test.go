package controlpanel

import (
	"testing"
	"time"

	"rsb0t-go/internal/loot"
)

// A kill made with Magic is net of the runes cast at it (a user request: "subtract runes spent
// (count and price per spell cast) from that NPC's drop value"). The runes travel as the kill's
// measured Spent, through the tally, into the source's cost.
func TestAMagicKillIsNetOfTheRunesCastAtIt(t *testing.T) {
	base := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"cowhide":   {Name: "Cowhide", Coins: 100, Trades: 20, Units: 9000},
		"fire rune": {Name: "Fire rune", Coins: 5, Trades: 20, Units: 90000},
		"mind rune": {Name: "Mind rune", Coins: 3, Trades: 20, Units: 90000},
	})
	cow := loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Cow", ID: 81, Level: 2}
	tally := loot.NewTally(func() time.Time { return base })
	for i := 0; i < 10; i++ {
		tally.Add(loot.Drop{Source: cow, Events: 1, Spent: []loot.Item{{ID: 554, Name: "Fire rune", Count: 3}, {ID: 558, Name: "Mind rune", Count: 3}}})
	}
	tally.Add(loot.Drop{Source: cow, Items: []loot.Item{{ID: 1739, Name: "Cowhide", Count: 10, Value: 1}}})
	a.loot.record(lootReading("acct1", 1, tally.Snapshot()...), base)
	// A second reading of the same cumulative tally adds nothing.
	a.loot.record(lootReading("acct1", 2, tally.Snapshot()...), base)

	lifetime, _, _, _ := a.loot.body()
	// The cow, and the Magic card's Combat spells row that mirrors its bill (loot.CombatSpells).
	if len(lifetime.Sources) != 2 {
		t.Fatalf("got %d sources, want the cow and the Combat spells mirror", len(lifetime.Sources))
	}
	var s, spells LootSourceView
	for _, v := range lifetime.Sources {
		if v.Source.MirrorsCost() {
			spells = v
		} else {
			s = v
		}
	}
	// The mirror carries the same bill and the same kills, and is not subtracted a second time below.
	if spells.Source.Name != "Combat spells" || spells.Events != 10 || !spells.CostKnown || spells.Cost != 240 {
		t.Errorf("combat spells row = %+v, want 10 kills and the same 240 gp bill", spells)
	}
	// 30 fire runes at 5 and 30 mind runes at 3 = 240 against 10 hides at 100.
	if !s.CostKnown || s.Cost != 240 || s.CostUnits != 60 || s.CostItem != "Fire rune + Mind rune" {
		t.Errorf("cow row cost %d units %d item %q known %v, want 240 for 60 runes", s.Cost, s.CostUnits, s.CostItem, s.CostKnown)
	}
	if lifetime.Gross != 1000 || lifetime.Value != 760 {
		t.Errorf("gross %d net %d, want 1000 and 760", lifetime.Gross, lifetime.Value)
	}
}

// TestOffensiveSpellsThatDidNotKillStillCostRunes: a fight that ended with no kill carries its
// runes with no event on the npc, and the Magic card's Combat spells row counts them too, while the
// fleet's net is charged once.
func TestOffensiveSpellsThatDidNotKillStillCostRunes(t *testing.T) {
	base := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	a := newStatsAggregator("", nil, fixedNow(base))
	a.loot.prices = priced(map[string]MarketPrice{
		"fire rune": {Name: "Fire rune", Coins: 5, Trades: 20, Units: 90000},
	})
	cow := loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Cow", ID: 81, Level: 2}
	tally := loot.NewTally(func() time.Time { return base })
	tally.Add(loot.Drop{Source: cow, Events: 1, Spent: []loot.Item{{ID: 554, Name: "Fire rune", Count: 4}}})
	tally.Add(loot.Drop{Source: cow, Spent: []loot.Item{{ID: 554, Name: "Fire rune", Count: 6}}}) // the cow lived
	a.loot.record(lootReading("acct1", 1, tally.Snapshot()...), base)

	lifetime, _, _, _ := a.loot.body()
	var spells, npc LootSourceView
	for _, v := range lifetime.Sources {
		if v.Source.MirrorsCost() {
			spells = v
		} else {
			npc = v
		}
	}
	if npc.Events != 1 || npc.Cost != 50 || spells.Events != 1 || spells.Cost != 50 {
		t.Fatalf("npc %+v / spells %+v, want 1 kill and 10 runes (50 gp) on both", npc, spells)
	}
	if lifetime.Cost != 50 || lifetime.Value != -50 {
		t.Fatalf("fleet cost %d net %d, want the 50 gp charged once", lifetime.Cost, lifetime.Value)
	}
	if len(lifetime.Accounts) != 1 || lifetime.Accounts[0].Events != 1 {
		t.Fatalf("accounts = %+v, want the one kill counted once", lifetime.Accounts)
	}
}
