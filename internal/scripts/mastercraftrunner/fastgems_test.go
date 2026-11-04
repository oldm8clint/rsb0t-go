package mastercraftrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// fastRig is an account at the booth with a chisel and 27 uncut rubies in the pack and stock more
// in the bank, Crafting at 90 (the next level far away), after one 7-tick load: the 27 are cut and
// the booth op for the next visit has gone out behind the last burst, as the gem round leaves it.
type fastRig struct {
	e     *tickEngine
	d     *fleetrunner.Deps
	q     *QuickBank
	ruby  gemplan.Gem
	pick  func(uncut map[string]int) (gemplan.Gem, bool)
	total int
	// xp0 is the Crafting XP (tenths) before the rig's 7-tick load.
	xp0 int
}

func newFastRig(t *testing.T, stock int) *fastRig {
	return newFastRigAt(t, stock, levels.XPAtLevel(90)*10)
}

// newFastRigAt is newFastRig with Crafting at xp tenths before its 7-tick load.
func newFastRigAt(t *testing.T, stock, xp int) *fastRig {
	t.Helper()
	e, d, q := newTickEngine(t, load(rubyU, 27), []struct{ id, n int32 }{{rubyU, int32(stock)}})
	e.craftXP = xp
	e.push()
	ruby, _ := gemplan.GemByUncutID(rubyU)
	r := &fastRig{e: e, d: d, q: q, ruby: ruby, total: stock + 27, xp0: xp}
	r.pick = func(uncut map[string]int) (gemplan.Gem, bool) { return ruby, uncut[ruby.Key] > 0 }
	if c := gemrunner.CutAllThen(GemDeps(d), ruby, func() { q.Prime(d) }); c.Reason != "done" || c.Cuts != 27 {
		t.Fatalf("the 7-tick load: %+v", c)
	}
	if !q.Primed() {
		t.Fatal("the booth op did not go out behind the last burst")
	}
	return r
}

// conserve checks no gem was lost or made: every ruby is uncut in the bank or the pack, or cut,
// and the cut ones are the XP's.
func (r *fastRig) conserve(t *testing.T) {
	xp0 := r.xp0
	t.Helper()
	var uncut, cut int32
	for _, b := range r.e.bank {
		switch b.id {
		case rubyU:
			uncut += b.n
		case rubyCut:
			cut += b.n
		}
	}
	uncut += r.e.packTotal(rubyU)
	cut += r.e.packTotal(rubyCut)
	if int(uncut+cut) != r.total {
		t.Fatalf("%d uncut + %d cut, want %d rubies", uncut, cut, r.total)
	}
	if got := (r.e.craftXP - xp0) / 850; got != int(cut) {
		t.Fatalf("%d cuts by the XP, %d cut rubies", got, cut)
	}
}

// THE POINT: a load is SIX engine ticks, with nothing waited for inside the cycle: the deposit,
// the withdrawal and three uses in one tick, five uses a tick after, the booth op the fifth event
// of the sixth, no event refused, no tick over five.
func TestFastGemsSixTicksALoad(t *testing.T) {
	r := newFastRig(t, 27*12)
	const loads = 10
	refused0 := r.e.refused
	res := FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: loads})
	if res.Reason != "done" || res.Loads != loads || res.Cuts != 27*loads || res.Planned != loads {
		t.Fatalf("%s", res.Describe())
	}
	if r.e.refused != refused0 {
		t.Fatalf("the engine refused %d event(s)", r.e.refused-refused0)
	}
	run := r.e.cutTicks[27:]
	for i := 1; i < loads; i++ {
		if gap := run[27*(i+1)-1] - run[27*i-1]; gap != 6 {
			t.Fatalf("load %d took %d ticks, want 6", i, gap)
		}
	}
	for tick, n := range r.e.usesTick {
		if n > 5 {
			t.Fatalf("tick %d applied %d uses", tick, n)
		}
	}
	if res.Ticks > 6*loads+fastSlack+2 {
		t.Fatalf("the run spanned %d ticks for %d loads", res.Ticks, loads)
	}
	if res.Chunks != mastercraftplan.FastChunks(loads) || r.e.badBytes != 0 {
		t.Fatalf("%d chunks sent, %d bad frames", res.Chunks, r.e.badBytes)
	}
	// It ends as a quick load does: the chisel and 27 cut rubies, the bank shut.
	if ok, why := gemSpec(r.ruby, r.pick).done(packItems(r.d)); !ok || r.e.open {
		t.Fatalf("pack: %s, bank open %v", why, r.e.open)
	}
	r.conserve(t)
	// And the next visit is the 7-tick one, which works from there.
	if v, qv := GemQuickVisit(r.d, r.q, r.pick); !qv.Done || v.Drawn != 27 {
		t.Fatalf("%+v / %+v", v, qv)
	}
}

// Jitter: chunks that reach the engine a tick late, or a batch delivered in two halves a tick
// apart, are absorbed by the chunks already waiting in its buffer: five are read every tick, so the
// stream stays on the tick boundary and every load is made, a tick or so later at most.
func TestFastGemsAbsorbsJitter(t *testing.T) {
	for name, set := range map[string]func(e *tickEngine){
		"late frames": func(e *tickEngine) {
			e.lateFrame = map[int]bool{e.sent + 5: true, e.sent + 13: true, e.sent + 14: true, e.sent + 31: true}
		},
		"split batch": func(e *tickEngine) {
			e.splitFrame = map[int]bool{e.sent + 6: true, e.sent + 22: true}
		},
	} {
		r := newFastRig(t, 27*8)
		set(r.e)
		refused0 := r.e.refused
		res := FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: 6})
		if res.Reason != "done" || res.Loads != 6 || r.e.refused != refused0 {
			t.Fatalf("%s: %s, %d refused", name, res.Describe(), r.e.refused-refused0)
		}
		r.conserve(t)
	}
}

