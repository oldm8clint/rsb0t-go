package combatrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Walking through doors (Deps.Doors), for the runs whose targets stand in rooms: the CombatTrainer's rungs at the
// Al Kharid palace, Falador castle and the Barbarian Village long hall. What the plan of it is, and the rules
// that keep it from becoming a loop, is combatplan/doors.go.
//
// Everything here is opt-in. The pack reach is judged on has every door shut, which is exactly right for a
// caller that must NOT go through a gate (the Ardougne pens' safe spots are a fence between the account and the
// cow), so Deps.Doors is false everywhere but the trainer and the two calls it makes in FightOne and WalkNear.

// DoorCrossTimeout bounds one crossing: the walk to the door, the Open, the wait for the leaf and the step
// through. The webwalk's own door crossing is a few seconds; the rest is its ladder when a door will not open.
const DoorCrossTimeout = 30 * time.Second

// doorPlanExpansions caps the planner's search for a route through doors. A route inside a room takes tens of
// expansions and a few hundred for a hall with two doors; a target no door leads to (a hut behind a diagonal
// door the graph has no edge for) would otherwise search for the planner's default budget of 1.2 million.
const doorPlanExpansions = 6000

// walkNearCrossings bounds the doors one WalkNear call opens on its way.
const walkNearCrossings = 2

// Reach is how a target is reached from where the account stands.
type Reach struct {
	// OK is whether there is a route at all. Doors is the shut doors and gates it crosses, 0 for a plain walk
	// (the route the baked pack, or the pack with the doors the server has opened, offers).
	OK    bool
	Doors int
	// Cost is the door route's cost in tiles, 0 for a plain walk.
	Cost int
	// Why is the reason when OK is false.
	Why string
	// First names the first door on a door route ("Large door at 3287,3172"), for the log.
	First string
	// Landing is the tile just past the route's last door: where CrossDoors walks to.
	Landing tile.Tile
}

// LivePack is the baked pack with the doors and walls the server has opened or shut since the bake laid over it,
// d.Pack itself when there is nothing to lay (or no loc index to read the base walls from). The bake has every
// door shut, so a room with its door standing open reads sealed on the pack alone.
func LivePack(d *Deps) *pathfind.Pack {
	if d.Pack == nil || d.Locs == nil || d.Config == nil || d.Session == nil {
		return d.Pack
	}
	return d.Pack.WithWalls(locindex.WallEdits(d.Locs, d.Config, d.Session.State().LocChanges()))
}

func navPoint(t tile.Tile) pathfind.NavPoint {
	return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level}
}

// ReachPlain is Reachable for a run that goes through doors, without the doors: the box search on the baked pack,
// then, with Deps.Doors, on the pack with the doors as the server has them (a door somebody has opened is no wall).
// OK is a walk with nothing to open. The answer a caller that ranks targets asks first for every one of them,
// because most are on the account's side of every wall and the planner (ReachThroughDoors) is worth asking only
// about the rest.
//
// It fails open as Reachable does: no pack or no known tile answers OK.
func ReachPlain(d *Deps, target tile.Tile) Reach {
	if d.Pack == nil {
		return Reach{OK: true, Why: "no collision pack is loaded, so reachability cannot be asked"}
	}
	here, ok := currentTile(d)
	if !ok {
		return Reach{OK: true, Why: "our own tile is not known yet"}
	}
	if here.Level != target.Level {
		return Reach{Why: "a different level"}
	}
	res := d.Pack.ReachableWithin(navPoint(here), navPoint(target), ApproachRadius)
	if res.OK {
		return Reach{OK: true}
	}
	if d.Doors {
		if live := LivePack(d); live != d.Pack {
			if r := live.ReachableWithin(navPoint(here), navPoint(target), ApproachRadius); r.OK {
				return Reach{OK: true}
			}
		}
	}
	return Reach{Why: res.Reason}
}

// ReachFor is ReachPlain and then, when Deps.Doors is set and a Walker is configured, the webwalk's own plan through
// the doors of its graph (ReachThroughDoors), accepted when it crosses doors and nothing else and is not long
// (combatplan.DoorRouteOK).
func ReachFor(d *Deps, target tile.Tile) Reach {
	r := ReachPlain(d, target)
	if r.OK || !d.Doors {
		return r
	}
	return ReachThroughDoors(d, target, r.Why)
}

// ReachThroughDoors is the webwalk's plan to target, for a target ReachPlain refused (plainWhy is its reason). Not OK
// without Deps.Doors and a Walker.
func ReachThroughDoors(d *Deps, target tile.Tile, plainWhy string) Reach {
	if !d.Doors {
		return Reach{Why: plainWhy}
	}
	return doorReach(d, target, plainWhy)
}

