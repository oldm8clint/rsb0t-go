package runecraftrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// THE DIRECT-OP LEGS (CR-1; runecraftplan/direct.go has the why and the engine rules).
//
// CraftDirect and PortalOutDirect send the walk to a stand and the click on the loc in ONE write -- a MOVE_OPCLICK with the pack's route,
// then the OPLOC -- and wait for the loc's result in the view's own time. They add nothing the plain Craft and PortalOut do not already
// guard: the waits are lagwait.go's, a click is never sent again for a late answer, and a failure that came out of a lagging view says so.
// What they add is the verdict "the direct op did nothing" (DirectFailed), which is the caller's cue to walk to the stand and click the
// plain way; it is only given when the account's view was keeping up, so a late answer is never mistaken for none.

// DirectOp is one leg's walk, to be sent in the same write as the click it leads to.
type DirectOp struct {
	// Route is the pack's corner-compressed route from the account's tile to the stand. It starts at the account's tile, which
	// actions.WalkOpClick drops. Empty when the account already stands on the stand: only the click goes out then.
	Route []pathfind.NavPoint
	// Steps is the tiles the route walks (the wait for the op to land is stretched by that many ticks), To the stand it ends on.
	Steps int
	To    tile.Tile
	// Run is the click's ctrl flag. False, as the walker's clicks are: the run setting of the account decides how fast the route is
	// walked, so a direct leg spends no more run energy than the plain one did (the run budget is CR-3 step 2).
	Run bool
}

func nav(t tile.Tile) pathfind.NavPoint { return pathfind.NavPoint{X: t.X, Z: t.Z, Level: t.Level} }

// PlanDirect plans the walk of a direct op from the account's tile to a stand: the pack's route, which must END on the stand, be at most
// runecraftplan.DirectMaxSteps long and be walked to its end by the server's own greedy replay of the waypoints it will queue
// (pathfind.Pack.EngineWalk: a route that parks the account against a pillar is refused here and not found out in the temple). why is
// empty when the op is planned. It plans nothing the pack cannot confirm: the caller walks the plain way on any why.
func PlanDirect(pk *pathfind.Pack, from, stand tile.Tile) (op DirectOp, why string) {
	op.To = stand
	if pk == nil {
		return op, "no collision pack"
	}
	if from.Level != stand.Level {
		return op, fmt.Sprintf("the account is on level %d and the stand on %d", from.Level, stand.Level)
	}
	if from == stand {
		return op, ""
	}
	r := pk.FindPath(nav(from), nav(stand))
	if !r.OK || len(r.Waypoints) == 0 {
		return op, "no route from " + fmt.Sprintf("(%d,%d)", from.X, from.Z) + " to the stand: " + r.Reason
	}
	if end := r.Waypoints[len(r.Waypoints)-1]; end != nav(stand) {
		return op, fmt.Sprintf("the route ends at (%d,%d), not on the stand (%d,%d)", end.X, end.Z, stand.X, stand.Z)
	}
	steps := r.Steps()
	if steps > runecraftplan.DirectMaxSteps {
		return op, fmt.Sprintf("the route is %d steps (at most %d)", steps, runecraftplan.DirectMaxSteps)
	}
	// What the engine queues: actions.WalkOpClick drops a first waypoint that is the account's own tile.
	queued := r.Waypoints
	if len(queued) > 1 && queued[0] == nav(from) {
		queued = queued[1:]
	}
	if end, ok := pk.EngineWalk(nav(from), queued, steps+2); !ok || end != nav(stand) {
		return op, fmt.Sprintf("the server's walk of the route would stop at (%d,%d), short of the stand", end.X, end.Z)
	}
	op.Route, op.Steps = r.Waypoints, steps
	return op, ""
}

// sendDirect writes the op's walk and click as ONE websocket message, so the engine reads both in the same tick. click sends the loc click
// through the session it is handed. It reports whether anything was sent: false (and a nil error) when another caller holds the account's
// input (actions.MovementHeld), where the plain click is dropped too.
func sendDirect(d *fleetrunner.Deps, op DirectOp, click func(s actions.Session) error) (sent bool, err error) {
	if actions.MovementHeld(d.Session) {
		return false, nil
	}
	b := actions.NewCoalescer(d.Session)
	s := b.Session()
	want := 1
	if len(op.Route) > 0 {
		if err := actions.WalkOpClick(s, op.Route, op.Run); err != nil {
			b.Discard()
			return false, err
		}
		want++
	}
	if err := click(s); err != nil {
		b.Discard()
		return false, err
	}
	if b.Pending() != want {
		// A part that queued nothing would leave a walk without its click or the other way about.
		b.Discard()
		return false, nil
	}
	if err := b.Flush(); err != nil {
		return false, err
	}
	return true, nil
}

