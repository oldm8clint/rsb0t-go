package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/essencerunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The live handlers that need no real data: the mule ask and the mining stage, over the simulated bank and the
// fake kit bundle every MasterCrafter round test uses.

func newRuneWorld(t *testing.T, r *masterRig, mod func(w *masterRuneWorld)) *masterRuneWorld {
	t.Helper()
	r.b.set.Rune.On = true
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: r.b.deps(r.ctx), started: time.Now(), set: r.b.set.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	if mod != nil {
		mod(w)
	}
	return w
}

// The ask: the lines go to the mule as NOTES in one bundled kit (a 3,000 essence ask is one pack slot), what arrives is
// banked at the post, and the bank figures are read off the bank: the next stage decides on them.
func TestTheRuneAskBanksWhatTheMuleGave(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Rune essence": 3000, "Air talisman": 1541, "Body talisman": 677})
	var items []fleetplan.ItemCount
	var opts fleetrunner.KitBundleOptions
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, o fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		items, opts = it, o
		r.sim.SetBank([]actionsim.Stack{{ID: runecraftplan.EssenceID, Count: 3000}, {ID: 1438, Count: 2}, {ID: 1446, Count: 2}})
		return fleetrunner.KitResult{Got: map[string]int{"Rune essence": 3000, "Air talisman": 2, "Body talisman": 2}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	w := newRuneWorld(t, r, nil)
	step := runecraftplan.Step{Stage: runecraftplan.StageAskMule, Lines: []runecraftplan.Line{
		{Item: "Rune essence", Qty: 3000}, {Item: "Air talisman", Qty: 2}, {Item: "Body talisman", Qty: 2}}}
	res := w.AskMule(step)
	if !res.OK || !w.asked {
		t.Fatalf("%+v asked %v\n%v", res, w.asked, r.logs)
	}
	if !opts.Noted || !opts.KeepNotes {
		t.Errorf("the ask is noted with the notes kept: %+v", opts)
	}
	got := map[string]int{}
	for _, it := range items {
		got[it.Item] = it.Count
	}
	if got["Rune essence"] != 3000 || got["Air talisman"] != 2 || got["Body talisman"] != 2 || len(got) != 3 {
		t.Errorf("asked %v", got)
	}
	if !w.bankKnown || w.bankEssence != 3000 || w.bankTal[runecraftplan.KeyAir] != 2 || w.bankTal[runecraftplan.KeyBody] != 2 {
		t.Errorf("the bank as the world sees it: %v %d %v", w.bankKnown, w.bankEssence, w.bankTal)
	}
	if l := r.b.ledger.Rune; l.Asked != 3000 || l.BankEssence != 3000 || !l.Known() {
		t.Errorf("ledger %+v", l)
	}
	if !r.logged("runecraft: asked mule for 3,000 Rune essence, 2 Air talisman, 2 Body talisman") {
		t.Errorf("log: %v", r.logs)
	}
}

// A mule that truly holds none is asked no more this round; one that could not be reached or deferred the kit is a
// passing failure (try again), and neither pretends something arrived.
func TestTheRuneAskTellsEmptyFromAbsent(t *testing.T) {
	r := newSimRig(t, nil, nil, map[string]int{"Rune essence": 3000})
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{"Rune essence": "declined: none in the bank"}}, fleetrunner.KitBundleInfo{Bundled: true}
	}
	w := newRuneWorld(t, r, nil)
	step := runecraftplan.Step{Stage: runecraftplan.StageAskMule, Lines: []runecraftplan.Line{{Item: "Rune essence", Qty: 3000}}}
	if res := w.AskMule(step); !res.OK || !w.asked || r.b.ledger.Rune.Asked != 0 {
		t.Fatalf("the mule is out: %+v asked %v", res, w.asked)
	}
	r2 := newSimRig(t, nil, nil, map[string]int{"Rune essence": 3000})
	scarabKitBundle = func(_ *fleetrunner.Deps, _ *mulebus.Bus, _, _ string, it []fleetplan.ItemCount, _ fleetrunner.KitBundleOptions) (fleetrunner.KitResult, fleetrunner.KitBundleInfo) {
		return fleetrunner.KitResult{Got: map[string]int{}, Declined: map[string]string{}}, fleetrunner.KitBundleInfo{Bundled: true, Reason: "the mule did not answer"}
	}
	w2 := newRuneWorld(t, r2, nil)
	if res := w2.AskMule(step); res.OK || w2.asked || !strings.Contains(res.Why, "nothing arrived") {
		t.Fatalf("the mule did not answer: %+v asked %v", res, w2.asked)
	}
}

// fakeRuneMine is the mining stretch's world for the handler: each load banks 27, the Guild lets the account in.
type fakeRuneMine struct {
	inMine bool
	pack   int
	bank   int
	refuse bool
	ticks  int
	// portalFails is how many portal clicks fail before one takes the account out; portals counts the clicks; onExit runs
	// when the portal takes the account out (a test moves the simulated account out of the mine's mapsquare).
	portalFails, portals int
	onExit               func()
}

