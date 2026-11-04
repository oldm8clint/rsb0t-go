package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/thieverplan"
)

func TestThieverSettingsDefaults(t *testing.T) {
	for _, values := range []map[string]interface{}{{}, nil} {
		got := thieverSettings(values)
		want := thieverplan.DefaultSettings()
		if got != want {
			t.Errorf("thieverSettings(%v) = %+v, want the manifest defaults %+v", values, got, want)
		}
		if got.Donor().Enabled() {
			t.Errorf("thieverSettings(%v): hand-overs must default off (blank mule)", values)
		}
		if !got.Suicide {
			t.Errorf("thieverSettings(%v): suicide farming must default on", values)
		}
	}
}

func TestThieverSettingsOverrides(t *testing.T) {
	got := thieverSettings(map[string]interface{}{
		"schedule": "1: Man, 40: Guard", "suicide": false, "food": "Shark",
		"foodWithdraw": 15.0, "minFood": 3.0, "eatBelowHp": 60.0, "bankAt": 10.0,
		"fare": 100.0, "restBelowHp": 55.0, "guideNpc": "Guide", "skipOption": "Yep",
		"mule": "mulecon", "askMuleForFood": false, "muleAt": 5.0, "muleKeep": "Coins, Shears",
		"logDetail": "Verbose",
	})
	if got.Schedule != "1: Man, 40: Guard" || got.Suicide {
		t.Errorf("thieverSettings(overrides) = %+v", got)
	}
	if got.Food != "Shark" || got.FoodWithdraw != 15 || got.MinFood != 3 || got.EatBelowHP != 60 {
		t.Errorf("thieverSettings(overrides) = %+v", got)
	}
	if got.BankAt != 10 || got.Fare != 100 || got.RestBelowHP != 55 {
		t.Errorf("thieverSettings(overrides) = %+v", got)
	}
	if got.Mule != "mulecon" || got.AskMuleForFood || got.MuleAt != 5 || got.MuleKeep != "Coins, Shears" || got.LogDetail != "Verbose" {
		t.Errorf("thieverSettings(overrides) = %+v", got)
	}
	if !got.Donor().Enabled() {
		t.Error("a named mule with a positive muleAt must enable hand-overs")
	}
}

func TestThieverSettingsScheduleBlankKeepsDefault(t *testing.T) {
	got := thieverSettings(map[string]interface{}{"schedule": ""})
	if got.Schedule != thieverplan.DefaultSchedule {
		t.Errorf("blank schedule should keep the default, got %q", got.Schedule)
	}
}

func TestThieverSettingsMuleAtClampedToDonorMax(t *testing.T) {
	got := thieverSettings(map[string]interface{}{"muleAt": 99_999_999.0})
	if got.MuleAt <= 0 || got.MuleAt > 1_000_000 {
		t.Errorf("muleAt = %d, want clamped within donorplan.MaxMuleAt", got.MuleAt)
	}
}
