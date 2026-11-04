package bot

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// fakeTracer is a tick sampler that hands back what the test gives it.
type fakeTracer struct {
	samples            []runecraftplan.LegSample
	armed, taken, shut int
}

func (f *fakeTracer) Arm()                            { f.armed++ }
func (f *fakeTracer) Take() []runecraftplan.LegSample { f.taken++; return f.samples }
func (f *fakeTracer) Close()                          { f.shut++ }
func legTile(x, z int) tile.Tile                      { return tile.Tile{X: x, Z: z} }
func legSample(tick, x, z int) runecraftplan.LegSample {
	return runecraftplan.LegSample{Tick: tick, At: legTile(x, z), Energy: 3}
}

// legsWorld is a gateLegs with the places the cycle ends at preset (the altar four tiles north of the Enter tile, the portal at the
// Enter tile) and a fake sampler.
func legsFixture() (*gateLegs, *fakeTracer) {
	nat := runecraftplan.Nature
	altarStand := legTile(nat.Enter.X, nat.Enter.Z+4)
	ft := &fakeTracer{}
	g := &gateLegs{trace: ft, tries: map[runecraftplan.Stage]int{}, haveSpots: true}
	g.spots = runecraftplan.LegSpots{
		Inside:   nat.Inside,
		AtAltar:  func(t tile.Tile) bool { return t == altarStand },
		AtPortal: func(t tile.Tile) bool { return t == nat.Enter },
	}
	e := nat.Enter
	ft.samples = []runecraftplan.LegSample{
		legSample(50, 2867, 3020), legSample(51, 2867, 3020), legSample(52, 2867, 3020),
		legSample(53, e.X, e.Z), legSample(54, e.X, e.Z+1), legSample(55, e.X, e.Z+2), legSample(56, e.X, e.Z+3), legSample(57, e.X, e.Z+4),
		legSample(58, e.X, e.Z+4), legSample(59, e.X, e.Z+4), legSample(60, e.X, e.Z+4), legSample(61, e.X, e.Z+4), legSample(62, e.X, e.Z+4),
		legSample(63, e.X, e.Z+3), legSample(64, e.X, e.Z+2), legSample(65, e.X, e.Z+1), legSample(66, e.X, e.Z),
		legSample(67, nat.Exit.X, nat.Exit.Z),
	}
	return g, ft
}

func TestGateLegsLogsOneLineForACompleteCycle(t *testing.T) {
	g, ft := legsFixture()
	g.arm(50)
	if ft.armed != 1 || !g.open {
		t.Fatalf("armed %d times, open %v", ft.armed, g.open)
	}
	g.enter(runecraftplan.StageUseTalisman)
	g.noteTalisman(51, 1)
	g.enter(runecraftplan.StageCraft)
	g.noteCraft(58, 1)
	g.enter(runecraftplan.StagePortalOut)
	g.notePortal(1, 4)
	line := g.finish(runecraftplan.Nature)
	want := "legs: close 50, click 51 (+1), inside 53 (+2), altar 57 (+4; 4 tile(s); run off; energy 3%), craft 58 (+1), hold 62 (+4), " +
		"portal 66 (+4; 4 tile(s); run off; energy 3%), exit 67 (+1); 17 ticks close to exit; energy 3% -> 3%; clicks talisman 1 craft 1 portal 1 (held back 4 ticks)"
	if line != want {
		t.Fatalf("\n got %s\nwant %s", line, want)
	}
	if g.open || ft.taken != 1 {
		t.Errorf("open %v after the exit, sampler taken %d times", g.open, ft.taken)
	}
	// One line a cycle: a second finish with no new arm says nothing.
	if again := g.finish(runecraftplan.Nature); again != "" {
		t.Errorf("a second line for one cycle: %s", again)
	}
}

// A stage entered twice in a cycle is a retry and the line says so.
func TestGateLegsSaysARetriedStage(t *testing.T) {
	g, _ := legsFixture()
	g.arm(50)
	g.enter(runecraftplan.StageUseTalisman)
	g.noteTalisman(51, 1)
	g.enter(runecraftplan.StageCraft)
	g.enter(runecraftplan.StageCraft)
	g.noteCraft(58, 1)
	g.notePortal(1, 4)
	if line := g.finish(runecraftplan.Nature); !strings.Contains(line, "RETRIED") {
		t.Fatalf("%s", line)
	}
}

