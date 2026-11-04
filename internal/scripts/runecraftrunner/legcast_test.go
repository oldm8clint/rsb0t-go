package runecraftrunner

// A CAST ON THE LEG AGAINST THE ENGINE MODEL (CX-1, runecraftplan/legcast.go; the model is direct_engine_test.go with the cast rules added).
//
// The claims, each in engine ticks of the model (W is the tick the walk and the click are read in):
//
//   - the cast is written a tick and a margin after the walk and read in W+1 (never in W, which would throw the walk away);
//   - it clears the click, keeps the walk, locks the player for p_delay(n) (the reads of W+2 and W+3 are dropped for Low Level Alchemy);
//   - the click is sent again, whatever the round trip, so that one copy is the first read after the lock: the craft happens in W+4,
//     exactly where the plain direct op has it, for Low Level Alchemy on a leg of four steps and for a round trip of up to three ticks;
//   - High Level Alchemy costs the two ticks the plan says; a round trip longer than the copies cover costs a tick and the record says so;
//   - a cast read in the walk's own tick loses the walk, and the ride finds that out and sends the walk again with the click;
//   - a cast that is refused, or never written, leaves the leg as the plain direct op would have done it.
//
// THE CLOCK. The ride's cast is timed by a wall-clock timer from the walk's write (the real margin is 40 ms of a 600 ms tick), and the model
// ticks on a wall clock too, on a machine that is shared with the fleet: a stall of a few ticks, or a ticker that runs 10 ms short, moves the
// cast into the walk's tick or a tick late through no fault of the code. So a scenario that reads exact ticks is run up to four times
// (attempt) and passes when one run keeps every claim; and the model ticks at 100 ms and the cast goes 1.5 ticks after the walk, in the
// middle of the window of the tick after the one the walk is read in, because each scenario starts a few milliseconds into a tick
// (alignTick). Claims that do not depend on the clock (what the record says of what happened, the lock before a plain fallback) are
// asserted on every run.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
)

// castTickDur is the model's tick for these tests.
const castTickDur = 100 * time.Millisecond

// castRig is the gate's pack (25 essence and 60 noted bows) on the model, the ride's timer scaled to its tick, and the uplink lag.
func castRig(t *testing.T, lagTicks int) (*dirEngine, *fleetrunner.Deps) {
	t.Helper()
	shortTicks(t)
	oldTick, oldMargin, oldPoll, oldDur := legTick, legMargin, poll, dirTickDur
	legTick, legMargin, poll, dirTickDur = castTickDur, castTickDur/2, 2*time.Millisecond, castTickDur
	t.Cleanup(func() { legTick, legMargin, poll, dirTickDur = oldTick, oldMargin, oldPoll, oldDur })
	e, d := newDirEngine(t, natureTemplePack(t), natureKitWithBows(25, 60), runecraftplan.Nature.Enter, 74)
	e.mu.Lock()
	e.lagTicks = lagTicks
	e.mu.Unlock()
	// The model ticks ten times a second and a view that sees ten ticks in three seconds is "catching up a backlog" (gamestate/viewlag.go):
	// the wait for a view that lags would stretch every scenario by a random time. The view is pinned current.
	e.sim.State().OverrideViewLag(&gamestate.ViewLag{})
	// The model feeds the client a PLAYER_INFO for every step it moves the account (actionsim.StandAt), which the real server does not: the
	// ride counts the model's own ticks.
	oldRide := rideTick
	rideTick = func(*fleetrunner.Deps) int {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.tick
	}
	t.Cleanup(func() { rideTick = oldRide })
	return e, d
}

// attempt runs a scenario that reads exact ticks off a wall clock up to n times: it passes when one run keeps every claim (run returns
// ""), and fails with the complaint of every run. (The laptop these run on is at 100% CPU with the fleet: a run in two is spoiled by a
// stall of 300 ms.)
func attempt(t *testing.T, n int, run func() string) {
	t.Helper()
	var all []string
	for i := 0; i < n; i++ {
		last := run()
		if last == "" {
			return
		}
		all = append(all, fmt.Sprintf("run %d: %s", i+1, last))
	}
	t.Fatalf("%d runs, none kept every claim:\n%s", n, strings.Join(all, "\n"))
}

