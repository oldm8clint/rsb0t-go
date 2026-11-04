package mazegraph

import "testing"

// routeDoor finds tile in the route from spawn.
func routeDoor(t *testing.T, spawn, tile Cell) (DoorInfo, int) {
	t.Helper()
	for i, d := range SelectRoute(spawn) {
		if d.Tile == tile {
			return d, i
		}
	}
	t.Fatalf("door %+v is not on the route from %+v", tile, spawn)
	return DoorInfo{}, 0
}

// The leaf of each door the 3 October log shows misjudged: it stands on the tile the player
// lands on and walls off that tile's first step toward the next door.
func TestLeafEdgeOfTheLoggedDoors(t *testing.T) {
	cases := []struct {
		spawn, door, next Cell
		leafA, leafB      Cell
	}{
		// door (2898,4600) opened -> (2898,4599); next door (2895,4598); live: "door
		// (2895,4598) ... opened from (2895,4598) -> (2897,4599)" seven times.
		{Spawns[0], Cell{2898, 4600}, Cell{2895, 4598}, Cell{2898, 4599}, Cell{2897, 4599}},
		// door (2916,4586) opened -> (2916,4585); next (2912,4584); live: "-> (2914,4585)" and
		// "walk leg to (2912,4584) stalled at (2915,4585)".
		{Spawns[1], Cell{2916, 4586}, Cell{2912, 4584}, Cell{2916, 4585}, Cell{2915, 4585}},
	}
	g := BuildLayoutGraph()
	for _, c := range cases {
		d, i := routeDoor(t, c.spawn, c.door)
		a, b := d.LeafEdge()
		leaf := EdgeKey(a.X, a.Z, b.X, b.Z)
		if leaf != EdgeKey(c.leafA.X, c.leafA.Z, c.leafB.X, c.leafB.Z) {
			t.Errorf("door %+v leaf blocks %+v-%+v, want %+v-%+v", c.door, a, b, c.leafA, c.leafB)
		}
		if d.To != c.leafA {
			t.Errorf("door %+v lands on %+v, want the leaf's tile %+v", c.door, d.To, c.leafA)
		}
		next := SelectRoute(c.spawn)[i+1]
		if next.Tile != c.next {
			t.Fatalf("door after %+v is %+v, want %+v", c.door, next.Tile, c.next)
		}
		plain := g.PathWithinRoom(d.To, next.From, 0, 4000)
		if len(plain) < 2 || EdgeKey(plain[0].X, plain[0].Z, plain[1].X, plain[1].Z) != leaf {
			t.Fatalf("the shortest walk from %+v to %+v should start across the leaf, got %v", d.To, next.From, plain)
		}
		alt := g.PathAvoiding(d.To, next.From, 4000, map[string]bool{leaf: true})
		if alt == nil {
			t.Fatalf("no walk from %+v to %+v round the leaf", d.To, next.From)
		}
		if len(alt) != len(plain) {
			t.Errorf("the walk round the leaf is %d cells, the plain one %d: it should cost nothing here", len(alt), len(plain))
		}
		for j := 1; j < len(alt); j++ {
			if EdgeKey(alt[j-1].X, alt[j-1].Z, alt[j].X, alt[j].Z) == leaf {
				t.Errorf("the walk round the leaf crosses it: %v", alt)
			}
		}
	}
}

func TestPathAvoidingNilWhenTheOnlyWayIsAvoided(t *testing.T) {
	g := synthCorridor()
	// The corridor x=2900, z 4570..4575 is one tile wide: avoid its 4570-4571 edge.
	avoid := map[string]bool{EdgeKey(2900, 4570, 2900, 4571): true}
	if p := g.PathAvoiding(Cell{2900, 4570}, Cell{2900, 4572}, 4000, avoid); p != nil {
		t.Errorf("got a path through an avoided edge in a one-tile corridor: %v", p)
	}
	if p := g.PathAvoiding(Cell{2900, 4570}, Cell{2900, 4572}, 4000, nil); len(p) != 3 {
		t.Errorf("with nothing avoided the path is %v, want 3 cells", p)
	}
}

func TestWaypointsAreCornersWithoutTheStartTile(t *testing.T) {
	path := []Cell{{2888, 4587}, {2888, 4588}, {2888, 4589}, {2889, 4589}, {2890, 4589}, {2890, 4590}}
	got := Waypoints(path)
	want := []Cell{{2888, 4589}, {2890, 4589}, {2890, 4590}}
	if len(got) != len(want) {
		t.Fatalf("Waypoints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Waypoints = %v, want %v", got, want)
		}
	}
	if Waypoints(path[:1]) != nil {
		t.Error("a one-cell path needs no click")
	}
	if w := Waypoints(path[:2]); len(w) != 1 || w[0] != path[1] {
		t.Errorf("a one-step path = %v, want just its end", w)
	}
	// The logged 24-cell walk (2888,4587) -> (2898,4600) is two straight runs: two waypoints,
	// one click, where the old glue sent 24 cells including the start.
	g := BuildLayoutGraph()
	long := g.PathWithinRoom(Cell{2888, 4587}, Cell{2898, 4600}, 0, 4000)
	if len(long) != 24 {
		t.Fatalf("path = %d cells, want 24", len(long))
	}
	if w := Waypoints(long); len(w) != 2 || w[0] != (Cell{2888, 4600}) || w[1] != (Cell{2898, 4600}) {
		t.Errorf("Waypoints = %v, want [(2888,4600) (2898,4600)]", w)
	}
	// Every walk of every spawn's route fits one click.
	for _, sp := range Spawns {
		cur := sp
		for _, d := range SelectRoute(sp) {
			p := g.PathWithinRoom(cur, d.From, 0, 4000)
			if w := Waypoints(p); len(w) > 25 {
				t.Errorf("walk %+v -> %+v needs %d waypoints, over one click's 25", cur, d.From, len(w))
			}
			cur = d.To
		}
	}
}
