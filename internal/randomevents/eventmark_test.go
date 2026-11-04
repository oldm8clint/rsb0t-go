package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/pathfind"
)

// markSession is an actions.Session that is only a distinct, non-zero-size registry key.
type markSession struct{ id int }

func (*markSession) Send(func(w *packet.Packet)) error { panic("not used by this test") }
func (*markSession) State() *gamestate.State           { return gamestate.New() }
func (*markSession) Cheats() *anticheat.Counters       { panic("not used by this test") }

func newMarkGuardian(t *testing.T) (*Guardian, *markSession) {
	t.Helper()
	s := &markSession{id: 1}
	g := NewGuardian(s, nil)
	t.Cleanup(g.Stop)
	return g, s
}

// settled rewinds the Guardian's last-event clock past eventSettle, as waiting would.
func settled(g *Guardian) {
	g.eventEnd.Store(time.Now().Add(-eventSettle - time.Second).UnixNano())
}

// TestActivityIsQuietUntilAnEventRuns: a window that opens and closes with no event handled is not
// disturbed, however long it is open.
func TestActivityIsQuietUntilAnEventRuns(t *testing.T) {
	g, s := newMarkGuardian(t)
	a := ActivityOf(s)
	if a.Disturbed() {
		t.Fatal("a Guardian that has handled nothing reads as disturbed")
	}
	time.Sleep(5 * time.Millisecond)
	if a.Disturbed() {
		t.Fatal("time passing disturbed a window with no event in it")
	}
	if g.Activity().Disturbed() {
		t.Fatal("Guardian.Activity disagrees with ActivityOf")
	}
}

// TestActivityNoticesAnEventHandledInsideTheWindow is the Cow card's Feather x5,014: a window opens,
// the Guardian handles an event (a Maze, a Genie) and finishes, the window closes. Busy would say
// "no event" by then.
func TestActivityNoticesAnEventHandledInsideTheWindow(t *testing.T) {
	g, s := newMarkGuardian(t)
	a := ActivityOf(s)
	end := g.BeginEvent()
	if !a.Disturbed() {
		t.Fatal("an event that began after the window opened was not noticed")
	}
	end()
	if !a.Disturbed() {
		t.Fatal("an event that began and ended inside the window was not noticed once it was over")
	}
	// A window that opens only after that is its own, once the settle has passed.
	settled(g)
	b := ActivityOf(s)
	if b.Disturbed() {
		t.Fatal("a window opened after the event settled is disturbed by it")
	}
	g.BeginEvent()()
	if !b.Disturbed() {
		t.Fatal("the next event was not noticed")
	}
}

// TestActivityOpenedDuringAnEventIsDisturbed: a window that starts while a handler is running (the
// reward has not arrived yet) is wrong however quiet the rest of it is.
func TestActivityOpenedDuringAnEventIsDisturbed(t *testing.T) {
	g, s := newMarkGuardian(t)
	end := g.BeginEvent()
	a := ActivityOf(s)
	if !a.Disturbed() {
		t.Fatal("a window opened while an event was being handled reads as clean")
	}
	end()
	if !a.Disturbed() {
		t.Fatal("it became clean when the event ended")
	}
}

// TestActivityOpenedJustAfterAnEventIsDisturbed: the handler's last check is not the last the pack
// hears of it (RandomEventLoot waits 1.2 s for the same reason), so a window that opens inside
// eventSettle of the end is still not clean, and one that opens after it is.
func TestActivityOpenedJustAfterAnEventIsDisturbed(t *testing.T) {
	g, s := newMarkGuardian(t)
	g.BeginEvent()()
	if !ActivityOf(s).Disturbed() {
		t.Fatal("a window opened the instant an event ended reads as clean")
	}
	g.eventEnd.Store(time.Now().Add(-eventSettle / 2).UnixNano())
	if !ActivityOf(s).Disturbed() {
		t.Fatal("a window opened half a settle after an event ended reads as clean")
	}
	settled(g)
	if ActivityOf(s).Disturbed() {
		t.Fatal("a window opened well after an event ended is still disturbed")
	}
}

// TestEndingAnEventTwiceCountsOnce: kick defers the end and a caller may also call it.
func TestEndingAnEventTwiceCountsOnce(t *testing.T) {
	g, _ := newMarkGuardian(t)
	end := g.BeginEvent()
	end()
	end()
	if got := g.eventSeq.Load(); got != 2 {
		t.Fatalf("eventSeq = %d after begin, end, end; want 2 (the seqlock must come back to even)", got)
	}
}

// TestActivityWithNoGuardianIsNeverDisturbed: a session with no Guardian (a harness, a bare test
// Context) cannot have handled an event, and the zero reading is quiet.
func TestActivityWithNoGuardianIsNeverDisturbed(t *testing.T) {
	if (Activity{}).Disturbed() {
		t.Fatal("the zero Activity is disturbed")
	}
	if ActivityOf(&markSession{id: 2}).Disturbed() {
		t.Fatal("a session with no Guardian is disturbed")
	}
	var g *Guardian
	if g.Activity().Disturbed() {
		t.Fatal("a nil Guardian's reading is disturbed")
	}
}

// TestKickHandlesEachEventInsideAnEventSpan runs the real kick against a real decoded state (the
// account standing in the maze's mapsquare, which Detect reports as a trapped event on its own) with
// the handler stubbed out, and checks what kick does AROUND it: the whole handler runs inside the
// span, and it is over by the time kick returns and releases the account.
func TestKickHandlesEachEventInsideAnEventSpan(t *testing.T) {
	sess := newRecordingSession()
	setTile(sess.state, mazeSquareMX*64, mazeSquareMZ*64, 0)
	g := NewGuardian(sess, nil)
	defer g.Stop()

	var insideReading Activity
	var handled int
	saved := guardianHandler
	guardianHandler = func(s actions.Session, _ *pathfind.Pack, ev *Event, _ int, _ func(string)) error {
		handled++
		insideReading = ActivityOf(s)
		return nil
	}
	defer func() { guardianHandler = saved }()

	before := ActivityOf(sess)
	g.kick()
	if handled != 1 {
		t.Fatalf("the handler ran %d times, want once (kick found no event to handle)", handled)
	}
	if !before.Disturbed() {
		t.Fatal("kick handled an event and a window opened before it was not disturbed")
	}
	if !insideReading.Disturbed() {
		t.Fatal("a reading taken inside the handler reads as clean: the span does not cover it")
	}
	if got := g.eventSeq.Load(); got%2 != 0 || got != 2 {
		t.Fatalf("eventSeq = %d after one handled event, want 2 (begun and ended)", got)
	}
	if g.inFlight.Load() {
		t.Fatal("kick returned still holding the account")
	}

	// A poll that finds nothing to handle does not touch the counter.
	idle := NewGuardian(&markSession{id: 3}, nil)
	defer idle.Stop()
	quiet := idle.Activity()
	idle.kick()
	if quiet.Disturbed() || idle.eventSeq.Load() != 0 {
		t.Fatal("an idle kick counted as event activity")
	}
}

// TestTrapExitIsEventActivity: the mime's reward arrives with the dialogue its exit presses, so a
// window open across it is disturbed too.
func TestTrapExitIsEventActivity(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	before := ActivityOf(sess)
	g.runTrapExit("mime")
	if !before.Disturbed() {
		t.Fatal("finishing a trapped event's exit was not counted as event activity")
	}
	if got := g.eventSeq.Load(); got != 2 {
		t.Fatalf("eventSeq = %d, want 2", got)
	}
}
