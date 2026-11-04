package actions

import (
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
)

func TestOpPlayer4SendsRawOpcodeBelowAnticheatThreshold(t *testing.T) {
	s := newFakeSession()
	if err := OpPlayer4(s, 10); err != nil {
		t.Fatalf("OpPlayer4: %v", err)
	}
	want := []byte{clientprot.OPPLAYER4, 0, 10}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("OpPlayer4(10) = % x, want % x", got, want)
	}
}

func TestOpPlayer4PrependsAnticheatOnceThresholdCrossed(t *testing.T) {
	s := newFakeSession()
	// A single call with a=70 crosses the >=66 oplogic5 threshold (see
	// internal/anticheat's own tests for the counter itself); this checks the extra
	// packet is prepended in front of OPPLAYER4, in the wire order Client.ts uses.
	if err := OpPlayer4(s, 70); err != nil {
		t.Fatalf("OpPlayer4: %v", err)
	}
	want := []byte{clientprot.ANTICHEAT_OPLOGIC5, 154, clientprot.OPPLAYER4, 0, 70}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("OpPlayer4(70) = % x, want % x", got, want)
	}
}

func TestOpPlayer4PropagatesSendError(t *testing.T) {
	s := newFakeSession()
	s.sendErr = errBoom
	if err := OpPlayer4(s, 1); err != errBoom {
		t.Fatalf("OpPlayer4 error = %v, want %v", err, errBoom)
	}
}

func TestInvButtonEncodesOptionObjSlotCom(t *testing.T) {
	cases := []struct {
		option int
		opcode byte
	}{
		{1, clientprot.INV_BUTTON1},
		{2, clientprot.INV_BUTTON2},
		{3, clientprot.INV_BUTTON3},
		{4, clientprot.INV_BUTTON4},
		{5, clientprot.INV_BUTTON5},
	}
	for _, tc := range cases {
		s := newFakeSession()
		if err := InvButton(s, tc.option, 995, 3, 5382); err != nil {
			t.Fatalf("option %d: InvButton: %v", tc.option, err)
		}
		want := []byte{tc.opcode, 0x03, 0xE3 /*995*/, 0, 3, 0x15, 0x06 /*5382*/}
		if got := s.lastSent(); string(got) != string(want) {
			t.Fatalf("option %d: InvButton = % x, want % x", tc.option, got, want)
		}
	}
}

func TestInvButtonRejectsOutOfRangeOption(t *testing.T) {
	s := newFakeSession()
	for _, opt := range []int{0, 6, -1} {
		if err := InvButton(s, opt, 1, 0, 1); err != errBadOption {
			t.Fatalf("option %d: err = %v, want errBadOption", opt, err)
		}
	}
	if s.sendCount() != 0 {
		t.Fatalf("InvButton with bad option should never call Send, got %d sends", s.sendCount())
	}
}

func TestInvButtonOption1CountsTowardOplogic6(t *testing.T) {
	s := newFakeSession()
	// a&3==0 increments oplogic6; below its threshold (133) no anticheat packet rides
	// along, so the payload is just the plain INV_BUTTON1 bytes.
	if err := InvButton(s, 1, 4, 0, 1); err != nil {
		t.Fatalf("InvButton: %v", err)
	}
	want := []byte{clientprot.INV_BUTTON1, 0, 4, 0, 0, 0, 1}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("InvButton(option1) = % x, want % x", got, want)
	}
}

func TestInvButtonOption2Through5NeverCountAnticheat(t *testing.T) {
	// Only option 1 (INV_BUTTON1) is wired to BeforeInvButton1; the others must never
	// gain an anti-cheat prefix even after many calls, since oplogic6 only increments
	// for option 1.
	s := newFakeSession()
	for i := 0; i < 200; i++ {
		if err := InvButton(s, 2, 4, 0, 1); err != nil {
			t.Fatalf("call %d: InvButton: %v", i, err)
		}
	}
	want := []byte{clientprot.INV_BUTTON2, 0, 4, 0, 0, 0, 1}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("InvButton(option2) after 200 calls = % x, want % x (no anticheat prefix)", got, want)
	}
}

func TestIfButtonEncodesComponent(t *testing.T) {
	s := newFakeSession()
	if err := IfButton(s, 3420); err != nil {
		t.Fatalf("IfButton: %v", err)
	}
	want := []byte{clientprot.IF_BUTTON, 0x0D, 0x5C} // 3420 = 0x0D5C
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("IfButton(3420) = % x, want % x", got, want)
	}
}

