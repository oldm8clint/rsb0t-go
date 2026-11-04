package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/lootchestfisherplan"
	"rsb0t-go/internal/webwalk"
)

// planFor plans a walk the way a combat-72 account's WalkTo does (danger zones on, hostiles' territory
// avoided), from a point to a point.
func planFor(p *webwalk.Planner, from, to webwalk.Point, combat int) webwalk.Route {
	zones := webwalk.ResolveDangerZones(nil, muleWolfRects(), webwalk.ZoneContext{IncludeAutomatic: true, CombatLevel: combat, CombatKnown: true, Start: &from, Dest: &to})
	state := &webwalk.WorldState{FreeSlots: 12, ItemCount: func(name string) int {
		if name == "Coins" {
			return 400
		}
		return 0
	}}
	return p.Plan(from, to, webwalk.Options{AvoidZones: zones, AllowNPC: true, AllowSpecial: true, State: state})
}

// THE FACTS the Games Room route rests on, planned over the real collision pack the way a combat-72 account's
// walk is (danger zones on, White Wolf Mountain's dens avoided): the chest is 141 route cost from Burthorpe, 159
// from the Falador west bank, 144 from the Falador Teleport's landing and 793 from the Catherby post. The route
// is the whole gain from the post and saves 18 tiles from the bank, which the two flights of stairs and a
// charge eat (craftChestGamesRoute "post" keeps only the first).
func TestGamesRoomRouteFactsOnTheRealPack(t *testing.T) {
	_, p := muleRealPack(t)
	chest := webwalk.Point{X: lootchestfisherplan.ChestStand.X, Z: lootchestfisherplan.ChestStand.Z}
	cost := map[string]int{}
	for name, from := range map[string]webwalk.Point{
		"burthorpe": {X: 2899, Z: 3564},
		"bank":      {X: lootchestfisherplan.FaladorBankStand.X, Z: lootchestfisherplan.FaladorBankStand.Z},
		"landing":   {X: 2965, Z: 3378},
		"post":      {X: 2809, Z: 3441},
	} {
		r := planFor(p, from, chest, 72)
		if !r.OK {
			t.Errorf("%s -> the chest: no route (%s)", name, r.Reason)
			continue
		}
		cost[name] = r.Cost
		t.Logf("%s -> the chest: cost %d, %d hops", name, r.Cost, len(r.Hops()))
	}
	if cost["burthorpe"] == 0 || cost["burthorpe"] >= cost["bank"] || cost["post"] < 5*cost["burthorpe"] {
		t.Errorf("the route facts moved: %v", cost)
	}
}
