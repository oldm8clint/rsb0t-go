package actions

import (
	"bytes"
	"reflect"
	"testing"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/protocol/serverprot"
)

// makeMenu is a chat modal 305 with two products (bow string 1777, unstrung bow 48), each with
// Make 1/5/10/X buttons, and a prompt line.
func makeMenu() (*fakeSession, *ChatDialog) {
	f := newFakeSession()
	components := archive.Components{
		305: {ID: 305, Type: archive.TypeLayer, Children: []int{300, 306, 307, 308, 309, 310, 316, 317, 318, 319, 320}},
		300: {ID: 300, Type: archive.TypeText, Text: "What would you like to make?"},
		306: {ID: 306, Type: archive.TypeModel},
		307: {ID: 307, ButtonType: archive.ButtonOK, ButtonText: "Make 1"},
		308: {ID: 308, ButtonType: archive.ButtonOK, ButtonText: "Make 5"},
		309: {ID: 309, ButtonType: archive.ButtonOK, ButtonText: "Make 10"},
		310: {ID: 310, ButtonType: archive.ButtonOK, ButtonText: "Make X"},
		316: {ID: 316, Type: archive.TypeModel},
		317: {ID: 317, ButtonType: archive.ButtonOK, ButtonText: "Make 1"},
		318: {ID: 318, ButtonType: archive.ButtonOK, ButtonText: "Make 5"},
		319: {ID: 319, ButtonType: archive.ButtonOK, ButtonText: "Make 10"},
		320: {ID: 320, ButtonType: archive.ButtonOK, ButtonText: "Make X"},
	}
	openChat(f.state, 305)
	for com, obj := range map[int]int{306: 1777, 316: 48} {
		c, o := com, obj
		f.state.Handle(serverprot.IF_SETOBJECT, payload(func(p *packet.Packet) { p.P2(uint16(c)); p.P2(uint16(o)); p.P2(200) }))
	}
	return f, NewChatDialog(f, components)
}

var makeObjs = objTable{
	1777: {ID: 1777, Name: "Bow string"},
	48:   {ID: 48, Name: "Longbow (u)"},
}

func TestMakeProductsPairsPicturesWithButtons(t *testing.T) {
	_, d := makeMenu()
	want := []MakeProduct{
		{ObjID: 1777, Buttons: []MakeButton{{1, 307}, {5, 308}, {10, 309}, {-1, 310}}},
		{ObjID: 48, Buttons: []MakeButton{{1, 317}, {5, 318}, {10, 319}, {-1, 320}}},
	}
	if got := d.MakeProducts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("MakeProducts = %+v", got)
	}
	if !d.IsMakeMenu() {
		t.Fatal("IsMakeMenu false")
	}
	if p, ok := FindMakeProduct(d.MakeProducts(), makeObjs, "LONGBOW"); !ok || p.ObjID != 48 {
		t.Fatalf("FindMakeProduct = %+v, %v", p, ok)
	}
}

func TestMakeAndMakeOne(t *testing.T) {
	f, d := makeMenu()
	if err := d.Make(makeObjs, "longbow", nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), ifButtonBytes(319)) {
		t.Fatalf("Make sent %x, want IF_BUTTON 319 (Make 10)", f.lastSent())
	}
	if err := d.MakeOne(makeObjs, "", nil); err != nil || !bytes.Equal(f.lastSent(), ifButtonBytes(307)) {
		t.Fatalf("MakeOne err = %v sent %x, want IF_BUTTON 307", err, f.lastSent())
	}

	f.afterSend = func(f *fakeSession) { closeModal(f.state) }
	if err := d.Make(makeObjs, "bow string", &instantWaiter{}); err != nil {
		t.Fatalf("Make with a closing modal: %v", err)
	}
	openChat(f.state, 305)
	f.afterSend = nil
	if err := d.Make(makeObjs, "bow string", &instantWaiter{}); err != ErrNotConfirmed {
		t.Fatalf("Make with no response: err = %v", err)
	}
	if err := d.Make(makeObjs, "arrow shafts", nil); err != ErrNotOnScreen {
		t.Fatalf("unknown product: err = %v", err)
	}
}

func TestMakeXAnswersTheCountDialog(t *testing.T) {
	f, d := makeMenu()
	f.afterSend = func(f *fakeSession) {
		if bytes.Equal(f.lastSent(), ifButtonBytes(310)) {
			openCountDialog(f.state)
		}
	}
	if err := d.MakeX(makeObjs, "bow string", 27, &instantWaiter{}); err != nil {
		t.Fatal(err)
	}
	want := payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.RESUME_P_COUNTDIALOG)
		p.P4(27)
	})
	if f.sendCount() != 2 || !bytes.Equal(f.lastSent(), want) {
		t.Fatalf("sent %d packets, last %x, want the count answer %x", f.sendCount(), f.lastSent(), want)
	}

	f2, d2 := makeMenu()
	if err := d2.MakeX(makeObjs, "bow string", 27, &instantWaiter{}); err != errNoCountDialog || f2.sendCount() != 1 {
		t.Fatalf("no count dialog: err = %v sends = %d", err, f2.sendCount())
	}
}

func TestChatDialogTexts(t *testing.T) {
	f, d := makeMenu()
	if got := d.Texts(); len(got) != 1 || got[0] != "What would you like to make?" {
		t.Fatalf("Texts = %q", got)
	}
	setText(f.state, 300, "How many?")
	if got := d.Texts(); len(got) != 1 || got[0] != "How many?" {
		t.Fatalf("Texts after IF_SETTEXT = %q", got)
	}
	closeModal(f.state)
	if d.Texts() != nil {
		t.Fatal("Texts with no chat modal")
	}
	_ = cachecfg.Obj{}
}

func TestChatDialogTextsDropsLinesTheServerCleared(t *testing.T) {
	components := archive.Components{
		8000: {ID: 8000, Children: []int{8001, 8002}},
		8001: {ID: 8001, Type: archive.TypeText, Text: "archive line"},
		8002: {ID: 8002, Type: archive.TypeText, Text: "kept archive line"},
	}
	f := newFakeSession()
	d := NewChatDialog(f, components)
	setText(f.state, 8001, "")
	openChat(f.state, 8000)
	if got := d.Texts(); len(got) != 1 || got[0] != "kept archive line" {
		t.Fatalf("Texts() = %q, want only the never-set archive line", got)
	}
}
