package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
	"rsb0t-go/internal/scripts/tile"
)

// 18 Oct 2026 05:30: the Nature gate's craft was "not seen" three times in a row in a view that went silent for ten seconds and read
// backlogs after it, the round ended on the third, the crafter walked to the mule's post and the gate stood closed for 40 minutes
// (the nature rounds back off 30 minutes after a passing failure). A stage that failed in a lagging view is tried again from a current
// one and does not count toward ending the round; a stage that never succeeds still ends it.

// scriptedCraft is a gate whose crafts follow a script: "lag" fails in a lagging view, "fail" fails in a current one, "ok" crafts.
type scriptedCraft struct {
	*simGate
	seq []string
	n   int
}

func (s *scriptedCraft) Craft(st runecraftplan.Step) runeStepResult {
	step := "ok"
	if s.n < len(s.seq) {
		step = s.seq[s.n]
	}
	s.n++
	switch step {
	case "lag":
		s.total++
		s.calls[runecraftplan.GateCraft]++
		return runeFail("craft of 25 essence: 25 essence still in the pack 6s after the click (the view was lagging: the craft may not have been read yet)").withLag(true)
	case "fail":
		s.total++
		s.calls[runecraftplan.GateCraft]++
		return runeFail("craft of 25 essence: the game refused the craft")
	}
	return s.simGate.Craft(st)
}

func gateLagLoop(s natureSteps) (natureLoopResult, []string) {
	var logs []string
	res := runGateLoop(s, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }, 5000)
	return res, logs
}

func TestGateLoopDoesNotEndTheRoundOnCraftsLostToLag(t *testing.T) {
	g := newSimGate()
	g.endAfterTrades = 1
	s := &scriptedCraft{simGate: g, seq: []string{"lag", "lag", "lag", "lag", "lag"}}
	res, logs := gateLagLoop(s)
	if !res.Done || res.Transient || g.natures != 25 || g.crafts != 1 {
		t.Fatalf("five crafts lost to lag then one that worked: %+v, %d Natures, %d crafts\n%s", res, g.natures, g.crafts, strings.Join(logs, "\n"))
	}
	if g.calls[runecraftplan.GateCraft] < 6 {
		t.Errorf("the craft was tried %d times, want the five lost ones and the one that worked", g.calls[runecraftplan.GateCraft])
	}
	all := strings.Join(logs, "\n")
	if strings.Contains(all, "WARNING: gate craft failed") {
		t.Errorf("a failure out of a lagging view was logged as a counted failure:\n%s", all)
	}
	if !strings.Contains(all, "gate craft failed in a lagging view (5 of") {
		t.Errorf("the excused failures are not in the log:\n%s", all)
	}
}

// A view that never lifts does not keep a stage that never succeeds from ending the round: ten failures are excused, three count.
func TestGateLoopStillEndsOnALagThatNeverLifts(t *testing.T) {
	g := newSimGate()
	seq := make([]string, 100)
	for i := range seq {
		seq[i] = "lag"
	}
	s := &scriptedCraft{simGate: g, seq: seq}
	res, _ := gateLagLoop(s)
	if !res.Transient || !strings.Contains(res.Reason, "craft failed 3 times in a row") {
		t.Fatalf("%+v", res)
	}
	if got, want := g.calls[runecraftplan.GateCraft], natureLagFails+natureStepFails; got != want {
		t.Errorf("the craft was tried %d times, want %d (%d excused, then %d counted)", got, want, natureLagFails, natureStepFails)
	}
}

// A failure in a current view is counted as it always was, and the excused ones do not hide it.
func TestGateLoopStillCountsTheFailuresALagDidNotCause(t *testing.T) {
	g := newSimGate()
	s := &scriptedCraft{simGate: g, seq: []string{"fail", "lag", "fail", "lag", "fail"}}
	res, _ := gateLagLoop(s)
	if !res.Transient || !strings.Contains(res.Reason, "craft failed 3 times in a row") || g.calls[runecraftplan.GateCraft] != 5 {
		t.Fatalf("%+v after %d crafts", res, g.calls[runecraftplan.GateCraft])
	}
	// Two counted failures and a success are not a round's end.
	g = newSimGate()
	g.endAfterTrades = 1
	s = &scriptedCraft{simGate: g, seq: []string{"fail", "lag", "fail", "ok"}}
	if res, _ := gateLagLoop(s); !res.Done || res.Transient {
		t.Fatalf("%+v", res)
	}
}

// The levelling round's loop has the same rule.
type lagRune struct {
	*simRune
	lag int
}

func (l *lagRune) Craft(st runecraftplan.Step) runeStepResult {
	if l.calls[runecraftplan.StageCraft] < l.lag {
		l.total++
		l.calls[runecraftplan.StageCraft]++
		return runeFail("craft of 27 essence: 27 essence still in the pack 6s after the click").withLag(true)
	}
	return l.simRune.Craft(st)
}

func TestLevelLoopDoesNotEndTheRoundOnCraftsLostToLag(t *testing.T) {
	s := newSimRune().rich()
	s.target = 2
	l := &lagRune{simRune: s, lag: 6}
	var logs []string
	res := runLevelLoop(l, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }, 500)
	if !res.Done || res.Transient {
		t.Fatalf("%+v\n%s", res, strings.Join(logs, "\n"))
	}
	if s.calls[runecraftplan.StageCraft] < 7 || s.level < 2 {
		t.Errorf("%d craft calls, level %d", s.calls[runecraftplan.StageCraft], s.level)
	}
	l = &lagRune{simRune: newSimRune().rich(), lag: 1000}
	l.target = 2
	res = runLevelLoop(l, func(string, ...any) {}, 500)
	if !res.Transient || !strings.Contains(res.Reason, "craft failed 3 times in a row") {
		t.Fatalf("a lag that never lifts: %+v", res)
	}
	if got, want := l.calls[runecraftplan.StageCraft], masterRuneLagFails+masterRuneStepFails; got != want {
		t.Errorf("%d craft calls, want %d", got, want)
	}
}

