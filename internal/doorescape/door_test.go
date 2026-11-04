package doorescape

import (
	"strings"
	"testing"
)

func doorIndex(t *testing.T, c *Catalog, x, z, level int) int {
	t.Helper()
	for i, d := range c.Doors {
		if d.X == x && d.Z == z && d.Level == level {
			return i
		}
	}
	t.Fatalf("no door at %d,%d level %d", x, z, level)
	return -1
}

// The state of a door is read off a client's own loc changes, the only thing a client knows: a LOC_DEL tombstone on
// the closed leaf's tile and a LOC_ADD_CHANGE of the open leaf on its tile is an open door, and every other
// combination is read for what it says.
func TestDoorStateIsReadFromTheClientsLocChanges(t *testing.T) {
	c := castle(t)
	main := doorIndex(t, c, 3279, 3939, 0)
	east := doorIndex(t, c, 3292, 3931, 0)
	mainOpen := c.Doors[main].Open()
	eastOpen := c.Doors[east].Open()
	tomb := func(d Door) LocChange {
		return LocChange{X: d.X, Z: d.Z, Level: d.Level, Type: -1, Shape: d.Shape, Rotate: d.Angle, Deleted: true}
	}
	leaf := func(p Placement) LocChange {
		return LocChange{X: p.X, Z: p.Z, Level: p.Level, Type: p.ID, Shape: p.Shape, Rotate: p.Angle}
	}

	if got := c.StateFromChanges(nil); got != c.DefaultState {
		t.Errorf("no changes read as %b, want every door shut (%b)", got, c.DefaultState)
	}
	open := c.StateFromChanges([]LocChange{tomb(c.Doors[main]), leaf(mainOpen)})
	if !open.IsOpen(main) || !open.IsShut(east) {
		t.Errorf("tombstone and open leaf: main open=%v east shut=%v, want both", open.IsOpen(main), open.IsShut(east))
	}
	// The way a freshly entered zone reports it: the closed leaf's tombstone and the open leaf, in either order.
	if got := c.StateFromChanges([]LocChange{leaf(mainOpen), tomb(c.Doors[main])}); got != open {
		t.Errorf("the same two records in the other order read differently: %b vs %b", got, open)
	}
	// Tombstone alone: the door is gone (the leaf has not been seen yet), not open and not shut.
	gone := c.StateFromChanges([]LocChange{tomb(c.Doors[main])})
	if gone.ClosedLeaf(main) || gone.OpenLeaf(main) {
		t.Errorf("a lone tombstone read as closed=%v open=%v, want neither leaf", gone.ClosedLeaf(main), gone.OpenLeaf(main))
	}
	// An open leaf with no tombstone (the closed leaf's removal not yet received): both there.
	both := c.StateFromChanges([]LocChange{leaf(mainOpen)})
	if !both.ClosedLeaf(main) || !both.OpenLeaf(main) {
		t.Errorf("a lone open leaf read as closed=%v open=%v, want both leaves", both.ClosedLeaf(main), both.OpenLeaf(main))
	}
	// A dynamic closed leaf put back by a close (a record of the closed id on its own tile) is shut.
	reshut := c.StateFromChanges([]LocChange{leaf(c.Doors[east].Closed())})
	if !reshut.IsShut(east) {
		t.Error("a record of the closed leaf on its own tile is not a shut door")
	}
	// Records that are not about a door change nothing: another type on the open leaf's tile, a record on another
	// level, a tombstone of another layer.
	other := c.StateFromChanges([]LocChange{
		{X: eastOpen.X, Z: eastOpen.Z, Level: eastOpen.Level, Type: 9999, Shape: 0},
		{X: eastOpen.X, Z: eastOpen.Z, Level: 1, Type: eastOpen.ID, Shape: eastOpen.Shape, Rotate: eastOpen.Angle},
		{X: c.Doors[east].X, Z: c.Doors[east].Z, Level: 0, Type: -1, Shape: 22, Deleted: true},
	})
	if other != c.DefaultState {
		t.Errorf("records that are not about a door moved the state: %b", other)
	}
}

// A shut door is a wall, an open door is a way through, and the open leaf is a wall of its own somewhere else.
func TestDoorLeavesAreWalls(t *testing.T) {
	c := castle(t)
	// The east room's east door (3292,3931, angle east): shut, 3292 -> 3293 is a wall; open, the passage is free and
	// the leaf (3293,3931, angle south) walls 3293,3931 off from 3293,3930.
	e := doorIndex(t, c, 3292, 3931, 0)
	shut, open := c.FlagsAt(Closed(e)), c.FlagsAt(State(0).With(e, false, true))
	if CanStep(shut, 0, 3292, 3931, 1, 0) {
		t.Error("walked through a shut door")
	}
	if !CanStep(open, 0, 3292, 3931, 1, 0) {
		t.Error("could not walk through the open door")
	}
	if CanStep(open, 0, 3293, 3931, 0, -1) {
		t.Error("the open leaf is not a wall: stepped from 3293,3931 south past it")
	}
	if !CanStep(shut, 0, 3293, 3931, 0, -1) {
		t.Error("with the door shut the way south of 3293,3931 should be free")
	}
	// Both leaves absent is a doorway with nothing in it.
	none := c.FlagsAt(State(0))
	if !CanStep(none, 0, 3292, 3931, 1, 0) || !CanStep(none, 0, 3293, 3931, 0, -1) {
		t.Error("a door with neither leaf is not open ground")
	}
}

