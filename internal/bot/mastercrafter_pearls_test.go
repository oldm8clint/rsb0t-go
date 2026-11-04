package bot

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// The pearl round asks the mule for EVERY oyster pearl and EVERY Bolts it publishes (owner, 3 Oct
// 2026), in one bundled kit, keeping the chisel it carries; a mule that answers "none" is a round
// that found nothing (a pause), not a passing failure.
func TestPearlRoundAsksForEveryPearlAndEveryBolt(t *testing.T) {
	r := newSimRig(t, []actionsim.Stack{{ID: gemplan.ChiselID, Count: 1}}, nil,
		map[string]int{"Oyster pearl": 100500, "Oyster pearls": 12, "Bolts": 603000, "Chisel": 1})
	var asked []fleetplan.ItemCount
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, items []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		asked = append(asked, items...)
		declined := map[string]string{}
		for _, it := range items {
			declined[it.Item] = "declined: none in the bank"
		}
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: declined}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	out := r.b.pearlRound(r.ctx, r.b.deps(r.ctx), time.Now())
	want := []fleetplan.ItemCount{{Item: pearlplan.Pearls, Count: 12}, {Item: pearlplan.Pearl, Count: 100500}, {Item: pearlplan.Bolt, Count: 603000}}
	if !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked %v, want %v", asked, want)
	}
	if out.Mode != mastercraftplan.ModePearls || out.Transient || out.Produced != 0 || out.HandoverTried {
		t.Fatalf("%+v", out)
	}
	if !r.logged("pearls: asked mule") {
		t.Fatalf("the ask is not logged: %v", r.logs)
	}
	// The chisel stayed in the pack through the first visit's deposit.
	if simCount(r.sim.Pack(), gemplan.ChiselID) != 1 {
		t.Fatalf("pack %v", r.sim.Pack())
	}
	r.b.afterRound(r.ctx, out, time.Second)
	if r.b.ledger.PearlPauseUntil.IsZero() || r.b.ledger.PearlRounds != 1 {
		t.Fatalf("an empty mule pauses the pearl rounds: %+v", r.b.ledger)
	}
}

// decideLive reads the pearl round's items in this account's own bank: pearls an earlier round left
// make the next decision resume it.
func TestOwnPearlReadsTheBank(t *testing.T) {
	r := newSimRig(t, nil, []actionsim.Stack{{ID: pearlplan.PearlID, Count: 270}, {ID: pearlplan.TipsID, Count: 6000}, {ID: pearlplan.BoltID, Count: 9}}, nil)
	got := r.b.ownPearl(r.ctx)
	if got != (pearlplan.Held{Pearl: 270, Tips: 6000, Bolts: 9}) {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(pearlplan.Ready(100000, 20000, 60, true, pearlplan.Held{}, got, true).Why, "270") {
		t.Fatal("own pearls do not resume the round")
	}
}
