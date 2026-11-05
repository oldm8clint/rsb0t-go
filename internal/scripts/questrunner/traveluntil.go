package questrunner

import (
	"time"

	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// TravelUntil walks to within radius of dest like Travel, with its own budget, and stops the walk as soon as stop returns true (asked every
// poll of the walk): a walk through ground a monster holds, which must break off when the account is hurt and has nothing left to eat
// (the Knight's Sword's visit to the Asgarnian Ice Dungeon). It reports whether the account stands within radius of dest, which a stopped
// walk does not; the account's eating beside the walk is the engine's. A nil stop is TravelFor without the stall bookkeeping.
func TravelUntil(d *Deps, dest tile.Tile, radius int, timeout time.Duration, stop func() bool) bool {
	here, ok := currentTileOK(d)
	if ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	if d.Walker == nil {
		return WalkNear(d, dest, radius)
	}
	defer d.beginWalk()()
	if ok {
		leavePocketFor(d, here, dest)
	}
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level},
		webwalk.WalkOptions{Radius: radius, Timeout: timeout, Until: stop, Combat: webwalk.CombatOptions{RunWhenAttacked: true}})
	at := currentTile(d)
	if at.Level == dest.Level && tile.Chebyshev(at, dest) <= radius {
		return true
	}
	if !res.Arrived() {
		d.logf("travel to %d,%d,%d: %s (%s), best %d", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason, res.Best)
	}
	return false
}
