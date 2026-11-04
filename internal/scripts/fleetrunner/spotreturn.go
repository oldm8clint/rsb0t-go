package fleetrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// RefugeMaxSteps bounds the walk to the nearest ground no hunter holds: a pen's herd chases a few tiles beyond its spawns, and a
// tile further than this from free ground is one the account should have left the pen from.
const RefugeMaxSteps = 24

// RunPath clicks the walk along path once, run held, and waits for the account to stand on dest, eating at the eat line against
// what faces it; false when it is not there in time or the wait was cancelled. It sends nothing more: a view that starts lagging
// mid-run is waited out, and the next step finds where the account really is.
func RunPath(d *Deps, from tile.Tile, path []tile.Tile, dest tile.Tile) bool {
	if len(path) == 0 {
		return false
	}
	points := make([]pathfind.NavPoint, 0, len(path)+1)
	points = append(points, NavOf(from))
	for _, p := range path {
		points = append(points, NavOf(p))
	}
	if err := actions.Walk(d.Session, points, true); err != nil {
		d.Logf("training: walking back to the safespot: %v", err)
		return false
	}
	var lastBite time.Time
	deadline := time.Now().Add(time.Duration(len(path))*800*time.Millisecond + 4*time.Second)
	for time.Now().Before(deadline) {
		if h := CurrentTile(d); h != nil && *h == dest {
			return true
		}
		EatIfLow(d, &lastBite, "off the safespot")
		if !d.Delay(300 * time.Millisecond) {
			break
		}
	}
	h := CurrentTile(d)
	return h != nil && *h == dest
}
