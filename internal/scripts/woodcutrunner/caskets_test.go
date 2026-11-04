package woodcutrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/fisherrunner"
)

// A casket's own reward can look like another casket to a careless matcher. Content 289's
// [opheld1,oystershell] hands back an "Empty oyster" -- which CONTAINS "oyster" -- so anything
// matching openables as a substring re-opens the spent shells forever. This is the guard for
// that: the woodcutter's casket pass borrows fisherrunner.IsOpenable rather than keeping a
// second list, and the exactness is the whole reason.
func TestSpentShellsAreNotOpenedAgain(t *testing.T) {
	for _, name := range []string{"Casket", "casket", "Oyster", "oyster"} {
		if !fisherrunner.IsOpenable(name) {
			t.Errorf("IsOpenable(%q) = false, want true — the fare pass would open nothing", name)
		}
	}
	for _, name := range []string{"Empty oyster", "Oyster pearl", "Oyster pearls", "Casket key", "Logs"} {
		if fisherrunner.IsOpenable(name) {
			t.Errorf("IsOpenable(%q) = true, want false — the fare pass would spin on its own rewards", name)
		}
	}
}

// The pass is bounded on purpose: a broke account pays a 60-coin ship fare out of caskets, and
// does NOT go looking for a 150,000-coin rune axe in them. If these bounds ever grow, an
// account with two hundred banked oysters spends its afternoon opening them instead of chopping.
func TestTheFarePassStaysASidetrip(t *testing.T) {
	if casketRounds > 5 {
		t.Errorf("casketRounds = %d: too many bank round trips for a ship fare", casketRounds)
	}
	if casketsPerRound > 28 {
		t.Errorf("casketsPerRound = %d: more than a pack holds", casketsPerRound)
	}
}