func TestCloseModalSendsOpcodeAndClearsModalsLocally(t *testing.T) {
	s := newFakeSession()
	openMainSide(s.state, 5292, 5382)
	if err := CloseModal(s); err != nil {
		t.Fatalf("CloseModal: %v", err)
	}
	if got := s.lastSent(); len(got) != 1 || got[0] != clientprot.CLOSE_MODAL {
		t.Fatalf("CloseModal wire bytes = % x, want [% x]", got, clientprot.CLOSE_MODAL)
	}
	main, side, _ := s.state.Modals()
	if main != -1 || side != -1 {
		t.Fatalf("CloseModal left main=%d side=%d, want both cleared", main, side)
	}
}

func TestCloseModalLeavesStateAloneOnSendError(t *testing.T) {
	s := newFakeSession()
	openMainSide(s.state, 5292, 5382)
	s.sendErr = errBoom
	if err := CloseModal(s); err != errBoom {
		t.Fatalf("CloseModal error = %v, want %v", err, errBoom)
	}
	main, _, _ := s.state.Modals()
	if main != 5292 {
		t.Fatalf("CloseModal cleared local state despite a Send error: main=%d", main)
	}
}

func TestResumePauseButtonFiresOnceThenLatches(t *testing.T) {
	s := newFakeSession()
	if err := ResumePauseButton(s, 3420); err != nil {
		t.Fatalf("first ResumePauseButton: %v", err)
	}
	want := []byte{clientprot.RESUME_PAUSEBUTTON, 0x0D, 0x5C}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("first ResumePauseButton = % x, want % x", got, want)
	}
	if err := ResumePauseButton(s, 3420); err != nil {
		t.Fatalf("second ResumePauseButton: %v", err)
	}
	if s.sendCount() != 1 {
		t.Fatalf("ResumePauseButton sent again after the latch was set: %d sends", s.sendCount())
	}
}

func TestAnswerCountDialogRequiresOpenDialog(t *testing.T) {
	s := newFakeSession()
	if err := AnswerCountDialog(s, 5); err != errNoCountDialog {
		t.Fatalf("AnswerCountDialog with no dialog open: err = %v, want errNoCountDialog", err)
	}
	if s.sendCount() != 0 {
		t.Fatal("AnswerCountDialog sent a packet despite no dialog being open")
	}
}

func TestAnswerCountDialogClampsNegativeToZeroAndClearsInput(t *testing.T) {
	s := newFakeSession()
	openCountDialog(s.state)
	if err := AnswerCountDialog(s, -5); err != nil {
		t.Fatalf("AnswerCountDialog: %v", err)
	}
	want := []byte{clientprot.RESUME_P_COUNTDIALOG, 0, 0, 0, 0}
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("AnswerCountDialog(-5) = % x, want % x (clamped to 0)", got, want)
	}
	if s.state.CountDialogOpen() {
		t.Fatal("AnswerCountDialog left the count dialog marked open")
	}
}

func TestAnswerCountDialogPositiveValue(t *testing.T) {
	s := newFakeSession()
	openCountDialog(s.state)
	if err := AnswerCountDialog(s, 1000); err != nil {
		t.Fatalf("AnswerCountDialog: %v", err)
	}
	want := []byte{clientprot.RESUME_P_COUNTDIALOG, 0, 0, 0x03, 0xE8} // 1000 = 0x3E8
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("AnswerCountDialog(1000) = % x, want % x", got, want)
	}
}

func TestCheatEncodesLengthAndTerminatedCommand(t *testing.T) {
	// CheatEnabled defaults false (see its own doc comment — production safety), so tests
	// exercising Cheat's actual packet encoding must opt in explicitly, restoring the prior
	// value after so this doesn't leak into other tests' default-off expectations.
	prev := CheatEnabled
	CheatEnabled = true
	defer func() { CheatEnabled = prev }()

	s := newFakeSession()
	if err := Cheat(s, "tele 3200 3200"); err != nil {
		t.Fatalf("Cheat: %v", err)
	}
	got := s.lastSent()
	if len(got) < 2 || got[0] != clientprot.CLIENT_CHEAT {
		t.Fatalf("Cheat opcode byte = % x, want %x first", got, clientprot.CLIENT_CHEAT)
	}
	cmd := "tele 3200 3200"
	wantLen := byte(len(cmd) + 1) // +1 for the newline PJStr appends
	if got[1] != wantLen {
		t.Fatalf("Cheat length byte = %d, want %d", got[1], wantLen)
	}
	body := got[2:]
	if string(body[:len(cmd)]) != cmd {
		t.Fatalf("Cheat command body = %q, want %q", body[:len(cmd)], cmd)
	}
	if body[len(cmd)] != 10 {
		t.Fatalf("Cheat command not newline-terminated: last byte = %d", body[len(cmd)])
	}
}
