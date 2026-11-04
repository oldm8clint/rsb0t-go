package freshstartrunner

import (
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/locreach"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/tile"
)

// A stile is a one-click crossing into wherever is on the other side, and the other side can be a
// field with no other way out.
//
// Live 2026-09-25, the ScarabSwarm Farmer rung north of East Ardougne (acct57, Acct217, Acct225,
// Acct226): walking from the farmyard to a Farmer behind the farmhouse wall, walkNearCore found no
// path and ran its recovery, and the recovery climbed the nearest "Stile" within ten tiles -- the
// sheep pen's, at 2647,3375, which has nothing to do with the farmhouse. The pen is a 269-tile
// field with that one stile as its only way in or out (cmd/doordiag on the real pack). Every
// account that went in stayed in: the old approach search tried the four tiles orthogonally beside
// the stile's ORIGIN tile, but content's fullstyle is two tiles long (length=2), so at angle 1 it
// covers 2647-2648,3375 and the tile beside it from inside the pen is 2649,3375, which that search
// never tried. "could not reach any of its 4 approach tiles", STRANDED, and a Lumbridge Teleport
// none of them could cast, for hours.
//
// So a climb now has to be worth making, and possible from either side:
//
//   - The tiles beside the obstacle come from its real footprint and placement (internal/locreach,
//     the engine's own reach rule for shape 10), so a two-tile stile can be crossed back.
//   - It is climbed only when the far side leads somewhere: to the walk's destination on the live
//     navigation pack, or, from inside a small enclosed area, out to open ground. Climbing into a
//     dead end on the way to somewhere else is refused, with a line saying so.
//   - From inside a small enclosed area the search reaches the whole area rather than ten tiles, so
//     an account in the far corner of the pen still finds the pen's only stile.

// climbOverRadius is how far from the account tryClimbOver looks for an obstacle on open ground.
const climbOverRadius = 10

// climbPlan is tryClimbOver's decision about one obstacle.
type climbPlan struct {
	// near are the tiles beside the obstacle the account can walk to, nearest first.
	near []tile.Tile
	ok   bool
	why  string
}

// climbStands is every tile the engine accepts an op on m from, the footprint left out (a stile
// blocks walking), read from the loc's real size and placement. Without the loc's type it is the
// origin's four neighbours, the set this package used before.
func climbStands(types locindex.Types, m locindex.Match) []tile.Tile {
	l := locreach.Loc{X: m.X, Z: m.Z, Level: m.Level, Width: 1, Length: 1, Angle: m.Angle, Shape: m.Shape}
	if types != nil {
		if typ, ok := types.Loc(m.ID); ok {
			l.Width, l.Length, l.ForceApproach = typ.Width, typ.Length, typ.ForceApproach
		}
	}
	x, z, w, h := l.Footprint()
	var out []tile.Tile
	for _, t := range locreach.Tiles(l) {
		if t.X >= x && t.X < x+w && t.Z >= z && t.Z < z+h {
			continue
		}
		out = append(out, t)
	}
	return out
}

