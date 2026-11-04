package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

func TestStaffPreferencePutsTheMagicStaffAboveThePlainStaff(t *testing.T) {
	bolt, _ := SpellByName("Wind Bolt")
	pref := StaffPreference(bolt)
	if pref[0] != StaffOfAir {
		t.Fatalf("for Wind Bolt the Staff of air pays two air a cast and must come first: %v", pref)
	}
	if pref[len(pref)-1] != PlainStaff {
		t.Fatalf("the plain Staff loses on runes, bonus and death and must come last: %v", pref)
	}
	idx := map[string]int{}
	for i, n := range pref {
		idx[n] = i
	}
	if idx[MagicStaff] > idx[PlainStaff] {
		t.Fatalf("Magic staff must outrank a plain Staff: %v", pref)
	}
	fire, _ := SpellByName("Fire Strike")
	if StaffPreference(fire)[0] != "Staff of fire" {
		t.Fatalf("the ranking is spell-aware: Fire Strike's three fire runes make a Staff of fire best, got %v", StaffPreference(fire))
	}
}

// THE LIVELOCK, at the decider. An account at its Magic target with its runes banked and no staff
// (a plain Staff lost to a Stage 6 death). The runner now reports TargetMet false for it
// (internal/bot: a staffless Stage-6-bound account has not met its target) and StaffWanted true.
// The control arm is the pre-fix decision: with StaffWanted false the same account thieves and
// never fetches a staff -- which is what origin/main did, because its only staff branch was gated
// on "still training".
func TestDecideStage3PhaseFetchesAStaffPastTheMagicTarget(t *testing.T) {
	at := func(tt tile.Tile) *tile.Tile { return &tt }
	s := Stage3Snapshot{Tile: at(ArdougneLever), HP: 20, MaxHP: 20, MagicLevel: 17, MagicTarget: 17, Food: 8}
	if got := DecideStage3Phase(s); got != Phase3Thieve {
		t.Fatalf("control (the old rule): a staffless account past its target thieved, got %s", got)
	}
	s.StaffWanted = true
	if got := DecideStage3Phase(s); got != Phase3Staff {
		t.Fatalf("staffless and headed for Stage 6: %s, want %s", got, Phase3Staff)
	}
	s.MuleAskDue = true
	if got := DecideStage3Phase(s); got != Phase3Mule {
		t.Fatalf("and the mule first when it is due: %s, want %s", got, Phase3Mule)
	}
	s.HaveStaff = true
	if got := DecideStage3Phase(s); got == Phase3Staff {
		t.Fatal("an account holding a staff must not fetch another")
	}
}
