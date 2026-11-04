package shopplan

import (
	"testing"
	"time"
)

func TestSupplyShopsAreTheContentRows(t *testing.T) {
	cases := []struct {
		name           string
		keeper         string
		baseline, rate int32
		sell, delta    int32
	}{
		{"Hickton", "Hickton", 200, 10, 1000, 10},
		{"lowe", "Lowe", 1500, 10, 1000, 10},
		{"Lathas", "Shop keeper", 200, 10, 1500, 10},
	}
	for _, c := range cases {
		s, ok := SupplyShopByName(c.name)
		if !ok {
			t.Fatalf("%s: not found", c.name)
		}
		it, ok := s.ItemByID(877)
		if !ok || it.Name != "Bolts" || it.Baseline != c.baseline || it.RestockTicks != c.rate || it.Cost != 3 {
			t.Errorf("%s: bolt row %+v", c.name, it)
		}
		if s.Keeper != c.keeper || s.Sell != c.sell || s.Delta != c.delta {
			t.Errorf("%s: keeper %q sell %d delta %d", c.name, s.Keeper, s.Sell, s.Delta)
		}
	}
	if _, ok := SupplyShopByName("Gulluck"); ok {
		t.Error("Gulluck (Grand Tree level 2, no webwalk route) is listed")
	}
}

func TestUnitsUnderCapWalksThePriceUp(t *testing.T) {
	s, _ := SupplyShopByName("Lowe")
	it, _ := s.ItemByID(877)
	// A full shelf: 3 gp at baseline, 1% of the base dearer a unit below it, never above 18.
	if p := UnitPrice(it, s, 1500); p != 3 {
		t.Fatalf("baseline price %d, want 3", p)
	}
	if p := UnitPrice(it, s, 1); p != 18 {
		t.Fatalf("last unit price %d, want the clamp's 18", p)
	}
	all, cost := UnitsUnderCap(it, s, 1500, 18, -1)
	if all != 1500 || cost != BuyoutCost(it, s, 1500, 1500) {
		t.Errorf("cap 18: %d units for %d, want the whole shelf", all, cost)
	}
	under, _ := UnitsUnderCap(it, s, 1500, 5, -1)
	if under <= 0 || under >= 1500 || UnitPrice(it, s, 1500-under) <= 5 || UnitPrice(it, s, 1500-under+1) > 5 {
		t.Errorf("cap 5 stopped at %d units", under)
	}
	if n, c := UnitsUnderCap(it, s, 1500, 18, 10); n != 3 || c != 9 {
		t.Errorf("10 coins: %d units for %d, want 3 for 9", n, c)
	}
}

func TestEstimateShelfRestocksOneUnitEveryRateTicks(t *testing.T) {
	it := Item{Baseline: 200, RestockTicks: 10}
	if got := EstimateShelf(it, 0, 0, false); got != 200 {
		t.Errorf("never seen: %d, want full", got)
	}
	if got := EstimateShelf(it, 0, 10*time.Minute, true); got != 100 {
		t.Errorf("bought out 10 minutes ago: %d, want 100 (one a 6 s)", got)
	}
	if got := EstimateShelf(it, 150, time.Hour, true); got != 200 {
		t.Errorf("an hour on: %d, want the baseline", got)
	}
}