// The diagonal doors block their whole tile, shut or open, and opening one moves the block: the main door's
// doorway 3279,3939 is solid until it is opened, and then 3279,3938 is.
func TestDiagonalDoorMovesItsBlock(t *testing.T) {
	c := castle(t)
	m := doorIndex(t, c, 3279, 3939, 0)
	if !c.Doors[m].Diagonal() {
		t.Fatal("the main door should be a diagonal door")
	}
	shut, open := c.FlagsAt(Closed(m)), c.FlagsAt(State(0).With(m, false, true))
	if Enterable(shut, 3279, 3939, 0) || !Enterable(shut, 3279, 3938, 0) {
		t.Error("shut: the doorway should be solid and 3279,3938 free")
	}
	if !Enterable(open, 3279, 3939, 0) || Enterable(open, 3279, 3938, 0) {
		t.Error("open: the doorway should be free and 3279,3938 solid (the leaf landed on the old stand tile)")
	}
	// The stand the catalogue used to name is exactly the tile the leaf covers: pushed off by the engine, and unenterable after.
	push, ok := c.Doors[m].Push()
	if !ok || push != (Tile{3280, 3938, 0}) {
		t.Fatalf("push = %v %v, want 3280,3938,0", push, ok)
	}
	// From either side of the open door the way out is the doorway, not the tile the leaf is on.
	if !CanStep(open, 0, 3280, 3939, -1, 0) || !CanStep(open, 0, 3279, 3939, 0, 1) {
		t.Error("3280,3939 -> 3279,3939 -> 3279,3940 should be a way out of the opened main door")
	}
}

// Plans: a shut door is opened from a tile the engine accepts, an open door is walked through, a door that
// will not open is gone round, and closing is the last thing tried.
func TestPlannerHandlesShutOpenAndLockedDoors(t *testing.T) {
	c := castle(t)
	m := doorIndex(t, c, 3279, 3939, 0)
	x2 := doorIndex(t, c, 3282, 3934, 0)
	room := Tile{3281, 3935, 0} // the stairs room, beside the stairs

	// Shut: walk to a stand, open, walk out. The stand is a tile the engine's reach test accepts for the closed
	// leaf, and not the tile the leaf lands on (no push needed).
	p := c.Plan(room, Options{State: c.DefaultState})
	if !p.OK || p.Ops() != 1 {
		t.Fatalf("stairs room, door shut: %+v, want a plan with one door op", p)
	}
	var op Step
	for _, s := range p.Steps {
		if s.Kind == StepDoor {
			op = s
		}
	}
	if op.Door != m || op.Close {
		t.Fatalf("the op is %+v, want opening the main door", op)
	}
	if !Reached(c.FlagsAt(c.DefaultState), 0, op.Stand.X, op.Stand.Z, c.Doors[m].Closed(), doorLocType) {
		t.Errorf("the planned stand %v does not reach the closed leaf", op.Stand)
	}
	if op.Stand == (Tile{3279, 3938, 0}) {
		t.Errorf("the planned stand is the tile the leaf lands on, %v: the account would be pushed off it", op.Stand)
	}
	if op.After != op.Stand {
		t.Errorf("the account is planned to end on %v after an op from %v that needs no push", op.After, op.Stand)
	}

	// Open already (another account's work): no op at all, a walk through.
	open := c.DefaultState.With(m, false, true)
	p = c.Plan(room, Options{State: open})
	if !p.OK || p.Ops() != 0 {
		t.Fatalf("stairs room, main door already open: %+v, want a plain walk", p)
	}

	// A door that will not open from the stand it was tried from (locked, or the click is refused): the plan
	// goes round by another stand; refused from every stand, there is no way out and the planner says so.
	refused := map[OpKey]bool{}
	var firstStand Tile
	firstStand = op.Stand
	refused[OpKey{StepDoor, m, false, firstStand}] = true
	p = c.Plan(room, Options{State: c.DefaultState, AvoidOp: refused})
	if !p.OK {
		t.Fatalf("with one stand refused the planner found no way out: %+v", p)
	}
	for _, s := range p.Steps {
		if s.Kind == StepDoor && s.Door == m && s.Stand == firstStand {
			t.Errorf("the plan used the refused stand %v again", firstStand)
		}
	}
	for _, st := range []Tile{{3279, 3938, 0}, {3280, 3939, 0}} {
		refused[OpKey{StepDoor, m, false, st}] = true
	}
	p = c.Plan(room, Options{State: c.DefaultState, AvoidOp: refused})
	if p.OK {
		// The main door is the only way out of the stairs room except through the hall, which is behind the other
		// diagonal door: that route exists, and must not use the main door at all.
		for _, s := range p.Steps {
			if s.Kind == StepDoor && s.Door == m {
				t.Errorf("plan opens the main door from %v although every stand of it was refused", s.Stand)
			}
		}
		uses := false
		for _, s := range p.Steps {
			uses = uses || (s.Kind == StepDoor && s.Door == x2)
		}
		if !uses {
			t.Errorf("plan goes round the main door without using the hall door: %+v", p.Steps)
		}
	}
	// Refuse the other diagonal door as well, from every stand it has: now the stairs room is sealed.
	for _, st := range []Tile{{3281, 3934, 0}, {3282, 3935, 0}, {3283, 3934, 0}} {
		refused[OpKey{StepDoor, x2, false, st}] = true
	}
	p = c.Plan(room, Options{State: c.DefaultState, AvoidOp: refused})
	if p.OK || !strings.Contains(p.Reason, "no way out") {
		t.Errorf("every door of the stairs room refused from every stand: %+v, want no way out", p)
	}
}

