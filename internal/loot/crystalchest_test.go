package loot

import "testing"

func TestCrystalChestIsAnOpenableWithTheKeysModel(t *testing.T) {
	src := CrystalChest()
	if src.Kind != KindOpenable {
		t.Fatalf("kind = %q, want %q so the panel's Openables filter shows it", src.Kind, KindOpenable)
	}
	if src.Action != ActionOpen {
		t.Fatalf("action = %q, want %q", src.Action, ActionOpen)
	}
	if src.Name != CrystalChestName {
		t.Fatalf("name = %q, want %q", src.Name, CrystalChestName)
	}
	// The card's model. An openable carries no Source.Model, so the panel draws obj:<ID>.
	if src.ID != CrystalKeyID {
		t.Fatalf("id = %d, want the crystal key's obj id %d so the card has a model to draw", src.ID, CrystalKeyID)
	}
	if src.Model != "" {
		t.Fatalf("model = %q, want empty: an openable's model kind comes from its Kind", src.Model)
	}
}

func TestCrystalChestDoesNotCollideWithAnotherOpenable(t *testing.T) {
	casket := Openable("Casket", 405)
	if CrystalChest().Key() == casket.Key() {
		t.Fatal("the crystal chest and a casket share a tally key")
	}
}

func TestACrystalChestDropTalliesAsOneOpenPerKey(t *testing.T) {
	tally := NewTally(nil)
	tally.Add(Drop{Source: CrystalChest(), Events: 2, Items: []Item{{ID: 1631, Name: "Uncut dragonstone", Count: 2}}})
	snap := tally.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot has %d sources, want 1", len(snap))
	}
	if snap[0].Events != 2 {
		t.Fatalf("events = %d, want 2", snap[0].Events)
	}
	if len(snap[0].Items) != 1 || snap[0].Items[0].Count != 2 {
		t.Fatalf("items = %+v, want two uncut dragonstones", snap[0].Items)
	}
}
