package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// THE DIRECT-OP LEGS ON THE LIVE WORLD ON THE REAL DATA (CR-1; runecraftplan/direct.go). The gate's real stage handlers (ToRuins, Craft,
// PortalOut) run on the Nature temple's real geometry against the live-rig engine, which now also plays the movement a direct op depends
// on: a MOVE_OPCLICK queues a walk, an OPLOC waits for it and runs the tick after the last step, the engine never walks to a loc, and a
// click with no walk behind it from a tile that does not reach the loc is "I can't reach that!".

type directGate struct {
	t     *testing.T
	r     *masterRig
	e     *liveEngine
	w     *masterNatureWorld
	d     *fleetrunner.Deps
	walks int // plain walks inside the temple (the walker's seam)
}

func newDirectGate(t *testing.T, direct bool) *directGate {
	t.Helper()
	r, e := liveRig(t)
	e.mu.Lock()
	e.xpT = levels.XPAtLevel(74) * 10
	e.setStat()
	e.pk = r.ctx.Pack
	e.mu.Unlock()
	d := r.b.deps(r.ctx)
	rw := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: r.b.set.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	rw.legs = newGateLegs(d)
	t.Cleanup(rw.legs.close)
	rw.direct = runecraftplan.NewDirectOps(direct)
	w := &masterNatureWorld{masterRuneWorld: rw, watch: runecraftrunner.NewGateWatcher(d), strangerSaid: map[string]time.Time{}, carrierSeen: time.Now()}
	w.stand, w.haveStand = runecraftplan.GateStandTile, true
	g := &directGate{t: t, r: r, e: e, w: w, d: d}
	oldWalk, oldKaramja, oldGate := masterWalkTo, masterWalkKaramja, masterGateStep
	masterWalkTo = func(dd *fleetrunner.Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
		g.walks++
		return oldWalk(dd, dest, radius, timeout)
	}
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		e.mu.Lock()
		if tile.Chebyshev(e.here, dest) > radius {
			e.moveTo(dest)
		}
		e.mu.Unlock()
		return true
	}
	t.Cleanup(func() { masterWalkTo, masterWalkKaramja, masterGateStep = oldWalk, oldKaramja, oldGate })
	return g
}

// start puts the account at the gate with a talisman and a load, and arms the cycle as a closed trade does.
func (g *directGate) start() {
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
	e.sim.Give(runecraftplan.EssenceID, 25)
	time.Sleep(3 * liveTick)
	closeTick := g.d.Session.State().Tick()
	masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 25,
			Ticks: runecraftrunner.GateTicks{Ask: closeTick - 5, Answer: closeTick - 5, Open: closeTick - 3, Close: closeTick}}
	}
	if res := g.w.Gate(); !res.OK || !g.w.legs.open {
		g.t.Fatalf("the trade did not arm the cycle: %+v\n%v", res, g.r.logs)
	}
}

func (g *directGate) step(name string, res runeStepResult) {
	g.t.Helper()
	if !res.OK {
		g.t.Fatalf("%s: %+v\n%s", name, res, strings.Join(g.r.logs, "\n"))
	}
}

// cycle runs one gate cycle: the trade closed, the talisman, the craft, the portal. It returns the cycle's legs line.
func (g *directGate) cycle() string {
	g.t.Helper()
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.start()
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
	g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	return g.legsLine()
}

func (g *directGate) legsLine() string {
	line := ""
	for _, l := range g.r.logs {
		if strings.Contains(l, "gate: legs: ") {
			line = l
		}
	}
	if line == "" {
		g.t.Fatalf("no legs line:\n%s", strings.Join(g.r.logs, "\n"))
	}
	return line
}

