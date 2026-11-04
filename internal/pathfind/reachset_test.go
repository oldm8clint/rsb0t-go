package pathfind

import (
	"os"
	"testing"
)

// routeLen is the tiles a corner-compressed route walks, diagonal steps counting one.
func routeLen(waypoints []NavPoint) int {
	n := 0
	for i := 1; i < len(waypoints); i++ {
		n += chebyshev(waypoints[i-1].X, waypoints[i-1].Z, waypoints[i].X, waypoints[i].Z)
	}
	return n
}

// walledPack is open ground with a wall of solid tiles across x=30 (a gap at z=25) and one
// walled-off tile, so some targets are reachable only the long way round and some not at all.
func walledPack() *Pack {
	p := openPack()
	for z := 0; z < 64; z++ {
		if z != 25 {
			makeSolid(p, 30, z)
		}
	}
	cutOffTile(p, 40, 40)
	makeSolid(p, 45, 45)
	return p
}

// compareReach holds a Reach to FindPathAvoiding target by target: the same answer to "is there a
// route", and for a reachable walkable target the same number of steps. The Reach covers the square
// of the given radius, which avoid must also refuse the outside of (as the flee's window does).
func compareReach(t *testing.T, p *Pack, from NavPoint, radius int, avoid func(x, z, level int) bool, targets []NavPoint) (reached int) {
	t.Helper()
	r := p.ReachFrom(from, radius, avoid)
	for _, to := range targets {
		want := p.FindPathAvoiding(from, to, avoid)
		if got := r.CanReach(to); got != want.OK {
			t.Fatalf("CanReach(%v) = %v, FindPathAvoiding ok = %v (%s)", to, got, want.OK, want.Reason)
		}
		if !want.OK {
			continue
		}
		reached++
		if p.Walkable(to.X, to.Z, to.Level) {
			steps, ok := r.Steps(to.X, to.Z, to.Level)
			// The route starts on the snapped start, which is the start itself on walkable ground.
			if !ok || steps != routeLen(want.Waypoints) {
				t.Fatalf("Steps(%v) = %d %v, the route walks %d", to, steps, ok, routeLen(want.Waypoints))
			}
		}
	}
	return reached
}

func TestReachAnswersWhatFindPathAnswers(t *testing.T) {
	p := walledPack()
	from := NavPoint{X: 20, Z: 20}
	var targets []NavPoint
	for x := 10; x < 55; x += 2 {
		for z := 10; z < 55; z += 2 {
			targets = append(targets, NavPoint{X: x, Z: z})
		}
	}
	if n := compareReach(t, p, from, 64, nil, targets); n < len(targets)/2 || n == len(targets) {
		t.Fatalf("%d of %d targets reachable: the fixture must have both reachable and unreachable ground", n, len(targets))
	}
	// The same with an avoid predicate: a 14-tile window and a keep-out square.
	avoid := func(x, z, level int) bool {
		return chebyshev(x, z, from.X, from.Z) > 14 || (x >= 24 && x <= 26 && z >= 18 && z <= 30)
	}
	compareReach(t, p, from, 14, avoid, targets)
}

// The flood does not enter a tile the predicate refuses, and a target that is only reachable
// through one is therefore not reachable, exactly as in FindPathAvoiding.
func TestReachHonoursAvoidAndCountsStepsAroundWalls(t *testing.T) {
	p := walledPack()
	from := NavPoint{X: 20, Z: 25}
	free := p.ReachFrom(from, 64, nil)
	if steps, ok := free.Steps(40, 25, 0); !ok || steps != 20 {
		t.Fatalf("through the gap: %d %v, want 20 steps", steps, ok)
	}
	plug := func(x, z, level int) bool { return x == 30 && z == 25 }
	blocked := p.ReachFrom(from, 64, plug)
	if _, ok := blocked.Steps(40, 25, 0); ok {
		t.Fatal("the wall's only gap is avoided and the far side is still reachable")
	}
	if blocked.CanReach(NavPoint{X: 40, Z: 25}) {
		t.Fatal("CanReach reports ground behind an avoided gap")
	}
	if blocked.Size() >= free.Size() {
		t.Fatalf("avoiding the gap did not shrink the flood (%d >= %d)", blocked.Size(), free.Size())
	}
	// A walkable tile with no exits is a component of one, and nothing outside it reaches it.
	if p.ReachFrom(from, 64, nil).CanReach(NavPoint{X: 40, Z: 40}) {
		t.Fatal("a walled-off tile is reachable")
	}
}

