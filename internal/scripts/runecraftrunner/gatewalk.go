package runecraftrunner

// Where the gate's two players stand, and the clicks that put them there (live 18 Oct 2026, see runecraftplan/gatestall.go).
//
// THE CRAFTER'S STAND CLICK was one waypoint, the stand itself. The engine walks a click's waypoints as a greedy chain (pathfind.
// EngineStep: diagonal, then along x, then along z; a refused step leaves the player where he is and the waypoint queued for good), so
// a stand with a pillar between it and where he stands is a click that parks him against the pillar and does nothing again, however
// often it is repeated. PlanStandWalk asks the collision pack what that one waypoint would do and, where it would not arrive, sends the
// pack's route instead (a few corner waypoints in the SAME single click, so no tick is spent on it).
//
// THE CARRIER'S APPROACH. A trade request is an op on a player: it runs the tick it is read only from a tile orthogonally beside the
// crafter. From anywhere else the engine walks the asker along a naive waypoint on the asker's side of him and the op runs on arrival;
// where that waypoint is a solid tile the asker stops short and is never told. ApproachTile names the tiles beside the crafter that
// the carrier can walk to by the pack's route, nearest first, and TradeWithCrafter walks to one when DecideApproach says the tile it
// stands on is a dead end.

import (
	"sort"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// StandWalkKind is how the stand click is made.
type StandWalkKind string

// The kinds.
const (
	// StandWalkNone: already on the stand.
	StandWalkNone StandWalkKind = ""
	// StandWalkLone: one waypoint, the stand; the engine's greedy steps arrive.
	StandWalkLone StandWalkKind = "lone"
	// StandWalkRouted: the lone click would park him short of the stand, so the click carries the pack's route.
	StandWalkRouted StandWalkKind = "routed"
	// StandWalkBlind: no pack (or no route in it): the lone click is all there is, whatever it does.
	StandWalkBlind StandWalkKind = "blind"
)

// StandWalk is the click that walks the crafter onto his stand.
type StandWalk struct {
	Kind      StandWalkKind
	Waypoints []pathfind.NavPoint
	// Parks is where a lone click would leave him, when it would not arrive (Kind routed or blind).
	Parks tile.Tile
}

func navPoint(t tile.Tile) pathfind.NavPoint {
	return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level}
}

// standWalkSteps bounds the greedy replay of a stand click: far more than any tile of the gate needs.
const standWalkSteps = 64

// PlanStandWalk chooses the stand click from here. With a pack the lone waypoint is replayed by the engine's own rule first; one that
// arrives is sent as it was (the cheapest click, and what the gate has always done from the exit tile), one that parks is replaced by
// the pack's route if that arrives.
func PlanStandWalk(pk *pathfind.Pack, here, stand tile.Tile) StandWalk {
	if here == stand {
		return StandWalk{Kind: StandWalkNone}
	}
	lone := []pathfind.NavPoint{navPoint(stand)}
	if pk == nil || here.Level != stand.Level {
		return StandWalk{Kind: StandWalkBlind, Waypoints: lone}
	}
	end, arrived := pk.EngineWalk(navPoint(here), lone, standWalkSteps)
	if arrived {
		return StandWalk{Kind: StandWalkLone, Waypoints: lone}
	}
	parks := tile.Tile{X: end.X, Z: end.Z, Level: end.Level}
	route := pk.FindPath(navPoint(here), navPoint(stand))
	if route.OK && len(route.Waypoints) > 0 {
		if last, ok := pk.EngineWalk(navPoint(here), route.Waypoints, standWalkSteps); ok && last == navPoint(stand) {
			return StandWalk{Kind: StandWalkRouted, Waypoints: route.Waypoints, Parks: parks}
		}
	}
	return StandWalk{Kind: StandWalkBlind, Waypoints: lone, Parks: parks}
}

// ClickStandWalk plans the stand click from here and sends it as one MOVE_GAMECLICK (running). It returns the plan so the caller can
// say what it did.
func ClickStandWalk(d *fleetrunner.Deps, here, stand tile.Tile) (StandWalk, error) {
	plan := PlanStandWalk(d.Pack, here, stand)
	if plan.Kind == StandWalkNone {
		return plan, nil
	}
	return plan, actions.Walk(d.Session, plan.Waypoints, true)
}

