package bot

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// THE ONE-WRITE ACCEPT ON THE LIVE WORLD ON THE REAL DATA (CR-2; runecraftplan/together.go, runecraftrunner/together.go). The real stage
// handlers (Gate, ToRuins, Craft, PortalOut) run on the Nature temple's real geometry against the live-rig engine; the trade itself is the
// stub of every gate test here (masterGateStep), which does what GateStep does when the carrier's accept is on the screen: it asks the plan
// and, if the plan says yes, WRITES the talisman use to the engine (the accept's own effects are the result the stub returns). What these
// tests prove is the wiring: when the plan says yes, what Gate does with a result that carries a written talisman, what ToRuins does with it
// (waits, never writes it again unless it did nothing), and the strikes, back-off and give-up of a cycle that lost.
// runecraftrunner/together_test.go proves the handshake itself against the engine's rules for a confirm screen.

func newTogetherGate(t *testing.T) *directGate {
	t.Helper()
	g := newDirectGate(t, true)
	g.w.together = runecraftplan.NewTogetherOps(true)
	return g
}

// place puts the account on the gate's stand with the talisman, and with the load a completed trade would have brought when withEssence.
func (g *directGate) place(withEssence bool) {
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
	if n := e.packCount(runecraftplan.EssenceID); n > 0 {
		e.sim.Remove(runecraftplan.EssenceID, n)
	}
	if withEssence {
		e.sim.Give(runecraftplan.EssenceID, 25)
	}
	time.Sleep(3 * liveTick)
}

// writeTalisman is the message half of a one-write accept: the use of the talisman on the ruins, written to the engine as GateStep writes it.
func (g *directGate) writeTalisman() *runecraftrunner.TalismanSent {
	g.t.Helper()
	nature := runecraftplan.Nature
	ruins, ok := runecraftrunner.FindRuins(g.d, nature)
	if !ok {
		g.t.Fatal("no ruins")
	}
	slot := -1
	for _, it := range g.d.Session.State().Inventory(runecraftrunner.PlayerPack).NonEmpty() {
		if int(it.ID) == nature.TalismanID {
			slot = it.Slot
		}
	}
	if slot < 0 {
		g.t.Fatal("no talisman in the pack")
	}
	st := g.d.Session.State()
	sent := &runecraftrunner.TalismanSent{Tick: st.Tick(), At: time.Now()}
	if err := actions.OpLocU(g.d.Session, ruins.X, ruins.Z, ruins.ID, int32(nature.TalismanID), slot, runecraftrunner.PlayerPack); err != nil {
		g.t.Fatal(err)
	}
	return sent
}

