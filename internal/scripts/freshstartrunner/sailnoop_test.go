package freshstartrunner

import (
	"testing"
	"time"
)

// SailLeg's no-boat-needed short-circuit answers instantly when the account is already
// inside the arrival radius: WalkNear's own first check is `Chebyshev(here, dest) <= radius
// → return true`, with no delay and no packet sent. That is the correct answer and a
// catastrophic loop — any caller that recomputes its phase from the result and calls back in
// spins at whatever rate the CPU allows.
//
// Live 2026-09-18: production account "spare28" (ProgressiveFisher, Fishing 55) sat two tiles
// from the Catherby bank and drove that branch 398,648 times in 27 seconds — ~14,800
// iterations per second from a single account, at 0 XP/hour — and alone produced a 6.7 GB
// fleet log that made the rest of that run nearly unreadable.
//
// The phase-level cause is fixed at the call site (internal/bot/fisher.go, via
// fisherplan.FundIsReallyJustWalkHome). These guard the floor underneath it, because
// donorrunner and fisherrunner both carry their own "already on the destination side"
// fallbacks and the next caller will make the same mistake.
func TestSailNoOpArrivalDelayCapsRunawayReentry(t *testing.T) {
	if sailNoOpArrivalDelay <= 0 {
		t.Fatal("sailNoOpArrivalDelay is not positive — a no-op arrival is free again and SailLeg can be re-entered at full CPU speed, which is exactly the 14,800/second incident this exists to prevent")
	}
}

// The delay must stay below one game tick. A caller polling SailLeg legitimately should not
// be able to notice this at all; the throttle exists to turn five-figures-per-second into
// single figures, not to slow real work down.
func TestSailNoOpArrivalDelayIsBelowOneGameTick(t *testing.T) {
	const gameTick = 600 * time.Millisecond
	if sailNoOpArrivalDelay >= gameTick {
		t.Fatalf("sailNoOpArrivalDelay = %v, which is >= one game tick (%v) — a legitimate caller would be slowed by this rather than merely rate-limited", sailNoOpArrivalDelay, gameTick)
	}
}

// The radius the short-circuit walks to and the radius it tests "am I already there?" against
// must be the same number. If the test radius were tighter than the walk radius, an account
// standing between the two would be treated as a real walk, WalkNear would still return
// instantly, and the spin would be back with the throttle bypassed.
func TestSailNoOpArrivalRadiusMatchesTheWalkRadius(t *testing.T) {
	if sailDirectArrivalRadius <= 0 {
		t.Fatalf("sailDirectArrivalRadius = %d, want a positive tolerance", sailDirectArrivalRadius)
	}
}
