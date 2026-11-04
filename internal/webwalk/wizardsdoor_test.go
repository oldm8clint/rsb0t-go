package webwalk

import (
	"testing"

	"rsb0t-go/internal/doorescape"
)

// The Wizards' Tower's diagonal door 1536 at 3107,3162 (shape 9, angle 3) used to be crossed by the baked edges
// 3106,3162 <-> 3108,3162. Opening it lands its leaf on 3106,3162 and pushes an account standing there to 3106,3161
// (content's door_open and door_open_move_player_out_of_way), so neither edge could ever be completed. See
// wizardsDoorWhy.

func wizardsDoor() doorescape.Door {
	return doorescape.Door{Name: "Door", ID: 1536, X: 3107, Z: 3162, Shape: doorescape.ShapeWallDiagonal, Angle: 3, OpenID: 1537, Op: 1}
}

// wizardsFlags is the tower's collision around the door as the engine holds it: every tile free, the door's block on
// its own tile while closed and on the leaf's tile once open.
func wizardsFlags(open bool) doorescape.FlagsFunc {
	d := wizardsDoor()
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

func TestTheWizardsDoorEdgesAreTheOnesTheEngineLetsAccountsCross(t *testing.T) {
	d := wizardsDoor()
	if o := d.Open(); o.X != 3106 || o.Z != 3162 {
		t.Fatalf("the open leaf lands on %d,%d, not 3106,3162", o.X, o.Z)
	}
	if p, ok := d.Push(); !ok || p.X != 3106 || p.Z != 3161 {
		t.Fatalf("opening pushes to %v (%v), not 3106,3161", p, ok)
	}
	closed, open := wizardsFlags(false), wizardsFlags(true)
	op := d.Closed()
	lt := doorescape.LocType{Width: 1, Length: 1}

	// Both stands can operate the closed door, and the crossing needs nothing else of them.
	for _, from := range [][2]int{{3108, 3162}, {3107, 3161}} {
		if !doorescape.Reached(closed, 0, from[0], from[1], op, lt) {
			t.Errorf("the engine does not run the door's op from %v", from)
		}
	}
	// Neither of the new stands is the leaf's tile, so nobody is pushed off it.
	if p, _ := d.Push(); (p.X == 3107 && p.Z == 3161) || (p.X == 3108 && p.Z == 3162) {
		t.Errorf("a stand is the push's landing tile")
	}
	// Open: the doorway is free, the tile the baked edges named is solid, and the way round exists.
	step := func(x, z, dx, dz int) bool { return doorescape.CanStep(open, 0, x, z, dx, dz) }
	if !step(3108, 3162, -1, 0) || !step(3107, 3162, 0, -1) || !step(3107, 3161, -1, 0) {
		t.Errorf("east to west (3108,3162 -> 3107,3162 -> 3107,3161 -> 3106,3161) is refused on an open door")
	}
	if !step(3106, 3161, 1, 0) || !step(3107, 3161, 0, 1) || !step(3107, 3162, 1, 0) {
		t.Errorf("west to east (3107,3161 -> 3107,3162 -> 3108,3162) is refused on an open door")
	}
	if step(3107, 3162, -1, 0) {
		t.Errorf("3107,3162 -> 3106,3162 (the baked landing) is allowed on an open door")
	}
	if step(3107, 3162, -1, -1) || step(3106, 3161, 1, 1) {
		t.Errorf("the diagonal across the leaf's corner is allowed; the server would refuse it")
	}
}

func TestTheWizardsDoorHasOnlyTheTwoCorrectedEdges(t *testing.T) {
	g, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	type key struct{ from, to Point }
	var found []key
	for _, e := range g.Edges {
		if e.LocID != 1536 || e.LocX != 3107 || e.LocZ != 3162 {
			continue
		}
		found = append(found, key{e.From, e.To})
		if e.From.X == 3106 && e.From.Z == 3162 || e.To.X == 3106 && e.To.Z == 3162 || e.From.X == 3107 && e.From.Z == 3162 || e.To.X == 3107 && e.To.Z == 3162 {
			t.Errorf("an edge still uses the door's own or the leaf's tile: %s", e.String())
		}
	}
	want := []key{{Point{X: 3108, Z: 3162}, Point{X: 3106, Z: 3161}}, {Point{X: 3107, Z: 3161}, Point{X: 3108, Z: 3162}}}
	if len(found) != len(want) {
		t.Fatalf("door edges %v, want %v", found, want)
	}
	for i := range want {
		if found[i] != want[i] {
			t.Errorf("door edge %d is %v, want %v", i, found[i], want[i])
		}
	}
}

// The Imp Catcher route the live log shows: in by the north door, through the diagonal door, up the staircase to
// Mizgog's floor, and back out the same way.
func TestTheRouteToMizgogCrossesTheWizardsDoorByTheCorrectedEdge(t *testing.T) {
	p := realPack(t)
	to := Point{X: 3103, Z: 3163, Level: 2}
	r := p.Plan(Point{X: 3090, Z: 3230}, to, Options{})
	if !r.OK {
		t.Fatalf("no route to Mizgog: %s", r.Reason)
	}
	var door *Edge
	for _, e := range r.Hops() {
		if e.LocID == 1536 && e.LocX == 3107 {
			door = e
		}
	}
	t.Log(r.String())
	if door == nil || door.From != (Point{X: 3108, Z: 3162}) || door.To != (Point{X: 3106, Z: 3161}) {
		t.Fatalf("the route does not cross the diagonal door from 3108,3162 to 3106,3161: %s", r.String())
	}
	if r.End() != to {
		t.Errorf("the route ends at %v", r.End())
	}
	back := p.Plan(Point{X: 3105, Z: 3160}, Point{X: 3090, Z: 3230}, Options{})
	if !back.OK {
		t.Fatalf("no route out of the west room: %s", back.Reason)
	}
	var out *Edge
	for _, e := range back.Hops() {
		if e.LocID == 1536 && e.LocX == 3107 {
			out = e
		}
	}
	if out == nil || out.From != (Point{X: 3107, Z: 3161}) || out.To != (Point{X: 3108, Z: 3162}) {
		t.Fatalf("the route out does not cross the diagonal door from 3107,3161 to 3108,3162: %s", back.String())
	}
}
