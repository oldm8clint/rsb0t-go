package fleetrunner

import (
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/levels"
)

// A walk under the staller's hitpoints line with food carried eats one bite so the stalling can go on (live 2 Nov 2026: 19 walks
// stood aside at 50-64% and the walk's eater, whose line is lower, never bit); over the line, or with nothing to eat, it eats nothing.
func TestTheStallerEatsABiteToClearItsHitpointsGate(t *testing.T) {
	cfg := travelTestConfig(t)
	lobster := objOf(t, cfg, "Lobster", false)
	bites := func(hp, food int) int {
		sim := actionsim.New(actionsim.Objs{}, nil)
		setStat(sim, fleetplan.HitpointsStat, levels.XPAtLevel(55), hp)
		if food > 0 {
			sim.Give(lobster, food)
		}
		d := quickDeps(sim, cfg)
		d.Log = func(string) {}
		(&Travel{Rub: true}).stallTopUp(d)
		return len(sentOps(sim, clientprot.OPHELD1))
	}
	if n := bites(33, 3); n != 1 {
		t.Errorf("33/55 with three Lobsters: %d bites, want 1", n)
	}
	if n := bites(40, 3); n != 0 {
		t.Errorf("40/55 (over 65%%): %d bites, want 0", n)
	}
	if n := bites(33, 0); n != 0 {
		t.Errorf("33/55 with no food: %d bites, want 0", n)
	}
}
