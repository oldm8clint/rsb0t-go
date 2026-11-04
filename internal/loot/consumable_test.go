package loot

import "testing"

func TestPotionDosesShareOneRowAndPriceADose(t *testing.T) {
	if BaseName("Strength potion(4)") != "Strength potion" || BaseName("Shark") != "Shark" || BaseName("Super restore(2)") != "Super restore" {
		t.Fatal("BaseName did not strip the dose count")
	}
	if Doses("Strength potion(4)") != 4 || Doses("Strength potion(1)") != 1 || Doses("Shark") != 1 {
		t.Fatal("Doses misread the form")
	}
	a := Consumable(nil, 2428, ActionDrink, "Attack potion(4)")
	b := Consumable(nil, 121, ActionDrink, "Attack potion(3)")
	if a.PartKey() != b.PartKey() || a.GroupKey() != b.GroupKey() || a.Key() == b.Key() {
		t.Fatalf("dose forms should be one row (%q/%q) and two records (%q/%q)", a.PartKey(), b.PartKey(), a.Key(), b.Key())
	}
	if a.Kind != KindConsumable || a.Skill != SkillConsumables {
		t.Fatalf("source = %+v", a)
	}
	if _, ok := a.Consumes(); ok || len(a.Inputs()) != 0 {
		t.Fatal("a consumable's cost is its Spent item, not a fixed input")
	}
}

func TestConsumablesAreOneCardApartFromEveryOtherKind(t *testing.T) {
	shark := Consumable(nil, 385, ActionEat, "Shark")
	wine := Consumable(nil, 1993, ActionDrink, "Jug of wine")
	if shark.GroupKey() != wine.GroupKey() || shark.PartKey() == wine.PartKey() {
		t.Fatal("foods and drinks are one card of two rows")
	}
	if shark.GroupKey() == Teleport("Varrock Teleport").GroupKey() || shark.GroupKey() == Fletching(nil, 1511, "Logs").GroupKey() {
		t.Fatal("Consumables shares a card with another tab")
	}
}

func TestConsumedIsNotAnythingWithoutACache(t *testing.T) {
	if _, ok := Consumed(nil, 385); ok {
		t.Fatal("nothing is edible without a cache to say so")
	}
	if _, ok := ConsumableOp(nil, 385); ok {
		t.Fatal("ConsumableOp answered without a cache")
	}
}
