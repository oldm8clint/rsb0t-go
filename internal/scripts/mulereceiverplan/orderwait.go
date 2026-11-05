package mulereceiverplan

import "time"

// The rules for when a queued hand-out order may be served, and when the mule may stop waiting
// for a trade request because one can be. They live here, in pure functions, so the bot's
// serveOrder / kitDue and the wait loop's early exit read ONE rule and cannot drift apart.
//
// Measured live 5 Nov 2026 (the MuleReceiver, 8,527 s): 36% of the mule's time was spent in
// mulerunner.RunCycle's idle wait after a wall-bus line. A donor that has just handed over is
// still in the announced queue until its cancel reaches the bus a few seconds later, and it orders
// its kit in the same breath. Loop saw "fishers not quiet", declined to serve the kit, and went
// into a whole idleTimeout (60 s) wait; the donor's cancel arrived 2-10 s in, the queue went
// quiet, and the kit still sat there until the wait ran out. Kit age from ask to delivery: median
// 75 s. RunCycle now asks OrderServable every pass and returns as soon as the answer turns yes.

// KitDue is whether the oldest queued kit may be served now: always while no fisher waits, and
// otherwise only once it has waited kitMaxDefer and no kit has just been served (kitTurnOwed).
func KitDue(fishersQuiet, kitTurnOwed bool, oldestKitAge, kitMaxDefer time.Duration) bool {
	if fishersQuiet {
		return true
	}
	return !kitTurnOwed && oldestKitAge >= kitMaxDefer
}

// OrderGate is everything OrderServable reads.
type OrderGate struct {
	// SingleOrders and Kits are the queued hand-out orders of each kind.
	SingleOrders, Kits int
	// KitTurnOwed is set when a hand-out was served and the fishing queue has not yet had its turn.
	KitTurnOwed bool
	// FishersQuiet is whether no hand-off donor is announced and no trade request is waiting.
	FishersQuiet bool
	// OldestKitAge is how long the oldest queued kit has waited; KitMaxDefer the longest a kit
	// waits behind the fishing hand-off.
	OldestKitAge, KitMaxDefer time.Duration
}

// OrderServable mirrors serveOrder: whether the next Loop call would spend itself on a hand-out.
func OrderServable(g OrderGate) bool {
	if g.SingleOrders+g.Kits == 0 {
		return false
	}
	// One hand-out per fishing turn: while a fisher waits, nothing is served until it has had it.
	if g.KitTurnOwed && !g.FishersQuiet {
		return false
	}
	if g.SingleOrders > 0 {
		return true
	}
	return KitDue(g.FishersQuiet, g.KitTurnOwed, g.OldestKitAge, g.KitMaxDefer)
}

// EarlyExitMinGap is the least time between two early exits from the wait. If an exit is followed
// by a Loop call that serves nothing (the two rules disagreed, a bank would not open), the wait
// runs on normally for this long instead of the mule spinning through Loop.
const EarlyExitMinGap = 5 * time.Second

// LeaveWait is whether RunCycle's idle wait should end now so the mule can serve a queued order.
// A trade request already waiting is never delayed by this (RunCycle is about to serve it), an
// open trade is handled before the wait is consulted, and the exit is rate limited.
func LeaveWait(g OrderGate, requestsQueued int, sinceLastExit time.Duration) bool {
	if requestsQueued > 0 || sinceLastExit < EarlyExitMinGap {
		return false
	}
	return OrderServable(g)
}
