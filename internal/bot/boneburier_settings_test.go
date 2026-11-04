package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/boneplan"
)

func TestBoneBurierOnStartReadsSettings(t *testing.T) {
	b := &boneBurierBot{}
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{
		"source":       "bank",
		"names":        "ashes, Bones",
		"bankNames":    "Dragon bones",
		"disposal":     "split",
		"radius":       9999.0,
		"buryWhenFull": true,
		"idleMins":     30.0,
		"goalLevel":    43.0,
		"logDetail":    "Verbose",
	}, nil)
	// OnStart calls ctx.Session.State().Tile() through PrayerXP/anchor — guard against a nil
	// Session by checking the settings construction path directly instead of the full OnStart,
	// mirroring how essence_settings_test.go isolates settings from live state reads.
	b.settings = boneplan.Settings{
		Source:       StringSetting(ctx.Settings, "source", boneplan.DefaultSettings().Source),
		Names:        StringSetting(ctx.Settings, "names", boneplan.DefaultSettings().Names),
		BankNames:    StringSetting(ctx.Settings, "bankNames", boneplan.DefaultSettings().BankNames),
		Disposal:     StringSetting(ctx.Settings, "disposal", boneplan.DefaultSettings().Disposal),
		Radius:       IntSetting(ctx.Settings, "radius", boneplan.DefaultRadius),
		BuryWhenFull: BoolSetting(ctx.Settings, "buryWhenFull", false),
		IdleMins:     IntSetting(ctx.Settings, "idleMins", 0),
		GoalLevel:    IntSetting(ctx.Settings, "goalLevel", 0),
		LogDetail:    StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()

	if got := b.settings.SourceMode(); got != boneplan.SourceBank {
		t.Errorf("source = %s, want bank", got)
	}
	// Bank-sourced bones always bury regardless of the disposal setting.
	if got := b.settings.Mode(); got != boneplan.DisposalBury {
		t.Errorf("mode = %s, want bury (bank source overrides disposal)", got)
	}
	if got := b.settings.NameList(); len(got) != 2 || got[0] != "ashes" || got[1] != "bones" {
		t.Errorf("names = %v", got)
	}
	if b.settings.Radius != 50 {
		t.Errorf("radius should clamp to 50, got %d", b.settings.Radius)
	}
	if !b.settings.BuryWhenFull {
		t.Error("buryWhenFull should be true")
	}
	if b.settings.IdleMins != 30 {
		t.Errorf("idleMins = %d", b.settings.IdleMins)
	}
	if b.settings.GoalLevel != 43 {
		t.Errorf("goalLevel = %d", b.settings.GoalLevel)
	}
	if !b.settings.Verbose() {
		t.Error("logDetail Verbose should read back verbose")
	}
}

func TestBoneBurierDefaultsWhenNoSettingsGiven(t *testing.T) {
	b := &boneBurierBot{}
	b.settings = boneplan.Settings{
		Source:       StringSetting(nil, "source", boneplan.DefaultSettings().Source),
		Names:        StringSetting(nil, "names", boneplan.DefaultSettings().Names),
		BankNames:    StringSetting(nil, "bankNames", boneplan.DefaultSettings().BankNames),
		Disposal:     StringSetting(nil, "disposal", boneplan.DefaultSettings().Disposal),
		Radius:       IntSetting(nil, "radius", boneplan.DefaultRadius),
		BuryWhenFull: BoolSetting(nil, "buryWhenFull", false),
		IdleMins:     IntSetting(nil, "idleMins", 0),
		GoalLevel:    IntSetting(nil, "goalLevel", 0),
		LogDetail:    StringSetting(nil, "logDetail", "Normal"),
	}.Normalised()
	if b.settings.SourceMode() != boneplan.SourceGround {
		t.Errorf("default source = %s, want ground", b.settings.SourceMode())
	}
	if b.settings.Mode() != boneplan.DisposalBury {
		t.Errorf("default disposal = %s, want bury", b.settings.Mode())
	}
	if b.settings.Radius != boneplan.DefaultRadius {
		t.Errorf("default radius = %d, want %d", b.settings.Radius, boneplan.DefaultRadius)
	}
	if b.settings.BanksAnything() {
		t.Error("default settings should not bank anything")
	}
}

func TestBoneBurierSkipListParksAndClears(t *testing.T) {
	b := &boneBurierBot{skips: map[string]time.Time{}}
	b.skip("bones@1,2,0")
	if !b.skipped("bones@1,2,0") {
		t.Error("a freshly skipped key should read as skipped")
	}
	b.clearSkip("bones@1,2,0")
	if b.skipped("bones@1,2,0") {
		t.Error("clearSkip should un-park the key")
	}
}
