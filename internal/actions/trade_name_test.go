package actions

import (
	"bytes"
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// msbBits packs [width, value] fields most-significant bit first.
func msbBits(fields ...[2]uint32) []byte {
	var out []byte
	pos := 0
	for _, f := range fields {
		for i := int(f[0]) - 1; i >= 0; i-- {
			for len(out) <= pos/8 {
				out = append(out, 0)
			}
			if (f[1]>>uint(i))&1 == 1 {
				out[pos/8] |= 0x80 >> uint(pos%8)
			}
			pos++
		}
	}
	return out
}

// showPlayer sends a PLAYER_INFO adding player index with an appearance named rawName
// (lowercase, underscores for spaces).
func showPlayer(f *fakeSession, index int, rawName string) {
	var hash int64
	for i := 0; i < len(rawName); i++ {
		c := rawName[i]
		hash *= 37
		switch {
		case c >= 'a' && c <= 'z':
			hash += int64(c-'a') + 1
		case c >= '0' && c <= '9':
			hash += int64(c-'0') + 27
		}
	}
	blob := payload(func(p *packet.Packet) {
		for i := 0; i < 2+12+5; i++ {
			p.P1(0)
		}
		for i := 0; i < 7; i++ {
			p.P2(0)
		}
		p.P8(hash)
		p.P1(3)
		p.P2(0)
	})
	bits := msbBits(
		[2]uint32{1, 0}, // no local update
		[2]uint32{8, 0}, // no old players
		[2]uint32{11, uint32(index)}, [2]uint32{5, 1}, [2]uint32{5, 0}, [2]uint32{1, 0}, [2]uint32{1, 1},
		[2]uint32{11, 2047},
	)
	ext := payload(func(p *packet.Packet) {
		p.P1(0x1) // APPEARANCE
		p.P1(byte(len(blob)))
		p.PData(blob, 0, len(blob))
	})
	f.state.Handle(serverprot.PLAYER_INFO, append(bits, ext...))
}

func TestSameUsername(t *testing.T) {
	if !SameUsername("Bob Smith", "bob_smith") || !SameUsername(" MULECON ", "mulecon") {
		t.Fatal("equivalent names did not match")
	}
	if SameUsername("bob", "bobby") || SameUsername("", "") {
		t.Fatal("different or empty names matched")
	}
}

func TestTradeRequestPlayerByName(t *testing.T) {
	f := newFakeSession()
	tr := NewTrade(f)
	if err := tr.RequestPlayer("mulecon"); err != ErrPlayerNotVisible || f.sendCount() != 0 {
		t.Fatalf("no players: err = %v sends = %d", err, f.sendCount())
	}

	showPlayer(f, 42, "mule_con")
	if err := tr.RequestPlayer("Mule Con"); err != nil {
		t.Fatal(err)
	}
	want := payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.OPPLAYER4)
		p.P2(42)
	})
	if !bytes.Equal(f.lastSent(), want) {
		t.Fatalf("sent %x, want OPPLAYER4 42 (%x)", f.lastSent(), want)
	}
}
