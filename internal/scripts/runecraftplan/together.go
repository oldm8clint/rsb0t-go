package runecraftplan

import (
	"fmt"
	"time"
)

// THE ONE-WRITE ACCEPT (CR-2 of docs/xp_rate_plan_2026-10-19.md, masterspeed step 3, 19 Oct 2026).
//
// Today the crafter accepts the trade's confirm screen, waits to SEE the trade close, and only then sends the talisman on the ruins: one
// round trip of his connection between the close and the click, a tick or two of the 4 the legs log measures from the close to the account
// inside the temple. The accept completes the trade synchronously ([if_button,tradeconfirm:accept] runs both_moveinv and
// if_close in the packet that is the second accept), and the engine reads a websocket message's packets in order in one tick, so a talisman
// click written together with that accept is read the moment the trade is over and runs in the same tick: nothing waits for the round trip.
//
// WHEN IT IS SAFE. Only when the accept is the SECOND one. The click is an op on a loc, and an op closes an open modal the moment it is
// decoded (OpLocUHandler: clearPendingAction), which on a trade screen is [if_close,tradeconfirm]: the offer goes back and the partner is
// told "Other player declined trade." So the crafter writes the pair only after his confirm screen says "Other player has accepted.": the
// carrier's accept has been executed by then, and the screen can only be gone (an accept is not taken back) if the carrier closed it in
// the very tick his write is read. He waits for that line, up to GateTogetherWait, and accepts the plain way when it does not come: the
// trade then completes when the carrier's accept does, exactly as before, so a slow carrier costs nothing.
//
// WHAT THE WAIT COSTS AND BUYS. Without the wait the crafter's accept goes out as soon as he sees the screen and is read one round trip
// of his later; the carrier, on a faster connection, has usually accepted by then, so his accept is the second one and the trade closes in
// that tick. With the wait the trade closes in the tick of his one write -- the same moment or one carrier round trip later -- and the
// talisman is already running. What he gains is the click's round trip (about a tick on the route the main account is pinned to) when the carrier is
// on the faster connection; when he is the faster, nothing is lost.
//
// THE FAILURE. A carrier that closes the screen in the same tick leaves the crafter's accept finding no screen and his click running: he
// teleports into the temple with no essence. That is detected where everything is (the essence did not arrive), counted as a failed trade,
// and the account leaves by the portal like any cycle with nothing to craft. Such a cycle switches the one-write accept off for the next
// TogetherBackoff cycles; TogetherGiveUp in a row switch it off for the round. A click that did nothing (a delayed player drops it) falls
// back to the plain click in the next stage and counts the same.
//
// THE LINE THAT NEVER SHOWS. Everything above rests on the confirm screen saying "Other player has accepted." (tradeconfirm:com_91, read
// from trade.rs2 and the interface pack; no live capture of it exists yet). A server that worded it otherwise, or a component that is not
// the one, would leave every cycle waiting the whole GateTogetherWait and then accepting the plain way, three ticks a cycle for nothing.
// So a wait that runs out is counted: TogetherWaitedOutLimit in a row (no cycle written in between) stand the one-write accept down for
// TogetherBackoff cycles, and a second stand-down with no success between gives it up for the round. A slow carrier now and then never
// reaches the limit.
const (
	// GateTogetherWait is how long the crafter waits on a confirm screen that matches the offer for the carrier's accept to show, with the
	// talisman ready to go out in the same write as his own: three ticks.
	GateTogetherWait = 1800 * time.Millisecond
	// TogetherBackoff is how many cycles accept the plain way after a one-write cycle failed, before it is tried again.
	TogetherBackoff = 10
	// TogetherGiveUp is how many one-write cycles in a row may fail before it is off for the rest of the round.
	TogetherGiveUp = 3
	// TogetherWaitedOutLimit is how many waits in a row may run out before the one-write accept stands down (see above).
	TogetherWaitedOutLimit = 4
)

