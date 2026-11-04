package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/shopplan"
)

func shopSettings(raw map[string]interface{}) shopplan.Settings {
	ctx := &Context{Settings: raw}
	bag := ctx.ResolveSettings(ShopBuyoutSettings)
	return shopplan.Settings{
		Shop:           bag.Str("shop", shopplan.Shops[0].Label),
		BuyItems:       bag.List("buyItems", nil),
		BudgetGp:       int32(bag.Num("budgetGp", float64(shopplan.DefaultBudgetGp))),
		PerTripGp:      int32(bag.Num("perTripGp", float64(shopplan.DefaultPerTripGp))),
		StopFloorGp:    int32(bag.Num("stopFloorGp", float64(shopplan.DefaultStopFloorGp))),
		RecheckSeconds: bag.Int("recheckSeconds", shopplan.DefaultRecheckSeconds),
		BankBooth:      bag.Str("bankBooth", shopplan.BoothName),
		LogDetail:      bag.Str("logDetail", "Normal"),
	}.Normalised()
}

func TestShopBuyoutSettingsDefaults(t *testing.T) {
	got := shopSettings(map[string]interface{}{})

	if got.Shop != shopplan.Shops[0].Label {
		t.Errorf("Shop = %q, want the first preset", got.Shop)
	}
	if len(got.BuyItems) != 0 {
		t.Errorf("no selection should mean the whole shop, got %v", got.BuyItems)
	}
	if got.BudgetGp != shopplan.DefaultBudgetGp || got.PerTripGp != shopplan.DefaultPerTripGp {
		t.Errorf("budget defaults should match the TS manifest, got %+v", got)
	}
	if got.StopFloorGp != shopplan.DefaultStopFloorGp || got.RecheckSeconds != shopplan.DefaultRecheckSeconds {
		t.Errorf("floor/recheck defaults should match the TS manifest, got %+v", got)
	}
	if got.BankBooth != shopplan.BoothName {
		t.Errorf("BankBooth = %q, want %q", got.BankBooth, shopplan.BoothName)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
}

func TestShopBuyoutSettingsOverrides(t *testing.T) {
	arrows, ok := shopplan.ShopByKey("archeryshop")
	if !ok {
		t.Fatal("the archery shop preset is missing")
	}
	got := shopSettings(map[string]interface{}{
		"shop":           arrows.Label,
		"buyItems":       []interface{}{"steel arrow", "Shortbow"},
		"budgetGp":       float64(4000),
		"perTripGp":      float64(1500),
		"stopFloorGp":    float64(250),
		"recheckSeconds": float64(15),
		"bankBooth":      "Bank chest",
		"logDetail":      "Verbose",
	})

	if got.Shop != arrows.Label {
		t.Errorf("Shop = %q, want the archery preset", got.Shop)
	}
	if len(got.BuyItems) != 2 || got.BuyItems[0] != "Steel arrow" || got.BuyItems[1] != "Shortbow" {
		t.Errorf("items should resolve to the table's spelling, got %v", got.BuyItems)
	}
	if got.BudgetGp != 4000 || got.PerTripGp != 1500 || got.StopFloorGp != 250 || got.RecheckSeconds != 15 {
		t.Errorf("numeric overrides should survive resolution, got %+v", got)
	}
	if got.BankBooth != "Bank chest" || !got.Verbose() {
		t.Errorf("booth/verbosity overrides should survive, got %+v", got)
	}
}

// A list setting resolved through a schema used to come back empty (the internal/settings bug
// the GemCutter port found and fixed). For this script that failure mode is expensive rather
// than merely wrong: an empty selection means "buy the WHOLE shop out", so the run would spend
// the entire budget on stock nobody asked for. Pin it here too.
func TestShopBuyoutItemSelectionSurvivesSchemaResolution(t *testing.T) {
	got := shopSettings(map[string]interface{}{
		"buyItems": []interface{}{"Death rune"},
	})
	if len(got.BuyItems) != 1 || got.BuyItems[0] != "Death rune" {
		t.Fatalf("a one-item selection must survive resolution, got %v", got.BuyItems)
	}
	chosen := got.Chosen()
	if len(chosen) != 1 || !chosen["death rune"] {
		t.Errorf("Chosen() should carry exactly the selection, got %v", chosen)
	}
}

// Choosing an item that belongs to a DIFFERENT preset must not silently widen the run.
func TestShopBuyoutDropsItemsTheChosenShopDoesNotStock(t *testing.T) {
	runes := shopplan.Shops[0]
	got := shopSettings(map[string]interface{}{
		"shop":     runes.Label,
		"buyItems": []interface{}{"Fire rune", "Shortbow"},
	})
	if len(got.BuyItems) != 1 || got.BuyItems[0] != "Fire rune" {
		t.Errorf("the bow belongs to the other shop and should be dropped, got %v", got.BuyItems)
	}
	if dropped := shopplan.DroppedItems(runes, []string{"Fire rune", "Shortbow"}); len(dropped) != 1 {
		t.Errorf("DroppedItems should report the bow so OnStart can warn, got %v", dropped)
	}
}

func TestShopBuyoutRegistered(t *testing.T) {
	b, ok := New("ShopBuyout")
	if !ok || b == nil {
		t.Fatal("ShopBuyout should be registered in the bot registry")
	}
	// Buying from a shop trains nothing, so this script deliberately has no primary skill —
	// the same treatment MuleReceiver gets, rather than a guessed entry.
	if skill, ok := PrimarySkill("ShopBuyout"); ok {
		t.Errorf("ShopBuyout should have no primary skill, got %q", skill)
	}
}