func (g *directGate) logged(sub string) bool {
	for _, l := range g.r.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// messageCounts is how many of the messages the engine read carried a walk in front of a loc click, how many MOVE_OPCLICK and how many
// OPLOC1 it read in all.
func (g *directGate) messageCounts() (walkAndClick, walks, clicks int) {
	g.e.mu.Lock()
	defer g.e.mu.Unlock()
	for _, ops := range g.e.messages {
		var seq []int
		for _, op := range ops {
			if op == clientprot.MOVE_OPCLICK || op == clientprot.OPLOC1 {
				seq = append(seq, op)
			}
		}
		switch len(seq) {
		case 2:
			if seq[0] == clientprot.MOVE_OPCLICK && seq[1] == clientprot.OPLOC1 {
				walkAndClick++
			}
		}
		for _, op := range seq {
			if op == clientprot.MOVE_OPCLICK {
				walks++
			} else {
				clicks++
			}
		}
	}
	return walkAndClick, walks, clicks
}

// Three whole cycles with the direct ops on: the altar and the portal each go out as ONE write of walk and click, the plain walker is never
// asked inside the temple, the engine runs the altar the tick after the plan's last step and the portal likewise from the first accepted
// read, nothing is dropped, and the cycle's line says so.
func TestTheNatureCycleRunsOnDirectOps(t *testing.T) {
	g := newDirectGate(t, true)
	var plan *runecraftrunner.TemplePlan
	for cycle := 1; cycle <= 3; cycle++ {
		line := g.cycle()
		if plan == nil {
			plan = g.w.plans[runecraftplan.KeyNature]
			if plan == nil {
				t.Fatal("no temple plan")
			}
		}
		if !strings.HasSuffix(line, "; direct ops: altar direct, portal direct") || strings.Contains(line, "RETRIED") || strings.Contains(line, "?") {
			t.Fatalf("cycle %d: %s", cycle, line)
		}
		if !strings.Contains(line, "clicks talisman 1 craft 1 portal 1") {
			t.Errorf("cycle %d clicks: %s", cycle, line)
		}
		t.Log(line)
	}
	if g.walks != 0 {
		t.Errorf("the plain walker was asked %d times inside the temple", g.walks)
	}
	pairs, walks, clicks := g.messageCounts()
	// Per cycle: the talisman is an OPLOCU, so the loc clicks are the altar's and the portal's, each with a walk in front of it.
	if pairs != 6 || walks != 6 || clicks != 6 {
		t.Errorf("walk-and-click messages %d, walks %d, loc clicks %d: want 6 of each over three cycles", pairs, walks, clicks)
	}
	e := g.e
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.craftTicks) != 3 || len(e.exitTicks) != 3 || e.dropped != 0 || e.cantReaches != 0 {
		t.Fatalf("crafts %v exits %v dropped %d can't reach %d", e.craftTicks, e.exitTicks, e.dropped, e.cantReaches)
	}
	// readTicks: the altar click and the portal click of each cycle, in order. The engine runs the altar plan.ToAltar ticks after its read
	// and the portal plan.ToPortal ticks after its read, which is no earlier than craft+5.
	if len(e.readTicks) != 6 {
		t.Fatalf("loc reads %v", e.readTicks)
	}
	for i := 0; i < 3; i++ {
		altarRead, portalRead := e.readTicks[2*i], e.readTicks[2*i+1]
		if got := e.craftTicks[i] - altarRead; got != plan.ToAltar {
			t.Errorf("cycle %d: crafted %d ticks after the click was read, want the plan's %d steps", i+1, got, plan.ToAltar)
		}
		if portalRead < e.craftTicks[i]+runecraftplan.HoldTicks+1 || e.exitTicks[i]-portalRead != plan.ToPortal {
			t.Errorf("cycle %d: portal read in tick %d (craft %d), out in tick %d: want no read in the hold and %d steps", i+1, portalRead, e.craftTicks[i], e.exitTicks[i], plan.ToPortal)
		}
	}
	if s := g.w.direct.Summary(); !strings.Contains(s, "altar 3 direct, 0 failed, 0 not seen, 0 plain") || !strings.Contains(s, "portal 3 direct, 0 failed, 0 not seen, 0 plain") {
		t.Errorf("summary %s", s)
	}
}

// With craftRuneDirectOps off the legs are walked, then clicked, as they were: no MOVE_OPCLICK is ever sent, the walker is asked for both
// legs, and the line says nothing about direct ops.
func TestTheNatureCycleWithDirectOpsOffIsTheWalkThenClick(t *testing.T) {
	g := newDirectGate(t, false)
	line := g.cycle()
	if strings.Contains(line, "direct ops") || strings.Contains(line, "RETRIED") {
		t.Errorf("%s", line)
	}
	if _, walks, _ := g.messageCounts(); walks != 0 {
		t.Errorf("%d MOVE_OPCLICK were sent with the direct ops off", walks)
	}
	if g.walks != 2 {
		t.Errorf("the walker was asked %d times inside the temple, want the altar's and the portal's", g.walks)
	}
	if g.w.direct.Tried(runecraftplan.LegAltar) != 0 || g.w.direct.Tried(runecraftplan.LegPortal) != 0 {
		t.Error("a direct op was tried")
	}
}

