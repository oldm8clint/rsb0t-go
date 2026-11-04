package runecraftrunner

// The gate stall of 18 Oct 2026 on the real collision pack (RSB0T_COLLISION_PACK; without it these skip).

import (
	"os"
	"sort"
	"testing"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

func loadRealPack(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("set RSB0T_COLLISION_PACK")
	}
	pk, err := pathfind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return pk
}

// the Ruined Pillar between the incident's tile and the stand, found by findloc (loc 2506, shape 10, at 2865,3020).
var gatePillar = tile.Tile{X: 2865, Z: 3020}

// gateTiles is every walkable tile at the gate (runecraftplan.NearGate), in order.
func gateTiles(pk *pathfind.Pack) []tile.Tile {
	var out []tile.Tile
	ex := runecraftplan.Nature.Exit
	for x := ex.X - runecraftplan.GateNear; x <= ex.X+runecraftplan.GateNear; x++ {
		for z := ex.Z - runecraftplan.GateNear; z <= ex.Z+runecraftplan.GateNear; z++ {
			if t := (tile.Tile{X: x, Z: z}); pk.Walkable(x, z, 0) && runecraftplan.NearGate(t) {
				out = append(out, t)
			}
		}
	}
	return out
}

func tileSet(ts []tile.Tile) map[tile.Tile]bool {
	m := map[tile.Tile]bool{}
	for _, t := range ts {
		m[t] = true
	}
	return m
}

// THE CRAFTER'S SIDE. One waypoint on the stand parks the crafter against the pillar from every tile on its far side, including the
// incident's tile (2864,3020) and the line the event monster's flee ended on (z 3022, x 2859 to 2862); the click the gate sends now
// arrives from every tile at the gate, and stays the plain click wherever that one arrives (the exit tile included: no cycle is lost).
func TestTheStandClickParksWestOfThePillarAndTheRoutedClickArrivesEverywhere(t *testing.T) {
	pk := loadRealPack(t)
	stand := runecraftplan.GateStandTile
	if pk.Walkable(gatePillar.X, gatePillar.Z, 0) {
		t.Fatalf("the pillar at %v is walkable in the pack", gatePillar)
	}
	if !pk.Walkable(stand.X, stand.Z, 0) || !pk.Walkable(runecraftplan.GateWaitTile.X, runecraftplan.GateWaitTile.Z, 0) {
		t.Fatal("the stand or the wait tile is not walkable")
	}
	var parked []tile.Tile
	lone, routed := 0, 0
	for _, from := range gateTiles(pk) {
		if from == stand {
			continue
		}
		oldEnd, oldArrived := pk.EngineWalk(navPoint(from), []pathfind.NavPoint{navPoint(stand)}, standWalkSteps)
		plan := PlanStandWalk(pk, from, stand)
		end, arrived := pk.EngineWalk(navPoint(from), plan.Waypoints, standWalkSteps)
		if !arrived || end != navPoint(stand) {
			t.Errorf("from %v the %s click ended at %v (arrived %v)", from, plan.Kind, end, arrived)
		}
		switch {
		case !oldArrived:
			parked = append(parked, from)
			routed++
			if plan.Kind != StandWalkRouted || plan.Parks != (tile.Tile{X: oldEnd.X, Z: oldEnd.Z}) {
				t.Errorf("from %v the lone click parks at %v but the plan is %s parking at %v", from, oldEnd, plan.Kind, plan.Parks)
			}
		default:
			lone++
			if plan.Kind != StandWalkLone || len(plan.Waypoints) != 1 {
				t.Errorf("from %v the lone click arrives but the plan is %s with %d waypoint(s)", from, plan.Kind, len(plan.Waypoints))
			}
		}
	}
	t.Logf("%d gate tiles click straight, %d would be parked by the lone click and are routed", lone, routed)
	set := tileSet(parked)
	for _, want := range []tile.Tile{{X: 2864, Z: 3020}, {X: 2862, Z: 3022}, {X: 2861, Z: 3022}, {X: 2860, Z: 3022}, {X: 2859, Z: 3022}} {
		if !set[want] {
			t.Errorf("%v (the incident) is not a tile the lone click parks from; parked set %v", want, sortedTiles(parked))
		}
	}
	if set[runecraftplan.Nature.Exit] {
		t.Errorf("the lone click parks from the portal's exit tile %v: every cycle would pay for the routed click", runecraftplan.Nature.Exit)
	}
}

