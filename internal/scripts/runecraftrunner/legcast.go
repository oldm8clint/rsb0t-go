package runecraftrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runecraftplan"
	"rsb0t-go/internal/scripts/tile"
)

// A CAST RIDING ON A DIRECT-OP LEG (CX-1; runecraftplan/legcast.go has the engine rules, the cost and the score).
//
// The leg is the direct op of CR-1 exactly as it was: the walk to the stand and the click on the loc in one write (T0). On top of it the ride
//
//   - writes the cast (T1) one tick and a margin after T0, by a timer from the write and not by a frame (a frame arrives at any moment of a
//     tick, so a cast keyed to one can be written 100 ms after T0 and be read in T0's own tick, which throws the walk away); the cast
//     clears the click, keeps the walk;
//   - sees the cast land (the bow fell) and notes the frame;
//   - writes the click again (T2), once a frame for LegCastResends frames that end at the first frame whose copy is surely read after the
//     lock, so whatever the round trip is, one copy is the first read the engine accepts and the others are dropped (a click read in the
//     lock is dropped without a word) or repeat it (the same interaction set again; a copy read after the op has run falls in the craft's
//     hold or finds no portal, and is dropped). If the account never moved -- the cast was read in the walk's tick, which threw the route
//     away -- the copy carries a new walk from where it stands;
//   - leaves the leg's completion to the caller's wait, which is the direct op's own, longer by the ticks of the lock.
//
// Nothing is ever waited on that the plain direct op did not wait on: when the cast is not written (the leg is complete first, the write
// fails) the click T0 queued stands, and when the cast is written but never seen to land (refused: no runes) the click is sent again at once
// and the record says Missed. The caller scores the record (runecraftplan.LegCasts) and walks the plain way after a leg that failed.

// LegCast is the cast to ride on one leg.
type LegCast struct {
	Spell runecraftplan.LegSpell
	// Bow is the item cast on (for the record).
	Bow string
	// Send writes the cast (the clear-move and the OPHELDT in one write); it is called once. Landed says the cast shows: the bow fell.
	Send   func() error
	Landed func() bool
}

// PackSlot is the pack slot holding object id (the first, when more than one does): the slot a cast on it names.
func PackSlot(d *fleetrunner.Deps, id int32) (gamestate.SlotItem, bool) {
	for _, it := range packItems(d) {
		if it.ID == id {
			return it, true
		}
	}
	return gamestate.SlotItem{}, false
}

// legTick is the server's tick for the ride's own timer, and legMargin the margin after it (variables so a test can shorten them to its
// engine's tick).
var (
	legTick   = 600 * time.Millisecond
	legMargin = runecraftplan.LegCastMargin
)

// SetLegCastClock replaces the ride's timer (the tick and the margin after it the cast is written) and the frame counter it reads, and
// returns the function that puts the old ones back. TEST SUPPORT, for a test outside this package whose engine ticks faster than the
// server and feeds the client a PLAYER_INFO for every step (actionsim.StandAt); frames may be nil to keep the client's own.
func SetLegCastClock(tick, margin time.Duration, frames func(*fleetrunner.Deps) int) (restore func()) {
	oldTick, oldMargin, oldFrames := legTick, legMargin, rideTick
	legTick, legMargin = tick, margin
	if frames != nil {
		rideTick = frames
	}
	return func() { legTick, legMargin, rideTick = oldTick, oldMargin, oldFrames }
}

// rideTick is the frame the client is in: the PLAYER_INFO packets it has read. A variable because the simulators feed the client a
// PLAYER_INFO for every step they move the account (actionsim.StandAt), which the real server does not; a test of the ride's frames supplies
// its engine's own tick.
var rideTick = func(d *fleetrunner.Deps) int { return d.Session.State().Tick() }

const (
	rideCastWait = iota
	rideLanding
	rideResend
	rideDone
)

// legRide is the state machine of one leg's cast. step is called on every poll of the leg's wait.
type legRide struct {
	d     *fleetrunner.Deps
	lc    *LegCast
	op    DirectOp
	from  tile.Tile
	click func(s actions.Session) error
	// done says the leg is complete (the essence gone, the account outside).
	done func() bool

	t0     time.Time
	sentAt time.Time
	phase  int
	// nextFrame is the first frame a copy of the click goes in, lastFrame the last frame one went in.
	nextFrame, lastFrame int
	rec                  runecraftplan.LegCastRecord
}

