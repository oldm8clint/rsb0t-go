package randomevents

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
)

// When HandleEvade finds no escape even on the live door overlay, open the closed door its way
// out goes through.
//
// Live on build e35420b6 (2026-09-24 18:21:09-18:33:03, the mule): the mule had just climbed out of
// the Lumbridge basement into the castle kitchen (3209,3217) when its Swarm spawned beside it. The
// kitchen's doors were shut, so on fleePack the room was sealed and HandleEvade answered "no safe
// reachable local escape" 196 times over twelve minutes. At 18:33 an ordinary walk left through
// the kitchen door at 3207,3217 in two seconds. fleeDoorBack does not cover this: it only uses a
// door the account is standing on or beside, and only for the combat flee.
//
// The fallback plans the same evade with every closed door within evadeDoorRadius opened, walks
// the first closed door that plan crosses, from the side the account is on, and opens it. It then
// returns, and the guardian's next attempt plans on fleePack again, now through the open door. The
// "route through operable doors, open the first blocked crossing" shape is freshstartrunner's
// pickPlannedDoor, which this package cannot import.

// evadeDoorRadius is how far from the account a closed door may be for the evade to plan through
// it.
const evadeDoorRadius = 16

// evadeMaxDoors bounds how many doors one HandleEvade call opens before it gives up planning: the
// Lumbridge kitchen's way out crosses three.
const evadeMaxDoors = 4

// evadeDoorOpenOps are the options that open a closed door or gate.
var evadeDoorOpenOps = []string{"Open"}

// closedDoorsNear lists the closed doors and gates (straight or corner walls with an Open option
// that block movement) within radius of x,z,level, on the map files with live changes applied.
// It never lists one inside known danger (dangerZones, wolf territory) unless the account already
// stands in that danger: the Catherby wolf zone is drawn generously enough to take in the range
// house, and the door the mule is shut in by there leads nowhere worse than where it is.
func closedDoorsNear(ix *locindex.Index, types locindex.Types, live []locindex.Change, x, z, level, radius int) []locindex.Match {
	var out []locindex.Match
	here := pathfind.NavPoint{X: x, Z: z, Level: level}
	inDanger, inWolves := inDangerZone(here), inWolfTerritory(here)
	consider := func(l locindex.Loc, isLive bool) {
		if l.Shape != 0 && l.Shape != 2 {
			return
		}
		t, ok := types.Loc(l.ID)
		if !ok || !t.BlockWalk {
			return
		}
		name := strings.ToLower(t.Name)
		if !strings.Contains(name, "door") && !strings.Contains(name, "gate") || strings.Contains(name, "trapdoor") {
			return
		}
		if p := (pathfind.NavPoint{X: l.X, Z: l.Z, Level: l.Level}); !inDanger && inDangerZone(p) || !inWolves && inWolfTerritory(p) {
			return
		}
		for _, label := range evadeDoorOpenOps {
			if op := cachecfg.OpIndex(t.Ops, label); op != 0 {
				out = append(out, locindex.Match{Loc: l, Name: t.Name, Op: op, Distance: chebyshev(x, z, l.X, l.Z), Live: isLive})
				return
			}
		}
	}
	changed := map[[2]int]bool{}
	for _, c := range live {
		if c.Level == level {
			changed[[2]int{c.X, c.Z}] = true
		}
	}
	ix.Each(x-radius, z-radius, x+radius, z+radius, level, func(l locindex.Loc) {
		if !changed[[2]int{l.X, l.Z}] {
			consider(l, false)
		}
	})
	for _, c := range live {
		if !c.Deleted && c.Level == level && chebyshev(x, z, c.X, c.Z) <= radius {
			consider(locindex.Loc{ID: c.ID, X: c.X, Z: c.Z, Level: c.Level, Shape: c.Shape, Angle: c.Angle}, true)
		}
	}
	return out
}

