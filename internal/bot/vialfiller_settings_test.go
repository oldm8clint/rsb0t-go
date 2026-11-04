package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/vialplan"
)

func TestVialSettingsDefaults(t *testing.T) {
	settings := map[string]interface{}{}
	got := vialplan.Settings{
		Bank:         StringSetting(settings, "bank", "Falador West"),
		BuyVials:     BoolSetting(settings, "buyVials", false),
		BuyEveryRuns: IntSetting(settings, "buyEveryRuns", vialplan.DefaultBuyEveryRuns),
		BuyQty:       IntSetting(settings, "buyQty", vialplan.DefaultBuyQty),
		CoinsPerTrip: IntSetting(settings, "coinsPerTrip", vialplan.DefaultCoinsPerTrip),
		LogDetail:    StringSetting(settings, "logDetail", "Normal"),
	}.Normalised()
	if got.Bank != "Falador West" {
		t.Errorf("Bank = %q, want Falador West", got.Bank)
	}
	if got.BuyVials {
		t.Error("BuyVials should default to false")
	}
	if got.BuyEveryRuns != vialplan.DefaultBuyEveryRuns {
		t.Errorf("BuyEveryRuns = %d, want %d", got.BuyEveryRuns, vialplan.DefaultBuyEveryRuns)
	}
	if got.BuyQty != vialplan.DefaultBuyQty {
		t.Errorf("BuyQty = %d, want %d", got.BuyQty, vialplan.DefaultBuyQty)
	}
	if got.CoinsPerTrip != vialplan.DefaultCoinsPerTrip {
		t.Errorf("CoinsPerTrip = %d, want %d", got.CoinsPerTrip, vialplan.DefaultCoinsPerTrip)
	}
}

func TestVialSettingsOverrides(t *testing.T) {
	settings := map[string]interface{}{
		"bank":         "Falador East",
		"buyVials":     true,
		"buyEveryRuns": float64(10),
		"buyQty":       float64(20),
		"coinsPerTrip": float64(5000),
	}
	got := vialplan.Settings{
		Bank:         StringSetting(settings, "bank", "Falador West"),
		BuyVials:     BoolSetting(settings, "buyVials", false),
		BuyEveryRuns: IntSetting(settings, "buyEveryRuns", vialplan.DefaultBuyEveryRuns),
		BuyQty:       IntSetting(settings, "buyQty", vialplan.DefaultBuyQty),
		CoinsPerTrip: IntSetting(settings, "coinsPerTrip", vialplan.DefaultCoinsPerTrip),
	}.Normalised()
	if got.Bank != "Falador East" || !got.BuyVials || got.BuyEveryRuns != 10 || got.BuyQty != 20 || got.CoinsPerTrip != 5000 {
		t.Errorf("overrides not applied: %+v", got)
	}
}
