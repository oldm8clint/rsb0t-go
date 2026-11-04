package essencerunner

import (
	"strings"
	"testing"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/serverprot"
)

// A chat left open before a walk is closed first (a courtesy: MoveClickHandler would close it too; the strand of 15 Oct 2026 was
// the miner's own queued clicks, see minesim_test.go).
func TestAWalkClosesALevelUpChatFirst(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2924, 4848)
	d, logs := gatewayDeps(sim)
	closeWalkModal(d)
	if n := len(sim.Sent()); n != 0 {
		t.Fatalf("with no modal open the walk sent %d packet(s)", n)
	}
	sim.State().Handle(serverprot.IF_OPENCHAT, []byte{0x13, 0x12})
	if _, _, chat := sim.State().Modals(); chat == gamestate.NoModal {
		t.Fatal("the test could not open a chat modal")
	}
	closeWalkModal(d)
	if _, _, chat := sim.State().Modals(); chat != gamestate.NoModal {
		t.Errorf("the chat modal %d is still open", chat)
	}
	if len(sim.Sent()) != 1 || len(*logs) == 0 {
		t.Errorf("sent %d packet(s), log %v; want one CLOSE_MODAL and a line", len(sim.Sent()), *logs)
	}
}

// A paused message box ("Your inventory is too full ...", interface 356 message1) is answered with its continue
// button, the way the client does, not CLOSE_MODAL.
func TestAWalkPressesContinueOnAPausedMessageBox(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2924, 4848)
	d, logs := gatewayDeps(sim)
	d.Components = archive.Components{
		356: {ID: 356, Type: archive.TypeLayer, Children: []int{357}},
		357: {ID: 357, ButtonType: archive.ButtonContinue},
	}
	sim.State().Handle(serverprot.IF_OPENCHAT, []byte{0x01, 0x64}) // 356
	closeWalkModal(d)
	if !sim.State().PauseButtonResumed() {
		t.Fatalf("no continue was pressed; log %v", *logs)
	}
	if len(sim.Sent()) != 1 || len(*logs) == 0 || !strings.Contains((*logs)[len(*logs)-1], "pressed continue") {
		t.Errorf("sent %d packet(s), log %v; want one RESUME_PAUSEBUTTON", len(sim.Sent()), *logs)
	}
}
