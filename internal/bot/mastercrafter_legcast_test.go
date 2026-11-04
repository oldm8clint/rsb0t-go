package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// THE CASTS ON THE NATURE GATE'S LEGS ON THE LIVE WORLD ON THE REAL DATA (CX-1; runecraftplan/legcast.go, runecraftrunner/legcast.go). The
// gate's real stage handlers (ToRuins, Craft, PortalOut) run on the Nature temple's real geometry against the live-rig engine, which now
// plays the cast rules too (OPHELDT clears the click and locks the player, a movement click read in a cast's tick loses its route). The
// engine ticks at 100 ms and the ride's timer is scaled to it, but this rig is not aligned to its ticks the way the runner's tests are
// (runecraftrunner/legcast_test.go does the exact ticks): what is asserted here is the wiring -- what is cast and when it is not, what is
// booked, scored and logged, and that a failing cast never costs the leg or the direct ops their standing.

// castGate is the direct-op gate with the gate's alchemy armed: Magic 66, a Staff of fire worn, 4,000 Natures, the fare, and 300 noted bows
// of the dearest tier the config knows.
type castGate struct {
	*directGate
	bow runecraftrunner.AlchBowIDs
}

const castStack = 300

func newCastGate(t *testing.T, mode runecraftplan.LegCastMode, hold bool) *castGate {
	t.Helper()
	oldDur := liveTickDur
	liveTickDur = 100 * time.Millisecond
	t.Cleanup(func() { liveTickDur = oldDur })
	dg := newDirectGate(t, true)
	g := &castGate{directGate: dg}
	e, w, d := dg.e, dg.w, dg.d
	cfg := dg.r.ctx.Config
	bows := runecraftrunner.ResolveAlchBows(cfg)
	if len(bows) == 0 {
		t.Fatal("the config knows none of the gate's bows")
	}
	g.bow = bows[0]
	// The view is pinned current (ten ticks in three seconds is "catching up" to gamestate.ViewLag), and the ride counts the engine's ticks.
	e.sim.State().OverrideViewLag(&gamestate.ViewLag{})
	t.Cleanup(runecraftrunner.SetLegCastClock(100*time.Millisecond, 50*time.Millisecond, func(*fleetrunner.Deps) int {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.tick
	}))
	oldStaff := gateAlchFireStaff
	t.Cleanup(func() { gateAlchFireStaff = oldStaff })
	gateAlchFireStaff = func(*fleetrunner.Deps) bool { return true }
	e.sim.Give(g.bow.Noted, castStack)
	e.sim.Give(int32(runecraftrunner.NatureID), 4000)
	e.sim.Give(995, runecraftplan.GateFare)
	e.mu.Lock()
	e.magicXP = levels.XPAtLevel(66) + 100
	e.mu.Unlock()
	mindHoldStat(e.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(66)+100, 66)
	d.Travel = runecraftTravel(w.set, true)
	w.set.GateAlch, w.set.AlchBows, w.set.AlchHold, w.set.LegCasts = true, 2000, hold, mode
	w.alch = gateAlchState{on: true, bows: bows, arrived: true, burnFrom: time.Now(), lastStack: castStack, held: map[string]int{g.bow.Name: castStack}}
	w.legcasts = runecraftplan.NewLegCasts(mode)
	w.castPlan, w.castBook = w.legCastPlan, w.legCastBook
	return g
}

// start is directGate.start with the 24 essence a gate that holds its bows takes.
func (g *castGate) start() {
	g.t.Helper()
	nature := runecraftplan.Nature
	e := g.e
	e.mu.Lock()
	e.moveTo(runecraftplan.GateStandTile)
	e.delayedUntil, e.teleportAt = 0, 0
	e.way, e.loc = nil, 0
	e.mu.Unlock()
	if e.packCount(int32(nature.TalismanID)) == 0 {
		e.sim.Give(int32(nature.TalismanID), 1)
	}
	e.sim.Give(runecraftplan.EssenceID, 24)
	time.Sleep(3 * liveTickDur)
	closeTick := g.d.Session.State().Tick()
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 24,
			Ticks: runecraftrunner.GateTicks{Ask: closeTick - 5, Answer: closeTick - 5, Open: closeTick - 3, Close: closeTick}}
	}
	if res := g.w.Gate(); !res.OK || !g.w.legs.open {
		g.t.Fatalf("the trade did not arm the cycle: %+v\n%v", res, g.r.logs)
	}
}

