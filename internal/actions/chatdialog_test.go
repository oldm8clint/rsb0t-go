package actions

import (
	"testing"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/protocol/clientprot"
)

func TestChatDialogIsOpen(t *testing.T) {
	s := newFakeSession()
	d := NewChatDialog(s, nil)
	if d.IsOpen() {
		t.Fatal("dialog reported open with nothing open")
	}
	openChat(s.state, 12345)
	if !d.IsOpen() {
		t.Fatal("dialog should be open once a chat modal is set")
	}
}

func TestChatDialogContinuePressesTheContinueButton(t *testing.T) {
	components := archive.Components{
		1000: {ID: 1000, Children: []int{1001, 1002}},
		1001: {ID: 1001, ButtonType: archive.ButtonOK, ButtonText: "not it"},
		1002: {ID: 1002, ButtonType: archive.ButtonContinue},
	}
	s := newFakeSession()
	d := NewChatDialog(s, components)
	if d.CanContinue() {
		t.Fatal("CanContinue with no chat modal open should be false")
	}
	openChat(s.state, 1000)
	if !d.CanContinue() {
		t.Fatal("CanContinue should find the BUTTON_CONTINUE child")
	}
	if err := d.Continue(); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	want := []byte{clientprot.RESUME_PAUSEBUTTON, 0x03, 0xEA} // comID 1002 = 0x3EA
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("Continue() = % x, want % x", got, want)
	}
	// The pause-button latch (shared with Trade/Bank) means a second Continue is a
	// silent no-op and CanContinue flips false.
	if d.CanContinue() {
		t.Fatal("CanContinue should be false once the pause button has been resumed")
	}
	if err := d.Continue(); err != nil {
		t.Fatalf("second Continue: %v", err)
	}
	if s.sendCount() != 1 {
		t.Fatalf("second Continue sent another packet: %d sends", s.sendCount())
	}
}

func TestChatDialogContinueNotFoundReturnsErr(t *testing.T) {
	components := archive.Components{
		2000: {ID: 2000, Children: []int{2001}},
		2001: {ID: 2001, ButtonType: archive.ButtonOK},
	}
	s := newFakeSession()
	d := NewChatDialog(s, components)
	openChat(s.state, 2000)
	if err := d.Continue(); err != ErrNotOnScreen {
		t.Fatalf("Continue with no continue button: err = %v, want ErrNotOnScreen", err)
	}
}

func TestChatDialogOptionsRecursesTreeInOrder(t *testing.T) {
	// Options nest inside a layout child, unlike the continue button's direct-child-only
	// scan — this exercises that Options() recurses while continueComID() doesn't.
	components := archive.Components{
		3000: {ID: 3000, Children: []int{3010, 3020}},
		3010: {ID: 3010, Children: []int{3011, 3012}}, // a layout wrapper
		3011: {ID: 3011, ButtonType: archive.ButtonOK, ButtonText: "Yes."},
		3012: {ID: 3012, ButtonType: archive.ButtonOK, Text: "No way!"},
		3020: {ID: 3020, ButtonType: archive.ButtonContinue}, // not an option
	}
	s := newFakeSession()
	d := NewChatDialog(s, components)
	openChat(s.state, 3000)
	opts := d.Options()
	if len(opts) != 2 {
		t.Fatalf("Options() = %+v, want 2 entries", opts)
	}
	if opts[0].ComID != 3011 || opts[0].Text != "Yes." {
		t.Fatalf("Options()[0] = %+v", opts[0])
	}
	if opts[1].ComID != 3012 || opts[1].Text != "No way!" {
		t.Fatalf("Options()[1] = %+v (Text should be used when ButtonText is empty)", opts[1])
	}
}

func TestChatDialogChooseOptionMatchesCaseInsensitiveSubstring(t *testing.T) {
	components := archive.Components{
		4000: {ID: 4000, Children: []int{4001, 4002}},
		4001: {ID: 4001, ButtonType: archive.ButtonOK, ButtonText: "Yes, I accept."},
		4002: {ID: 4002, ButtonType: archive.ButtonOK, ButtonText: "No thanks."},
	}
	s := newFakeSession()
	d := NewChatDialog(s, components)
	openChat(s.state, 4000)

	if err := d.ChooseOption("NO THANKS"); err != nil {
		t.Fatalf("ChooseOption: %v", err)
	}
	want := []byte{clientprot.IF_BUTTON, 0x0F, 0xA2} // comID 4002 = 0xFA2
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("ChooseOption(NO THANKS) = % x, want % x", got, want)
	}
}

