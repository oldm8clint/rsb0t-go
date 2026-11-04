package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/alchplan"
)

func TestAlcherSettingsReadAndClamp(t *testing.T) {
	s := alchplan.Settings{
		Spell:        StringSetting(map[string]interface{}{"spell": "High Level Alchemy"}, "spell", alchplan.AutoSpell),
		Items:        StringSetting(map[string]interface{}{"items": "Rune platebody, Adamant kiteshield"}, "items", alchplan.DefaultItems),
		CastsPerTrip: IntSetting(map[string]interface{}{"castsPerTrip": 99999.0}, "castsPerTrip", alchplan.DefaultCastsPerTrip),
		UseFireStaff: BoolSetting(map[string]interface{}{"useFireStaff": false}, "useFireStaff", true),
		LogDetail:    StringSetting(map[string]interface{}{"logDetail": "Verbose"}, "logDetail", "Normal"),
	}.Normalised()

	if s.Spell != "High Level Alchemy" {
		t.Errorf("spell = %q", s.Spell)
	}
	if s.CastsPerTrip != 5000 {
		t.Errorf("castsPerTrip should clamp to 5000, got %d", s.CastsPerTrip)
	}
	if s.UseFireStaff {
		t.Error("useFireStaff should read back false")
	}
	if !s.Verbose() {
		t.Error("logDetail Verbose should read back verbose")
	}
	if got := s.ItemList(); len(got) != 2 || got[0] != "Rune platebody" || got[1] != "Adamant kiteshield" {
		t.Errorf("ItemList = %v", got)
	}
}

func TestAlcherDefaults(t *testing.T) {
	s := alchplan.Settings{
		Spell:        StringSetting(nil, "spell", alchplan.AutoSpell),
		Items:        StringSetting(nil, "items", alchplan.DefaultItems),
		CastsPerTrip: IntSetting(nil, "castsPerTrip", alchplan.DefaultCastsPerTrip),
		UseFireStaff: BoolSetting(nil, "useFireStaff", true),
		LogDetail:    StringSetting(nil, "logDetail", "Normal"),
	}.Normalised()
	if s.Spell != alchplan.AutoSpell {
		t.Errorf("default spell = %q", s.Spell)
	}
	if s.CastsPerTrip != alchplan.DefaultCastsPerTrip {
		t.Errorf("default castsPerTrip = %d", s.CastsPerTrip)
	}
	if !s.UseFireStaff {
		t.Error("default useFireStaff should be true")
	}
}

func TestAlcherOnStartStopsWhenLevelTooLow(t *testing.T) {
	b := &alcherBot{}
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, map[string]interface{}{
		"spell": "High Level Alchemy",
	}, nil)
	// MagicXP/MagicLevel read ctx.Session.State() — guard by constructing settings directly the
	// way essence_settings_test.go isolates settings from a live Session, rather than calling
	// the full OnStart (which needs a real Session for the magic level read).
	b.settings = alchplan.Settings{
		Spell:        StringSetting(ctx.Settings, "spell", alchplan.AutoSpell),
		Items:        StringSetting(ctx.Settings, "items", alchplan.DefaultItems),
		CastsPerTrip: IntSetting(ctx.Settings, "castsPerTrip", alchplan.DefaultCastsPerTrip),
		UseFireStaff: BoolSetting(ctx.Settings, "useFireStaff", true),
		LogDetail:    StringSetting(ctx.Settings, "logDetail", "Normal"),
	}.Normalised()
	b.spell = alchplan.ChooseSpell(b.settings.Spell, 1) // magic level 1: neither spell castable
	if b.spell != nil {
		t.Fatalf("magic level 1 should not resolve a spell, got %+v", b.spell)
	}
}
