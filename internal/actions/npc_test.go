package actions

import (
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
)

func TestOpNpcEncodesOptionAndIndex(t *testing.T) {
	cases := []struct {
		option int
		opcode byte
	}{
		{1, clientprot.OPNPC1},
		{2, clientprot.OPNPC2},
		{3, clientprot.OPNPC3},
		{4, clientprot.OPNPC4},
		{5, clientprot.OPNPC5},
	}
	for _, tc := range cases {
		s := newFakeSession()
		if err := OpNpc(s, tc.option, 4265); err != nil {
			t.Fatalf("option %d: OpNpc: %v", tc.option, err)
		}
		want := []byte{tc.opcode, 0x10, 0xA9} // 4265 = 0x10A9
		if got := s.lastSent(); string(got) != string(want) {
			t.Fatalf("option %d: OpNpc = % x, want % x", tc.option, got, want)
		}
	}
}

func TestOpNpcRejectsOutOfRangeOption(t *testing.T) {
	s := newFakeSession()
	for _, opt := range []int{0, 6, -3} {
		if err := OpNpc(s, opt, 1); err != errBadOption {
			t.Fatalf("option %d: err = %v, want errBadOption", opt, err)
		}
	}
	if s.sendCount() != 0 {
		t.Fatal("OpNpc with bad option should never call Send")
	}
}
