package loot

import "testing"

// TestOnlyTheSourcesThatReallyConsumeSomethingHaveACost.
//
// A Cost is subtracted from a card's earnings, so a wrong one is worse than none: it would
// invent an expense the fleet never paid. This pins down both lists — what consumes, and what
// explicitly does not.
func TestOnlyTheSourcesThatReallyConsumeSomethingHaveACost(t *testing.T) {
	// Fletching consumes its MATERIAL, which is the source's own item because a production
	// source is keyed on its input (production.go).
	logs := Fletching(nil, 1511, "Logs")
	c, ok := logs.Consumes()
	if !ok {
		t.Fatal("a fletching source consumes its material")
	}
	if c.Name != "Logs" || c.ID != 1511 || c.Per != 1 {
		t.Errorf("fletching cost = %+v, want one Logs (1511) per event", c)
	}

	// The crystal chest consumes one Crystal key per open, whatever it gives back.
	c, ok = CrystalChest().Consumes()
	if !ok {
		t.Fatal("a crystal chest open consumes a key")
	}
	if c.Name != CrystalKeyName || c.ID != CrystalKeyID || c.Per != 1 {
		t.Errorf("chest cost = %+v, want one %s (%d) per event", c, CrystalKeyName, CrystalKeyID)
	}

	// An oyster does not: the closed oyster is never loot (a user request: "just oyster pearls"),
	// so opening one costs nothing on the card.
	if c, ok = Openable("Oyster", 407).Consumes(); ok {
		t.Errorf("oyster cost = %+v, want none", c)
	}

	// Nothing else does — including the casket, which has the same SHAPE. cost.go argues that
	// omission rather than leaving it to inference, and this is what makes it a decision: if
	// someone gives caskets a cost, this test says so loudly enough to go and read why.
	for _, src := range []Source{
		Openable("Casket", 405),
		Woodcutting(nil, 1276, "Tree"),
		Mining(nil, 2491, "Rocks"),
		Fishing(nil, 316),
		NPC(nil, ActionKill, 100, "Goblin"),
		NPC(nil, ActionPickpocket, 1, "Man"),
		Trail("Clue scroll", 2677),
	} {
		if c, ok := src.Consumes(); ok {
			t.Errorf("%s (%s) claims to consume %+v; see cost.go before changing this", src.Name, src.Kind, c)
		}
	}
}

// TestAProductionSourceWithoutAMaterialHasNoCost: a Source read back from an older persisted
// file, or one built by hand, can be missing the id or the name the cost is priced from. Half a
// cost is not a cost — it would price an unnamed item at whatever the book says about "".
func TestAProductionSourceWithoutAMaterialHasNoCost(t *testing.T) {
	for _, src := range []Source{
		{Kind: KindProduction, Skill: SkillFletching, Action: ActionFletch, Name: "", ID: 1511},
		{Kind: KindProduction, Skill: SkillFletching, Action: ActionFletch, Name: "Logs", ID: -1},
	} {
		if c, ok := src.Consumes(); ok {
			t.Errorf("incomplete production source %+v claimed a cost of %+v", src, c)
		}
	}
}

// TestTheChestsCostIsNamedNotInferredFromTheSourceID.
//
// CrystalChest()'s Source.ID is the KEY's obj id, which is a presentation choice — the chest is a
// loc and the panel can only draw an npc or an obj, so the card borrows the key's model
// (crystalchest.go). If that choice is ever revisited, the cost must not silently follow it to
// whatever the new id is.
func TestTheChestsCostIsNamedNotInferredFromTheSourceID(t *testing.T) {
	moved := CrystalChest()
	moved.ID = 12345 // as if the card's model changed
	c, ok := moved.Consumes()
	if !ok {
		t.Fatal("the chest still consumes a key")
	}
	if c.ID != CrystalKeyID || c.Name != CrystalKeyName {
		t.Fatalf("cost followed the card's model to %+v; it must stay the crystal key", c)
	}
}
