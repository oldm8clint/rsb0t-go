package bot

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/peerbank"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

func publishRuneMule(r *rotRig, held map[string]int) {
	var bank []peerbank.Holding
	for item, n := range held {
		bank = append(bank, peerbank.Holding{Item: item, Count: n})
	}
	r.ctx.Peers.Publish("mule", tile.Tile{X: 2809, Z: 3441}, true, bank, nil, false)
}

// The mule on 7 October 2026 07:15 (5.6M coins, 395,000 Water, 109 Nature, no Fire, Chaos or
// Death): the runner buys what it is out of first and up to the price ceiling, and takes its own
// priority and caps back when the mule has been restocked.
func TestRuneRunnerBuysTheRunesTheMuleIsOutOfFirst(t *testing.T) {
	r := newRotRig(t, "shortbuyer1", 41, runerunnerplan.BettyStand.X, runerunnerplan.BettyStand.Z, map[string]interface{}{"buyAll": false})
	defer releaseBoards("shortbuyer1")
	r.ctx.Peers = peerbank.New()
	base := append([]string{}, r.b.set.RuneList...)
	baseChaos := r.b.set.CapTable.For("Chaos rune")

	// No bank published: the settings stand.
	r.b.refreshShortage(r.ctx)
	if !reflect.DeepEqual(r.b.set.RuneList, base) || r.b.set.CapTable.For("Chaos rune") != baseChaos {
		t.Fatalf("an unpublished mule changed the plan: %v", r.b.set.RuneList)
	}

	publishRuneMule(r, map[string]int{"Coins": 5628483, "Water rune": 395773, "Body rune": 193811, "Nature rune": 109, "Law rune": 40362})
	r.b.refreshShortage(r.ctx)
	if got := r.b.set.RuneList[:4]; !reflect.DeepEqual(got, []string{"Nature rune", "Chaos rune", "Death rune", "Fire rune"}) {
		t.Fatalf("priority starts %v", got)
	}
	for rune, want := range map[string]int{"Nature rune": 120, "Chaos rune": 90, "Death rune": 180, "Fire rune": 24} {
		if got := r.b.set.CapTable.For(rune); got != want {
			t.Errorf("cap %s = %d, want %d", rune, got, want)
		}
	}
	if !strings.Contains(r.log(), "shortage: the mule is short of") {
		t.Errorf("no shortage line in\n%s", r.log())
	}

	// Restocked: back to the settings' own order and caps, from the saved base.
	publishRuneMule(r, map[string]int{"Nature rune": 5000, "Chaos rune": 9000, "Death rune": 4000, "Fire rune": 20000})
	r.b.refreshShortage(r.ctx)
	if !reflect.DeepEqual(r.b.set.RuneList, base) || r.b.set.CapTable.For("Chaos rune") != baseChaos {
		t.Fatalf("a restocked mule left the plan changed: %v chaos cap %d", r.b.set.RuneList, r.b.set.CapTable.For("Chaos rune"))
	}
}

func TestRuneRunnerShortageFirstOffKeepsTheSettings(t *testing.T) {
	r := newRotRig(t, "shortbuyer2", 41, runerunnerplan.BettyStand.X, runerunnerplan.BettyStand.Z, map[string]interface{}{"shortageFirst": false, "buyAll": false})
	defer releaseBoards("shortbuyer2")
	r.ctx.Peers = peerbank.New()
	base := append([]string{}, r.b.set.RuneList...)
	publishRuneMule(r, map[string]int{"Water rune": 395773})
	r.b.refreshShortage(r.ctx)
	if !reflect.DeepEqual(r.b.set.RuneList, base) || r.b.set.CapTable.For("Chaos rune") != 45 {
		t.Fatalf("shortageFirst off still changed the plan: %v", r.b.set.RuneList)
	}
}

// A round planned for a Lundail-first phase carries the purse station (the live acct35 and
// Acct171 had none, so never asked the mule for coins).
func TestRuneRunnerLundailFirstRoundPlansThePurse(t *testing.T) {
	r := newRotRig(t, "shortbuyer3", 41, 2809, 3441, map[string]interface{}{"rotationPhase": 3.0})
	defer releaseBoards("shortbuyer3")
	r.b.newRound(r.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r.b.deps(r.ctx))), false)
	if runerunnerplan.Index(r.b.st.Ring, "purse@41") < 0 {
		t.Fatalf("no purse station in %s", runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
	if runerunnerplan.Index(r.b.st.Ring, "draw@41") >= 0 {
		t.Fatalf("a draw station in a Lundail-first ring %s", runerunnerplan.PlanString(r.b.st.Ring, 0))
	}
}

// The purse station does nothing (and walks nowhere) when a fresh reading shows the account
// funded, and says so; the top-up off does the same.
func TestRuneRunnerPurseSkipsWhenFundedOrOff(t *testing.T) {
	// buyAll off: the top-up threshold is the setting's own 150,000 (with buyAll it follows the circuit's cost:
	// runerunnerplan.CircuitTopUp, buyall_test.go).
	r := newRotRig(t, "shortbuyer4", 41, 2655, 3283, map[string]interface{}{"rotationPhase": 3.0, "buyAll": false})
	defer releaseBoards("shortbuyer4")
	r.b.newRound(r.ctx, runerunnerplan.AreaOf(fleetrunner.Here(r.b.deps(r.ctx))), false)
	i := runerunnerplan.Index(r.b.st.Ring, "purse@41")
	r.b.st.JumpTo(i)
	r.b.st.BankCoins, r.b.st.BankReadAt = 400000, time.Now().Add(-time.Minute)
	r.b.runStation(r.ctx, r.b.deps(r.ctx), r.b.st.Ring[i], fleetrunner.Here(r.b.deps(r.ctx)))
	if got := r.b.st.Done["purse@41"]; got.Outcome != runerunnerplan.OutcomeSkipped || !strings.Contains(got.Reason, "not under topUpBelow") {
		t.Fatalf("purse with a funded bank: %+v\n%s", got, r.log())
	}
	if s, _ := r.b.st.Current(); s.Key() == "purse@41" {
		t.Errorf("the cursor stayed on the purse")
	}
}
