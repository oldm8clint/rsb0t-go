package fleetplan

// How long a ScarabSwarm ask may stand in the mule's queue (mulequeue, 2026-09-26).
//
// LIVE, build 26b2816f, 16:30-17:24 26 Sep (the last 20 MB of run/fleet.log): 380 asks gave up on
// the queue, "the mule has N donor(s), kit(s) and order(s) ahead at about 1m35s each: longer than this
// ask's 1m30s" -- 169 of them with ONE entry ahead, 126 with two. The gate compared queue x cycle
// with the ask's 90-second Timeout, so a single entry ahead (1m35s) was always "too long", and the
// account fell back to a shop trip (the Port Sarim ship to Lundail, about 20 minutes) or rested
// unfed. Twelve world errands paid both hops and then refused at the post on the same gate.
//
// Two things were wrong with that comparison:
//
//  1. The ask's Timeout is not how long the order waits. A bundled kit order (the default,
//     muleKitOrder) waits at least donorrunner.DefaultKitTimeout, five minutes, whatever the
//     caller's Timeout (scarabPlaceMuleOrder). The gate refused waits the order would have kept.
//  2. The mule's quoted cycle was measured only between DONOR trades (13 in that hour), so it
//     spanned every kit hand-out and cook round in between. The mule's own log for the same hour:
//     125 trades, a kit served a median 20 s (p90 42 s) after it was queued, 4 s from draw to
//     delivery. mule.go now folds hand-outs into the measurement too; this file is the reader's half.
//
// The rule: an ask may wait as long as its order will actually wait, but never longer than its
// fallback would take (a fallback that is quicker than the queue wins), and never less than the
// caller's own Timeout.

import "time"

// ShopFallbackCost is what a declined Stage 3 supply ask costs the account instead: the rune shop
// trip (Port Sarim ship to Lundail and back, about 20 minutes live) or, for food, resting unfed.
// Every realistic mule queue is shorter, so it caps nothing today; it keeps the budget honest if a
// caller's order wait is ever raised past what the fallback costs.
const ShopFallbackCost = 20 * time.Minute

// MuleQueueBudget is how long an ask may stand in the mule's queue: the longer of the caller's
// ask timeout and the wait its order actually keeps (orderWait), capped by what the fallback costs
// (fallback <= 0: no cap). It is never below askTimeout.
func MuleQueueBudget(askTimeout, orderWait, fallback time.Duration) time.Duration {
	budget := max(askTimeout, orderWait)
	if fallback > 0 && fallback < budget {
		budget = max(fallback, askTimeout)
	}
	return budget
}

// MuleQueueWait is the wait the mule's published queue implies (queue entries x its cycle), and
// whether it is longer than budget. A non-positive queue or cycle (nothing published) is never too
// long: the mule is an accelerator, and "unknown" is not a reason to go to the shop.
func MuleQueueWait(queue int, cycle, budget time.Duration) (wait time.Duration, tooLong bool) {
	if queue <= 0 || cycle <= 0 {
		return 0, false
	}
	wait = time.Duration(queue) * cycle
	return wait, wait > budget
}
