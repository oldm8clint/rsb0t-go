package combatplan

import (
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

var (
	reachArdougne = tile.Tile{X: 2655, Z: 3283}
	reachDraynor  = tile.Tile{X: 3092, Z: 3245}
)

// Live 7 Oct 03:06: acct89 at Ardougne wanted Cassie's shield and Peksa's helm and made three failed trips to
// each. With a few coins nothing across the water is planned, and each shop left out is reported once.
func TestPlanReachableLeavesOutShopsAcrossTheWaterWhenThePurseCannotPayTheShips(t *testing.T) {
	l := Levels{Attack: 49, Strength: 44, Defence: 43}
	full := BuyPlanExcept(l, nil, nil, nil, nil)
	if len(full) == 0 {
		t.Fatal("test premise: a melee 49/44/43 account wants gear")
	}
	plan, skipped := PlanReachable(l, nil, nil, nil, nil, &reachArdougne, 20)
	if len(plan) != 0 {
		t.Errorf("planned %d purchases from shops across the water with 20 coins: %+v", len(plan), plan)
	}
	if len(skipped) == 0 {
		t.Fatal("nothing reported skipped")
	}
	seen := map[string]bool{}
	for _, s := range skipped {
		if seen[s.Shop] {
			t.Errorf("%s reported twice", s.Shop)
		}
		seen[s.Shop] = true
		if s.Fares != 120 || s.Coins != 20 {
			t.Errorf("%+v: want 120 fares, 20 coins", s)
		}
	}
}

// With the round trip's fares the same account shops as before; so does an account already on the shops' side.
func TestPlanReachableKeepsEverythingForAnAccountThatCanPayOrIsThere(t *testing.T) {
	l := Levels{Attack: 49, Strength: 44, Defence: 43}
	full := BuyPlanExcept(l, nil, nil, nil, nil)
	rich, skipped := PlanReachable(l, nil, nil, nil, nil, &reachArdougne, 120)
	if len(skipped) != 0 || len(rich) != len(full) {
		t.Errorf("120 coins: %d purchases (want %d), skipped %+v", len(rich), len(full), skipped)
	}
	there, skipped := PlanReachable(l, nil, nil, nil, nil, &reachDraynor, 0)
	if len(skipped) != 0 || len(there) != len(full) {
		t.Errorf("on the shops' side with no coins: %d purchases (want %d), skipped %+v", len(there), len(full), skipped)
	}
	unknown, skipped := PlanReachable(l, nil, nil, nil, nil, nil, 0)
	if len(skipped) != 0 || len(unknown) != len(full) {
		t.Errorf("no position: %d purchases (want %d), skipped %+v", len(unknown), len(full), skipped)
	}
}

func TestCrossingFares(t *testing.T) {
	falador, _ := ShopSiteByShop(ShopFaladorShield)
	if got := CrossingFares(&reachArdougne, falador); got != 120 {
		t.Errorf("Ardougne to Falador and back: %d, want 120", got)
	}
	if got := CrossingFares(&reachDraynor, falador); got != 0 {
		t.Errorf("Draynor to Falador: %d, want 0", got)
	}
}