// gateWith runs Gate() with the trade stub answering for GateStep: the plan is asked (as the handshake does on a matching confirm screen)
// and result builds what the handshake came to.
func (g *directGate) gateWith(result func(opts runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult) runeStepResult {
	g.t.Helper()
	masterGateStep = func(_ *fleetrunner.Deps, _ *runecraftrunner.GateWatcher, o runecraftrunner.GateOptions) runecraftrunner.GateResult {
		plan := false
		if o.Together != nil && o.Together.Plan != nil {
			plan = o.Together.Plan()
		}
		return result(o, plan)
	}
	return g.w.Gate()
}

// useMessages is how many messages the engine read that carried a use of an item on a loc (the talisman).
func (g *directGate) useMessages() int {
	g.e.mu.Lock()
	defer g.e.mu.Unlock()
	n := 0
	for _, ops := range g.e.messages {
		for _, op := range ops {
			if op == clientprot.OPLOCU {
				n++
			}
		}
	}
	return n
}

func (g *directGate) logCount(sub string) int {
	n := 0
	for _, l := range g.r.logs {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

func received(closeTick int, sent *runecraftrunner.TalismanSent) runecraftrunner.GateResult {
	return runecraftrunner.GateResult{Kind: runecraftrunner.GateReceived, Partner: "Acct 1", Essence: 25,
		Ticks:    runecraftrunner.GateTicks{Ask: closeTick - 9, Answer: closeTick - 8, Open: max(1, closeTick-6), Close: closeTick},
		Together: runecraftrunner.TogetherInfo{Planned: true, Sent: sent != nil, Talisman: sent, ConfirmTick: closeTick - 4, OtherTick: closeTick - 2}}
}

// A whole cycle with the one-write accept: the plan says yes on the stand, the trade's result carries the written talisman, Gate keeps it,
// ToRuins WAITS for it (the engine reads ONE use in the whole cycle), the account is inside, and the cycle's line says the click went with the
// accept, and measures the legs after it from the close.
func TestAOneWriteCycleIsWaitedForNotWrittenAgain(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	res := g.gateWith(func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		if o.Together == nil || !plan {
			t.Fatalf("the gate did not offer the one-write accept on the stand (option %v, plan %v)", o.Together != nil, plan)
		}
		closeTick := g.d.Session.State().Tick()
		return received(closeTick, g.writeTalisman())
	})
	if !res.OK || g.w.talisman == nil || !g.w.legs.open {
		t.Fatalf("Gate: %+v, talisman kept %v, cycle armed %v\n%v", res, g.w.talisman != nil, g.w.legs.open, g.r.logs)
	}
	if !g.logged("; one write: accept and talisman in tick") {
		t.Errorf("the trade line does not say the talisman went with the accept:\n%s", strings.Join(g.r.logs, "\n"))
	}
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
	g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	if g.w.talisman != nil {
		t.Error("the written talisman was not consumed")
	}
	if n := g.useMessages(); n != 1 {
		t.Errorf("the engine read %d uses of the talisman in the cycle, want the one that was written", n)
	}
	if g.w.together.Tried() != 1 || strings.Contains(g.w.together.Summary(), "1 failed") {
		t.Errorf("the cycle was not scored as a success: %s", g.w.together.Summary())
	}
	line := g.legsLine()
	for _, want := range []string{"with the accept)", "clicks talisman 1 craft 1 portal 1", "; one write: the confirm accept and the talisman", " ticks open to inside"} {
		if !strings.Contains(line, want) {
			t.Errorf("the legs line lacks %q:\n%s", want, line)
		}
	}
	t.Log(line)
}

// A written use that did nothing (the engine dropped it) is not waited on for ever: ToRuins writes the use once more after the wait,
// the account lands, and the cycle counts as a failure of the one-write accept (ten plain cycles).
func TestAWrittenUseThatDidNothingIsUsedOnceMoreAndBacksOff(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	res := g.gateWith(func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		closeTick := g.d.Session.State().Tick()
		// Written, as far as the handshake knows; the engine never read it.
		return received(closeTick, &runecraftrunner.TalismanSent{Tick: closeTick - 1, At: time.Now()})
	})
	if !res.OK || g.w.talisman == nil {
		t.Fatalf("Gate: %+v", res)
	}
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	if !nature.Inside(g.e.here) || g.useMessages() != 1 {
		t.Fatalf("inside %v after %d uses read: the second use must land the account", nature.Inside(g.e.here), g.useMessages())
	}
	if !g.logged("one-write accept: failed (the use written with the accept did nothing") || !strings.Contains(g.w.together.Summary(), "1 failed") {
		t.Errorf("the failure was not scored:\n%s\n%s", strings.Join(g.r.logs, "\n"), g.w.together.Summary())
	}
	for i := 1; i <= runecraftplan.TogetherBackoff; i++ {
		if g.w.together.Try() {
			t.Fatalf("plain cycle %d of %d was tried", i, runecraftplan.TogetherBackoff)
		}
	}
	if !g.w.together.Try() {
		t.Error("not tried again after the back-off")
	}
}

// The cycle that lost: the talisman went and the trade did not bring the essence. It is a FAILED TRADE and a strike (whatever the view did),
// the account is waited for until it lands in the temple (so the next stage is decided from where it stands, not from the gate), the
// talisman is not kept for ToRuins, and the one-write accept is in its back-off. The sixth in a row ends the stage like any six failed trades.
func TestACycleThatLostIsAStrikeAndNeverGoesUnnoticed(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	lost := func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		sent := g.writeTalisman()
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Lag: true,
			Reason:   "the trade closed and the pack gained 0 essence (wanted 25) (the talisman was written with the accept, so the account teleports into the temple without the essence)",
			Together: runecraftrunner.TogetherInfo{Planned: true, Sent: true, Talisman: sent}}
	}
	g.place(false)
	res := g.gateWith(lost)
	if !res.OK || g.w.failedTrades != 1 || g.w.talisman != nil {
		t.Fatalf("Gate after one lost cycle: %+v, strikes %d, talisman kept %v", res, g.w.failedTrades, g.w.talisman != nil)
	}
	if !nature.Inside(g.e.here) {
		t.Errorf("Gate returned with the account at %v: it must wait for the use that is on its way", g.e.here)
	}
	if !g.logged("WARNING: trade with Acct 1 failed (1 in a row)") || !g.logged("one-write accept: failed (the trade with Acct 1 did not bring the essence") {
		t.Errorf("the loss was not logged:\n%s", strings.Join(g.r.logs, "\n"))
	}
	if g.w.together.Try() {
		t.Error("the back-off did not start")
	}
	// The stage that follows, from where the account stands: nothing to craft inside the temple, so the portal.
	step := runecraftplan.DecideGateStage(runecraftplan.GateSnapshot{Level: 74, Members: true, Where: runecraftplan.RegionTemple, Temple: runecraftplan.KeyNature, PackEssence: 0, Talisman: true})
	if step.Stage != runecraftplan.GatePortalOut {
		t.Errorf("the stage after a teleport without essence is %s", step.Stage)
	}
	g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	// Five more in a row (the lag flag does not excuse them): the stage fails at the sixth.
	for strike := 2; strike <= natureTradeFails; strike++ {
		g.place(false)
		res = g.gateWith(lost)
		if strike < natureTradeFails && (!res.OK || g.w.failedTrades != strike) {
			t.Fatalf("strike %d: %+v, strikes %d", strike, res, g.w.failedTrades)
		}
		g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
	}
	if res.OK || !strings.Contains(res.Why, "trades in a row failed") {
		t.Errorf("the sixth strike did not fail the stage: %+v", res)
	}
	// A trade that works clears the strikes.
	g.place(true)
	if res := g.gateWith(func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		return received(g.d.Session.State().Tick(), nil)
	}); !res.OK || g.w.failedTrades != 0 {
		t.Errorf("a good trade: %+v, strikes %d", res, g.w.failedTrades)
	}
}