// cantReachSince is whether the game said "I can't reach that" after t.
func cantReachSince(d *fleetrunner.Deps, t time.Time) bool {
	return strings.Contains(strings.ToLower(said(d, t)), "can't reach")
}

// CraftDirect is Craft with the altar click sent in the same write as the walk to the altar's stand (op), the way the real client clicks a
// loc, and verified the same way. The engine runs the click the tick after the last step, so the craft is seen op.Steps+1 ticks after the
// read, with no account-side reaction in between.
//
// DirectFailed (with Reason) is set when the click demonstrably did nothing and the essence is still in the pack: the engine said "I can't
// reach that!", or nothing at all happened by the time the walk and the click were worth, in a view that was keeping up. The caller then
// walks to the stand and clicks the plain way. A refusal by the game itself (no level, no stones), an unseen result in a lagging view,
// and a craft that took the essence but gave no experience are failures of the craft, not of the direct op: DirectFailed is false.
// Nothing is clicked a second time here (CraftDirectCast does, after a cast has cleared the click, and says so in its record).
func CraftDirect(d *fleetrunner.Deps, a runecraftplan.Altar, altar locindex.Match, op DirectOp) CraftResult {
	return CraftDirectCast(d, a, altar, op, nil)
}

// CraftDirectCast is CraftDirect with a cast riding on the leg (lc, legcast.go): the cast is written a tick after the walk and the click,
// which it clears, and the click is sent again after the cast's lock. res.Cast is the ride's record. A leg whose cast failed and left the
// essence in the pack says DirectFailed like any direct leg that did nothing, after the lock is over, so the plain way's first click is not
// dropped; the caller scores the record (res.Cast.Tried with no Landed is a cast that never showed, whatever the leg did).
func CraftDirectCast(d *fleetrunner.Deps, a runecraftplan.Altar, altar locindex.Match, op DirectOp, lc *LegCast) (res CraftResult) {
	st := d.Session.State()
	var note seenNote
	note.lagged = AwaitView(d)
	res.Lag = note.any()
	res.Essence = Essence(d)
	res.Level0, _ = Level(d)
	_, xp0 := Level(d)
	runes0 := PackCount(d, int32(a.RuneID))
	if res.Essence == 0 {
		res.Reason = "no Rune essence in the pack to bind"
		return res
	}
	gone := func() bool { return Essence(d) == 0 }
	click := func(s actions.Session) error { return actions.OpLoc(s, altar.Op, altar.X, altar.Z, altar.ID) }
	from := Here(d)
	at, tick0 := time.Now(), st.Tick()
	sent, err := sendDirect(d, op, click)
	if err != nil {
		res.Reason = "the click could not be sent: " + err.Error()
		return res
	}
	if !sent {
		res.DirectFailed, res.Reason = true, "the walk and the click were held back by another caller"
		return res
	}
	res.Direct, res.Clicks = true, 1
	var ride *legRide
	opTick := 0
	if lc != nil {
		ride = newLegRide(d, runecraftplan.LegAltar, lc, op, from, click, gone)
		defer func() {
			if res.DirectFailed {
				ride.awaitLockEnd()
			}
			res.Cast = ride.rec
			if res.OK() {
				res.Cast.Op = opTick
			}
		}()
	}
	refused := func() bool {
		s := strings.ToLower(said(d, at))
		return strings.Contains(s, "rune stones") || strings.Contains(s, "level of at least")
	}
	budget := craftWait/2 + time.Duration(op.Steps)*tickPace
	if ride != nil {
		budget += ride.extra()
	}
	_, n := waitSeen(d, func() bool {
		if ride != nil {
			ride.step()
		}
		if gone() {
			if opTick == 0 {
				opTick = rideTick(d)
			}
			return true
		}
		return refused() || cantReachSince(d, at)
	}, budget)
	note.add(n)
	res.Lag = note.any()
	if !gone() {
		s := said(d, at)
		low := strings.ToLower(s)
		if s != "" {
			res.Said = s
		}
		switch {
		case strings.Contains(low, "rune stones") || strings.Contains(low, "level of at least") || strings.Contains(low, "members"):
			res.Reason = "the game refused the craft: " + s
			return res
		case strings.Contains(low, "can't reach"):
			res.DirectFailed = true
			res.Reason = "the engine refused the direct click: " + s
			return res
		case note.lagged:
			// The result has not been SEEN in a view that lags: the click may have landed. Wait for the view in its own time and never
			// click again (the altar answers an empty pack with a message box that stops every later loc click: lagwait.go).
			_, n = waitSeen(d, gone, craftWait/2)
			note.add(n)
			res.Lag = note.any()
		default:
			res.DirectFailed = true
			res.Reason = fmt.Sprintf("nothing happened in the %s the walk of %d step(s) and the click were worth", budget, op.Steps)
			if ride != nil {
				res.Reason += " (with a cast on the leg: " + ride.castOpFailed() + ")"
			}
			return res
		}
		if !gone() {
			res.Left = Essence(d)
			res.Reason = fmt.Sprintf("%d essence still in the pack after the direct click", res.Left)
			if res.Said = said(d, at); res.Said != "" {
				res.Reason += " (the game said: " + res.Said + ")"
			}
			res.Reason += " (the view was lagging: the craft may not have been read yet)"
			return res
		}
	}
	return finishCraft(d, a, res, note, xp0, runes0, at, tick0)
}