// cycle is one gate cycle: the trade closed, the talisman, the craft, the portal. It returns the cycle's legs line and its cast line.
func (g *castGate) cycle() (legs, cast string) {
	g.t.Helper()
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.start()
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
	g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	for _, l := range g.r.logs {
		if strings.Contains(l, "gate: cast on leg: ") {
			cast = l
		}
	}
	return g.legsLine(), cast
}

func (g *castGate) bowsLeft() int { return g.e.packCount(g.bow.Noted) }

// A whole cycle with the casts on: the altar leg and the portal leg each carry a Low Level Alchemy on the dearest bow, the plain walker is
// never asked, both casts are booked (the coins at Low's 0.4 of the bow's cost, 31 experience each), and the cycle says so in its legs line
// and in a line of its own. Some cycles of the rig slip a tick or lose a walk to the unaligned clock, so the first cycle of six that kept both
// casts is the one read.
func TestTheNatureCycleCastsOnBothLegsOnTheRealData(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsLow2, true)
	w, e := g.w, g.e
	var failures []string
	for cycle := 1; cycle <= 6; cycle++ {
		before := g.bowsLeft()
		legs, cast := g.cycle()
		clean := strings.Contains(legs, "; leg casts: altar low landed, portal low landed") && strings.Contains(cast, "altar low ("+g.bow.Name+")") &&
			strings.Contains(cast, "portal low ("+g.bow.Name+")") && !strings.Contains(cast, "THROWN AWAY")
		if !clean {
			failures = append(failures, fmt.Sprintf("cycle %d: %s | %s", cycle, legs, cast))
			continue
		}
		if g.bowsLeft() != before-2 {
			t.Errorf("the pack held %d bows before the cycle and %d after: two casts take two", before, g.bowsLeft())
		}
		if g.walks != 0 {
			t.Errorf("the plain walker was asked %d times inside the temple", g.walks)
		}
		pay := runecraftplan.LowLegSpell.Pays(costOf(g.bow.Name))
		e.mu.Lock()
		casts, crafts, exits := e.casts, len(e.craftTicks), len(e.exitTicks)
		e.mu.Unlock()
		a := w.alch
		if a.leg.casts != casts || a.leg.xp != 31*casts || a.leg.coins != pay*casts || a.coins != pay*casts {
			t.Errorf("booked %d casts, %d xp, %d coins (round %d): the engine cast %d at %d coins each", a.leg.casts, a.leg.xp, a.leg.coins, a.coins, casts, pay)
		}
		if crafts != cycle || exits != cycle {
			t.Errorf("crafts %d exits %d after %d cycle(s)", crafts, exits, cycle)
		}
		if pace := w.alchPace(); !strings.Contains(pace, fmt.Sprintf("alchemy %d cast(s) (%d on the temple legs)", casts, casts)) {
			t.Errorf("the pace line says %q", pace)
		}
		if got := w.alchLine(); !strings.Contains(got, fmt.Sprintf("%d High Level Alchemy cast(s)", casts)) {
			t.Errorf("the alchemy line says %q", got)
		}
		sum := w.direct.Summary()
		if !strings.Contains(sum, fmt.Sprintf("altar %d direct, 0 failed", cycle)) || !strings.Contains(sum, fmt.Sprintf("portal %d direct, 0 failed", cycle)) {
			t.Errorf("the direct ops were charged: %s", sum)
		}
		if s := w.legcasts.Summary(); !strings.Contains(s, "leg casts (low2)") {
			t.Errorf("summary %q", s)
		}
		t.Log(legs)
		t.Log(cast)
		return
	}
	t.Fatalf("no cycle of six cast cleanly on both legs:\n%s", strings.Join(failures, "\n"))
}

