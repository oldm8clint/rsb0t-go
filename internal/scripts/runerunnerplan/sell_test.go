package runerunnerplan

import (
	"strings"
	"testing"
)

// The costs in the cache the owner's items carry (scripts/skill_crafting cut_gems.obj, skill_smithing
// smelting.obj): Dragonstone 10,000, Runite bar 5,000, Diamond 2,000.
const (
	costDragonstone = 10000
	costRuniteBar   = 5000
	costDiamond     = 2000
)

func outlet(t *testing.T, key string) SellOutlet {
	t.Helper()
	o, ok := SellOutletFor(key)
	if !ok {
		t.Fatalf("no outlet %s", key)
	}
	return o
}

// THE KARAMJA QUESTION. A general store pays 40% of cost for the first unit -- 66.7% of High Level Alchemy --
// and 3 points of cost (delta 30) less for each unit it already holds, to a floor of a tenth of the cost. The
// Karamja General Store is priced exactly like Varrock's and Arhein's, so travelling there buys nothing.
func TestGeneralStoreLadderForADragonstone(t *testing.T) {
	var ladder []int
	for i := 0; i < 12; i++ {
		ladder = append(ladder, SalePrice(costDragonstone, 400, 30, -1, i))
	}
	want := []int{4000, 3700, 3400, 3100, 2800, 2500, 2200, 1900, 1600, 1300, 1000, 1000}
	for i := range want {
		if ladder[i] != want[i] {
			t.Fatalf("ladder %v, want %v", ladder, want)
		}
	}
	// 66.7% of alch for the first, 46.7% by the fifth, 16.7% at the floor.
	if AlchValue(costDragonstone) != 6000 || AlchValue(costRuniteBar) != 3000 || AlchValue(costDiamond) != 1200 || AlchValue(1) != 1 {
		t.Errorf("alch values %d %d %d %d", AlchValue(costDragonstone), AlchValue(costRuniteBar), AlchValue(costDiamond), AlchValue(1))
	}
	for _, key := range []string{"VarrockGeneral", "Arhein", "KaramjaGeneral"} {
		o := outlet(t, key)
		if o.BuyMult != 400 || o.Delta != 30 || !o.AllStock {
			t.Errorf("%s pays %d/%d", key, o.BuyMult, o.Delta)
		}
		if n, total := PlanSale(costDragonstone, o.BuyMult, o.Delta, -1, 0, 40, 0); n != 6 || total != 19500 {
			t.Errorf("%s: %d Dragonstones for %d, want 6 for 19500", key, n, total)
		}
	}
	// A shelf that already holds some pays less: the stock decays one unit a minute for an unlisted item.
	if p := SalePrice(costDragonstone, 400, 30, -1, 6); p != 2200 {
		t.Errorf("the seventh Dragonstone pays %d", p)
	}
	// Selling pays 3,250 a Dragonstone on average at best, against the 5,980 a Nature rune cast on one returns.
	if avg := 19500 / 6; avg != 3250 || avg >= AlchValue(costDragonstone)-20 {
		t.Errorf("the sale average %d is not 3,250 under the alch return", avg)
	}
}

