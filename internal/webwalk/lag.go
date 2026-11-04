package webwalk

import (
	"fmt"
	"time"
)

// A WALK UNDER A LAGGING VIEW (ogresafe-1017, 17 Oct 2026).
//
// Two things the walk's poll loop assumed and a lagging connection breaks. First, that a tile which has not changed for
// six ticks is a step the server refused. On 17 Oct 12:21:27 two Psiphon tunnels hung for 105 seconds: no byte came
// down, the account's tile never changed, and the walker "stalled", clicked again, "stalled", taught the planner that
// all four exits of the safespot it stood on were blocked (learnBlockedStep), and ran its scene, unstick and backoff
// rungs, every one of them a click from a tile the account had left minutes of server time before. Second, that a click
// sent from the tile the walker sees starts where the player is: the engine walks a click's start tile first
// (actions.Walk), so one sent from a tile the player has already left walks it back along the way it came. The server
// took all of it, in real time.
//
// So a Client that can say its picture is not current (ViewClient) is waited for: no click, no stall, no repath, no
// learned block, no ladder rung. When the picture is current again every timer of the leg starts over and the next click
// is sent from where the account really is. The wait is bounded (lagHoldMax); a walk that cannot get a fresh view in
// that time fails and sends nothing.

// ViewClient is optional on a Client. A Client that implements it can say that what Tile reports is not the player's
// tile now: the connection has gone quiet, or is delivering a backlog. A Client without it is always current.
type ViewClient interface {
	// ViewLagging reports whether the client's picture of the game is behind, and says why (for the log).
	ViewLagging() (lagging bool, why string)
}

// A WALK UNDER A SLOW SERVER (gatecraftlag-1018, 18 Oct 2026).
//
// The hold above waits out a view that is SILENT (no tick for three seconds) or catching up on a backlog. From 01:30 on 18 Oct the
// server also ticked slowly, a second or two apart and never three seconds, which neither test sees, and the stall check of the
// poll loop is a wall-clock one: six ticks' worth of seconds without a new tile. The Nature gate's walks between the temple's
// arrival tile, its altar and its portal "stalled" at 3.6 seconds nineteen times between 01:35 and 05:30 (the logged "stalled at
// 2400,4835,0, clicking again" and its twin at 2399,4839,0), and four of them (02:33, 02:35, 04:41, 05:30) stalled again and taught the
// planner that a step of the 4-tile walk was "blocked live" for two minutes; the 04:41 one began inside a craft's four-tick delay
// ("waiting 1 tick(s) for the craft's hold, 3 click(s)"), where every click is dropped, and the 05:30 one was followed by three
// unconfirmed crafts. A stall is the server's refusal of a step, so it is counted in the server's time: stallTicks game ticks
// without a new tile, on a client that counts them (TickClient).
//
// And a block is not taught on the second stall: it is taught on the third (stallsToLearn), each of them with a click of its own
// from the real tile, because the first stalls of a walk are the ones a click that was read late, or dropped inside the delay of the
// action before it (a craft's four ticks, a teleport's two), produces.

// TickClient is optional on a Client. A Client that implements it counts the game ticks it has read, which lets the walker tell a
// server that is slow from a player that is stuck.
type TickClient interface {
	// ServerTicks is how many game ticks (PLAYER_INFO) the client has read since it logged in.
	ServerTicks() int
}

const (
	// stallTicks is how many game ticks a player must stand still for before the poll loop calls it a stall, on a client that counts
	// them: the stallFor of a server that keeps its pace (six ticks of 600 ms), in the server's own time.
	stallTicks = 6
	// stallValve is the wall time after which a stall counts whatever the tick count says, so a client whose ticks stopped counting
	// (and is not lagging) cannot make a walk wait for ever.
	stallValve = 4 * stallFor
	// stallsToLearn is how many stalls in a row, each answered with a fresh click from the tile the account stands on, teach the
	// planner that the step is blocked (it was two).
	stallsToLearn = 3
)

// serverTicks is the client's tick count, false when it does not count.
func (w *Walker) serverTicks() (int, bool) {
	tc, ok := w.Client.(TickClient)
	if !ok {
		return 0, false
	}
	return tc.ServerTicks(), true
}

// stalledFor reports whether a player whose tile has not changed for held (wall time), and since the client's tick count was
// sinceTick, has stalled: stallFor of wall time AND stallTicks of game time, or stallValve of wall time alone.
func (w *Walker) stalledFor(held time.Duration, sinceTick int) bool {
	if held < stallFor {
		return false
	}
	if ticks, ok := w.serverTicks(); ok && ticks-sinceTick < stallTicks && held < stallValve {
		return false
	}
	return true
}

