package actions

import (
	"testing"

	"rsb0t-go/internal/acquiretask"
)

func TestAcquireTaskEnsure_AlreadyHeldDoesNothing(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	openMainSide(s.state, ShopRoot, 0)
	setFullInventory(s.state, ShopStock, [2]int32{995, 1000})
	setFullInventory(s.state, ShopPack, [2]int32{995, 10})

	task := NewAcquireTask(nil, sh)
	plan, err := task.Ensure(995, acquiretask.Need{Item: "Pot", Count: 5})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if plan != nil {
		t.Fatalf("plan = %v, want nil (already held enough)", plan)
	}
	if len(s.sent) != 0 {
		t.Fatalf("sent %d packets, want 0 — nothing should be bought", len(s.sent))
	}
}

func TestAcquireTaskEnsure_BuysShortfallFromShop(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	openMainSide(s.state, ShopRoot, 0)
	setFullInventory(s.state, ShopStock, [2]int32{995, 1000})
	setFullInventory(s.state, ShopPack, [2]int32{995, 2})

	task := NewAcquireTask(nil, sh)
	plan, err := task.Ensure(995, acquiretask.Need{Item: "Pot", Count: 5})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := []acquiretask.Step{{Source: acquiretask.SourceShop, Count: 3}}
	if len(plan) != 1 || plan[0] != want[0] {
		t.Fatalf("plan = %v, want %v", plan, want)
	}
	if len(s.sent) == 0 {
		t.Fatal("Ensure bought nothing, expected a shop buy click")
	}
}

func TestAcquireTaskEnsure_ShopShortStockTrailsGather(t *testing.T) {
	s := newFakeSession()
	sh := NewShop(s)
	openMainSide(s.state, ShopRoot, 0)
	// Shop only has 2 of the item in stock, but 5 are needed and nothing is held.
	setFullInventory(s.state, ShopStock, [2]int32{995, 2})
	setFullInventory(s.state, ShopPack, [2]int32{0, 0})

	task := NewAcquireTask(nil, sh)
	plan, err := task.Ensure(995, acquiretask.Need{Item: "Pot", Count: 5})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := []acquiretask.Step{
		{Source: acquiretask.SourceShop, Count: 2},
		{Source: acquiretask.SourceGather, Count: 3},
	}
	if len(plan) != len(want) || plan[0] != want[0] || plan[1] != want[1] {
		t.Fatalf("plan = %v, want %v", plan, want)
	}
}

func TestAcquireTaskEnsure_NoBankNoShopIsAllGather(t *testing.T) {
	task := NewAcquireTask(nil, nil)
	plan, err := task.Ensure(995, acquiretask.Need{Item: "Pot", Count: 4})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := []acquiretask.Step{{Source: acquiretask.SourceGather, Count: 4}}
	if len(plan) != 1 || plan[0] != want[0] {
		t.Fatalf("plan = %v, want %v", plan, want)
	}
}

func TestAcquireTaskEnsure_PrefersBankOverShop(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	sh := NewShop(s)
	openMainSide(s.state, BankMainModal, 5063)
	setFullInventory(s.state, BankWithdraw, [2]int32{995, 1000})

	task := NewAcquireTask(b, sh)
	plan, err := task.Ensure(995, acquiretask.Need{Item: "Pot", Count: 3})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := []acquiretask.Step{{Source: acquiretask.SourceBank, Count: 3}}
	if len(plan) != 1 || plan[0] != want[0] {
		t.Fatalf("plan = %v, want %v", plan, want)
	}
	// Bank isn't open for withdrawal purposes unless BankMainModal is the main modal —
	// it is here, so this should have sent real withdraw clicks (Withdraw-1 x3, since
	// bank.go has no arbitrary-amount option yet).
	if len(s.sent) != 3 {
		t.Fatalf("sent %d packets, want 3 withdraw-one clicks", len(s.sent))
	}
}

func TestAcquireTaskEnsure_WithdrawExactBatchesLikeShop(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	openMainSide(s.state, BankMainModal, 5063)
	setFullInventory(s.state, BankWithdraw, [2]int32{995, 1_000_000})

	task := NewAcquireTask(b, nil)
	plan, err := task.Ensure(995, acquiretask.Need{Item: "Coins", Count: 16})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	want := []acquiretask.Step{{Source: acquiretask.SourceBank, Count: 16}}
	if len(plan) != 1 || plan[0] != want[0] {
		t.Fatalf("plan = %v, want %v", plan, want)
	}
	if len(s.sent) != 3 { // 10 + 5 + 1
		t.Fatalf("sent %d packets, want 3 (10,5,1 batched clicks)", len(s.sent))
	}
}

func TestAcquireTaskEnsure_BankErrorPropagates(t *testing.T) {
	s := newFakeSession()
	b := NewBank(s, nil)
	openMainSide(s.state, BankMainModal, 5063)
	setFullInventory(s.state, BankWithdraw, [2]int32{995, 100})
	s.sendErr = errBoom

	task := NewAcquireTask(b, nil)
	_, err := task.Ensure(995, acquiretask.Need{Item: "Pot", Count: 1})
	if err == nil {
		t.Fatal("Ensure with a failing Send = nil error, want the wrapped send failure")
	}
}