// The design's figures for each outlet at a stop of 40% of the alchemy value, checked against the rule: the deeper
// the shelf the more units, but never the first unit's price.
func TestSellOutletsAtAFortyPercentStop(t *testing.T) {
	for _, c := range []struct {
		key          string
		item         string
		cost         int
		n, total     int
		firstPercent int
	}{
		{"VarrockGeneral", "Dragonstone", costDragonstone, 6, 19500, 66},
		{"VarrockGeneral", "Runite bar", costRuniteBar, 6, 9750, 66},
		{"VarrockGeneral", "Diamond", costDiamond, 6, 3900, 66},
		{"Aemad", "Dragonstone", costDragonstone, 9, 28800, 66},
		{"Aemad", "Runite bar", costRuniteBar, 9, 14400, 66},
		{"Aemad", "Diamond", costDiamond, 9, 5760, 66},
		{"Khazard", "Dragonstone", costDragonstone, 17, 54400, 66},
		{"Khazard", "Runite bar", costRuniteBar, 17, 27200, 66},
		{"Khazard", "Diamond", costDiamond, 17, 10880, 66},
		{"Jiminua", "Dragonstone", costDragonstone, 14, 51800, 83},
		{"Jiminua", "Runite bar", costRuniteBar, 14, 25900, 83},
		{"Jiminua", "Diamond", costDiamond, 14, 10360, 83},
	} {
		o := outlet(t, c.key)
		n, total := PlanSale(c.cost, o.BuyMult, o.Delta, -1, 0, 40, 0)
		if n != c.n || total != c.total {
			t.Errorf("%s %s: %d for %d, want %d for %d", c.key, c.item, n, total, c.n, c.total)
		}
		if pct := SalePrice(c.cost, o.BuyMult, o.Delta, -1, 0) * 100 / AlchValue(c.cost); pct != c.firstPercent {
			t.Errorf("%s %s: the first unit pays %d%% of alch, want %d%%", c.key, c.item, pct, c.firstPercent)
		}
	}
	// A bound on the units wins over the stop; a stop of 100% sells nothing at a general store (66.7% is the best).
	if n, total := PlanSale(costDragonstone, 400, 20, -1, 0, 40, 5); n != 5 || total != 4000+3800+3600+3400+3200 {
		t.Errorf("maxUnits 5: %d for %d", n, total)
	}
	if n, total := PlanSale(costDragonstone, 400, 30, -1, 0, 100, 0); n != 0 || total != 0 {
		t.Errorf("a stop at the full alch value: %d for %d", n, total)
	}
	// The floor of a tenth of the cost (1,000) is 16.7% of alch: a 10% stop never trips, so the bound is the
	// caller's (the units in the pack), and with none given the plan's own.
	if n, _ := PlanSale(costDragonstone, 400, 30, -1, 0, 10, 25); n != 25 {
		t.Errorf("a 10%% stop with 25 in the pack sells %d", n)
	}
	if n, _ := PlanSale(costDragonstone, 400, 30, -1, 0, 10, 0); n != PlanSaleMaxUnits {
		t.Errorf("an unbounded 10%% stop sells %d", n)
	}
}

// A shelf BELOW its stock base pays more: the Ardougne gem stall lists Diamond at base 0 (buy 800, delta 30), so the
// first Diamond pays 1,600 -- 133% of alch -- and ten pay 13,300. A listed line recovers, so each world's stall is
// a one-off pool.
func TestGemStallPaysMoreThanAlchForTheFirstUnits(t *testing.T) {
	o := outlet(t, "GemStall")
	if o.AllStock {
		t.Error("the gem stall takes only the gems it lists")
	}
	for i, want := range []int{1600, 1540, 1480} {
		if got := SalePrice(costDiamond, o.BuyMult, o.Delta, 0, i); got != want {
			t.Errorf("Diamond %d pays %d, want %d", i+1, got, want)
		}
	}
	for n, want := range map[int]int{5: 7400, 10: 13300, 20: 20600} {
		if got := SaleTotal(costDiamond, o.BuyMult, o.Delta, 0, 0, n); got != want {
			t.Errorf("%d Diamonds pay %d, want %d", n, got, want)
		}
	}
	if first := SalePrice(costDiamond, o.BuyMult, o.Delta, 0, 0) * 100 / AlchValue(costDiamond); first != 133 {
		t.Errorf("the first Diamond pays %d%% of alch", first)
	}
	// Above its base the same line pays less than a general store.
	if SalePrice(costDiamond, o.BuyMult, o.Delta, 0, 40) >= SalePrice(costDiamond, 400, 30, -1, 0) {
		t.Error("a stocked gem line should pay less than a general store's first unit")
	}
}

func TestSaleLineAndRegistry(t *testing.T) {
	line := outlet(t, "Aemad").SaleLine("Dragonstone", costDragonstone, -1, 0, 40)
	if line != "Aemad Dragonstone: 9 for 28800 (first 4000 of alch 6000)" {
		t.Errorf("line %q", line)
	}
	if _, ok := SellOutletFor("Fionella"); ok {
		t.Error("an unknown outlet was found")
	}
	seen := map[string]bool{}
	for _, o := range SellOutlets {
		if seen[o.Key] || o.Key == "" || o.BuyMult <= 0 || o.Delta <= 0 || strings.TrimSpace(o.Where) == "" {
			t.Errorf("outlet %+v", o)
		}
		seen[o.Key] = true
	}
	// The price never falls under a tenth of the cost, whatever the stock.
	if p := SalePrice(costDragonstone, 400, 30, -1, 100000); p != 1000 {
		t.Errorf("deep shelf pays %d", p)
	}
	if p := SalePrice(1, 400, 30, -1, 0); p != 0 {
		t.Errorf("a one-coin item pays %d (truncated)", p)
	}
}
