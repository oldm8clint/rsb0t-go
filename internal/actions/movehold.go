package actions

import (
	"reflect"
	"sync"
	"sync/atomic"
)

// MovementHold gives one caller an account's movement for a while: every Walk and OpLoc sent
// through the account's own Session is dropped -- the same thing the server does with a click
// from a delayed player -- while those sent through Owner go out as usual.
//
// Why it exists (live, 3 October 2026): the maze random event teleports an account in the
// middle of whatever its script was doing, and a script part-way through a long webwalk kept
// walking. In 18 of the 25 failed maze solves that day the account's own walker was clicking
// alongside the Guardian's solver -- "walk to 3222,3218,0: scene/unstick/backoff" toward home,
// "stalled near shut Wall at 2888,4574, opening it" on the maze's wall locs, whose op is a
// mesbox that then swallowed the solver's door clicks -- and dragged the account into dead
// ends, so the solver's walks "stalled" and its approach tiles could not be reached. Waiting
// for every script to notice the Guardian is not enough: a walker only looks between legs.
//
// A hold ends when Release is called or, by itself, the first time a held click finds
// active() false (for the maze: once the account has left the maze's mapsquare). Taking a new
// hold on the same session replaces the old one.
//
// HoldInput is the same hold for a caller that cannot send through Owner because the session is
// the key of a dozen maps along its way (the Guardian's handlers): the holder is the goroutine
// that took it, its clicks go out as usual, and every other goroutine's are dropped. A
// HoldInput hold also drops the interaction clicks (OpNpc, OpObj, OpHeld and the use-on ones),
// not only Walk and OpLoc: see HoldInput.
type MovementHold struct {
	// Owner is the session to send the holder's own clicks through.
	Owner Session

	base Session
	h    *movementHold
}

type movementHold struct {
	active  func() bool
	dropped atomic.Int64
	// owner is the goroutine whose sends are the holder's own, 0 for a hold whose holder sends through
	// MovementHold.Owner instead.
	owner uint64
	// interactions makes the hold drop the interaction clicks too (HoldInput).
	interactions bool
	// exempt, when set, is asked in the sending goroutine: true lets the click through. The Guardian's passes
	// a goroutine that holds the account's action lock (randomevents.Lock), which is the lock protocol the
	// hold enforces for the scripts that do not take it.
	exempt func() bool
}

// holderSession is MovementHold.Owner: the account's session, marked as the holder's.
type holderSession struct{ Session }

var movementHolds sync.Map // base Session -> *movementHold

// HoldMovement takes s's movement for the caller; see MovementHold. active may be nil (held
// until Release). A session whose dynamic type cannot be a map key is never held.
func HoldMovement(s Session, active func() bool) *MovementHold {
	return takeHold(s, &movementHold{active: active})
}

// HoldInput takes s's movement and its interactions for the calling goroutine, the Guardian's, for as
// long as active says so (nil: until Release). Every Walk, WalkOpClick, OpLoc, OpNpc, OpObj, OpHeld
// (but not option 1, Eat, Drink and Open: an account must still be able to eat) and use-on click that
// another goroutine sends through the account's own session is dropped, as HoldMovement drops a Walk,
// and the calling goroutine's go out as usual.
//
// Why: live 18 Oct 2026 20:01-20:03, a ProgressiveFisher at the Fishing Guild bank had a Strange plant
// beside it. The Guardian picked at it and then fled it while the fisher's own bank code clicked the
// booth and walked to the next one over the top of both -- each of those clicks is read by the engine as
// a new interaction (MoveClickHandler and OpNpcHandler both run clearPendingAction) and replaces the
// one before -- and the account died 30 seconds after the plant turned. Every script the Guardian runs
// under has a path that does not wait for it (a bank open, a walker, a shop), so the Guardian takes the
// clicks away while it handles an event instead of asking each of them to look.
//
// The holder is found by goroutine, not by session, because the Guardian's handlers key a dozen maps on
// the session they are given (its registry, the leash, the combat memory) and a wrapper would split them.
// exempt (nil for none) lets a click through that the holder has agreed to: a goroutine holding the
// account's action lock.
func HoldInput(s Session, active func() bool, exempt func() bool) *MovementHold {
	return takeHold(s, &movementHold{active: active, owner: GoroutineID(), interactions: true, exempt: exempt})
}

func takeHold(s Session, h *movementHold) *MovementHold {
	base := s
	if o, ok := s.(holderSession); ok {
		base = o.Session
	}
	if holdable(base) {
		movementHolds.Store(base, h)
	}
	return &MovementHold{Owner: holderSession{base}, base: base, h: h}
}

// Release ends the hold now, if it is still the session's current one.
func (m *MovementHold) Release() {
	if holdable(m.base) {
		movementHolds.CompareAndDelete(m.base, m.h)
	}
}

// Dropped is how many clicks sent through the account's own session the hold has dropped.
func (m *MovementHold) Dropped() int64 { return m.h.dropped.Load() }

// MovementHeldBy reports, without counting a drop, whether another caller holds s's movement now:
// for a sender that batches clicks into one message through a wrapper session, which the hold
// cannot see.
func MovementHeldBy(s Session) bool { return heldAgainst(s, false) != nil }

func holdable(s Session) bool {
	t := reflect.TypeOf(s)
	return t != nil && t.Comparable()
}

// heldAgainst is the hold on s that applies to the calling goroutine now, nil for none: no hold, a hold that has
// ended (dropped from the table as it is found), the holder's own session or goroutine. interaction says the
// click being judged is an interaction one, which only a HoldInput hold drops.
func heldAgainst(s Session, interaction bool) *movementHold {
	if _, own := s.(holderSession); own || !holdable(s) {
		return nil
	}
	v, ok := movementHolds.Load(s)
	if !ok {
		return nil
	}
	h := v.(*movementHold)
	if h.active != nil && !h.active() {
		movementHolds.CompareAndDelete(s, h)
		return nil
	}
	if interaction && !h.interactions {
		return nil
	}
	if h.owner != 0 && GoroutineID() == h.owner {
		return nil
	}
	if h.exempt != nil && h.exempt() {
		return nil
	}
	return h
}

// movementHeld reports whether a Walk or OpLoc sent through s must be dropped, counting it.
func movementHeld(s Session) bool {
	h := heldAgainst(s, false)
	if h == nil {
		return false
	}
	h.dropped.Add(1)
	return true
}

// interactionHeld reports whether an interaction click (OpNpc, OpObj, OpHeld, use-on) sent through s must be
// dropped, counting it. Only a HoldInput hold drops these.
func interactionHeld(s Session) bool {
	h := heldAgainst(s, true)
	if h == nil {
		return false
	}
	h.dropped.Add(1)
	return true
}
