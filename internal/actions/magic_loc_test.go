package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestCastOnLocSendsOpLocT(t *testing.T) {
	f := newFakeSession()
	if err := CastOnLoc(f, "Charge Water Orb", 2844, 3422, 2151); err != nil {
		t.Fatal(err)
	}
	want := payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.OPLOCT)
		p.P2(2844)
		p.P2(3422)
		p.P2(2151)
		p.P2(1179)
	})
	if !bytes.Equal(f.lastSent(), want) {
		t.Fatalf("sent %x, want %x", f.lastSent(), want)
	}
	if id, ok := LocSpellComponent("air_orb"); !ok || id != 1186 {
		t.Fatalf("LocSpellComponent(air_orb) = %d,%v", id, ok)
	}
	if err := CastOnLoc(f, "Wind Strike", 1, 1, 1); err != ErrUnknownSpell {
		t.Fatalf("a combat spell on a loc: err = %v", err)
	}
}
