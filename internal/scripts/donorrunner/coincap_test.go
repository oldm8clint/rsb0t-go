package donorrunner

import (
	"testing"

	"rsb0t-go/internal/scripts/donorplan"
)

// At the cap a hand-off leaves its coins out and keeps the rest; under it, or with the mule's bank unknown, coins cross.
func TestTheMulesCoinCapKeepsCoinsWithTheDonor(t *testing.T) {
	defer muleStock.Store(nil)
	load := func() []donorplan.LoadGroup {
		return []donorplan.LoadGroup{{Name: "Coins", ID: 995, Slots: 1}, {Name: "Raw shark", ID: 383, Slots: 20}}
	}
	o := Options{Mule: "mule", CoinsName: "Coins", Load: load}
	held, known := 0, true
	SetMuleStock(func(mule, item string) (int, bool) {
		if mule != "mule" || item != "Coins" {
			return 0, false
		}
		return held, known
	})
	for _, c := range []struct {
		held  int
		known bool
		coins bool
	}{
		{1_310_000_000, true, false},
		{MuleCoinCap, true, false},
		{MuleCoinCap - 1, true, true},
		{1_310_000_000, false, true},
	} {
		held, known = c.held, c.known
		got := effectiveLoad(o)
		hasCoins := false
		for _, g := range got {
			if g.Name == "Coins" {
				hasCoins = true
			}
		}
		if hasCoins != c.coins || len(got) < 1 || got[len(got)-1].Name != "Raw shark" {
			t.Errorf("mule %d (known %v): load %+v, want coins %v", c.held, c.known, got, c.coins)
		}
	}
	// Nothing but coins at the cap: the plan is empty, so the donor is not sent to the mule for it.
	held, known = 1_310_000_000, true
	o.Load = func() []donorplan.LoadGroup { return []donorplan.LoadGroup{{Name: "Coins", ID: 995, Slots: 1}} }
	if got := effectiveLoad(o); len(got) != 0 {
		t.Errorf("coins only at the cap: %+v", got)
	}
}
