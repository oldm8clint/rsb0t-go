package runecraftrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// ---- the plan ----

func TestPlanDirectRoutesTheNatureLegsAndTheServerWalksThem(t *testing.T) {
	pk := natureTemplePack(t)
	n := runecraftplan.Nature
	op, why := PlanDirect(pk, n.Enter, natStandAltar)
	if why != "" || op.Steps != 4 || len(op.Route) == 0 || op.Route[len(op.Route)-1] != nav(natStandAltar) || op.To != natStandAltar || op.Run {
		t.Fatalf("arrival to the altar stand: %+v, %q", op, why)
	}
	back, why := PlanDirect(pk, natStandAltar, natStandPortal)
	if why != "" || back.Steps != 4 || back.Route[len(back.Route)-1] != nav(natStandPortal) {
		t.Fatalf("altar stand to the portal stand: %+v, %q", back, why)
	}
	// Already on the stand: no walk at all, only the click.
	here, why := PlanDirect(pk, natStandAltar, natStandAltar)
	if why != "" || len(here.Route) != 0 || here.Steps != 0 {
		t.Errorf("standing on the stand: %+v, %q", here, why)
	}
}

// Everything the plan refuses is a reason to walk the plain way, said in words.
func TestPlanDirectRefusesWhatThePackCannotConfirm(t *testing.T) {
	pk := natureTemplePack(t)
	n := runecraftplan.Nature
	for _, c := range []struct {
		name       string
		pk         *pathfind.Pack
		from, to   tile.Tile
		wantReason string
	}{
		{"no pack", nil, n.Enter, natStandAltar, "no collision pack"},
		{"another level", pk, tile.Tile{X: 2400, Z: 4835, Level: 1}, natStandAltar, "level"},
		{"too far for a temple leg", pk, n.Enter, tile.Tile{X: 2409, Z: 4849}, "steps (at most"},
		{"a stand inside the altar", pk, n.Enter, tile.Tile{X: 2400, Z: 4841}, "not on the stand"},
		{"outside the pack's ground", pk, n.Enter, tile.Tile{X: 2500, Z: 4835}, ""},
	} {
		op, why := PlanDirect(c.pk, c.from, c.to)
		if why == "" || len(op.Route) != 0 {
			t.Errorf("%s: planned %+v", c.name, op)
			continue
		}
		if c.wantReason != "" && !strings.Contains(why, c.wantReason) {
			t.Errorf("%s: the reason is %q, want %q in it", c.name, why, c.wantReason)
		}
	}
}

// A pillar between the account and the stand: the server walks a waypoint greedily, so a lone waypoint behind a pillar parks the account
// (the 18 Oct 2026 pillar that cost the gate 56 minutes); the pack's route goes round it and the plan only accepts a route the server's own
// replay arrives by.
func TestPlanDirectGoesRoundAPillarThatParksALoneWaypoint(t *testing.T) {
	pillar := tile.Tile{X: 2400, Z: 4837}
	pk := natureTemplePack(t, pillar)
	n := runecraftplan.Nature
	if end, ok := pk.EngineWalk(nav(n.Enter), []pathfind.NavPoint{nav(natStandAltar)}, 20); ok {
		t.Fatalf("the model is wrong: a lone waypoint behind a pillar arrived at %v", end)
	}
	op, why := PlanDirect(pk, n.Enter, natStandAltar)
	if why != "" || op.Steps != 4 {
		t.Fatalf("round the pillar: %+v, %q", op, why)
	}
	for _, p := range op.Route {
		if p.X == pillar.X && p.Z == pillar.Z {
			t.Errorf("the route goes through the pillar: %v", op.Route)
		}
	}
	if len(op.Route) < 3 {
		t.Errorf("a route round a pillar has corners: %v", op.Route)
	}
}

// ---- the write ----

