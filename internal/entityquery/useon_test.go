package entityquery

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

func TestUseItemOnDispatchesByKind(t *testing.T) {
	st := gamestate.New()
	s := &recSession{st: st}
	handle(st, serverprot.IF_SETICON, func(p *packet.Packet) { p.P2(3213); p.P1(3) })
	handle(st, serverprot.UPDATE_INV_FULL, func(p *packet.Packet) {
		p.P2(3214)
		p.P2(2)
		p.P2(0)
		p.P1(0)
		p.P2(1926) // bucket 1925 in slot 1
		p.P1(1)
	})
	eq := actions.NewEquipment(s, archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
	})

	build := func(opcode int, fields ...int) []byte {
		w := packet.NewWriter(16)
		w.P1Enc(byte(opcode))
		for _, f := range fields {
			w.P2(uint16(f))
		}
		return w.Data[:w.Pos]
	}
	cases := []struct {
		target Entity
		want   []byte
	}{
		{Entity{Kind: KindNpc, Index: 77}, build(clientprot.OPNPCU, 77, 1925, 1, 3214)},
		{Entity{Kind: KindLoc, ID: 2644, X: 3210, Z: 3214}, build(clientprot.OPLOCU, 3210, 3214, 2644, 1925, 1, 3214)},
		{Entity{Kind: KindGroundItem, ID: 1511, X: 3209, Z: 3211}, build(clientprot.OPOBJU, 3209, 3211, 1511, 1925, 1, 3214)},
	}
	for _, c := range cases {
		if err := UseItemOn(eq, 1925, c.target); err != nil {
			t.Fatal(err)
		}
		if got := s.sent[len(s.sent)-1]; !bytes.Equal(got, c.want) {
			t.Fatalf("kind %v: sent %x, want %x", c.target.Kind, got, c.want)
		}
	}
	if err := UseItemOn(eq, 1925, Entity{Kind: KindPlayer}); err != ErrNoActions {
		t.Fatalf("player target: err = %v", err)
	}
}
