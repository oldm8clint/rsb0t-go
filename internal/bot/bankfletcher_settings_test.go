package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/bankfletchplan"
)

func TestBankFletchSettingsDefaults(t *testing.T) {
	settings := map[string]interface{}{}
	got := bankfletchplan.Settings{
		Material:  StringSetting(settings, "material", bankfletchplan.Logs),
		LogDetail: StringSetting(settings, "logDetail", "Normal"),
	}.Normalised()
	if got.Material != bankfletchplan.Logs {
		t.Errorf("Material = %q, want %q", got.Material, bankfletchplan.Logs)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
}

func TestBankFletchSettingsOverrides(t *testing.T) {
	settings := map[string]interface{}{
		"material":  "Oak logs",
		"logDetail": "Verbose",
	}
	got := bankfletchplan.Settings{
		Material:  StringSetting(settings, "material", bankfletchplan.Logs),
		LogDetail: StringSetting(settings, "logDetail", "Normal"),
	}.Normalised()
	if got.Material != "Oak logs" {
		t.Errorf("Material override not applied: %+v", got)
	}
	if !got.Verbose() {
		t.Error("Verbose logDetail should report Verbose() true")
	}
}
