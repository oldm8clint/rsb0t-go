package actions

import (
	"bytes"
	"testing"
	"time"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
)

type objTable map[int]cachecfg.Obj

func (t objTable) Obj(id int) (cachecfg.Obj, bool) {
	o, ok := t[id]
	return o, ok
}

var testObjs = objTable{
	526:  {ID: 526, Name: "Bones", IOps: [5]string{"Bury"}},
	1205: {ID: 1205, Name: "Bronze dagger", IOps: [5]string{"", "Wield"}},
	1129: {ID: 1129, Name: "Leather body", IOps: [5]string{"", "Wear"}},
	590:  {ID: 590, Name: "Tinderbox", IOps: [5]string{"Use"}},
	995:  {ID: 995, Name: "Coins"},
	996:  {ID: 996, Name: "coins"}, // a noted stack shares the name
	11:   {ID: 11, Name: "Odd", IOps: [5]string{"", "", "", "Rub"}},
	12:   {ID: 12, Name: "Staff", Ops: [5]string{"", "", "Pick-up"}},
}

func TestGroundAndHeldOpDefaults(t *testing.T) {
	if got := GroundOps([5]string{}); got[2] != "Take" {
		t.Fatalf("GroundOps default = %v", got)
	}
	if got := GroundOps([5]string{"", "", "Pick-up"}); got[2] != "Pick-up" {
		t.Fatalf("GroundOps kept %v", got)
	}
	if got := HeldOps([5]string{}); got[4] != "Drop" {
		t.Fatalf("HeldOps default = %v", got)
	}
	if OptionIndex([5]string{"", "Wield"}, "WIELD") != 2 || OptionIndex([5]string{}, "") != 0 {
		t.Fatal("OptionIndex mismatch")
	}
	if EquipOption(testObjs[1205].IOps) != 2 || EquipOption(testObjs[1129].IOps) != 2 || EquipOption(testObjs[590].IOps) != 0 {
		t.Fatal("EquipOption mismatch")
	}
	if EquipOption([5]string{"", "", "Equip"}) != 3 {
		t.Fatal("EquipOption must find Equip in any slot")
	}
}

func TestTakeGroundItemSendsOpObj3(t *testing.T) {
	f := newFakeSession()
	item := gamestate.GroundItem{ID: 526, Count: 1, X: 3210, Z: 3212}
	if err := TakeGroundItem(f, testObjs, item); err != nil {
		t.Fatal(err)
	}
	want := payload(func(p *packet.Packet) {
		p.P1Enc(clientprot.OPOBJ3)
		p.P2(3210)
		p.P2(3212)
		p.P2(526)
	})
	if !bytes.Equal(f.lastSent(), want) {
		t.Fatalf("sent %x, want %x", f.lastSent(), want)
	}
	if err := TakeGroundItem(newFakeSession(), nil, item); err != nil {
		t.Fatalf("Take without types: %v", err)
	}
	if err := InteractGroundItem(newFakeSession(), testObjs, gamestate.GroundItem{ID: 12}, "Take"); err != ErrNoSuchAction {
		t.Fatalf("a type with its own op3 has no Take: err = %v", err)
	}
}

// equipmentSession has a backpack (tab 3 -> 3213 -> 3214) and a worn panel (tab 4 -> 1644 -> 1688).
func equipmentSession() (*fakeSession, *Equipment) {
	f := newFakeSession()
	components := archive.Components{
		3213: {ID: 3213, Type: archive.TypeLayer, Children: []int{3214}},
		3214: {ID: 3214, Type: archive.TypeInv, ObjOps: true},
		1644: {ID: 1644, Type: archive.TypeLayer, Children: []int{1688}},
		1688: {ID: 1688, Type: archive.TypeInv},
	}
	setIcon(f, 3, 3213)
	setIcon(f, 4, 1644)
	setFullInventory(f.state, 3214, [2]int32{590, 1}, [2]int32{1205, 1}, [2]int32{11, 1}, [2]int32{995, 100}, [2]int32{996, 50})
	setFullInventory(f.state, 1688, [2]int32{0, 0}, [2]int32{0, 0}, [2]int32{0, 0}, [2]int32{1129, 1})
	return f, NewEquipment(f, components)
}

