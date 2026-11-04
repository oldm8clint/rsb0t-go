package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The follow-ups of the P1 review (14 Oct 2026): a stuck round is paused whatever it made, a failing round is not resumed at every
// check (mastercraftplan/resume_test.go), the end of a round banks the pack or says loudly that it did not, and the teleport kit's
// top-up is best-effort.

// ---- a stuck round ----

func TestRecordWaitsAStuckRuneRoundIsPausedWhateverItMade(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for name, out := range map[string]masterRoundOutcome{
		"crafted a load, then had no essence": {Mode: mastercraftplan.ModeRunecraft, Produced: 540, Stuck: true, Why: "no essence source"},
		"mined and crafted, then stuck":       {Mode: mastercraftplan.ModeRunecraft, Produced: 1, Stuck: true, Why: "no essence source"},
		"stuck and stranded":                  {Mode: mastercraftplan.ModeRunecraft, Produced: 27, Stuck: true, Transient: true, Stranded: true, Why: "STRANDED"},
		"stuck with nothing made":             {Mode: mastercraftplan.ModeRunecraft, Stuck: true, Why: "no essence source"},
	} {
		r := newMasterRig(t)
		r.b.set.Rune.On = true
		led := &r.b.ledger
		led.Rune.LastStop = "no essence source: own bank and pack 0"
		r.b.recordWaits(r.ctx, out, now)
		if want := now.Add(r.b.set.FruitlessPause); led.Rune.PauseUntil != want {
			t.Errorf("%s: pause until %v, want %v", name, led.Rune.PauseUntil, want)
		}
		if led.Rune.LastStop == "" {
			t.Errorf("%s: the reason is kept for the log after a restart", name)
		}
		if !r.logged("rounds are paused until") {
			t.Errorf("%s: the pause is not said: %v", name, r.logs)
		}
		if out.Transient && led.Fails(mastercraftplan.ModeRunecraft) != 1 {
			t.Errorf("%s: a transient stuck round also backs off", name)
		}
		if !out.Transient && led.Fails(mastercraftplan.ModeRunecraft) != 0 {
			t.Errorf("%s: fails %d", name, led.Fails(mastercraftplan.ModeRunecraft))
		}
	}
	// A round that made something and is NOT stuck still clears its pause (the existing rule).
	r := newMasterRig(t)
	r.b.ledger.Rune.PauseUntil = now.Add(time.Hour)
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeRunecraft, Produced: 540}, now)
	if !r.b.ledger.Rune.PauseUntil.IsZero() {
		t.Error("a round that crafted and ended normally clears the pause")
	}
}

func TestRecordWaitsAStuckNatureRoundIsPausedWhateverItMade(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	r := newMasterRig(t)
	r.b.set.Rune.On = true
	r.b.ledger.Rune.Gate.LastStop = "no Nature talisman in the pack"
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, Produced: 750, Stuck: true, Why: "stuck"}, now)
	if got := r.b.ledger.Rune.Gate.PauseUntil; got != now.Add(r.b.set.FruitlessPause) {
		t.Fatalf("pause until %v", got)
	}
	// Not stuck and made something: cleared. Stuck, transient and made something: paused AND backed off.
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, Produced: 750}, now)
	if !r.b.ledger.Rune.Gate.PauseUntil.IsZero() {
		t.Fatal("a normal round clears the pause")
	}
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeNature, Produced: 750, Stuck: true, Transient: true, Why: "stuck"}, now)
	if r.b.ledger.Rune.Gate.PauseUntil.IsZero() || r.b.ledger.Fails(mastercraftplan.ModeNature) != 1 {
		t.Fatalf("pause %v fails %d", r.b.ledger.Rune.Gate.PauseUntil, r.b.ledger.Fails(mastercraftplan.ModeNature))
	}
}