// Three lost cycles in a row give the one-write accept up for the round: Gate offers it no more.
func TestThreeLostCyclesGiveTheOneWriteAcceptUp(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	lost := func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateFailed, Partner: "Acct 1", Reason: "no essence",
			Together: runecraftrunner.TogetherInfo{Planned: true, Sent: true, Talisman: g.writeTalisman()}}
	}
	for i := 0; i < runecraftplan.TogetherGiveUp; i++ {
		g.place(false)
		if res := g.gateWith(lost); !res.OK {
			t.Fatalf("loss %d: %+v", i+1, res)
		}
		g.step("portal", g.w.PortalOut(runecraftplan.Step{Stage: runecraftplan.StagePortalOut, Altar: nature}))
		for !g.w.together.GivenUp() && !g.w.together.Try() {
		}
	}
	if !g.w.together.GivenUp() || !g.logged("one-write accept: failed 3 times in a row") {
		t.Fatalf("not given up:\n%s", strings.Join(g.r.logs, "\n"))
	}
	g.place(true)
	var offered bool
	g.gateWith(func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		offered = o.Together != nil
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing}
	})
	if offered {
		t.Error("a given-up one-write accept was still offered to the handshake")
	}
}

// When the plan is asked: yes on the stand with the talisman and a current view, and every reason it says no is a plain accept that is not
// counted and is said once.
func TestThePlanSaysNoForEachReasonAndYesOnTheStand(t *testing.T) {
	g := newTogetherGate(t)
	g.r.logs = nil
	g.place(true)
	var opts runecraftrunner.GateOptions
	g.gateWith(func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
		opts = o
		return runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing}
	})
	if opts.Together == nil || opts.Together.Altar.Key != runecraftplan.KeyNature || opts.Together.Ruins.ID != runecraftplan.Nature.RuinsID || opts.Together.Plan == nil {
		t.Fatalf("the option: %+v", opts.Together)
	}
	if !opts.Together.Plan() {
		t.Fatalf("the plan said no on the stand:\n%s", strings.Join(g.r.logs, "\n"))
	}
	if g.w.together.Tried() != 0 {
		t.Error("asking the plan counted a cycle (cycles are counted by what they came to)")
	}
	// A view that went silent a few seconds ago.
	g.e.sim.State().OverrideViewLag(&gamestate.ViewLag{Silence: 8 * time.Second, SilenceEnded: time.Now().Add(-10 * time.Second)})
	if opts.Together.Plan() || opts.Together.Plan() {
		t.Error("the plan said yes in a shaky view")
	}
	g.e.sim.State().OverrideViewLag(&gamestate.ViewLag{})
	// A tile the engine does not accept the use from.
	g.e.mu.Lock()
	g.e.moveTo(runecraftplan.GateWaitTile)
	g.e.mu.Unlock()
	time.Sleep(3 * liveTick)
	if opts.Together.Plan() {
		t.Error("the plan said yes from a tile that is not a stand")
	}
	g.e.mu.Lock()
	g.e.moveTo(runecraftplan.GateStandTile)
	g.e.mu.Unlock()
	time.Sleep(3 * liveTick)
	// A use that failed from a stand this round.
	g.w.standTry[runecraftplan.KeyNature] = 1
	if opts.Together.Plan() {
		t.Error("the plan said yes after a use failed from a stand")
	}
	g.w.standTry[runecraftplan.KeyNature] = 0
	if !opts.Together.Plan() {
		t.Error("the plan did not say yes again")
	}
	for _, want := range []string{"the view is lagging", "the account does not stand where the engine accepts a use on the ruins", "a use on the ruins has failed from a stand this round"} {
		if n := g.logCount("one-write accept: the confirm accept is sent on its own: " + want); n != 1 {
			t.Errorf("%q was said %d times, want once:\n%s", want, n, strings.Join(g.r.logs, "\n"))
		}
	}
	// The talisman gone from the pack.
	g.e.sim.Remove(int32(runecraftplan.Nature.TalismanID), 1)
	time.Sleep(3 * liveTick)
	if opts.Together.Plan() {
		t.Error("the plan said yes with no talisman")
	}
}

