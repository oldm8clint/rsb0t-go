package gamestate

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

func TestIfSetObjectAndSetModel(t *testing.T) {
	s := New()
	if _, ok := s.InterfaceObject(306); ok {
		t.Fatal("object known before IF_SETOBJECT")
	}
	s.Handle(serverprot.IF_SETOBJECT, payload(func(p *packet.Packet) { p.P2(306); p.P2(1777); p.P2(200) }))
	if obj, ok := s.InterfaceObject(306); !ok || obj != 1777 {
		t.Fatalf("InterfaceObject = %d,%v, want 1777", obj, ok)
	}
	s.Handle(serverprot.IF_SETMODEL, payload(func(p *packet.Packet) { p.P2(306); p.P2(55) }))
	if _, ok := s.InterfaceObject(306); ok {
		t.Fatal("IF_SETMODEL must clear the object model")
	}
}
