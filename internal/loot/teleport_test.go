package loot

import "testing"

// TestTeleportNameByRunes: a cast that spent exactly a spell's runes is that spell.
func TestTeleportNameByRunes(t *testing.T) {
	cases := []struct {
		lost Counts
		xp   int
		want string
	}{
		{Counts{FireRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 35, "Varrock Teleport"},
		{Counts{EarthRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 41, "Lumbridge Teleport"},
		{Counts{WaterRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 48, "Falador Teleport"},
		{Counts{AirRuneID: 5, LawRuneID: 1}, 56, "Camelot Teleport"},
		{Counts{WaterRuneID: 2, LawRuneID: 2}, 61, "Ardougne Teleport"},
		{Counts{EarthRuneID: 2, LawRuneID: 2}, 68, "Watchtower Teleport"},
		{Counts{FireRuneID: 2, LawRuneID: 2}, 68, "Trollheim Teleport"},
		{Counts{AirRuneID: 1, LawRuneID: 1}, 43, "Telekinetic Grab"},
		// The experience is not consulted when the runes are exact (it may be scaled elsewhere).
		{Counts{FireRuneID: 1, AirRuneID: 3, LawRuneID: 1}, 350, "Varrock Teleport"},
	}
	for _, c := range cases {
		if got := TeleportName(c.lost, c.xp); got != c.want {
			t.Errorf("TeleportName(%v, %d) = %q, want %q", c.lost, c.xp, got, c.want)
		}
	}
}

// TestTeleportNameWithAStaffNeedsTheExperience: a Staff of air pays Air runes, so a Varrock cast
// loses a Fire and a Law rune only, and the experience tells it from a Lumbridge cast.
func TestTeleportNameWithAStaffNeedsTheExperience(t *testing.T) {
	if got := TeleportName(Counts{FireRuneID: 1, LawRuneID: 1}, 35); got != "Varrock Teleport" {
		t.Errorf("Varrock from an air staff = %q", got)
	}
	if got := TeleportName(Counts{EarthRuneID: 1, LawRuneID: 1}, 41); got != "Lumbridge Teleport" {
		t.Errorf("Lumbridge from an air staff = %q", got)
	}
	// Law runes alone and an experience two spells share: unknown, but still a cast with a cost.
	if got := TeleportName(Counts{LawRuneID: 2}, 68); got != "Teleport" {
		t.Errorf("ambiguous cast = %q, want the generic name", got)
	}
	// More of a rune than any spell asks for is no spell.
	if got := TeleportName(Counts{AirRuneID: 9, LawRuneID: 1}, 55); got != "Teleport" {
		t.Errorf("nonsense cast = %q", got)
	}
}

// TestTeleportsAreOneCardOfOneKindWithARowPerSpell: the Teleports tab is its own kind, every spell
// on one card, each its own row, and nothing about a teleport consumes a fixed input.
func TestTeleportsAreOneCardOfOneKindWithARowPerSpell(t *testing.T) {
	v, a := Teleport("Varrock Teleport"), Teleport("Ardougne Teleport")
	if v.Kind != KindTeleport || v.GroupKey() != a.GroupKey() || v.PartKey() == a.PartKey() {
		t.Fatalf("group %q/%q parts %q/%q", v.GroupKey(), a.GroupKey(), v.PartKey(), a.PartKey())
	}
	if _, ok := v.Consumes(); ok || len(v.Inputs()) != 0 {
		t.Fatal("a teleport's cost is measured (Drop.Spent), not a fixed input")
	}
	if Teleport("").Name != "Teleport" {
		t.Fatal("an unnamed cast has the generic name")
	}
}

// TestTeleportCastIsTalliedWithItsRunes: a Drop with only a cost still counts, and the runes add up.
func TestTeleportCastIsTalliedWithItsRunes(t *testing.T) {
	tally := NewTally(nil)
	src := Teleport("Varrock Teleport")
	cost := Items(nil, Counts{FireRuneID: 1, AirRuneID: 3, LawRuneID: 1})
	tally.Add(Drop{Source: src, Events: 1, Spent: cost})
	tally.Add(Drop{Source: src, Events: 1, Spent: cost})
	got := tally.Snapshot()
	if len(got) != 1 || got[0].Events != 2 || len(got[0].Items) != 0 {
		t.Fatalf("snapshot = %+v", got)
	}
	for _, it := range got[0].Spent {
		if it.ID == LawRuneID && it.Count != 2 {
			t.Fatalf("law runes spent = %d, want 2", it.Count)
		}
	}
}
