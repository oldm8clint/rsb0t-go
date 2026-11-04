package consolidateplan

import "testing"

// TestPlanHandOffLootOnly is the solo dump (ScarabSwarm supplyMode soloDump / soloMarketDump):
// coins, runes, food and staffs stay whatever the surplus, only loot crosses, and only loot
// justifies the trip.
func TestPlanHandOffLootOnly(t *testing.T) {
	s := Defaults()
	s.Loot = false // the mode turns the loot half on whatever consolidateLoot says
	s.LootOnly = true
	in := HandOffInput{
		Holdings: Holdings{Coins: 5_000_000, Runes: map[string]int{"Chaos rune": 90_000}, Food: map[string]int{"Shark": 400}},
		Keep:     Keep{Coins: 50_000, Food: 16},
		Bank: []BankRow{
			{Name: "Coins", ID: 995, Count: 5_000_000, Value: 1},
			{Name: "Chaos rune", ID: 562, Count: 90_000, Value: 90},
			{Name: "Shark", ID: 385, Count: 400, Value: 300},
			{Name: "Staff of air", ID: 1381, Count: 3, Value: 1500},
			{Name: "Rune arrow", ID: 892, Count: 100, Value: 400},
		},
		Kit:       stage5Kit(),
		MuleKinds: map[string]bool{"rune arrow": true},
		MuleKnown: true,
	}
	h := PlanHandOff(s, in)
	if !h.Walk || h.Why != "loot worth the trip" {
		t.Fatalf("40,000 of arrows justify a loot-only trip: %+v", h)
	}
	if h.Deposit.Coins != 0 || h.Deposit.RuneUnits() != 0 || h.Deposit.FoodUnits() != 0 {
		t.Fatalf("a loot-only trip carries no surplus: %+v", h.Deposit)
	}
	for _, l := range h.Lines() {
		if l.Kind != KindLoot || l.Name != "Rune arrow" {
			t.Fatalf("only the arrows cross, got %+v", h.Lines())
		}
	}
	// No loot: the huge surplus alone never walks.
	in.Bank = in.Bank[:4]
	if h := PlanHandOff(s, in); h.Walk || len(h.Lines()) != 0 {
		t.Fatalf("surplus coins, runes, food and staffs never justify a loot-only trip: %+v", h)
	}
	if n := s.Normalised(); !n.Loot {
		t.Fatal("LootOnly turns the loot half on")
	}
	// Without LootOnly the same holdings send the surplus as before.
	s.LootOnly, s.Loot = false, true
	if h := PlanHandOff(s, in); !h.Walk || h.Deposit.Coins == 0 {
		t.Fatalf("the ordinary hand-off still carries the surplus: %+v", h)
	}
}