// The walk and the click leave in ONE websocket message, in that order, the way the real client does a click on a loc.
func TestSendDirectWritesTheWalkAndTheClickAsOneMessage(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	op, why := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	if why != "" {
		t.Fatal(why)
	}
	altar := natureAltarMatch()
	sent, err := sendDirect(d, op, func(s actions.Session) error { return actions.OpLoc(s, altar.Op, altar.X, altar.Z, altar.ID) })
	if err != nil || !sent {
		t.Fatalf("sent %v err %v", sent, err)
	}
	msgs := e.sim.Sent()
	if len(msgs) != 1 {
		t.Fatalf("%d messages were written, want 1", len(msgs))
	}
	var ops []int
	for _, op := range actionsim.Opcodes(msgs[0]) {
		if op == clientprot.MOVE_OPCLICK || op == clientprot.OPLOC1 {
			ops = append(ops, op)
		}
	}
	if joinOps(ops) != "MOVE_OPCLICK+OPLOC1" {
		t.Errorf("the message holds %s", joinOps(ops))
	}
	// A leg whose account already stands on the stand writes the click alone.
	e2, d2 := newDirEngine(t, natureTemplePack(t), natureKit(25), natStandAltar, 74)
	if sent, err := sendDirect(d2, DirectOp{To: natStandAltar}, func(s actions.Session) error { return actions.OpLoc(s, altar.Op, altar.X, altar.Z, altar.ID) }); err != nil || !sent {
		t.Fatalf("a click alone: sent %v err %v", sent, err)
	}
	if got := e2.sim.Sent(); len(got) != 1 || len(actionsim.Opcodes(got[0])) < 1 || joinOps(filterOps(actionsim.Opcodes(got[0]))) != "OPLOC1" {
		t.Errorf("a click alone: %v", got)
	}
}

// planTo is the plan callback PortalOutDirect asks: the direct op from where the account stands to a stand.
func planTo(d *fleetrunner.Deps, to tile.Tile) (DirectOp, bool) {
	op, why := PlanDirect(d.Pack, Here(d), to)
	return op, why == ""
}

func filterOps(in []int) []int {
	var out []int
	for _, op := range in {
		if op == clientprot.MOVE_OPCLICK || op == clientprot.OPLOC1 {
			out = append(out, op)
		}
	}
	return out
}

// Another caller holds the account's input (the Guardian's solver): nothing is written, and the caller is told so.
func TestSendDirectWritesNothingWhileAnotherCallerHoldsTheInput(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	hold := actions.HoldMovement(d.Session, nil)
	defer hold.Release()
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	altar := natureAltarMatch()
	sent, err := sendDirect(d, op, func(s actions.Session) error { return actions.OpLoc(s, altar.Op, altar.X, altar.Z, altar.ID) })
	if err != nil || sent || len(e.sim.Sent()) != 0 {
		t.Fatalf("sent %v err %v, %d messages", sent, err, len(e.sim.Sent()))
	}
	res := CraftDirect(d, runecraftplan.Nature, altar, op)
	if res.OK() || !res.DirectFailed || res.Direct || !strings.Contains(res.Reason, "held back") || e.count(essID) != 25 {
		t.Errorf("a craft under a hold: %+v", res)
	}
}

// ---- the altar leg ----

