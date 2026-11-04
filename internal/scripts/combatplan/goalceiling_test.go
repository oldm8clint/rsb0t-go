package combatplan

import "testing"

// The ladder's own top, and the cliff one level above it.
//
// A goal of exactly 75 finishes only because AllGoalsMet fires before the band is ever asked.
// A goal of 76 asks PickRung for a rung covering level 75, gets BlockedNoBand, and combattrainer
// turns that into a returned error — hours into the run, not at the start of it.
func TestMaxTrainableGoalIsTheTopOfTheLadder(t *testing.T) {
	if got := MaxTrainableGoal(MeleeLadder(), true); got != 75 {
		t.Fatalf("members ceiling = %d, want 75 (icegiants ends at 75 exclusive)", got)
	}
	// A free world neither raises nor lowers it: icegiants is free and travel-proven.
	if got := MaxTrainableGoal(MeleeLadder(), false); got != 75 {
		t.Fatalf("free ceiling = %d, want 75", got)
	}

	// The ceiling and PickRung must agree about where the ladder stops.
	if c := PickRung(MeleeLadder(), 75, 126, true); c.OK {
		t.Fatalf("level 75 should have no rung at all, got %q", c.Rung.Key)
	}
	if c := PickRung(MeleeLadder(), 74, 126, true); !c.OK {
		t.Fatalf("level 74 is the last level a rung covers and must resolve: %s", c.Why)
	}
}

// Only rungs PickRung would actually accept may raise the ceiling. Promising 90 on a rung no
// run can reach is the same terminal error wearing a different hat.
func TestMaxTrainableGoalCountsOnlyRungsPickRungWouldAccept(t *testing.T) {
	ladder := []Rung{
		{Key: "proven", FromLevel: 1, ToLevel: 50, TravelProven: true},
		{Key: "unproven", FromLevel: 50, ToLevel: 90, TravelProven: false},
		{Key: "membersonly", FromLevel: 50, ToLevel: 80, TravelProven: true, Members: true},
	}
	if got := MaxTrainableGoal(ladder, false); got != 50 {
		t.Fatalf("free ceiling = %d, want 50: the 90 rung is unproven and the 80 rung is members-only", got)
	}
	if got := MaxTrainableGoal(ladder, true); got != 80 {
		t.Fatalf("members ceiling = %d, want 80: the 90 rung is still unproven", got)
	}
	if got := MaxTrainableGoal(nil, true); got != 0 {
		t.Fatalf("an empty ladder has no ceiling, got %d", got)
	}
}
