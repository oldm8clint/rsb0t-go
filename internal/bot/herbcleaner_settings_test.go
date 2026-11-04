package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/herbplan"
)

func herbSettings(raw map[string]interface{}) (herbplan.Settings, bool) {
	ctx := &Context{Settings: raw}
	bag := ctx.ResolveSettings(HerbCleanerSettings)
	s := herbplan.Settings{
		Herbs:     bag.List("herbs", nil),
		BankBooth: bag.Str("bankBooth", herbplan.BoothName),
		LogDetail: bag.Str("logDetail", "Normal"),
	}.Normalised()
	return s, bag.Bool("membersWorld", true)
}

func TestHerbCleanerSettingsDefaults(t *testing.T) {
	got, members := herbSettings(map[string]interface{}{})
	if len(got.Herbs) != 0 {
		t.Errorf("no herbs selected should mean every herb the level allows, got %v", got.Herbs)
	}
	if got.BankBooth != herbplan.BoothName {
		t.Errorf("BankBooth = %q, want %q", got.BankBooth, herbplan.BoothName)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
	if !members {
		t.Error("membersWorld should default true — every herb is a members object and the engine this port was proven against is a members world")
	}
	if n := len(herbplan.Eligible(99, got.Herbs, nil, members)); n != len(herbplan.Herbs) {
		t.Errorf("default settings should allow all %d herbs at Herblore 99, got %d", len(herbplan.Herbs), n)
	}
}

func TestHerbCleanerSettingsOverrides(t *testing.T) {
	got, members := herbSettings(map[string]interface{}{
		"herbs":        []interface{}{"ranarr WEED", "Guam leaf"},
		"bankBooth":    "Bank chest",
		"membersWorld": false,
		"logDetail":    "Verbose",
	})
	// Canonical spelling, in the order they were listed. Note these must be DISPLAY NAMES: the
	// settings schema matches a list value against its own Options (which are the display names)
	// before the script ever sees it, so a bare table key like "ranarr" is dropped upstream of
	// Normalised. OnStart's warning is what makes that visible rather than silent.
	if len(got.Herbs) != 2 || got.Herbs[0] != "Ranarr weed" || got.Herbs[1] != "Guam leaf" {
		t.Errorf("herbs should resolve to their canonical spelling, got %v", got.Herbs)
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

// The list-setting path specifically: a schema-resolved TypeList used to come back as the
// caller's fallback, which silently widened a run from the chosen item to all of them (the bug
// the GemCutter port found in internal/settings). Pin it for this script too.
func TestHerbCleanerSettingsListResolves(t *testing.T) {
	got, members := herbSettings(map[string]interface{}{
		"herbs": []interface{}{"Guam leaf"},
	})
	if len(got.Herbs) != 1 || got.Herbs[0] != "Guam leaf" {
		t.Fatalf("a one-herb selection must survive schema resolution, got %v", got.Herbs)
	}
	eligible := herbplan.Eligible(99, got.Herbs, nil, members)
	if len(eligible) != 1 || eligible[0].Key != "guam" {
		t.Errorf("selecting one herb must not widen the run, got %d herb(s)", len(eligible))
	}
}

// A free world leaves the run with nothing to do at all, because content refuses the identify
// before it looks at the level. OnStart turns this into a clear refusal rather than a spin.
func TestHerbCleanerSettingsFreeWorldAllowsNothing(t *testing.T) {
	got, members := herbSettings(map[string]interface{}{
		"herbs":        []interface{}{"Guam leaf"},
		"membersWorld": false,
	})
	if n := len(herbplan.Eligible(99, got.Herbs, nil, members)); n != 0 {
		t.Errorf("a free world should leave no herb eligible, got %d", n)
	}
}

// The silent failure this guards against is a run quietly WIDER than asked for: a dropped name
// can leave the list empty, which means "clean everything". Both loss paths must be reported.
func TestDroppedHerbNames(t *testing.T) {
	// A bare table key survives HerbByName but never reaches the script, because the settings
	// schema matches list values against its display-name Options first.
	raw := []string{"Guam leaf", "ranarr", "Banana", "  "}
	kept, _ := herbSettings(map[string]interface{}{"herbs": []interface{}{"Guam leaf", "ranarr", "Banana"}})
	got := droppedHerbNames(raw, kept.Herbs)
	if len(got) != 2 || got[0] != "ranarr" || got[1] != "Banana" {
		t.Errorf("droppedHerbNames = %v, want the key-spelled entry and the typo", got)
	}
	if n := len(droppedHerbNames([]string{"Guam leaf"}, []string{"Guam leaf"})); n != 0 {
		t.Errorf("a name that survived must not be reported as dropped, got %d", n)
	}
	if n := len(droppedHerbNames(nil, nil)); n != 0 {
		t.Errorf("no names means nothing dropped, got %d", n)
	}
}

func TestHerbCleanerRegistered(t *testing.T) {
	b, ok := New("HerbCleaner")
	if !ok || b == nil {
		t.Fatal("HerbCleaner should be registered in the bot registry")
	}
	if skill, ok := PrimarySkill("HerbCleaner"); !ok || skill != "Herblore" {
		t.Errorf("PrimarySkill(HerbCleaner) = %q/%v, want Herblore/true", skill, ok)
	}
}