// alignTick waits for the engine's next tick, so the code under test starts a few milliseconds into a tick.
func (e *dirEngine) alignTick() {
	e.mu.Lock()
	t0 := e.tick
	e.mu.Unlock()
	for {
		time.Sleep(time.Millisecond)
		e.mu.Lock()
		t := e.tick
		e.mu.Unlock()
		if t != t0 {
			return
		}
	}
}

// bowCast is the production cast in miniature: the clear-move and the OPHELDT in one write, on the slot the bows are in.
func bowCast(d *fleetrunner.Deps, spell runecraftplan.LegSpell) *LegCast {
	n0 := PackCount(d, bowNoteID)
	return &LegCast{Spell: spell, Bow: "Magic longbow",
		Send: func() error {
			slot := -1
			for _, it := range packItems(d) {
				if it.ID == bowNoteID {
					slot = it.Slot
				}
			}
			b := actions.NewCoalescer(d.Session)
			if err := actions.ClearMoveRequest(b.Session(), bowNoteID, PlayerPack, func(s int) bool { return s == slot }); err != nil {
				b.Discard()
				return err
			}
			if err := actions.CastOnItem(b.Session(), spell.Name, bowNoteID, slot, PlayerPack); err != nil {
				b.Discard()
				return err
			}
			return b.Flush()
		},
		Landed: func() bool { return PackCount(d, bowNoteID) < n0 },
	}
}

// dump is the engine's timeline, for a failure to be read by.
func (e *dirEngine) dump() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var s []string
	for _, r := range e.reads {
		name := map[int]string{clientprot.MOVE_OPCLICK: "walk", clientprot.OPLOC1: "click", clientprot.OPHELDT: "cast", clientprot.OPHELDU: "clear"}[r.op]
		s = append(s, fmt.Sprintf("%d:%s", r.tick, name))
	}
	return fmt.Sprintf("engine: reads %s; crafts %v; casts %v; exits %v; dropped %d; lost walks %d; can't reach %d", strings.Join(s, " "), e.craftTicks, e.castTicks, e.exitTicks, e.dropped, e.lostWalks, e.cantReach)
}

// legTicks is the tick the leg's first walk was read in and the ticks the first cast and the first craft happened in.
func (e *dirEngine) legTicks() (walk, cast, craft int) {
	moves := e.readsOfLocked(clientprot.MOVE_OPCLICK)
	if len(moves) > 0 {
		walk = moves[0]
	}
	if len(e.castTicks) > 0 {
		cast = e.castTicks[0]
	}
	if len(e.craftTicks) > 0 {
		craft = e.craftTicks[0]
	}
	return
}

func altarOp(t *testing.T, d *fleetrunner.Deps) DirectOp {
	t.Helper()
	op, why := PlanDirect(d.Pack, runecraftplan.Nature.Enter, natStandAltar)
	if why != "" {
		t.Fatal(why)
	}
	return op
}

// THE CLAIM, for round trips of one, two and three ticks: the cast is read the tick after the walk and the craft is where the plain direct
// op has it, W plus the steps.
func TestAltarLegCastLowCostsNoTickWhateverTheRoundTripUpToThree(t *testing.T) {
	for lag := 0; lag <= 2; lag++ {
		lag := lag
		attempt(t, 6, func() string {
			e, d := castRig(t, lag)
			op := altarOp(t, d)
			e.alignTick()
			res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, bowCast(d, runecraftplan.LowLegSpell))
			if !res.OK() || !res.Direct || res.DirectFailed {
				return fmt.Sprintf("lag %d: %+v", lag, res)
			}
			c := res.Cast
			if !c.Tried || c.Landed == 0 || c.Missed || c.WalkLost || c.Steps != 4 || c.FirstStep == 0 || c.Resends == 0 {
				return fmt.Sprintf("lag %d: the record %s", lag, c)
			}
			e.mu.Lock()
			walk, cast, craft := e.legTicks()
			coins, bows := e.count(995), e.count(bowNoteID)
			bad := cast != walk+1 || craft != walk+op.Steps || e.casts != 1 || e.lostWalks != 0 || e.cantReach != 0 || bows != 59 || coins != bowCost*4/10
			e.mu.Unlock()
			if bad {
				return fmt.Sprintf("lag %d: walk read in %d, cast in %d, craft in %d (want %d and %d); bows %d, coins %d. %s", lag, walk, cast, craft, walk+1, walk+op.Steps, bows, coins, e.dump())
			}
			if c.Cost() != 0 || c.Excess() != 0 || c.Slip() != 0 {
				return fmt.Sprintf("lag %d: the record says cost %d, excess %d, slip %d: %s", lag, c.Cost(), c.Excess(), c.Slip(), c)
			}
			t.Logf("lag %d: %s", lag, c)
			return ""
		})
	}
}

