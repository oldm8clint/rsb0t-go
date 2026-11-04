package shopplan

import "testing"

func mustShop(t *testing.T, key string) Shop {
	t.Helper()
	s, ok := ShopByKey(key)
	if !ok {
		t.Fatalf("shop %q missing from the preset table", key)
	}
	return s
}

func mustItem(t *testing.T, s Shop, name string) Item {
	t.Helper()
	it, ok := s.ItemByName(name)
	if !ok {
		t.Fatalf("%s does not stock %q", s.Key, name)
	}
	return it
}

// At exactly the baseline stock the haggle term is zero, so a unit costs the object's own
// cost scaled by the keeper's sell multiplier and nothing else. Both Varrock keepers carry
// shop_sell_multiplier=1000, which is 1.000x, so a fire rune costs exactly its cost= of 4.
func TestUnitPriceAtBaselineIsTheScaledCost(t *testing.T) {
	shop := mustShop(t, "runeshop")
	fire := mustItem(t, shop, "Fire rune")
	if got := UnitPrice(fire, shop, fire.Baseline); got != 4 {
		t.Errorf("a fire rune at baseline stock should cost its cost= of 4, got %d", got)
	}
	arrows := mustShop(t, "archeryshop")
	steel := mustItem(t, arrows, "Steel arrow")
	if got := UnitPrice(steel, arrows, steel.Baseline); got != 12 {
		t.Errorf("a steel arrow at baseline stock should cost 12, got %d", got)
	}
}

// Draining the shelf raises the price, by shop_delta per unit below baseline against the
// 1000-scale. 100 fire runes below baseline is a haggle of -1000, so the multiplier doubles.
func TestUnitPriceRisesAsStockDrains(t *testing.T) {
	shop := mustShop(t, "runeshop")
	fire := mustItem(t, shop, "Fire rune")
	if got := UnitPrice(fire, shop, fire.Baseline-100); got != 8 {
		t.Errorf("100 below baseline should double a 4gp rune to 8, got %d", got)
	}
	if UnitPrice(fire, shop, fire.Baseline-10) < UnitPrice(fire, shop, fire.Baseline) {
		t.Error("draining stock must never lower the price")
	}
}

// Content clamps the haggle term to [-5000, 1000] and then floors the percentage at 100.
// An empty shelf hits the lower clamp; an over-stocked one hits both the upper clamp and the
// percentage floor, and then the buying-side floor of 1gp.
func TestUnitPriceHitsContentsClamps(t *testing.T) {
	shop := mustShop(t, "runeshop")
	death := mustItem(t, shop, "Death rune")
	// stock 0 is 1000 below baseline: -1000 * 10 = -10000, clamped to -5000, so the
	// percentage is 1000 + 5000 = 6000 and a 30gp rune costs (6000 * 30) / 1000 = 180.
	if got := UnitPrice(death, shop, 0); got != 180 {
		t.Errorf("an empty death rune shelf should hit the -5000 haggle clamp at 180gp, got %d", got)
	}
	fire := mustItem(t, shop, "Fire rune")
	// 1000 OVER baseline: +10000 clamped to +1000, so the percentage is 1000 - 1000 = 0,
	// floored to 100, giving (100 * 4) / 1000 = 0, floored again to the buying-side minimum.
	if got := UnitPrice(fire, shop, fire.Baseline+1000); got != 1 {
		t.Errorf("a heavily over-stocked shelf should floor at 1gp, got %d", got)
	}
}

// Content's buying side never charges 0, even for a 1gp object on a glutted shelf.
func TestUnitPriceNeverDropsBelowOne(t *testing.T) {
	arrows := mustShop(t, "archeryshop")
	bronze := mustItem(t, arrows, "Bronze arrow")
	for _, stock := range []int32{0, bronze.Baseline, bronze.Baseline + 5000} {
		if got := UnitPrice(bronze, arrows, stock); got < 1 {
			t.Errorf("UnitPrice at stock %d = %d, must never be below 1", stock, got)
		}
	}
}

