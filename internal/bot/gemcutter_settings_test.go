package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/gemplan"
)

func gemSettings(raw map[string]interface{}) (gemplan.Settings, bool) {
	ctx := &Context{Settings: raw}
	bag := ctx.ResolveSettings(GemCutterSettings)
	s := gemplan.Settings{
		Gems:      bag.List("gems", nil),
		BankBooth: bag.Str("bankBooth", gemplan.BoothName),
		LogDetail: bag.Str("logDetail", "Normal"),
	}.Normalised()
	return s, bag.Bool("membersWorld", true)
}

func TestGemCutterSettingsDefaults(t *testing.T) {
	got, members := gemSettings(map[string]interface{}{})
	if len(got.Gems) != 0 {
		t.Errorf("no gems selected should mean every gem the level allows, got %v", got.Gems)
	}
	if got.BankBooth != gemplan.BoothName {
		t.Errorf("BankBooth = %q, want %q", got.BankBooth, gemplan.BoothName)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
	if !members {
		t.Error("membersWorld should default true — the engine this port was proven against is a members world")
	}
	// With no selection, every gem at a high enough level is in play.
	if n := len(gemplan.Eligible(99, got.Gems, nil, members)); n != len(gemplan.Gems) {
		t.Errorf("default settings should allow all %d gems at Crafting 99, got %d", len(gemplan.Gems), n)
	}
}

func TestGemCutterSettingsOverrides(t *testing.T) {
	got, members := gemSettings(map[string]interface{}{
		"gems":         []interface{}{"ruby", "Diamond"},
		"bankBooth":    "Bank chest",
		"membersWorld": false,
		"logDetail":    "Verbose",
	})
	if len(got.Gems) != 2 || got.Gems[0] != "Ruby" || got.Gems[1] != "Diamond" {
		t.Errorf("gems should resolve to their canonical spelling in table order, got %v", got.Gems)
	}
	if got.BankBooth != "Bank chest" {
		t.Errorf("BankBooth = %q, want the override", got.BankBooth)
	}
	if !got.Verbose() {
		t.Error("Verbose logDetail should report Verbose() true")
	}
	if members {
		t.Error("membersWorld false should survive resolution")
	}
}

// A free world must not leave a members-only gem in the run: the server refuses to cut it at
// all, so a run that kept it would spin on an uncuttable pack.
func TestGemCutterSettingsFreeWorldDropsMembersGems(t *testing.T) {
	got, members := gemSettings(map[string]interface{}{
		"gems":         []interface{}{"Opal", "Sapphire"},
		"membersWorld": false,
	})
	eligible := gemplan.Eligible(99, got.Gems, nil, members)
	if len(eligible) != 1 || eligible[0].Key != "sapphire" {
		t.Errorf("a free world should leave only Sapphire of that pair, got %d gem(s)", len(eligible))
	}
}

func TestGemCutterRegistered(t *testing.T) {
	b, ok := New("GemCutter")
	if !ok || b == nil {
		t.Fatal("GemCutter should be registered in the bot registry")
	}
	if skill, ok := PrimarySkill("GemCutter"); !ok || skill != "Crafting" {
		t.Errorf("PrimarySkill(GemCutter) = %q/%v, want Crafting/true", skill, ok)
	}
}
