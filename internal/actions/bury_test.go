package actions

import (
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
)

// TestBurySendsOpheld1WithObjSlotCom mirrors actions_test.go's
// TestInvButtonEncodesOptionObjSlotCom: Bury is a thin OpHeld(1, ...) wrapper, so the
// only pure logic worth pinning down is that it emits OPHELD1 (not InvButton, not a
// different OPHELD option) with the same p2(objId) p2(slot) p2(comId) payload shape
// equipment.go's Equip already proved live for OPHELD2.
func TestBurySendsOpheld1WithObjSlotCom(t *testing.T) {
	s := newFakeSession()
	// Big bones (obj id 532), slot 3, backpack component 3214 (see COLLAB.md's
	// confirmed-live PlayerPack constant).
	if err := Bury(s, 532, 3, 3214); err != nil {
		t.Fatalf("Bury: %v", err)
	}
	want := []byte{clientprot.OPHELD1, 0x02, 0x14 /*532*/, 0, 3, 0x0C, 0x8E /*3214*/}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Bury(532, 3, 3214) = % x, want % x", got, want)
	}
}

func TestBuryPropagatesSendError(t *testing.T) {
	s := newFakeSession()
	s.sendErr = errBoom
	if err := Bury(s, 532, 0, 3214); err != errBoom {
		t.Fatalf("Bury error = %v, want %v", err, errBoom)
	}
}

// TestOpenSendsOpheld1WithObjSlotCom mirrors TestBurySendsOpheld1WithObjSlotCom: Open is the
// identical OPHELD1 wrapper, just a casket's own op-menu entry rather than a bone's.
func TestOpenSendsOpheld1WithObjSlotCom(t *testing.T) {
	s := newFakeSession()
	if err := Open(s, 405, 5, 3214); err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := []byte{clientprot.OPHELD1, 0x01, 0x95 /*405*/, 0, 5, 0x0C, 0x8E /*3214*/}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Open(405, 5, 3214) = % x, want % x", got, want)
	}
}

func TestOpenPropagatesSendError(t *testing.T) {
	s := newFakeSession()
	s.sendErr = errBoom
	if err := Open(s, 405, 0, 3214); err != errBoom {
		t.Fatalf("Open error = %v, want %v", err, errBoom)
	}
}