// A cycle that never armed (a round resumed inside the temple), or whose craft was never seen, has no line, and nothing here may panic
// on the nil gateLegs a levelling round carries.
func TestGateLegsCutShortCyclesAndNilAreQuiet(t *testing.T) {
	g, _ := legsFixture()
	if line := g.finish(runecraftplan.Nature); line != "" {
		t.Errorf("a line for a cycle that was never armed: %s", line)
	}
	g.arm(50)
	g.noteTalisman(51, 1)
	if line := g.finish(runecraftplan.Nature); line != "" { // no craft tick
		t.Errorf("a line for a cycle with no craft: %s", line)
	}
	var none *gateLegs
	none.arm(1)
	none.enter(runecraftplan.StageCraft)
	none.noteTalisman(1, 1)
	none.noteCraft(1, 1)
	none.notePortal(1, 1)
	none.close()
	if none.finish(runecraftplan.Nature) != "" {
		t.Error("a nil gateLegs made a line")
	}
}

func TestGateLegsCloseDropsTheSampler(t *testing.T) {
	g, ft := legsFixture()
	g.close()
	if ft.shut != 1 {
		t.Errorf("sampler closed %d times", ft.shut)
	}
	(&gateLegs{}).close() // no sampler: nothing to drop
}

// ---- the wiring, on the live world on the real data ----

// One full gate cycle through the real stage handlers on the real Nature temple: the trade's close arms the sampler, the talisman, the
// craft and the portal each report to it, and the exit logs the line with every mark present; the next cycle starts clean.
func TestTheGateCycleLogsItsLegsOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	nature := runecraftplan.Nature
	e.mu.Lock()
	e.xpT = levels.XPAtLevel(74) * 10
	e.setStat()
	e.mu.Unlock()
	d := r.b.deps(r.ctx)
	rw := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: r.b.set.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	rw.legs = newGateLegs(d)
	defer rw.legs.close()
	w := &masterNatureWorld{masterRuneWorld: rw, watch: runecraftrunner.NewGateWatcher(d), strangerSaid: map[string]time.Time{}, carrierSeen: time.Now()}
	w.stand, w.haveStand = runecraftplan.GateStandTile, true
	old, oldGate := masterWalkKaramja, masterGateStep
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		e.mu.Lock()
		if tile.Chebyshev(e.here, dest) > radius {
			e.moveTo(dest)
		}
		e.mu.Unlock()
		return true
	}
	t.Cleanup(func() { masterWalkKaramja, masterGateStep = old, oldGate })

	for cycle := 1; cycle <= 2; cycle++ {
		e.mu.Lock()
		e.moveTo(runecraftplan.GateStandTile)
		e.delayedUntil, e.teleportAt = 0, 0
		e.mu.Unlock()
		e.sim.Give(int32(nature.TalismanID), 1)
		e.sim.Give(runecraftplan.EssenceID, 25)
		time.Sleep(3 * liveTick)

		closeTick := d.Session.State().Tick()
		masterGateStep = func(*fleetrunner.Deps, *runecraftrunner.GateWatcher, runecraftrunner.GateOptions) runecraftrunner.GateResult {
			return runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 25,
				Ticks: runecraftrunner.GateTicks{Ask: closeTick - 5, Answer: closeTick - 5, Open: closeTick - 3, Close: closeTick}}
		}
		if res := w.Gate(); !res.OK || !w.legs.open || w.legs.in.Close != closeTick {
			t.Fatalf("cycle %d: the trade did not arm the sampler: %+v open %v close %d (want %d)\n%v", cycle, res, w.legs.open, w.legs.in.Close, closeTick, r.logs)
		}
		if res := w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}); !res.OK {
			t.Fatalf("cycle %d talisman: %+v\n%v", cycle, res, r.logs)
		}
		if res := w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}); !res.OK {
			t.Fatalf("cycle %d craft: %+v\n%v", cycle, res, r.logs)
		}
		if res := w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}); !res.OK {
			t.Fatalf("cycle %d portal: %+v\n%v", cycle, res, r.logs)
		}
		line := ""
		for _, l := range r.logs {
			if strings.Contains(l, "gate: legs: ") {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("cycle %d: no legs line\n%v", cycle, r.logs)
		}
		for _, want := range []string{"close " + strconv.Itoa(closeTick), ", click ", ", inside ", ", altar ", ", craft ", ", hold ", ", portal ", ", exit ", "ticks close to exit", "clicks talisman 1 craft 1 portal 1"} {
			if !strings.Contains(line, want) {
				t.Errorf("cycle %d: %q is not in %s", cycle, want, line)
			}
		}
		if strings.Contains(line, "RETRIED") || strings.Contains(line, "?") {
			t.Errorf("cycle %d: a clean cycle read as incomplete or retried: %s", cycle, line)
		}
		t.Log(line)
		r.logs = nil
	}
}
