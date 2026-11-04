package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/mastercraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// cannonFake plays the Falador west bank and furnace for the trip loop: a bank of Steel bars, a pack, and the furnace turning the pack's
// bars into four cannonballs each. The loop's seams are replaced for the test.
type cannonFake struct {
	steelBank, steelPack int
	balls                int
	mould                bool
	failOpens            int
	noFurnace            bool
	furnaceReason        string // the furnace's refusal, "" for none
	draws                []int
}

func newCannonFake(t *testing.T, steel int) *cannonFake {
	masterCannonRetryWait = 0
	t.Cleanup(func() { masterCannonRetryWait = 3 * time.Second })
	f := &cannonFake{steelBank: steel, mould: true}
	oldKit, oldFind, oldApproach, oldSmelt := masterCannonKitVisit, masterCannonFindFurnace, masterCannonApproach, masterCannonSmelt
	t.Cleanup(func() {
		masterCannonKitVisit, masterCannonFindFurnace, masterCannonApproach, masterCannonSmelt = oldKit, oldFind, oldApproach, oldSmelt
	})
	masterCannonKitVisit = func(_ *fleetrunner.Deps, remaining int) (mastercraftrunner.CannonKitResult, bool) {
		if f.failOpens > 0 {
			f.failOpens--
			return mastercraftrunner.CannonKitResult{}, false
		}
		f.steelBank += f.steelPack
		f.steelPack = 0
		res := mastercraftrunner.CannonKitResult{Free: 27, Mould: f.mould}
		if !f.mould {
			res.NoMould = true
			return res, true
		}
		n := mastercraftplan.CannonDraw(27, remaining, f.steelBank)
		f.steelBank -= n
		f.steelPack = n
		res.Drew = n
		if n > 0 {
			f.draws = append(f.draws, n)
		}
		return res, true
	}
	masterCannonFindFurnace = func(*fleetrunner.Deps, tile.Tile, int) (locindex.Match, bool) {
		return locindex.Match{Loc: locindex.Loc{ID: 11666, X: 2975, Z: 3369}}, !f.noFurnace
	}
	masterCannonApproach = func(*fleetrunner.Deps, locindex.Match, mastercraftrunner.WalkFunc, time.Duration) bool { return true }
	masterCannonSmelt = func(_ *fleetrunner.Deps, _ locindex.Match, _ func() bool) mastercraftrunner.CannonResult {
		if f.furnaceReason != "" {
			return mastercraftrunner.CannonResult{Reason: f.furnaceReason}
		}
		n := f.steelPack
		f.steelPack = 0
		f.balls += n * mastercraftplan.CannonballsPerBar
		return mastercraftrunner.CannonResult{Bars: n, Balls: n * mastercraftplan.CannonballsPerBar, Uses: n, XP: n * mastercraftplan.CannonballXPTenths / 10, Reason: "done"}
	}
	return f
}

func (f *cannonFake) run(r *masterRig, bars int) *cannonRun {
	run := &cannonRun{b: r.b, ctx: r.ctx, d: &fleetrunner.Deps{}, plan: mastercraftplan.CannonballPlan{Bars: bars}, hold: func() bool { return true }}
	run.loop()
	return run
}

// A batch of 100 bars is three full packs of 27 and one of 19, 400 balls, and the round is finished (not cut short).
func TestCannonRoundSmeltsTheBatchInFullPacks(t *testing.T) {
	r := newMasterRig(t)
	f := newCannonFake(t, 5000)
	run := f.run(r, 100)
	if !run.finished || run.refused || run.transient || run.bars != 100 || run.balls != 400 || f.balls != 400 {
		t.Fatalf("%+v fake %+v", run, f)
	}
	if run.trips != 4 {
		t.Errorf("%d trips: 27 + 27 + 27 + 19", run.trips)
	}
	if got := f.draws; len(got) != 4 || got[0] != 27 || got[1] != 27 || got[2] != 27 || got[3] != 19 {
		t.Errorf("draws %v", got)
	}
	if f.steelBank != 4900 {
		t.Errorf("the bank holds %d, want 4900", f.steelBank)
	}
	if !strings.Contains(run.reason, "batch of 100 bars is smelted") {
		t.Errorf("reason %q", run.reason)
	}
	// The log says what each trip did.
	var trip bool
	for _, l := range r.logs {
		if strings.Contains(l, "cannonballs: trip 1:") && strings.Contains(l, "round so far: 27 of 100 bars -> 108 cannonballs") {
			trip = true
		}
	}
	if !trip {
		t.Errorf("no trip line in %v", r.logs)
	}
}