func TestChatDialogChooseOptionEmptyMatchPicksFirst(t *testing.T) {
	components := archive.Components{
		5000: {ID: 5000, Children: []int{5001, 5002}},
		5001: {ID: 5001, ButtonType: archive.ButtonOK, ButtonText: "First"},
		5002: {ID: 5002, ButtonType: archive.ButtonOK, ButtonText: "Second"},
	}
	s := newFakeSession()
	d := NewChatDialog(s, components)
	openChat(s.state, 5000)
	if err := d.ChooseOption(""); err != nil {
		t.Fatalf("ChooseOption(\"\"): %v", err)
	}
	want := []byte{clientprot.IF_BUTTON, 0x13, 0x89} // comID 5001 = 0x1389
	if got := s.lastSent(); string(got) != string(want) {
		t.Fatalf("ChooseOption(\"\") = % x, want % x", got, want)
	}
}

// The multi-choice chat interfaces ship "option1", "option2", ... as their archive text and
// the server sets the real labels with IF_SETTEXT, so Options and ChooseOption must read the
// live text (webwalk2 found the placeholders live on the Karamja and Brimhaven ships).
func TestChatDialogOptionsPreferLiveTextOverArchivePlaceholders(t *testing.T) {
	components := archive.Components{
		7000: {ID: 7000, Children: []int{7001, 7002, 7003, 7004}},
		7001: {ID: 7001, Type: archive.TypeText, ButtonType: archive.ButtonOK, Text: "option1"},
		7002: {ID: 7002, Type: archive.TypeText, ButtonType: archive.ButtonOK, Text: "option2"},
		7003: {ID: 7003, Type: archive.TypeText, ButtonType: archive.ButtonOK, Text: "option3"},
		7004: {ID: 7004, Type: archive.TypeText, ButtonType: archive.ButtonOK, Text: "option4"},
	}
	s := newFakeSession()
	d := NewChatDialog(s, components)
	setText(s.state, 7001, "Yes please.")
	setText(s.state, 7002, "No thank you.")
	setText(s.state, 7003, "") // cleared by the server: not an option
	// 7004 never set: the archive text is all there is.
	openChat(s.state, 7000)

	opts := d.Options()
	want := []Option{{7001, "Yes please."}, {7002, "No thank you."}, {7004, "option4"}}
	if len(opts) != len(want) {
		t.Fatalf("Options() = %+v, want %+v", opts, want)
	}
	for i := range want {
		if opts[i] != want[i] {
			t.Fatalf("Options()[%d] = %+v, want %+v", i, opts[i], want[i])
		}
	}

	if err := d.ChooseOption("no thank"); err != nil {
		t.Fatalf("ChooseOption(no thank): %v", err)
	}
	if got, wantB := s.lastSent(), []byte{clientprot.IF_BUTTON, 0x1B, 0x5A}; string(got) != string(wantB) { // 7002 = 0x1B5A
		t.Fatalf("ChooseOption(no thank) = % x, want % x", got, wantB)
	}
	if err := d.ChooseOption("option1"); err != ErrItemNotFound {
		t.Fatalf("ChooseOption(option1) matched a replaced placeholder: err = %v", err)
	}
	if err := d.ChooseOption(""); err != nil {
		t.Fatalf("ChooseOption(\"\"): %v", err)
	}
	if got, wantB := s.lastSent(), []byte{clientprot.IF_BUTTON, 0x1B, 0x59}; string(got) != string(wantB) { // 7001 = 0x1B59
		t.Fatalf("ChooseOption(\"\") = % x, want % x", got, wantB)
	}
}

func TestChatDialogChooseOptionNoMatchOrNoOptions(t *testing.T) {
	s := newFakeSession()
	d := NewChatDialog(s, nil)
	if err := d.ChooseOption("anything"); err != ErrNotOnScreen {
		t.Fatalf("ChooseOption with no chat modal open: err = %v, want ErrNotOnScreen", err)
	}

	components := archive.Components{
		6000: {ID: 6000, Children: []int{6001}},
		6001: {ID: 6001, ButtonType: archive.ButtonOK, ButtonText: "Only option"},
	}
	openChat(s.state, 6000)
	d2 := NewChatDialog(s, components)
	if err := d2.ChooseOption("does not exist"); err != ErrItemNotFound {
		t.Fatalf("ChooseOption(no match): err = %v, want ErrItemNotFound", err)
	}
}