// stallDetail says how long a stall was in the server's time, for the log: "7 game ticks in 4.2s".
func (w *Walker) stallDetail(held time.Duration, sinceTick int) string {
	if ticks, ok := w.serverTicks(); ok {
		return fmt.Sprintf("%d game ticks in %s", ticks-sinceTick, held.Round(100*time.Millisecond))
	}
	return held.Round(100 * time.Millisecond).String()
}

// lagHoldMax bounds the time one walk waits for a lagging view in total. The 17 Oct stall was 105 seconds.
const lagHoldMax = 3 * time.Minute

// viewLagging asks the Client, which is current when it cannot say.
func (w *Walker) viewLagging() (bool, string) {
	vc, ok := w.Client.(ViewClient)
	if !ok {
		return false, ""
	}
	return vc.ViewLagging()
}

// pause waits d like Client.Delay and, for a wait that is a wait (d > 0), first lets the walk's Watch run (WalkOptions.Watch:
// the eater). Delay(0) is the stop check and watches nothing.
func (w *Walker) pause(d time.Duration) bool {
	if d > 0 && w.watch != nil {
		w.watch()
	}
	return w.Client.Delay(d)
}

// sendWalk sends one movement click through the Client and counts it: a walk that never clicked has nothing to stop.
func (w *Walker) sendWalk(path []Point, run bool) error {
	w.walk.clicks++
	return w.Client.Walk(path, run)
}

// holdForLag waits out a lagging view. held is true when it waited at all (the caller's picture of the account is then
// from before the wait: its timers and its last click mean nothing); stopped when the client was told to stop; gaveUp when
// the walk's total wait reached lagHoldMax. It returns at once, held false, when the view is current.
func (w *Walker) holdForLag() (held, stopped, gaveUp bool) {
	lag, why := w.viewLagging()
	if !lag {
		return false, false, false
	}
	began := w.Client.Now()
	w.logf("view lagging (%s): holding every click and every timer until it catches up", why)
	for {
		if !w.pause(tick) {
			w.noteLagHeld(began)
			return true, true, false
		}
		lag, why = w.viewLagging()
		spent := w.walk.lagHeld + w.Client.Now().Sub(began)
		if !lag {
			w.noteLagHeld(began)
			w.logf("view current again after %s: planning on from the account's real tile", w.Client.Now().Sub(began).Round(100*time.Millisecond))
			return true, false, false
		}
		if spent >= lagHoldMax {
			w.noteLagHeld(began)
			w.logf("view still lagging (%s) after %s: giving the walk up without a click", why, spent.Round(time.Second))
			return true, false, true
		}
	}
}

// noteLagHeld adds the wait that began at began to the walk's total and credits it against the walk's deadline: the
// wait is not walking.
func (w *Walker) noteLagHeld(began time.Time) {
	d := w.Client.Now().Sub(began)
	w.walk.lagHeld += d
	w.walk.lagCredit += d
}

// stopMoving ends an abandoned walk: the last movement click's path is still live on the server (a walk that gave up
// left the account running the 40 tiles it had last been sent along), so a click to the account's own tile, which the
// engine takes as a stop, goes out unless the caller asked to keep the path (WalkOptions.KeepPath). It sends nothing for
// a walk that arrived or was stopped or died, for one that never clicked, or while the view lags: a tile that may be
// behind the player would walk it back to it, which is worse than the path.
func (w *Walker) stopMoving(dest Point, res Result, o WalkOptions) {
	if o.KeepPath || w.walk.clicks == 0 {
		return
	}
	switch res.Outcome {
	case OutcomeArrived, OutcomeStopped, OutcomeDied, OutcomeInterrupted:
		// An interrupted walk's caller (an Until) has found what it walked for: the engine's move click would clear its
		// interaction and close its modal.
		return
	}
	if mc := w.modalClient(); mc != nil && mc.ModalOpen() {
		return // MoveClickHandler closes the open modal (clearPendingAction), a random event's among them
	}
	if lag, why := w.viewLagging(); lag {
		w.logf("walk to %s: %s, and the view is lagging (%s): not sending a stop click from a tile that may be behind the player", dest, res.Outcome, why)
		return
	}
	me, ok := w.Client.Tile()
	if !ok {
		return
	}
	if err := w.Client.Walk([]Point{me}, false); err != nil {
		return
	}
	w.logf("walk to %s: %s: stopped the account on %s so the last click's path does not carry on", dest, res.Outcome, me)
}

// blindRefused reports whether the walk's NoBlind hook forbids the ladder's blind clicks (the scene click straight at the
// destination, the one-tile unstick steps) from where the account stands. Said once a walk.
func (w *Walker) blindRefused(dest Point) bool {
	if w.noBlind == nil {
		return false
	}
	me, ok := w.Client.Tile()
	if !ok || !w.noBlind(me) {
		return false
	}
	if !w.walk.blindSaid {
		w.walk.blindSaid = true
		w.logf("walk to %s: no blind clicks from %s (ground a monster could hunt the account on): waiting instead of clicking straight at the destination", dest, me)
	}
	return true
}