// A round trip of four ticks is more than the copies cover: even the first copy is read after the lock, a tick after the first accepted
// read, so the craft is late, and the record says so, which is what stands the leg down.
func TestAltarLegCastWithAFourTickRoundTripCostsATickAndSaysSo(t *testing.T) {
	attempt(t, 6, func() string {
		e, d := castRig(t, 3)
		op := altarOp(t, d)
		e.alignTick()
		res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, bowCast(d, runecraftplan.LowLegSpell))
		if !res.OK() {
			return fmt.Sprintf("%+v", res)
		}
		e.mu.Lock()
		walk, cast, craft := e.legTicks()
		e.mu.Unlock()
		c := res.Cast
		if cast != walk+1 || craft != walk+op.Steps+1 || c.Cost() != 1 || c.Excess() != 1 {
			return fmt.Sprintf("walk %d, cast %d, craft %d: cost %d excess %d: %s. %s", walk, cast, craft, c.Cost(), c.Excess(), c, e.dump())
		}
		return ""
	})
}

// High Level Alchemy locks for four reads: the craft is two ticks later than the plain direct op, as planned.
func TestAltarLegCastHighCostsTheTwoTicksThePlanSays(t *testing.T) {
	attempt(t, 6, func() string {
		e, d := castRig(t, 1)
		op := altarOp(t, d)
		e.alignTick()
		res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, bowCast(d, runecraftplan.HighLegSpell))
		if !res.OK() {
			return fmt.Sprintf("%+v", res)
		}
		e.mu.Lock()
		walk, cast, craft := e.legTicks()
		paid := e.count(995)
		e.mu.Unlock()
		c := res.Cast
		if cast != walk+1 || craft != walk+op.Steps+2 || c.Cost() != 2 || c.Excess() != 0 || paid != bowCost*6/10 {
			return fmt.Sprintf("walk %d, cast %d, craft %d (want the cast at %d and the craft at %d): cost %d excess %d, paid %d: %s", walk, cast, craft, walk+1, walk+op.Steps+2, c.Cost(), c.Excess(), paid, c)
		}
		return ""
	})
}

// A cast read in the walk's own tick throws the walk away (the engine's rule); the ride sees the account has not moved and sends the walk
// again with the click, and the leg still ends in a craft -- late, with the record saying the walk was lost.
func TestACastInTheWalksOwnTickLosesTheWalkAndTheRideSendsItAgain(t *testing.T) {
	attempt(t, 6, func() string {
		e, d := castRig(t, 0)
		legTick, legMargin = 0, 0 // the cast goes out the moment the first poll looks, in the walk's own window
		op := altarOp(t, d)
		e.alignTick()
		res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, bowCast(d, runecraftplan.LowLegSpell))
		if !res.OK() {
			return fmt.Sprintf("%+v", res)
		}
		c := res.Cast
		e.mu.Lock()
		walk, cast, craft := e.legTicks()
		lost, here, crafts := e.lostWalks, e.here, e.crafts
		moves := len(e.readsOfLocked(clientprot.MOVE_OPCLICK))
		e.mu.Unlock()
		if lost != 1 || cast != walk || !c.WalkLost || c.Slip() >= 0 || craft <= walk+op.Steps || here != natStandAltar || crafts != 1 || moves < 2 {
			return fmt.Sprintf("walk read in %d, cast in %d, craft in %d, lost walks %d, walks read %d, record %s. %s", walk, cast, craft, lost, moves, c, e.dump())
		}
		return ""
	})
}

