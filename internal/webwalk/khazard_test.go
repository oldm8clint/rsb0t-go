package webwalk

import (
	"testing"

	"rsb0t-go/internal/doorescape"
)

// The Khazard stronghold of Tree Gnome Village had its ladder in the graph and neither of its two doors, so a walk
// from its upper floor planned down the ladder into a shut room: live 16 Oct 2026, 03:15 and 03:41, two accounts
// died on the floor above 2505,3256 with every walk "unreachable". See khazard.go.

// khazardInnerDoor is loc 1530 at 2505,3256 (maps/m39_50.jm2 "0 9 56: 1530 9 3"): the Wizards' Tower door's twin.
func khazardInnerDoor() doorescape.Door {
	return doorescape.Door{Name: "Door", ID: 1530, X: 2505, Z: 3256, Shape: doorescape.ShapeWallDiagonal, Angle: 3, OpenID: 1531, Op: 1}
}

// khazardFlags is the hall and ladder room's collision around the door as the engine holds it: every tile free, the
// door's block on its own tile while closed and on the leaf's tile once open.
func khazardFlags(open bool) doorescape.FlagsFunc {
	d := khazardInnerDoor()
	block := d.Closed()
	if open {
		block = d.Open()
	}
	return func(x, z, level int) uint32 {
		if x == block.X && z == block.Z {
			return doorescape.Loc
		}
		return 0
	}
}

func TestTheKhazardInnerDoorIsCrossedByTheStandsTheEngineAllows(t *testing.T) {
	d := khazardInnerDoor()
	if o := d.Open(); o.X != 2504 || o.Z != 3256 {
		t.Fatalf("the open leaf lands on %d,%d, not 2504,3256", o.X, o.Z)
	}
	if p, ok := d.Push(); !ok || p.X != 2504 || p.Z != 3255 {
		t.Fatalf("opening pushes to %v (%v), not 2504,3255", p, ok)
	}
	closed, open := khazardFlags(false), khazardFlags(true)
	op := d.Closed()
	lt := doorescape.LocType{Width: 1, Length: 1}
	// Both stands can operate the closed door.
	for _, from := range [][2]int{{2506, 3256}, {2505, 3255}} {
		if !doorescape.Reached(closed, 0, from[0], from[1], op, lt) {
			t.Errorf("the engine does not run the door's op from %v", from)
		}
	}
	// Neither stand is the leaf's tile or the push's landing tile, and the landings are not either of them.
	push, _ := d.Push()
	for _, s := range [][2]int{{2506, 3256}, {2505, 3255}, {2504, 3255}} {
		if s[0] == d.Open().X && s[1] == d.Open().Z {
			t.Errorf("%v is the open leaf's tile", s)
		}
	}
	if (push.X == 2506 && push.Z == 3256) || (push.X == 2505 && push.Z == 3255) {
		t.Errorf("a stand is the push's landing tile")
	}
	// Open: the doorway is free and the way round the leaf exists both ways; the leaf's corner is not a step.
	step := func(x, z, dx, dz int) bool { return doorescape.CanStep(open, 0, x, z, dx, dz) }
	if !step(2506, 3256, -1, 0) || !step(2505, 3256, 0, -1) || !step(2505, 3255, -1, 0) {
		t.Errorf("east to west (2506,3256 -> 2505,3256 -> 2505,3255 -> 2504,3255) is refused on an open door")
	}
	if !step(2504, 3255, 1, 0) || !step(2505, 3255, 0, 1) || !step(2505, 3256, 1, 0) {
		t.Errorf("west to east (2505,3255 -> 2505,3256 -> 2506,3256) is refused on an open door")
	}
	if step(2505, 3256, -1, 0) {
		t.Errorf("2505,3256 -> 2504,3256 (under the leaf) is allowed on an open door")
	}
	if step(2505, 3256, -1, -1) {
		t.Errorf("the diagonal across the leaf's corner is allowed; the server would refuse it")
	}
}

