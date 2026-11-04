package loot

import "testing"

// TestAKillWithRunesIsMirroredOnTheMagicCard: the bill of a kill made with spells is on the npc and
// repeated on the Magic card's Combat spells row, once, and a kill without runes mirrors nothing.
func TestAKillWithRunesIsMirroredOnTheMagicCard(t *testing.T) {
	tally := NewTally(nil)
	cow := NPC(nil, ActionKill, 81, "Cow")
	tally.Add(Drop{Source: cow, Events: 1, Spent: []Item{{ID: FireRuneID, Name: "Fire rune", Count: 3}}})
	tally.Add(Drop{Source: cow, Events: 1}) // a melee kill
	tally.Add(Drop{Source: cow, Spent: []Item{{ID: FireRuneID, Name: "Fire rune", Count: 2}}})
	var mirror, npc *Totals
	snap := tally.Snapshot()
	for i := range snap {
		if snap[i].Source.MirrorsCost() {
			mirror = &snap[i]
		} else {
			npc = &snap[i]
		}
	}
	if mirror == nil || npc == nil || len(snap) != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if npc.Events != 2 || mirror.Events != 1 || len(mirror.Spent) != 1 || mirror.Spent[0].Count != 5 || npc.Spent[0].Count != 5 {
		t.Fatalf("npc %+v mirror %+v, want the same 5 runes on both and only the spell kill on the row", npc, mirror)
	}
	if mirror.Source.GroupKey() != Alchemy(1, "x", true, true).GroupKey() {
		t.Fatal("Combat spells is not on the Magic card")
	}
	if _, ok := mirror.Source.Consumes(); ok || len(mirror.Source.Inputs()) != 0 {
		t.Fatal("the mirror row carries measured runes, not a fixed input")
	}
}
