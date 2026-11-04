package rec

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/tickmanip"
)

// fake is a scripted Source: the test moves seen the way PLAYER_INFO would.
type fake struct {
	seen     int
	x, z     int
	anim     int
	animTick int
	energy   int
	main     int
	chat     int
	unset    int
}

func (f *fake) Tick() int                   { return f.seen }
func (f *fake) Tile() (int, int, int, bool) { return f.x, f.z, 0, true }
func (f *fake) SelfAnim() (int, int)        { return f.anim, f.animTick }
func (f *fake) Energy() (int, bool)         { return f.energy, true }
func (f *fake) MapFlagUnsetCount() int      { return f.unset }
func (f *fake) Modals() (int, int, int)     { return f.main, 0, f.chat }

// playerInfo is what the state does on a PLAYER_INFO: the counter moves, then EventTick is emitted.
func (f *fake) playerInfo(r *Recorder) {
	f.seen++
	r.Feed(gamestate.Event{Kind: gamestate.EventTick, Tick: f.seen})
}

// TestAttributionOfAServerCycle replays the packet order of one engine cycle and checks every event lands in the
// row of the tick that produced it: messages arrive before the tick's PLAYER_INFO, inventory and experience after.
func TestAttributionOfAServerCycle(t *testing.T) {
	f := &fake{main: gamestate.NoModal, chat: gamestate.NoModal}
	r := NewFrom(f)

	// Tick 1 is the first one the client counts. A cycle's stream: messages, PLAYER_INFO, inventory, stats.
	f.playerInfo(r) // tick 1's PLAYER_INFO
	// ---- the server now processes tick 2: its script writes a message immediately ----
	r.Feed(gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatGame, Text: "You get some logs."})
	f.anim, f.animTick = 879, 2
	f.energy = 99
	f.playerInfo(r) // tick 2's PLAYER_INFO closes the cycle's processing
	// ---- tick 2's flush continues after its PLAYER_INFO ----
	r.Feed(gamestate.Event{Kind: gamestate.EventInventoryChanged, Comp: 3214, Slot: 3, ItemID: 1511, PreviousID: 0, Count: 1})
	r.Feed(gamestate.Event{Kind: gamestate.EventSkillXP, Skill: 8, XPDelta: 25})
	// ---- tick 3 ----
	f.playerInfo(r)

	two := r.RowAt(2)
	if len(two.Msgs) != 1 || two.Msgs[0] != "You get some logs." {
		t.Errorf("tick 2 messages = %q, want the log message (written before tick 2's PLAYER_INFO)", two.Msgs)
	}
	if net := two.Net(); net[1511] != 1 {
		t.Errorf("tick 2 inventory = %v, want +1 log (written after tick 2's PLAYER_INFO)", net)
	}
	if two.XP[8] != 25 {
		t.Errorf("tick 2 xp = %v, want 25 woodcutting xp", two.XP)
	}
	if two.Anim != 879 {
		t.Errorf("tick 2 animation = %d, want 879", two.Anim)
	}
	if one := r.RowAt(1); len(one.Msgs) != 0 || len(one.Inv) != 0 || one.Anim != tickmanip.NoAnim {
		t.Errorf("tick 1 picked up tick 2's events: %+v", one)
	}
	if three := r.RowAt(3); len(three.Msgs) != 0 || len(three.Inv) != 0 {
		t.Errorf("tick 3 picked up tick 2's events: %+v", three)
	}
	// The energy the client holds at tick 3's PLAYER_INFO is the one tick 2's flush set.
	if two.Energy != 99 {
		t.Errorf("tick 2 energy = %d, want 99", two.Energy)
	}
}

