package fisherplan

import "testing"

// The live 2026-09-20 loss: a Big net account (Fishing 16+) whose net a whirlpool took.
func lostNet(coins, bankCoins, caskets, fish int, bankTools []string) FishSnapshot {
	here := Catherby().Bank
	bc, fb := bankCoins, fish
	return FishSnapshot{
		Tile: &here, Level: 30, Target: DefaultTarget,
		Coins: coins, BankCoins: &bc, BankTools: bankTools, FishBanked: &fb,
		BankCaskets: caskets,
	}
}

func TestLostNetRecoveryFollowsTheOwnersOrder(t *testing.T) {
	// (a) coins already in the bank: withdraw them and buy.
	if got := DecideFishPhase(lostNet(0, 40, 3, 100, []string{})); got != PhaseBank {
		t.Errorf("banked coins cover the net = %s, want bank (withdraw them)", got)
	}
	// (b) short of the price with caskets banked: open them before anything else.
	s := lostNet(3, 4, 2, 100, []string{})
	s.MuleCoinsReady = true
	if got := DecideFishPhase(s); got != PhaseOpen {
		t.Errorf("short with caskets banked = %s, want open", got)
	}
	// (c) no caskets: ask the mule before selling the catch.
	s.BankCaskets = 0
	if got := DecideFishPhase(s); got != PhaseAskCoins {
		t.Errorf("short, no caskets, mule ready = %s, want askcoins", got)
	}
	// The mule already asked (cooling down): selling the catch is the fallback.
	s.MuleCoinsReady = false
	if got := DecideFishPhase(s); got != PhaseSell {
		t.Errorf("short, no caskets, mule cooling down = %s, want sell", got)
	}
}

func TestHarryOutOfStockFishesWithWhatIsOwned(t *testing.T) {
	// Coins in hand, Harry out of stock, a Small fishing net in the bank: go and fish with it
	// (the bank phase withdraws it) rather than standing at the shop.
	s := lostNet(80, 0, 0, 2157, []string{"Small fishing net"})
	s.ShopOut = true
	if got := DecideFishPhase(s); got != PhaseBank {
		t.Errorf("out of stock with a small net banked = %s, want bank (withdraw the small net)", got)
	}
	s.HeldTools = []string{"Small fishing net"}
	s.BankTools = []string{}
	s.Coins = 0
	bc := 80
	s.BankCoins = &bc
	if got := DecideFishPhase(s); got != PhaseFish {
		t.Errorf("out of stock holding a small net = %s, want fish", got)
	}
	// Nothing owned at all: wait away from the shop instead of camping it.
	s = lostNet(80, 0, 0, 2157, []string{})
	s.ShopOut = true
	if got := DecideFishPhase(s); got != PhaseRestock {
		t.Errorf("out of stock owning nothing = %s, want restock", got)
	}
	// The retry timer ran out: buy again.
	s.ShopOut = false
	if got := DecideFishPhase(s); got != PhaseBuy {
		t.Errorf("retry due = %s, want buy", got)
	}
}
