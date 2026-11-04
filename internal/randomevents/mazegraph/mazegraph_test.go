package mazegraph

import "testing"

// These tests port the pinned assertions from rsb0t-main's own
// test/runtime/randomevents/maze/mazeGraph.test.ts, minus the ones that exercise
// parseJm2Locs (this port consumes MazeLayout — the same tool's already-parsed output
// — directly, so there's no .jm2 text to parse here; see layout.go's doc comment).
// Matching the original's real-map counts (49 doors, 1459 walls, etc.) against our own
// independently-transcribed MazeLayout is the strongest evidence the transcription in
// layout.go is correct.

func TestBuildMaze_WallStraightBlocksOneCardinalEdge(t *testing.T) {
	g := BuildMaze([]Loc{{LX: 10, LZ: 10, ID: WallID, Shape: 0, Angle: 2}}) // angle EAST=2
	if !g.WallEdge[EdgeKey(2890, 4554, 2891, 4554)] {
		t.Error("expected the east edge to be blocked")
	}
	if g.WallEdge[EdgeKey(2890, 4554, 2889, 4554)] {
		t.Error("west edge should not be blocked")
	}
}

func TestBuildMaze_WallLBlocksNorthAndWestEdges(t *testing.T) {
	g := BuildMaze([]Loc{{LX: 10, LZ: 10, ID: WallID, Shape: 2, Angle: 0}}) // angle WEST=0
	if !g.WallEdge[EdgeKey(2890, 4554, 2890, 4555)] {
		t.Error("expected the north edge to be blocked")
	}
	if !g.WallEdge[EdgeKey(2890, 4554, 2889, 4554)] {
		t.Error("expected the west edge to be blocked")
	}
	if g.WallEdge[EdgeKey(2890, 4554, 2891, 4554)] {
		t.Error("east edge should not be blocked")
	}
}

func TestBuildMaze_SquareCornerContributesNoCardinalWall(t *testing.T) {
	g := BuildMaze([]Loc{{LX: 10, LZ: 10, ID: WallID, Shape: 3, Angle: 0}})
	if len(g.WallEdge) != 0 {
		t.Errorf("expected no wall edges from shape 3, got %d", len(g.WallEdge))
	}
}

func TestBuildMaze_DoorRecordedOnItsEdge(t *testing.T) {
	g := BuildMaze([]Loc{{LX: 10, LZ: 10, ID: 3628, Shape: 0, Angle: 2}})
	info, ok := g.Door[EdgeKey(2890, 4554, 2891, 4554)]
	if !ok {
		t.Fatal("expected a door on that edge")
	}
	want := DoorInfo{Tile: Cell{X: 2890, Z: 4554}, ID: 3628, Angle: 2}
	if info != want {
		t.Errorf("door info = %+v, want %+v", info, want)
	}
}

func TestDoorPassable_Dir1OpensFromAxisSideOnly(t *testing.T) {
	d1 := DoorInfo{Tile: Cell{X: 2890, Z: 4554}, ID: 3629, Angle: 2}
	if !DoorPassable(d1, 2890, 4554) {
		t.Error("expected passable from the axis-aligned side")
	}
	if DoorPassable(d1, 2891, 4554) {
		t.Error("expected NOT passable from the other side")
	}
}

func TestDoorPassable_Dir2OpensFromOffAxisSideOnly(t *testing.T) {
	d2 := DoorInfo{Tile: Cell{X: 2890, Z: 4554}, ID: 3630, Angle: 1}
	if DoorPassable(d2, 2890, 4554) {
		t.Error("expected NOT passable from the axis-aligned side")
	}
	if !DoorPassable(d2, 2890, 4555) {
		t.Error("expected passable from the off-axis side")
	}
}

func TestDoorPassable_Dir0OpensFromEitherSide(t *testing.T) {
	d0 := DoorInfo{Tile: Cell{X: 2890, Z: 4554}, ID: 3628, Angle: 2}
	if !DoorPassable(d0, 2890, 4554) || !DoorPassable(d0, 2891, 4554) {
		t.Error("expected dir-0 door passable from both sides")
	}
}

