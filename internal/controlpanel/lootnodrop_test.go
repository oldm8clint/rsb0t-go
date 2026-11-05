package controlpanel

import (
	"testing"

	"rsb0t-go/internal/loot"
)

func TestUnfileNoDropKillsMovesItsItemsOffTheTrooper(t *testing.T) {
	trooper := loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Khazard trooper", ID: 475}
	dwarf := loot.Source{Kind: loot.KindNPC, Action: loot.ActionKill, Name: "Dwarf", ID: 118}
	chaos := loot.Item{ID: 562, Name: "Chaos rune", Count: 6}
	m := map[string]map[string]*lootRec{"a": {
		trooper.Key(): {Source: trooper, Events: 40, First: 5, Last: 9, Items: []loot.Item{chaos}, XP: 3},
		dwarf.Key():   {Source: dwarf, Events: 10, Items: []loot.Item{{ID: 995, Name: "Coins", Count: 20}}},
	}}
	unfileNoDropKills(m)
	if got := m["a"][trooper.Key()]; len(got.Items) != 0 || got.Events != 40 || got.XP != 3 {
		t.Fatalf("trooper record %+v", got)
	}
	pk := loot.Pickup(nil, 562, "Chaos rune")
	if got := m["a"][pk.Key()]; got == nil || got.Items[0].Count != 6 || got.First != 5 {
		t.Fatalf("pickup record %+v", got)
	}
	// Then the fold files the runes under the dwarf, the one kill here that can drop them.
	removed, target := foldRunePickups(m)
	if !removed[pk.Key()] || target["a"].Name != "Dwarf" {
		t.Fatalf("fold: %v %v", removed, target)
	}
	if got := m["a"][dwarf.Key()]; len(got.Items) != 2 {
		t.Errorf("dwarf items %+v", got.Items)
	}
	unfileNoDropKills(m) // idempotent
}