// Content's buy_item loop recomputes the price for every single unit, so a bulk purchase
// costs more than units times the opening price.
func TestBuyoutCostChargesEachUnitAtItsOwnPrice(t *testing.T) {
	shop := mustShop(t, "runeshop")
	fire := mustItem(t, shop, "Fire rune")
	const stock, units = 2000, 200
	got := BuyoutCost(fire, shop, stock, units)

	want := int32(0)
	for i := int32(0); i < units; i++ {
		want += UnitPrice(fire, shop, stock-i)
	}
	if got != want {
		t.Errorf("BuyoutCost = %d, want the per-unit sum %d", got, want)
	}
	if flat := UnitPrice(fire, shop, stock) * units; got <= flat {
		t.Errorf("BuyoutCost %d should exceed the flat opening-price estimate %d", got, flat)
	}
}

func TestBuyoutPlanTakesTheValuableItemsFirst(t *testing.T) {
	shop := mustShop(t, "runeshop")
	stock := map[int32]int32{}
	for _, it := range shop.Items {
		stock[it.ID] = it.Baseline
	}
	plan := BuyoutPlan(shop, stock, 1000, nil)
	if len(plan) == 0 {
		t.Fatal("1000gp against a full rune shop should plan something")
	}
	if plan[0].Item.Key != "deathrune" {
		t.Errorf("the most valuable stocked item should come first, got %q", plan[0].Item.Key)
	}
	for i := 1; i < len(plan); i++ {
		if plan[i].Item.Cost > plan[i-1].Item.Cost {
			t.Errorf("plan is not ordered most-valuable-first at index %d", i)
		}
	}
	total := int32(0)
	for _, w := range plan {
		total += w.EstCost
	}
	if total > 1000 {
		t.Errorf("the plan estimates %dgp against a 1000gp purse", total)
	}
}

func TestBuyoutPlanEmptySelectionMeansTheWholeShop(t *testing.T) {
	shop := mustShop(t, "runeshop")
	stock := map[int32]int32{}
	for _, it := range shop.Items {
		stock[it.ID] = it.Baseline
	}
	// A purse far larger than the shelf is worth, so every row can be afforded.
	all := BuyoutPlan(shop, stock, 10000000, nil)
	if len(all) != len(shop.Items) {
		t.Errorf("no selection should mean all %d rows, planned %d", len(shop.Items), len(all))
	}
	one := BuyoutPlan(shop, stock, 10000000, map[string]bool{"fire rune": true})
	if len(one) != 1 || one[0].Item.Key != "firerune" {
		t.Errorf("a one-item selection should plan only that item, got %d line(s)", len(one))
	}
}

func TestBuyoutPlanSkipsWhatIsOutOfStock(t *testing.T) {
	shop := mustShop(t, "runeshop")
	stock := map[int32]int32{554: 0, 560: 5}
	plan := BuyoutPlan(shop, stock, 10000, nil)
	for _, w := range plan {
		if w.Item.ID == 554 {
			t.Error("an out-of-stock row must never be planned")
		}
	}
	if len(plan) != 1 || plan[0].Item.Key != "deathrune" || plan[0].Units != 5 {
		t.Errorf("only the 5 stocked death runes should be planned, got %+v", plan)
	}
}

func TestNeedsSpaceSplitsStackableFromNot(t *testing.T) {
	shop := mustShop(t, "archeryshop")
	arrow := mustItem(t, shop, "Bronze arrow")
	bow := mustItem(t, shop, "Shortbow")

	if NeedsSpace(arrow, false, false) || NeedsSpace(bow, false, false) {
		t.Error("a pack with a free slot never needs space")
	}
	if !NeedsSpace(arrow, false, true) {
		t.Error("a full pack holding no arrows yet has nowhere to put the first one")
	}
	if NeedsSpace(arrow, true, true) {
		t.Error("a full pack that already holds a stack can still grow it")
	}
	if !NeedsSpace(bow, true, true) {
		t.Error("an unstackable bow needs its own slot even when the pack holds one already")
	}
}