// With the mode off nothing is cast and the cycle is the direct-op cycle of before: no tag in the legs line, no cast line.
func TestTheNatureCycleWithLegCastsOffCastsNothing(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsOff, true)
	legs, cast := g.cycle()
	g.e.mu.Lock()
	casts := g.e.casts
	g.e.mu.Unlock()
	if casts != 0 || cast != "" || strings.Contains(legs, "leg casts") || g.bowsLeft() != castStack || !strings.HasSuffix(legs, "; direct ops: altar direct, portal direct") {
		t.Fatalf("casts %d, cast line %q, legs %s, bows %d", casts, cast, legs, g.bowsLeft())
	}
	if g.w.alchPace() != "" && strings.Contains(g.w.alchPace(), "temple legs") {
		t.Errorf("pace %q", g.w.alchPace())
	}
}

// The casts need the bows held through the trades: with craftRuneAlchHold off nothing is cast, and the log says why once a round for each
// leg, not once a cycle.
func TestLegCastsWithoutTheHoldCastNothingAndSaySoOnce(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsLow2, false)
	var said int
	for cycle := 0; cycle < 2; cycle++ {
		g.cycle()
		for _, l := range g.r.logs {
			if strings.Contains(l, "walked without a cast: the bows are not held through the trades") {
				said++
			}
		}
	}
	g.e.mu.Lock()
	casts := g.e.casts
	g.e.mu.Unlock()
	if casts != 0 || g.bowsLeft() != castStack {
		t.Errorf("casts %d, bows %d", casts, g.bowsLeft())
	}
	// Both legs say it once in the first cycle; the second says nothing (the logs are cleared each cycle, so said counts both cycles).
	if said != 2 {
		t.Errorf("the reason was logged %d times over two cycles, want once for each of the two legs", said)
	}
}

// A cast the game refuses (no Nature rune) clears the click all the same: the leg is clicked again and completes, the direct op is not
// charged for it, and two refusals in a row stand the leg's casts down.
func TestARefusedCastCostsTheLegNothingAndStandsTheCastsDown(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsLow2, true)
	g.e.mu.Lock()
	g.e.refuseCasts = true
	g.e.mu.Unlock()
	var stood string
	for cycle := 1; cycle <= 3; cycle++ {
		legs, _ := g.cycle()
		// The first two cycles cast and are refused; the third is in the back-off the second one started, and casts nothing.
		if failed := strings.Contains(legs, "leg casts: altar low failed, portal low failed"); failed != (cycle <= 2) {
			t.Errorf("cycle %d: legs %s", cycle, legs)
		}
		for _, l := range g.r.logs {
			if strings.Contains(l, "walks without a cast for the next 15 cycle") {
				stood = l
			}
		}
	}
	e := g.e
	e.mu.Lock()
	crafts, exits, casts := len(e.craftTicks), len(e.exitTicks), e.casts
	e.mu.Unlock()
	if crafts != 3 || exits != 3 || casts != 0 || g.bowsLeft() != castStack {
		t.Errorf("crafts %d exits %d casts %d bows %d: a refused cast must not cost a leg", crafts, exits, casts, g.bowsLeft())
	}
	if stood == "" {
		t.Errorf("two refusals in a row did not stand the casts down")
	}
	if sum := g.w.direct.Summary(); !strings.Contains(sum, "altar 3 direct, 0 failed") || !strings.Contains(sum, "portal 3 direct, 0 failed") {
		t.Errorf("a refused cast was charged to the direct ops: %s", sum)
	}
	if g.w.legcasts.GivenUp(runecraftplan.LegAltar) {
		t.Error("given up after one stand-down")
	}
}

