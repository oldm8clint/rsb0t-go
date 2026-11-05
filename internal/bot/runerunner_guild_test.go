package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runerunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
)

func (r *rotRig) setRanged(level int) {
	mindHoldStat(r.sim.State(), combatrunner.RangedStat, levels.XPAtLevel(level), level)
	r.b.noteRanged(r.ctx)
}

// The Ranging Guild's shop (runerunnerplan/rangingguild.go): left out of the ring below Ranged 40 with the reason logged, placed after
// each Hickton stop from 40, and bought from like any ammo shelf.
func TestDargaudIsPlacedFromRanged40(t *testing.T) {
	set := map[string]interface{}{"ammoShops": []interface{}{"Gerrant", "Lowe", "Hickton"}, "ammoEveryWorld": true}
	r := ammoRig(t, "guildrig1", 260, set)
	r.setRanged(1)
	r.b.newRound(r.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r.b.deps(r.ctx))), false)
	if ring := runerunnerplan.PlanString(r.b.st.Ring, 0); strings.Contains(ring, "Dargaud") {
		t.Fatalf("Ranged 1 placed the guild shop: %s", ring)
	}
	if !strings.Contains(r.log(), "the Ranging Guild shop is left out of the round: the Ranging Guild door needs Ranged 40") {
		t.Errorf("no reason line\n%s", r.log())
	}

	r2 := ammoRig(t, "guildrig2", 270, set)
	r2.setRanged(40)
	r2.b.newRound(r2.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r2.b.deps(r2.ctx))), false)
	ring := runerunnerplan.PlanString(r2.b.st.Ring, 0)
	if n := strings.Count(ring, "Dargaud@"); n != 3 || !strings.Contains(ring, "Hickton@270 Dargaud@270") {
		t.Fatalf("Ranged 40 ring: %s", ring)
	}

	// The station buys the guild's shelf: shafts and tips, at the shop's prices, on its own stand.
	r2.atKey(t, "Dargaud@270")
	r2.ctx.CurrentWorld = 270
	r2.pack(r2.rich(), 24, nil)
	var stand = runerunnerplan.AmmoSite{}
	r2.b.ammoBuySeam = func(site runerunnerplan.AmmoSite, planFor func(map[string]int, int) fleetplan.PurchasePlan) runerunner.AmmoVisit {
		stand = site
		seen := map[string]int{}
		for item, n := range map[string]int{"Arrow shaft": 1000, "Steel arrowtips": 300, "Mithril arrowtips": 200, "Adamant arrowtips": 200, "Rune arrowtips": 150} {
			if site.Shop.Sells(item) {
				seen[item] = n
			}
		}
		plan := planFor(seen, r2.rich())
		v := runerunner.AmmoVisit{Walked: true, Plan: plan, ShopVisit: fleetrunner.ShopVisit{Seen: seen, Bought: map[string]int{}}}
		for _, l := range plan.Lines {
			v.Bought[l.Rune] = l.Count
			v.Spent += site.Shop.CostToBuy(l.Rune, seen[l.Rune], l.Count)
		}
		return v
	}
	got := r2.runAmmo()
	if got.Outcome != runerunnerplan.OutcomeBought || stand.Key != runerunnerplan.SiteDargaud || got.Bought["Steel arrowtips"] == 0 {
		t.Fatalf("guild visit: %+v\n%s", got, r2.log())
	}

	// A ring planned at 40 and walked after a death or a stat drain below it skips the stop.
	r2.setRanged(30)
	r2.atKey(t, "Dargaud@270")
	delete(r2.b.st.Done, "Dargaud@270")
	if res := r2.runAmmo(); res.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(res.Reason, "needs Ranged 40") {
		t.Errorf("under 40: %+v", res)
	}
}

// While Ranged is under 40 the ScarabSwarm phase runs the Ranged stint (and does not once it is 40).
func TestSwarmPhaseTrainsRangedTo40(t *testing.T) {
	r := newRotRig(t, "guildrig3", 280, 2809, 3441, map[string]interface{}{"swarmSettings": map[string]interface{}{"maxStage": 5.0}})
	fake := &runeFakeSwarm{}
	r.b.newSwarm = func() masterSwarm { return fake }
	r.setRanged(12)
	if err := r.b.startSwarm(r.ctx); err != nil {
		t.Fatal(err)
	}
	if fake.settings["endgameRangedBow"] != true || fake.settings["endgameTrainRanged"] != true || fake.settings["maxStage"] != 5.0 {
		t.Errorf("settings %v", fake.settings)
	}
	r.b.stopSwarm(r.ctx)
	r.setRanged(41)
	if err := r.b.startSwarm(r.ctx); err != nil {
		t.Fatal(err)
	}
	if _, on := fake.settings["endgameRangedBow"]; on {
		t.Errorf("trained at 41: %v", fake.settings)
	}
}