func TestBuildLayoutGraph_RealMapCounts(t *testing.T) {
	g := BuildLayoutGraph()
	if len(g.Door) != 49 {
		t.Errorf("door count = %d, want 49 (matches mazeGraph.test.ts's pinned real-map count)", len(g.Door))
	}
	if len(g.WallEdge) <= 1000 {
		t.Errorf("wall edge count = %d, want > 1000", len(g.WallEdge))
	}
	if g.MinX < 2880 {
		t.Errorf("MinX = %d, want >= 2880", g.MinX)
	}
	if g.MaxX > 2943 {
		t.Errorf("MaxX = %d, want <= 2943", g.MaxX)
	}

	// Cross-check against DoorDirs' own id set, same as the original's
	// `Object.keys(DOOR_DIRS).reduce(...)` sum-of-counts-by-id assertion (== 49): every
	// door in the built graph must be one of the five documented door ids.
	seen := map[int]int{}
	for _, d := range g.Door {
		seen[d.ID]++
	}
	total := 0
	for id := range DoorDirs {
		total += seen[id]
	}
	if total != 49 {
		t.Errorf("sum of door counts by DoorDirs id = %d, want 49 (got %v)", total, seen)
	}
}

func synthCorridor() *Graph {
	g := BuildMaze(nil)
	for z := 4570; z <= 4575; z++ {
		g.WallEdge[EdgeKey(2900, z, 2899, z)] = true
		g.WallEdge[EdgeKey(2900, z, 2901, z)] = true
	}
	g.Door[EdgeKey(2900, 4572, 2900, 4573)] = DoorInfo{Tile: Cell{X: 2900, Z: 4572}, ID: 3628, Angle: 1}
	return g
}

func TestSolveRoute_SyntheticCorridor_SingleDoor(t *testing.T) {
	g := synthCorridor()
	route := SolveRoute(g, Cell{X: 2900, Z: 4570}, Cell{X: 2900, Z: 4575})
	if len(route) != 1 || route[0].Tile != (Cell{X: 2900, Z: 4572}) {
		t.Errorf("route = %+v, want a single door at (2900,4572)", route)
	}
}

func TestSolveRoute_SyntheticCorridor_GatedDoorBlocksWrongSide(t *testing.T) {
	g := synthCorridor()
	g.Door[EdgeKey(2900, 4572, 2900, 4573)] = DoorInfo{Tile: Cell{X: 2900, Z: 4572}, ID: 3630, Angle: 1}
	route := SolveRoute(g, Cell{X: 2900, Z: 4570}, Cell{X: 2900, Z: 4575})
	if len(route) != 0 {
		t.Errorf("route = %+v, want empty (wrong-side gated door)", route)
	}
}

func TestPathWithinRoom_OpenFloorReachesAdjacent(t *testing.T) {
	g := BuildMaze(nil)
	path := g.PathWithinRoom(Cell{X: 0, Z: 0}, Cell{X: 3, Z: 0}, 1, 1000)
	if len(path) == 0 || path[len(path)-1] != (Cell{X: 2, Z: 0}) {
		t.Errorf("path = %v, want to end adjacent to (3,0)", path)
	}
	if path[0] != (Cell{X: 0, Z: 0}) {
		t.Errorf("path should start at `from`, got %v", path)
	}
}

func TestPathWithinRoom_BlockedByUnopenedDoor(t *testing.T) {
	// A fully enclosed 1-wide tube from (0,0) to (0,5): walls on both flanks for
	// every z, plus capped ends, with a single door in the middle. Unlike
	// synthCorridor() (used by the SolveRoute tests, which only need to reach a
	// touch-stand, not exit the tube), this must be airtight or the BFS will
	// correctly route around it through open exterior space instead of proving
	// anything about the door.
	g := BuildMaze(nil)
	for z := 0; z <= 5; z++ {
		g.WallEdge[EdgeKey(0, z, -1, z)] = true
		g.WallEdge[EdgeKey(0, z, 1, z)] = true
	}
	g.WallEdge[EdgeKey(0, 0, 0, -1)] = true // south cap
	g.WallEdge[EdgeKey(0, 5, 0, 6)] = true  // north cap
	g.Door[EdgeKey(0, 2, 0, 3)] = DoorInfo{Tile: Cell{X: 0, Z: 2}, ID: 3628, Angle: 1}

	// From south of the door to north of it: must cross the door edge, which
	// PathWithinRoom deliberately treats as blocked (not yet opened).
	path := g.PathWithinRoom(Cell{X: 0, Z: 0}, Cell{X: 0, Z: 5}, 0, 1000)
	if path != nil {
		t.Errorf("expected nil (blocked by the unopened door, and the tube is airtight otherwise), got %v", path)
	}
}

