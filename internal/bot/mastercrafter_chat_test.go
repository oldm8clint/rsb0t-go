package bot

import (
	"testing"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// A chat dialog with a continue button and no random event on (a level-up's congratulations) is
// pressed before the next load; none open is left alone. Live 5 Oct 2026: the main account's Fletching 98
// level-up stopped three bow runs and the bank until a Genie's dialog happened to replace it.
func TestMasterDismissesALevelUpDialog(t *testing.T) {
	s := newAPISession()
	ctx := NewContext(s, "main", nil, nil, nil, archive.Components{
		4000: {ID: 4000, Children: []int{4001, 4002}},
		4001: {ID: 4001, ButtonType: archive.ButtonOK},
		4002: {ID: 4002, ButtonType: archive.ButtonContinue},
	}, mulebus.NewBus(), nil, nil, nil, func(string) {})
	ctx.requestStop() // every wait returns at once
	if masterDismissChat(ctx) || len(s.sent) != 0 {
		t.Fatalf("no dialog open: nothing to press (sent %d)", len(s.sent))
	}
	w := packet.NewWriter(4)
	w.P2(4000)
	s.st.Handle(serverprot.IF_OPENCHAT, w.Data[:w.Pos])
	if !masterDismissChat(ctx) {
		t.Fatal("a level-up dialog was left open")
	}
	if len(s.sent) != 1 || s.sent[0][0] != clientprot.RESUME_PAUSEBUTTON || s.sent[0][1] != 0x0F || s.sent[0][2] != 0xA2 {
		t.Fatalf("sent %v, want RESUME_PAUSEBUTTON on 4002", s.sent)
	}
	if masterDismissChat(ctx) || len(s.sent) != 1 {
		t.Fatal("pressed twice: the continue is one press per open")
	}
}
