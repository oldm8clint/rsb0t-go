package mulereceiverplan

import "time"

// THE COOKED-FOOD BUFFER (a user request): the mule held no cooked food and cooked 28 raw fish
// at a time when asked, so food asks failed on cooking speed, not on raw stock (2.8M raw bass, 6.2M
// raw mackerel, 4.2M raw cod in the bank).
//
// WHY IDLE COOKING ALONE DOES NOT FILL ONE. The mule cooks only when nothing is waiting: no queued
// trade request, no announced donor, no hand-out order. With 17,696 kit asks in 70 hours (about four a
// minute) there is almost never such a moment, so the bank's raw fish stayed raw. One cook round is a
// full pack (28) in about 99 seconds, so the ceiling is about 1,000 cooked a hour with the mule doing
// nothing else, and a fleet kit of 500 food is half an hour of cooking per account: a buffer is a stock
// built over days, not something a queue can wait for.
//
// THE RULE. While the cooked stock is under the buffer, a cook round may run even though hand-out KITS
// are queued, as long as (1) nothing time-critical is waiting (no trade request, no announced donor,
// no single-item order), (2) every queued kit's donor will still be waiting after the round and the
// walk back, and (3) the last such round was not just now (one kit turn between rounds, so the queue
// is served in between). A round that starts here is never cut short by a kit that arrives mid-round:
// a kit waits minutes, a round takes about 100 seconds.

// CookBufferInput is what CookBufferDue reads.
type CookBufferInput struct {
	// Buffer is the cooked-food count to build and hold (setting cookBufferFood); 0 is off.
	Buffer int
	// Cooked is the cooked food (any FoodOptions form) in the bank at its last reading, and
	// CookedKnown whether there has been a reading (an unread bank is not an empty one).
	Cooked      int
	CookedKnown bool
	// Raw is the raw fish the bank held at that reading: nothing to cook means no round.
	Raw int
	// QueuedRequests, Incoming and Orders are the time-critical work waiting: trade requests,
	// announced donors, single-item hand-out orders.
	QueuedRequests, Incoming, Orders int
	// Kits is how many kits are queued; MinKitWait the shortest time any of them will still wait.
	Kits       int
	MinKitWait time.Duration
	// SinceLastBufferRound is the time since a buffer round last ran (a large value when never).
	SinceLastBufferRound time.Duration
	// OnTrip: the mule is away on a pickup trip.
	OnTrip bool
}

// CookRoundDuration is what one cook round and its walks are budgeted at.
const CookRoundDuration = 2 * time.Minute

// CookBufferMinGap is the least time between buffer rounds started over queued kits: one kit turn.
const CookBufferMinGap = 90 * time.Second

// CookBufferDue reports whether a buffer cook round should run now even though kits are queued. With
// no kits queued the ordinary idle-cook gate already decides, so this answers false there (it exists
// only to waive the "no kit waiting" rule, never to cook with a trade request waiting).
func CookBufferDue(in CookBufferInput) bool {
	if in.Buffer <= 0 || in.OnTrip {
		return false
	}
	if !in.CookedKnown || in.Cooked >= in.Buffer || in.Raw <= 0 {
		return false
	}
	if in.QueuedRequests > 0 || in.Incoming > 0 || in.Orders > 0 {
		return false
	}
	if in.Kits == 0 || in.Kits > CookBufferMaxKits {
		return false
	}
	if in.MinKitWait < CookRoundDuration+kitMargin {
		return false
	}
	return in.SinceLastBufferRound >= CookBufferMinGap
}

// CookBufferMaxKits is the most queued kits a buffer round may run in front of. Each round delays every
// kit in the line by about a round, so a long line is served first: live 4 Oct 05:00, with world errands
// waiting in the mule's line up to 30 minutes, a round ran before every kit and the mule handed out one
// kit every 98 s (88 s of cooking, 9 s of hand-out) with 12 queued.
const CookBufferMaxKits = 3

// kitMargin is the slack a kit's donor must keep beyond a round, for the walk back and the hand-out
// itself (a kit's own minimum, kitMinLeft in internal/bot, is two minutes).
const kitMargin = 2 * time.Minute

// CookThroughputPerHour is the cooked food a mule can make in an hour of nothing but cooking: a full
// pack a round.
func CookThroughputPerHour() int {
	rounds := int(time.Hour / CookRoundDuration)
	return rounds * PackSlots
}
