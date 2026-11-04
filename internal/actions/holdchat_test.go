package actions

import (
	"testing"
	"time"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/protocol/clientprot"
)

const (
	testLevelUpChat = 4416
	testLevelUpBtn  = 4440
)

func levelUpComponents() archive.Components {
	return archive.Components{
		testLevelUpChat: {ID: testLevelUpChat, Type: archive.TypeLayer, Children: []int{testLevelUpBtn}},
		testLevelUpBtn:  {ID: testLevelUpBtn, ButtonType: archive.ButtonContinue},
	}
}

// chatRig is the server's side of a hold against a level-up chat: the click and the Continue each close the chat in the next tick
// (OpLocHandler's clearPendingAction; the pause button finishing its script), unless the chat is stubborn.
type chatRig struct {
	*gateRig
	stubborn  bool // neither the click nor the Continue closes it
	closeAt   int  // the tick the pending close lands in, 0 none
	clicks    int
	continues int
}

func newChatRig() *chatRig {
	c := &chatRig{gateRig: newGateRig()}
	c.f.afterSend = func(f *fakeSession) {
		c.sentAt = append(c.sentAt, f.state.Tick())
		switch tailOp(f.lastSent()) {
		case clientprot.RESUME_PAUSEBUTTON:
			c.continues++
			if !c.stubborn && c.closeAt == 0 {
				c.closeAt = f.state.Tick() + 1
			}
		case clientprot.OPLOC1, clientprot.OPNPC1, clientprot.OPOBJ3:
			c.clicks++
			if !c.stubborn && c.closeAt == 0 {
				c.closeAt = f.state.Tick() + 1
			}
		}
	}
	c.onTick = func() {
		if c.closeAt != 0 && c.f.state.Tick() >= c.closeAt {
			c.closeAt = 0
			closeModal(c.f.state)
		}
	}
	return c
}

// tailOp is the opcode of the packet a message ends with (anti-cheat side packets ride in front of a click).
func tailOp(b []byte) int {
	for _, c := range []struct{ op, n int }{
		{clientprot.OPLOC1, 7}, {clientprot.OPNPC1, 3}, {clientprot.OPOBJ3, 7}, {clientprot.RESUME_PAUSEBUTTON, 3},
	} {
		if len(b) >= c.n && int(b[len(b)-c.n]) == c.op {
			return c.op
		}
	}
	return -1
}

func (c *chatRig) hold(busy func() bool) *HoldChat {
	h := NewHoldChat(c.f, c, levelUpComponents(), LocKey(10, 10, 7), func() error { return OpLoc(c.f, 1, 10, 10, 7) })
	h.Busy = busy
	return h
}

// run plays a hold loop: one Step per poll, a click when it says so, until it hands the chat back or polls runs out.
func (c *chatRig) run(h *HoldChat, polls int, poll time.Duration) (handedBack bool) {
	for i := 0; i < polls; i++ {
		switch h.Step() {
		case HoldClick:
			if err := h.Click(); err != nil {
				return false
			}
		case HoldHandBack:
			return true
		}
		c.Delay(poll)
	}
	return false
}

// A level-up that opens mid-hold is answered with its Continue, the chat closes, the target is clicked once more and the loop
// carries on: not one click while the chat was up.
func TestALevelUpIsAnsweredInPlaceAndTheTargetClickedOnce(t *testing.T) {
	c := newChatRig()
	h := c.hold(nil)
	if err := h.Click(); err != nil { // the hold's first click
		t.Fatal(err)
	}
	c.closeAt = 0
	c.Delay(2 * time.Second)
	openChat(c.f.state, testLevelUpChat)
	if c.run(h, 30, 600*time.Millisecond) {
		t.Fatal("the chat was handed back though it has a Continue button")
	}
	if h.Answered != 1 || c.continues != 1 {
		t.Errorf("answered %d chat(s) with %d Continue(s); want one of each", h.Answered, c.continues)
	}
	if h.Clicks != 2 || c.clicks != 2 {
		t.Errorf("%d click(s) sent (%d by the hold); want the first and one after the chat closed", c.clicks, h.Clicks)
	}
	if got := c.perTick(); got > 1 {
		t.Errorf("%d packets in one tick", got)
	}
}

// A chat that was up when the target was clicked is the click's to close: no Continue, no second click.
func TestAChatUpWhenTheTargetIsClickedIsLeftToTheClick(t *testing.T) {
	c := newChatRig()
	openChat(c.f.state, testLevelUpChat)
	h := c.hold(nil)
	if err := h.Click(); err != nil {
		t.Fatal(err)
	}
	if c.run(h, 20, 600*time.Millisecond) {
		t.Fatal("handed back")
	}
	if h.Answered != 0 || c.continues != 0 {
		t.Errorf("answered %d chat(s), sent %d Continue(s); the click closes the chat and the loop must not press anything", h.Answered, c.continues)
	}
	if c.clicks != 1 {
		t.Errorf("%d click(s); want the one", c.clicks)
	}
}

