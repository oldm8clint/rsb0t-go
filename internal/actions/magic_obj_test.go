package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

// OPOBJT is OpObjTDecoder's g2 x, g2 z, g2 obj, g2 spellCom, with Telekinetic Grab's component.
func TestCastOnObjSendsOpObjT(t *testing.T) {
	for _, name := range []string{"Telekinetic Grab", "telekinetic_grab"} {
		f := newFakeSession()
		if err := CastOnObj(f, name, 2554, 3401, 532); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		want := payload(func(p *packet.Packet) {
			p.P1Enc(clientprot.OPOBJT)
			p.P2(2554)
			p.P2(3401)
			p.P2(532)
			p.P2(1168)
		})
		if !bytes.Equal(f.lastSent(), want) {
			t.Errorf("%q: sent %x, want %x", name, f.lastSent(), want)
		}
	}
	f := newFakeSession()
	if err := CastOnObj(f, "Wind Strike", 1, 1, 1); err != ErrUnknownSpell || f.sendCount() != 0 {
		t.Fatalf("a combat spell on an obj: err = %v, sends = %d", err, f.sendCount())
	}
}