// The setting off, and the levelling round (no state at all), never offer it: the handshake is the accept on its own.
func TestTheOneWriteAcceptIsNotOfferedWhenItIsOffOrNotTheGate(t *testing.T) {
	g := newTogetherGate(t)
	g.place(true)
	for name, ops := range map[string]*runecraftplan.TogetherOps{"off": runecraftplan.NewTogetherOps(false), "nil": nil} {
		g.w.together = ops
		offered := false
		g.gateWith(func(o runecraftrunner.GateOptions, plan bool) runecraftrunner.GateResult {
			offered = o.Together != nil
			return runecraftrunner.GateResult{Kind: runecraftrunner.GateNothing}
		})
		if offered {
			t.Errorf("%s: offered", name)
		}
	}
}

// A written talisman that ToRuins finds too old, or the account no longer on its stand, is forgotten: the plain use follows, once.
func TestAStaleWrittenTalismanIsForgottenAndTheUseIsPlain(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	g.w.talisman = &runecraftrunner.TalismanSent{Tick: 1, At: time.Now().Add(-time.Minute)}
	g.step("talisman", g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature}))
	if g.useMessages() != 1 || g.w.talisman != nil || !g.logged("one-write accept: the talisman written with the accept is not waited for") {
		t.Fatalf("uses %d, kept %v:\n%s", g.useMessages(), g.w.talisman != nil, strings.Join(g.r.logs, "\n"))
	}
}

