package fleetrunner

import (
	"os"
	"sync"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

var exitData struct {
	once sync.Once
	pk   *pathfind.Pack
	pl   *webwalk.Planner
	err  error
}

func exitPlanner(t *testing.T) (*pathfind.Pack, *webwalk.Planner) {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	exitData.once.Do(func() {
		pk, err := pathfind.Load(path)
		if err != nil {
			exitData.err = err
			return
		}
		g, err := webwalk.Default()
		if err != nil {
			exitData.err = err
			return
		}
		exitData.pk, exitData.pl = pk, webwalk.NewPlanner(pk, g)
	})
	if exitData.err != nil {
		t.Fatal(exitData.err)
	}
	return exitData.pk, exitData.pl
}

// The ground nothing holds is never far: from the tile the 17 Oct account died on (2509,3106, one tile inside the last row of its AFK
// zone, between the Ogres' spawns) and from every held tile of the Yanille pen's inside, the nearest refuge is a short walk.
func TestARefugeIsAShortWalkFromAnyHeldTileOfTheYanillePen(t *testing.T) {
	pk, _ := exitPlanner(t)
	pen := fleetplan.YanilleWestOgres
	grid := PenHeld(pen)
	held := func(x, z int) bool { return grid.Holds(x, z, 0) }
	spot := func(t tile.Tile) bool { return pen.OnSafeSpot(&t) }
	died := tile.Tile{X: 2509, Z: 3106}
	if !grid.Holds(died.X, died.Z, 0) {
		t.Fatalf("the death tile %v is not held by the pen's threats", died)
	}
	path, ok := fleetplan.NearestRefuge(died, held, PackCanStep(pk), spot, RefugeMaxSteps)
	if !ok || len(path) > 8 || !spot(path[len(path)-1]) {
		t.Fatalf("from the death tile: refuge path %v ok=%v, want a walk of a few steps that ends on one of the pen's safespots", path, ok)
	}
	t.Logf("from %v: %d steps to %v (a safespot: %v)", died, len(path), path[len(path)-1], spot(path[len(path)-1]))
	far, checked := 0, 0
	for x := pen.Stand.X - 20; x <= pen.Stand.X+20; x++ {
		for z := pen.Stand.Z - 20; z <= pen.Stand.Z+20; z++ {
			from := tile.Tile{X: x, Z: z}
			// The Combat Training Camp's compound (z 3085-3092) is the penned Ogres' own: every walkable tile of it is held, and a
			// refuge is not a walk of 24 steps from it. An account there leaves the pen (returnToSpot).
			if !grid.Holds(x, z, 0) || !pk.Walkable(x, z, 0) || pk.ExitMask(x, z, 0) == 0 || z < 3093 {
				continue
			}
			checked++
			p, ok := fleetplan.NearestRefuge(from, held, PackCanStep(pk), spot, RefugeMaxSteps)
			if !ok {
				far++
				continue
			}
			if last := p[len(p)-1]; held(last.X, last.Z) {
				t.Fatalf("from %v the refuge %v is held", from, last)
			}
		}
	}
	if checked < 100 {
		t.Fatalf("only %d held tiles checked", checked)
	}
	t.Logf("%d held walkable tiles of the pen's inside, %d with no refuge within %d steps", checked, far, RefugeMaxSteps)
	if far*20 > checked {
		t.Errorf("%d of %d held tiles have no refuge within %d steps", far, checked, RefugeMaxSteps)
	}
}