// ---- the carrier's approach ----

// approachMaxSteps bounds the walk a carrier makes to another side of the crafter: it is a few tiles round a pillar.
const approachMaxSteps = 16

// ApproachSpot is a tile beside the crafter that the carrier can trade from and the route to it.
type ApproachSpot struct {
	Tile      tile.Tile
	Waypoints []pathfind.NavPoint
	Steps     int
}

// ApproachTile picks the tile beside the crafter to trade from: orthogonally next to him with no wall between (pathfind.InteractTiles),
// reachable from here by the pack's route within approachMaxSteps, the fewest steps first and, among equals, the one nearest prefer
// (the gate's wait tile). ok is false when no such tile exists or the carrier is already on one.
func ApproachTile(pk *pathfind.Pack, here, crafter, prefer tile.Tile) (ApproachSpot, bool) {
	if pk == nil || here.Level != crafter.Level {
		return ApproachSpot{}, false
	}
	var best ApproachSpot
	found := false
	for _, at := range pk.InteractTiles(navPoint(crafter)) {
		cand := tile.Tile{X: at.X, Z: at.Z, Level: at.Level}
		if cand == crafter {
			continue // a trade is not run from his own tile
		}
		if cand == here {
			return ApproachSpot{}, false
		}
		route := pk.FindPath(navPoint(here), at)
		if !route.OK || len(route.Waypoints) == 0 || route.Waypoints[len(route.Waypoints)-1] != at {
			continue
		}
		steps := route.Steps()
		if steps > approachMaxSteps {
			continue
		}
		if end, ok := pk.EngineWalk(navPoint(here), route.Waypoints, approachMaxSteps+8); !ok || end != at {
			continue
		}
		c := ApproachSpot{Tile: cand, Waypoints: route.Waypoints, Steps: steps}
		if !found || c.Steps < best.Steps || c.Steps == best.Steps && tile.Chebyshev(c.Tile, prefer) < tile.Chebyshev(best.Tile, prefer) {
			best, found = c, true
		}
	}
	return best, found
}

// ApproachTiles lists every tile beside the crafter a carrier could trade from, for a log line.
func ApproachTiles(pk *pathfind.Pack, crafter tile.Tile) []tile.Tile {
	if pk == nil {
		return nil
	}
	var out []tile.Tile
	for _, at := range pk.InteractTiles(navPoint(crafter)) {
		if t := (tile.Tile{X: at.X, Z: at.Z, Level: at.Level}); t != crafter {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	return out
}

// WiggleTile is a tile for the crafter to step to and straight back from his stand, which clears whatever the engine still holds of
// him (a queued interaction, a half-finished walk): one of the tiles beside the stand that a carrier could trade from, the nearest
// prefer. ok is false without a pack or when nothing is beside the stand.
func WiggleTile(pk *pathfind.Pack, stand, prefer tile.Tile) (tile.Tile, bool) {
	var best tile.Tile
	found := false
	for _, t := range ApproachTiles(pk, stand) {
		if !found || tile.Chebyshev(t, prefer) < tile.Chebyshev(best, prefer) {
			best, found = t, true
		}
	}
	return best, found
}

// approachWait is how long a carrier waits for its walk to a new tile before it asks again regardless.
const approachWait = 8 * time.Second

// walkToApproach sends the carrier's route and waits for it to arrive (or for a screen, a stop or approachWait).
func walkToApproach(d *fleetrunner.Deps, a ApproachSpot, trade *actions.Trade) bool {
	if err := actions.Walk(d.Session, a.Waypoints, true); err != nil {
		return false
	}
	deadline := time.Now().Add(approachWait)
	for time.Now().Before(deadline) {
		if Here(d) == a.Tile {
			return true
		}
		if trade.Active() || !d.Delay(gatePoll) {
			return false
		}
	}
	return Here(d) == a.Tile
}