func TestDecideStage(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		want Stage
	}{
		// The budget being spent still banks, so the haul is deposited before the run stops;
		// the stop itself lives in DecideBank, exactly as the TypeScript orders it.
		{"budget spent still banks the haul", Snapshot{BudgetLeft: 0, CoinsHeld: 5000, MinUnitPrice: 4}, StageBank},
		{"a full pack banks", Snapshot{BudgetLeft: 1000, CoinsHeld: 5000, PackFull: true, MinUnitPrice: 4}, StageBank},
		{"too few coins to buy anything banks", Snapshot{BudgetLeft: 1000, CoinsHeld: 3, MinUnitPrice: 4}, StageBank},
		{"funded and with room buys", Snapshot{BudgetLeft: 1000, CoinsHeld: 500, MinUnitPrice: 4}, StageBuy},
	}
	for _, c := range cases {
		if got := DecideStage(c.snap); got != c.want {
			t.Errorf("%s: DecideStage = %q, want %q", c.name, got, c.want)
		}
	}
}

// The live failure this whole pair of helpers exists for: a run bought fire runes down to 7gp
// and then waited at the shelf forever, because 7gp cleared the 4gp BASELINE price while the
// drained shelf was charging 16gp. The stock was never the problem.
func TestCanAffordAnythingUsesTheCurrentPriceNotTheBaseline(t *testing.T) {
	shop := mustShop(t, "runeshop")
	fire := mustItem(t, shop, "Fire rune")

	// A shelf drained far below baseline charges much more than the baseline 4gp.
	drained := map[int32]int32{fire.ID: 1700}
	price := UnitPrice(fire, shop, 1700)
	if price <= UnitPrice(fire, shop, fire.Baseline) {
		t.Fatalf("this test needs a drained shelf to be dearer; got %d", price)
	}
	chosen := map[string]bool{"fire rune": true}

	if !AnyChosenInStock(shop, drained, chosen) {
		t.Error("1700 runes on the shelf is certainly in stock")
	}
	if CanAffordAnything(shop, drained, 7, chosen) {
		t.Errorf("7gp must not count as affording a rune that currently costs %d", price)
	}
	if !CanAffordAnything(shop, drained, price, chosen) {
		t.Errorf("exactly %dgp should afford one unit at that price", price)
	}
}

// The two reasons a pass plans nothing need opposite responses, so they must be separable.
func TestAnyChosenInStockSeparatesAnEmptyShelfFromAnEmptyPurse(t *testing.T) {
	shop := mustShop(t, "runeshop")
	chosen := map[string]bool{"fire rune": true}
	empty := map[int32]int32{554: 0}

	if AnyChosenInStock(shop, empty, chosen) {
		t.Error("a drained shelf is not in stock")
	}
	if CanAffordAnything(shop, empty, 1000000, chosen) {
		t.Error("no amount of coins can buy from an empty shelf")
	}
	// A chosen item out of stock while an unchosen one is stocked is still "nothing to buy".
	if AnyChosenInStock(shop, map[int32]int32{560: 50}, chosen) {
		t.Error("death runes in stock say nothing about the chosen fire runes")
	}
}

// Without NeedCoins the run stands at the keeper forever; this is the regression pin.
func TestDecideStageBanksWhenItCannotAffordAnything(t *testing.T) {
	snap := Snapshot{BudgetLeft: 9007, CoinsHeld: 7, MinUnitPrice: 4, NeedCoins: true}
	if got := DecideStage(snap); got != StageBank {
		t.Errorf("a run that cannot afford the current price must go back to the bank, got %q", got)
	}
	// The same purse without the flag still buys, which is what MinUnitPrice alone decided
	// live — and why the flag had to exist.
	snap.NeedCoins = false
	if got := DecideStage(snap); got != StageBuy {
		t.Errorf("without the flag the old behaviour stands, got %q", got)
	}
}

