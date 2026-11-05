package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// The Vial of water shuttle (runerunnerplan/vialshuttle.go): Aemad's pack arrives full, so the pack is banked before the shelf, and
// the runner goes back to the bank and the shelf until the 500 Vials are gone.
func TestAemadVialShuttleBanksUntilDepleted(t *testing.T) {
	r := ropeRig(t, "vialrig1", 240, map[string]interface{}{
		"ammoItems": []interface{}{"Bronze arrow", "Vial of water"}, "ammoStopAbove": []interface{}{}, "vialTrips": 30.0,
	})
	r.atKey(t, "Aemad@242")
	r.ctx.CurrentWorld = 242
	free := 0 // the pack arrives full of rune stacks
	r.b.ammoPackSeam = func() (int, int, map[string]int) { return r.rich(), free, map[string]int{} }
	banked := 0
	r.b.bankPackSeam = func(at tile.Tile) {
		if at != runerunnerplan.ArdougneBank {
			t.Errorf("banked at %v", at)
		}
		banked++
		free = 27
	}
	stock := map[string]int{"Vial of water": 500, "Bronze arrow": 500}
	visits := 0
	r.b.ammoBuySeam = func(site runerunnerplan.AmmoSite, planFor func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		visits++
		seen := map[string]int{}
		for item, n := range stock {
			if site.Shop.Sells(item) {
				seen[item] = n
			}
		}
		plan := planFor(seen, r.rich())
		v := runerunner.AmmoVisit{Walked: true, Plan: plan, ShopVisit: fleetrunner.ShopVisit{Seen: seen, Bought: map[string]int{}}}
		for _, l := range plan.Lines {
			v.Bought[l.Rune] = l.Count
			stock[l.Rune] -= l.Count
			v.Spent += site.Shop.CostToBuy(l.Rune, seen[l.Rune], l.Count)
		}
		return v
	}
	res := r.runAmmo()
	if res.Outcome != runerunnerplan.OutcomeBought || res.Bought["Vial of water"] < 450 || stock["Vial of water"] >= runerunnerplan.MinVialTrip {
		t.Fatalf("vials bought %d, %d left on the shelf: %+v\n%s", res.Bought["Vial of water"], stock["Vial of water"], res, r.log())
	}
	if banked < 10 || visits < 10 {
		t.Errorf("banked %d times in %d visits: a 500 shelf takes about 20 trips of 26", banked, visits)
	}
	if !strings.Contains(r.log(), "vial shuttle: banking the pack at East Ardougne") || !strings.Contains(r.log(), "vial shuttle: station=Aemad@242 done after") {
		t.Errorf("log lines\n%s", r.log())
	}

	// vialTrips 0 is the old behaviour: one visit, no extra banking.
	r2 := ropeRig(t, "vialrig2", 250, map[string]interface{}{
		"ammoItems": []interface{}{"Bronze arrow", "Vial of water"}, "ammoStopAbove": []interface{}{}, "vialTrips": 0.0,
	})
	r2.atKey(t, "Aemad@252")
	r2.ctx.CurrentWorld = 252
	r2.pack(r2.rich(), 26, nil)
	n := 0
	r2.b.bankPackSeam = func(tile.Tile) { n++ }
	visits2 := 0
	r2.shelf(map[string]int{"Vial of water": 500, "Bronze arrow": 500}, r2.rich(), &visits2, 1)
	if res := r2.runAmmo(); res.Bought["Vial of water"] != 24 || visits2 != 1 || n != 0 {
		t.Errorf("shuttle off: %+v visits %d banked %d\n%s", res, visits2, n, r2.log())
	}
}
