package fleetrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/levels"
)

// The hurt step cost reaches every pen's hunters, not only the pen the walk starts in: the Feldip herd, the Yanille west ogres and the
// Ice warriors' dungeon all cost HurtHeldTileCost a tile, open ground nothing.
func TestHurtStepCostCoversEveryPensHunters(t *testing.T) {
	cost := hurtStepCost()
	for _, c := range []struct {
		name    string
		x, z, l int
		want    int
	}{
		{"the Feldip ogre spawn at 2574,2988 (where the 18 Oct death was)", 2574, 2988, 0, HurtHeldTileCost},
		{"a Feldip tile in the dead account's walk, 2576,2987", 2576, 2987, 0, HurtHeldTileCost},
		{"the Yanille west ogres at 2500,3092", 2500, 3092, 0, HurtHeldTileCost},
		{"the Ice warriors' dungeon at 3044,9581", 3044, 9581, 0, HurtHeldTileCost},
		{"the East Ardougne bank", 2655, 3283, 0, 0},
		{"the Feldip spawn, upstairs", 2574, 2988, 1, 0},
		{"Lumbridge", 3222, 3218, 0, 0},
	} {
		if got := cost(c.x, c.z, c.l); got != c.want {
			t.Errorf("%s: step cost %d, want %d", c.name, got, c.want)
		}
	}
	if HurtHeldTileCost <= HeldTileCost {
		t.Errorf("a hurt walk's held tile (%d) costs no more than a healthy pen walk's (%d)", HurtHeldTileCost, HeldTileCost)
	}
}

// walkHurt is hitpoints under the line AND an empty pack: it reads the session, hitpoints first.
func TestWalkHurtNeedsLowHitpointsAndAnEmptyPack(t *testing.T) {
	cfg := travelTestConfig(t)
	lobster := objOf(t, cfg, "Lobster", false)
	mk := func(hp int, food int) *Deps {
		sim := actionsim.New(actionsim.Objs{}, nil)
		setStat(sim, fleetplan.HitpointsStat, levels.XPAtLevel(55), hp)
		if food > 0 {
			sim.Give(lobster, food)
		}
		return quickDeps(sim, cfg)
	}
	if !walkHurt(mk(18, 0)) {
		t.Error("18/55 with an empty pack is hurt")
	}
	if walkHurt(mk(18, 2)) {
		t.Error("18/55 with two Lobsters is not: it can eat")
	}
	if walkHurt(mk(40, 0)) {
		t.Error("40/55 with an empty pack is over the line")
	}
	if walkHurt(nil) || walkHurt(&Deps{}) {
		t.Error("no session: not hurt")
	}
}

// THE STALLER, 18 Oct 15:34:56 and 15:37:05: "standing aside at 18/55 hitpoints so a bite can land" on an account with nothing to eat. It
// rubs now (the gate has no bite to make room for) and says nothing of a bite; with food in the pack the gate and its line are unchanged.
func TestTheRunStallerNoLongerStandsAsideForABiteThatCannotLand(t *testing.T) {
	cfg := travelTestConfig(t)
	games := objOf(t, cfg, "Games necklace(8)", false)
	lobster := objOf(t, cfg, "Lobster", false)
	run := func(food int) (rubs int, lines string) {
		sim := actionsim.New(actionsim.Objs{}, []actionsim.Stack{{ID: games, Count: 1}})
		setStat(sim, fleetplan.HitpointsStat, levels.XPAtLevel(55), 18)
		if food > 0 {
			sim.Give(lobster, food)
		}
		d := quickDeps(sim, cfg)
		var out []string
		d.Log = func(s string) { out = append(out, s) }
		tr := &Travel{Rub: true}
		tr.RubPoll(d, time.Now(), 2*time.Second, 20, true)
		return len(sentOps(sim, clientprot.OPHELD4)), strings.Join(out, "\n")
	}
	if rubs, lines := run(0); rubs != 1 || strings.Contains(lines, "standing aside") {
		t.Errorf("18/55, no food: %d rubs (want 1) and the log %q (want nothing about a bite)", rubs, lines)
	}
	if rubs, lines := run(3); rubs != 0 || !strings.Contains(lines, "standing aside at 18/55 hitpoints so a bite can land") {
		t.Errorf("18/55, three Lobsters: %d rubs (want 0) and the log %q (want the old line)", rubs, lines)
	}
}