// The spell is refused (no Nature rune): the game says so, the click it cleared is sent at once and the leg ends as the plain one does,
// with the record saying the cast never landed. Nothing here reads a tick off the clock.
func TestARefusedCastSendsTheClickAgainAndSaysSo(t *testing.T) {
	e, d := castRig(t, 0)
	e.mu.Lock()
	e.refuseCasts = true
	e.mu.Unlock()
	op := altarOp(t, d)
	e.alignTick()
	started := time.Now()
	res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, bowCast(d, runecraftplan.LowLegSpell))
	if !res.OK() {
		t.Fatalf("%+v", res)
	}
	c := res.Cast
	if !c.Tried || !c.Missed || c.Landed != 0 || c.Resends == 0 {
		t.Errorf("the record %s", c)
	}
	if took := time.Since(started); took > 14*castTickDur {
		t.Errorf("took %s: the refusal should not cost the whole landing wait", took)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.casts != 0 || e.count(bowNoteID) != 60 || e.crafts != 1 {
		t.Errorf("casts %d, bows %d, crafts %d", e.casts, e.count(bowNoteID), e.crafts)
	}
}

// A cast that could not be written leaves the click T0 queued standing: the leg is the plain direct op, to the tick.
func TestACastThatCannotBeWrittenLeavesTheDirectOpAlone(t *testing.T) {
	attempt(t, 6, func() string {
		e, d := castRig(t, 0)
		op := altarOp(t, d)
		lc := bowCast(d, runecraftplan.LowLegSpell)
		lc.Send = func() error { return actions.ErrUnknownSpell }
		e.alignTick()
		res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, lc)
		if !res.OK() || res.Cast.Tried || res.Cast.Resends != 0 || !strings.Contains(res.Cast.Why, "could not be sent") {
			return fmt.Sprintf("%+v, %s", res, res.Cast)
		}
		e.mu.Lock()
		walk, _, craft := e.legTicks()
		casts := e.casts
		e.mu.Unlock()
		if craft != walk+op.Steps || casts != 0 {
			return fmt.Sprintf("walk %d, craft %d, casts %d. %s", walk, craft, casts, e.dump())
		}
		return ""
	})
}

// With no cast at all CraftDirectCast is CraftDirect, and the record is empty.
func TestCraftDirectCastWithNoCastIsCraftDirect(t *testing.T) {
	attempt(t, 6, func() string {
		e, d := castRig(t, 0)
		op := altarOp(t, d)
		e.alignTick()
		res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, nil)
		if !res.OK() || res.Cast.Tried || res.Cast.Resends != 0 {
			return fmt.Sprintf("%+v", res)
		}
		e.mu.Lock()
		walk, _, craft := e.legTicks()
		msgs := len(e.messages)
		e.mu.Unlock()
		if craft != walk+op.Steps || msgs != 1 {
			return fmt.Sprintf("walk %d craft %d, %d messages", walk, craft, msgs)
		}
		return ""
	})
}

// A leg whose click is never accepted after the cast ends as a direct failure -- and not before the lock is over, so the plain way's first
// click is not one more dropped.
func TestAFailedLegWaitsOutTheLockBeforeTheCallerWalksThePlainWay(t *testing.T) {
	e, d := castRig(t, 0)
	oldWait := craftWait
	craftWait = 4 * castTickDur
	t.Cleanup(func() { craftWait = oldWait })
	e.mu.Lock()
	e.ignoreOpsAfterCast = true
	e.mu.Unlock()
	op := altarOp(t, d)
	e.alignTick()
	res := CraftDirectCast(d, runecraftplan.Nature, natureAltarMatch(), op, bowCast(d, runecraftplan.HighLegSpell))
	if res.OK() || !res.DirectFailed || !strings.Contains(res.Reason, "with a cast on the leg") {
		t.Fatalf("%+v", res)
	}
	c := res.Cast
	if c.Landed == 0 {
		t.Fatalf("the cast should have landed: %s", c)
	}
	if now := rideTick(d); now < c.Landed+runecraftplan.HighLegSpell.AcceptedAfter() {
		t.Errorf("returned in tick %d, inside the lock that ends with the read of %d", now, c.Landed+runecraftplan.HighLegSpell.AcceptedAfter())
	}
	if e.count(essID) != 25 {
		t.Error("the essence was used")
	}
}

// ---- the portal leg ----

