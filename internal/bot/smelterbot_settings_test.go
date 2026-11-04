package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/smeltplan"
)

// smelterSettings resolves through the bot's own helper, so these tests exercise the exact path
// OnStart takes rather than a re-implementation of it.
func smelterSettings(raw map[string]interface{}) smeltplan.Settings {
	s, _ := resolveSmelterSettings(&Context{Settings: raw})
	return s
}

func TestSmelterBotSettingsDefaults(t *testing.T) {
	got := smelterSettings(map[string]interface{}{})
	if got.Bar != "Bronze bar" {
		t.Errorf("Bar = %q, want Bronze bar — the one row a Smithing 1 account can smelt", got.Bar)
	}
	if got.BankBooth != smeltplan.BoothName {
		t.Errorf("BankBooth = %q, want %q", got.BankBooth, smeltplan.BoothName)
	}
	if got.Verbose() {
		t.Error("default LogDetail should not be verbose")
	}
	if r := got.Recipe(); r.Level != 1 {
		t.Errorf("the default recipe needs Smithing %d, want 1", r.Level)
	}
}

func TestSmelterBotSettingsOverrides(t *testing.T) {
	got := smelterSettings(map[string]interface{}{
		"bar":       "steel",
		"bankBooth": "Bank chest",
		"logDetail": "Verbose",
	})
	if got.Bar != "Steel bar" {
		t.Errorf("Bar = %q, want the canonical Steel bar", got.Bar)
	}
	if got.BankBooth != "Bank chest" {
		t.Errorf("BankBooth = %q, want the override", got.BankBooth)
	}
	if !got.Verbose() {
		t.Error("Verbose logDetail should report Verbose() true")
	}
	r := got.Recipe()
	if !r.HasSecondary() || r.Secondary.Name != "Coal" || r.Secondary.Per != 2 {
		t.Errorf("steel should need 2 coal, got %+v", r.Secondary)
	}
}

// The short names a user is most likely to type must reach the right content row, since the
// TS source carried an explicit Adamant->Adamantite / Rune->Runite mapping for exactly this.
func TestSmelterBotSettingsAcceptsShortBarNames(t *testing.T) {
	if got := smelterSettings(map[string]interface{}{"bar": "Adamant"}); got.Bar != "Adamantite bar" {
		t.Errorf("Adamant resolved to %q, want Adamantite bar", got.Bar)
	}
	if got := smelterSettings(map[string]interface{}{"bar": "Rune"}); got.Bar != "Runite bar" {
		t.Errorf("Rune resolved to %q, want Runite bar", got.Bar)
	}
}

// An unknown bar must not leave the run with no recipe at all.
func TestSmelterBotSettingsUnknownBarFallsBack(t *testing.T) {
	got := smelterSettings(map[string]interface{}{"bar": "Dragon bar"})
	if got.Bar != smeltplan.DefaultSettings().Bar {
		t.Errorf("an unknown bar should fall back to the default, got %q", got.Bar)
	}
}

func TestSmelterBotRegistered(t *testing.T) {
	b, ok := New("SmelterBot")
	if !ok || b == nil {
		t.Fatal("SmelterBot should be registered in the bot registry")
	}
	if skill, ok := PrimarySkill("SmelterBot"); !ok || skill != "Smithing" {
		t.Errorf("PrimarySkill(SmelterBot) = %q/%v, want Smithing/true", skill, ok)
	}
}

// Every option the schema offers must be a bar the plan can actually resolve, so a panel
// dropdown cannot select something the run would silently replace with bronze.
func TestSmelterBotSettingsOptionsAreAllRealBars(t *testing.T) {
	opts := SmelterBotSettings["bar"].Options
	if len(opts) != len(smeltplan.Recipes) {
		t.Fatalf("the bar dropdown offers %d options, want %d", len(opts), len(smeltplan.Recipes))
	}
	for _, name := range opts {
		if _, ok := smeltplan.RecipeFor(name); !ok {
			t.Errorf("the dropdown offers %q, which has no recipe", name)
		}
	}
}