// A stray click (any user event the stream did not plan, here a close) puts every later chunk one
// event off the tick boundary: the booth op then shares its tick with the deposit and the
// withdrawal, which the shut bank refuses, and the cuts stop. The run sees the cuts fall behind,
// stops sending, lets the engine drain, and the 7-tick quick visit resyncs from whatever the pack
// holds; the next fast run is clean. No gem is lost.
func TestFastGemsStrayClickDesyncsAndTheQuickPathRecovers(t *testing.T) {
	r := newFastRig(t, 27*20)
	r.e.strayAt = map[int]bool{r.e.ticks + 9: true}
	res := FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: 10})
	if res.Reason != "desync" || res.Loads >= 10 {
		t.Fatalf("%s", res.Describe())
	}
	if r.e.refused == 0 {
		t.Fatal("the stray click should have made the engine refuse the misaligned events")
	}
	r.conserve(t)
	// The fallback, as the gem round does it: a 7-tick quick visit and cut, then fast again.
	v, qv := GemQuickVisit(r.d, r.q, r.pick)
	if !qv.Done || v.Drawn != 27 {
		t.Fatalf("the quick visit after the desync: %+v / %+v", v, qv)
	}
	if c := gemrunner.CutAllThen(GemDeps(r.d), r.ruby, func() { r.q.Prime(r.d) }); c.Reason != "done" || c.Cuts != 27 {
		t.Fatalf("the 7-tick cut after the desync: %+v", c)
	}
	refused0 := r.e.refused
	again := FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: 4})
	if again.Reason != "done" || again.Loads != 4 || r.e.refused != refused0 {
		t.Fatalf("the next run: %s", again.Describe())
	}
	r.conserve(t)
}

// A level gained by a cut in a booth's tick opens the level-up box before the booth can open the
// bank (the engine queue runs first, and the interaction needs no box open), and the whole next
// load would be refused. The run plans around it: the load whose booth tick gains the level is its
// last, sent without the booth op. The quick visit after it closes the box with its booth op and
// carries on.
func TestFastGemsEndsBeforeALevelUpInABoothTick(t *testing.T) {
	// The 52nd cut of the run (load 2, use 25: its booth tick) reaches level 91; the rig's 7-tick
	// load cuts 27 before the run.
	runXP := levels.XPAtLevel(91)*10 - 51*850 - 400
	r := newFastRigAt(t, 27*10, runXP-27*850)
	r.e.levelUps = true
	if got := mastercraftplan.FastLoads(8, runXP/10, r.ruby.XP); got != 2 {
		t.Fatalf("FastLoads = %d, want 2", got)
	}
	refused0 := r.e.refused
	res := FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: 8})
	if res.Reason != "done" || res.Planned != 2 || res.Loads != 2 || r.e.refused != refused0 {
		t.Fatalf("%s, %d refused", res.Describe(), r.e.refused-refused0)
	}
	if r.e.levelUpsSeen != 1 || !r.e.chatBox {
		t.Fatalf("level-ups %d, box open %v", r.e.levelUpsSeen, r.e.chatBox)
	}
	if v, qv := GemQuickVisit(r.d, r.q, r.pick); !qv.Done || v.Drawn != 27 || r.e.chatBox {
		t.Fatalf("the quick visit after the level-up: %+v / %+v", v, qv)
	}
	r.conserve(t)
}

// A run that cannot start (not a full load of the gem in the pack, or the gem to cut next is
// another) makes the visit the 7-tick way instead, so no visit is lost.
func TestFastGemsNotFastMakesTheQuickVisit(t *testing.T) {
	r := newFastRig(t, 27*3)
	other, _ := gemplan.GemByUncutID(1623) // sapphire: not what the pack holds
	res := FastGems(r.d, r.q, other, r.pick, FastOptions{MaxLoads: 5})
	if res.Reason != "not-fast" || res.Fallback == nil || res.Fallback.Drawn != 27 || res.Fallback.Gem.Key != "ruby" {
		t.Fatalf("%s / %+v", res.Describe(), res.Fallback)
	}
	// Too few in the bank for a whole load: the same.
	r = newFastRig(t, 20)
	res = FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: 5})
	if res.Reason != "not-fast" || res.Fallback == nil || res.Fallback.Drawn != 20 {
		t.Fatalf("%s / %+v", res.Describe(), res.Fallback)
	}
}

// The hold (a stop, the round's budget, a random event) stops the run within a tick, and the
// engine drains what it already had.
func TestFastGemsStopsOnTheHold(t *testing.T) {
	r := newFastRig(t, 27*10)
	n := 0
	res := FastGems(r.d, r.q, r.ruby, r.pick, FastOptions{MaxLoads: 8, Hold: func() bool { n++; return n < 10 }})
	if res.Reason != "held" || res.Chunks > 10+fastLookahead {
		t.Fatalf("%s", res.Describe())
	}
	r.conserve(t)
}