// TogetherOps is a round's memory of how the one-write accept is doing. A nil *TogetherOps (and one made with on false) never tries one.
type TogetherOps struct {
	on          bool
	fails, wait int
	off         bool
	// waitedRun is the waits in a row that ran out; standDowns the stand-downs since the last cycle that worked.
	waitedRun, standDowns int
	// The round's counts: writes that worked, failed, were not seen; cycles accepted the plain way (the carrier's accept did not show in
	// time, or the cycle was in its back-off).
	ok, failed, unseen, waitedOut, backoff int
}

// NewTogetherOps is the state for a round; on is craftRuneOneWrite.
func NewTogetherOps(on bool) *TogetherOps { return &TogetherOps{on: on} }

// On reports whether the one-write accept is switched on at all.
func (t *TogetherOps) On() bool { return t != nil && t.on }

// Try reports whether this cycle's accept is to go out with the talisman. A cycle in the back-off (or after the give-up) answers false and
// counts the plain cycle it is about to run, so the back-off is measured in cycles. Call it once per cycle, after every other reason to
// accept the plain way has been ruled out.
func (t *TogetherOps) Try() bool {
	if !t.On() {
		return false
	}
	if t.off {
		t.backoff++
		return false
	}
	if t.wait > 0 {
		t.wait--
		t.backoff++
		return false
	}
	return true
}

// NoteWaitedOut records a cycle that was to go out together and accepted the plain way because the carrier's accept did not show in
// GateTogetherWait, and returns a line for the log when it is the TogetherWaitedOutLimit-th in a row (the one-write accept stands down for
// the back-off, the second time for the round). The accept itself was the plain one, which is what the code did before.
func (t *TogetherOps) NoteWaitedOut() string {
	if !t.On() {
		return ""
	}
	t.waitedOut++
	t.waitedRun++
	if t.waitedRun < TogetherWaitedOutLimit {
		return ""
	}
	t.waitedRun = 0
	t.standDowns++
	if t.standDowns >= 2 {
		t.off = true
		return fmt.Sprintf("one-write accept: the carrier's accept was not on the screen after %s in %d trades in a row, twice: accepting the plain way for the rest of the round", GateTogetherWait, TogetherWaitedOutLimit)
	}
	t.wait = TogetherBackoff
	return fmt.Sprintf("one-write accept: the carrier's accept was not on the screen after %s in %d trades in a row: accepting the plain way for the next %d cycle(s)", GateTogetherWait, TogetherWaitedOutLimit, TogetherBackoff)
}

// Note records what a one-write cycle came to and returns a line for the log ("" when nothing is worth saying). why is the failure, in words.
func (t *TogetherOps) Note(o DirectOutcome, why string) string {
	if !t.On() {
		return ""
	}
	// A cycle that was written is one in which the carrier's accept DID show.
	t.waitedRun = 0
	switch o {
	case DirectOK:
		t.ok++
		t.standDowns = 0
		back := t.fails > 0
		t.fails = 0
		if back {
			return "one-write accept: worked again"
		}
	case DirectUnseen:
		t.unseen++
	case DirectFailed:
		t.failed++
		t.fails++
		if t.fails >= TogetherGiveUp {
			t.off = true
			return fmt.Sprintf("one-write accept: failed %d times in a row (%s): accepting and clicking the plain way for the rest of the round", t.fails, why)
		}
		t.wait = TogetherBackoff
		return fmt.Sprintf("one-write accept: failed (%s): accepting and clicking the plain way for the next %d cycle(s)", why, TogetherBackoff)
	}
	return ""
}

// Summary is one line of the round's counts for the log.
func (t *TogetherOps) Summary() string {
	if !t.On() {
		return "one-write accept: off"
	}
	s := fmt.Sprintf("one-write accept: %d worked, %d failed, %d not seen; %d plain (%d waited out, %d in a back-off)", t.ok, t.failed, t.unseen, t.waitedOut+t.backoff, t.waitedOut, t.backoff)
	if t.off {
		s += " (given up)"
	}
	return s
}

// Tried is how many one-write cycles there have been (worked, failed or not seen).
func (t *TogetherOps) Tried() int {
	if t == nil {
		return 0
	}
	return t.ok + t.failed + t.unseen
}

// GivenUp reports whether the one-write accept is plain for the rest of the round.
func (t *TogetherOps) GivenUp() bool { return t != nil && t.off }
