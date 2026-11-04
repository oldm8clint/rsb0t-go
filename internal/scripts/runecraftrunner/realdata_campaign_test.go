package runecraftrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/essenceplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The Nature campaign's buy-out (runecraft P4), on the real pack and the real item config. It spends the mule's whole coin stack at the
// Seers stalls, so its trip is the post to the stall and back with the essence, and its goods are a note stack and a coin stack.

// The trip between the mule's post at Catherby and the Seers stalls' stand is a short road on the walker's own graph and collision pack,
// there and back, on foot (no ship, no jungle): the design priced it at about 100 tiles each way.
func TestTheBuyOutTripPlansOnTheRealPack(t *testing.T) {
	r := loadReal(t)
	g, err := webwalk.Default()
	if err != nil {
		t.Fatal(err)
	}
	planner := webwalk.NewPlanner(r.pk, g)
	plan := func(from, to tile.Tile) webwalk.Route {
		return planner.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: to.X, Z: to.Z, Level: to.Level}, webwalk.Options{})
	}
	post := tile.Tile{X: 2809, Z: 3441}
	stand := questplan.SeersMarketStand
	for _, c := range []struct {
		name     string
		from, to tile.Tile
	}{
		{"the Catherby post to the Seers stalls", post, stand},
		{"the Seers stalls to the Catherby post", stand, post},
	} {
		route := plan(c.from, c.to)
		if !route.OK {
			t.Errorf("%s: no route (%s)", c.name, route.Reason)
			continue
		}
		if route.Cost < 60 || route.Cost > 250 {
			t.Errorf("%s: cost %d, want a walk of about a hundred", c.name, route.Cost)
		}
		t.Logf("%s: cost %d", c.name, route.Cost)
	}
	// The market is on world 1's side of the map: the stalls stand on the mainland, level 0.
	if stand.Level != 0 || stand.X < 2600 || stand.X > 2900 {
		t.Errorf("the stalls' stand is %v", stand)
	}
}

// What the buy-out carries is a few stacks: the essence the stalls hand over is a bank note that stacks (a hundred thousand is one
// slot, and the mule's trade takes it in one Offer-All), the coins stack, and the Natures stack. An essence that did not stack when noted
// would be a hundred thousand slots of nothing.
func TestTheBuyOutsGoodsAreStacksOnTheRealConfig(t *testing.T) {
	r := loadReal(t)
	plain, ok := r.cfg.Obj(runecraftplan.EssenceID)
	if !ok || plain.Name != runecraftplan.EssenceName || plain.Stackable || plain.Noted() {
		t.Fatalf("Rune essence %d: %+v", runecraftplan.EssenceID, plain)
	}
	noted, ok := r.cfg.Obj(runecraftplan.NotedEssenceID)
	if !ok || !noted.Noted() || !noted.Stackable || noted.CertLink != runecraftplan.EssenceID || noted.Name != runecraftplan.EssenceName {
		t.Fatalf("the note of Rune essence %d: %+v", runecraftplan.NotedEssenceID, noted)
	}
	for _, id := range []int{CoinsID, int(NatureID)} {
		o, ok := r.cfg.Obj(id)
		if !ok || !o.Stackable {
			t.Errorf("object %d (%+v) must stack: the buy-out's coins and the campaign's Natures are one slot each", id, o)
		}
	}
	// The stalls' price ceiling and the lot are the plan's: a lot at the ceiling is under the Seers Market's trade limit.
	if essenceplan.BuyOutLot*essenceplan.MarketUnitCeiling > 5_000_000 {
		t.Errorf("a lot of %d at %d is over a stall's limit", essenceplan.BuyOutLot, essenceplan.MarketUnitCeiling)
	}
}
