package webwalk

import (
	"os"
	"sync"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
)

var (
	exitPackOnce sync.Once
	exitPack     *pathfind.Pack
	exitPackErr  error
)

func exitCollisionPack(t testing.TB) *pathfind.Pack {
	t.Helper()
	path := os.Getenv(packEnv)
	if path == "" {
		t.Skipf("%s not set", packEnv)
	}
	exitPackOnce.Do(func() { exitPack, exitPackErr = pathfind.Load(path) })
	if exitPackErr != nil {
		t.Fatal(exitPackErr)
	}
	return exitPack
}

func findExit(exits []randomevents.LevelExit, locID int, standX, standZ, standLevel int) (randomevents.LevelExit, bool) {
	for _, e := range exits {
		if e.LocID == locID && e.Stand == (randomevents.StrandTile{X: standX, Z: standZ, Level: standLevel}) {
			return e, true
		}
	}
	return randomevents.LevelExit{}, false
}

// The Guardian takes the walker's own staircases: the Wizards' Tower's, which the 17 Oct 2026 Swarm death was one tile
// of top room from, are in the exit list with the options the walker clicks, and the ladder DOWN into Sedridor's
// basement is not (a way deeper into a dead end is no way out).
func TestLevelExitsHoldTheWizardsTowerStairs(t *testing.T) {
	exits := LevelExits()
	if len(exits) < 50 {
		t.Fatalf("only %d level exits in the graph", len(exits))
	}
	top, ok := findExit(exits, 1740, 3104, 3161, 2)
	if !ok || top.Label != "Climb-down" || top.LocX != 3104 || top.LocZ != 3160 || top.Land != (randomevents.StrandTile{X: 3103, Z: 3158, Level: 1}) {
		t.Fatalf("the top room's staircase down = %+v (found %v)", top, ok)
	}
	if mid, ok := findExit(exits, 1739, 3103, 3161, 1); !ok || mid.Label != "Climb-down" || mid.Land.Level != 0 {
		t.Fatalf("the middle floor's staircase down = %+v (found %v)", mid, ok)
	}
	if up, ok := findExit(exits, 1738, 3105, 3160, 0); !ok || up.Label != "Climb-up" || up.Land.Level != 1 {
		t.Fatalf("the ground floor's staircase up = %+v (found %v)", up, ok)
	}
	for _, e := range exits {
		if e.Stand.Level == 0 && e.Stand.Z < 4000 && e.Land.Z > 9000 {
			t.Errorf("a way DOWN into a dungeon is a level exit: %+v", e)
		}
		if e.Label == "" || e.LocID < 0 {
			t.Errorf("an exit with nothing to operate: %+v", e)
		}
	}
	// A ladder out of a dungeon is: Sedridor's basement ladder up.
	for _, e := range exits {
		if e.Stand.Z > 9000 && e.Land.Z < 4000 {
			return
		}
	}
	t.Error("no ladder out of a dungeon is a level exit")
}

// The Guardian's plan from every tile of the tower's top room and middle floor, on the real map with the graph's own
// exits: the Swarm's leash (15) is longer than either floor, and the staircase down is the plan, a few steps away,
// from every one of them.
func TestEveryTileOfTheTowerFloorsHasAStaircaseDown(t *testing.T) {
	pack := exitCollisionPack(t)
	floors := []struct {
		level      int
		start      pathfind.NavPoint
		wantLocID  int
		maxSteps   int
		minTiles   int
		wantLabels string
	}{
		{2, pathfind.NavPoint{X: 3103, Z: 3162, Level: 2}, 1740, 8, 30, "Climb-down"},
		{1, pathfind.NavPoint{X: 3104, Z: 3162, Level: 1}, 1739, 8, 28, "Climb-down"},
	}
	for _, f := range floors {
		reach := pack.ReachFrom(f.start, 20, func(x, z, level int) bool { return level != f.level })
		tiles := 0
		reach.Each(func(x, z, level, steps int) bool {
			tiles++
			// The Swarm one tile off, wherever the account stands: take any walkable neighbour for it.
			spawnX, spawnZ := x+1, z
			if !pack.Walkable(spawnX, spawnZ, level) {
				spawnX, spawnZ = x, z+1
			}
			exit, n, ok := randomevents.LevelExitForLab(pack, pathfind.NavPoint{X: x, Z: z, Level: level}, spawnX, spawnZ, 15)
			if !ok {
				t.Errorf("no level exit from %d,%d,%d", x, z, level)
				return true
			}
			if exit.LocID != f.wantLocID || exit.Label != f.wantLabels || n > f.maxSteps {
				t.Errorf("from %d,%d,%d the exit is %s on loc %d, %d steps; want %s on loc %d within %d", x, z, level, exit.Label, exit.LocID, n, f.wantLabels, f.wantLocID, f.maxSteps)
			}
			return true
		})
		if tiles < f.minTiles {
			t.Errorf("level %d: only %d tiles reached from %v", f.level, tiles, f.start)
		}
		t.Logf("level %d: %d tiles, each with a staircase down", f.level, tiles)
	}
}

// The Guardian plans to an exit's stand tile on the collision pack, so an exit whose stand is not walkable there is simply never
// chosen. This keeps the data honest: almost every exit the graph hands over has a walkable stand and landing.
func TestLevelExitStandsAndLandingsAreWalkableOnTheRealPack(t *testing.T) {
	pack := exitCollisionPack(t)
	exits := LevelExits()
	bad := 0
	for _, e := range exits {
		if !pack.Walkable(e.Stand.X, e.Stand.Z, e.Stand.Level) || !pack.Walkable(e.Land.X, e.Land.Z, e.Land.Level) {
			bad++
			t.Logf("not walkable: %s on loc %d at %d,%d, stand %v, landing %v", e.Label, e.LocID, e.LocX, e.LocZ, e.Stand, e.Land)
		}
	}
	t.Logf("%d level exits, %d with a stand or landing the pack does not walk", len(exits), bad)
	if bad*10 > len(exits) {
		t.Errorf("%d of %d level exits have an unwalkable stand or landing", bad, len(exits))
	}
}
