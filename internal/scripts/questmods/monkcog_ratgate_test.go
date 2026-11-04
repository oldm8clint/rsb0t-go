package questmods

// The Clock Tower's rat gate on a simulated server (RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG; each test skips
// without them): the real cogCrossRatGate, the real FindLoc and OperateLoc over the real loc index and config, the real Dialogue.
//
// Content: [oploc1,ctratgatec] (quest_cog_gates_and_levers.rs2) is
//
//	if (testbit(%cogquest, ^quest_cog_rat_door_bit) = ^true) {
//	    ~mesbox("The death throes of the rats ...");   // ends in p_pausebutton
//	    if_close;
//	    ~open_and_close_door2(...);                     // the account goes through
//	} else { mes("This door does not seem to be openable."); }
//
// so with the rats dead the door opens only once the box has been CONTINUED. The engine's closeModal() drops a script paused on its
// button (Player.closeModal: activeScript = null at PAUSEBUTTON), so a CLOSE_MODAL, whether the immediate clear after the op or the
// next OperateLoc's own pre-op clear, abandons the door and the quest can never finish. The world here is that script: the op raises
// the box a few ticks later, a Continue moves the account into the room, a CLOSE_MODAL drops the script and moves nobody.

import (
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

const (
	// Any chat interface will do: a layer holding the text and the Continue button, as message2 is.
	rgBoxChat, rgBoxText, rgBoxContinue = 4100, 4101, 4102
	rgRefusal                           = "This door does not seem to be openable."
)

// rgRoom is where the door script leaves the account: the white cog's room, west of the gate.
var rgRoom = tile.Tile{X: 2577, Z: 9656}

type ratGateWorld struct {
	t    *testing.T
	sim  *actionsim.Session
	d    *questrunner.Deps
	chat *chatlog.Log

	ratsDead bool // the rat-door bit of %cogquest
	latency  int  // client delays between the op and the box: the tick or more it takes the account to reach the gate and the script to run

	armed, boxUp bool
	wait         int
	ops          int // gate ops the server read
	continues    int // Continues read while the box was up
	closes       int // CLOSE_MODALs read while the box was up
	strayCloses  int // CLOSE_MODALs read with no box up
	sent         []int
}

func newRatGateWorld(t *testing.T, ratsDead bool, latency int) *ratGateWorld {
	t.Helper()
	pk, ix, cfg := csData(t)
	w := &ratGateWorld{t: t, sim: actionsim.New(actionsim.Objs{}, nil), chat: chatlog.New(), ratsDead: ratsDead, latency: latency}
	comps := archive.Components{
		rgBoxChat:     {ID: rgBoxChat, Type: archive.TypeLayer, Children: []int{rgBoxText, rgBoxContinue}},
		rgBoxText:     {ID: rgBoxText, Type: archive.TypeText, Text: "The death throes of the rats seem to have shaken the door"},
		rgBoxContinue: {ID: rgBoxContinue, ButtonType: archive.ButtonContinue, ButtonText: "Click here to continue"},
	}
	w.d = &questrunner.Deps{Session: w.sim, Pack: pk, Locs: ix, Config: cfg, Components: comps, Chat: w.chat,
		Log: func(s string) { t.Log(s) }, Delay: w.delay}
	m, ok := questrunner.FindLoc(w.d, questplan.LocRatGate, "", "Go-through", questplan.RatGateTile, 3)
	if !ok {
		t.Fatalf("no rat gate (loc %d) near %v on the real loc index", questplan.LocRatGate, questplan.RatGateTile)
	}
	var stand tile.Tile
	for _, s := range questrunner.LocStands(w.d, m, questplan.RatGateTile) {
		if !questplan.InCogWhiteRoom(s) {
			stand = s
			break
		}
	}
	if stand == (tile.Tile{}) {
		t.Fatalf("the rat gate has no stand outside the white room")
	}
	if !questplan.InCogWhiteRoom(rgRoom) {
		t.Fatalf("%v is not in the white cog's room", rgRoom)
	}
	w.sim.StandAt(stand.X, stand.Z)
	w.sim.OnSend = w.onSend
	return w
}

// delay is the client's pacing and, with it, the server's ticks: the box arrives after latency of them.
func (w *ratGateWorld) delay(time.Duration) bool {
	if w.armed {
		if w.wait--; w.wait <= 0 {
			w.armed, w.boxUp = false, true
			w.sim.OpenChat(rgBoxChat)
		}
	}
	time.Sleep(time.Millisecond)
	return true
}

func (w *ratGateWorld) onSend(b []byte) {
	for _, op := range actionsim.Opcodes(b) {
		switch op {
		case clientprot.OPLOC1, clientprot.OPLOC2, clientprot.OPLOC3, clientprot.OPLOC4, clientprot.OPLOC5:
			w.ops++
			w.sent = append(w.sent, op)
			if w.ratsDead {
				w.armed, w.wait = true, w.latency
			} else {
				w.chat.Record(rgRefusal)
			}
		case clientprot.RESUME_PAUSEBUTTON:
			w.sent = append(w.sent, op)
			if w.boxUp {
				// The script resumes: if_close, then the door and the account through it.
				w.continues++
				w.boxUp = false
				w.sim.CloseChat()
				w.sim.StandAt(rgRoom.X, rgRoom.Z)
			}
		case clientprot.CLOSE_MODAL:
			w.sent = append(w.sent, op)
			if w.boxUp {
				// closeModal(): the script paused on its button is dropped. Nobody moves.
				w.closes++
				w.boxUp = false
				w.sim.CloseChat()
			} else {
				w.strayCloses++
			}
		}
	}
}

func (w *ratGateWorld) here() tile.Tile { return questrunner.CurrentTile(w.d) }

// With the rats dead the gate's box is continued and the account ends in the room, however many ticks the box takes to arrive and
// whether or not the account had to walk to the gate first.
func TestRatGateBoxIsContinuedNotClosed(t *testing.T) {
	for _, latency := range []int{1, 3, 8} {
		w := newRatGateWorld(t, true, latency)
		if !cogCrossRatGate(w.d) {
			t.Fatalf("latency %d: cogCrossRatGate = false (at %v; ops %d, continues %d, closes %d)", latency, w.here(), w.ops, w.continues, w.closes)
		}
		if !questplan.InCogWhiteRoom(w.here()) {
			t.Errorf("latency %d: the account is at %v, not in the white room", latency, w.here())
		}
		if w.ops != 1 || w.continues != 1 {
			t.Errorf("latency %d: the server read %d gate ops and %d continues, want 1 and 1 (sent %v)", latency, w.ops, w.continues, w.sent)
		}
		if w.closes != 0 || w.strayCloses != 0 {
			t.Errorf("latency %d: %d CLOSE_MODALs dropped the door script and %d more were sent with no box up", latency, w.closes, w.strayCloses)
		}
	}
}

// The account is not left holding a chat: the next step's first op is not stopped by a box still up.
func TestRatGateLeavesNoChatUp(t *testing.T) {
	w := newRatGateWorld(t, true, 2)
	if !cogCrossRatGate(w.d) {
		t.Fatalf("cogCrossRatGate = false at %v", w.here())
	}
	if main, _, chat := w.sim.State().Modals(); main != -1 || chat != -1 {
		t.Errorf("modals left up: main %d, chat %d", main, chat)
	}
}

// A gate whose rats are still alive answers with a game message and no box. The call says so at once, rather than waiting the
// whole cogGateWait for a box that is not coming, and sends no CLOSE_MODAL or Continue at all.
func TestRatGateRefusedFailsAtOnce(t *testing.T) {
	w := newRatGateWorld(t, false, 2)
	start := time.Now()
	if cogCrossRatGate(w.d) {
		t.Fatalf("cogCrossRatGate = true with the rats alive (at %v)", w.here())
	}
	if took := time.Since(start); took > cogGateWait/2 {
		t.Errorf("the refusal took %s: it waited for a box that was never coming (cogGateWait is %s)", took, cogGateWait)
	}
	if w.ops != 1 {
		t.Errorf("the server read %d gate ops, want 1", w.ops)
	}
	if w.continues != 0 || w.closes != 0 || w.strayCloses != 0 {
		t.Errorf("a refusal sent %d continues and %d closes (%d stray); it has no box to answer", w.continues, w.closes, w.strayCloses)
	}
	if questplan.InCogWhiteRoom(w.here()) {
		t.Errorf("the account is in the room though the gate refused")
	}
}

// The world is faithful: a box closed instead of continued drops the door script, which is why the test above proves something. The
// account stays outside the room, and a Continue sent for the box then does nothing.
func TestRatGateWorldDropsTheDoorWhenTheBoxIsClosed(t *testing.T) {
	w := newRatGateWorld(t, true, 1)
	m, ok := questrunner.FindLoc(w.d, questplan.LocRatGate, "", "Go-through", questplan.RatGateTile, 3)
	if !ok {
		t.Fatal("no rat gate")
	}
	if err := questrunner.OperateLoc(w.d, m, "Go-through"); err != nil {
		t.Fatal(err)
	}
	// What the old code did: clear at once (no box yet, a no-op), then, when the box was up, clear again.
	if questrunner.ClearLeftoverModal(w.d) {
		t.Fatal("a modal was up the instant the op was sent: the world has no latency")
	}
	for i := 0; i < 5 && !w.boxUp; i++ {
		w.delay(0)
	}
	if !w.boxUp {
		t.Fatal("the box never came up")
	}
	if !questrunner.ClearLeftoverModal(w.d) {
		t.Fatal("ClearLeftoverModal found no box to close")
	}
	for i := 0; i < 5; i++ {
		w.delay(0)
	}
	if questplan.InCogWhiteRoom(w.here()) {
		t.Fatal("the door opened though the box was closed, not continued")
	}
	if w.closes != 1 || w.continues != 0 {
		t.Errorf("closes %d, continues %d, want 1 and 0", w.closes, w.continues)
	}
}