// A written talisman whose account is inside the temple by the time the craft is decided has landed: the craft scores it and clears it.
func TestACraftScoresAWrittenTalismanThatAlreadyLanded(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	g.e.mu.Lock()
	g.e.moveTo(nature.Enter)
	g.e.mu.Unlock()
	g.w.talisman = &runecraftrunner.TalismanSent{Tick: 1, At: time.Now()}
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
	if g.w.talisman != nil || g.w.together.Tried() != 1 || !g.logged("had landed before the stage was decided") {
		t.Fatalf("kept %v, tried %d:\n%s", g.w.talisman != nil, g.w.together.Tried(), strings.Join(g.r.logs, "\n"))
	}
}

// The trade line of a plain cycle is the line it was, and a planned cycle that was accepted on its own says why.
func TestTheTradeLineSaysWhatTheOneWriteAcceptDid(t *testing.T) {
	if got := togetherWord(runecraftrunner.GateResult{}); got != "" {
		t.Errorf("a handshake that never planned one: %q", got)
	}
	waited := runecraftrunner.GateResult{Together: runecraftrunner.TogetherInfo{Planned: true, WaitedOut: true, Why: "x"}}
	if got := togetherWord(waited); !strings.Contains(got, "plain accept (the carrier's accept did not show in 1.8s)") {
		t.Errorf("waited out: %q", got)
	}
	held := runecraftrunner.GateResult{Together: runecraftrunner.TogetherInfo{Planned: true, Why: "another caller holds the account's input"}}
	if got := togetherWord(held); !strings.Contains(got, "plain accept (another caller holds the account's input)") {
		t.Errorf("held: %q", got)
	}
	sent := runecraftrunner.GateResult{Together: runecraftrunner.TogetherInfo{Planned: true, Sent: true, ConfirmTick: 10, OtherTick: 11, Talisman: &runecraftrunner.TalismanSent{Tick: 12}}}
	if got := togetherWord(sent); got != "; one write: accept and talisman in tick 12 (confirm screen seen in 10, the carrier's accept in 11)" {
		t.Errorf("sent: %q", got)
	}
}

// ---- a written talisman that LANDS before ToRuins can wait for it ----
//
// The talisman written with the accept is read in the tick the trade completes and lands about a tick after the close frame, so when
// ToRuins runs it is already in flight: the account can be inside the temple when the stage is decided, when the written talisman is taken,
// or when AwaitView returns (a lagging view delivers the landing then). From inside the temple the ruins are over a hundred tiles and no walk
// away, so ToRuins must neither walk to a stand nor use the talisman a second time (review of CR-2).

// waitInside waits for the view to show the account inside the Nature temple.
func (g *directGate) waitInside() {
	g.t.Helper()
	nature := runecraftplan.Nature
	for end := time.Now().Add(5 * time.Second); !nature.Inside(runecraftrunner.Here(g.d)); {
		if time.Now().After(end) {
			g.t.Fatalf("the view never showed the account inside the temple (at %s)", fleetrunner.HereIs(g.d))
		}
		time.Sleep(liveTick)
	}
}

// moveInside puts the account inside the temple, where the written talisman takes it, and waits for the view to show it.
func (g *directGate) moveInside() {
	g.t.Helper()
	g.e.mu.Lock()
	g.e.moveTo(runecraftplan.Nature.Enter)
	g.e.mu.Unlock()
	g.waitInside()
}

