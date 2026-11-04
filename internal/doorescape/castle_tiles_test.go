package doorescape

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// replay walks a plan out on the engine model, step by step, with nothing but the rules of the engine: every walked
// tile must be a legal step from the one before over the doors as they are at that moment, every click must be
// from a tile the engine's reach test accepts for the loc it names, a door's leaves change as open_door and
// close_door change them (with the push a diagonal door gives an account on the leaf's tile), a staircase lands
// where stairs.rs2 puts the account, and the plan must end on open ground outside the compound. It is the check
// on the planner's own reconstruction of its route.
func replay(c *Catalog, from Tile, s State, p Plan) error {
	pos := from
	for i, st := range p.Steps {
		f := c.FlagsAt(s)
		switch st.Kind {
		case StepWalk:
			for _, to := range st.Tiles {
				dx, dz := to.X-pos.X, to.Z-pos.Z
				if to.Level != pos.Level || dx < -1 || dx > 1 || dz < -1 || dz > 1 || (dx == 0 && dz == 0) {
					return fmt.Errorf("step %d: %v is not one step from %v", i, to, pos)
				}
				if !CanStep(f, pos.Level, pos.X, pos.Z, dx, dz) {
					return fmt.Errorf("step %d: the engine does not allow %v -> %v with the doors as they are", i, pos, to)
				}
				pos = to
			}
		case StepDoor:
			d := c.Doors[st.Door]
			if pos != st.Stand {
				return fmt.Errorf("step %d: door %q is clicked from %v but the account is at %v", i, d.Name, st.Stand, pos)
			}
			if st.Close {
				if !s.OpenLeaf(st.Door) || !Reached(f, pos.Level, pos.X, pos.Z, d.Open(), doorLocType) {
					return fmt.Errorf("step %d: closing door %q from %v is not possible", i, d.Name, pos)
				}
				if push, ok := d.PushClose(); ok && pos.X == d.X && pos.Z == d.Z {
					pos = push
				}
				s = s.With(st.Door, true, false)
			} else {
				if !s.ClosedLeaf(st.Door) || !Reached(f, pos.Level, pos.X, pos.Z, d.Closed(), doorLocType) {
					return fmt.Errorf("step %d: opening door %q from %v is not possible", i, d.Name, pos)
				}
				if push, ok := d.Push(); ok && pos.X == d.Open().X && pos.Z == d.Open().Z {
					pos = push
				}
				s = s.With(st.Door, false, true)
			}
			if pos != st.After {
				return fmt.Errorf("step %d: door %q leaves the account at %v, the plan says %v", i, d.Name, pos, st.After)
			}
		case StepStairs:
			stair := c.Stairs[st.Stair]
			if pos != st.Stand || !Reached(f, pos.Level, pos.X, pos.Z, stair.Placement, stair.Type) {
				return fmt.Errorf("step %d: the staircase %q cannot be operated from %v", i, stair.Name, pos)
			}
			pos = stair.Land
			if pos != st.After {
				return fmt.Errorf("step %d: staircase lands at %v, the plan says %v", i, pos, st.After)
			}
		}
	}
	if !c.Free(pos.X, pos.Z, pos.Level) {
		return fmt.Errorf("the plan ends at %v, which is not open ground outside the compound", pos)
	}
	return nil
}