func TestPathWithinRoom_AlreadyAdjacentReturnsTrivialPath(t *testing.T) {
	g := BuildMaze(nil)
	path := g.PathWithinRoom(Cell{X: 5, Z: 5}, Cell{X: 5, Z: 6}, 1, 100)
	if len(path) != 1 || path[0] != (Cell{X: 5, Z: 5}) {
		t.Errorf("path = %v, want a trivial one-cell path (already adjacent)", path)
	}
}

func TestSolveRoute_RealMap_EverySpawnReachesChamberDoor(t *testing.T) {
	g := BuildLayoutGraph()
	chamberDoor := ShrineDoor // (2910,4576)
	var tails []Cell
	for _, spawn := range Spawns {
		route := SolveRoute(g, spawn, Shrine)
		if len(route) == 0 {
			t.Errorf("spawn %+v: no route found", spawn)
			continue
		}
		last := route[len(route)-1]
		if last.Tile != chamberDoor {
			t.Errorf("spawn %+v: last door = %+v, want chamber door %+v", spawn, last.Tile, chamberDoor)
		}
		tails = append(tails, last.Tile)
		// Regression: must never treat the walled-off south-of-SW tile (2911,4574) as
		// part of the route (the old buggy goal test — manhattan distance 1 — accepted
		// it through a wall).
		for _, d := range route {
			if d.Tile == (Cell{X: 2911, Z: 4574}) {
				t.Errorf("spawn %+v: route incorrectly passes through walled tile (2911,4574)", spawn)
			}
		}
	}
	for _, tail := range tails {
		if tail != chamberDoor {
			t.Errorf("expected every spawn's route to converge on the same chamber door, got %+v", tail)
		}
	}
}

// --- approach-tile choice: orthogonal, never diagonal ---
//
// Pinned to the 2026-09-14 production incident: account `spare2` stood at
// (2902,4555), diagonal to route door 1 at (2903,4554) (local (23,10): type 3628,
// straight wall, angle NORTH — it blocks the edge (2903,4554)-(2903,4555)), and the
// solver clicked it forever. The only tiles from which the engine's reachWall1 accepts
// an Open on that door are its own tile, the tile across the edge, and the two tiles
// flanking it along the wall — the diagonal is never one of them.

// incidentDoor is route door 1 from the SW spawn, exactly as the real layout records it.
func incidentDoor(t *testing.T) DoorInfo {
	t.Helper()
	g := BuildLayoutGraph()
	d, ok := g.Door[EdgeKey(2903, 4554, 2903, 4555)]
	if !ok {
		t.Fatal("the incident door (2903,4554) is not in the real layout graph")
	}
	if d.ID != 3628 || d.Angle != 1 {
		t.Fatalf("incident door = %+v, want type 3628 angle 1 (north)", d)
	}
	return d
}

func TestDoorSidesAreCardinalNeighboursOnTheDoorAxis(t *testing.T) {
	d := incidentDoor(t)
	own, across := d.Sides()
	if own != (Cell{X: 2903, Z: 4554}) || across != (Cell{X: 2903, Z: 4555}) {
		t.Errorf("Sides() = %+v, %+v; want (2903,4554) and (2903,4555)", own, across)
	}
	if manhattan(own, across) != 1 {
		t.Errorf("the two sides of a door must be cardinally adjacent, got %+v and %+v", own, across)
	}
}

func TestApproachTileFromTheIncidentDiagonalIsOrthogonal(t *testing.T) {
	d := incidentDoor(t)
	d.From, d.To = Cell{}, Cell{} // as a caller without route context would see it
	me := Cell{X: 2902, Z: 4555}  // the live incident tile: dx=-1, dz=+1 from the door
	got := ApproachTile(d, me)
	if got == me {
		t.Fatalf("ApproachTile returned the player's own diagonal tile %+v — that is the incident", got)
	}
	if got != (Cell{X: 2903, Z: 4555}) {
		t.Errorf("ApproachTile from %+v = %+v, want the near orthogonal side (2903,4555)", me, got)
	}
	own, across := d.Sides()
	if got != own && got != across {
		t.Errorf("ApproachTile must return one of the door's two sides, got %+v", got)
	}
}

