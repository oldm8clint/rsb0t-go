package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/flaxplan"
)

func TestFlaxSettingsDefaults(t *testing.T) {
	settings := map[string]interface{}{}
	got := flaxplan.Settings{
		FlaxName:  StringSetting(settings, "flaxName", flaxplan.Flax),
		PickOp:    StringSetting(settings, "pickOp", flaxplan.PickOp),
		BankBooth: StringSetting(settings, "bankBooth", flaxplan.BoothName),
		LogDetail: StringSetting(settings, "logDetail", "Normal"),
	}.Normalised()
	if got.FlaxName != flaxplan.Flax {
		t.Errorf("FlaxName = %q, want %q", got.FlaxName, flaxplan.Flax)
	}
	if got.PickOp != flaxplan.PickOp {
		t.Errorf("PickOp = %q, want %q", got.PickOp, flaxplan.PickOp)
	}
	if got.BankBooth != flaxplan.BoothName {
		t.Errorf("BankBooth = %q, want %q", got.BankBooth, flaxplan.BoothName)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
}

func TestFlaxSettingsOverrides(t *testing.T) {
	settings := map[string]interface{}{
		"flaxName":  "Special flax",
		"pickOp":    "Harvest",
		"bankBooth": "Bank chest",
		"logDetail": "Verbose",
	}
	got := flaxplan.Settings{
		FlaxName:  StringSetting(settings, "flaxName", flaxplan.Flax),
		PickOp:    StringSetting(settings, "pickOp", flaxplan.PickOp),
		BankBooth: StringSetting(settings, "bankBooth", flaxplan.BoothName),
		LogDetail: StringSetting(settings, "logDetail", "Normal"),
	}.Normalised()
	if got.FlaxName != "Special flax" || got.PickOp != "Harvest" || got.BankBooth != "Bank chest" {
		t.Errorf("overrides not applied: %+v", got)
	}
	if !got.Verbose() {
		t.Error("Verbose logDetail should report Verbose() true")
	}
}
