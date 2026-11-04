package actions

import (
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
)

func TestOpLocEncodesOptionTileAndType(t *testing.T) {
	cases := []struct {
		option int
		opcode byte
	}{
		{1, clientprot.OPLOC1},
		{2, clientprot.OPLOC2},
		{3, clientprot.OPLOC3},
		{4, clientprot.OPLOC4},
		{5, clientprot.OPLOC5},
	}
	for _, tc := range cases {
		s := newFakeSession()
		if err := OpLoc(s, tc.option, 2724, 3494, 2213); err != nil {
			t.Fatalf("option %d: OpLoc: %v", tc.option, err)
		}
		want := []byte{
			tc.opcode,
			0x0A, 0xA4, // 2724 = 0x0AA4
			0x0D, 0xA6, // 3494 = 0x0DA6
			0x08, 0xA5, // 2213 = 0x08A5
		}
		if got := s.lastSent(); string(got) != string(want) {
			t.Fatalf("option %d: OpLoc = % x, want % x", tc.option, got, want)
		}
	}
}

func TestOpLocRejectsOutOfRangeOption(t *testing.T) {
	s := newFakeSession()
	for _, opt := range []int{0, 6, 100} {
		if err := OpLoc(s, opt, 1, 1, 1); err != errBadOption {
			t.Fatalf("option %d: err = %v, want errBadOption", opt, err)
		}
	}
	if s.sendCount() != 0 {
		t.Fatal("OpLoc with bad option should never call Send")
	}
}