func opHeldBytes(option int, obj int32, slot, com int) []byte {
	return payload(func(p *packet.Packet) {
		p.P1Enc(byte(opHeldOpcodes[option-1]))
		p.P2(uint16(obj))
		p.P2(uint16(slot))
		p.P2(uint16(com))
	})
}

func TestEquipItemPicksTheVerbFromConfig(t *testing.T) {
	f, e := equipmentSession()
	if err := e.EquipItem(testObjs, 1205); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), opHeldBytes(2, 1205, 1, 3214)) {
		t.Fatalf("sent %x, want OPHELD2 dagger slot 1", f.lastSent())
	}
	if err := e.EquipItem(testObjs, 590); err != ErrNoSuchAction {
		t.Fatalf("equipping a tinderbox: err = %v", err)
	}
	if err := e.EquipItem(nil, 590); err != nil || !bytes.Equal(f.lastSent(), opHeldBytes(2, 590, 0, 3214)) {
		t.Fatalf("nil types must fall back to option 2: err = %v sent %x", err, f.lastSent())
	}
	if err := e.EquipItem(testObjs, 4151); err != ErrItemNotFound {
		t.Fatalf("an item not held: err = %v", err)
	}
	if !e.ContainsWorn(testObjs, "leather BODY") || e.ContainsWorn(testObjs, "Bronze dagger") {
		t.Fatal("ContainsWorn mismatch")
	}
}

func TestInteractHeld(t *testing.T) {
	f, e := equipmentSession()
	if err := e.InteractHeld(testObjs, 590, "use"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.lastSent(), opHeldBytes(1, 590, 0, 3214)) {
		t.Fatalf("sent %x, want OPHELD1", f.lastSent())
	}
	if err := e.InteractHeld(testObjs, 590, "Drop"); err != nil || !bytes.Equal(f.lastSent(), opHeldBytes(5, 590, 0, 3214)) {
		t.Fatalf("default Drop: err = %v sent %x", err, f.lastSent())
	}
	if err := e.InteractHeld(testObjs, 11, "Rub"); err != nil || !bytes.Equal(f.lastSent(), opHeldBytes(4, 11, 2, 3214)) {
		t.Fatalf("Rub (option 4): err = %v sent %x", err, f.lastSent())
	}
	if err := e.InteractHeld(nil, 590, "Use"); err != ErrNoSuchAction {
		t.Fatalf("nil types: err = %v", err)
	}
}

func TestInventoryHelpers(t *testing.T) {
	_, e := equipmentSession()
	pack := e.Inventory()
	if CountByID(pack, 995) != 100 || CountByName(pack, testObjs, "COINS") != 150 {
		t.Fatalf("counts: id %d name %d", CountByID(pack, 995), CountByName(pack, testObjs, "COINS"))
	}
	if it, ok := FindByName(pack, testObjs, "bronze dagger"); !ok || it.Slot != 1 {
		t.Fatalf("FindByName = %+v, %v", it, ok)
	}
	if _, ok := FindByName(pack, nil, "Coins"); ok {
		t.Fatal("FindByName without types")
	}
	if FreeSlots(pack) != 23 || IsFull(pack) {
		t.Fatalf("FreeSlots = %d", FreeSlots(pack))
	}
	full := make([]gamestate.SlotItem, 28)
	if !IsFull(full) || FreeSlots(full) != 0 {
		t.Fatal("a 28-slot pack must be full")
	}
}

// instantWaiter confirms immediately, since fakeSession applies server state inside Send.
type instantWaiter struct{ ticks int }

func (w *instantWaiter) Delay(time.Duration) bool { return true }
func (w *instantWaiter) DelayUntil(cond func() bool, _ time.Duration) bool {
	return cond()
}
func (w *instantWaiter) DelayTicks(n int) bool { w.ticks += n; return true }

