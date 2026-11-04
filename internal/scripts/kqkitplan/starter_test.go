package kqkitplan

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
)

func fireBolt(t *testing.T) fleetplan.Spell {
	t.Helper()
	sp, ok := fleetplan.SpellByName("Fire Bolt")
	if !ok {
		t.Fatal("no Fire Bolt")
	}
	return sp
}

func TestStarterLinesAskForTheWholeKitOfAFreshAccount(t *testing.T) {
	lines := StarterLines(StarterInput{Spell: fireBolt(t), MaxHP: 40})
	if len(lines) != 3 {
		t.Fatalf("a fresh account's kit has %d lines, want staff, chaos and food: %+v", len(lines), lines)
	}
	staff, chaos, food := lines[0], lines[1], lines[2]
	if staff.Count != 1 || !staff.TryDown || len(staff.Choices) < 2 {
		t.Errorf("staff line = %+v, want one staff down a ladder", staff)
	}
	if chaos.Choices[0] != "Chaos rune" || chaos.Count != StarterChaos {
		t.Errorf("chaos line = %+v, want %d Chaos rune", chaos, StarterChaos)
	}
	if !food.Bulk || food.Count != StarterFood {
		t.Errorf("food line = %+v, want a bulk %d food", food, StarterFood)
	}
}

func TestStarterLinesAreOnlyTheShortfall(t *testing.T) {
	lines := StarterLines(StarterInput{Spell: fireBolt(t), MaxHP: 40, Staff: "Staff of fire", ChaosHeld: 400, FoodHeld: 480})
	if len(lines) != 2 || lines[0].Count != 600 || lines[1].Count != 20 {
		t.Fatalf("lines = %+v, want 600 chaos and 20 food, no staff", lines)
	}
	if got := StarterLines(StarterInput{Spell: fireBolt(t), MaxHP: 40, Staff: "Magic staff", ChaosHeld: 1000, FoodHeld: 500}); len(got) != 0 {
		t.Fatalf("an account holding the whole kit got lines: %+v", got)
	}
}

func TestStarterLinesNeverRepeatOnceDelivered(t *testing.T) {
	if got := StarterLines(StarterInput{Delivered: true, Spell: fireBolt(t)}); got != nil {
		t.Fatalf("a delivered account was asked again: %+v", got)
	}
	if due, _ := StarterDue(StarterInput{Delivered: true, Spell: fireBolt(t)}, true, true, 0, 10); due {
		t.Fatal("StarterDue true for a delivered account: the kit would repeat")
	}
}

func TestStarterDueGates(t *testing.T) {
	in := StarterInput{Spell: fireBolt(t), MaxHP: 40}
	if due, why := StarterDue(in, true, true, 0, 10); !due {
		t.Fatalf("a fresh account in a training stage was not due: %s", why)
	}
	if due, _ := StarterDue(in, false, true, 0, 10); due {
		t.Error("due outside a training stage")
	}
	if due, _ := StarterDue(in, true, false, 0, 10); due {
		t.Error("due somewhere unsafe")
	}
	if due, _ := StarterDue(in, true, true, 10, 10); due {
		t.Error("due after the retry cap")
	}
	held := StarterInput{Spell: fireBolt(t), Staff: "Staff of air", ChaosHeld: 5000, FoodHeld: 900}
	if due, _ := StarterDue(held, true, true, 0, 10); due {
		t.Error("due for an account that already holds the kit")
	}
}

func TestStarterMarkedOnAnythingArriving(t *testing.T) {
	if StarterMarked(nil) || StarterMarked(map[string]int{"Chaos rune": 0}) {
		t.Error("marked with nothing delivered")
	}
	if !StarterMarked(map[string]int{"Staff of air": 1}) {
		t.Error("not marked after a staff arrived")
	}
}

func TestKitsPerHourUsesTheTighterOfServingTimeAndTheWindow(t *testing.T) {
	// the mule: 45 s a bundled kit, 30,000 per item per 30-minute window.
	got := KitsPerHour(45*time.Second, 30000, 30*time.Minute)
	if got != 60 {
		t.Fatalf("KitsPerHour = %d, want 60 (the window binds: 30 kits per 30 minutes)", got)
	}
	if got := KitsPerHour(45*time.Second, 1_000_000, 30*time.Minute); got != 80 {
		t.Fatalf("with an unbounded window KitsPerHour = %d, want 80 (the 45 s trade binds)", got)
	}
	if a := 329; a/KitsPerHour(45*time.Second, 30000, 30*time.Minute) > 6 {
		t.Errorf("329 accounts would take more than six hours")
	}
}