// ONE failure sends the altar leg back to walk-then-click -- in the same stage, essence still in the pack -- and for the next ten cycles;
// the portal leg, which did not fail, stays direct; then the altar tries again and says it worked.
func TestAFailedAltarLegWalksTheOldWayAndThenTriesAgain(t *testing.T) {
	g := newDirectGate(t, true)
	e := g.e
	// Cycle 1: the engine drops the walk half (as a hold would), so the click alone is refused.
	e.mu.Lock()
	e.ignoreMoves = true
	e.mu.Unlock()
	// The portal leg needs its walk: let it through by releasing the quirk once the craft is done.
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.start()
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
	e.mu.Lock()
	e.ignoreMoves = false
	e.mu.Unlock()
	g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	line := g.legsLine()
	for _, want := range []string{"clicks talisman 1 craft 1 portal 1", "RETRIED, so the legs include the retry", "; direct ops: altar fell back, portal direct"} {
		if !strings.Contains(line, want) {
			t.Errorf("cycle 1 lacks %q: %s", want, line)
		}
	}
	if !g.logged("direct op: the altar leg failed") || !g.logged("walking to the stand and clicking instead, and for the next 10 cycle(s)") {
		t.Errorf("the failure was not logged:\n%s", strings.Join(g.r.logs, "\n"))
	}
	if g.walks != 1 {
		t.Errorf("the plain walker was asked %d times in the failed cycle, want 1 (the altar's, after the fallback)", g.walks)
	}
	e.mu.Lock()
	if e.cantReaches != 1 || len(e.craftTicks) != 1 {
		t.Errorf("can't reach %d, crafts %d", e.cantReaches, len(e.craftTicks))
	}
	e.mu.Unlock()

	// Cycles 2 to 11: the altar leg is walked the old way (the walker is asked once a cycle for it), the portal leg stays direct.
	for cycle := 2; cycle <= 1+runecraftplan.DirectBackoff; cycle++ {
		walks0 := g.walks
		_, moves0, _ := g.messageCounts()
		line := g.cycle()
		if !strings.HasSuffix(line, "; direct ops: altar plain, portal direct") || strings.Contains(line, "RETRIED") {
			t.Fatalf("cycle %d: %s", cycle, line)
		}
		if _, moves, _ := g.messageCounts(); moves-moves0 != 1 {
			t.Errorf("cycle %d: %d walks were sent, want the portal leg's only", cycle, moves-moves0)
		}
		if g.walks-walks0 != 1 {
			t.Errorf("cycle %d: the plain walker was asked %d times, want the altar's", cycle, g.walks-walks0)
		}
	}
	// Cycle 12: the back-off is over and the altar leg is tried again.
	line = g.cycle()
	if !strings.HasSuffix(line, "; direct ops: altar direct, portal direct") {
		t.Fatalf("cycle 12: %s", line)
	}
	if !g.logged("direct op: the altar leg worked again") {
		t.Errorf("the recovery was not logged:\n%s", strings.Join(g.r.logs, "\n"))
	}
	if g.w.direct.GivenUp(runecraftplan.LegAltar) {
		t.Error("the altar leg was given up after one failure")
	}
	t.Log(g.w.direct.Summary())
}

// The portal leg falls back the same way, inside one PortalOut call: the click without its walk is refused, the plain approach and click
// follow, the account is out, and the line says the leg fell back.
func TestAFailedPortalLegWalksTheOldWay(t *testing.T) {
	g := newDirectGate(t, true)
	e := g.e
	nature := runecraftplan.Nature
	g.start()
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
	walks0 := g.walks
	e.mu.Lock()
	e.ignoreMoves = true
	e.mu.Unlock()
	g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	line := g.legsLine()
	if !strings.HasSuffix(line, "; direct ops: altar direct, portal fell back") || !strings.Contains(line, "clicks talisman 1 craft 1 portal 2") || !strings.Contains(line, "RETRIED") {
		t.Errorf("%s", line)
	}
	if !g.logged("direct op: the portal leg failed (the engine said: I can't reach that)") {
		t.Errorf("the failure was not logged:\n%s", strings.Join(g.r.logs, "\n"))
	}
	if g.walks-walks0 != 1 {
		t.Errorf("the plain approach ran %d times, want 1", g.walks-walks0)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.exitTicks) != 1 || nature.Inside(e.here) {
		t.Errorf("exits %v, standing at %v", e.exitTicks, e.here)
	}
}

// A view that went silent a few seconds ago is not one to read a verdict out of: the cycle is walked and clicked the old way, and no
// direct attempt is counted for it.
func TestAShakyViewWalksTheOldWay(t *testing.T) {
	g := newDirectGate(t, true)
	g.e.sim.State().OverrideViewLag(&gamestate.ViewLag{Silence: 8 * time.Second, SilenceEnded: time.Now().Add(-10 * time.Second)})
	line := g.cycle()
	if strings.Contains(line, "direct ops") {
		t.Errorf("%s", line)
	}
	if _, walks, _ := g.messageCounts(); walks != 0 {
		t.Errorf("%d MOVE_OPCLICK were sent in a shaky view", walks)
	}
	if g.walks != 2 || g.w.direct.Tried(runecraftplan.LegAltar) != 0 || g.w.direct.Tried(runecraftplan.LegPortal) != 0 {
		t.Errorf("walks %d, tried %d and %d", g.walks, g.w.direct.Tried(runecraftplan.LegAltar), g.w.direct.Tried(runecraftplan.LegPortal))
	}
}