// The levelling round hands the stage machine's "stuck" to the outcome.
func TestTheRuneRoundReportsStuckInItsOutcome(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{})
	r.b.set.Rune.On = true
	withRuneBank(t, r)
	mindHoldStat(r.sim.State(), runecraftrunner.Stat, levels.XPAtLevel(5), 5)
	r.sim.StandAt(2809, 3441)
	out := r.b.runecraftRound(r.ctx, r.b.deps(r.ctx), time.Now(), mastercraftplan.Decision{Mode: mastercraftplan.ModeRunecraft, Why: "test"})
	if !out.Stuck || out.Transient || out.Produced != 0 {
		t.Fatalf("%+v\n%v", out, r.logs)
	}
	if r.b.ledger.Rune.LastStop == "" {
		t.Fatal("the reason is kept")
	}
	// The same outcome through afterRound is paused and not backed off.
	r.b.afterRound(r.ctx, out, time.Second)
	if r.b.ledger.Rune.PauseUntil.IsZero() || r.b.ledger.Fails(mastercraftplan.ModeRunecraft) != 0 {
		t.Fatalf("pause %v fails %d", r.b.ledger.Rune.PauseUntil, r.b.ledger.Fails(mastercraftplan.ModeRunecraft))
	}
}

// ---- the end of a round always banks or says it did not ----

const airRuneObj = 556

func finishRig(t *testing.T) (*masterRig, *masterRuneWorld) {
	t.Helper()
	stackable(t, airRuneObj)
	r := newSimRig(t, []actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 27}, {ID: airRuneObj, Count: 54}}, nil, nil)
	w := newRuneWorld(t, r, func(w *masterRuneWorld) {
		w.post, w.postKnown = tile.Tile{X: 2809, Z: 3441}, true
		w.lastAltar = runecraftplan.Air
	})
	r.sim.StandAt(runecraftplan.Air.Bank.X, runecraftplan.Air.Bank.Z)
	masterWalkTo = func(d *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		r.sim.StandAt(dest.X, dest.Z)
		return true
	}
	return r, w
}

// The bank near the ruins would not open: the pack is banked at the mule's post after the walk home, and the failure is said.
func TestFinishBanksAtThePostWhenTheBankNearTheRuinsWillNotOpen(t *testing.T) {
	r, w := finishRig(t)
	old := runecraftrunner.OpenBank
	t.Cleanup(func() { runecraftrunner.OpenBank = old })
	runecraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	what, stranded := w.finish()
	if stranded {
		t.Fatalf("%s", what)
	}
	if !strings.Contains(what, "would not open") || !strings.Contains(what, "banked the pack at the mule's post") || strings.Contains(what, "NOT banked") {
		t.Fatalf("what: %s", what)
	}
	if !r.logged("WARNING: the bank at Falador east would not open") {
		t.Fatalf("log: %v", r.logs)
	}
	if p := r.sim.Pack(); len(p) != 0 {
		t.Fatalf("the pack is banked: %v", p)
	}
	if b := r.sim.Bank(); simCount(b, runecraftplan.EssenceID) != 27 || simCount(b, airRuneObj) != 54 {
		t.Fatalf("bank %v", b)
	}
}

// Neither bank opens: the pack stays with the account and the round says so loudly.
func TestFinishSaysLoudlyWhenNothingBanks(t *testing.T) {
	r, w := finishRig(t)
	old, oldD := runecraftrunner.OpenBank, mastercraftrunner.OpenBank
	t.Cleanup(func() { runecraftrunner.OpenBank, mastercraftrunner.OpenBank = old, oldD })
	runecraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	what, _ := w.finish()
	if !strings.Contains(what, "NOT banked") || !strings.Contains(what, "27 essence") || !strings.Contains(what, "54 Air rune") {
		t.Fatalf("what: %s", what)
	}
	if !r.logged("WARNING: the round ends with the pack NOT BANKED: 27 essence, 54 Air rune") || !r.logged("would not open") {
		t.Fatalf("log: %v", r.logs)
	}
	if len(r.sim.Pack()) == 0 {
		t.Fatal("nothing could have been banked")
	}
}

// With a bank that opens nothing is said and the pack is banked where it was meant to be.
func TestFinishBanksNearTheRuinsWithoutAWarning(t *testing.T) {
	r, w := finishRig(t)
	withRuneBank(t, r)
	what, _ := w.finish()
	if !strings.Contains(what, "banked the pack at Falador east") || strings.Contains(what, "NOT") || r.logged("WARNING") {
		t.Fatalf("what: %s\n%v", what, r.logs)
	}
}