func navOf(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

// inPocket reports whether from stands in a small enclosed area on pk: fewer walkable tiles joined
// to it than largeComponentThreshold, the size tryEscapeSealedPocket already uses to tell a room from
// the open world. room is that area when it is one.
func inPocket(pk *pathfind.Pack, from tile.Tile) (room map[tile.Tile]bool, pocket bool) {
	room = localComponent(pk, from, largeComponentThreshold)
	return room, len(room) < largeComponentThreshold
}

// planClimb decides whether climbing the obstacle whose stand tiles are stands helps an account at
// from that is trying to reach dest, on pk. Pure: no session, no I/O.
//
// A stand the account can walk to (within reachableFixtureWindow) is on its side; the rest are
// across. The climb is worth it when a tile across can walk to dest, or when the account is in a
// pocket and a tile across opens onto ground at least largeComponentThreshold tiles big.
func planClimb(pk *pathfind.Pack, from, dest tile.Tile, stands []tile.Tile, pocket bool) climbPlan {
	if pk == nil {
		return climbPlan{why: "no collision pack to tell where it leads"}
	}
	var near, far []tile.Tile
	for _, s := range stands {
		if s.Level != from.Level || !pk.Walkable(s.X, s.Z, s.Level) {
			continue
		}
		if s == from || pk.ReachableWithin(navOf(from), navOf(s), reachableFixtureWindow).OK {
			near = append(near, s)
		} else {
			far = append(far, s)
		}
	}
	if len(near) == 0 {
		return climbPlan{why: "no tile beside it can be walked to from here"}
	}
	if len(far) == 0 {
		return climbPlan{why: "both sides of it are already walkable from here, so it leads nowhere new"}
	}
	sort.SliceStable(near, func(i, j int) bool { return tile.Chebyshev(from, near[i]) < tile.Chebyshev(from, near[j]) })
	if pocket {
		for _, f := range far {
			if len(localComponent(pk, f, largeComponentThreshold)) >= largeComponentThreshold {
				return climbPlan{near: near, ok: true, why: "it leads out of this enclosed area to open ground"}
			}
		}
	}
	if from.Level == dest.Level {
		for _, f := range far {
			if r := pk.FindPathAvoiding(navOf(f), navOf(dest), inDangerZone); r.OK {
				return climbPlan{near: near, ok: true, why: "the far side walks to the destination"}
			}
		}
	}
	return climbPlan{why: "the far side does not lead to the destination"}
}

// climbSearchRadius is how far tryClimbOver looks: climbOverRadius on open ground, and far enough to
// take in the whole of a pocket's bounding box (plus the fence line and the obstacle beyond it) from
// inside one.
func climbSearchRadius(from tile.Tile, room map[tile.Tile]bool, pocket bool) int {
	r := climbOverRadius
	if !pocket || len(room) == 0 {
		return r
	}
	bb := roomBBox(room)
	for _, v := range []int{from.X - bb[0], bb[2] - from.X, from.Z - bb[1], bb[3] - from.Z} {
		if v+2 > r {
			r = v + 2
		}
	}
	return r
}

// tryClimbOver looks for a climb-over obstacle (climbOverNames) that gets the account at from
// closer to dest, walks beside it and climbs it — the same one-shot, re-evaluate-on-the-caller's-
// next-iteration shape as tryChangeLevel/tryOpenDoor. It reports whether the account actually
// crossed (its tile changed), not merely that a click was sent. See this file's doc comment for the
// live incident behind every rule here.
func tryClimbOver(d *Deps, from, dest tile.Tile) bool {
	if d.Locs == nil || d.Config == nil {
		return false
	}
	if cur := currentTile(d); cur != nil {
		from = *cur
	}
	pk := liveNavigationPack(d)
	if pk == nil {
		return false
	}
	room, pocket := inPocket(pk, from)
	radius := climbSearchRadius(from, room, pocket)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	var found []locindex.Match
	seen := map[[3]int]bool{}
	for _, name := range climbOverNames {
		for _, op := range climbOverOps {
			for _, m := range d.Locs.Find(d.Config, locindex.Query{X: from.X, Z: from.Z, Level: from.Level, Radius: radius, Name: name, Op: op}, live) {
				if guardianOwned(m) {
					continue
				}
				key := [3]int{m.X, m.Z, m.ID}
				if seen[key] {
					continue
				}
				seen[key] = true
				found = append(found, m)
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Distance < found[j].Distance })
	for _, m := range found {
		plan := planClimb(pk, from, dest, climbStands(d.Config, m), pocket)
		if !plan.ok {
			d.logf("not climbing %q at %d,%d,%d on the way to %d,%d,%d: %s", m.Name, m.X, m.Z, m.Level, dest.X, dest.Z, dest.Level, plan.why)
			continue
		}
		reached := false
		for _, s := range plan.near {
			if cur := currentTile(d); cur != nil && *cur == s {
				reached = true
				break
			}
			if walkAdjacent(d, s, 0, 8*time.Second) {
				reached = true
				break
			}
		}
		if !reached {
			d.logf("%q at %d,%d,%d: could not reach any of the %d tiles beside it on this side", m.Name, m.X, m.Z, m.Level, len(plan.near))
			continue
		}
		before := from
		if cur := currentTile(d); cur != nil {
			before = *cur
		}
		d.logf("climbing over %q at %d,%d,%d (%s)", m.Name, m.X, m.Z, m.Level, plan.why)
		if err := actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID); err != nil {
			d.logf("OpLoc on %q: %v", m.Name, err)
			return false
		}
		return waitFor(d, "the climb-over to finish", 5*time.Second, func() bool {
			cur := currentTile(d)
			return cur != nil && (cur.X != before.X || cur.Z != before.Z)
		})
	}
	return false
}