func TestDecideBank(t *testing.T) {
	// Budget first, as the TypeScript checks it.
	if d := DecideBank(100000, 0, 1000, 0, 5000); !d.Stop {
		t.Error("a spent budget must stop the run")
	}
	// Then the bank floor.
	if d := DecideBank(4999, 0, 1000, 50000, 5000); !d.Stop {
		t.Error("bank coins below the floor must stop the run")
	}
	// A normal top-up draws the per-trip amount less what is already carried.
	d := DecideBank(100000, 400, 1000, 50000, 5000)
	if d.Stop || d.Withdraw != 600 {
		t.Errorf("want a 600gp top-up, got %+v", d)
	}
	// Never more than the budget still allows.
	if d := DecideBank(100000, 0, 1000, 250, 0); d.Withdraw != 250 {
		t.Errorf("the withdrawal must respect the remaining budget, got %d", d.Withdraw)
	}
	// Never more than the bank actually holds.
	if d := DecideBank(300, 0, 1000, 50000, 0); d.Withdraw != 300 {
		t.Errorf("the withdrawal must be capped by the bank's coins, got %d", d.Withdraw)
	}
	// Already carrying enough draws nothing.
	if d := DecideBank(100000, 1000, 1000, 50000, 5000); d.Stop || d.Withdraw != 0 {
		t.Errorf("a funded pack needs no withdrawal, got %+v", d)
	}
}

func TestMinUnitPriceIsTheCheapestChosenRow(t *testing.T) {
	shop := mustShop(t, "runeshop")
	// Mind and body runes cost 3 at baseline, the cheapest rows in the shop.
	if got := MinUnitPrice(shop, nil); got != 3 {
		t.Errorf("MinUnitPrice over the whole rune shop = %d, want 3", got)
	}
	if got := MinUnitPrice(shop, map[string]bool{"death rune": true}); got != 30 {
		t.Errorf("MinUnitPrice for death runes alone = %d, want 30", got)
	}
}

func TestSettingsNormalised(t *testing.T) {
	got := Settings{
		Shop:     "aubury's runes — varrock (varrock east bank)",
		BuyItems: []string{"fire rune", "Fire rune", "Nonesuch"},
	}.Normalised()

	if got.Shop != Shops[0].Label {
		t.Errorf("a shop label should resolve to its canonical spelling, got %q", got.Shop)
	}
	if len(got.BuyItems) != 1 || got.BuyItems[0] != "Fire rune" {
		t.Errorf("items should de-duplicate into the table's spelling, got %v", got.BuyItems)
	}
	if got.BudgetGp != DefaultBudgetGp || got.PerTripGp != DefaultPerTripGp || got.RecheckSeconds != DefaultRecheckSeconds {
		t.Errorf("zero numbers should fall back to the manifest defaults, got %+v", got)
	}
	if got.BankBooth != BoothName {
		t.Errorf("a blank bank booth should fall back to %q, got %q", BoothName, got.BankBooth)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}

	// An unrecognised shop falls back to the first preset rather than leaving the run without
	// a keeper to walk to.
	if fallback := (Settings{Shop: "no such shop"}).Normalised(); fallback.Shop != Shops[0].Label {
		t.Errorf("an unknown shop should fall back to the first preset, got %q", fallback.Shop)
	}
}

// An item name is dropped when the CHOSEN shop does not stock it, which silently widens the
// run to the whole shop — so it has to be reportable.
func TestDroppedItemsReportsWhatTheShopDoesNotStock(t *testing.T) {
	shop := mustShop(t, "runeshop")
	dropped := DroppedItems(shop, []string{"Fire rune", "Shortbow", "", "  "})
	if len(dropped) != 1 || dropped[0] != "Shortbow" {
		t.Errorf("only the bow belongs to the other shop, got %v", dropped)
	}
}

