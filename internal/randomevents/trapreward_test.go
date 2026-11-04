package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// The reward-dialogue exit is driven through real decoded state (IF_OPENCHAT, IF_SETICON,
// REBUILD/PLAYER_INFO for the tile) and a captured sender, like overlap_test.go. What the
// "server" does in reply to the continue is the one thing simulated, and it is exactly what
// the lab measured: the mesbox closes and ~initalltabs re-sends tabs 3 and 4.

const (
	testMesbox   = 356  // the chat modal the lab saw ("You can now use the Glass Wall emote!")
	testContinue = 357  // its BUTTON_CONTINUE child
	testText     = 358  // its text line
	testPackTab  = 3213 // what tabs 3 and 4 held again after the press, measured in the lab
	testWornTab  = 1644
)

func rewardArchive() archive.Components {
	return archive.Components{
		testMesbox:   {ID: testMesbox, Children: []int{testText, testContinue}},
		testText:     {ID: testText, Type: archive.TypeText, Text: "You can now use the Glass Wall emote!"},
		testContinue: {ID: testContinue, ButtonType: archive.ButtonContinue},
	}
}

func setIcon(s *gamestate.State, comID, icon int) {
	s.Handle(serverprot.IF_SETICON, payload(func(p *packet.Packet) { p.P2(uint16(comID)); p.P1(uint8(icon)) }))
}

// blindAfterMime is the state Acct188 was in: back home, tabs 3 and 4 null, the emote mesbox
// open. The session's onSend plays the server's part when the continue arrives.
func blindAfterMime(t *testing.T, restoreOnContinue bool) *overlapSession {
	t.Helper()
	st := gamestate.New()
	setTile(st, 2728, 3480, 0) // Seers normal trees, outside both trap mapsquares
	setIcon(st, 65535, 3)
	setIcon(st, 65535, 4)
	st.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(testMesbox) }))
	s := &overlapSession{state: st}
	s.onSend = func(op byte) {
		if op != clientprot.RESUME_PAUSEBUTTON || !restoreOnContinue {
			return
		}
		st.Handle(serverprot.IF_CLOSE, nil)
		setIcon(st, testPackTab, 3)
		setIcon(st, testWornTab, 4)
	}
	previous := currentInterfaceArchive()
	SetInterfaceArchive(rewardArchive())
	t.Cleanup(func() { SetInterfaceArchive(previous) })
	return s
}

func countOp(sent []byte, op byte) int {
	n := 0
	for _, b := range sent {
		if b == op {
			n++
		}
	}
	return n
}

func TestTrapExitPendingExpires(t *testing.T) {
	var te trapExit
	now := time.Now()
	if te.pending(now) != "" {
		t.Fatal("a fresh trapExit must have nothing pending")
	}
	te.note("mime", now)
	if got := te.pending(now.Add(time.Minute)); got != "mime" {
		t.Fatalf("pending a minute later = %q, want mime", got)
	}
	if got := te.pending(now.Add(trapExitExpiry + time.Second)); got != "" {
		t.Fatalf("pending past the expiry = %q, want nothing", got)
	}
	if got := te.pending(now.Add(time.Minute)); got != "" {
		t.Fatal("an expired exit must stay dropped")
	}
}

func TestTrapExitDueOnlyOnceOutOfTheTrapSquare(t *testing.T) {
	st := gamestate.New()
	s := &overlapSession{state: st}
	g := NewGuardian(s, nil)
	defer g.Stop()
	if _, due := g.trapExitDue(); due {
		t.Fatal("nothing noted, nothing due")
	}
	g.trapped.note("mime", time.Now())
	if _, due := g.trapExitDue(); due {
		t.Fatal("an unknown tile must not count as out of the cage")
	}
	setTile(st, 2008, 4760, 0) // inside the mime's mapsquare (31,74)
	if _, due := g.trapExitDue(); due {
		t.Fatal("still inside the mime square: not due yet")
	}
	if !g.Busy() {
		t.Fatal("control: inside the cage the mime itself keeps Busy true, exactly as before this change")
	}
	setTile(st, 2728, 3480, 0)
	if name, due := g.trapExitDue(); !due || name != "mime" {
		t.Fatalf("back home: due=%v name=%q, want the mime's exit due", due, name)
	}
	if !g.Busy() {
		t.Fatal("an exit still to finish must hold Busy, so a script waits for the Guardian to press the reward")
	}
}