// landedWell is what every one of these cycles must come to: the stage done with the account inside, no walk, no use but the written one
// (uses is how many the engine read), the talisman consumed and the cycle scored a success of the one-write accept.
func (g *directGate) landedWell(res runeStepResult, uses int) {
	g.t.Helper()
	nature := runecraftplan.Nature
	logs := strings.Join(g.r.logs, "\n")
	if !res.OK {
		g.t.Fatalf("ToRuins: %+v\n%s", res, logs)
	}
	g.e.mu.Lock()
	inside := nature.Inside(g.e.here)
	g.e.mu.Unlock()
	if !inside {
		g.t.Errorf("the account was moved out of the temple (at %v)\n%s", g.e.here, logs)
	}
	if g.walks != 0 || g.logged("walked to") || g.logged("could not walk") {
		g.t.Errorf("a walk was made from inside the temple (%d plain walks)\n%s", g.walks, logs)
	}
	if n := g.useMessages(); n != uses {
		g.t.Errorf("the engine read %d uses of the talisman, want %d\n%s", n, uses, logs)
	}
	if g.w.talisman != nil {
		g.t.Error("the written talisman was not consumed")
	}
	if g.logged("not waited for") {
		g.t.Errorf("the landed talisman was forgotten as if it had been displaced\n%s", logs)
	}
	if g.w.together.Tried() != 1 || strings.Contains(g.w.together.Summary(), "1 failed") {
		g.t.Errorf("the cycle was not scored as a success: %s\n%s", g.w.together.Summary(), logs)
	}
	if len(g.w.standBad[nature.Key]) != 0 || g.w.standTry[nature.Key] != 0 {
		g.t.Errorf("a stand was marked: tries %d, bad %v", g.w.standTry[nature.Key], g.w.standBad[nature.Key])
	}
}

// The account is already inside the temple when ToRuins runs, with the talisman it was written with: the stage is done, and the craft follows.
func TestAWrittenTalismanThatAlreadyTookTheAccountInsideIsNeitherWalkedToNorUsedAgain(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	g.w.talisman = &runecraftrunner.TalismanSent{Tick: g.d.Session.State().Tick(), At: time.Now()}
	g.moveInside()
	res := g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature})
	g.landedWell(res, 0)
	if !g.logged("had taken the account inside before the stage ran") {
		t.Errorf("the landing was not said:\n%s", strings.Join(g.r.logs, "\n"))
	}
	g.step("craft", g.w.Craft(runecraftplan.Step{Stage: runecraftplan.StageCraft, Altar: nature}))
}

// The account is seen on the stand when the written talisman is taken, and the view that lags delivers the landing only when it catches up
// (AwaitView): the stand-walk branch must not run from inside the temple.
func TestAWrittenTalismanThatLandsWhileALaggingViewIsAwaitedIsNeitherWalkedToNorUsedAgain(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	g.w.talisman = &runecraftrunner.TalismanSent{Tick: g.d.Session.State().Tick(), At: time.Now()}
	st := g.e.sim.State()
	st.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
	t.Cleanup(func() { st.OverrideViewLag(&gamestate.ViewLag{}) })
	go func() {
		time.Sleep(400 * time.Millisecond)
		g.e.mu.Lock()
		g.e.moveTo(nature.Enter)
		g.e.mu.Unlock()
		time.Sleep(4 * liveTick)
		st.OverrideViewLag(&gamestate.ViewLag{})
	}()
	if nature.Inside(runecraftrunner.Here(g.d)) {
		t.Fatal("the account was inside before the stage ran")
	}
	res := g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature})
	g.landedWell(res, 0)
	if !g.logged("had taken the account inside before the stage ran") {
		t.Errorf("the landing was not said:\n%s", strings.Join(g.r.logs, "\n"))
	}
}

// The real thing against the engine: the use is written and ToRuins runs at once, so the landing arrives at some point of the stage
// (before the take, in the wait for the view, or while the use is waited for). Whichever it is, the engine reads the ONE use that was
// written, nothing is walked, and the cycle is a success.
func TestAWrittenTalismanThatLandsAtAnyPointOfTheStageIsReadOnce(t *testing.T) {
	for _, pause := range []time.Duration{0, liveTick, 2 * liveTick, 4 * liveTick, 8 * liveTick} {
		g := newTogetherGate(t)
		nature := runecraftplan.Nature
		g.r.logs = nil
		g.place(true)
		g.w.talisman = g.writeTalisman()
		time.Sleep(pause)
		res := g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature})
		g.landedWell(res, 1)
		if t.Failed() {
			t.Fatalf("pause %s", pause)
		}
	}
}

