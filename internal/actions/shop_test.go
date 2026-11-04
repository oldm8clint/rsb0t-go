package actions

import (
	"testing"

	"rsb0t-go/internal/protocol/clientprot"
)

func TestShopIsOpenStockAndPack(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	if sh.IsOpen() {
		t.Fatal("shop reported open with nothing open")
	}
	openMainSide(s.state, ShopRoot, 0)
	if !sh.IsOpen() {
		t.Fatal("shop should be open once ShopRoot is the main modal")
	}
	setFullInventory(s.state, ShopStock, [2]int32{995, 1000})
	setFullInventory(s.state, ShopPack, [2]int32{1779, 10})
	if stock := sh.Stock(); len(stock) != 1 || stock[0].ID != 995 {
		t.Fatalf("Stock() = %+v", stock)
	}
	if pack := sh.Pack(); len(pack) != 1 || pack[0].ID != 1779 {
		t.Fatalf("Pack() = %+v", pack)
	}
}

func TestShopBuyRequiresOpenScreenAndTheItem(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	if err := sh.Buy(995, 1); err != ErrNotOnScreen {
		t.Fatalf("Buy off-screen: err = %v, want ErrNotOnScreen", err)
	}
	openMainSide(s.state, ShopRoot, 0)
	setFullInventory(s.state, ShopStock, [2]int32{995, 1000})
	if err := sh.Buy(1779, 1); err != ErrItemNotFound {
		t.Fatalf("Buy(missing item): err = %v, want ErrItemNotFound", err)
	}
}

// TestShopBuyBatchesIntoStepSizes is the pure decision logic COLLAB.md's task
// description calls out specifically: a quantity is split into Buy-10/Buy-5/Buy-1
// clicks (SHOP_STEPS), never a single big click, matching the engine's
// USER_OPS_PER_TICK limit. This drives the fake session's inventory to look unchanged
// between clicks (as a real shop would while the stock count itself is tracked
// separately), asserting only on which option each click used and in what order.
func TestShopBuyBatchesIntoStepSizes(t *testing.T) {
	cases := []struct {
		name        string
		n           int32
		wantOptions []byte
	}{
		{"exactly one ten", 10, []byte{clientprot.INV_BUTTON4}},                                                        // shopBuy10 option=4
		{"ten plus five", 15, []byte{clientprot.INV_BUTTON4, clientprot.INV_BUTTON3}},                                  // 10 then 5
		{"ten plus five plus one", 16, []byte{clientprot.INV_BUTTON4, clientprot.INV_BUTTON3, clientprot.INV_BUTTON2}}, // 10,5,1 (each step consumes exactly its own qty, not "up to remaining")
		{"three ones", 3, []byte{clientprot.INV_BUTTON2, clientprot.INV_BUTTON2, clientprot.INV_BUTTON2}},              // 1,1,1
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeSession()
			sh := NewShop(s)
			openMainSide(s.state, ShopRoot, 0)
			setFullInventory(s.state, ShopStock, [2]int32{995, 1_000_000})
			if err := sh.Buy(995, tc.n); err != nil {
				t.Fatalf("Buy(%d): %v", tc.n, err)
			}
			if len(s.sent) != len(tc.wantOptions) {
				t.Fatalf("Buy(%d) sent %d packets, want %d: %v", tc.n, len(s.sent), len(tc.wantOptions), s.sent)
			}
			for i, want := range tc.wantOptions {
				if s.sent[i][0] != want {
					t.Errorf("Buy(%d) click %d opcode = %x, want %x", tc.n, i, s.sent[i][0], want)
				}
			}
		})
	}
}

func TestShopSellUsesSellSteps(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	openMainSide(s.state, ShopRoot, 0)
	setFullInventory(s.state, ShopPack, [2]int32{1779, 1_000_000})
	if err := sh.Sell(1779, 6); err != nil {
		t.Fatalf("Sell: %v", err)
	}
	// 6 = 5 + 1: shopSell5(option3) then shopSell1(option2).
	want := []byte{clientprot.INV_BUTTON3, clientprot.INV_BUTTON2}
	if len(s.sent) != len(want) {
		t.Fatalf("Sell(6) sent %d packets, want %d: %v", len(s.sent), len(want), s.sent)
	}
	for i, w := range want {
		if s.sent[i][0] != w {
			t.Errorf("Sell(6) click %d opcode = %x, want %x", i, s.sent[i][0], w)
		}
	}
}

func TestShopBuyStopsEarlyWhenStockRunsOut(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	openMainSide(s.state, ShopRoot, 0)
	setFullInventory(s.state, ShopStock, [2]int32{995, 1_000_000})
	// Simulate the stock actually running out after the first click.
	s.afterSend = func(f *fakeSession) {
		setFullInventory(f.state, ShopStock, [2]int32{0, 0})
	}
	if err := sh.Buy(995, 50); err != ErrItemNotFound {
		t.Fatalf("Buy after stock runs out: err = %v, want ErrItemNotFound", err)
	}
	if len(s.sent) != 1 {
		t.Fatalf("Buy sent %d packets before stopping, want 1", len(s.sent))
	}
}

func TestShopClose(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	if err := sh.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := s.lastSent(); len(got) != 1 || got[0] != clientprot.CLOSE_MODAL {
		t.Fatalf("Close() = % x, want CLOSE_MODAL", got)
	}
}