func TestReachFromAnUnwalkableStartIsEmpty(t *testing.T) {
	p := walledPack()
	r := p.ReachFrom(NavPoint{X: 5, Z: 5, Level: 2}, 10, nil) // no such level in the fixture
	if r.Size() != 0 || r.CanReach(NavPoint{X: 5, Z: 5, Level: 2}) {
		t.Fatalf("an unwalkable start flooded %d tiles", r.Size())
	}
	if _, ok := r.Steps(5, 5, 2); ok {
		t.Fatal("an empty Reach has steps")
	}
}

// On the real map, in the ice dungeon the live fleet was stuck in, every target in a large square
// agrees with FindPathAvoiding, with and without a window predicate.
func TestReachMatchesFindPathOnTheRealPack(t *testing.T) {
	path := os.Getenv(packEnv)
	if path == "" {
		t.Skipf("set %s to the collision pack (collision.lcnav or .gz) to run this test", packEnv)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	from := NavPoint{X: 3041, Z: 9588}
	var targets []NavPoint
	for x := 2990; x <= 3090; x += 3 {
		for z := 9540; z <= 9620; z += 3 {
			targets = append(targets, NavPoint{X: x, Z: z})
		}
	}
	window := func(x, z, level int) bool { return chebyshev(x, z, from.X, from.Z) > 40 }
	if n := compareReach(t, p, from, 40, window, targets); n == 0 || n == len(targets) {
		t.Fatalf("%d of %d targets reachable: expected both reachable and unreachable ground", n, len(targets))
	}
}

// SearchStats counts exactly the A* searches and expansions run between two readings, which is what
// lets the combat flee be held to a bound in a test and measured in a log.
func TestSearchStatsCountSearchesAndExpansions(t *testing.T) {
	p := openPack()
	s0, e0 := SearchStats()
	r := p.FindPath(NavPoint{X: 10, Z: 10}, NavPoint{X: 20, Z: 25})
	s1, e1 := SearchStats()
	if !r.OK || s1-s0 != 1 || e1-e0 != int64(r.Expanded) || r.Expanded == 0 {
		t.Fatalf("one search: %d searches, %d expanded (the route says %d, ok %v)", s1-s0, e1-e0, r.Expanded, r.OK)
	}
	// A flood is not a search and costs none.
	p.ReachFrom(NavPoint{X: 10, Z: 10}, 20, nil)
	if s2, e2 := SearchStats(); s2 != s1 || e2 != e1 {
		t.Fatalf("a flood counted as %d searches, %d expansions", s2-s1, e2-e1)
	}
	// A search that fails counts what it expanded before saying so.
	w := walledPack()
	f0, g0 := SearchStats()
	res := w.FindPath(NavPoint{X: 20, Z: 20}, NavPoint{X: 40, Z: 40}) // the walled-off tile
	if res.OK {
		t.Fatal("a route to a walled-off tile")
	}
	if f1, g1 := SearchStats(); f1-f0 != 1 || g1-g0 != int64(res.Expanded) || res.Expanded == 0 {
		t.Fatalf("a failed search counted %d searches, %d expansions (it says %d)", f1-f0, g1-g0, res.Expanded)
	}
}

// The square is a hard edge: ground outside it is never entered, and a target outside is not
// reachable, however open the map is there. Each lists exactly the reached tiles, in a fixed order.
func TestReachStopsAtItsSquareAndListsTilesInOrder(t *testing.T) {
	p := openPack()
	from := NavPoint{X: 30, Z: 30}
	r := p.ReachFrom(from, 4, nil)
	if want := 9 * 9; r.Size() != want {
		t.Fatalf("a radius-4 flood of open ground reached %d tiles, want %d", r.Size(), want)
	}
	if _, ok := r.Steps(34, 30, 0); !ok {
		t.Fatal("the square's edge is not reached")
	}
	if _, ok := r.Steps(35, 30, 0); ok || r.CanReach(NavPoint{X: 45, Z: 30}) {
		t.Fatal("ground outside the square was reached")
	}
	if steps, _ := r.Steps(34, 34, 0); steps != 4 {
		t.Fatalf("the far corner is %d steps off, want 4 (diagonals count one)", steps)
	}
	n, lastZ, lastX := 0, -1, -1
	r.Each(func(x, z, level, steps int) bool {
		if z < lastZ || (z == lastZ && x <= lastX) {
			t.Fatalf("tile %d,%d came after %d,%d", x, z, lastX, lastZ)
		}
		lastX, lastZ = x, z
		n++
		return true
	})
	if n != r.Size() {
		t.Fatalf("Each listed %d tiles of %d", n, r.Size())
	}
	stopped := 0
	r.Each(func(x, z, level, steps int) bool { stopped++; return stopped < 3 })
	if stopped != 3 {
		t.Fatalf("Each did not stop when told to (%d calls)", stopped)
	}
}

// InteractSteps is the walk to the nearest tile an interaction with the target is accepted from: for a walkable target its own tile
// or the one beside it, for a solid one the tile beside it (which Steps of the target itself cannot say), the long way round a wall,
// and -1 for a target nothing can touch.
func TestInteractStepsCountsTheWalkToWhereTheTargetCanBeTouched(t *testing.T) {
	p := walledPack()
	from := NavPoint{X: 20, Z: 20}
	r := p.ReachFrom(from, 40, nil)

	// A walkable target on the near side: the tile itself or the neighbour on the way, never more than the walk to the tile.
	near := NavPoint{X: 25, Z: 20}
	own, ok := r.Steps(near.X, near.Z, near.Level)
	if !ok {
		t.Fatal("fixture: the near target is not reachable")
	}
	if got := r.InteractSteps(near); got < 0 || got > own || got < own-1 {
		t.Errorf("InteractSteps of a walkable target %d steps away = %d, want %d or %d", own, got, own-1, own)
	}

	// The same distance as the crow flies, across the wall: the walk goes through the gap at z=25.
	low := p.ReachFrom(NavPoint{X: 20, Z: 10}, 40, nil)
	far := NavPoint{X: 35, Z: 10}
	if direct := chebyshev(20, 10, far.X, far.Z); low.InteractSteps(far) <= direct {
		t.Errorf("InteractSteps across the wall = %d, not more than the %d tiles of the straight line", low.InteractSteps(far), direct)
	}

	// A solid tile has no steps of its own, and is touched from the tile beside it.
	solid := NavPoint{X: 45, Z: 45}
	if _, ok := r.Steps(solid.X, solid.Z, solid.Level); ok {
		t.Fatal("fixture: the solid tile is reachable")
	}
	if got := r.InteractSteps(solid); got <= 0 {
		t.Errorf("InteractSteps of a solid tile = %d, want the walk to the tile beside it", got)
	}

	// A tile walled in by solid ground is touched from nowhere, and another level or an empty flood has no answer.
	sealed := walledPack()
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			makeSolid(sealed, 52+dx, 52+dz)
		}
	}
	if got := sealed.ReachFrom(from, 40, nil).InteractSteps(NavPoint{X: 52, Z: 52}); got != -1 {
		t.Errorf("InteractSteps of a sealed target = %d, want -1", got)
	}
	if got := r.InteractSteps(NavPoint{X: 25, Z: 20, Level: 1}); got != -1 {
		t.Errorf("InteractSteps of another level = %d, want -1", got)
	}
	if got := p.ReachFrom(NavPoint{X: 5, Z: 5, Level: 2}, 10, nil).InteractSteps(near); got != -1 {
		t.Errorf("InteractSteps from an empty flood = %d, want -1", got)
	}
}