// permissiveEscape says whether a tile can reach open ground at all if every door were gone and every staircase
// could be used both ways: the most any door state could ever allow.
func permissiveEscape(c *Catalog, from Tile) bool {
	f := c.FlagsAt(State(0))
	g := c.Grid
	seen := map[int]bool{}
	start := g.Index(from.X, from.Z, from.Level)
	queue := []int{start}
	seen[start] = true
	for len(queue) > 0 {
		t := g.TileAt(queue[0])
		queue = queue[1:]
		if c.Free(t.X, t.Z, t.Level) {
			return true
		}
		for d := 0; d < 8; d++ {
			if !CanStep(f, t.Level, t.X, t.Z, DX[d], DZ[d]) {
				continue
			}
			n := g.Index(t.X+DX[d], t.Z+DZ[d], t.Level)
			if n >= 0 && !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
		for _, st := range c.Stairs {
			if st.Placement.Level != t.Level || !Reached(f, t.Level, t.X, t.Z, st.Placement, st.Type) {
				continue
			}
			if n := g.Index(st.Land.X, st.Land.Z, st.Land.Level); n >= 0 && !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return false
}

// compoundTiles lists every tile of the compound that can be stood on with the doors as the map has them.
func compoundTiles(c *Catalog) []Tile {
	f := c.FlagsAt(c.DefaultState)
	var out []Tile
	for level := 0; level < c.Grid.Levels; level++ {
		r := c.Compound[level]
		for z := r.MinZ; z <= r.MaxZ; z++ {
			for x := r.MinX; x <= r.MaxX; x++ {
				if Enterable(f, x, z, level) {
					out = append(out, Tile{x, z, level})
				}
			}
		}
	}
	return out
}

// sealedPockets are the tiles of the castle with NO way out, listed by what the engine model says and checked
// against the most permissive model there is (every door removed, every staircase usable both ways): none of
// them can leave even then, so none can be entered on foot either, and the only way an account is ever on one is
// to be put there. They are a few rooms on the first and second floors that the map gives neither a door nor a
// staircase (and one doorway-less corner of the ground floor), none within reach of the summon's arrival tiles.
var sealedPockets = []Tile{
	{3283, 3939, 0},
	{3282, 3926, 1}, {3282, 3927, 1}, {3283, 3927, 1}, {3283, 3939, 1}, {3283, 3940, 1}, {3284, 3938, 1}, {3284, 3939, 1}, {3284, 3940, 1},
	{3284, 3941, 1}, {3285, 3938, 1}, {3285, 3939, 1}, {3285, 3940, 1}, {3285, 3941, 1}, {3286, 3939, 1}, {3286, 3940, 1},
	{3283, 3939, 2}, {3283, 3940, 2}, {3284, 3938, 2}, {3284, 3941, 2}, {3285, 3938, 2}, {3285, 3941, 2}, {3286, 3930, 2},
	{3286, 3931, 2}, {3286, 3939, 2}, {3286, 3940, 2}, {3287, 3929, 2}, {3287, 3932, 2}, {3288, 3929, 2}, {3288, 3932, 2},
	{3289, 3929, 2}, {3289, 3930, 2}, {3289, 3931, 2}, {3289, 3932, 2}, {3290, 3930, 2}, {3290, 3931, 2}, {3290, 3932, 2},
	{3293, 3933, 2}, {3293, 3934, 2}, {3294, 3932, 2}, {3294, 3935, 2}, {3295, 3932, 2}, {3295, 3935, 2}, {3296, 3933, 2},
	{3296, 3934, 2},
}

// From every tile of the castle on every level there is a plan out, bounded, that replays on the engine model and
// ends on open ground, except from the sealed pockets, which are exactly the tiles that cannot get out under any
// door state at all.
func TestEveryTileOfTheCastleHasAWayOut(t *testing.T) {
	c := castle(t)
	sealed := map[Tile]bool{}
	for _, s := range sealedPockets {
		sealed[s] = true
	}
	perLevel := map[int][2]int{}
	var noPlan []string
	maxCost, maxOps, maxExpanded := 0, 0, 0
	tiles := compoundTiles(c)
	for _, tl := range tiles {
		p := c.Plan(tl, Options{State: c.DefaultState})
		permissive := permissiveEscape(c, tl)
		n := perLevel[tl.Level]
		n[0]++
		switch {
		case p.OK:
			n[1]++
			if !permissive {
				t.Errorf("%v: a plan exists though no door state could ever let the tile out", tl)
			}
			if err := replay(c, tl, c.DefaultState, p); err != nil {
				t.Errorf("%v: the plan does not replay on the engine model: %v", tl, err)
			}
			if p.Cost > maxCost {
				maxCost = p.Cost
			}
			if p.Ops() > maxOps {
				maxOps = p.Ops()
			}
			if p.Expanded > maxExpanded {
				maxExpanded = p.Expanded
			}
			for _, s := range p.Steps {
				if s.Kind == StepDoor && s.Close {
					t.Errorf("%v: the default-state plan closes a door (%s), which is only for traps", tl, c.Doors[s.Door].Name)
				}
			}
		default:
			noPlan = append(noPlan, tl.String())
			if permissive {
				t.Errorf("%v: no plan (%s) though the tile can leave when every door is gone", tl, p.Reason)
			}
			if !sealed[tl] {
				t.Errorf("%v: no plan and not a listed sealed pocket", tl)
			}
		}
		perLevel[tl.Level] = n
	}
	var listed []string
	for _, s := range sealedPockets {
		listed = append(listed, s.String())
		if c.Free(s.X, s.Z, s.Level) || !c.Inside(s.X, s.Z, s.Level) {
			t.Errorf("sealed pocket %v is not inside the compound", s)
		}
	}
	sort.Strings(listed)
	sort.Strings(noPlan)
	if strings.Join(listed, " ") != strings.Join(noPlan, " ") {
		t.Errorf("the tiles without a plan are\n%v\nthe listed sealed pockets are\n%v", noPlan, listed)
	}
	// Bounded: the search settles a few hundred states, the route is a handful of doors.
	if maxCost > 45 || maxOps > 4 || maxExpanded > 2000 {
		t.Errorf("a plan out of the default state costs up to %d with %d ops and %d settled states: not the bounded search it is meant to be", maxCost, maxOps, maxExpanded)
	}
	t.Logf("%d tiles (level: enterable, with a plan): %v; %d sealed; worst plan cost %d, ops %d, settled states %d", len(tiles), perLevel, len(noPlan), maxCost, maxOps, maxExpanded)

	// The tiles the summons put accounts on, and every tile around the stairs, have a plan.
	for _, a := range []Tile{{3282, 3936, 3}, {3282, 3937, 3}, {3281, 3937, 3}, {3281, 3936, 3}, {3282, 3936, 2}, {3279, 3936, 2}, {3282, 3936, 1}, {3279, 3936, 1}, {3282, 3936, 0}, {3279, 3936, 0}} {
		if p := c.Plan(a, Options{State: c.DefaultState}); !p.OK {
			t.Errorf("arrival or landing tile %v has no plan: %+v", a, p)
		}
	}
}

// The same from every door state the golden file holds (all shut, all open, each door alone, every pair, 120 at
// random, with leaves both there or both gone among them): sampled across 10 of them, a tile that could ever leave
// still has a plan, and the plan replays on the engine model. Closing a door is allowed here, and is what gets
// an account out of the states that seal a room with its own open leaves.
func TestEveryTileHasAWayOutInAnyDoorState(t *testing.T) {
	g := loadGolden(t)
	c := castle(t)
	tiles := compoundTiles(c)
	var escapable []Tile
	for _, tl := range tiles {
		if permissiveEscape(c, tl) {
			escapable = append(escapable, tl)
		}
	}
	plans, closes := 0, 0
	maxCost, maxExpanded := 0, 0
	for n := 0; n < len(g.States); n += 20 {
		s := stateFromLeaves(t, g.States[n].Leaves, len(c.Doors))
		for _, tl := range escapable {
			p := c.Plan(tl, Options{State: s})
			plans++
			if !p.OK {
				t.Errorf("state %d (%s), tile %v: no way out (%s)", n, g.States[n].Leaves, tl, p.Reason)
				continue
			}
			if err := replay(c, tl, s, p); err != nil {
				t.Errorf("state %d, tile %v: the plan does not replay: %v", n, tl, err)
			}
			for _, st := range p.Steps {
				if st.Kind == StepDoor && st.Close {
					closes++
				}
			}
			if p.Cost > maxCost {
				maxCost = p.Cost
			}
			if p.Expanded > maxExpanded {
				maxExpanded = p.Expanded
			}
		}
	}
	// A plan out of any state stays bounded too.
	if maxExpanded > 20000 {
		t.Errorf("a plan out of some door state settled %d states", maxExpanded)
	}
	t.Logf("%d plans over %d door states: %d door closes used, worst cost %d, worst settled states %d", plans, (len(g.States)+19)/20, closes, maxCost, maxExpanded)
}
