package bot

// The Nature gate's DIRECT-OP legs (CR-1 of docs/xp_rate_plan_2026-10-19.md; runecraftplan/direct.go has the why, the engine rules and
// the numbers, runecraftrunner/direct.go the I/O).
//
// After the talisman lands the crafter inside the temple, the altar click goes out in the SAME write as the walk to the altar's stand
// (a MOVE_OPCLICK with the pack's route, then the OPLOC), and after the craft's hold the portal click goes out with the walk to the
// portal's stand at the first read the engine accepts (craft+5). Nothing waits to SEE the account arrive any more.
//
// Each leg is an optimisation of the walk-then-click below it, never a replacement: the plain way runs whenever a leg is not planned
// (craftRuneDirectOps off, a view that is shaky, no route the pack and the server's own greedy walk agree on, the account already on the
// stand), and after any direct attempt that did nothing. DirectOps keeps the score: a failed leg is plain for DirectBackoff cycles, a
// leg that failed DirectGiveUp times in a row for the rest of the round.

import (
	"time"

	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/runecraftrunner"
)

// directPlanned says why a leg was not planned as a direct op, once a round for each reason (a pack with no route would otherwise say it
// every cycle).
func (w *masterRuneWorld) directPlanned(l runecraftplan.Leg, why string) {
	key := l.String() + ": " + why
	if w.directSaid == nil {
		w.directSaid = map[string]bool{}
	}
	if w.directSaid[key] {
		return
	}
	w.directSaid[key] = true
	w.logf("direct op: the %s leg is walked and clicked the plain way: %s", l, why)
}

// directNote tells the state what a direct attempt came to and logs what it answers.
func (w *masterRuneWorld) directNote(l runecraftplan.Leg, o runecraftplan.DirectOutcome, why string) {
	switch o {
	case runecraftplan.DirectOK:
		w.legs.noteMode(l, runecraftplan.ModeDirect)
	case runecraftplan.DirectFailed:
		w.legs.noteMode(l, runecraftplan.ModeFallback)
	}
	if line := w.direct.Note(l, o, why); line != "" {
		w.logf("%s", line)
	}
}

// craftDirect sends the altar leg as a direct op when it can. handled is true when it produced the step's result (the craft, or a failure
// that a second click cannot repair); false means the plain walk-then-click is to follow.
func (w *masterRuneWorld) craftDirect(st runecraftplan.Step, altar, portal locindex.Match) (res runeStepResult, handled bool) {
	if !w.direct.On() {
		return res, false
	}
	a, d := st.Altar, w.d
	// A view that is lagging or was silent a minute ago is not one to plan a route from or to read a verdict out of: the plain way has
	// the lag waits worked out, step by step.
	if runecraftrunner.Shaky(d) {
		return res, false
	}
	plan := w.templePlan(a, altar, portal)
	if plan == nil {
		w.directPlanned(runecraftplan.LegAltar, "no temple walk was planned from the collision pack")
		return res, false
	}
	op, why := runecraftrunner.PlanDirect(d.Pack, runecraftrunner.Here(d), plan.AltarStand)
	if why != "" {
		w.directPlanned(runecraftplan.LegAltar, why)
		return res, false
	}
	if !w.direct.Try(runecraftplan.LegAltar) {
		return res, false
	}
	// A cast rides on the leg when the gate's alchemy has bows to cast (mastercrafter_legcast.go); the leg is this direct op either way.
	var lc *runecraftrunner.LegCast
	if w.castPlan != nil {
		lc = w.castPlan(runecraftplan.LegAltar, op.Steps)
	}
	workStart := time.Now()
	cr := runecraftrunner.CraftDirectCast(d, a, altar, op, lc)
	castFailed := w.legCastScore(runecraftplan.LegAltar, lc, cr.Cast, cr.OK(), cr.Lag)
	switch {
	case cr.OK():
		w.directNote(runecraftplan.LegAltar, runecraftplan.DirectOK, "")
		return w.craftDone(a, cr, workStart), true
	case cr.DirectFailed:
		if lc != nil && cr.Cast.Tried && castFailed {
			// The click was cleared by a cast and never took: that is the cast's failure, scored on its own, and not the direct op's.
			w.legs.noteMode(runecraftplan.LegAltar, runecraftplan.ModeFallback)
		} else {
			w.directNote(runecraftplan.LegAltar, runecraftplan.DirectFailed, cr.Reason)
		}
		// The essence is still in the pack: the plain way takes over in this very step. A second entry marks the cycle RETRIED in the legs line.
		w.legs.enter(st.Stage)
		return res, false
	case cr.Lag:
		w.directNote(runecraftplan.LegAltar, runecraftplan.DirectUnseen, cr.Reason)
	case cr.Tick > 0 && !cr.Verify.OK:
		// The essence left the pack and no experience came: there is nothing left to click, and the leg is judged by it.
		w.directNote(runecraftplan.LegAltar, runecraftplan.DirectFailed, cr.Reason)
	}
	return runeFailf("craft of %d essence: %s", cr.Essence, cr.Reason).withLag(cr.Lag), true
}

