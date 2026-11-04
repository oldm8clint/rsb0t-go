package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// The exact packet sequence content 289 sends for ONE page of an NPC dialogue
// (content/scripts/interface_chat/scripts/chat.rs2, [proc,chatnpc_page], the one-line arm):
// if_setnpchead(npcchat1:com_0, $npc), if_setanim(npcchat1:com_0, anim), two if_settext, then
// if_openchat(npcchat1). Component ids are the ones a live Lost City engine really used for
// Hans on 2026-09-21: chat modal 4882, head 4883, name 4884, line 4885.
func TestNpcDialoguePageIsFullyDecoded(t *testing.T) {
	s := New()
	s.Handle(serverprot.IF_SETNPCHEAD, payload(func(p *packet.Packet) { p.P2(4883); p.P2(0) }))
	s.Handle(serverprot.IF_SETANIM, payload(func(p *packet.Packet) { p.P2(4883); p.P2(591) }))
	s.Handle(serverprot.IF_SETTEXT, payload(func(p *packet.Packet) { p.P2(4884); p.PJStr("Hans") }))
	s.Handle(serverprot.IF_SETTEXT, payload(func(p *packet.Packet) {
		p.P2(4885)
		p.PJStr("Hello. What are you doing here?")
	}))
	s.Handle(serverprot.IF_OPENCHAT, payload(func(p *packet.Packet) { p.P2(4882) }))

	head, ok := s.InterfaceHeadOf(4883)
	if !ok || head.Type != 2 || head.ID != 0 {
		t.Fatalf("head = %+v, ok=%v; want an npc head (type 2) of npc 0", head, ok)
	}
	ui := renderUIOf(s)
	if ui.ChatModal != 4882 {
		t.Fatalf("chat modal = %d, want 4882", ui.ChatModal)
	}
	if got := ui.Heads[4883]; got.Type != 2 || got.ID != 0 {
		t.Fatalf("RenderUI head = %+v, want {2 0}", got)
	}
	if got := ui.Anims[4883]; got != 591 {
		t.Fatalf("RenderUI anim = %d, want 591", got)
	}
	if ui.Texts[4884] != "Hans" {
		t.Fatalf("name text = %q", ui.Texts[4884])
	}
}

func TestIfSetPlayerHeadRecordsTheLocalPlayer(t *testing.T) {
	s := New()
	s.Handle(serverprot.IF_SETPLAYERHEAD, payload(func(p *packet.Packet) { p.P2(4901) }))
	head, ok := s.InterfaceHeadOf(4901)
	// The reference client packs the local player's own appearance into model1Id, which
	// needs the decoded appearance block rather than this packet; the renderer builds it
	// from the player it has already placed, so only the KIND travels here.
	if !ok || head.Type != 3 || head.ID != 0 {
		t.Fatalf("head = %+v, ok=%v; want a player head (type 3)", head, ok)
	}
}

func TestIfSetAnimMinusOneIsKept(t *testing.T) {
	s := New()
	s.Handle(serverprot.IF_SETANIM, payload(func(p *packet.Packet) { p.P2(300); p.P2(0xFFFF) }))
	ui := renderUIOf(s)
	if got, ok := ui.Anims[300]; !ok || got != -1 {
		t.Fatalf("anim = %d,%v; want -1 (the client's own \"stop animating\" value)", got, ok)
	}
}

func TestIfSetHide(t *testing.T) {
	s := New()
	if _, ok := s.InterfaceHidden(1234); ok {
		t.Fatal("hidden known before IF_SETHIDE")
	}
	s.Handle(serverprot.IF_SETHIDE, payload(func(p *packet.Packet) { p.P2(1234); p.P1(1) }))
	if hidden, ok := s.InterfaceHidden(1234); !ok || !hidden {
		t.Fatalf("hidden = %v,%v; want true", hidden, ok)
	}
	s.Handle(serverprot.IF_SETHIDE, payload(func(p *packet.Packet) { p.P2(1234); p.P1(0) }))
	if hidden, ok := s.InterfaceHidden(1234); !ok || hidden {
		t.Fatalf("hidden = %v,%v; want false", hidden, ok)
	}
}

func TestIfOpenOverlayIsItsOwnSlot(t *testing.T) {
	s := New()
	if got := s.OverlayModalID(); got != NoModal {
		t.Fatalf("overlay = %d before IF_OPENOVERLAY, want %d", got, NoModal)
	}
	s.Handle(serverprot.IF_OPENMAIN, payload(func(p *packet.Packet) { p.P2(5292) }))
	s.Handle(serverprot.IF_OPENOVERLAY, payload(func(p *packet.Packet) { p.P2(197) }))
	if got := s.OverlayModalID(); got != 197 {
		t.Fatalf("overlay = %d, want 197", got)
	}
	// An overlay is drawn over the viewport ALONGSIDE the main modal, never instead of it.
	if s.MainModalID != 5292 {
		t.Fatalf("main modal = %d, want 5292 (an overlay must not close it)", s.MainModalID)
	}
	ui := renderUIOf(s)
	if ui.Overlay != 197 {
		t.Fatalf("RenderUI overlay = %d, want 197", ui.Overlay)
	}
	s.Handle(serverprot.IF_OPENOVERLAY, payload(func(p *packet.Packet) { p.P2(0xFFFF) }))
	if got := s.OverlayModalID(); got != -1 {
		t.Fatalf("overlay = %d after -1, want -1", got)
	}
}

func TestIfSetScrollPosAndPosition(t *testing.T) {
	s := New()
	s.Handle(serverprot.IF_SETSCROLLPOS, payload(func(p *packet.Packet) { p.P2(6969); p.P2(240) }))
	s.Handle(serverprot.IF_SETPOSITION, payload(func(p *packet.Packet) { p.P2(70); p.P2(0xFFF8); p.P2(12) }))
	ui := renderUIOf(s)
	if ui.ScrollPos[6969] != 240 {
		t.Fatalf("scroll = %d, want 240", ui.ScrollPos[6969])
	}
	if got := ui.Positions[70]; got.X != -8 || got.Y != 12 {
		t.Fatalf("position = %+v, want {-8 12}", got)
	}
}

// A session that never received any of these packets must add nothing to the snapshot: the
// maps are omitempty so an existing viewer and the fleet's own status path are unchanged.
func TestRenderUIOmitsUntouchedVisualState(t *testing.T) {
	s := New()
	ui := renderUIOf(s)
	if ui.Heads != nil || ui.Anims != nil || ui.Hidden != nil || ui.ScrollPos != nil || ui.Positions != nil {
		t.Fatalf("untouched visual state is present: %+v", ui)
	}
	if ui.Overlay != NoModal {
		t.Fatalf("overlay = %d, want %d", ui.Overlay, NoModal)
	}
}

// renderUIOf is renderUILocked with the lock taken: the tests above exercise interface
// decoding on a State that has no scene, which RenderSnapshot itself (rightly) refuses.
func renderUIOf(s *State) RenderUI {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renderUILocked()
}
