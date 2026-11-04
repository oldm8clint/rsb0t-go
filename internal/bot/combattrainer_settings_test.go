package bot

import (
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
)

func resolveTrainer(raw map[string]interface{}) combatTrainerSettings {
	return readCombatTrainerSettings(CombatTrainerSettings.Resolve(raw))
}

func TestCombatTrainerSettingsDefaults(t *testing.T) {
	s := resolveTrainer(map[string]interface{}{})
	// Base 75 in all five combat stats plus 43 Prayer is the account owner's stated target, so
	// that is what the registered script asks for when nobody overrides it. A melee-only run is
	// expressed by setting the Ranged and Magic goals to 0.
	want := combatplan.Goals{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 75, Prayer: 43}
	if s.goals != want {
		t.Fatalf("goals = %+v, want %+v", s.goals, want)
	}
	if s.magicElement != "Fire" {
		t.Fatalf("magicElement = %q, want Fire", s.magicElement)
	}
	if !s.members {
		t.Fatal("the default is the members ladder: the free-to-play one is 22 giants in total")
	}
	if !s.buyGear {
		t.Fatal("buyGear should default on")
	}
	// On by default, because gear buying with no budget is what stopped a 1-to-45 window
	// finishing: the same band measured 68 XP/min with shopping on against 506 with it off, and
	// 19 shop trips bought 4 items. Off is the control arm and the escape hatch, never the
	// default.
	if !s.gearBudget {
		t.Fatal("gearTripBudget should default on")
	}
	if s.eatFrac != 0.5 {
		t.Fatalf("eatFrac = %v, want 0.5", s.eatFrac)
	}
	if s.forcedRung != "auto" {
		t.Fatalf("rung = %q, want auto", s.forcedRung)
	}
	if s.verbose {
		t.Fatal("logDetail should default to Normal")
	}
}

// TestCombatTrainerSettingsFromStrings is the panel case: every value arrives as a string.
func TestCombatTrainerSettingsFromStrings(t *testing.T) {
	s := resolveTrainer(map[string]interface{}{
		"goalAttack":   "60",
		"goalStrength": "61",
		"goalDefence":  "62",
		"goalPrayer":   "43",
		"membersWorld": "false",
		"buyGear":      "false",
		"eatAtHp":      "70",
		"rung":         "Giants",
		"logDetail":    "Verbose",
		"food":         "Lobster",
	})
	if s.goals.Attack != 60 || s.goals.Strength != 61 || s.goals.Defence != 62 {
		t.Fatalf("goals = %+v", s.goals)
	}
	if s.members {
		t.Fatal("membersWorld \"false\" should resolve false")
	}
	if s.buyGear {
		t.Fatal("buyGear \"false\" should resolve false")
	}
	// The panel sends booleans as strings, and a budget that silently stayed on when an operator
	// turned it off would make the control arm measure the wrong thing.
	if !resolveTrainer(map[string]interface{}{}).gearBudget {
		t.Fatal("gearTripBudget should resolve true when unset")
	}
	if resolveTrainer(map[string]interface{}{"gearTripBudget": "false"}).gearBudget {
		t.Fatal("gearTripBudget \"false\" should resolve false")
	}
	if s.eatFrac != 0.7 {
		t.Fatalf("eatFrac = %v, want 0.7", s.eatFrac)
	}
	if s.forcedRung != "giants" {
		t.Fatalf("rung = %q, want it lower-cased for the ladder lookup", s.forcedRung)
	}
	if !s.verbose {
		t.Fatal("logDetail Verbose should resolve verbose")
	}
	if s.food != "Lobster" {
		t.Fatalf("food = %q", s.food)
	}
}

func TestCombatTrainerSettingsClamp(t *testing.T) {
	// Out-of-range numbers clamp rather than producing a goal no account can reach or an eat
	// threshold that never fires.
	// 75, not 99, and the difference is a terminal bug rather than a preference: the ladder's
	// top rung ends at 75 exclusive, so a goal above 75 asks PickRung for a band no rung covers
	// and stops the run with an error AFTER it has trained for hours.
	s := resolveTrainer(map[string]interface{}{"goalAttack": 200, "eatAtHp": 1})
	if s.goals.Attack != 75 {
		t.Fatalf("goalAttack = %d, want it clamped to the ladder's 75 ceiling", s.goals.Attack)
	}
	if s.eatFrac != 0.1 {
		t.Fatalf("eatFrac = %v, want it clamped to 0.1", s.eatFrac)
	}
	// An unknown food is dropped back to the default rather than accepted, since the schema
	// matches against its own options.
	if got := resolveTrainer(map[string]interface{}{"food": "Gravel"}).food; got != "" {
		t.Fatalf("food = %q, want the default for a food no option matches", got)
	}
}

// TestCombatTrainerIsRegistered guards the registration itself, which is the whole point of
// this adapter file.
func TestCombatTrainerIsRegistered(t *testing.T) {
	b, ok := New("CombatTrainer")
	if !ok {
		t.Fatal("CombatTrainer is not registered")
	}
	if _, isTrainer := b.(*combatTrainerBot); !isTrainer {
		t.Fatalf("CombatTrainer built a %T", b)
	}
	if skill, ok := PrimarySkill("CombatTrainer"); !ok || skill != "Hitpoints" {
		t.Fatalf("primary skill = %q %v, want Hitpoints", skill, ok)
	}
}