// The default graph holds the stronghold's doors exactly as khazard.go says, and nothing else on those locs.
func TestTheKhazardDoorsAreInTheDefaultGraphAsDocumented(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		loc      int
		from, to Point
	}
	var found []key
	for _, e := range g.Edges {
		if (e.LocID == 1530 && e.LocX == 2505 && e.LocZ == 3256) || (e.LocID == 2184 && e.LocX == 2502 && e.LocZ == 3250) {
			found = append(found, key{e.LocID, e.From, e.To})
			if e.Kind != KindDoor || e.Action != "Open" {
				t.Errorf("%s is not a door that opens", e.String())
			}
		}
	}
	want := []key{
		{1530, Point{X: 2506, Z: 3256}, Point{X: 2504, Z: 3255}},
		{1530, Point{X: 2505, Z: 3255}, Point{X: 2506, Z: 3256}},
		{2184, Point{X: 2502, Z: 3251}, Point{X: 2502, Z: 3250}},
	}
	if len(found) != len(want) {
		t.Fatalf("door edges %v, want %v", found, want)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("door edge %d is %v, want %v", i, found[i], want[i])
		}
	}
	// No edge uses the inner door's own tile or its leaf's tile as a stand or a landing.
	for _, k := range found[:2] {
		for _, p := range []Point{k.from, k.to} {
			if (p.X == 2505 && p.Z == 3256) || (p.X == 2504 && p.Z == 3256) {
				t.Errorf("an inner-door edge uses the door's own or the leaf's tile: %v", k)
			}
		}
	}
}

func hopsOf(r Route) []*Edge { return r.Hops() }

func hasHop(r Route, locID int, action string) *Edge {
	for _, h := range hopsOf(r) {
		if h.LocID == locID && h.Action == action {
			return h
		}
	}
	return nil
}

// THE DEATHS' WALK. From the floor above the ladder room (the tiles the two accounts were on) to the bank in East
// Ardougne and to the ground outside: down the ladder, out by the front door, on. Before the doors were in the graph
// every one of these was "unreachable" after 65 expansions: the 37 tiles up there and the 28 of the ladder room.
func TestAWalkFromTheKhazardUpperFloorGetsOutOfTheStronghold(t *testing.T) {
	p := realPack(t)
	bank := Point{X: 2616, Z: 3332}
	outside := Point{X: 2502, Z: 3249}
	for _, from := range []Point{
		{X: 2506, Z: 3258, Level: 1}, // where the first account stood when its fight was lost
		{X: 2505, Z: 3256, Level: 1}, // the second
		{X: 2504, Z: 3252, Level: 1}, // the ladder's landing
		{X: 2503, Z: 3253, Level: 1}, // the ladder's stand
		{X: 2500, Z: 3257, Level: 1}, // the far corner
	} {
		for _, to := range []Point{outside, bank} {
			r := p.Plan(from, to, Options{})
			if !r.OK {
				t.Errorf("%s -> %s: %s", from, to, r.Reason)
				continue
			}
			ladder := hasHop(r, 1746, "Climb-down")
			door := hasHop(r, 2184, "Open")
			if ladder == nil || ladder.To.Level != 0 || door == nil || door.From != (Point{X: 2502, Z: 3251}) || door.To != (Point{X: 2502, Z: 3250}) {
				t.Errorf("%s -> %s does not go down the ladder and out of the front door: %s", from, to, r.String())
			}
			if Chebyshev(r.End(), to) > 2 {
				t.Errorf("%s -> %s ends at %s", from, to, r.End())
			}
		}
	}
	// The planner's own bookkeeping: the first hop is the ladder (the 65 tiles it settled before were the whole of
	// what it could reach).
	r := p.Plan(Point{X: 2506, Z: 3258, Level: 1}, bank, Options{})
	if !r.OK || r.Expanded <= 65 {
		t.Errorf("the walk from the upper floor settled %d nodes (ok %v): it never left the stronghold", r.Expanded, r.OK)
	}
	t.Log(r.String())
}

