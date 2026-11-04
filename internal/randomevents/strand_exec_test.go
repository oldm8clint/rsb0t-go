package randomevents

import (
	"strings"
	"sync"
	"testing"

	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// recordingSession is an actions.Session over a real decoded gamestate.State that records every packet
// the Guardian's strand recovery sends, instead of putting it on a socket.
type recordingSession struct {
	mu     sync.Mutex
	state  *gamestate.State
	cheats *anticheat.Counters
	sent   [][]byte
}

func newRecordingSession() *recordingSession {
	return &recordingSession{state: gamestate.New(), cheats: &anticheat.Counters{}}
}

func (r *recordingSession) Send(build func(w *packet.Packet)) error {
	w := packet.NewWriter(64)
	build(w)
	r.mu.Lock()
	r.sent = append(r.sent, append([]byte(nil), w.Data[:w.Pos]...))
	r.mu.Unlock()
	return nil
}
func (r *recordingSession) State() *gamestate.State     { return r.state }
func (r *recordingSession) Cheats() *anticheat.Counters { return r.cheats }
func (r *recordingSession) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

// setTileExact is setTile without the multiple-of-8 restriction: the build-area origin is the tile
// rounded down to a zone, and the local offset carries the remainder.
func setTileExact(s *gamestate.State, x, z, level int) {
	zoneX := x/8 + 6
	zoneZ := z/8 + 6
	s.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) {
		p.P2(uint16(zoneX))
		p.P2(uint16(zoneZ))
	}))
	s.Handle(serverprot.PLAYER_INFO, bitPack(3,
		bitField{1, 1},
		bitField{2, 3},
		bitField{2, uint32(level)},
		bitField{7, uint32(x % 8)},
		bitField{7, uint32(z % 8)},
		bitField{1, 0},
		bitField{1, 0},
	))
}

func TestSetTileExactPlacesTheAccount(t *testing.T) {
	s := gamestate.New()
	setTileExact(s, 3281, 3936, 3)
	if x, z, l, ok := s.Tile(); !ok || x != 3281 || z != 3936 || l != 3 {
		t.Fatalf("Tile = %d,%d,%d %v, want 3281,3936,3", x, z, l, ok)
	}
}

// The Guardian's side, end to end up to the packet: on the castle roof the first poll walks to the stair's
// stand tile, the next (standing on it) operates the staircase, and the Guardian is Busy throughout so a
// script's WaitForGuardian holds. An account on ordinary ground costs nothing and is never Busy.
func TestGuardianRecoverStrandWalksThenClicksTheStair(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	var logs []string
	g.Log = func(s string) { logs = append(logs, s) }

	setTileExact(sess.state, 3222, 3218, 0)
	if g.recoverStrand() || g.Busy() || sess.count() != 0 {
		t.Fatal("an account in Lumbridge must cost the strand recovery nothing and never be Busy")
	}

	setTileExact(sess.state, 3282, 3936, 3)
	if !g.recoverStrand() {
		t.Fatal("on the roof the first poll must act")
	}
	if sess.count() != 1 {
		t.Fatalf("first poll sent %d packets, want one walk click", sess.count())
	}
	if !g.Busy() {
		t.Fatal("the Guardian must be Busy while the ladder works, or every script's WaitForGuardian walks straight past it")
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "Rogues' Castle") || !strings.Contains(logs[0], "Planning the way out of the walls") {
		t.Fatalf("no arrival line naming the site: %v", logs)
	}

	setTileExact(sess.state, 3281, 3936, 3)
	if !g.recoverStrand() {
		t.Fatal("on the stand tile the poll must act")
	}
	if sess.count() != 2 {
		t.Fatalf("second poll sent %d packets in all, want the walk and the staircase click", sess.count())
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "Climb-down on loc 1740 at 3280,3936") {
		t.Fatalf("the click was not logged as Climb-down on staircase 1740 at 3280,3936:\n%s", joined)
	}

	// Out of the site: the visit is closed with a line and the Guardian stops being Busy.
	setTileExact(sess.state, 3222, 3218, 0)
	if g.recoverStrand() {
		t.Fatal("outside every site the poll must not act")
	}
	if g.Busy() {
		t.Fatal("still Busy after the account left the site")
	}
	if all := strings.Join(logs, "\n"); !strings.Contains(all, "strand: left Rogues' Castle") {
		t.Fatalf("no 'left the site' line:\n%s", all)
	}
}

// EmergencyFlee, which every script's WaitForGuardian calls every 500ms, steps aside while the ladder has
// the account (strandHolds), and not otherwise.
func TestStrandHoldsOnlyWhileTheLadderIsWorking(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	if strandHolds(sess) {
		t.Fatal("holding before the account has been anywhere")
	}
	setTileExact(sess.state, 3282, 3936, 3)
	g.recoverStrand()
	if !strandHolds(sess) {
		t.Fatal("not holding while the ladder works the castle roof")
	}
	setTileExact(sess.state, 3222, 3218, 0)
	g.recoverStrand()
	if strandHolds(sess) {
		t.Fatal("still holding after the account left the site")
	}
	if strandHolds(newRecordingSession()) {
		t.Fatal("a session with no Guardian cannot be held")
	}
}

// A script's own action sequence holds the lock: the ladder must neither wait on it nor send under it.
func TestGuardianRecoverStrandSkipsAPollWhenTheActionLockIsHeld(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	setTileExact(sess.state, 3282, 3936, 3)
	unlock := g.Lock()
	if g.recoverStrand() {
		t.Fatal("recovery ran while a script held the action lock")
	}
	unlock()
	if sess.count() != 0 {
		t.Fatalf("%d packets were sent under the script's lock", sess.count())
	}
}

// The lair: no grace, straight to the lever's own tile.
func TestGuardianRecoverStrandHeadsForTheDragonsLever(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	var logs []string
	g.Log = func(s string) { logs = append(logs, s) }
	setTileExact(sess.state, 2718, 9816, 0)
	if !g.recoverStrand() || sess.count() != 1 {
		t.Fatalf("lair arrival: acted=%v packets=%d, want one walk click toward the lever", g.Busy(), sess.count())
	}
	setTileExact(sess.state, 2717, 9801, 0)
	g.recoverStrand()
	if all := strings.Join(logs, "\n"); !strings.Contains(all, "Pull on loc 1817 at 2717,9801") {
		t.Fatalf("the lever was not pulled:\n%s", all)
	}
}
