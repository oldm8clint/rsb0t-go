package fleet

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// A death records what the pack and the worn tab held, less the three priciest units the server keeps, at the tile, under the
// place and the cause, and the Manager hands it to the panel (DeathEvents).
func TestADeathIsRecordedWithItsLostItems(t *testing.T) {
	dc := gamestate.DeathContext{
		HaveTile: true, X: 2508, Z: 3097,
		Attackers: []gamestate.DeathActor{{Type: 111}, {Type: 111}},
		Pack: []gamestate.SlotItem{
			{Slot: 0, Item: gamestate.Item{ID: 858, Count: 4}},
			{Slot: 1, Item: gamestate.Item{ID: 995, Count: 250}},
		},
		Worn: []gamestate.SlotItem{{Slot: 3, Item: gamestate.Item{ID: 1333, Count: 1}}},
	}
	now := time.UnixMilli(1_800_000_000_000)
	d := deathOf("bot1", "Fighter", now, dc, deathInput(dc, nil), nil)
	if d.ID != "bot1@1800000000000" || d.Account != "bot1" || d.Script != "Fighter" || d.At != 1_800_000_000_000 {
		t.Fatalf("death = %+v", d)
	}
	if !d.HaveTile || d.X != 2508 || d.Z != 3097 || d.Place != "Yanille west ogres" || d.Cause != "npc 111" {
		t.Fatalf("where/why = %+v", d)
	}
	// No cache: every cost is 0, so nothing is kept (the server keeps only items that cost something) and all three stacks drop.
	got := map[int]int64{}
	for _, s := range d.Items {
		got[s.ID] = s.Count
	}
	if len(got) != 3 || got[858] != 4 || got[995] != 250 || got[1333] != 1 {
		t.Fatalf("items = %+v", d.Items)
	}

	var m Manager
	m.deathLoot.get().NoteDeath(d)
	if evs := m.DeathEvents(); len(evs) != 1 || evs[0].ID != d.ID {
		t.Fatalf("DeathEvents = %+v", evs)
	}
}
