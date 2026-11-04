package runerunnerplan

import (
	"testing"

	"rsb0t-go/internal/scripts/consolidateplan"
	"rsb0t-go/internal/scripts/donorplan"
)

// UNSPENT COINS STAY IN THE RUNNER'S OWN BANK (a user request). The runner now holds the mule's whole stack
// (11,229,284 coins that day), more than the 8,000,000 consolidateCoinsReserve the swarm phase used to keep, and the
// daily ScarabSwarm deposit trip would have handed the 3,000,000 above it straight back to the mule: a hand-back and
// a re-ask every round. The phase's reserve is now above any bank, and the planner's own sums show no coin crossing.
func TestSwarmPhaseHandsNoCoinBackToTheMule(t *testing.T) {
	s := DefaultSettings()
	m := s.SwarmSettingsMap()
	reserve, ok := m["consolidateCoinsReserve"].(float64)
	if !ok || int(reserve) != SwarmKeepAllCoins {
		t.Fatalf("reserve %v", m["consolidateCoinsReserve"])
	}
	cs := consolidateplan.Defaults()
	cs.CoinsReserve = int(reserve)
	for _, bank := range []int{500000, 8000001, 11229284, 40000000} {
		keep := consolidateplan.Keep{Coins: cs.CoinsReserve, Runes: map[string]int{}}
		plan := consolidateplan.PlanDeposit(cs, consolidateplan.Holdings{Coins: bank, Runes: map[string]int{}, Food: map[string]int{}}, keep, 0)
		if plan.Coins != 0 || plan.Walk {
			t.Errorf("a bank of %d: the trip hands over %d coins (walk %v: %s)", bank, plan.Coins, plan.Walk, plan.Why)
		}
	}
	// The old reserve is the control: 8,000,000 hands over everything above it.
	cs.CoinsReserve = MaxCircuitFunds
	old := consolidateplan.PlanDeposit(cs, consolidateplan.Holdings{Coins: 11229284, Runes: map[string]int{}, Food: map[string]int{}}, consolidateplan.Keep{Coins: MaxCircuitFunds, Runes: map[string]int{}}, 0)
	if old.Coins != 11229284-MaxCircuitFunds {
		t.Fatalf("control: the old reserve hands over %d", old.Coins)
	}
	// An explicit reserve in swarmSettings still stands.
	s.SwarmSettings = map[string]interface{}{"consolidateCoinsReserve": 123456.0}
	if got := s.SwarmSettingsMap()["consolidateCoinsReserve"]; got != 123456.0 {
		t.Errorf("an explicit consolidateCoinsReserve was replaced: %v", got)
	}
}

// Nothing in the delivery hands coins over either: only the listed runes, staffs and ammo cross, whatever the pack holds.
func TestDeliveryNeverCarriesCoins(t *testing.T) {
	rules := MuleRules([]string{"Mind rune", "Air rune", "Staff of air", "Bolts"})
	pack := []donorplan.ItemRow{
		{Name: "Coins", ID: 995, Count: 11229284},
		{Name: "Mind rune", ID: 558, Count: 900},
		{Name: "Bolts", ID: 9140, Count: 2380},
		{Name: "Cake", ID: 1891, Count: 1},
	}
	load := donorplan.PlanLoad(pack, donorplan.DefaultMuleFreeSlots, rules)
	for _, row := range load {
		if row.Name == "Coins" {
			t.Fatalf("coins are in the delivery: %+v", load)
		}
	}
	if len(load) != 2 {
		t.Errorf("load %+v, want the minds and the bolts", load)
	}
}
