package hostileevade

import (
	"os"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/hostileavoid"
)

func TestRouteStepsCountsDiagonalsAsOne(t *testing.T) {
	w := []pathfind.NavPoint{{X: 0, Z: 0}, {X: 3, Z: 3}, {X: 3, Z: 10}}
	if got := routeSteps(w); got != 10 {
		t.Fatalf("routeSteps = %d, want 10", got)
	}
	if routeSteps(nil) != 0 {
		t.Fatal("an empty route has steps")
	}
}

// The live case: the White wolf by Harry's shop at Catherby. Needs the real collision pack.
func TestWolfByHarrysShopIsWalledOff(t *testing.T) {
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	wolf := hostileavoid.NPC{X: 2842, Z: 3450}
	for _, at := range [][2]int{{2835, 3443}, {2836, 3441}, {2833, 3443}} {
		steps, walled := walledOff(pk, hostileavoid.Snapshot{X: at[0], Z: at[1]}, wolf, 9)
		if !walled {
			t.Errorf("at %v the wolf is %s away — want it walled off", at, steps)
		}
	}
	// Out in the open beside it, it is not.
	if steps, walled := walledOff(pk, hostileavoid.Snapshot{X: 2844, Z: 3452}, wolf, 9); walled {
		t.Errorf("beside the wolf (%s) reads as walled off", steps)
	}
}
