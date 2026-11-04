package randomevents

import (
	"strings"
	"testing"
	"time"
)

// TestAnEventAtTheFarEndExplainsTheJump is the live reading this came from, 2026-09-19 on the
// first arm of the reachability fix: 3292,3167 -> 2891,4597, 1430 tiles, printed as UNEXPLAINED
// with Guardian's own very next line reading "random event: maze — handling".
func TestAnEventAtTheFarEndExplainsTheJump(t *testing.T) {
	d, moved := ClassifyDisplacement(3292, 3167, 0, 2891, 4597, 0, pollInterval)
	if !moved {
		t.Fatal("a 1430-tile jump did not register as a displacement at all")
	}
	if d.Kind != DisplacementUnexplained {
		t.Fatalf("without the event the jump classified as %q, want %q", d.Kind, DisplacementUnexplained)
	}
	got := WithEventPresent(d, "maze")
	if got.Kind != DisplacementEvent || got.Name != "maze" {
		t.Fatalf("WithEventPresent = %q/%q, want %q/\"maze\"", got.Kind, got.Name, DisplacementEvent)
	}
	if line := got.String(); !strings.Contains(line, "maze") || strings.Contains(line, "UNEXPLAINED") {
		t.Fatalf("the log line still reads as unexplained: %s", line)
	}
}

// TestAnEventDoesNotExplainAwayAFailTeleport is the guard that matters more than the relabel: a
// Genie fail-teleport lands the account in a catalogued pocket, and an event that happens to be
// detectable there must not be allowed to take the credit and hide it.
func TestAnEventDoesNotExplainAwayAFailTeleport(t *testing.T) {
	// The live Jolly Boar Inn excursion, from displacement_test.go's own fixture.
	d, moved := ClassifyDisplacement(3114, 9835, 0, 3278, 3500, 1, 72*time.Second)
	if !moved || d.Kind != DisplacementFailTeleport {
		t.Fatalf("the fixture is not a fail-teleport: moved %v, kind %q", moved, d.Kind)
	}
	got := WithEventPresent(d, "maze")
	if got.Kind != DisplacementFailTeleport || got.Name != d.Name {
		t.Fatalf("an event relabelled a catalogued fail-teleport as %q/%q", got.Kind, got.Name)
	}
}

// TestAnEventDoesNotRelabelAStaircase keeps the same guard over the two ordinary kinds, so a
// dungeon entrance taken while an event is present still reads as a dungeon entrance.
func TestAnEventDoesNotRelabelAStaircase(t *testing.T) {
	d, moved := ClassifyDisplacement(2800, 3400, 0, 2802, 3401, 1, pollInterval)
	if !moved || d.Kind != DisplacementLevelChange {
		t.Fatalf("the fixture is not a level change: moved %v, kind %q", moved, d.Kind)
	}
	if got := WithEventPresent(d, "maze"); got.Kind != DisplacementLevelChange {
		t.Fatalf("an event relabelled a staircase as %q", got.Kind)
	}
}
