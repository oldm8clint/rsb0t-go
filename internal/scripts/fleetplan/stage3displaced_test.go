package fleetplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// TestStage3LeavesTheWildernessItDidNotWalkInto is the 2026-10-02 04:35 Rogues' Castle summon in the
// planner. 237 accounts were set down at 3282,3936 level 3, in the Wilderness at level 53, with no trip
// under way. Stage 3 read "in the Wilderness" as "on the way to the Mage Bank" and walked "to
// 3158,3950,0: unreachable" every thirteen seconds for twelve hours; the right answer for an account
// that did not pull the Ardougne lever is the trip's own way home, the Deserted Keep lever.
func TestStage3LeavesTheWildernessItDidNotWalkInto(t *testing.T) {
	roof := &tile.Tile{X: 3282, Z: 3936, Level: 3}
	if r := RegionOf(roof); r != Region3Wilderness {
		t.Fatalf("the castle roof is region %q, want the Wilderness", r)
	}
	for _, c := range []struct {
		name string
		trip Trip
		want Phase3
	}{
		{"displaced, no trip", Trip{}, Phase3ToKeep},
		{"displaced, a stale Bought flag", Trip{Bought: true}, Phase3ToKeep},
		{"on the way in", Trip{Active: true}, Phase3ToBank},
		{"on the way out", Trip{Active: true, Bought: true}, Phase3ToKeep},
	} {
		snap := Stage3Snapshot{Tile: roof, Trip: c.trip}
		if got := DecideStage3Phase(snap); got != c.want {
			t.Errorf("%s: phase %s, want %s", c.name, got, c.want)
		}
	}
	// The same at the Keep landing the trip itself arrives on: an active trip still goes in.
	keep := &tile.Tile{X: KeepLand.X, Z: KeepLand.Z}
	if got := DecideStage3Phase(Stage3Snapshot{Tile: keep, Trip: Trip{Active: true}}); got != Phase3ToBank {
		t.Errorf("an active trip at the Keep landing chose %s, want %s", got, Phase3ToBank)
	}
	if got := DecideStage3Phase(Stage3Snapshot{Tile: keep}); got != Phase3ToKeep {
		t.Errorf("an account at the Keep landing with no trip chose %s, want %s", got, Phase3ToKeep)
	}
}

// A dropped-in account in the Mage Bank is told to deposit and leave, not to buy a trip's worth of runes
// with coins it was never planned to draw: DepositAtGundai (with the worn-items fix) and the climb out
// follow from Bought.
func TestStage3MageBankPhasesAreUnchangedForATripInProgress(t *testing.T) {
	bank := &tile.Tile{X: 2543, Z: 4713}
	if got := DecideStage3Phase(Stage3Snapshot{Tile: bank, Trip: Trip{Active: true}}); got != Phase3Withdraw {
		t.Errorf("a trip arriving in the Mage Bank chose %s, want %s", got, Phase3Withdraw)
	}
	if got := DecideStage3Phase(Stage3Snapshot{Tile: bank, Trip: Trip{Active: true, Bought: true}, PackOnlyBlade: true}); got != Phase3ClimbOut {
		t.Errorf("a finished trip chose %s, want %s", got, Phase3ClimbOut)
	}
}