// portalDirectPlan is the plan for runecraftrunner.PortalOutCast and the cast the leg may carry, which the plan fills in when it asks for it
// (cast points at a nil *LegCast when none is planned).
type portalDirectPlan struct {
	plan func() (runecraftrunner.DirectOp, *runecraftrunner.LegCast, bool)
	cast **runecraftrunner.LegCast
}

// portalDirect is the plan for runecraftrunner.PortalOutCast: asked once the craft's hold is over, it says whether the portal leg goes
// out as a direct op and with what route, and which cast rides on it. It is nil (the plain PortalOut) for the rounds that have no direct ops
// and for a leave() that knows no craft to time from.
func (w *masterRuneWorld) portalDirect(a runecraftplan.Altar, portal locindex.Match, craftTick int) *portalDirectPlan {
	if !w.direct.On() || craftTick <= 0 || w.direct.GivenUp(runecraftplan.LegPortal) {
		return nil
	}
	d := w.d
	cast := new(*runecraftrunner.LegCast)
	return &portalDirectPlan{cast: cast, plan: func() (runecraftrunner.DirectOp, *runecraftrunner.LegCast, bool) {
		masterDismissChat(w.ctx) // a level-up's congratulations would stop the portal as it stops the bank
		runecraftrunner.AwaitView(d)
		if runecraftrunner.Shaky(d) || runecraftrunner.Reached(d, portal) {
			return runecraftrunner.DirectOp{}, nil, false
		}
		altar, ok := runecraftrunner.FindAltar(d, a)
		if !ok {
			return runecraftrunner.DirectOp{}, nil, false
		}
		plan := w.templePlan(a, altar, portal)
		if plan == nil {
			w.directPlanned(runecraftplan.LegPortal, "no temple walk was planned from the collision pack")
			return runecraftrunner.DirectOp{}, nil, false
		}
		op, why := runecraftrunner.PlanDirect(d.Pack, runecraftrunner.Here(d), plan.PortalStand)
		if why != "" {
			w.directPlanned(runecraftplan.LegPortal, why)
			return runecraftrunner.DirectOp{}, nil, false
		}
		if !w.direct.Try(runecraftplan.LegPortal) {
			return runecraftrunner.DirectOp{}, nil, false
		}
		if w.castPlan != nil {
			*cast = w.castPlan(runecraftplan.LegPortal, op.Steps)
		}
		return op, *cast, true
	}}
}

// portalDirectNote scores the portal leg from what PortalOutCast did. castFailed says a cast rode on the leg and failed: a click it cleared
// that never took is the cast's failure, scored on its own (legCastScore), and not the direct op's.
func (w *masterRuneWorld) portalDirectNote(res runecraftrunner.PortalResult, castFailed bool) {
	switch {
	case res.DirectFailed && res.Cast.Tried && castFailed:
		w.legs.noteMode(runecraftplan.LegPortal, runecraftplan.ModeFallback)
	case res.DirectFailed:
		w.directNote(runecraftplan.LegPortal, runecraftplan.DirectFailed, res.DirectWhy)
	case res.Direct && res.Out:
		w.directNote(runecraftplan.LegPortal, runecraftplan.DirectOK, "")
	case res.Direct && res.Lag:
		w.directNote(runecraftplan.LegPortal, runecraftplan.DirectUnseen, res.Reason)
	}
}