// TestModalIsFiledUnderTheTickBefore: interface opens are written last in the flush, so the client learns one
// PLAYER_INFO later, and the row of the tick that opened it must say Modal.
func TestModalIsFiledUnderTheTickBefore(t *testing.T) {
	f := &fake{main: gamestate.NoModal, chat: gamestate.NoModal}
	r := NewFrom(f)
	f.playerInfo(r) // tick 1
	f.chat = 8880   // the dialogue opened during tick 1's processing; encodeOut writes it after PLAYER_INFO 1
	f.playerInfo(r) // tick 2
	if !r.RowAt(1).Modal {
		t.Error("tick 1 should read Modal: the chat interface was open when its flush ended")
	}
	if r.RowAt(2).Modal {
		t.Error("tick 2 must not read Modal before tick 3's PLAYER_INFO has said so")
	}
	f.playerInfo(r) // tick 3 (still open)
	if !r.RowAt(2).Modal {
		t.Error("tick 2 should read Modal once tick 3 arrived")
	}
}

// TestWaitForReleasesOnArrival: WaitFor returns as soon as the tick lands, and a tick already seen returns at once.
func TestWaitForReleasesOnArrival(t *testing.T) {
	f := &fake{main: gamestate.NoModal, chat: gamestate.NoModal}
	r := NewFrom(f)
	if !r.WaitFor(0, time.Millisecond) {
		t.Fatal("tick 0 is behind the counter and must return at once")
	}
	done := make(chan bool)
	go func() { done <- r.WaitFor(2, time.Second) }()
	time.Sleep(10 * time.Millisecond)
	f.playerInfo(r)
	select {
	case <-done:
		t.Fatal("WaitFor(2) returned after only tick 1")
	case <-time.After(20 * time.Millisecond):
	}
	f.playerInfo(r)
	if ok := <-done; !ok {
		t.Fatal("WaitFor(2) timed out although tick 2 arrived")
	}
	if r.WaitFor(5, 20*time.Millisecond) {
		t.Error("WaitFor of a tick that never arrives must time out")
	}
}

// TestSentFilesUnderTheTickTheServerReadsIt: a packet written after the client saw tick N is read in tick N+1.
func TestSentFilesUnderTheTickTheServerReadsIt(t *testing.T) {
	f := &fake{main: gamestate.NoModal, chat: gamestate.NoModal}
	r := NewFrom(f)
	f.playerInfo(r)
	f.playerInfo(r) // client has seen tick 2
	r.Note(r.NextReadTick(), "knife")
	r.Note(r.NextReadTick(), "make1")
	row := r.RowAt(3)
	if len(row.Sent) != 2 || row.Sent[0] != "knife" || row.Sent[1] != "make1" {
		t.Errorf("tick 3 sent = %v, want [knife make1] in write order", row.Sent)
	}
}

func TestSnapshotCopiesRows(t *testing.T) {
	f := &fake{main: gamestate.NoModal, chat: gamestate.NoModal}
	r := NewFrom(f)
	f.playerInfo(r)
	r.Feed(gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatGame, Text: "a"})
	f.playerInfo(r)
	set := r.Snapshot(2, 1, 2)
	r.Feed(gamestate.Event{Kind: gamestate.EventChatMessage, ChatType: gamestate.ChatGame, Text: "b"})
	if got := set.At(0).Msgs; len(got) != 1 || got[0] != "a" {
		t.Errorf("snapshot row at arm tick 0 = %q, want [a] and unchanged by later events", got)
	}
}

// TestMapUnsetIsCountedPerTick: UNSET_MAP_FLAG packets written while a tick is processed arrive before its
// PLAYER_INFO, so the count between two PLAYER_INFO belongs to the later tick.
func TestMapUnsetIsCountedPerTick(t *testing.T) {
	f := &fake{main: gamestate.NoModal, chat: gamestate.NoModal}
	r := NewFrom(f)
	f.playerInfo(r) // tick 1
	f.unset += 2    // the server refused two clicks while processing tick 2
	f.playerInfo(r) // tick 2
	f.playerInfo(r) // tick 3
	if got := r.RowAt(2).MapUnset; got != 2 {
		t.Errorf("tick 2 MapUnset = %d, want 2", got)
	}
	if got := r.RowAt(3).MapUnset; got != 0 {
		t.Errorf("tick 3 MapUnset = %d, want 0", got)
	}
}
