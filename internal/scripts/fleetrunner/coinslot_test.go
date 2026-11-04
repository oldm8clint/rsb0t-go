package fleetrunner

import (
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/fleetplan"
)

// The walk kit draws one coin when the pack holds none and the bank some, so a full pack still has a stack for the casts' coins
// (live 2 Nov 2026: 35 accounts paused walk alch on a full pack with no coins); a pack that holds coins draws none.
func TestDrawAlchKitDrawsOneCoinForTheCastsCoins(t *testing.T) {
	cfg := travelTestConfig(t)
	nature := objOf(t, cfg, "Nature rune", false)
	run := func(packCoins int) (pack, bank int) {
		var start []actionsim.Stack
		if packCoins > 0 {
			start = []actionsim.Stack{{ID: coinsID, Count: packCoins}}
		}
		sim := actionsim.New(certObjs(cfg), start)
		sim.OpenBank([]actionsim.Stack{{ID: nature, Count: 500}, {ID: coinsID, Count: 1000}})
		setStat(sim, fleetplan.MagicStat, 200000, 60)
		d := fastDeps(sim, cfg)
		tr := &Travel{Alch: fleetplan.WalkAlchSettings{Enabled: true, Kit: 100, Carry: 100}.Normalised()}
		tr.DrawAlchKit(d, actions.NewBank(sim, d.Components), testWaiter{d})
		return held(sim.Pack(), coinsID), held(sim.Bank(), coinsID)
	}
	if pack, bank := run(0); pack != 1 || bank != 999 {
		t.Errorf("no coins carried: pack %d bank %d, want 1 and 999", pack, bank)
	}
	if pack, bank := run(50); pack != 50 || bank != 1000 {
		t.Errorf("50 coins carried: pack %d bank %d, want 50 and 1000", pack, bank)
	}
}