func TestBatchOptions(t *testing.T) {
	cases := []struct {
		remaining int32
		want      []int
	}{
		{1, []int{shopBuy1}},
		{16, []int{shopBuy10, shopBuy5, shopBuy1}},
		{25, []int{shopBuy10, shopBuy10, shopBuy5}},
		{57, []int{shopBuy10, shopBuy10, shopBuy10, shopBuy10, shopBuy10}}, // five per tick
		{0, nil},
	}
	for _, c := range cases {
		got := batchOptions(shopSteps, c.remaining)
		if len(got) != len(c.want) {
			t.Errorf("batch(%d) = %v, want %v", c.remaining, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("batch(%d) = %v, want %v", c.remaining, got, c.want)
				break
			}
		}
	}
}

func TestBuyCountReportsWhatLanded(t *testing.T) {
	f := newFakeSession()
	openMain(f.state, ShopRoot)
	setFullInventory(f.state, ShopStock, [2]int32{1931, 7})
	setFullInventory(f.state, ShopPack, [2]int32{995, 1000})
	held := int32(0)
	f.afterSend = func(f *fakeSession) {
		// The shop only ever has 7 pots: each click lands until they run out.
		if held < 7 {
			held++
			if held > 7 {
				held = 7
			}
		}
		setFullInventory(f.state, ShopPack, [2]int32{995, 1000}, [2]int32{1931, held})
		setFullInventory(f.state, ShopStock, [2]int32{1931, 7 - held})
	}
	w := &instantWaiter{}
	got, err := NewShop(f).BuyCount(1931, 25, w)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Fatalf("BuyCount = %d, want 7 (stock ran out)", got)
	}
	if w.ticks == 0 {
		t.Fatal("BuyCount never settled a tick")
	}

	if _, err := NewShop(newFakeSession()).BuyCount(1931, 1, w); err != ErrNotOnScreen {
		t.Fatalf("closed shop: err = %v", err)
	}
	if _, err := NewShop(f).BuyCount(4151, 1, w); err != ErrItemNotFound {
		t.Fatalf("unstocked item: err = %v", err)
	}
}

func TestSellCountReportsWhatLeft(t *testing.T) {
	f := newFakeSession()
	openMain(f.state, ShopRoot)
	setFullInventory(f.state, ShopStock)
	setFullInventory(f.state, ShopPack, [2]int32{1779, 12})
	held := int32(12)
	f.afterSend = func(f *fakeSession) {
		// Sell 10 and Sell 5 take what the stack holds.
		held -= 10
		if held < 0 {
			held = 0
		}
		if held == 0 {
			setFullInventory(f.state, ShopPack)
		} else {
			setFullInventory(f.state, ShopPack, [2]int32{1779, held})
		}
	}
	got, err := NewShop(f).SellCount(1779, 12, &instantWaiter{})
	if err != nil || got != 12 {
		t.Fatalf("SellCount = %d, %v; want 12", got, err)
	}
}

// TestOpHeld4CarriesOpLogic9 checks the side-packet reaches the wire ahead of OPHELD4 once due,
// and that no other held option advances or sends it.
func TestOpHeld4CarriesOpLogic9(t *testing.T) {
	f, _ := equipmentSession()
	for i := 0; i < 200; i++ {
		if err := OpHeld(f, 1, 590, 0, 3214); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(f.lastSent(), opHeldBytes(1, 590, 0, 3214)) {
		t.Fatalf("OPHELD1 grew a side-packet: %x", f.lastSent())
	}
	for i := 1; i < 116; i++ {
		if err := OpHeld(f, 4, 11, 2, 3214); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(f.lastSent(), opHeldBytes(4, 11, 2, 3214)) {
			t.Fatalf("call %d: side-packet sent early: %x", i, f.lastSent())
		}
	}
	if err := OpHeld(f, 4, 11, 2, 3214); err != nil {
		t.Fatal(err)
	}
	want := append([]byte{clientprot.ANTICHEAT_OPLOGIC9, 0xC6, 0xA4, 0x39}, opHeldBytes(4, 11, 2, 3214)...)
	if !bytes.Equal(f.lastSent(), want) {
		t.Fatalf("call 116: sent %x, want %x", f.lastSent(), want)
	}
}
