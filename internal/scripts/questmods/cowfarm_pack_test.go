package questmods

// The Lumbridge cow pasture against the real data (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX, RSB0T_CONFIG_JAG, and RSB0T_CONTENT for the
// cows' spawns; each test skips without them): the pen is closed, its gate is the way in, the farm's stand, its walks' end tiles and
// its beat are inside, and every cow's spawn is a walk over open ground from them. The old farm was tested only from the pasture's
// middle and walked to a tile on the road.

import (
	"slices"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// penBox is the pen's bounding box (the fences, east and west x 3253 to 3265, south z 3255, north z 3298).
func inPenBox(t tile.Tile) bool { return t.X >= 3242 && t.X <= 3265 && t.Z >= 3255 && t.Z <= 3298 }

// The farm's tiles are floor inside the pen, the beat's stops and every cow's spawn are a walk from the stand, and the spawns the plan
// lists are the content's.
func TestCowPastureStandBeatAndSpawnsAreInsideThePen(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	stand := questplan.CowPastureStand
	for name, tl := range map[string]tile.Tile{"stand": stand, "demon's field": questplan.DemonCowField, "the rungs' field": CowField} {
		if !d.Pack.Walkable(tl.X, tl.Z, 0) {
			t.Errorf("%s %v is not floor", name, tl)
		}
		if !g.inside(tl) {
			t.Errorf("%s %v is not inside the pen", name, tl)
		}
	}
	for i, stop := range questplan.CowPastureSweep {
		if !d.Pack.Walkable(stop.X, stop.Z, 0) {
			t.Errorf("stop %d of the beat, %v, is not floor", i, stop)
		}
		if !g.inside(stop) {
			t.Errorf("stop %d of the beat, %v, is not inside the pen", i, stop)
		}
	}
	for _, sp := range questplan.CowPastureSpawns {
		if n := g.stepsTo(stand, sp); n < 0 {
			t.Errorf("the cow spawned at %v is not a walk over open ground from the stand %v", sp, stand)
		}
		// And from every stop of the beat that is within the window of it.
		for i, stop := range questplan.CowPastureSweep {
			if tile.Chebyshev(stop, sp) <= demonCowWindow-2 {
				if n := g.stepsTo(stop, sp); n < 0 {
					t.Errorf("the cow spawned at %v is not a walk from stop %d of the beat, %v", sp, i, stop)
				}
			}
		}
	}

	var content []tile.Tile
	for _, sp := range demonCowSpawns(t) {
		if inPenBox(sp) {
			content = append(content, sp)
		}
	}
	got := slices.Clone(questplan.CowPastureSpawns)
	less := func(a, b tile.Tile) int {
		if a.X != b.X {
			return a.X - b.X
		}
		return a.Z - b.Z
	}
	slices.SortFunc(got, less)
	slices.SortFunc(content, less)
	if !slices.Equal(got, content) {
		t.Errorf("the plan lists the pen's cows as %v, the content's maps spawn %v", got, content)
	}
}

// The pen is closed and the gate is its way in. The tiles the old farm's walks ended on (within 8 of the middle, within 12 of it) are
// on the road side, and from there no cow is a walk away over open ground; with the gate's two leaves open every one is, and the farm's
// stand is.
func TestCowPastureIsAClosedPenWhoseGateIsTheWayIn(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	stand := questplan.CowPastureStand

	// The two leaves are where the plan says, closed in the pack, openable ("Gate", op Open).
	var edits []pathfind.WallEdit
	for _, gate := range questplan.CowPastureGates {
		found := false
		for _, l := range d.Locs.At(gate.X, gate.Z, 0) {
			info, ok := d.Config.Loc(l.ID)
			if !ok || info.Name != "Gate" || !info.BlockWalk || l.Shape != 0 {
				continue
			}
			if !slices.Contains(info.Ops[:], "Open") {
				t.Errorf("the gate loc %d at %v has ops %v, not Open", l.ID, gate, info.Ops)
			}
			edits = append(edits, pathfind.WallEdit{X: l.X, Z: l.Z, Level: l.Level, Shape: l.Shape, Angle: l.Angle})
			found = true
		}
		if !found {
			t.Errorf("no closed Gate at %v in the loc index", gate)
		}
	}
	open := d.Pack.WithWalls(edits)

	// The road side: the tiles the farm's old walks ended on, and every floor tile within 12 of the middle that is not in the pen.
	field := questplan.DemonCowField
	var road []tile.Tile
	for x := field.X - 12; x <= field.X+12; x++ {
		for z := field.Z - 12; z <= field.Z+12; z++ {
			tl := tile.Tile{X: x, Z: z}
			// West of the straight fence (x 3252|3253, z 3248 to 3271); north of it the diagonal fence leaves the pen's own ground at x 3252.
			if d.Pack.Walkable(x, z, 0) && x <= 3252 && z <= 3271 {
				road = append(road, tl)
			}
		}
	}
	for _, seen := range []tile.Tile{{X: 3252, Z: 3267}, {X: 3251, Z: 3268}, {X: 3252, Z: 3266}} {
		if tile.Chebyshev(seen, field) > 8 {
			t.Errorf("%v is %d from the middle: the Demon Slayer farm's walk (within 8) would not stop there", seen, tile.Chebyshev(seen, field))
		}
		if !slices.Contains(road, seen) {
			t.Errorf("%v is not a road tile", seen)
		}
	}
	if len(road) < 50 {
		t.Fatalf("only %d road tiles west of the fence within 12 of the middle", len(road))
	}
	for _, from := range road {
		if g.inside(from) {
			t.Errorf("the road tile %v reads as inside the pen", from)
			continue
		}
		for _, sp := range questplan.CowPastureSpawns {
			if tile.Chebyshev(from, sp) <= demonCowSearch {
				if n := g.stepsTo(from, sp); n >= 0 {
					t.Errorf("the cow spawned at %v is %d steps from the road tile %v over the shut pack", sp, n, from)
				}
			}
		}
	}

	// With the gate open the stand is a walk from the road tiles near the gate, and the route crosses at a leaf.
	for _, from := range []tile.Tile{{X: 3252, Z: 3267}, {X: 3251, Z: 3268}, {X: 3252, Z: 3266}, {X: 3247, Z: 3262}} {
		res := open.FindPath(pathfind.NavPoint{X: from.X, Z: from.Z}, pathfind.NavPoint{X: stand.X, Z: stand.Z})
		if !res.OK {
			t.Errorf("no route from %v to the stand with the gate open: %s", from, res.Reason)
			continue
		}
		crossed := false
		for _, wp := range res.Waypoints {
			if wp.X >= 3253 {
				crossed = true
			}
		}
		if !crossed {
			t.Errorf("the route from %v to the stand does not cross the fence: %v", from, res.Waypoints)
		}
	}
	// From the Al Kharid toll gate's landing, where the log's walks to the middle began (21:35), the planner's own route is as long as
	// the live log's: the crossing is the gate at 3253,3267.
	res := open.FindPath(pathfind.NavPoint{X: 3266, Z: 3228}, pathfind.NavPoint{X: stand.X, Z: stand.Z})
	if !res.OK || res.Steps() > 80 {
		t.Errorf("from the toll gate's landing to the stand: ok %v, %d steps", res.OK, res.Steps())
	}
	// And the other way: with the gate open every spawn is a walk from the road, which is what the farm has once it is through.
	reach := open.ReachFrom(pathfind.NavPoint{X: 3252, Z: 3267}, demonCowWindow, nil)
	for _, sp := range questplan.CowPastureSpawns {
		if tile.Chebyshev(sp, tile.Tile{X: 3252, Z: 3267}) > demonCowWindow-2 {
			continue
		}
		if n := interactSteps(open, reach, tile.Tile{X: 3252, Z: 3267}, sp); n < 0 {
			t.Errorf("with the gate open the cow spawned at %v is not a walk from the gate's road side", sp)
		}
	}
}

// A walk into the pen may end on any tile within CowPastureEnterRadius of the stand, and every one of them is inside; the walks the old
// farms asked for (within 8 and within 12 of the middle) could end outside it. This is the test that would have caught the fault.
func TestCowPastureEnterRadiusIsInsideAndTheOldRadiiAreNot(t *testing.T) {
	d := csDeps(t)
	g := &cowGround{pk: d.Pack}
	stand := questplan.CowPastureStand
	r := questplan.CowPastureEnterRadius
	n := 0
	for x := stand.X - r; x <= stand.X+r; x++ {
		for z := stand.Z - r; z <= stand.Z+r; z++ {
			if !d.Pack.Walkable(x, z, 0) {
				continue
			}
			n++
			if !g.inside(tile.Tile{X: x, Z: z}) {
				t.Errorf("%d,%d is within %d of the stand and not inside the pen", x, z, r)
			}
		}
	}
	if n < 20 {
		t.Fatalf("only %d floor tiles within %d of the stand", n, r)
	}
	field := questplan.DemonCowField
	for _, radius := range []int{8, 12} {
		outside := 0
		for x := field.X - radius; x <= field.X+radius; x++ {
			for z := field.Z - radius; z <= field.Z+radius; z++ {
				if d.Pack.Walkable(x, z, 0) && !g.inside(tile.Tile{X: x, Z: z}) {
					outside++
				}
			}
		}
		if outside == 0 {
			t.Errorf("every floor tile within %d of the middle is inside the pen: the old walks could not have stopped outside it", radius)
		}
		t.Logf("%d floor tiles within %d of the pasture's middle are outside the pen", outside, radius)
	}
}
