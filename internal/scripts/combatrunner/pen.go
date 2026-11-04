package combatrunner

// A rung whose ground is a fenced pen (combatplan.Enclosure): is the account inside it, how far is a monster by a walk over open
// ground, and the walk in by the gate. combatplan/pen.go has the reason; this is the half that reads the collision pack, the way
// questmods/cowfarm.go does for the quest farms (it is a copy of that file's cowGround, which this package cannot import).
//
// The pack the questions are asked of is the BAKED one, every door and gate shut. That is the point: "inside" means on the pen's side
// of the fence, whatever state the gate is in. With the gate opened (by this account or another, the server leaves it open for five
// minutes) the live pack would read an account on the road as inside, and a monster behind the fence as a short walk away.

import (
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// PenWindow is the half side of the square the flood over open ground covers around the account: wider than the pen's longest
	// reach from its entry, so every cow in view is inside it.
	PenWindow = 34
	// PenFloodReuse is the longest a flood is reused while the account stands on the same tile.
	PenFloodReuse = 2 * time.Second
)

// PenGround is the open ground round the account, flooded over the collision pack, and what it says about the pen. It holds the last
// flood, so a pick that asks about a dozen monsters from one tile floods once.
type PenGround struct {
	pen   combatplan.Enclosure
	pk    *pathfind.Pack
	reach *pathfind.Reach
	from  tile.Tile
	at    time.Time
	// now is the clock the reuse is judged by (time.Now; a test's own).
	now func() time.Time
}

// NewPenGround is the ground of pen read from pk. A nil pack answers as if the account were always inside and every monster a
// distance away: the trainer then behaves as it did before it knew about pens.
func NewPenGround(pk *pathfind.Pack, pen combatplan.Enclosure) *PenGround {
	return &PenGround{pen: pen, pk: pk, now: time.Now}
}

// Pen is the pen this ground is of.
func (g *PenGround) Pen() combatplan.Enclosure { return g.pen }

// flood is the open ground around here, nil when the pack is not loaded or the account stands off it.
func (g *PenGround) flood(here tile.Tile) *pathfind.Reach {
	if g.pk == nil {
		return nil
	}
	if g.reach != nil && g.from == here && g.now().Sub(g.at) < PenFloodReuse {
		return g.reach
	}
	reach := g.pk.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, PenWindow, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	g.reach, g.from, g.at = reach, here, g.now()
	return reach
}

// Inside reports whether here is in the pen: the pen's Enter tile is a walk over open ground away. From the road side of the gate it
// is not, whatever the distance. Without a pack it cannot be told and is true.
func (g *PenGround) Inside(here tile.Tile) bool {
	if g.pk == nil {
		return true
	}
	reach := g.flood(here)
	if reach == nil {
		return false
	}
	_, ok := reach.Steps(g.pen.Enter.X, g.pen.Enter.Z, g.pen.Enter.Level)
	return ok
}

// Steps is the walk from here to a tile a melee attack on whatever stands at t is accepted from, over open ground, -1 for none. Not
// the distance to t's own tile: see pathfind.Reach.InteractSteps. Without a flood (no pack, or an account standing off it) it is the
// distance, and -1 for another level.
func (g *PenGround) Steps(here, t tile.Tile) int {
	if t.Level != here.Level {
		return -1
	}
	reach := g.flood(here)
	if reach == nil {
		return tile.Chebyshev(here, t)
	}
	return reach.InteractSteps(pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level})
}

// EnterPen walks the account into the pen by its gate, when it is not in it, and reports whether it ends inside. The walk is to the
// pen's Enter tile with its EnterRadius, every floor tile within which is inside (a test holds that on the real pack): the walker
// stops at the first tile inside the radius it was given, and the quest farms' "within 8" and "within 12" of the pasture's middle
// ended on the road, outside the fence, with every cow behind it (18 October 2026).
func EnterPen(d *Deps, g *PenGround, timeout time.Duration) bool {
	here, ok := currentTile(d)
	if !ok {
		return false
	}
	if g.Inside(here) {
		return true
	}
	pen := g.Pen()
	d.logf("pen: at %d,%d,%d, outside the pen: walking in by the gate to %d,%d,%d (within %d)",
		here.X, here.Z, here.Level, pen.Enter.X, pen.Enter.Z, pen.Enter.Level, pen.EnterRadius)
	if !TravelTo(d, pen.Enter, pen.EnterRadius, timeout) {
		return false
	}
	now, ok := currentTile(d)
	if !ok || !g.Inside(now) {
		d.logf("pen: the walk in ended at %d,%d,%d, still outside the pen", now.X, now.Z, now.Level)
		return false
	}
	return true
}