// doorReach asks the webwalk's planner for a route to target through doors. boxWhy is why the box search said no.
func doorReach(d *Deps, target tile.Tile, boxWhy string) Reach {
	if d.Walker == nil {
		return Reach{Why: boxWhy}
	}
	route := d.Walker.Plan(webwalk.Point{X: target.X, Z: target.Z, Level: target.Level}, webwalk.WalkOptions{
		Radius:     1,
		NoTeleport: true,
		Search:     webwalk.Options{MaxExpansions: doorPlanExpansions},
	})
	dr := combatplan.DoorRoute{OK: route.OK, Cost: route.Cost}
	var first, last *webwalk.Edge
	for _, h := range route.Hops() {
		if (h.Kind == webwalk.KindDoor || h.Kind == webwalk.KindGate) && h.Special == nil && h.HasLoc() {
			dr.Doors++
			if first == nil {
				first = h
			}
			last = h
			continue
		}
		dr.Other++
	}
	if ok, why := combatplan.DoorRouteOK(dr); !ok {
		return Reach{Why: boxWhy + "; " + why}
	}
	return Reach{
		OK: true, Doors: dr.Doors, Cost: route.Cost,
		First:   fmt.Sprintf("%s at %d,%d", first.LocName, first.LocX, first.LocZ),
		Landing: tile.Tile{X: last.To.X, Z: last.To.Z, Level: last.To.Level},
	}
}

// CrossDoors walks the account through the doors of a door route (ReachFor with Doors above zero) to the tile just
// past the last one, opening each that is shut, and reports whether it got there. The webwalk does the work, so
// the door is found the way every other crossing finds it, a refusing door is struck and banned the way it is
// everywhere, and the walk's own lines ("hop [door] Open", "crossed Large door at 3287,3172") are in the log.
//
// Afterwards the doors stand open for the five minutes the server leaves them (open_and_close_doors.rs2), and the
// pack with the doors as the server has them (LivePack) sees them so: the rest of the way is an ordinary walk.
func CrossDoors(d *Deps, r Reach) bool {
	if d.Walker == nil || r.Doors == 0 {
		return false
	}
	from, _ := currentTile(d)
	res := d.Walker.WalkTo(webwalk.Point{X: r.Landing.X, Z: r.Landing.Z, Level: r.Landing.Level}, webwalk.WalkOptions{
		Radius:     0,
		Timeout:    DoorCrossTimeout,
		NoTeleport: true,
	})
	if res.Arrived() {
		d.logf("door route: crossed %d door(s) from %d,%d,%d to %d,%d,%d (first: %s)", r.Doors, from.X, from.Z, from.Level, r.Landing.X, r.Landing.Z, r.Landing.Level, r.First)
		return true
	}
	d.logf("door route: did not cross %d door(s) from %d,%d,%d toward %d,%d,%d (first: %s): the walk ended %s (%s)",
		r.Doors, from.X, from.Z, from.Level, r.Landing.X, r.Landing.Z, r.Landing.Level, r.First, res.Outcome, res.Reason)
	return false
}

// ReachStand puts the account on the stand's own side of any wall, for a trainer that finds nothing it can reach
// from where it stands. TravelTo(stand, 2) is satisfied by a tile on the wrong side of a wall (the Al Kharid
// stand has a Large door two tiles to its west), so asking the pack first is the point: a stand the account can
// walk to is already reached, one behind doors is crossed to, and only a stand no route reaches is left to the
// travel's own way out (a sealed room).
func ReachStand(d *Deps, stand tile.Tile, timeout time.Duration) bool {
	r := ReachFor(d, stand)
	switch {
	case !r.OK:
		return TravelTo(d, stand, 2, timeout)
	case r.Doors > 0:
		return CrossDoors(d, r)
	}
	return true
}

// livePathNear is a path to dest on the pack with the doors as the server has them, for WalkNear's fallback when the
// baked pack has none. Bounded to a box round the account, because a live pack has lost the baked one's memory of
// what is unreachable and a search for something that is would flood every tile the account can walk to.
func livePathNear(d *Deps, here, dest tile.Tile) (pathfind.PathResult, bool) {
	live := LivePack(d)
	if live == nil || live == d.Pack {
		return pathfind.PathResult{}, false
	}
	box := tile.Chebyshev(here, dest) + ApproachRadius
	res := live.FindPathAvoiding(navPoint(here), navPoint(dest), func(x, z, level int) bool {
		return level != here.Level || tile.Chebyshev(tile.Tile{X: x, Z: z}, tile.Tile{X: here.X, Z: here.Z}) > box
	})
	return res, res.OK
}
