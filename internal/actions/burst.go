package actions

// TWO (OR UP TO FIVE) ACTIONS A TICK. The engine reads at most five USER_EVENT packets a player a
// tick and leaves the rest in the socket buffer, in order, for the next tick (engine NetworkPlayer
// decodeIn, ClientGameProtCategory.USER_EVENT limit 5); a packet it refuses (a click on a slot that no
// longer holds the item, a button on an interface that is not open) counts as a CLIENT_EVENT and does not
// use one of the five. So several independent clicks that content applies with no delay can all go out
// in the tick the first one does, and a caller that sends one, waits for it to show and sends the next
// is spending a round trip (one to two ticks) on every one of them.
//
// SendBurst is the shared sender for that: it writes the packets in order, five user events to a burst,
// and starts the next burst only once a server tick has gone by (or the caller says its results are
// already in), so nothing is queued ahead of the engine. It does not confirm anything: the caller reads
// its own result ONCE afterwards, the way Bank.Batch does (fastbank.go), and retries what fell short the
// old one-at-a-time way.
//
// What may NOT go in a burst together, and why every caller of this file checks it first:
//
//   - two interactions that REPLACE each other (an OPLOC, OPNPC, OPOBJ or a walk click clears the
//     previous interaction; so does an OPHELD whose interface is not the main modal), because only the
//     last would run;
//   - anything whose content script ends in p_delay (high alchemy p_delay(3), a bone buried p_delay(0),
//     a teleport spell, a cooked item): the handlers refuse every later op while the player is delayed,
//     so the second action of the burst is dropped, silently (OpHeldHandler/InvButtonHandler
//     `player.delayed`);
//   - two foods (eating sets %eat_delay to the clock plus 2, and the next bite is refused until it
//     passes; potions and karambwans set none and can share a tick with a bite);
//   - one action that opens a dialog (a make menu, a count dialog) followed by a click that needs the
//     dialog, unless the click is one the server accepts blind (Bank.Batch's Withdraw-X answer).

import (
	"errors"
	"time"
)

// ErrBurstStopped is a burst cut short by a stop request.
var ErrBurstStopped = errors.New("actions: stopped during a burst")

// BurstPacket is one entry of a burst: Events user events that must go out in the same tick (a
// Withdraw-X button and the count answer behind it are 2) and the call that writes them.
type BurstPacket struct {
	Events int
	Send   func() error
}

// BurstResult is what one SendBurst did.
type BurstResult struct {
	// Sent is how many user events went out, Bursts in how many ticks (at most PerTick events each).
	Sent, Bursts int
	// Err is the first send that failed, or ErrBurstStopped.
	Err error
}

// SendBurst writes packets in order, at most perTick user events (0 is BankMaxPerTick) to a burst. A
// burst after the first waits for the next server tick, at most fastChunkWait, or for early to report
// true (the caller's results have already arrived, so the engine has certainly read the burst); w stops
// the wait. It returns as soon as the last packet is written.
func SendBurst(s Session, w Waiter, perTick int, early func() bool, packets []BurstPacket) (res BurstResult) {
	w = waiterOr(w)
	if perTick <= 0 || perTick > BankMaxPerTick {
		perTick = BankMaxPerTick
	}
	st := s.State()
	lastTick := st.Tick()
	for i := 0; i < len(packets); {
		if res.Bursts > 0 {
			deadline := time.Now().Add(fastChunkWait)
			for st.Tick() <= lastTick && time.Now().Before(deadline) {
				if early != nil && early() {
					break
				}
				if !w.Delay(fastPoll) {
					res.Err = ErrBurstStopped
					return res
				}
			}
		}
		lastTick = st.Tick()
		events := 0
		for i < len(packets) && (events == 0 || events+packets[i].Events <= perTick) {
			if err := packets[i].Send(); err != nil {
				res.Err = err
				res.Sent += events
				return res
			}
			events += packets[i].Events
			i++
		}
		res.Sent += events
		res.Bursts++
	}
	return res
}