func sortedTiles(ts []tile.Tile) []tile.Tile {
	out := append([]tile.Tile(nil), ts...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		return out[i].Z < out[j].Z
	})
	return out
}

// THE CARRIERS' SIDE. With the crafter on the incident tile no carrier east of the pillar can ask him: the engine walks it toward the
// pillar's tile and it stops against it, from the wait tile and from every tile the carriers use. The approach picks a tile on his open
// side that is reachable, and trading from it is an op the engine runs at once.
func TestNoCarrierEastOfThePillarCanAskACrafterWestOfItAndTheApproachFindsATileThatCan(t *testing.T) {
	pk := loadRealPack(t)
	crafter := tile.Tile{X: 2864, Z: 3020}
	wait := runecraftplan.GateWaitTile
	end, reached := pk.NaiveApproach(navPoint(wait), navPoint(crafter), 64)
	if reached {
		t.Fatalf("the wait tile %v reaches a crafter at %v by the engine's own approach (ended %v): the incident does not reproduce", wait, crafter, end)
	}
	if end.X != 2866 || end.Z != 3020 {
		t.Errorf("the carrier from the wait tile ended at %v, want 2866,3020 (against the pillar; the live carriers were logged there)", end)
	}
	var stuck, fine []tile.Tile
	for _, from := range gateTiles(pk) {
		if from == crafter {
			continue
		}
		if _, ok := pk.NaiveApproach(navPoint(from), navPoint(crafter), 64); ok {
			fine = append(fine, from)
		} else {
			stuck = append(stuck, from)
		}
	}
	t.Logf("crafter at %v: %d gate tiles reach him, %d are dead ends", crafter, len(fine), len(stuck))
	if !tileSet(fine)[runecraftplan.Nature.Exit] {
		t.Errorf("the exit tile cannot reach the crafter at %v", crafter)
	}
	for _, x := range []int{2866, 2867, 2868, 2869, 2870, 2871} {
		for _, z := range []int{3019, 3020, 3021, 3022} {
			from := tile.Tile{X: x, Z: z}
			if pk.Walkable(x, z, 0) && tileSet(fine)[from] {
				t.Errorf("%v, east of the pillar, reaches the crafter by the engine's approach", from)
			}
		}
	}
	// The approach: a tile beside him the carrier can walk to and trade from.
	ap, ok := ApproachTile(pk, wait, crafter, wait)
	if !ok {
		t.Fatalf("no approach tile beside %v from %v; the tiles beside him: %v", crafter, wait, ApproachTiles(pk, crafter))
	}
	if !pk.OperableFrom(navPoint(ap.Tile), navPoint(crafter)) {
		t.Errorf("the approach tile %v cannot trade with %v", ap.Tile, crafter)
	}
	if end, ok := pk.EngineWalk(navPoint(wait), ap.Waypoints, 32); !ok || end != navPoint(ap.Tile) {
		t.Errorf("the approach route ended at %v (arrived %v), want %v", end, ok, ap.Tile)
	}
	t.Logf("from the wait tile the approach is %v in %d step(s) by %v", ap.Tile, ap.Steps, ap.Waypoints)
	if ap.Steps > 8 {
		t.Errorf("the approach is %d steps", ap.Steps)
	}
}

// The ordinary gate is untouched: with the crafter on his stand the wait tile trades at once, so a carrier there is never moved. (A
// carrier on the wait tile asking a crafter who has only just come out of the portal is walked toward the Mysterious stone beside the
// exit and stops against it: the first request is held GateStandHold for that reason, and the second finds him on his stand.)
func TestTheOrdinaryGateStillTradesFromTheWaitTile(t *testing.T) {
	pk := loadRealPack(t)
	stand, wait := runecraftplan.GateStandTile, runecraftplan.GateWaitTile
	if !pk.OperableFrom(navPoint(wait), navPoint(stand)) {
		t.Fatalf("the wait tile %v is not beside the stand %v", wait, stand)
	}
	if _, ok := ApproachTile(pk, wait, stand, wait); ok {
		t.Error("a carrier already on a tile that trades was offered another")
	}
	exit := runecraftplan.Nature.Exit
	end, ok := pk.NaiveApproach(navPoint(wait), navPoint(exit), 32)
	t.Logf("a request from the wait tile to a crafter at the exit tile %v: reached %v, the asker ends at %v", exit, ok, end)
}
