package actions

import (
	"time"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
)

// HOLD-LOOP CHAT POLICY: what a loop that clicks a target and then watches it (mine a rock, chop a tree, pick flax) does about a
// chat that is, or comes, up. It is the half of the Mine fix (docs/essence_mine_exit_2026-10-15.md) that is not the click gate
// (clickgate.go), moved here so every such loop has the same rules instead of its own copy of them:
//
//   - A chat that was already up when the target was clicked is the click's to close, not an interruption: the click closes any
//     chat on the server (OpLocHandler and its siblings call clearPendingAction), and the client still shows it until the
//     server's IF_CLOSE arrives a tick later. It is ignored until HoldSettleTicks have passed.
//   - A level-up, or any chat with a Continue button, that opens while the loop holds the target is answered in place: Continue
//     once, wait for the close, click the target once more, carry on. The loop is not handed back to a caller that calls it
//     again with the chat still up.
//   - A make menu is the account's own action, never an interruption: it is left alone.
//   - A chat it cannot answer or close after HoldMaxChatClicks clicks, or one that belongs to a random event (Busy), is handed
//     back (HoldHandBack), after a tick's wait, and the caller's next click is spaced by the gate anyway.

const (
	// HoldSettleTicks is how long a chat that was up when the target was clicked is given to go: the click is read in the next
	// tick, the server closes the chat there and IF_CLOSE comes back with that tick's output. A chat still up after this is new.
	HoldSettleTicks = 3
	// HoldAnswerTicks is how long a chat is given to go after its Continue was pressed before the target is clicked to close it.
	HoldAnswerTicks = 3
	// HoldMaxChatClicks is how many times one chat that will not go is closed by clicking the target before it is handed back.
	HoldMaxChatClicks = 3
)

// holdSettleWall is HoldSettleTicks in wall time, for a client whose tick counter has stopped.
const holdSettleWall = HoldSettleTicks * sleepTick

// HoldVerdict is what a hold loop does after HoldChat.Step.
type HoldVerdict int

const (
	// HoldOn: nothing for the loop to do about chats; carry on holding.
	HoldOn HoldVerdict = iota
	// HoldClick: click the target again now, with HoldChat.Click.
	HoldClick
	// HoldHandBack: a chat the loop cannot answer; return "dialog" to the caller.
	HoldHandBack
)

// HoldChat is one hold loop's chat state and gated click. Build one per hold (per call of Mine, Pick, Chop).
type HoldChat struct {
	// Busy reports that a random event owns the chat (randomevents.Busy): its handler answers it, and a click on the target would
	// close it. Nil means never.
	Busy func() bool
	// OnAnswer is called after Continue was pressed on chat, for the caller's log line. Nil is silent.
	OnAnswer func(chat int)
	// Held is called, at most every 30 s per session, when a click has to wait for the gate (see WaitClickGap). Nil is silent.
	Held func(ticks int)

	// Answered counts the chats answered with their Continue button, Clicks the clicks Click sent.
	Answered, Clicks int

	s    Session
	w    Waiter
	st   *gamestate.State
	dlg  *ChatDialog
	key  string
	send func() error

	stale     int // the chat that was up when the target was last clicked: that click closes it
	staleTick int
	staleAt   time.Time

	pressedTick int // the tick Continue was pressed on the chat in front of us, -1 when none
	pressedChat int // and which chat that was: a different one opening next is a new dialogue
	reclicks    int // clicks on the target to close the chat in front of us
}

// NewHoldChat is the chat policy and gated click of a hold on the target key names. send is the click itself (OpLoc, OpNpc, ...),
// called only through Click, after the gate. w is what the gate and the hand-back wait with (nil: plain sleeps).
func NewHoldChat(s Session, w Waiter, components archive.Components, key string, send func() error) *HoldChat {
	return &HoldChat{
		s: s, w: waiterOr(w), st: s.State(), dlg: NewChatDialog(s, components), key: key, send: send,
		stale: gamestate.NoModal, pressedTick: -1, pressedChat: gamestate.NoModal,
	}
}

func (h *HoldChat) chatUp() int {
	_, _, chat := h.st.Modals()
	return chat
}

// Click waits for the gate, notes which chat the click is about to close, and sends the click. ErrClickStopped means the caller
// was stopped while it was held back; any other error is send's.
func (h *HoldChat) Click() error {
	if !WaitClickGap(h.s, h.w, h.key, h.Held) {
		return ErrClickStopped
	}
	h.stale, h.staleTick, h.staleAt = h.chatUp(), h.st.Tick(), time.Now()
	if err := h.send(); err != nil {
		return err
	}
	MarkClick(h.s, h.key)
	h.Clicks++
	return nil
}

// Clicked records a click that went out some other way than Click (a batch that carries the click, a tick-controlled opening): the
// chat then up is that click's to close, and the next gated click is spaced from it.
func (h *HoldChat) Clicked() {
	h.stale, h.staleTick, h.staleAt = h.chatUp(), h.st.Tick(), time.Now()
	MarkClick(h.s, h.key)
	h.Clicks++
}

// settled says whether the chat the last click was closing has had time to go.
func (h *HoldChat) settled() bool {
	return h.st.Tick()-h.staleTick >= HoldSettleTicks || time.Since(h.staleAt) >= holdSettleWall
}

// Step is one look at the chat, once per poll of the hold loop.
func (h *HoldChat) Step() HoldVerdict {
	if chat := h.chatUp(); chat != gamestate.NoModal {
		if h.dlg.IsMakeMenu() {
			// A make menu is the account's own action (the knife-on-logs batch of a chop or fish manipulation, a use on a furnace),
			// open for a tick or two and closed by that action: not an interruption, and a click on the target would close it.
			return HoldOn
		}
		if h.pressedTick >= 0 && chat != h.pressedChat {
			h.pressedTick = -1 // the script went on to another chat (a level-up with an unlock box): that one needs its own Continue
		}
		switch {
		case chat == h.stale && !h.settled():
			// The chat the click is closing: its IF_CLOSE has not come back yet. Not an interruption.
		case h.pressedTick >= 0 && h.st.Tick()-h.pressedTick < HoldAnswerTicks:
			// Continue was pressed; the script finishes in the next tick and closes the chat.
		case h.Busy != nil && h.Busy():
			// A random event's own dialogue: its handler answers it, and a click on the target would close it.
			h.w.Delay(sleepTick)
			return HoldHandBack
		case h.pressedTick < 0 && h.dlg.CanContinue():
			if err := h.dlg.Continue(); err == nil {
				h.pressedTick, h.pressedChat = h.st.Tick(), chat
				h.Answered++
				if h.OnAnswer != nil {
					h.OnAnswer(chat)
				}
			}
		default:
			// Nothing to press, or it did not go: a click on the target closes any chat on the server.
			if h.reclicks >= HoldMaxChatClicks {
				return HoldHandBack
			}
			h.reclicks++
			h.pressedTick = -1
			return HoldClick
		}
		return HoldOn
	}
	if h.pressedTick >= 0 {
		// The chat we answered is gone: its script is finished, the interaction it interrupted carries on by itself (the click set
		// it before the level-up ran), and one click makes sure of it.
		h.pressedTick, h.reclicks = -1, 0
		return HoldClick
	}
	h.reclicks = 0
	return HoldOn
}