func (f *fakeRuneMine) InMine() bool     { return f.inMine }
func (f *fakeRuneMine) PackFull() bool   { return f.pack >= 27 }
func (f *fakeRuneMine) Haul() int        { return f.pack }
func (f *fakeRuneMine) PickHeld() bool   { return true }
func (f *fakeRuneMine) PickName() string { return "rune pickaxe" }
func (f *fakeRuneMine) Tick() int        { f.ticks += 10; return f.ticks }
func (f *fakeRuneMine) Stopped() bool    { return false }
func (f *fakeRuneMine) PortalOut() bool {
	f.portals++
	if f.portalFails > 0 {
		f.portalFails--
		return false
	}
	f.inMine = false
	if f.onExit != nil {
		f.onExit()
	}
	return true
}
func (f *fakeRuneMine) FetchPickaxe() (string, bool) { return "", false }
func (f *fakeRuneMine) Mine() essencerunner.MineResult {
	f.pack = 27
	return essencerunner.MineResult{Gained: 27, Reason: "full"}
}
func (f *fakeRuneMine) BankDeposit() (int, int, bool) {
	n := f.pack
	f.pack = 0
	f.bank += n
	return f.bank, n, true
}
func (f *fakeRuneMine) TeleportIn() essencerunner.TeleportResult {
	if f.refuse {
		return essencerunner.TeleportResult{QuestRefused: true, Reason: "Rune Mysteries is not complete"}
	}
	f.inMine = true
	return essencerunner.TeleportResult{Landed: true, Ticks: 5}
}

func withRuneMineEnv(t *testing.T, env runecraftrunner.MineEnv) {
	t.Helper()
	old := runeMineEnv
	t.Cleanup(func() { runeMineEnv = old })
	runeMineEnv = func(*fleetrunner.Deps, runecraftrunner.LiveOptions) runecraftrunner.MineEnv { return env }
}

// The mining stage: a stretch of whole loads, the bank's total and the ledger's counters updated, the gateway the
// setting names; a refused one marks the mine failed so the stage machine stops instead of trying again.
func TestTheRuneMineStageRunsAStretchAndRecordsIt(t *testing.T) {
	r := newSimRig(t, nil, nil, nil)
	mindHoldStat(r.sim.State(), 6, levels.XPAtLevel(78), 78) // Magic 78: the Guild's door
	env := &fakeRuneMine{bank: 40}
	withRuneMineEnv(t, env)
	w := newRuneWorld(t, r, func(w *masterRuneWorld) { w.bankKnown, w.bankEssence = true, 40 })
	res := w.Mine(runecraftplan.Step{Stage: runecraftplan.StageMine, Goal: 108})
	if !res.OK || w.mineFailed || w.mined != 108 || w.bankEssence != 40+108 {
		t.Fatalf("%+v mined %d bank %d failed %v\n%v", res, w.mined, w.bankEssence, w.mineFailed, r.logs)
	}
	if l := r.b.ledger.Rune; l.Mined != 108 || l.MineStretches != 1 || l.BankEssence != 148 {
		t.Errorf("ledger %+v", l)
	}
	for _, want := range []string{
		"runecraft: mining stretch at the guild gateway (Wizard Distentor): 108 essence to bank",
		"essencerunner: guild: Distentor Teleport ok, landed (0,0) after 5 ticks",
		"runecraft: mining stretch over after",
	} {
		if !r.logged(want) {
			t.Errorf("log lacks %q:\n%s", want, strings.Join(r.logs, "\n"))
		}
	}
	// The Guild refuses the teleport: nothing is banked, the stage fails and the world will not mine again this round.
	r2 := newSimRig(t, nil, nil, nil)
	mindHoldStat(r2.sim.State(), 6, levels.XPAtLevel(78), 78)
	withRuneMineEnv(t, &fakeRuneMine{refuse: true})
	w2 := newRuneWorld(t, r2, nil)
	res = w2.Mine(runecraftplan.Step{Stage: runecraftplan.StageMine, Goal: 108})
	if res.OK || !w2.mineFailed || !strings.Contains(res.Why, "refused") {
		t.Fatalf("%+v failed %v", res, w2.mineFailed)
	}
	// Under the Guild's Magic 66 the stage does not even start the stretch.
	r3 := newSimRig(t, nil, nil, nil)
	mindHoldStat(r3.sim.State(), 6, levels.XPAtLevel(50), 50)
	withRuneMineEnv(t, &fakeRuneMine{})
	w3 := newRuneWorld(t, r3, nil)
	res = w3.Mine(runecraftplan.Step{Stage: runecraftplan.StageMine, Goal: 108})
	if res.OK || !w3.mineFailed || !strings.Contains(res.Why, "Magic 66") {
		t.Fatalf("Magic 50: %+v", res)
	}
}

