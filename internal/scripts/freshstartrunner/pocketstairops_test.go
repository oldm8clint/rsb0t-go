package freshstartrunner

import (
	"testing"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/tile"
)

// TestPocketEscapeGoesDownFromARaisedFloor is Rogues' Castle's second floor (2026-10-02): the stair there is
// one loc offering Climb, Climb-up and Climb-down, and the old always-up order picked option 2 and climbed
// the account back to the roof it had just come down from. From a raised floor the pocket escape must pick
// Climb-down (option 3); from the ground it still picks Climb-up, which is what a basement's ladder offers.
func TestPocketEscapeGoesDownFromARaisedFloor(t *testing.T) {
	types := stairTypes{
		1739: {ID: 1739, Name: "Staircase", Width: 1, Length: 1, Ops: [5]string{"Climb", "Climb-up", "Climb-down"}},
	}
	ix, err := locindex.New([]locindex.Loc{{ID: 1739, X: 3280, Z: 3936, Level: 2, Shape: 10}})
	if err != nil {
		t.Fatal(err)
	}
	from := tile.Tile{X: 3282, Z: 3936, Level: 2}

	down, name := pickDirectionalMatch(ix, types, nil, from, stairNames, pocketStairOps(from.Level), 30)
	if down == nil || name != "Staircase" || down.Op != 3 {
		t.Fatalf("from the second floor picked %+v (%q), want option 3, Climb-down", down, name)
	}
	if got := types[1739].Ops[down.Op-1]; got != "Climb-down" {
		t.Fatalf("option %d is %q", down.Op, got)
	}

	// The control: the old order, which the ground floor and every basement still use.
	up, _ := pickDirectionalMatch(ix, types, nil, from, stairNames, pocketStairOps(0), 30)
	if up == nil || up.Op != 2 {
		t.Fatalf("the ground-level order picked %+v, want option 2, Climb-up", up)
	}
	if &pocketStairOps(0)[0] != &stairOpsUp[0] || &pocketStairOps(3)[0] != &stairOpsDown[0] {
		t.Error("pocketStairOps must hand back stairOpsUp from the ground and stairOpsDown from any raised floor")
	}
}
