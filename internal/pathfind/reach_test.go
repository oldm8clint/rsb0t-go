package pathfind

import (
	"os"
	"testing"
)

// openPack is one mapsquare of open, fully connected ground at level 0: every tile walkable,
// every direction a legal step. Shaped exactly like livewalls_test.go's own fixture.
func openPack() *Pack {
	p := &Pack{Version: 2}
	row := new([256]uint16)
	row[0] = 1
	p.index[0][0] = row
	slot := levelSlot{exit: make([]byte, exitBytes), walk: make([]byte, walkBytes), wall: make([]byte, wallBytes)}
	for i := range slot.exit {
		slot.exit[i] = 255
	}
	for i := range slot.walk {
		slot.walk[i] = 255
	}
	p.slots = []levelSlot{slot}
	return p
}

// opposite is the reverse of each direction in astar.go's own DX/DZ table. It is written out
// rather than computed because the obvious arithmetic is wrong: the table pairs the four
// cardinals first and the four diagonals after, so (dir+4)%8 turns north into north-east.
var opposite = [8]int{2, 3, 0, 1, 6, 7, 4, 5}

// cutOffTile removes every legal step into and out of (x,z), leaving the tile itself walkable.
// That is the shape of a floor tile inside a walled room, which is the case that matters: the Al
// Kharid palace's interior is perfectly walkable ground, and a warrior standing on it can be one
// step away from the account and still have no route to it.
func cutOffTile(p *Pack, x, z int) {
	p.slots[0].exit[tileIndex(x, z)] = 0
	for dir := 0; dir < 8; dir++ {
		nx, nz := x+dx[dir], z+dz[dir]
		p.slots[0].exit[tileIndex(nx, nz)] &^= 1 << uint(opposite[dir])
	}
}

// makeSolid is the other shape: a tile nothing can stand on, such as a building's own footprint.
func makeSolid(p *Pack, x, z int) {
	idx := tileIndex(x, z)
	p.slots[0].walk[idx>>3] &^= 1 << uint(idx&7)
	cutOffTile(p, x, z)
}

// TestReachableWithinAcceptsOpenGround is the control: without it a check that answers "no" to
// everything would pass every test below.
func TestReachableWithinAcceptsOpenGround(t *testing.T) {
	p := openPack()
	if res := p.ReachableWithin(NavPoint{X: 20, Z: 20}, NavPoint{X: 24, Z: 23}, 24); !res.OK {
		t.Fatalf("open ground four tiles away is not reachable: %q", res.Reason)
	}
}

// TestReachableWithinRejectsATileOneStepAwayThroughAWall is the bug this file exists for,
// reduced to its geometry: the target is Chebyshev 1 away, which is why the fight loop skipped
// its approach walk and clicked it, and there is no route to it at all.
func TestReachableWithinRejectsATileOneStepAwayThroughAWall(t *testing.T) {
	p := openPack()
	cutOffTile(p, 31, 30)
	from := NavPoint{X: 32, Z: 30}
	to := NavPoint{X: 31, Z: 30}
	if d := chebyshev(from.X, from.Z, to.X, to.Z); d != 1 {
		t.Fatalf("the fixture is not the case under test: the target is %d tiles away, want 1", d)
	}
	if !p.Walkable(to.X, to.Z, to.Level) {
		t.Fatal("the fixture is not the case under test: the target tile must be walkable ground behind a wall")
	}
	if res := p.ReachableWithin(from, to, 24); res.OK {
		t.Fatalf("a walled-off tile one step away answered reachable, which is the bug: %v", res.Waypoints)
	}
}

// TestReachableWithinRoutesAroundAWallWhenThereIsAWayRound keeps the check from degenerating
// into "is it in a straight line": a target behind a wall with both ends open IS reachable, and
// answering otherwise would stop a run fighting things it can reach.
func TestReachableWithinRoutesAroundAWallWhenThereIsAWayRound(t *testing.T) {
	p := openPack()
	for z := 20; z <= 30; z++ {
		makeSolid(p, 40, z)
	}
	if res := p.ReachableWithin(NavPoint{X: 39, Z: 25}, NavPoint{X: 41, Z: 25}, 24); !res.OK {
		t.Fatalf("a target behind a wall with both ends open is not reachable: %q", res.Reason)
	}
}

