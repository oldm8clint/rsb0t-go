package fleetplan

import (
	"testing"
)

// The Magic staff is alch stock (owner, 5 Oct); every staff the stages cast with or wield stays refused.
func TestNeverAlchLetsTheMagicStaffThrough(t *testing.T) {
	for _, n := range []string{"Magic staff", "magic staff", " Magic staff "} {
		if NeverAlch(n) {
			t.Errorf("%q is refused", n)
		}
	}
	for _, n := range []string{"Staff of air", "Staff of fire", "Staff of water", "Staff of earth", "Staff", "Battlestaff", "Mystic staff",
		"Nature rune", "Coins", "Rope", "Knife", "Shantay pass", ""} {
		if !NeverAlch(n) {
			t.Errorf("%q is not refused", n)
		}
	}
	held := []AlchCandidate{{Name: "Magic staff", Count: 4, Value: 120}, {Name: "Staff of air", Count: 1, Value: 900}, {Name: "Staff", Count: 1, Value: 6}}
	if got := RankAlchables(held, modesOn()); len(got) != 1 || got[0].Name != "Magic staff" {
		t.Errorf("ranked %+v, want the Magic staff alone", got)
	}
}