// Two doors whose leaves land on each other's passage seal the tile between them: with the east room's door and the
// east chamber's door both open, 3293,3931 has no walk out. The way out is back the way the account came, through
// the east room and the hall (nothing to close), and when that way is not there (the hall door refused) the
// plan closes a door, the dear way; with either door shut a plain walk gets out.
func TestPlannerEscapesTheTwoDoorTrap(t *testing.T) {
	c := castle(t)
	e := doorIndex(t, c, 3292, 3931, 0)
	f := doorIndex(t, c, 3294, 3932, 0)
	d4 := doorIndex(t, c, 3288, 3930, 0)
	alcove := Tile{3293, 3931, 0}

	both := c.DefaultState.With(e, false, true).With(f, false, true)
	if p := c.Plan(alcove, Options{State: both, NoOps: true}); p.OK {
		t.Fatalf("a walk out of the alcove exists with both doors open: %+v", p)
	}
	back := c.Plan(alcove, Options{State: both})
	if !back.OK || back.Ops() == 0 {
		t.Fatalf("no way out of the alcove with both doors open: %+v", back)
	}
	for _, s := range back.Steps {
		if s.Kind == StepDoor && s.Close {
			t.Errorf("the plan closes a door although the way back through the east room is open: %+v", back.Steps)
		}
	}
	// Refuse the hall door from every stand: the east room is a dead end now and only a close gets out.
	refused := map[OpKey]bool{}
	for _, st := range []Tile{{3288, 3929, 0}, {3288, 3930, 0}, {3289, 3930, 0}} {
		refused[OpKey{StepDoor, d4, false, st}] = true
	}
	shut := c.Plan(alcove, Options{State: both, AvoidOp: refused})
	if !shut.OK {
		t.Fatalf("no way out of the alcove with both doors open and the hall door refused: %+v", shut)
	}
	closes := 0
	for _, s := range shut.Steps {
		if s.Kind == StepDoor && s.Close {
			closes++
		}
	}
	if closes != 1 {
		t.Errorf("the plan closes %d doors, want exactly one: %+v", closes, shut.Steps)
	}
	if shut.Cost <= back.Cost {
		t.Errorf("closing (%d) is not dearer than going back (%d), so it would not be the last resort", shut.Cost, back.Cost)
	}
	if p := c.Plan(alcove, Options{State: both, AvoidOp: refused, NoClose: true}); p.OK {
		t.Errorf("a no-close plan exists with the hall door refused: %+v", p)
	}
	for _, s := range []State{c.DefaultState, c.DefaultState.With(e, false, true), c.DefaultState.With(f, false, true)} {
		if p := c.Plan(alcove, Options{State: s, NoOps: true}); !p.OK {
			t.Errorf("a walk out of the alcove should exist with at most one of the two doors open: %b", s)
		}
	}
}

// A plan from a tile outside the grid, and from one already out, are answered without a search.
func TestPlannerEdgeCases(t *testing.T) {
	c := castle(t)
	if p := c.Plan(Tile{1, 1, 0}, Options{State: c.DefaultState}); p.OK || p.Reason == "" {
		t.Errorf("a tile outside the grid: %+v", p)
	}
	if p := c.Plan(Tile{3279, 3945, 0}, Options{State: c.DefaultState}); !p.OK || len(p.Steps) != 0 || p.Expanded != 0 {
		t.Errorf("a tile already out: %+v", p)
	}
	if p := c.Plan(Tile{3281, 3935, 0}, Options{State: c.DefaultState, MaxExpansions: 3}); p.OK || !strings.Contains(p.Reason, "budget") {
		t.Errorf("a budget of three expansions: %+v, want the budget reason", p)
	}
}