// A chat the click cannot close and Continue does not finish: one Continue, then at most a click every other tick, three of them,
// then it is handed back. Never a flood.
func TestAChatThatStaysUpIsClickedAtMostEveryOtherTickThenHandedBack(t *testing.T) {
	c := newChatRig()
	c.stubborn = true
	h := c.hold(nil)
	if err := h.Click(); err != nil {
		t.Fatal(err)
	}
	c.Delay(2 * time.Second)
	openChat(c.f.state, testLevelUpChat)
	// A caller with nothing to wait on polls every 100 ms: six looks a tick.
	if !c.run(h, 400, 100*time.Millisecond) {
		t.Fatalf("a chat that never goes was never handed back (%d click(s), %d Continue(s))", c.clicks, c.continues)
	}
	if c.continues != 1 {
		t.Errorf("%d Continue(s); one per open chat", c.continues)
	}
	if c.clicks != 1+HoldMaxChatClicks {
		t.Errorf("%d click(s); want the first and %d to close the chat", c.clicks, HoldMaxChatClicks)
	}
	if got := c.perTick(); got > 1 {
		t.Errorf("%d packets in one tick; a chat that stays up must not make the loop click faster", got)
	}
	if got := c.minGap(); got < 1 {
		t.Errorf("two packets in the same tick")
	}
	// sentAt[0] is the first click, [1] the Continue, the rest the clicks that try to close the chat.
	for i := 3; i < len(c.sentAt); i++ {
		if c.sentAt[i]-c.sentAt[i-1] < ClickGapTicks {
			t.Errorf("packets at ticks %v: two clicks within %d tick(s)", c.sentAt, ClickGapTicks)
		}
	}
}

// A tight loop that calls Step and Click with no wait of its own, against a chat that stays up for ever, cannot send more than
// one click a tick: the gate does the waiting.
func TestATightLoopAgainstAStubbornChatCannotFlood(t *testing.T) {
	c := newChatRig()
	c.stubborn = true
	h := c.hold(nil)
	openChat(c.f.state, testLevelUpChat)
	for i := 0; i < 2000; i++ {
		switch h.Step() {
		case HoldClick:
			if err := h.Click(); err != nil {
				t.Fatal(err)
			}
		case HoldHandBack:
			// handed back: the caller returns "dialog" and its caller calls it again at once, with the same chat up
			h = c.hold(nil)
		}
		c.advance(20 * time.Millisecond) // the only time that passes is the loop's own work
	}
	t.Logf("%d click(s) and %d Continue(s) in %v of game time, at most %d packet(s) in a tick", c.clicks, c.continues, c.clock, c.perTick())
	if got := c.perTick(); got > 2 {
		t.Errorf("%d packets in one tick from a loop with nothing to wait on; the gate and the one-Continue latch allow a click and a Continue", got)
	}
	if c.clicks > 2000*20*int(time.Millisecond)/int(sleepTick*ClickGapTicks)+2 {
		t.Errorf("%d click(s) in %v of game time; more than one per %d ticks", c.clicks, 2000*20*time.Millisecond, ClickGapTicks)
	}
}

func TestARandomEventsChatIsHandedBackAfterATickAndNeverClicked(t *testing.T) {
	c := newChatRig()
	h := c.hold(func() bool { return true })
	openChat(c.f.state, testLevelUpChat)
	before := c.f.state.Tick()
	if got := h.Step(); got != HoldHandBack {
		t.Fatalf("verdict %v, want a hand back", got)
	}
	if c.f.state.Tick() == before {
		t.Error("the hand back did not wait a tick")
	}
	if c.clicks != 0 || c.continues != 0 {
		t.Errorf("%d click(s) and %d Continue(s) at a random event's dialogue", c.clicks, c.continues)
	}
}

func TestAHoldWithNoChatDoesNothing(t *testing.T) {
	c := newChatRig()
	h := c.hold(nil)
	if err := h.Click(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if got := h.Step(); got != HoldOn {
			t.Fatalf("verdict %v with no chat", got)
		}
		c.Delay(600 * time.Millisecond)
	}
	if c.clicks != 1 || c.continues != 0 {
		t.Errorf("%d click(s), %d Continue(s)", c.clicks, c.continues)
	}
}

func TestAStopDuringAHoldClickEndsIt(t *testing.T) {
	c := newChatRig()
	h := c.hold(nil)
	if err := h.Click(); err != nil {
		t.Fatal(err)
	}
	c.stopped = true
	if err := h.Click(); err != ErrClickStopped {
		t.Errorf("a click held back by the gate and stopped returned %v", err)
	}
}

// A make menu (a chop or fish manipulation's knife-on-logs batch opens one for a tick) is the account's own action: the hold does not
// press, click or hand it back.
func TestAMakeMenuIsNotAnInterruption(t *testing.T) {
	c := newChatRig()
	f, md := makeMenu()
	h := NewHoldChat(f, c, md.archive, LocKey(10, 10, 7), func() error { return OpLoc(f, 1, 10, 10, 7) })
	for i := 0; i < 20; i++ {
		if got := h.Step(); got != HoldOn {
			t.Fatalf("verdict %v at poll %d for a make menu", got, i)
		}
		c.Delay(600 * time.Millisecond)
	}
	if len(f.sent) != 0 {
		t.Errorf("%d packet(s) sent against a make menu", len(f.sent))
	}
}