// A server that ticks slowly (three ticks in three seconds), a Magic level a cast away from the next, and a dry stack are each a leg walked
// as the direct op of before, with the reason said.
func TestLegCastsAreNotPlannedWhereTheTimingOrTheKitCannotHoldThem(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsLow2, true)
	w, e := g.w, g.e
	say := func() string { return strings.Join(g.r.logs, " | ") }
	if lc := w.castPlan(runecraftplan.LegAltar, 4); lc == nil {
		t.Fatalf("a healthy view with the kit was not cast on: %s", say())
	}
	// The plan above took a cycle of the score's back-off bookkeeping and a held snapshot; nothing was sent.
	e.sim.State().OverrideViewLag(&gamestate.ViewLag{InWindow: 3})
	if lc := w.castPlan(runecraftplan.LegAltar, 4); lc != nil || !strings.Contains(say(), "ticking slowly") {
		t.Errorf("a slow server: %v, %s", lc, say())
	}
	e.sim.State().OverrideViewLag(&gamestate.ViewLag{InWindow: 5})
	mindHoldStat(e.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(67)-20, 66)
	if lc := w.castPlan(runecraftplan.LegPortal, 4); lc != nil || !strings.Contains(say(), "level Magic up") {
		t.Errorf("a cast from the next level: %v, %s", lc, say())
	}
	mindHoldStat(e.sim.State(), fleetplan.MagicStat, levels.XPAtLevel(66)+100, 66)
	if lc := w.castPlan(runecraftplan.LegPortal, 2); lc != nil || !strings.Contains(say(), "too few") {
		t.Errorf("a two-step leg: %v, %s", lc, say())
	}
	e.sim.Remove(g.bow.Noted, castStack)
	if lc := w.castPlan(runecraftplan.LegPortal, 4); lc != nil || !strings.Contains(say(), "no bows held") {
		t.Errorf("a dry stack: %v, %s", lc, say())
	}
	e.mu.Lock()
	casts := e.casts
	e.mu.Unlock()
	if casts != 0 {
		t.Errorf("%d casts were sent by a plan", casts)
	}
}

// The startup line says what the setting will do, and says it plainly when there is nothing to cast with.
func TestLegCastStartupSaysWhatWillBeCast(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsHigh1, true)
	g.r.logs = nil
	g.w.legCastStartup()
	if got := strings.Join(g.r.logs, "\n"); !strings.Contains(got, "leg casts: ON") || !strings.Contains(got, "altar leg none, portal leg High Level Alchemy") {
		t.Errorf("high1: %s", got)
	}
	g.w.set.AlchHold = false
	g.r.logs = nil
	g.w.legCastStartup()
	if got := strings.Join(g.r.logs, "\n"); !strings.Contains(got, "nothing to cast with") || !strings.Contains(got, "craftRuneAlchHold") {
		t.Errorf("no hold: %s", got)
	}
	g.w.legcasts = runecraftplan.NewLegCasts(runecraftplan.LegCastsOff)
	g.r.logs = nil
	g.w.legCastStartup()
	if got := strings.Join(g.r.logs, "\n"); !strings.Contains(got, "leg casts: OFF") {
		t.Errorf("off: %s", got)
	}
}

// What a landed cast books: the experience and coins at the spell's rate, the bow off the held snapshot (so the idle casts' bookkeeping
// does not pay it again at High Level's rate), and the clock the idle casts keep their lock by.
func TestABookedLegCastIsNotPaidAgainByTheIdleCasts(t *testing.T) {
	g := newCastGate(t, runecraftplan.LegCastsLow2, true)
	w := g.w
	g.e.sim.Remove(g.bow.Noted, 1) // the engine took the bow
	rec := runecraftplan.LegCastRecord{Leg: runecraftplan.LegAltar, Spell: runecraftplan.LowLegSpell, Bow: g.bow.Name, Tried: true, Landed: 5}
	w.legCastBook(runecraftplan.LegAltar, rec)
	pay := runecraftplan.LowLegSpell.Pays(costOf(g.bow.Name))
	if w.alch.leg.casts != 1 || w.alch.leg.xp != 31 || w.alch.coins != pay || w.alch.held[g.bow.Name] != castStack-1 || w.alch.castAt.IsZero() {
		t.Fatalf("booked %+v, coins %d, held %v", w.alch.leg, w.alch.coins, w.alch.held)
	}
	w.noteHeld() // the idle casts' look at the pack: the bow is gone, and was paid for
	if w.alch.coins != pay {
		t.Errorf("the idle look paid the bow again: %d coins, want %d", w.alch.coins, pay)
	}
	if w.alchLocked() == false {
		t.Error("the idle casts' clock does not know about the cast")
	}
}