// evadeDoorOut is the fallback's plan, pure over the pack and the loc index for a real-pack test.
// pack must be the live overlay (fleePack) on which evadeEscape found nothing. It returns the first
// closed door the doors-open escape route crosses, and near, the tile of that crossing on the
// account's side, where the door is opened from.
func evadeDoorOut(pack *pathfind.Pack, ix *locindex.Index, types locindex.Types, live []locindex.Change, origin pathfind.NavPoint, threatX, threatZ, attempt int, toward pathfind.NavPoint) (door locindex.Match, near pathfind.NavPoint, ok bool) {
	if pack == nil || ix == nil || types == nil {
		return locindex.Match{}, pathfind.NavPoint{}, false
	}
	doors := closedDoorsNear(ix, types, live, origin.X, origin.Z, origin.Level, evadeDoorRadius)
	if len(doors) == 0 {
		return locindex.Match{}, pathfind.NavPoint{}, false
	}
	open := pack.WithWalls(closedDoorEdits(doors))
	dest, found, _ := evadeEscape(open, origin, threatX, threatZ, attempt, toward)
	if !found {
		return locindex.Match{}, pathfind.NavPoint{}, false
	}
	route := open.FindPath(origin, dest)
	if !route.OK {
		return locindex.Match{}, pathfind.NavPoint{}, false
	}
	return firstClosedDoorOn(pack, doors, route)
}

// closedDoorEdits removes the walls of doors, for planning as if they were open.
func closedDoorEdits(doors []locindex.Match) []pathfind.WallEdit {
	edits := make([]pathfind.WallEdit, 0, len(doors))
	for _, m := range doors {
		edits = append(edits, pathfind.WallEdit{X: m.X, Z: m.Z, Level: m.Level, Shape: m.Shape, Angle: m.Angle})
	}
	return edits
}

// firstClosedDoorOn walks route, planned with doors open, and returns the first of doors whose
// crossing pack (the live overlay) refuses, with near, the tile it is crossed from. False when the
// first refused step is not one of doors, or no step is refused.
func firstClosedDoorOn(pack *pathfind.Pack, doors []locindex.Match, route pathfind.PathResult) (door locindex.Match, near pathfind.NavPoint, ok bool) {
	for i := 1; i < len(route.Waypoints); i++ {
		cur, end := route.Waypoints[i-1], route.Waypoints[i]
		sx, sz := signum(end.X-cur.X), signum(end.Z-cur.Z)
		dir := stepDir(sx, sz)
		if dir < 0 {
			continue
		}
		for cur.X != end.X || cur.Z != end.Z {
			nx, nz := cur.X+sx, cur.Z+sz
			if pack.ExitMask(cur.X, cur.Z, cur.Level)&(1<<uint(dir)) == 0 {
				// The first step the live pack refuses: it must be one of the doors opened
				// for planning, crossed from cur. Anything else is not this fallback's to fix.
				for _, m := range doors {
					if doorCrossing(m, cur.X, cur.Z, nx, nz) {
						return m, cur, true
					}
				}
				return locindex.Match{}, pathfind.NavPoint{}, false
			}
			cur.X, cur.Z = nx, nz
		}
	}
	return locindex.Match{}, pathfind.NavPoint{}, false
}

// doorCrossing reports whether stepping from ax,az to bx,bz crosses door m's wall: a straight
// wall stands on the side of its own tile that its angle names (0 west, 1 north, 2 east, 3 south),
// a corner wall on that side and the one clockwise of it.
func doorCrossing(m locindex.Match, ax, az, bx, bz int) bool {
	sides := []int{m.Angle & 3}
	if m.Shape == 2 {
		sides = append(sides, (m.Angle+1)&3)
	}
	for _, side := range sides {
		wx, wz := m.X+wallSide[side][0], m.Z+wallSide[side][1]
		if ax == m.X && az == m.Z && bx == wx && bz == wz || bx == m.X && bz == m.Z && ax == wx && az == wz {
			return true
		}
	}
	return false
}

// wallSide maps a wall angle to the neighbour it separates its tile from.
var wallSide = [4][2]int{{-1, 0}, {0, 1}, {1, 0}, {0, -1}}

// stepDir is pathfind's direction index (0 north, 1 east, 2 south, 3 west, then the diagonals
// NE, SE, SW, NW) for a unit step, or -1.
func stepDir(sx, sz int) int {
	for n, v := range [8][2]int{{0, 1}, {1, 0}, {0, -1}, {-1, 0}, {1, 1}, {1, -1}, {-1, -1}, {-1, 1}} {
		if v[0] == sx && v[1] == sz {
			return n
		}
	}
	return -1
}