// A bank that runs dry ends the round finished with what it had.
func TestCannonRoundEndsWhenTheBankIsEmpty(t *testing.T) {
	r := newMasterRig(t)
	f := newCannonFake(t, 40)
	run := f.run(r, 500)
	if !run.finished || run.bars != 40 || run.balls != 160 || !strings.Contains(run.reason, "no Steel bar left") {
		t.Fatalf("%+v", run)
	}
}

// No Ammo mould: refused, never a use of the furnace (no blind attempts).
func TestCannonRoundWithoutAMouldDoesNotSmelt(t *testing.T) {
	r := newMasterRig(t)
	f := newCannonFake(t, 500)
	f.mould = false
	run := f.run(r, 100)
	if !run.refused || run.finished || run.bars != 0 || f.balls != 0 || !strings.Contains(run.reason, "no Ammo mould") {
		t.Fatalf("%+v", run)
	}
}

// A furnace refusal (level, free world) ends the round as a refusal; a stalled furnace backs the round off after three tries.
func TestCannonRoundFurnaceFailures(t *testing.T) {
	r := newMasterRig(t)
	f := newCannonFake(t, 500)
	f.furnaceReason = "level"
	if run := f.run(r, 100); !run.refused || run.transient || !strings.Contains(run.reason, "level") {
		t.Errorf("level: %+v", run)
	}
	f = newCannonFake(t, 500)
	f.furnaceReason = "stalled"
	run := f.run(r, 100)
	if run.refused || !run.transient || run.finished || run.trips != masterCannonFails {
		t.Errorf("stalled: %+v", run)
	}
	f = newCannonFake(t, 500)
	f.noFurnace = true
	if run := f.run(r, 100); !run.transient || !strings.Contains(run.reason, "loc index") {
		t.Errorf("no furnace: %+v", run)
	}
	f = newCannonFake(t, 500)
	f.failOpens = 5
	if run := f.run(r, 100); !run.transient || !strings.Contains(run.reason, "would not open") {
		t.Errorf("bank: %+v", run)
	}
	// One failed open is retried and the round goes on.
	f = newCannonFake(t, 500)
	f.failOpens = 1
	if run := f.run(r, 27); !run.finished || run.bars != 27 {
		t.Errorf("one retry: %+v", run)
	}
}

// The hold ends the round cut short: neither finished nor refused, so the ledger keeps it active.
func TestCannonRoundHeldIsCutShort(t *testing.T) {
	r := newMasterRig(t)
	f := newCannonFake(t, 500)
	run := &cannonRun{b: r.b, ctx: r.ctx, d: &fleetrunner.Deps{}, plan: mastercraftplan.CannonballPlan{Bars: 100}, hold: func() bool { return false }}
	run.loop()
	if run.finished || run.refused || run.trips != 0 || len(f.draws) != 0 {
		t.Errorf("%+v", run)
	}
}

// The deliver round keeps the Steel bars of a cannonball round under way (as it does an anvil round's) and always carries the balls.
func TestCannonBarsAreKeptWhileTheRoundIsActive(t *testing.T) {
	r := newMasterRig(t)
	if r.b.cannonKeepsBars() {
		t.Fatal("no round under way")
	}
	r.b.ledger.Cannon.Active = true
	if !r.b.cannonKeepsBars() {
		t.Fatal("a round under way keeps its bars")
	}
	if ps := mastercraftrunner.CannonProducts(); len(ps) != 1 || ps[0].Name != "Cannonball" {
		t.Fatalf("%+v", ps)
	}
}

// A round that smelted nothing for a reason that will not pass (no mould) is paused; one that failed for a passing reason backs off;
// one that made balls clears both.
func TestCannonRoundWaits(t *testing.T) {
	r := newMasterRig(t)
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	set := r.b.set
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeCannonballs, Why: "no Ammo mould"}, now)
	if until := r.b.ledger.Cannon.PauseUntil; !until.Equal(now.Add(set.FruitlessPause)) {
		t.Errorf("pause %v", until)
	}
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeCannonballs, Produced: 400}, now)
	if !r.b.ledger.Cannon.PauseUntil.IsZero() {
		t.Error("a round that made balls clears the pause")
	}
	r.b.recordWaits(r.ctx, masterRoundOutcome{Mode: mastercraftplan.ModeCannonballs, Transient: true, Why: "the bank would not open"}, now)
	if until, ok := r.b.ledger.BackedOff(mastercraftplan.ModeCannonballs, now); !ok || !until.After(now) {
		t.Errorf("backoff %v %v", until, ok)
	}
}
