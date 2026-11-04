package randomevents

import (
	"testing"

	"rsb0t-go/internal/pathfind"
)

// fleeFromCombatWalking itself needs a real actions.Session and pathfind.Pack (real
// FindPath routing against real collision data) to exercise meaningfully -- this package's
// own established boundary (see handle_test.go's doc comment, which gives the identical
// reasoning for walkableFlee/walkTo/HandleHazard/HandleEvade) is to not fake those, since a
// stub FindPath/Session would prove nothing about real routing or server behavior. What IS
// pure and tested directly here is the decision logic fleeFromCombatWalking's loop leans
// on: whether to turn running on, and whether a given tile counts as "arrived".

func TestWantRunForFleeAlreadyOnIsNoop(t *testing.T) {
	if wantRunForFlee(true, 99, true) {
		t.Fatal("wantRunForFlee(runOn=true, ...) must report false — nothing to do if run is already on")
	}
}

func TestWantRunForFleeEnablesWithAnyEnergy(t *testing.T) {
	// Ports RunManager.ts's own ENERGY_MIN_ATTACKED=1: under attack, any energy at all is
	// worth spending to break off, unlike the normal cruising floor.
	if !wantRunForFlee(false, 1, true) {
		t.Fatal("wantRunForFlee(runOn=false, energy=1) must report true — fleeing should spend down to the last point of energy")
	}
	if !wantRunForFlee(false, 100, true) {
		t.Fatal("wantRunForFlee(runOn=false, energy=100) must report true")
	}
}

func TestWantRunForFleeNoEnergyOrUnknownRefusesRun(t *testing.T) {
	if wantRunForFlee(false, 0, true) {
		t.Fatal("wantRunForFlee(energy=0) must report false — zero energy can't run")
	}
	if wantRunForFlee(false, 100, false) {
		t.Fatal("wantRunForFlee(energyOK=false) must report false — energy unknown, don't assume it's safe to spend")
	}
}

func TestReachedFleeDestWithinTolerance(t *testing.T) {
	dest := pathfind.NavPoint{X: 100, Z: 100, Level: 0}
	cases := []struct {
		name        string
		x, z, level int
		want        bool
	}{
		{"exact tile", 100, 100, 0, true},
		{"within tolerance", 98, 101, 0, true},
		{"just outside tolerance", 97, 100, 0, false},
		{"right level, far away", 200, 200, 0, false},
		{"same tile, wrong level", 100, 100, 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reachedFleeDest(c.x, c.z, c.level, dest); got != c.want {
				t.Errorf("reachedFleeDest(%d,%d,%d) = %v, want %v", c.x, c.z, c.level, got, c.want)
			}
		})
	}
}

func TestReachedFleeDestToleranceIsChebyshev(t *testing.T) {
	dest := pathfind.NavPoint{X: 0, Z: 0, Level: 0}
	// Chebyshev distance, not Euclidean/Manhattan: (2,2) is distance 2, still within the
	// fleeWalkArrivalTolerance=2 floor, even though Manhattan distance would be 4.
	if !reachedFleeDest(2, 2, 0, dest) {
		t.Fatal("reachedFleeDest((2,2) from (0,0)) = false, want true (Chebyshev distance 2 is within tolerance)")
	}
	if reachedFleeDest(3, 0, 0, dest) {
		t.Fatal("reachedFleeDest((3,0) from (0,0)) = true, want false (Chebyshev distance 3 exceeds tolerance)")
	}
}