// THE ID TRAP, pinned in both directions, because this table contains both halves of it and a
// future edit deriving an id by arithmetic would silently corrupt one half. See the package
// doc comment.
func TestObjectIDsAreNeverDerivedByArithmetic(t *testing.T) {
	runes := mustShop(t, "runeshop")
	fire := mustItem(t, runes, "Fire rune")
	water := mustItem(t, runes, "Water rune")
	// Direction one: id+1 is NOT a note here. Fire rune 554 plus one is water rune 555, a
	// different item the same shop stocks. Deriving "the noted variant" by arithmetic would
	// buy the wrong rune.
	if fire.ID+1 != water.ID {
		t.Fatalf("this test is pinning that fire %d and water %d are adjacent ids", fire.ID, water.ID)
	}
	if fire.ID != 554 || water.ID != 555 {
		t.Errorf("rune ids must stay the live-confirmed 554/555, got %d/%d", fire.ID, water.ID)
	}

	// Direction two: for the bows id+1 IS the note, so the table must hold the item and never
	// the note. 842 is the noted Shortbow and must not appear anywhere.
	arrows := mustShop(t, "archeryshop")
	bow := mustItem(t, arrows, "Shortbow")
	if bow.ID != 841 {
		t.Errorf("Shortbow must be the free-to-play item 841, not one of the other three ids sharing that name, got %d", bow.ID)
	}
	for _, s := range Shops {
		for _, it := range s.Items {
			// No note, and no members-only twin, ever belongs in a stock row.
			if it.ID == 842 || it.ID == 840 || it.ID == 50 || it.ID == 48 {
				t.Errorf("%s: id %d is a note or a members twin, not the stocked object", it.Key, it.ID)
			}
		}
	}
}

// ItemByName is only safe because no single shop stocks two rows under one display name;
// across the whole object table those names are ambiguous. Pin the property the safety rests on.
func TestNoShopStocksTwoRowsUnderOneName(t *testing.T) {
	for _, s := range Shops {
		names := map[string]bool{}
		ids := map[int32]bool{}
		for _, it := range s.Items {
			lower := lower(it.Name)
			if names[lower] {
				t.Errorf("%s stocks two rows named %q, which makes ItemByName ambiguous", s.Key, it.Name)
			}
			names[lower] = true
			if ids[it.ID] {
				t.Errorf("%s stocks object %d twice", s.Key, it.ID)
			}
			ids[it.ID] = true
			if it.ID <= 0 || it.Cost <= 0 || it.Baseline <= 0 || it.RestockTicks <= 0 {
				t.Errorf("%s/%s has an unset field: %+v", s.Key, it.Key, it)
			}
		}
	}
}

func lower(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}

func TestShopLabelsAndLookups(t *testing.T) {
	if len(ShopLabels()) != len(Shops) {
		t.Error("every preset should offer a label")
	}
	for _, s := range Shops {
		got, ok := ShopByLabel(s.Label)
		if !ok || got.Key != s.Key {
			t.Errorf("ShopByLabel(%q) did not round-trip", s.Label)
		}
		if s.Sell <= 0 || s.Delta <= 0 || s.Keeper == "" || len(s.Items) == 0 {
			t.Errorf("preset %q is missing keeper params or stock", s.Key)
		}
		if s.BankStand == s.ShopStand {
			t.Errorf("preset %q banks and shops on one tile, which cannot be right", s.Key)
		}
	}
	if _, ok := ShopByLabel("nothing"); ok {
		t.Error("an unknown label must not resolve")
	}
	if n := len(AllItemNames()); n == 0 {
		t.Error("AllItemNames should offer the settings option list")
	}
}

func TestItemByIDFindsTheStockRow(t *testing.T) {
	shop := mustShop(t, "runeshop")
	it, ok := shop.ItemByID(560)
	if !ok || it.Key != "deathrune" {
		t.Errorf("ItemByID(560) should be the death rune, got %+v/%v", it, ok)
	}
	if _, ok := shop.ItemByID(841); ok {
		t.Error("the rune shop must not resolve a bow id")
	}
}