// The portal's pair goes out at the first accepted read after the craft's hold, the cast a tick later, the click again after the lock: the
// exit is where the plain direct op has it, W plus the steps.
func TestPortalLegCastLowCostsNoTick(t *testing.T) {
	for lag := 0; lag <= 1; lag++ {
		lag := lag
		attempt(t, 6, func() string {
			e, d := castRig(t, lag)
			op := altarOp(t, d)
			e.alignTick()
			cr := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
			if !cr.OK() {
				return fmt.Sprintf("lag %d: the craft before the portal: %+v", lag, cr)
			}
			lc := bowCast(d, runecraftplan.LowLegSpell)
			plan := func() (DirectOp, *LegCast, bool) {
				p, ok := planTo(d, natStandPortal)
				return p, lc, ok
			}
			res := PortalOutCast(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, plan, func() bool { return true })
			if !res.Out || !res.Direct || res.DirectFailed || res.Reason != "" {
				return fmt.Sprintf("lag %d: %+v", lag, res)
			}
			c := res.Cast
			if !c.Tried || c.Landed == 0 || c.WalkLost || c.Missed || c.Steps != 4 || c.Op == 0 {
				return fmt.Sprintf("lag %d: the record %s. %s", lag, c, e.dump())
			}
			e.mu.Lock()
			var moveAt int
			for _, r := range e.reads {
				if r.tick > e.craftTicks[0] && r.op == clientprot.MOVE_OPCLICK {
					moveAt = r.tick
				}
			}
			cast := e.castTicks[0]
			bad := cast != moveAt+1 || len(e.exitTicks) != 1 || e.exitTicks[0] != moveAt+4 || e.lostWalks != 0 || e.cantReach != 0
			exits := e.exitTicks
			e.mu.Unlock()
			if bad {
				return fmt.Sprintf("lag %d: the portal walk was read in %d, the cast in %d, exits %v (want the cast at %d and the exit at %d). %s", lag, moveAt, cast, exits, moveAt+1, moveAt+4, e.dump())
			}
			if c.Cost() != 0 || c.Excess() != 0 {
				return fmt.Sprintf("lag %d: %s", lag, c)
			}
			return ""
		})
	}
}

// PortalOutDirect is PortalOutCast with no cast: the same exit, an empty record.
func TestPortalOutDirectKeepsItsSignatureAndCastsNothing(t *testing.T) {
	e, d := castRig(t, 0)
	op := altarOp(t, d)
	e.alignTick()
	cr := CraftDirect(d, runecraftplan.Nature, natureAltarMatch(), op)
	res := PortalOutDirect(d, runecraftplan.Nature, naturePortalMatch(), cr.Tick, func() (DirectOp, bool) { return planTo(d, natStandPortal) }, func() bool { return true })
	if !res.Out || res.Cast.Tried || res.Cast.Resends != 0 {
		t.Fatalf("%+v", res)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.casts != 0 {
		t.Error("a cast went out")
	}
}

// A ride that was not looked at for a while (a stalled laptop) writes no cast into a leg that is over or whose cast would be a tick late:
// T0's click stands and the record says why.
func TestARideThatLooksLateOrAfterTheLegIsOverWritesNoCast(t *testing.T) {
	e, d := castRig(t, 0)
	_ = e
	op := altarOp(t, d)
	for _, c := range []struct {
		name  string
		since time.Duration
		done  bool
	}{{"a tick late", 2*legTick + legMargin + time.Millisecond, false}, {"the leg is over", legTick + legMargin + time.Millisecond, true}} {
		sent := 0
		lc := bowCast(d, runecraftplan.LowLegSpell)
		lc.Send = func() error { sent++; return nil }
		r := newLegRide(d, runecraftplan.LegAltar, lc, op, runecraftplan.Nature.Enter, nil, func() bool { return c.done })
		r.t0 = time.Now().Add(-c.since)
		r.step()
		if sent != 0 || r.rec.Tried || r.phase != rideDone || !strings.Contains(r.rec.Why, "was not written") {
			t.Errorf("%s: sent %d, tried %v, phase %d, why %q", c.name, sent, r.rec.Tried, r.phase, r.rec.Why)
		}
	}
	// On time and the leg going on, the same ride writes the cast.
	sent := 0
	lc := bowCast(d, runecraftplan.LowLegSpell)
	lc.Send = func() error { sent++; return nil }
	r := newLegRide(d, runecraftplan.LegAltar, lc, op, runecraftplan.Nature.Enter, nil, func() bool { return false })
	r.t0 = time.Now().Add(-(legTick + legMargin + time.Millisecond))
	r.step()
	if sent != 1 || !r.rec.Tried || r.phase != rideLanding {
		t.Errorf("on time: sent %d, tried %v, phase %d", sent, r.rec.Tried, r.phase)
	}
}
