package gamestate

import (
	"fmt"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"testing"
)

func captureReadyState() *State {
	s := New()
	s.haveTile = true
	s.scene.tick = 7
	s.scene.state = SceneReady
	return s
}

func TestRenderUICopiesServerObservedInterfacesAndSlots(t *testing.T) {
	s := captureReadyState()
	s.Handle(serverprot.IF_OPENMAIN, payload(func(p *packet.Packet) { p.P2(5292) }))
	s.Handle(serverprot.UPDATE_INV_FULL, payload(func(p *packet.Packet) { p.P2(5382); p.P2(1); p.P2(996); p.P1(100) }))
	s.Handle(serverprot.IF_SETTEXT, payload(func(p *packet.Packet) { p.P2(5383); p.PJStr("Bank of RuneScape") }))
	s.Handle(serverprot.IF_SETICON, payload(func(p *packet.Packet) { p.P2(3213); p.P1(3) }))
	s.Handle(serverprot.VARP_SMALL, payload(func(p *packet.Packet) { p.P2(173); p.P1(1) }))
	s.Handle(serverprot.UPDATE_STAT, payload(func(p *packet.Packet) { p.P1(10); p.P4(605032); p.P1(68) }))
	s.Handle(serverprot.UPDATE_RUNENERGY, []byte{83})
	s.Handle(serverprot.MESSAGE_GAME, payload(func(p *packet.Packet) { p.PJStr("You catch a lobster.") }))
	view, ok := s.RenderSnapshot()
	if !ok || view.UI.Version != 1 || view.UI.Tick != 7 || view.UI.Main != 5292 || view.UI.ChatModal != NoModal {
		t.Fatalf("UI=%+v ready%v", view.UI, ok)
	}
	if inv := view.UI.Inventories[5382]; len(inv.Slots) != 1 || inv.Slots[0].ID != 995 || inv.Slots[0].Count != 100 || inv.FullGeneration == 0 {
		t.Fatalf("bank=%+v", inv)
	}
	if view.UI.SideIcons[3] != 3213 || view.UI.Varps[173] != 1 || view.UI.Skills[10].Level != 68 || *view.UI.RunEnergy != 83 || len(view.UI.Chat) != 1 {
		t.Fatalf("UI=%+v", view.UI)
	}
	view.UI.Inventories[5382].Slots[0].Count = 999
	view.UI.Texts[5383] = "mutated"
	view.UI.SideIcons[3] = 7
	view.UI.Varps[173] = 0
	view.UI.Chat[0].Text = "mutated"
	*view.UI.RunEnergy = 0
	next, _ := s.RenderSnapshot()
	if next.UI.Inventories[5382].Slots[0].Count != 100 || next.UI.Texts[5383] != "Bank of RuneScape" || next.UI.SideIcons[3] != 3213 || next.UI.Varps[173] != 1 || next.UI.Chat[0].Text != "You catch a lobster." || *next.UI.RunEnergy != 83 {
		t.Fatal("UI capture aliases live state")
	}
}

func TestRenderChatIsBoundedAndDoesNotChangeEventDelivery(t *testing.T) {
	s := captureReadyState()
	seen := 0
	unsub := s.Subscribe(func(e Event) {
		if e.Kind == EventChatMessage {
			seen++
		}
	})
	defer unsub()
	for i := 0; i < 150; i++ {
		s.Handle(serverprot.MESSAGE_GAME, payload(func(p *packet.Packet) { p.PJStr(fmt.Sprintf("line%d", i)) }))
	}
	view, _ := s.RenderSnapshot()
	if seen != 150 || len(view.UI.Chat) != 100 || view.UI.Chat[0].Text != "line50" || view.UI.Chat[99].Text != "line149" {
		t.Fatalf("seen%d history%+v", seen, view.UI.Chat)
	}
	if view.UI.RunEnergy != nil || view.UI.Weight != nil {
		t.Fatal("unknown energy/weight must not become invented zeros")
	}
}