func signum(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// openEvadeDoor walks to near on pack (the live overlay) and opens door from there, reporting
// whether the door's tile changed afterwards (content's open_door deletes the closed door there).
func openEvadeDoor(s actions.Session, pack *pathfind.Pack, door locindex.Match, near pathfind.NavPoint, log func(string)) bool {
	x, z, level, ok := s.State().Tile()
	if !ok || level != near.Level {
		return false
	}
	if log != nil {
		log(fmt.Sprintf("no way through with the doors as they are — opening the %s at %d,%d from %d,%d", door.Name, door.X, door.Z, near.X, near.Z))
	}
	if x != near.X || z != near.Z {
		res := pack.FindPath(pathfind.NavPoint{X: x, Z: z, Level: level}, near)
		if !res.OK {
			return false
		}
		if err := actions.Walk(s, actions.SendableWaypoints(res.Waypoints), true); err != nil {
			return false
		}
		budget := time.Duration(len(res.Waypoints)+3) * 700 * time.Millisecond
		if budget > walkToMaxWait {
			budget = walkToMaxWait
		}
		deadline := time.Now().Add(budget)
		for {
			if nx, nz, _, ok := s.State().Tile(); ok && nx == near.X && nz == near.Z {
				break
			}
			if !time.Now().Before(deadline) {
				return false
			}
			time.Sleep(evadeDoorPoll)
		}
	}
	before := doorTileState(s, door)
	if err := actions.OpLoc(s, door.Op, door.X, door.Z, door.ID); err != nil {
		return false
	}
	deadline := time.Now().Add(evadeDoorOpenWait)
	for time.Now().Before(deadline) {
		time.Sleep(evadeDoorPoll)
		if doorTileState(s, door) != before {
			return true
		}
	}
	return false
}

// evadeDoorPoll/evadeDoorOpenWait: how often openEvadeDoor looks, and how long it waits for the
// door to change after the click (two game ticks and some).
const (
	evadeDoorPoll     = 150 * time.Millisecond
	evadeDoorOpenWait = 2500 * time.Millisecond
)

// doorTileState describes the live change at the door's tile, "" when there is none.
func doorTileState(s actions.Session, door locindex.Match) string {
	for _, c := range s.State().LocChanges() {
		if c.X == door.X && c.Z == door.Z && c.Level == door.Level && (c.Shape == 0 || c.Shape == 2) {
			return fmt.Sprintf("%d/%d/%v", c.Type, c.Rotate, c.Deleted)
		}
	}
	return ""
}

// evadeThroughDoor is HandleEvade's fallback when evadeEscape found nothing on live (fleePack):
// plan through the closed doors near the account and open the first one on the way out. It
// reports whether a door was opened. It needs the registered loc index and loc configs, as
// fleePack does, and does nothing without them.
func evadeThroughDoor(s actions.Session, live *pathfind.Pack, origin pathfind.NavPoint, ev *Event, attempt int, log func(string)) bool {
	ix := currentLocIndex()
	types, ok := currentResolver().(locindex.Types)
	if live == nil || ix == nil || !ok {
		return false
	}
	changes := locindex.ChangesFrom(s.State().LocChanges())
	door, near, found := evadeDoorOut(live, ix, types, changes, origin, ev.ThreatX, ev.ThreatZ, attempt, safeRefFor(s))
	if !found {
		return false
	}
	return openEvadeDoor(s, live, door, near, log)
}

// approachThroughDoor is approachDialogNPC's fallback when no interaction tile of target has a
// route on nav (the live overlay): plan to one with the closed doors near the account opened,
// and open the first door that route crosses. It reports whether a door was opened; the
// guardian's next attempt then approaches through it.
func approachThroughDoor(s actions.Session, nav *pathfind.Pack, here, target pathfind.NavPoint, log func(string)) bool {
	ix := currentLocIndex()
	types, ok := currentResolver().(locindex.Types)
	if nav == nil || ix == nil || !ok {
		return false
	}
	doors := closedDoorsNear(ix, types, locindex.ChangesFrom(s.State().LocChanges()), here.X, here.Z, here.Level, evadeDoorRadius)
	if len(doors) == 0 {
		return false
	}
	open := nav.WithWalls(closedDoorEdits(doors))
	for _, goal := range open.InteractTiles(target) {
		route := open.FindPath(here, goal)
		if !route.OK {
			continue
		}
		door, near, found := firstClosedDoorOn(nav, doors, route)
		if !found {
			return false
		}
		return openEvadeDoor(s, nav, door, near, log)
	}
	return false
}
