package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/essenceplan"
)

// buildEssenceSettings is essenceBot.OnStart's own load (essenceplan.DecodeSettings), so a change to one
// without the other fails a test rather than silently drifting. A settings error fails the test that asked.
func buildEssenceSettings(values map[string]interface{}) essenceplan.Settings {
	got, err := essenceplan.DecodeSettings(values)
	if err != nil {
		panic(err)
	}
	return got
}

func TestEssenceSettingsDefaults(t *testing.T) {
	for _, values := range []map[string]interface{}{{}, nil} {
		got := buildEssenceSettings(values)
		want := essenceplan.DefaultSettings()
		if got != want {
			t.Errorf("buildEssenceSettings(%v) = %+v, want the manifest defaults %+v", values, got, want)
		}
		if got.Donor().Enabled() {
			t.Errorf("buildEssenceSettings(%v): hand-overs must default off (blank mule)", values)
		}
	}
}

func TestEssenceSettingsOverrides(t *testing.T) {
	got := buildEssenceSettings(map[string]interface{}{
		"pickaxe": "Rune", "mule": "mulecon", "muleAt": 500.0, "muleKeep": "Uncut sapphire, Coins", "logDetail": "Verbose",
	})
	want := essenceplan.DefaultSettings()
	want.Pickaxe, want.Mule, want.MuleAt, want.MuleKeep, want.LogDetail = "Rune", "mulecon", 500, "Uncut sapphire, Coins", "Verbose"
	if got != want {
		t.Errorf("buildEssenceSettings(overrides) = %+v, want %+v", got, want)
	}
	if !got.Donor().Enabled() {
		t.Error("a named mule with a positive muleAt must enable hand-overs")
	}
	if !got.Verbose() {
		t.Error("logDetail Verbose must read back Verbose()")
	}
}

func TestEssenceSettingsMuleAtClampedToDonorMax(t *testing.T) {
	got := buildEssenceSettings(map[string]interface{}{"muleAt": 99_999_999.0})
	if got.MuleAt <= 0 || got.MuleAt > 10_000_000 {
		t.Errorf("muleAt = %d, want clamped within donorplan.MaxMuleAt", got.MuleAt)
	}
}

func TestEssenceBotKeepInPackHoldsOnlyAUsablePickaxe(t *testing.T) {
	b := &essenceBot{settings: essenceplan.Settings{Pickaxe: essenceplan.BestAvailable}}
	if got := b.keepInPack(41, []string{"Rune pickaxe", "Rune essence"}); len(got) != 1 || got[0] != "Rune pickaxe" {
		t.Errorf("keepInPack(held rune pickaxe) = %v, want [Rune pickaxe]", got)
	}
	if got := b.keepInPack(41, []string{"Rune essence"}); len(got) != 0 {
		t.Errorf("keepInPack(no pickaxe held) = %v, want empty", got)
	}
	// A specific tier selection not held is not "kept" either — a bank trip should withdraw
	// the right one, not silently protect whatever tier happens to be in the pack.
	b2 := &essenceBot{settings: essenceplan.Settings{Pickaxe: "Rune"}}
	if got := b2.keepInPack(41, []string{"Iron pickaxe"}); len(got) != 0 {
		t.Errorf("keepInPack(wrong tier held) = %v, want empty", got)
	}
}

// The supply settings come through the same load: a Guild miner is the supply loop, one note stack, 5,000.
func TestEssenceSettingsGuildMiner(t *testing.T) {
	got := buildEssenceSettings(map[string]interface{}{"gateway": "guild", "mule": "mulecon", "muleStopAt": 90000.0})
	if !got.SupplyLoop() || got.MuleAt != essenceplan.GuildMuleAt || got.MuleItems != essenceplan.MuleItemsEssence || got.MuleStopAt != 90000 || got.Batch != essenceplan.DefaultBatch {
		t.Errorf("%+v", got)
	}
	if _, err := essenceplan.DecodeSettings(map[string]interface{}{"muleStop": 1}); err == nil {
		t.Error("a typo in a setting must fail the start, not be ignored")
	}
}
