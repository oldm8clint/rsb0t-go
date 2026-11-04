package actions

import (
	"testing"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// objBox is content's level-up unlock box, the exact interface that made a live bot spin for
// four minutes. `[proc,objbox_page]` in chat.rs2 opens objbox1 as a CHAT modal carrying one
// if_setobject product picture, one line of text and a continue button — and no Make buttons at
// all, because it is a message, not a menu.
func objBox(obj int) (*fakeSession, *ChatDialog) {
	f := newFakeSession()
	components := archive.Components{
		306: {ID: 306, Type: archive.TypeLayer, Children: []int{307, 308, 309}},
		307: {ID: 307, Type: archive.TypeModel},
		308: {ID: 308, Type: archive.TypeText, Text: "You can now make Shortbows."},
		309: {ID: 309, ButtonType: archive.ButtonContinue, ButtonText: "Click here to continue"},
	}
	openChat(f.state, 306)
	f.state.Handle(serverprot.IF_SETOBJECT, payload(func(p *packet.Packet) { p.P2(307); p.P2(uint16(obj)); p.P2(250) }))
	return f, NewChatDialog(f, components)
}

var objBoxObjs = objTable{
	841:  {ID: 841, Name: "Shortbow"},
	52:   {ID: 52, Name: "Arrow shaft"},
	1777: {ID: 1777, Name: "Bow string"},
}

// A level-up unlock box is not a make menu. Before the button requirement in MakeProducts it
// read as one: the picture became a product with an empty button list, IsMakeMenu went true,
// and every caller that gates on "a make menu is open" refused to press continue past it. The
// dialogue then never closed, so the run deadlocked against a box one click would have cleared.
func TestLevelUpObjBoxIsNotAMakeMenu(t *testing.T) {
	_, d := objBox(841)
	if got := d.MakeProducts(); len(got) != 0 {
		t.Fatalf("MakeProducts = %+v, want none: an object picture with no Make button is not a product", got)
	}
	if d.IsMakeMenu() {
		t.Fatal("IsMakeMenu true for a level-up unlock box")
	}
	if d.Offers(objBoxObjs, "shortbow") {
		t.Fatal("Offers true for a box that offers no way to make anything")
	}
	// The whole point: the caller must still see this as a dialogue it should click through.
	if !d.CanContinue() {
		t.Fatal("CanContinue false: the bot could never clear the box")
	}
}

// A real make menu is unaffected by the button requirement.
func TestOffersNamesTheProductNotJustAnyMenu(t *testing.T) {
	_, d := makeMenu()
	if !d.IsMakeMenu() {
		t.Fatal("IsMakeMenu false for a real make menu")
	}
	if !d.Offers(makeObjs, "bow string") {
		t.Fatal("Offers false for a product the menu really lists")
	}
	// The bug in one line: this menu is open, so IsMakeMenu is true, but it cannot make the
	// thing the caller came for. A caller that waits on IsMakeMenu proceeds anyway.
	if d.Offers(makeObjs, "arrow shaft") {
		t.Fatal("Offers true for a product this menu does not list")
	}
	if got := d.DescribeProducts(makeObjs); got != "[Bow string Longbow (u)]" {
		t.Fatalf("DescribeProducts = %q", got)
	}
}

// A menu whose pictures outnumber its buttons keeps only the products that can be clicked,
// rather than inventing button-less ones the caller would then fail to drive.
func TestMakeProductsDropsPicturesWithNoButtons(t *testing.T) {
	f := newFakeSession()
	components := archive.Components{
		400: {ID: 400, Type: archive.TypeLayer, Children: []int{401, 402, 403, 404, 405, 406}},
		401: {ID: 401, Type: archive.TypeModel},
		402: {ID: 402, ButtonType: archive.ButtonOK, ButtonText: "Make 1"},
		403: {ID: 403, ButtonType: archive.ButtonOK, ButtonText: "Make 5"},
		404: {ID: 404, ButtonType: archive.ButtonOK, ButtonText: "Make 10"},
		405: {ID: 405, ButtonType: archive.ButtonOK, ButtonText: "Make X"},
		406: {ID: 406, Type: archive.TypeModel},
	}
	openChat(f.state, 400)
	for com, obj := range map[int]int{401: 52, 406: 1777} {
		c, o := com, obj
		f.state.Handle(serverprot.IF_SETOBJECT, payload(func(p *packet.Packet) { p.P2(uint16(c)); p.P2(uint16(o)); p.P2(200) }))
	}
	d := NewChatDialog(f, components)
	got := d.MakeProducts()
	if len(got) != 1 || got[0].ObjID != 52 || len(got[0].Buttons) != 4 {
		t.Fatalf("MakeProducts = %+v, want only the shaft product with its four buttons", got)
	}
	if d.Offers(objBoxObjs, "bow string") {
		t.Fatal("Offers true for the picture with no buttons of its own")
	}
}