// newLegRide starts the ride the moment T0 has been written.
func newLegRide(d *fleetrunner.Deps, leg runecraftplan.Leg, lc *LegCast, op DirectOp, from tile.Tile, click func(s actions.Session) error, done func() bool) *legRide {
	return &legRide{d: d, lc: lc, op: op, from: from, click: click, done: done, t0: time.Now(), phase: rideCastWait,
		rec: runecraftplan.LegCastRecord{Leg: leg, Spell: lc.Spell, Bow: lc.Bow, Steps: op.Steps}}
}

// extra is how much longer than the plain direct op the leg's wait is to be: the lock, the copies of the click and the tick the cast waits.
func (r *legRide) extra() time.Duration {
	return time.Duration(r.lc.Spell.AcceptedAfter()+runecraftplan.LegCastResends+2) * tickPace
}

// step advances the ride by one poll.
func (r *legRide) step() {
	tick := rideTick(r.d)
	if r.rec.FirstStep == 0 && Here(r.d) != r.from {
		r.rec.FirstStep = tick
	}
	switch r.phase {
	case rideCastWait:
		since := time.Since(r.t0)
		if since < legTick+legMargin {
			return
		}
		// A loop that was not looked at for a while (the laptop is at 100% CPU) must not write a cast into a leg that is over or about to be:
		// the cast would lock the account into the next leg's first click. Late by a tick or more, or the leg complete, the cast is not
		// written and T0's click stands.
		if r.done() || since > 2*legTick+legMargin {
			r.rec.Why = fmt.Sprintf("the cast was not written: the ride looked %s after the click (the leg is complete or its cast would be a tick late)", since.Round(10*time.Millisecond))
			r.phase = rideDone
			return
		}
		r.rec.Sent = tick
		if err := r.lc.Send(); err != nil {
			// Nothing was written, so the click T0 queued stands: the leg goes on as the plain direct op.
			r.rec.Why = "the cast could not be sent: " + err.Error()
			r.phase = rideDone
			return
		}
		r.rec.Tried = true
		r.sentAt = time.Now()
		r.phase = rideLanding
	case rideLanding:
		switch {
		case r.lc.Landed():
			r.rec.Landed = tick
			r.nextFrame = tick + r.lc.Spell.AcceptedAfter() - runecraftplan.LegCastResends
			r.phase = rideResend
		case r.refused() || tick >= r.rec.Sent+runecraftplan.LegCastLandTicks:
			// Refused (no rune, no bow at that slot) or lost: nothing locks the account, and a refused cast has cleared the click all the
			// same (OpHeldTHandler clears the pending action before the script's checks), so the click goes again at once.
			r.rec.Missed = true
			r.nextFrame = tick
			r.phase = rideResend
		}
	case rideResend:
		if tick < r.nextFrame || tick == r.lastFrame || r.rec.Resends >= runecraftplan.LegCastResends {
			return
		}
		r.lastFrame = tick
		r.resend()
	}
}

// refused is the game saying the cast was refused (a rune short, a level short, an item that cannot be alched).
func (r *legRide) refused() bool {
	s := strings.ToLower(said(r.d, r.sentAt))
	return strings.Contains(s, "do not have enough") || strings.Contains(s, "level of at least") || strings.Contains(s, "cannot use alchemy")
}

// resend writes one copy of the click: alone when the account has moved (the route is still queued), with a new walk from where it stands
// when it never has (the walk was thrown away).
func (r *legRide) resend() {
	op := DirectOp{To: r.op.To}
	if here := Here(r.d); here == r.from && r.from != r.op.To {
		if o, why := PlanDirect(r.d.Pack, here, r.op.To); why == "" {
			op = o
			r.rec.WalkLost = true
		}
	}
	if sent, err := sendDirect(r.d, op, r.click); err == nil && sent {
		r.rec.Resends++
	}
}

// awaitLockEnd waits until the cast's lock is over before a plain walk-then-click follows a failed leg, so its first click is not dropped.
func (r *legRide) awaitLockEnd() {
	if !r.rec.Tried {
		return
	}
	end := time.Duration(r.lc.Spell.AcceptedAfter()+runecraftplan.LegCastLandTicks) * tickPace
	if r.rec.Landed > 0 {
		waitUntil(r.d, func() bool { return rideTick(r.d) >= r.rec.Landed+r.lc.Spell.AcceptedAfter() }, end)
		return
	}
	waitUntil(r.d, func() bool { return time.Since(r.sentAt) >= end }, end+tickPace)
}

// castOpFailed words why a leg with a cast ended without its op.
func (r *legRide) castOpFailed() string {
	switch {
	case !r.rec.Tried:
		return r.rec.Why
	case r.rec.Missed:
		return "the cast was never seen to land"
	case r.rec.WalkLost:
		return "the walk was thrown away"
	}
	return fmt.Sprintf("the op did not come off after the cast (%d copies of the click)", r.rec.Resends)
}