func TestAFailureOnlyCarriesLagWhenItIsOne(t *testing.T) {
	if !runeFail("x").withLag(true).Lag {
		t.Error("a failure out of a lagging view does not carry it")
	}
	if runeFail("x").Lag || runeFail("x").withLag(false).Lag {
		t.Error("a failure in a current view carries lag")
	}
	if runeOK().withLag(true).Lag {
		t.Error("a stage that worked carries lag")
	}
}

// ---- the live world on the real data ----

// The real Nature craft stage in a temple, the altar answering seven seconds after the click and the view lagging for six of them (past
// the craft's two waits of three seconds): it was a failure before, and the second click it sent met an empty pack. Now the craft is
// seen when it lands, from one click.
func TestTheLiveGateCraftWaitsOutALateAnswerInALaggingViewOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	nature := runecraftplan.Nature
	e.mu.Lock()
	e.xpT = levels.XPAtLevel(74) * 10
	e.setStat()
	e.moveTo(nature.Enter)
	e.craftLateTicks = 280 // 7 s
	e.mu.Unlock()
	e.sim.Give(runecraftplan.EssenceID, 25)
	time.Sleep(3 * liveTick)
	d := r.b.deps(r.ctx)
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: r.b.set.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	st := e.sim.State()
	go func() {
		time.Sleep(400 * time.Millisecond)
		st.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
		time.Sleep(5600 * time.Millisecond)
		st.OverrideViewLag(&gamestate.ViewLag{})
	}()
	res := w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature})
	if !res.OK || res.Lag {
		t.Fatalf("%+v\n%v", res, r.logs)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.crafts) != 1 || e.altarClicks != 1 {
		t.Errorf("%d crafts from %d altar clicks: a late answer is not a reason to click again", len(e.crafts), e.altarClicks)
	}
}

// And when the answer does not come inside the stretched waits either, the failure says the view lagged, so the loop above excuses it.
func TestTheLiveGateCraftThatIsNotSeenInALaggingViewSaysSoOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	nature := runecraftplan.Nature
	e.mu.Lock()
	e.xpT = levels.XPAtLevel(74) * 10
	e.setStat()
	e.moveTo(nature.Enter)
	e.craftLateTicks = 2000 // never, in this test
	e.mu.Unlock()
	e.sim.Give(runecraftplan.EssenceID, 25)
	time.Sleep(3 * liveTick)
	d := r.b.deps(r.ctx)
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: r.b.set.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	st := e.sim.State()
	go func() {
		time.Sleep(400 * time.Millisecond)
		st.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
		time.Sleep(1500 * time.Millisecond)
		st.OverrideViewLag(&gamestate.ViewLag{})
	}()
	res := w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature})
	if res.OK || !res.Lag || !strings.Contains(res.Why, "still in the pack") {
		t.Fatalf("%+v", res)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.altarClicks != 1 {
		t.Errorf("%d altar clicks, want the one: the second is not sent for a late answer in a view that lags", e.altarClicks)
	}
}

// The talisman on the Nature ruins, the account never seen inside in a view that lags: the stage fails as a lagging view's, and the stand it
// was used from is neither counted against nor set aside (a stand is set aside after two uses that failed for no reason said).
func TestTheLiveTalismanThatIsNotSeenInALaggingViewCostsNeitherAStandNorARoundOnTheRealData(t *testing.T) {
	r, e := liveRig(t)
	nature := runecraftplan.Nature
	e.mu.Lock()
	e.moveTo(tile.Tile{X: 2866, Z: 3020})
	e.delayedUntil, e.teleportAt = 0, 0
	e.teleportLateTicks = 2000 // never, in this test
	e.mu.Unlock()
	e.sim.Give(int32(nature.TalismanID), 1)
	time.Sleep(3 * liveTick)
	d := r.b.deps(r.ctx)
	w := &masterRuneWorld{b: r.b, ctx: r.ctx, d: d, started: time.Now(), set: r.b.set.Rune, members: true,
		standTry: map[string]int{}, plans: map[string]*runecraftrunner.TemplePlan{}, tripStart: time.Now()}
	old := masterWalkKaramja
	masterWalkKaramja = func(_ *fleetrunner.Deps, dest tile.Tile, radius int, _ time.Duration) bool {
		e.mu.Lock()
		if tile.Chebyshev(e.here, dest) > radius {
			e.moveTo(dest)
		}
		e.mu.Unlock()
		return true
	}
	t.Cleanup(func() { masterWalkKaramja = old })
	st := e.sim.State()
	go func() {
		time.Sleep(1500 * time.Millisecond)
		st.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
		time.Sleep(1500 * time.Millisecond)
		st.OverrideViewLag(&gamestate.ViewLag{})
	}()
	res := w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature})
	if res.OK || !res.Lag || !strings.Contains(res.Why, "not seen inside") {
		t.Fatalf("%+v", res)
	}
	if w.standTry[nature.Key] != 0 || len(w.standBad[nature.Key]) != 0 {
		t.Errorf("the stand was counted against after a use that was not seen in a lagging view: tries %d, set aside %v", w.standTry[nature.Key], w.standBad[nature.Key])
	}
}
