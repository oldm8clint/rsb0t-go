package actions

import (
	"rsb0t-go/internal/protocol/clientprot"
	"testing"
)

func TestPlayerOptionsAndUseWireLayout(t *testing.T) {
	for i, opcode := range []byte{clientprot.OPPLAYER1, clientprot.OPPLAYER2, clientprot.OPPLAYER3, clientprot.OPPLAYER4, clientprot.OPPLAYER5} {
		s := newFakeSession()
		if err := OpPlayer(s, i+1, 10); err != nil {
			t.Fatal(err)
		}
		if got, want := s.lastSent(), []byte{opcode, 0, 10}; string(got) != string(want) {
			t.Fatalf("option %d: % x != % x", i+1, got, want)
		}
	}
	s := newFakeSession()
	if err := OpPlayerU(s, 42, 0x1234, 3, 3214); err != nil {
		t.Fatal(err)
	}
	if got, want := s.lastSent(), []byte{clientprot.OPPLAYERU, 0, 42, 0x12, 0x34, 0, 3, 0x0c, 0x8e}; string(got) != string(want) {
		t.Fatalf("use: % x != % x", got, want)
	}
	for _, option := range []int{0, 6} {
		if OpPlayer(s, option, 10) == nil {
			t.Fatal("invalid option accepted")
		}
	}
}

func TestPlayerOneSidePacketThresholdResetAndDisable(t *testing.T) {
	s := newFakeSession()
	for i := 0; i < 51; i++ {
		if err := OpPlayer(s, 1, 10); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.lastSent(); len(got) != 3 {
		t.Fatalf("early side packet: % x", got)
	}
	if err := OpPlayer(s, 1, 10); err != nil {
		t.Fatal(err)
	}
	if got, want := s.lastSent(), []byte{clientprot.ANTICHEAT_OPLOGIC4, 131, clientprot.OPPLAYER1, 0, 10}; string(got) != string(want) {
		t.Fatalf("threshold: % x", got)
	}
	s.Cheats().SetEnabled(false)
	_ = OpPlayer(s, 1, 10)
	if len(s.lastSent()) != 3 {
		t.Fatal("disabled side packet emitted")
	}
	s.Cheats().SetEnabled(true)
	s.Cheats().Reset()
	_ = OpPlayer(s, 1, 10)
	if len(s.lastSent()) != 3 {
		t.Fatal("login failed to reset counter")
	}
}