// THE MINE IS NEVER WHERE A ROUND ENDS. The round's clock running out while the account is in the mine used to end the
// stretch right there (the stretch checks it at the top of every step), the loop then picked the mining stage again with
// the clock already out, three of those failed the round, and ScarabSwarm started in the mine.
func TestTheRuneMineStageTakesTheAccountOutWhenTheRoundsClockRunsOut(t *testing.T) {
	r := newSimRig(t, nil, nil, nil)
	mindHoldStat(r.sim.State(), 6, levels.XPAtLevel(78), 78)
	env := &fakeRuneMine{inMine: true, pack: 27}
	withRuneMineEnv(t, env)
	w := newRuneWorld(t, r, func(w *masterRuneWorld) { w.started = time.Now().Add(-5 * time.Hour) }) // the 240 minutes are long used
	if !w.Expired() {
		t.Fatal("the round's clock should be out")
	}
	res := w.Mine(runecraftplan.Step{Stage: runecraftplan.StageMine, Goal: 108})
	if !res.OK || env.inMine || env.portals != 1 || w.mineFailed {
		t.Fatalf("%+v inMine %v portals %d mineFailed %v: %v", res, env.inMine, env.portals, w.mineFailed, r.logs)
	}
	if !r.logged("the stretch is over (the round's time is used up) with the account inside the mine - leaving by the portal first") {
		t.Errorf("log: %v", r.logs)
	}
}

// A portal that will not take the account out is a failure of the stage that SAYS so, not a quiet end inside the mine.
func TestTheRuneMineStageFailsWhenTheAccountCannotLeave(t *testing.T) {
	r := newSimRig(t, nil, nil, nil)
	mindHoldStat(r.sim.State(), 6, levels.XPAtLevel(78), 78)
	env := &fakeRuneMine{inMine: true, pack: 27, portalFails: 99}
	withRuneMineEnv(t, env)
	w := newRuneWorld(t, r, func(w *masterRuneWorld) { w.started = time.Now().Add(-5 * time.Hour) })
	res := w.Mine(runecraftplan.Step{Stage: runecraftplan.StageMine, Goal: 108})
	if res.OK || !strings.Contains(res.Why, "still inside the mine") || !env.inMine || env.portals != runecraftrunner.LeaveTries {
		t.Fatalf("%+v inMine %v portals %d", res, env.inMine, env.portals)
	}
}

// mineTile is a tile in the essence mine's mapsquare (45,75) and outsideTile one beside the Wizards' Guild.
var mineTile, outsideTile = tile.Tile{X: 2891, Z: 4830}, tile.Tile{X: 2594, Z: 3086}

// shortLeave makes the exit tries instant.
func shortLeave(t *testing.T) {
	t.Helper()
	old := masterRuneLeavePause
	t.Cleanup(func() { masterRuneLeavePause = old })
	masterRuneLeavePause = 0
}

// noBank makes every bank visit fail at once, so a test of the exit does not walk to one.
func noBank(t *testing.T) {
	t.Helper()
	old := runecraftrunner.OpenBank
	t.Cleanup(func() { runecraftrunner.OpenBank = old })
	runecraftrunner.OpenBank = func(*fleetrunner.Deps, tile.Tile) (*actions.Bank, bool) { return nil, false }
}

// The end of a round that finds the account in the mine takes the exit FIRST, and says it did.
func TestTheEndOfARoundTakesTheAccountOutOfTheMineFirst(t *testing.T) {
	shortLeave(t)
	noBank(t)
	r := newSimRig(t, nil, nil, nil)
	r.sim.StandAt(mineTile.X, mineTile.Z)
	env := &fakeRuneMine{inMine: true, portalFails: 1, onExit: func() { r.sim.StandAt(outsideTile.X, outsideTile.Z) }}
	withRuneMineEnv(t, env)
	w := newRuneWorld(t, r, nil)
	what, stranded := w.finish()
	if stranded || env.inMine || env.portals != 2 || !strings.Contains(what, "left the mine") {
		t.Fatalf("what %q stranded %v inMine %v portals %d: %v", what, stranded, env.inMine, env.portals, r.logs)
	}
	if x, z, _, _ := r.sim.State().Tile(); x != outsideTile.X || z != outsideTile.Z {
		t.Errorf("the account stands at %d,%d", x, z)
	}
}

// Four exit tries that all fail: the round reports the account STRANDED (switchTo then holds it, see mastercrafter_strand_test.go)
// rather than "left inside the mine (the next round carries on from there)" and a ScarabSwarm that starts in it.
func TestTheEndOfARoundReportsAnAccountThatCannotLeaveTheMine(t *testing.T) {
	shortLeave(t)
	noBank(t)
	r := newSimRig(t, nil, nil, nil)
	r.sim.StandAt(mineTile.X, mineTile.Z)
	env := &fakeRuneMine{inMine: true, portalFails: 99}
	withRuneMineEnv(t, env)
	w := newRuneWorld(t, r, nil)
	what, stranded := w.finish()
	if !stranded || !env.inMine || env.portals != masterRuneLeaveTries || !strings.Contains(what, "STRANDED inside the mine") {
		t.Fatalf("what %q stranded %v portals %d", what, stranded, env.portals)
	}
	if !r.logged("WARNING: STRANDED inside the mine") {
		t.Errorf("log: %v", r.logs)
	}
}
