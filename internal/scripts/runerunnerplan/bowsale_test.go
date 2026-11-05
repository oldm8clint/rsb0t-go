package runerunnerplan

import "testing"

func TestPlanBowDisposal(t *testing.T) {
	outlets := SellOutlets
	bows := []BowStack{
		{Name: "Yew longbow (u)", Cost: 320, Count: 40, Strings: true}, // the best stringing option: kept
		{Name: "Maple shortbow (u)", Cost: 100, Count: 10},
		{Name: "Oak longbow (u)", Cost: 160, Count: 10},
		{Name: "Willow shortbow (u)", Cost: 50, Count: 0}, // nothing held
	}
	// An account that can cast alchs everything it does not string: the cast nets 96-20 and 60-20 a bow, more than a general store's 40%.
	got := PlanBowDisposal(bows, true, outlets)
	if len(got) != 3 || got[0].Name != "Oak longbow (u)" || got[0].Action != BowAlch || got[1].Name != "Maple shortbow (u)" || got[2].Action != BowKeep {
		t.Fatalf("with casts: %+v", got)
	}
	if got[0].PerUnit != 96-NatureCost {
		t.Errorf("oak per unit %d", got[0].PerUnit)
	}
	// One that cannot cast sells to a general store, the best return per unit first, at the store that pays most for the stack.
	got = PlanBowDisposal(bows, false, outlets)
	if len(got) != 3 || got[0].Action != BowSell || got[0].Name != "Oak longbow (u)" || got[0].Outlet == "" || got[2].Action != BowKeep {
		t.Fatalf("no casts: %+v", got)
	}
	// Ten oak longbows at cost 160 pay 64 for the first and 3% of the cost less for each next one, at a delta-30 store.
	if want := SaleTotal(160, 400, 30, -1, 0, 10); got[0].Outlet != "Khazard" && got[0].Return < want {
		t.Errorf("oak return %d under the delta-30 total %d at %s", got[0].Return, want, got[0].Outlet)
	}
	// A bow only a sale beats the cast on: a Nature rune dearer than the margin.
	cheap := []BowStack{{Name: "Shortbow (u)", Cost: 40, Count: 3}}
	if got = PlanBowDisposal(cheap, true, outlets); len(got) != 1 || got[0].Action != BowSell {
		t.Errorf("a 40-coin bow's cast nets 4: sell it: %+v", got)
	}
}