// TestReachableWithinStaysInsideItsRadius is what makes the check cheap enough to ask about
// every candidate several times a minute: a route that would have to leave the box answers no
// rather than expanding the whole region the account is standing in.
func TestReachableWithinStaysInsideItsRadius(t *testing.T) {
	p := openPack()
	for z := 0; z <= 63; z++ {
		makeSolid(p, 40, z)
	}
	if res := p.ReachableWithin(NavPoint{X: 39, Z: 25}, NavPoint{X: 41, Z: 25}, 24); res.OK {
		t.Fatal("a target behind an unbroken wall answered reachable")
	}
	res := p.ReachableWithin(NavPoint{X: 10, Z: 10}, NavPoint{X: 10, Z: 40}, 8)
	if res.OK || res.Reason != "further away than the radius allows" {
		t.Fatalf("a target outside the radius answered %v (%q)", res.OK, res.Reason)
	}
}

// TestReachableWithinRefusesAnotherLevel pins the one answer that is not a search: a monster on
// another level is never reachable by a walk, whatever the tile distance says.
func TestReachableWithinRefusesAnotherLevel(t *testing.T) {
	p := openPack()
	if res := p.ReachableWithin(NavPoint{X: 20, Z: 20, Level: 0}, NavPoint{X: 20, Z: 21, Level: 1}, 24); res.OK {
		t.Fatal("a tile on another level answered reachable")
	}
}

// TestReachableWithinAnswersTheRealAlKharidPalaceTile is the live incident, pinned against the
// real collision pack: CombatTrainer's Al Kharid stand is 3288,3172 and the warrior it wedged
// three long runs on was at 3286,3171 — one Chebyshev step away, inside the palace wall, with
// the pathfinder already printing "no path from 3288,3172 to 3286,3171: unreachable" in the log
// while the fight loop clicked it anyway.
//
// Skips without the pack, as every other pack-backed test here does.
func TestReachableWithinAnswersTheRealAlKharidPalaceTile(t *testing.T) {
	path := os.Getenv(packEnv)
	if path == "" {
		t.Skip("set " + packEnv + " to run this against the real pack")
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("loading the pack: %v", err)
	}
	stand := NavPoint{X: 3288, Z: 3172}
	walled := NavPoint{X: 3286, Z: 3171}
	if res := p.ReachableWithin(stand, walled, 24); res.OK {
		t.Errorf("the palace tile 3286,3171 answered reachable from the stand: %v", res.Waypoints)
	}
	// The control, so the check above cannot pass by answering no to everything on this pack:
	// something next to the stand has to be reachable.
	reachable := 0
	for dir := 0; dir < 8; dir++ {
		at := NavPoint{X: stand.X + dx[dir], Z: stand.Z + dz[dir]}
		if p.ReachableWithin(stand, at, 24).OK {
			reachable++
		}
	}
	if reachable == 0 {
		t.Error("nothing next to the Al Kharid stand is reachable, so the check is answering no to everything")
	}
	t.Logf("%d of the 8 tiles around the stand are reachable; the palace tile is not", reachable)
}

// TestStepsCountsTheTilesWalkedNotTheWaypoints pins what Steps means. Waypoints are
// corner-compressed, so their COUNT is a measure of how bendy a route is and not of how long it
// is: the two are routinely off by a factor of three, which is exactly the confusion that made
// a caller reach for a straight line instead.
func TestStepsCountsTheTilesWalkedNotTheWaypoints(t *testing.T) {
	p := openPack()
	res := p.FindPath(NavPoint{X: 0, Z: 0}, NavPoint{X: 10, Z: 0})
	if !res.OK {
		t.Fatalf("expected a route across open ground: %s", res.Reason)
	}
	if got := res.Steps(); got != 10 {
		t.Fatalf("Steps() = %d over a straight 10-tile run, want 10 (waypoints: %d)", got, len(res.Waypoints))
	}
	// Diagonals are one step each, the same way chebyshev counts them, so a 10-by-10 move is
	// 10 steps and not 20.
	res = p.FindPath(NavPoint{X: 0, Z: 0}, NavPoint{X: 10, Z: 10})
	if !res.OK {
		t.Fatalf("expected a diagonal route: %s", res.Reason)
	}
	if got := res.Steps(); got != 10 {
		t.Fatalf("Steps() = %d over a 10-by-10 diagonal, want 10", got)
	}
}

// TestStepsIsZeroWithoutARoute keeps the "check OK first" contract honest: a failed search has
// no waypoints, so a caller that scores by Steps without looking at OK would rank an
// unreachable destination as the closest thing on the map.
func TestStepsIsZeroWithoutARoute(t *testing.T) {
	if got := (PathResult{Reason: "nope"}).Steps(); got != 0 {
		t.Fatalf("Steps() = %d on a failed result, want 0", got)
	}
}
