package bot

import (
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/combatplan"
)

// The schema's ceiling belongs to the LADDER, so it is asserted against the ladder rather than
// against a number typed twice. These five used to offer 99, which made any goal above 75 a run
// that trained for hours and then stopped with "no rung available".
func TestCombatTrainerGoalsAreCappedAtTheLadderCeiling(t *testing.T) {
	ceiling := combatplan.MaxTrainableGoal(combatplan.MeleeLadder(), true)
	if ceiling != 75 {
		t.Fatalf("the ladder's ceiling is %d, want 75", ceiling)
	}

	for _, key := range []string{"goalAttack", "goalStrength", "goalDefence", "goalRanged", "goalMagic"} {
		def, ok := CombatTrainerSettings[key]
		if !ok {
			t.Fatalf("%s is missing from the schema", key)
		}
		if def.Max == nil {
			t.Fatalf("%s has no Max, so a panel could ask for a goal the ladder cannot finish", key)
		}
		if int(*def.Max) != ceiling {
			t.Fatalf("%s Max = %v, want the ladder's own ceiling %d", key, *def.Max, ceiling)
		}
	}

	// Ranged and Magic keep Min 0, which is how a melee-only run is expressed; the melee three
	// keep Min 1. Collapsing that distinction would silently turn "skip this stat" into "train
	// it to 1".
	for _, key := range []string{"goalRanged", "goalMagic"} {
		if min := CombatTrainerSettings[key].Min; min == nil || *min != 0 {
			t.Fatalf("%s Min = %v, want 0 so the discipline can be switched off", key, min)
		}
	}
	for _, key := range []string{"goalAttack", "goalStrength", "goalDefence"} {
		if min := CombatTrainerSettings[key].Min; min == nil || *min != 1 {
			t.Fatalf("%s Min = %v, want 1", key, min)
		}
	}

	// Prayer is NOT capped with the others, and that is deliberate: it is not banded, so
	// PrayerRung answers at any level and a high Prayer goal is slow rather than terminal.
	if max := CombatTrainerSettings["goalPrayer"].Max; max == nil || *max != 99 {
		t.Fatalf("goalPrayer Max = %v, want 99: Prayer is not banded and must not inherit the ladder's ceiling", max)
	}

	// The loader is strict about unknown keys, so the schema's size is part of its contract.
	if n := len(CombatTrainerSettings); n != 15 {
		t.Fatalf("the schema has %d keys, want 15 — adding or renaming one changes what reads it", n)
	}
}

// The guard behind the schema. It refuses at OnStart, which is the difference between an
// account that reports a fixable mistake immediately and one that trains for hours first.
func TestCombatTrainerRefusesAGoalAboveTheLadderCeiling(t *testing.T) {
	trainer := func(g combatplan.Goals) *combatTrainerBot {
		return &combatTrainerBot{
			ladder: combatplan.MeleeLadder(),
			cfg:    combatTrainerSettings{members: true, goals: g},
		}
	}

	err := trainer(combatplan.Goals{Attack: 80, Strength: 75, Defence: 75, Ranged: 75, Magic: 75, Prayer: 43}).checkGoalsReachable()
	if err == nil {
		t.Fatal("goalAttack 80 has no rung above 75 and must be refused at OnStart")
	}
	if !strings.Contains(err.Error(), "goalAttack") {
		t.Fatalf("the message must name the setting to change: %v", err)
	}
	if !strings.Contains(err.Error(), "75") {
		t.Fatalf("the message must name the real ceiling: %v", err)
	}

	// Exactly 75 is the owner's own configuration and must still start, and a Prayer goal far
	// above it is fine because Prayer is not banded.
	if err := trainer(combatplan.Goals{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 75, Prayer: 99}).checkGoalsReachable(); err != nil {
		t.Fatalf("75 across the board with Prayer 99 must be accepted: %v", err)
	}

	// 0 means "skip this discipline" and has to stay legal.
	if err := trainer(combatplan.Goals{Attack: 75, Strength: 75, Defence: 75, Ranged: 0, Magic: 0, Prayer: 43}).checkGoalsReachable(); err != nil {
		t.Fatalf("a melee-only run must be accepted: %v", err)
	}

	// Magic is checked too, not just the melee three.
	if err := trainer(combatplan.Goals{Attack: 75, Strength: 75, Defence: 75, Ranged: 75, Magic: 90, Prayer: 43}).checkGoalsReachable(); err == nil || !strings.Contains(err.Error(), "goalMagic") {
		t.Fatalf("goalMagic 90 must be refused and named: %v", err)
	}
}
