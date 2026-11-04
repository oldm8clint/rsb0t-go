package freshstartrunner

import (
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// A level-change fixture the account cannot reach is not an exit.
//
// Live 2026-09-24, spare102: frozen at 3032,3218,1 for over an hour, logging "STRANDED IN
// KNOWN RANDOM-EVENT FAIL-TELEPORT POCKET: Port Sarim, above Brian's Battleaxe Bazaar" and
// "changing level via Ladder at 3049,3208,1 — no approach tile to Ladder worked" every 11 s. It was
// not in that pocket at all: 3032,3218,1 is the deck of the Karamja -> Port Sarim ship (content's
// customs_officer.rs2 lands a passenger on 3032,3217,1), a 46-tile walkable component on the real
// pack, x 3032..3036 z 3213..3224, whose only way off is the "Gangplank" (loc 2084,
// karamjashipplank_off, op "Cross") at 3031,3217. The Bazaar's ladder is in the same mapsquare and
// on the same level, 17 tiles away in a different, unreachable room, and tryChangeLevel always took
// it because it searches staircases and ladders first and only falls back to a gangplank when there
// is no stair within 25 tiles AT ALL. The deck's own "Ship's ladder"s are not "Ladder".
//
// pickReachableMatch is pickDirectionalMatch that skips a fixture no orthogonal neighbour of which
// the account can reach on the live navigation pack, so the nearest fixture that is actually an
// exit wins.

// reachableFixtureWindow bounds each reachability search to a box around the account: the rooms
// these fixtures sit in are small, and a route that has to leave the box to reach one is not
// "beside" the account.
const reachableFixtureWindow = 40

// fixtureReachable reports whether the account can stand orthogonally beside m, the only sides a
// directional fixture is guaranteed to accept (see tryChangeLevel), on pack.
func fixtureReachable(pack *pathfind.Pack, from tile.Tile, m locindex.Match) bool {
	if pack == nil || m.Level != from.Level {
		return false
	}
	if abs(from.X-m.X)+abs(from.Z-m.Z) == 1 {
		return true
	}
	window := func(x, z, level int) bool {
		return level != from.Level || abs(x-from.X) > reachableFixtureWindow || abs(z-from.Z) > reachableFixtureWindow
	}
	res := pack.FindPathAvoiding(pathfind.NavPoint{X: from.X, Z: from.Z, Level: from.Level}, pathfind.NavPoint{X: m.X, Z: m.Z, Level: m.Level}, window)
	if !res.OK || len(res.Waypoints) == 0 {
		return false
	}
	last := res.Waypoints[len(res.Waypoints)-1]
	return last.Level == m.Level && abs(last.X-m.X)+abs(last.Z-m.Z) == 1
}

// pickReachableMatch is pickDirectionalMatch over every candidate within radius, keeping only the
// ones fixtureReachable accepts: op priority first, then the nearest.
func pickReachableMatch(pack *pathfind.Pack, locs *locindex.Index, types locindex.Types, live []locindex.Change, from tile.Tile, names []string, opsByPriority []string, radius int) (*locindex.Match, string) {
	for _, op := range opsByPriority {
		var found *locindex.Match
		var foundName string
		bestDist := -1
		for _, name := range names {
			for _, m := range locs.Find(types, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: radius, Name: name, Op: op}, live) {
				if guardianOwned(m) {
					continue
				}
				dist := tile.Chebyshev(from, tile.Tile{X: m.X, Z: m.Z, Level: m.Level})
				if found != nil && dist >= bestDist {
					continue
				}
				if !fixtureReachable(pack, from, m) {
					continue
				}
				mCopy := m
				found, foundName, bestDist = &mCopy, name, dist
			}
		}
		if found != nil {
			return found, foundName
		}
	}
	return nil, ""
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