// The hall (the crumbled wall's landing) and the ladder room walk out too: through the inner door for the hall.
func TestAWalkFromTheKhazardHallCrossesTheInnerDoorAndLeaves(t *testing.T) {
	p := realPack(t)
	bank := Point{X: 2616, Z: 3332}
	r := p.Plan(Point{X: 2509, Z: 3255}, bank, Options{})
	if !r.OK {
		t.Fatalf("no route out of the hall: %s", r.Reason)
	}
	inner := hasHop(r, 1530, "Open")
	front := hasHop(r, 2184, "Open")
	if inner == nil || inner.From != (Point{X: 2506, Z: 3256}) || inner.To != (Point{X: 2504, Z: 3255}) || front == nil {
		t.Fatalf("the route from the hall does not cross the inner door and leave by the front door: %s", r.String())
	}
	room := p.Plan(Point{X: 2503, Z: 3253}, bank, Options{})
	if !room.OK || hasHop(room, 2184, "Open") == nil || hasHop(room, 1530, "Open") != nil {
		t.Fatalf("the route from the ladder room: ok %v, %s", room.OK, room.String())
	}
	// And back through the inner door from the ladder room to the hall (the ladder room's own stand).
	back := p.Plan(Point{X: 2503, Z: 3253}, Point{X: 2509, Z: 3255}, Options{})
	if !back.OK {
		t.Fatalf("no route from the ladder room to the hall: %s", back.Reason)
	}
	if h := hasHop(back, 1530, "Open"); h == nil || h.From != (Point{X: 2505, Z: 3255}) || h.To != (Point{X: 2506, Z: 3256}) {
		t.Fatalf("the route to the hall does not cross the inner door from 2505,3255: %s", back.String())
	}
}

// The doors are exits, never entrances: the crumbled wall is the way in (a custom step climbs it, once the ballista
// has fired) and no walk from outside plans into the stronghold. A route that did would walk accounts at stage 1 into
// the Khazard commanders' tower.
func TestNoWalkPlansIntoTheKhazardStronghold(t *testing.T) {
	p := realPack(t)
	cheap := Options{MaxExpansions: 120000}
	for _, to := range []Point{
		{X: 2503, Z: 3253}, {X: 2509, Z: 3255}, {X: 2506, Z: 3258, Level: 1}, {X: 2503, Z: 3253, Level: 1},
	} {
		for _, from := range []Point{{X: 2502, Z: 3249}, {X: 2509, Z: 3252}, {X: 2616, Z: 3332}} {
			if r := p.Plan(from, to, cheap); r.OK {
				t.Errorf("%s -> %s plans: %s", from, to, r.String())
			}
		}
	}
	// No graph edge goes from outside the stronghold to inside it. The inside is the three floods the walls close.
	inside := map[Point]bool{}
	for _, seed := range []Point{{X: 2509, Z: 3255}, {X: 2503, Z: 3253}, {X: 2503, Z: 3253, Level: 1}} {
		for pt := range khazardFlood(p, seed) {
			inside[pt] = true
		}
	}
	if len(inside) != 49+28+37 {
		t.Fatalf("the stronghold is %d tiles, not the 49 + 28 + 37 the walls close", len(inside))
	}
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		if inside[e.To] && !inside[e.From] {
			t.Errorf("an edge enters the stronghold: %s", e.String())
		}
	}
}

// khazardFlood is the tiles a walk can reach from seed with no hop.
func khazardFlood(p *Planner, from Point) map[Point]bool {
	seen := map[Point]bool{from: true}
	q := []Point{from}
	for len(q) > 0 {
		c := q[0]
		q = q[1:]
		m := p.col.ExitMask(c.X, c.Z, c.Level)
		for d := 0; d < 8; d++ {
			if m&(1<<uint(d)) == 0 {
				continue
			}
			n := Point{X: c.X + dirX[d], Z: c.Z + dirZ[d], Level: c.Level}
			if !seen[n] {
				seen[n] = true
				q = append(q, n)
			}
		}
		if len(seen) > 5000 {
			break
		}
	}
	return seen
}

// Every khazardStrongholdEdges stand and landing survived the planner's "walkable in the pack" filter.
func TestTheKhazardEdgesSurviveThePack(t *testing.T) {
	p := realPack(t)
	for _, x := range khazardStrongholdEdges {
		found := false
		for _, e := range p.EdgesFrom(x.Edge.From) {
			found = found || (e.LocID == x.Edge.LocID && e.To == x.Edge.To && e.Action == x.Edge.Action)
		}
		if !found {
			t.Errorf("edge %s was dropped by the planner: an end of it is not walkable in the pack", x.Edge.String())
		}
	}
}