func TestApproachTileNeverDiagonalFromAnySurroundingTile(t *testing.T) {
	// Every one of the 8 neighbours of every door tile in the real layout (plus the
	// door tile itself) must map to a cardinal side of that door's edge — no choice
	// may ever be diagonal to the door tile.
	g := BuildLayoutGraph()
	for _, d := range g.Door {
		d.From, d.To = Cell{}, Cell{}
		own, across := d.Sides()
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				me := Cell{X: d.Tile.X + dx, Z: d.Tile.Z + dz}
				got := ApproachTile(d, me)
				if got != own && got != across {
					t.Fatalf("door %+v from %+v: ApproachTile = %+v, not one of its sides %+v/%+v", d.Tile, me, got, own, across)
				}
				if adx, adz := got.X-d.Tile.X, got.Z-d.Tile.Z; adx != 0 && adz != 0 {
					t.Fatalf("door %+v from %+v: ApproachTile = %+v is diagonal to the door tile", d.Tile, me, got)
				}
			}
		}
	}
}

func TestApproachTilePrefersTheNearerSideAndHonoursRouteFrom(t *testing.T) {
	d := DoorInfo{Tile: Cell{X: 2900, Z: 4560}, ID: 3630, Angle: 1} // blocks (2900,4560)-(2900,4561)
	if !DoorPassable(d, 2900, 4561) || DoorPassable(d, 2900, 4560) {
		t.Fatalf("test premise: a 3630 north door must open only from the across side")
	}
	// Two tiles out along the wall on the door's own row: own side is nearer.
	if got := ApproachTile(d, Cell{X: 2902, Z: 4560}); got != d.Tile {
		t.Errorf("own-row case: got %+v, want the nearer own tile %+v", got, d.Tile)
	}
	// Diagonal to the door tile on the across row: across side is nearer.
	if got := ApproachTile(d, Cell{X: 2901, Z: 4561}); got != (Cell{X: 2900, Z: 4561}) {
		t.Errorf("diagonal case: got %+v, want across (2900,4561)", got)
	}
	// A route-filled From always wins, regardless of where the player is.
	d.From, d.To = Cell{X: 2900, Z: 4561}, Cell{X: 2900, Z: 4560}
	if got := ApproachTile(d, Cell{X: 2902, Z: 4560}); got != d.From {
		t.Errorf("route From must be honoured, got %+v want %+v", got, d.From)
	}
}

func TestSolveRouteDoorsCarryOrthogonalFromAndTo(t *testing.T) {
	g := BuildLayoutGraph()
	for _, spawn := range Spawns {
		route := SolveRoute(g, spawn, Shrine)
		if len(route) == 0 {
			t.Fatalf("spawn %+v: no route", spawn)
		}
		for i, d := range route {
			own, across := d.Sides()
			if d.From == (Cell{}) || d.To == (Cell{}) {
				t.Fatalf("spawn %+v door %d %+v: From/To not filled in", spawn, i+1, d.Tile)
			}
			if !((d.From == own && d.To == across) || (d.From == across && d.To == own)) {
				t.Fatalf("spawn %+v door %d %+v: From/To = %+v/%+v are not the door's two sides %+v/%+v", spawn, i+1, d.Tile, d.From, d.To, own, across)
			}
			if d.From.X != d.Tile.X && d.From.Z != d.Tile.Z {
				t.Fatalf("spawn %+v door %d: From %+v is diagonal to door tile %+v", spawn, i+1, d.From, d.Tile)
			}
			if !DoorPassable(d, d.From.X, d.From.Z) {
				t.Fatalf("spawn %+v door %d %+v: route approaches from %+v but the door won't open from there", spawn, i+1, d.Tile, d.From)
			}
			if ApproachTile(d, Cell{X: d.Tile.X - 1, Z: d.Tile.Z - 1}) != d.From {
				t.Fatalf("spawn %+v door %d: ApproachTile must honour a route-filled From", spawn, i+1)
			}
		}
	}
	// And the exact production incident: from the SW spawn, door 1 is (2903,4554),
	// approached from (2903,4555) — never from (2902,4555).
	route := SolveRoute(g, Spawns[3], Shrine)
	if route[0].Tile != (Cell{X: 2903, Z: 4554}) {
		t.Fatalf("SW route door 1 = %+v, want (2903,4554) (matches the production log)", route[0].Tile)
	}
	if route[0].From != (Cell{X: 2903, Z: 4555}) || route[0].To != (Cell{X: 2903, Z: 4554}) {
		t.Errorf("SW route door 1 From/To = %+v/%+v, want (2903,4555)/(2903,4554)", route[0].From, route[0].To)
	}
	if len(route) != 18 {
		t.Errorf("SW route has %d doors, production log said 18", len(route))
	}
}
