package freshstartrunner

import (
	"sort"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// walkToUseApproaches caps how many of a loc's own interact tiles are tried before falling back,
// so one booth standing behind a counter cannot spend a whole bank trip walking.
const walkToUseApproaches = 3

// WalkToUse puts the account on a tile the engine accepts an interaction with the loc at target
// from, rather than merely one tile from it.
//
// WalkNear(target, 1) is Chebyshev, so it stops on the loc's four diagonals too, and the engine
// (inOperableDistance/reachRectangle1) refuses a click from a diagonal in silence: the bank never
// opens and the caller's 20-second wait burns. Live, 12 October 2026: every ScarabSwarm hand-over
// at the mule's Catherby post left the account on 2808,3441, corner to corner with the booths at
// 2807,3442 and 2809,3442, and the reopen after the trade timed out twice (about 1.15 timeouts per
// hand-over, 836 in 4.6 hours) before a walk to 2810,3441 opened the third. pathfind.CanInteractFrom
// is the engine's own rule (mulerunner, combatrunner and woodcutrunner each keep a copy).
//
// Falls back to WalkNear(target, 1) with no pack, no known tile, or no reachable interact tile, so
// it is never worse than the walk it replaces.
func WalkToUse(d *Deps, target tile.Tile, timeout time.Duration) bool {
	if d.Pack == nil {
		return WalkNear(d, target, 1, timeout)
	}
	here := currentTile(d)
	if here == nil {
		return WalkNear(d, target, 1, timeout)
	}
	at := pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level}
	if d.Pack.CanInteractFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, at) {
		return true
	}
	stands := d.Pack.InteractTiles(at)
	if len(stands) == 0 {
		return WalkNear(d, target, 1, timeout)
	}
	sort.SliceStable(stands, func(i, j int) bool {
		return tile.Chebyshev(*here, tile.Tile{X: stands[i].X, Z: stands[i].Z, Level: stands[i].Level}) <
			tile.Chebyshev(*here, tile.Tile{X: stands[j].X, Z: stands[j].Z, Level: stands[j].Level})
	})
	stands = reachableStands(d, *here, target, stands)
	// Said in the log only for the corner case: it is the click the server used to ignore.
	corner := here.Level == target.Level && tile.Chebyshev(*here, target) <= 1
	for i, stand := range stands {
		if i >= walkToUseApproaches {
			break
		}
		if WalkNear(d, tile.Tile{X: stand.X, Z: stand.Z, Level: stand.Level}, 0, timeout) {
			if corner {
				d.logf("walked from %d,%d onto %d,%d to use the loc at %d,%d: the tile we were on touched it corner to corner only, which the engine refuses",
					here.X, here.Z, stand.X, stand.Z, target.X, target.Z)
			}
			return true
		}
		// A stopping bot fails the next stand and the fallback walk the same way, and a stand on the far side of a
		// counter (the Catherby booths' north tiles, 2807..2812,3443) floods the whole component to say so.
		if !d.Delay(0) {
			return false
		}
	}
	return WalkNear(d, target, 1, timeout)
}

// walkToUseFloodSlack is how far past the loc the reachability flood looks, so a stand reached round a short counter
// or a doorway is still found.
const walkToUseFloodSlack = 12

// reachableStands keeps the stands one bounded flood from here reaches, in the order given, or all of them when it
// reaches none (a stand behind a door the baked pack has shut is still tried, as before). A stand on the keeper's side
// of a counter cost a failed walk and a door hunt each time it was tried: pprof 15 Oct 2026 (12:01, 1.37 fleet cores)
// put 27% of the fleet's CPU under openBoothNear -> WalkToUse -> walkNearCore -> tryOpenDoor/pickDoorCandidate.
func reachableStands(d *Deps, here, target tile.Tile, stands []pathfind.NavPoint) []pathfind.NavPoint {
	if here.Level != target.Level {
		return stands
	}
	ground := d.Pack.Flood(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, tile.Chebyshev(here, target)+walkToUseFloodSlack)
	var out []pathfind.NavPoint
	for _, s := range stands {
		if s.Level == here.Level && ground[[2]int{s.X, s.Z}] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return stands
	}
	return out
}
