package actions

import (
	"testing"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

func TestOpObjEncodesOptionTileAndType(t *testing.T) {
	cases := []struct {
		option int
		opcode byte
	}{
		{1, clientprot.OPOBJ1},
		{2, clientprot.OPOBJ2},
		{3, clientprot.OPOBJ3},
		{4, clientprot.OPOBJ4},
		{5, clientprot.OPOBJ5},
	}
	for _, tc := range cases {
		s := newFakeSession()
		if err := OpObj(s, tc.option, 2724, 3494, 995); err != nil {
			t.Fatalf("option %d: OpObj: %v", tc.option, err)
		}
		want := []byte{
			tc.opcode,
			0x0A, 0xA4, // 2724 = 0x0AA4
			0x0D, 0xA6, // 3494 = 0x0DA6
			0x03, 0xE3, // 995 = 0x03E3
		}
		if got := s.lastSent(); string(got) != string(want) {
			t.Fatalf("option %d: OpObj = % x, want % x", tc.option, got, want)
		}
	}
}

func TestOpObjRejectsOutOfRangeOption(t *testing.T) {
	s := newFakeSession()
	for _, opt := range []int{0, 6, 100} {
		if err := OpObj(s, opt, 1, 1, 1); err != errBadOption {
			t.Fatalf("option %d: err = %v, want errBadOption", opt, err)
		}
	}
	if s.sendCount() != 0 {
		t.Fatal("OpObj with bad option should never call Send")
	}
}

// TestOpObj5CarriesOpLogic3FromTheBuildBase: with a build base of z 3152, the first OPOBJ5
// already puts oplogic3 past 118, so it goes out with the side-packet; before any
// REBUILD_NORMAL nothing is counted or sent.
func TestOpObj5CarriesOpLogic3FromTheBuildBase(t *testing.T) {
	s := newFakeSession()
	if err := OpObj(s, 5, 3160, 3160, 995); err != nil {
		t.Fatal(err)
	}
	if got := s.lastSent(); got[0] != clientprot.OPOBJ5 {
		t.Fatalf("before a rebuild: sent % x, want plain OPOBJ5", got)
	}
	s.state.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) { p.P2(400); p.P2(400) }))
	if err := OpObj(s, 5, 3160, 3160, 995); err != nil {
		t.Fatal(err)
	}
	got := s.lastSent()
	if len(got) != 12 || got[0] != clientprot.ANTICHEAT_OPLOGIC3 || got[5] != clientprot.OPOBJ5 {
		t.Fatalf("after a rebuild: sent % x, want OPLOGIC3 + p4(0) then OPOBJ5", got)
	}
}

// TestOpLoc3CarriesOpLogic2AheadOfTheOpcode checks the wiring order Client.ts uses: the
// OPLOGIC2 side-packet first, then the OPLOC3 packet itself.
func TestOpLoc3CarriesOpLogic2AheadOfTheOpcode(t *testing.T) {
	s := newFakeSession()
	for i := 1; i < 124; i++ {
		if err := OpLoc(s, 3, 2724, 3494, 1276); err != nil {
			t.Fatal(err)
		}
		if got := s.lastSent(); got[0] != clientprot.OPLOC3 {
			t.Fatalf("call %d: side-packet sent early: % x", i, got)
		}
	}
	if err := OpLoc(s, 3, 2724, 3494, 1276); err != nil {
		t.Fatal(err)
	}
	got := s.lastSent()
	want := []byte{clientprot.ANTICHEAT_OPLOGIC2, 0x94, 0x42, clientprot.OPLOC3, 0x0A, 0xA4, 0x0D, 0xA6, 0x04, 0xFC}
	if string(got) != string(want) {
		t.Fatalf("call 124: % x, want % x", got, want)
	}
}

// TestOpObjOutsideTheBuildAreaMovesNoCounter: with the build base at 3152,3152 a tile at
// z 3100 is local z -52, which a real client cannot click; it must not lower oplogic8.
func TestOpObjOutsideTheBuildAreaMovesNoCounter(t *testing.T) {
	s := newFakeSession()
	s.state.Handle(serverprot.REBUILD_NORMAL, payload(func(p *packet.Packet) { p.P2(400); p.P2(400) }))
	for i := 0; i < 10; i++ {
		if err := OpObj(s, 4, 3160, 3100, 995); err != nil {
			t.Fatal(err)
		}
	}
	// Local z 74 inside the area, once: 74 < 75 unless the out-of-area calls moved the sum.
	if err := OpObj(s, 4, 3160, 3152+74, 995); err != nil {
		t.Fatal(err)
	}
	if got := s.lastSent(); got[0] != clientprot.OPOBJ4 {
		t.Fatalf("sent % x: an out-of-area click moved oplogic8", got)
	}
	if err := OpObj(s, 4, 3160, 3152+1, 995); err != nil {
		t.Fatal(err)
	}
	if got := s.lastSent(); got[0] != clientprot.ANTICHEAT_OPLOGIC8 {
		t.Fatalf("sent % x, want OPLOGIC8 at a sum of 75", got)
	}
}
