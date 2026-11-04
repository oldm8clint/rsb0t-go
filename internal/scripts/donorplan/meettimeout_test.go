package donorplan

import (
	"testing"
	"time"
)

// MeetTimeoutMuleVisible was lowered from 4 minutes to 100s on 2026-09-19 after measuring an
// 85-donor live cohort: 72 hand-off attempts, 20 trades completed, and 13 donors each burning
// the full timeout before giving up -- roughly 52 donor-minutes of fishing lost in twelve
// minutes, while the mule ran flat out at 1.9 trades/min.
//
// These pin the two properties that make that value correct, so a later tuning pass cannot
// silently undo either one.

// The reason this longer timeout exists at all is the mule's idle cooking round, which puts it
// ~8 tiles away at the Catherby range for about 99 seconds. If the timeout ever drops below
// that, donors abandon hand-offs seconds before the mule walks back -- the exact bug the
// constant was introduced to fix. Shortening it for queue reasons must not reintroduce that.
func TestMeetTimeoutMuleVisibleStillCoversACookingRound(t *testing.T) {
	const cookingRound = 99 * time.Second
	if MeetTimeoutMuleVisible <= cookingRound {
		t.Fatalf("MeetTimeoutMuleVisible = %v, which no longer outlasts the mule's ~%v cooking round — donors will abandon hand-offs just before the mule returns, which is the bug this constant exists to prevent",
			MeetTimeoutMuleVisible, cookingRound)
	}
}

// It must stay genuinely bounded and meaningfully shorter than the old 4-minute value.
// Waiting longer cannot fix a throughput limit: a donor queued behind 84 others simply loses
// the wait and leaves anyway, so the time is better spent fishing and returning later.
func TestMeetTimeoutMuleVisibleDoesNotReturnToTheOldCeiling(t *testing.T) {
	const old = 4 * time.Minute
	if MeetTimeoutMuleVisible >= old {
		t.Fatalf("MeetTimeoutMuleVisible = %v, back at or above the measured-bad %v — at fleet scale that converts directly into lost fishing time, not into completed trades",
			MeetTimeoutMuleVisible, old)
	}
}

// The mule-absent timeout must never exceed the mule-visible one: a donor that can actually
// see the mule has strictly more reason to keep waiting than one that cannot.
func TestMeetTimeoutOrderingHolds(t *testing.T) {
	if MeetTimeout > MeetTimeoutMuleVisible {
		t.Fatalf("MeetTimeout (%v, mule NOT visible) exceeds MeetTimeoutMuleVisible (%v, mule visible) — the donor would wait longer for a mule it cannot see than for one it can",
			MeetTimeout, MeetTimeoutMuleVisible)
	}
}
