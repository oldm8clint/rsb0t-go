package freshstartrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/tile"
)

// The range door occupies a walkable tile, but its north edge is baked closed.
// It accepts Open from its own tile; generic neighbouring-door approaches fail.
var rangeDoorOutside = tile.Tile{X: 2816, Z: 3438, Level: 0}
var rangeDoorInside = tile.Tile{X: 2816, Z: 3439, Level: 0}

func rangeDoorSides(pk *pathfind.Pack, from, dest tile.Tile) (near, far tile.Tile, ok bool) {
	if pk == nil || from.Level != 0 || dest.Level != 0 || tile.Chebyshev(from, rangeDoorInside) > 15 {
		return
	}
	reachable := func(a, b tile.Tile) bool {
		return pk.FindPath(pathfind.NavPoint{X: a.X, Z: a.Z, Level: a.Level}, pathfind.NavPoint{X: b.X, Z: b.Z, Level: b.Level}).OK
	}
	if reachable(from, rangeDoorInside) && reachable(rangeDoorOutside, dest) {
		return rangeDoorInside, rangeDoorOutside, true
	}
	if reachable(from, rangeDoorOutside) && reachable(rangeDoorInside, dest) {
		return rangeDoorOutside, rangeDoorInside, true
	}
	return
}

func tryRangeDoor(d *Deps, from, dest tile.Tile) bool {
	near, far, ok := rangeDoorSides(d.Pack, from, dest)
	if !ok || d.Locs == nil || d.Config == nil || !walkAdjacent(d, near, 0, 15*time.Second) {
		return false
	}
	if randomevents.Busy(d.Session) {
		return false
	}
	unlock, ok := randomevents.TryLock(d.Session)
	if !ok {
		return false
	}
	defer unlock()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	query := locindex.Query{X: 2816, Z: 3438, Level: 0, Radius: 0, Name: "Door", Op: "Open"}
	if door, closed := d.Locs.Nearest(d.Config, query, live); closed {
		d.logf("opening Catherby range door from %d,%d", near.X, near.Z)
		if actions.OpLoc(d.Session, door.Op, door.X, door.Z, door.ID) != nil {
			return false
		}
		if !waitFor(d, "Catherby range door opening", 3*time.Second, func() bool {
			_, closed := d.Locs.Nearest(d.Config, query, locindex.ChangesFrom(d.Session.State().LocChanges()))
			return !closed
		}) {
			return false
		}
	}
	if actions.Walk(d.Session, []pathfind.NavPoint{{X: far.X, Z: far.Z, Level: far.Level}}, false) != nil {
		return false
	}
	return waitFor(d, "crossing Catherby range door", 5*time.Second, func() bool {
		here := currentTile(d)
		return here != nil && *here == far
	})
}