// PortalOutDirect is PortalOut with the first portal click sent in the same write as the walk to the portal's stand: once the craft's hold
// is over (the first accepted read, craft+5), plan is asked for the op from where the account stands; when it says no (ok false) or the
// direct click did nothing in a view that was keeping up, the plain approach and clicks follow, exactly as PortalOut does them, and the
// result says DirectFailed. An unseen result in a lagging view ends the call without another click.
func PortalOutDirect(d *fleetrunner.Deps, a runecraftplan.Altar, portal locindex.Match, craftTick int, plan func() (DirectOp, bool), approach func() bool) PortalResult {
	if plan == nil {
		return portalOut(d, a, portal, craftTick, approach, nil)
	}
	return portalOut(d, a, portal, craftTick, approach, func() (DirectOp, *LegCast, bool) {
		op, ok := plan()
		return op, nil, ok
	})
}

// PortalOutCast is PortalOutDirect with a cast riding on the leg: plan also says which cast (nil: none). res.Cast is the ride's record.
func PortalOutCast(d *fleetrunner.Deps, a runecraftplan.Altar, portal locindex.Match, craftTick int, plan func() (DirectOp, *LegCast, bool), approach func() bool) PortalResult {
	return portalOut(d, a, portal, craftTick, approach, plan)
}

// directPortal sends the direct op for the portal and waits for it. done is true when the call is over (the account is out, or the result
// was not seen in a lagging view, or the click could not be sent); false when the plain way is to follow. clicks is how many of res.Clicks
// are the direct one.
func directPortal(d *fleetrunner.Deps, a runecraftplan.Altar, portal locindex.Match, op DirectOp, lc *LegCast, out func() bool, res *PortalResult, note *seenNote) (done bool, clicks int) {
	withdraw := expect(d, "runecraft: the "+a.Name+" temple's portal", randomevents.LandingNear(a.Exit.X, a.Exit.Z, 3))
	click := func(s actions.Session) error { return actions.OpLoc(s, portal.Op, portal.X, portal.Z, portal.ID) }
	from := Here(d)
	at := time.Now()
	sent, err := sendDirect(d, op, click)
	if err != nil {
		withdraw()
		res.Reason = "the click could not be sent: " + err.Error()
		return true, 0
	}
	if !sent {
		withdraw()
		res.DirectFailed, res.DirectWhy = true, "the walk and the click were held back by another caller"
		return false, 0
	}
	res.Direct = true
	res.Clicks++
	wait := portalWait/3 + time.Duration(op.Steps)*tickPace
	var ride *legRide
	exitTick := 0
	if lc != nil {
		ride = newLegRide(d, runecraftplan.LegPortal, lc, op, from, click, out)
		wait += ride.extra()
		defer func() {
			if res.DirectFailed {
				ride.awaitLockEnd()
			}
			res.Cast = ride.rec
			if res.Out {
				res.Cast.Op = exitTick
			}
		}()
	}
	_, n := waitSeen(d, func() bool {
		if ride != nil {
			ride.step()
		}
		if out() {
			if exitTick == 0 {
				exitTick = rideTick(d)
			}
			return true
		}
		return cantReachSince(d, at)
	}, wait)
	note.add(n)
	res.Lag = note.any()
	if out() {
		res.Out, res.At = true, Here(d)
		return true, 1
	}
	if note.lagged {
		// Not seen in a view that lags: the click may have landed, so it is not sent again and the plain way does not follow.
		_, n = waitSeen(d, out, portalWait/3)
		note.add(n)
		res.Lag = note.any()
		if out() {
			res.Out, res.At = true, Here(d)
			return true, 1
		}
		res.Reason = fmt.Sprintf("still inside the %s temple after the direct click on the portal (the view was lagging: the exit may not have been read yet)", a.Name)
		return true, 1
	}
	withdraw()
	res.DirectFailed = true
	if cantReachSince(d, at) {
		res.DirectWhy = "the engine said: I can't reach that"
	} else {
		res.DirectWhy = fmt.Sprintf("nothing happened in the %s the walk of %d step(s) and the click were worth", wait, op.Steps)
	}
	return false, 1
}