// The gate round's end says so when the post's bank will not open.
func TestFinishNatureSaysWhenThePackIsNotBanked(t *testing.T) {
	r := newNatureRig(t, []actionsim.Stack{{ID: natTal, Count: 1}, {ID: natRune, Count: 4000}, {ID: coinsObj, Count: 100}}, nil, nil)
	r.sim.StandAt(2809, 3441)
	w := newNatureWorld(t, r, nil)
	old := mastercraftrunner.OpenBank
	t.Cleanup(func() { mastercraftrunner.OpenBank = old })
	mastercraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
	what, stranded := w.finishNature()
	if stranded || !strings.Contains(what, "NOT banked") || !r.logged("WARNING: the bank at the mule's post would not open") {
		t.Fatalf("%s\n%v", what, r.logs)
	}
}

// ---- the teleport kit's top-up is best-effort ----

func kitStep() runecraftplan.Step {
	return runecraftplan.Step{Stage: runecraftplan.StageAskMule, Altar: runecraftplan.Air, Optional: true, Lines: []runecraftplan.Line{
		{Item: runecraftplan.RuneLaw, Qty: 20}, {Item: runecraftplan.RuneWater, Qty: 10}, {Item: runecraftplan.RuneAir, Qty: 80}}}
}

// A busy mule (nothing arrived) does not fail the round, and is not asked again this round or for two hours.
func TestTheKitTopUpDoesNotFailTheRoundWhenTheMuleIsBusy(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Law rune": 28403, "Water rune": 500, "Air rune": 683500})
	scarabKitBundle = func(*fleetrunner.Deps, *mulebus.Bus, string, string, []fleetplan.ItemCount, fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Reason: "the mule is busy"}
	}
	w := newRuneWorld(t, r, nil)
	if res := w.AskMule(kitStep()); !res.OK || !w.asked {
		t.Fatalf("%+v asked %v\n%v", res, w.asked, r.logs)
	}
	if !r.b.ledger.Rune.KitAskRecent(time.Now()) {
		t.Fatal("the ask is remembered so the next round does not repeat it")
	}
	if !r.logged("the teleport kit top-up") || !r.logged("carrying on without it") {
		t.Fatalf("log: %v", r.logs)
	}
	// The snapshot says so to the stage machine.
	if !w.Read().KitAskRecent {
		t.Fatal("the stage machine is told the kit was asked lately")
	}
}

// A mule that answers: what arrives is banked, and the same rules hold.
func TestTheKitTopUpBanksWhatArrives(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Law rune": 28403, "Water rune": 500, "Air rune": 683500})
	stackable(t, runecraftrunner.AirRuneID, runecraftrunner.LawRuneID, runecraftrunner.WaterRuneID)
	scarabKitBundle = func(*fleetrunner.Deps, *mulebus.Bus, string, string, []fleetplan.ItemCount, fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		r.sim.Give(runecraftrunner.LawRuneID, 20)
		r.sim.Give(runecraftrunner.WaterRuneID, 10)
		r.sim.Give(runecraftrunner.AirRuneID, 80)
		return fleetrunner.KitResult{Got: map[string]int{"Law rune": 20, "Water rune": 10, "Air rune": 80}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	w := newRuneWorld(t, r, nil)
	w.post, w.postKnown = tile.Tile{X: 2809, Z: 3441}, true
	if res := w.AskMule(kitStep()); !res.OK {
		t.Fatalf("%+v\n%v", res, r.logs)
	}
	if len(r.sim.Pack()) != 0 || w.bankKit.Law != 20 || w.bankKit.Water != 10 || w.bankKit.Air != 80 {
		t.Fatalf("pack %v kit in the bank %+v", r.sim.Pack(), w.bankKit)
	}
}

// A required ask that fails is still a failure (the Optional rule is the kit-only ask's).
func TestARequiredAskStillFailsTheStageWhenTheMuleIsBusy(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Rune essence": 3000})
	scarabKitBundle = func(*fleetrunner.Deps, *mulebus.Bus, string, string, []fleetplan.ItemCount, fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Reason: "the mule is busy"}
	}
	w := newRuneWorld(t, r, nil)
	res := w.AskMule(runecraftplan.Step{Stage: runecraftplan.StageAskMule, Lines: []runecraftplan.Line{{Item: "Rune essence", Qty: 3000}}})
	if res.OK {
		t.Fatalf("a required ask that brought nothing is a failure: %+v", res)
	}
	if r.b.ledger.Rune.KitAskRecent(time.Now()) {
		t.Fatal("only a kit-only ask is remembered as one")
	}
}