// A written talisman whose account was moved off its stand while the view was awaited is not waited for: it is forgotten, and the plain
// way follows (a walk to a stand, one use).
func TestAWrittenTalismanOfAnAccountMovedOffItsStandDuringTheViewWaitIsForgotten(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	g.r.logs = nil
	g.place(true)
	g.w.talisman = &runecraftrunner.TalismanSent{Tick: g.d.Session.State().Tick(), At: time.Now()}
	st := g.e.sim.State()
	st.OverrideViewLag(&gamestate.ViewLag{Lagging: true, Silent: true, Age: 6 * time.Second})
	t.Cleanup(func() { st.OverrideViewLag(&gamestate.ViewLag{}) })
	go func() {
		time.Sleep(400 * time.Millisecond)
		g.e.mu.Lock()
		g.e.moveTo(runecraftplan.GateWaitTile)
		g.e.mu.Unlock()
		time.Sleep(4 * liveTick)
		st.OverrideViewLag(&gamestate.ViewLag{})
	}()
	res := g.w.ToRuins(runecraftplan.Step{Stage: runecraftplan.StageUseTalisman, Altar: nature})
	if !res.OK || !g.logged("one-write accept: the talisman written with the accept is not waited for") {
		t.Fatalf("%+v\n%s", res, strings.Join(g.r.logs, "\n"))
	}
	if !nature.Inside(g.e.here) || g.useMessages() != 1 || g.w.talisman != nil {
		t.Errorf("inside %v, %d uses read, kept %v\n%s", nature.Inside(g.e.here), g.useMessages(), g.w.talisman != nil, strings.Join(g.r.logs, "\n"))
	}
}

// The three answers to a written talisman, asked directly: still on the stand (waited for), inside the temple (landed), moved off the
// stand (forgotten, once, neither a success nor a failure).
func TestRecheckTalismanTellsLandedFromDisplaced(t *testing.T) {
	g := newTogetherGate(t)
	nature := runecraftplan.Nature
	ruins, ok := runecraftrunner.FindRuins(g.d, nature)
	if !ok {
		t.Fatal("no ruins")
	}
	g.r.logs = nil
	g.place(true)
	s := &runecraftrunner.TalismanSent{Tick: 1, At: time.Now()}
	if got, landed := g.w.recheckTalisman(nature, ruins, s); got != s || landed {
		t.Fatalf("on the stand: %v landed %v", got, landed)
	}
	if got, landed := g.w.recheckTalisman(nature, ruins, nil); got != nil || landed {
		t.Fatalf("nothing written: %v landed %v", got, landed)
	}
	g.e.mu.Lock()
	g.e.moveTo(runecraftplan.GateWaitTile)
	g.e.mu.Unlock()
	time.Sleep(3 * liveTick)
	if got, landed := g.w.recheckTalisman(nature, ruins, s); got != nil || landed {
		t.Fatalf("off the stand: %v landed %v", got, landed)
	}
	if n := g.logCount("is not waited for"); n != 1 {
		t.Errorf("the displacement was said %d times, want once", n)
	}
	g.moveInside()
	if got, landed := g.w.recheckTalisman(nature, ruins, s); got != s || !landed {
		t.Fatalf("inside: %v landed %v", got, landed)
	}
	// A talisman older than talismanFresh that is found inside the temple has landed too (an account inside needs no stand): it is not
	// forgotten, and it is not counted a success either, since the write is not known to be what did it.
	g.w.talisman = &runecraftrunner.TalismanSent{Tick: 1, At: time.Now().Add(-time.Minute)}
	got, landed := g.w.takeTalisman(nature, ruins)
	if got == nil || !landed {
		t.Errorf("a stale talisman found inside: %v landed %v", got, landed)
	}
	if g.logCount("is not waited for") != 1 || g.w.talisman != nil {
		t.Errorf("a stale talisman found inside was forgotten (or kept):\n%s", strings.Join(g.r.logs, "\n"))
	}
	if r := g.w.talismanHadLanded(nature, got); !r.OK {
		t.Errorf("%+v", r)
	}
	// Two cycles came to nothing known (the displaced one and this one), none to a success or a failure.
	if !strings.Contains(g.w.together.Summary(), "0 worked, 0 failed, 2 not seen") {
		t.Errorf("a talisman that landed long after it was written was counted a success: %s", g.w.together.Summary())
	}
}