// TestKickPressesTheRewardDialogueAfterTheMime is the Acct188 fixture end to end through the
// Guardian's own poll: a mime handled, the return teleport landing between two handling
// attempts (so no "cleared" line), the emote mesbox open and the tabs null. One kick must press
// the continue exactly once, see the tabs come back, and say so.
func TestKickPressesTheRewardDialogueAfterTheMime(t *testing.T) {
	s := blindAfterMime(t, true)
	g := NewGuardian(s, nil)
	defer g.Stop()
	var lines []string
	g.Log = func(m string) { lines = append(lines, m) }
	g.trapped.note("mime", time.Now())

	g.kick()

	if n := countOp(s.sent, clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Fatalf("RESUME_PAUSEBUTTON sent %d times, want exactly 1 (sent % x)", n, s.sent)
	}
	if st := s.state; st.SideIcon(3) != testPackTab || st.SideIcon(4) != testWornTab {
		t.Fatalf("tabs after the press = %d/%d", st.SideIcon(3), st.SideIcon(4))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "random event: mime — pressed continue on the reward dialogue") || !strings.Contains(joined, "tabs back") {
		t.Fatalf("the press must be logged with its outcome; got:\n%s", joined)
	}
	if _, due := g.trapExitDue(); due {
		t.Fatal("the exit must be finished after the press")
	}
	if g.inFlight.Load() {
		t.Fatal("the action lock must be released")
	}
	// Nothing parses the new line as an outcome: the Loot/Random-events panels count only
	// handling/cleared/retry/error/rejected.
	for _, l := range lines {
		if _, outcome, ok := parseOutcomeForTest(l); ok {
			t.Fatalf("line %q parses as outcome %q; it must not", l, outcome)
		}
	}
	// A second kick has nothing left to do.
	g.kick()
	if n := countOp(s.sent, clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Fatalf("a second kick pressed again: %d presses", n)
	}
}

// parseOutcomeForTest mirrors the outcome tails internal/fleet's parsers recognise (fleet.go's
// parseRandomEvent, randomloot.go and phases.go), so this package can assert its new line is
// none of them without importing internal/fleet.
func parseOutcomeForTest(msg string) (string, string, bool) {
	const prefix = "random event: "
	if !strings.HasPrefix(msg, prefix) {
		return "", "", false
	}
	rest := msg[len(prefix):]
	i := strings.Index(rest, " — ")
	if i < 0 {
		return "", "", false
	}
	tail := rest[i+len(" — "):]
	switch {
	case tail == "cleared", tail == "still present after handling", tail == "handling",
		strings.HasPrefix(tail, "handler error: "), strings.HasPrefix(tail, "rejected for this account"):
		return rest[:i], tail, true
	}
	return "", "", false
}

func TestTrapExitWithNoDialogueSendsNothing(t *testing.T) {
	s := blindAfterMime(t, true)
	s.state.Handle(serverprot.IF_CLOSE, nil) // an item reward: no mesbox at all
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.trapped.note("mime", time.Now())
	started := time.Now()
	g.kick()
	if len(s.sent) != 0 {
		t.Fatalf("with no dialogue open nothing may be sent; sent % x", s.sent)
	}
	if took := time.Since(started); took > trapRewardWait+time.Second {
		t.Fatalf("the exit must be bounded: took %s", took)
	}
	if _, due := g.trapExitDue(); due {
		t.Fatal("the exit is finished even when there was nothing to press")
	}
}

func TestSkipTrappedRewardLeavesTheDialogue(t *testing.T) {
	s := blindAfterMime(t, true)
	g := NewGuardian(s, nil)
	defer g.Stop()
	LabSkipTrappedReward.Store(true)
	t.Cleanup(func() { LabSkipTrappedReward.Store(false) })
	g.trapped.note("mime", time.Now())
	g.kick()
	if len(s.sent) != 0 {
		t.Fatalf("the lab knob must leave the dialogue alone; sent % x", s.sent)
	}
}

func TestTrapExitWaitsForTheActionLock(t *testing.T) {
	s := blindAfterMime(t, true)
	g := NewGuardian(s, nil)
	defer g.Stop()
	g.trapped.note("mime", time.Now())
	unlock := g.Lock() // a script mid bank sequence
	g.kick()
	if len(s.sent) != 0 {
		t.Fatal("the exit must not press while a script holds the action lock")
	}
	unlock()
	g.kick()
	if n := countOp(s.sent, clientprot.RESUME_PAUSEBUTTON); n != 1 {
		t.Fatalf("after the lock is released the next poll presses once; got %d", n)
	}
}

// TestBusyHoldsInsideTheCageDuringTheRetryWindow: after "still present after handling" the
// Guardian sets a 2 x pollInterval retry window on the mime's signature, which filters it out of
// nextEvent -- and Busy used to go false with it, so WaitForGuardian let the bot plan walks from
// inside the cage. Standing in the trap square now keeps Busy true on its own.
func TestBusyHoldsInsideTheCageDuringTheRetryWindow(t *testing.T) {
	st := gamestate.New()
	s := &overlapSession{state: st}
	g := NewGuardian(s, nil)
	defer g.Stop()
	setTile(st, 2008, 4760, 0)
	ev := g.nextEvent()
	if ev == nil || ev.Kind != KindTrapped {
		t.Fatalf("control: the mime square is detected as the trapped event, got %+v", ev)
	}
	g.mu.Lock()
	g.retryUntil[eventSignature(ev)] = time.Now().Add(time.Minute)
	g.mu.Unlock()
	if g.nextEvent() != nil {
		t.Fatal("control: the retry window hides the mime from nextEvent")
	}
	if !g.Busy() {
		t.Fatal("inside the cage Busy must stay true through the retry window")
	}
	setTile(st, 2728, 3480, 0)
	if g.Busy() {
		t.Fatal("out of the cage, with nothing noted and nothing detected, Busy is false")
	}
}