// THE CLAIM: the walk and the click are read in one tick, the craft runs the tick after the last step -- Steps ticks after the read -- and
// nothing waited to see the account arrive.
func TestCraftDirectCraftsTheTickAfterTheLastStep(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	op, why := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	if why != "" {
		t.Fatal(why)
	}
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if !res.OK() || !res.Direct || res.DirectFailed || res.Clicks != 1 || res.Essence != 25 || res.Left != 0 || res.Runes != 25 || res.XPGain != 225 {
		t.Fatalf("%+v", res)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	moves, locs := e.readsOfLocked(clientprot.MOVE_OPCLICK), e.readsOfLocked(clientprot.OPLOC1)
	if len(moves) != 1 || len(locs) != 1 || moves[0] != locs[0] {
		t.Fatalf("the walk was read in ticks %v and the click in %v: they must be one tick", moves, locs)
	}
	if len(e.craftTicks) != 1 || e.craftTicks[0]-locs[0] != op.Steps {
		t.Errorf("read in tick %d, crafted in %v: %d steps should put the craft %d ticks after the read", locs[0], e.craftTicks, op.Steps, op.Steps)
	}
	if e.dropped != 0 || e.cantReach != 0 || e.here != natStandAltar {
		t.Errorf("dropped %d, can't reach %d, standing at %v", e.dropped, e.cantReach, e.here)
	}
	if len(e.messages) != 1 || joinOps(filterOps(e.messages[0])) != "MOVE_OPCLICK+OPLOC1" {
		t.Errorf("messages %v", e.messages)
	}
	t.Logf("walk and click read in tick %d, %d step(s), crafted in tick %d", locs[0], op.Steps, e.craftTicks[0])
}

func (e *dirEngine) readsOfLocked(op int) []int {
	var out []int
	for _, r := range e.reads {
		if r.op == op {
			out = append(out, r.tick)
		}
	}
	return out
}

// THE OTHER CLAIM, the portal: the first accepted read is craft+5, the walk and the click go out in it, and the exit runs Steps ticks after.
func TestPortalOutDirectLeavesAtTheFirstAcceptedReadAfterTheHold(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	cr := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if !cr.OK() {
		t.Fatalf("%+v", cr)
	}
	asked := 0
	plan := func() (DirectOp, bool) {
		asked++
		return planTo(d, natStandPortal)
	}
	approachCalls := 0
	res := PortalOutDirect(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, plan, func() bool { approachCalls++; return true })
	if !res.Out || !res.Direct || res.DirectFailed || res.Clicks != 1 || res.Reason != "" || asked != 1 || approachCalls != 0 {
		t.Fatalf("%+v, plan asked %d, approach %d", res, asked, approachCalls)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	craft := e.craftTicks[0]
	// The second walk and the second click: the reads after the craft's.
	var moveAt, clickAt int
	for _, r := range e.reads {
		if r.tick > craft && r.op == clientprot.MOVE_OPCLICK {
			moveAt = r.tick
		}
		if r.tick > craft && r.op == clientprot.OPLOC1 {
			clickAt = r.tick
		}
	}
	// Both belong to one read, no earlier than the first accepted one (craft+5; an earlier one would be dropped, and dropped is checked
	// below) and, a scheduling hiccup of the test machine apart, no later.
	if moveAt != clickAt || moveAt < craft+5 || moveAt > craft+6 {
		t.Errorf("crafted in tick %d, the portal's walk was read in %d and its click in %d: both belong to the first accepted read, %d", craft, moveAt, clickAt, craft+5)
	}
	if len(e.exitTicks) != 1 || e.exitTicks[0] != moveAt+4 {
		t.Errorf("exit ticks %v, want %d (the read and four steps)", e.exitTicks, moveAt+4)
	}
	t.Logf("crafted in tick %d, portal walk and click read in tick %d, out in tick %v", craft, moveAt, e.exitTicks)
	if e.dropped != 0 || e.cantReach != 0 {
		t.Errorf("dropped %d, can't reach %d: a packet read in the hold is lost without a word", e.dropped, e.cantReach)
	}
}

// ---- the altar leg, when the direct op does not do it ----

// The walk was dropped (a hold would): the click alone is refused by the engine -- it does not path to a loc -- and says so. The essence
// is still in the pack and the plain way works from the same state.
func TestCraftDirectSaysSoWhenTheEngineRefusesTheClick(t *testing.T) {
	shortTicks(t)
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	e.mu.Lock()
	e.ignoreMoves = true
	e.mu.Unlock()
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	started := time.Now()
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if res.OK() || !res.Direct || !res.DirectFailed || res.Clicks != 1 || !strings.Contains(res.Reason, "can't reach") || res.Lag {
		t.Fatalf("%+v", res)
	}
	if took := time.Since(started); took > craftWait/2 {
		t.Errorf("took %s: the refusal should end the wait", took)
	}
	if e.count(essID) != 25 || e.crafts != 0 {
		t.Errorf("the essence was used: %d left, %d crafts", e.count(essID), e.crafts)
	}
	// The plain way, from where the failure left the account: walk to the stand, click.
	e.mu.Lock()
	e.moveTo(natStandAltar)
	e.mu.Unlock()
	if plain := Craft(d, runecraftplan.Nature, natureAltarMatch()); !plain.OK() || plain.Essence != 25 {
		t.Fatalf("the plain craft after a failed direct op: %+v", plain)
	}
}

// The click was lost (a packet the engine never read): the account walked to the stand and nothing happened. That is a direct failure only
// once the walk and the click were worth have passed, in a view that kept up -- and the essence is still there for the plain click.
func TestCraftDirectSaysSoWhenNothingHappens(t *testing.T) {
	shortTicks(t)
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	e.mu.Lock()
	e.ignoreOps = true
	e.mu.Unlock()
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if res.OK() || !res.DirectFailed || res.Clicks != 1 || !strings.Contains(res.Reason, "nothing happened") || res.Lag {
		t.Fatalf("%+v", res)
	}
	e.mu.Lock()
	e.ignoreOps = false
	here := e.here
	e.mu.Unlock()
	if here != natStandAltar {
		t.Errorf("the walk half should have landed on the stand: %v", here)
	}
	if plain := Craft(d, runecraftplan.Nature, natureAltarMatch()); !plain.OK() {
		t.Fatalf("the plain craft after a lost click: %+v", plain)
	}
}

// A craft that took the essence and gave no experience is a failed craft -- there is nothing left to click -- and not a direct failure.
func TestCraftDirectWithoutExperienceIsNotAFallback(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	e.mu.Lock()
	e.noXP = true
	e.mu.Unlock()
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if res.OK() || res.DirectFailed || res.Tick == 0 || res.Verify.OK || res.Left != 0 || !strings.Contains(res.Reason, "experience did not rise") {
		t.Fatalf("%+v", res)
	}
}

// The game's own refusals are not the direct op's: no level, no essence.
func TestCraftDirectRefusalsAreTheGamesNotTheOps(t *testing.T) {
	_, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 30)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if res.OK() || res.DirectFailed || !res.Direct || !strings.Contains(res.Reason, "refused") || !strings.Contains(res.Reason, "level of at least 44") {
		t.Errorf("level too low: %+v", res)
	}
	_, d2 := newDirEngine(t, natureTemplePack(t), nil, runecraftplan.Nature.Enter, 74)
	res = CraftDirect(d2, runecraftplan.Nature, natureAltarMatch(), op)
	if res.OK() || res.DirectFailed || res.Direct || res.Clicks != 0 || !strings.Contains(res.Reason, "no Rune essence") {
		t.Errorf("an empty pack: %+v", res)
	}
}

// A craft that lands late in a view that lags is a craft, and the click is not sent again: the lag is the view's.
func TestCraftDirectInALaggingViewWaitsAndNeverClicksAgain(t *testing.T) {
	shortTicks(t)
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	e.mu.Lock()
	e.craftLateTicks = 32 // 1.6 s after the op runs: after the 1.2 s the walk and the click are worth, inside the second look
	e.mu.Unlock()
	lagFor(e.sim.State(), 100*time.Millisecond, 500*time.Millisecond)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if !res.OK() || res.DirectFailed || !res.Lag || res.Clicks != 1 || res.Left != 0 {
		t.Fatalf("a craft that landed late: %+v", res)
	}
	if locs := e.readsOf(clientprot.OPLOC1); len(locs) != 1 {
		t.Errorf("the altar was clicked %d times", len(locs))
	}
}

// A craft that never shows in a view that lags fails WITH the lag and is not a direct failure: the plain way would click again into
// whatever the first click did.
func TestCraftDirectUnseenInALaggingViewIsNotADirectFailure(t *testing.T) {
	shortTicks(t)
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	e.mu.Lock()
	e.craftLateTicks = 4000
	e.mu.Unlock()
	lagFor(e.sim.State(), 60*time.Millisecond, 500*time.Millisecond)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	res := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if res.OK() || res.DirectFailed || !res.Lag || res.Clicks != 1 || !strings.Contains(res.Reason, "view was lagging") {
		t.Fatalf("%+v", res)
	}
}

// ---- the portal leg, when the direct op does not do it ----

// The walk was dropped: the click is refused, and the plain approach and click follow in the same call. The result says the direct op
// failed and the account is out all the same.
func TestPortalOutDirectFallsBackToTheApproachAndTheClick(t *testing.T) {
	shortTicks(t)
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	cr := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if !cr.OK() {
		t.Fatalf("%+v", cr)
	}
	e.mu.Lock()
	e.ignoreMoves = true
	e.mu.Unlock()
	approach := func() bool {
		e.mu.Lock()
		e.moveTo(natStandPortal) // the walker's walk to the stand
		e.mu.Unlock()
		return true
	}
	plan := func() (DirectOp, bool) { return planTo(d, natStandPortal) }
	res := PortalOutDirect(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, plan, approach)
	if !res.Out || !res.Direct || !res.DirectFailed || res.Clicks != 2 || !strings.Contains(res.DirectWhy, "can't reach") || res.Reason != "" {
		t.Fatalf("%+v", res)
	}
	if e.portals != 1 || runecraftplan.Nature.Inside(Here(d)) {
		t.Errorf("portals %d, standing at %v", e.portals, Here(d))
	}
}

// A plan that says no is the plain PortalOut, to the byte: no direct write, the approach, one click.
func TestPortalOutDirectWithNoPlanIsThePlainPortalOut(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	cr := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if !cr.OK() {
		t.Fatalf("%+v", cr)
	}
	approached := 0
	approach := func() bool {
		approached++
		e.mu.Lock()
		e.moveTo(natStandPortal)
		e.mu.Unlock()
		return true
	}
	res := PortalOutDirect(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, func() (DirectOp, bool) { return DirectOp{}, false }, approach)
	if !res.Out || res.Direct || res.DirectFailed || res.Clicks != 1 || approached != 1 {
		t.Fatalf("%+v, approach %d", res, approached)
	}
	// And a nil plan is PortalOut itself.
	e2, d2 := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	cr2 := CraftDirect(d2, runecraftplan.Nature, natureAltarMatch(), op)
	if !cr2.OK() {
		t.Fatalf("%+v", cr2)
	}
	res = PortalOutDirect(d2, runecraftplan.Nature, naturePortalMatch(), cr2.Tick, nil, func() bool {
		e2.mu.Lock()
		e2.moveTo(natStandPortal)
		e2.mu.Unlock()
		return true
	})
	if !res.Out || res.Direct || res.Clicks != 1 {
		t.Fatalf("a nil plan: %+v", res)
	}
}

// An exit that is late in a view that lags is an exit: it is waited for and the portal is not clicked again.
func TestPortalOutDirectInALaggingViewWaitsAndNeverClicksAgain(t *testing.T) {
	shortTicks(t)
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), runecraftplan.Nature.Enter, 74)
	op, _ := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	cr := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	if !cr.OK() {
		t.Fatalf("%+v", cr)
	}
	e.mu.Lock()
	e.portalLateTicks = 18 // 0.9 s after the op runs
	e.mu.Unlock()
	// The craft's hold takes about 200 ms of real time, then the click goes out; the view lags from before it to 0.4 s, and the exit lands
	// around the 1 s the walk and the click are worth, in the first look or the second.
	lagFor(e.sim.State(), 50*time.Millisecond, 400*time.Millisecond)
	plan := func() (DirectOp, bool) { return planTo(d, natStandPortal) }
	res := PortalOutDirect(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, plan, func() bool { t.Error("the plain approach ran after a result that was not seen"); return true })
	if !res.Out || res.DirectFailed || !res.Lag || res.Clicks != 1 {
		t.Fatalf("%+v", res)
	}
	if locs := len(e.readsOf(clientprot.OPLOC1)); locs != 2 { // the altar's and the portal's, never a third
		t.Errorf("%d loc clicks read, want the altar's and the portal's only", locs)
	}
}

// The plain PortalOut and Craft are untouched by all this: the direct engine plays the plain way too (the stand walked to, then the click).
func TestThePlainLegsStillWorkOnTheMovementEngine(t *testing.T) {
	e, d := newDirEngine(t, natureTemplePack(t), natureKit(25), natStandAltar, 74)
	cr := Craft(d, runecraftplan.Nature, natureAltarMatch())
	if !cr.OK() || cr.Direct || cr.Clicks != 1 {
		t.Fatalf("%+v", cr)
	}
	res := PortalOut(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, func() bool {
		e.mu.Lock()
		e.moveTo(natStandPortal)
		e.mu.Unlock()
		return true
	})
	if !res.Out || res.Direct || res.Clicks != 1 {
		t.Fatalf("%+v", res)
	}
}
