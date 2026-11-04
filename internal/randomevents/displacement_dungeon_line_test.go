package randomevents

import (
	"strings"
	"testing"
)

// The Edgeville trapdoor and its ladder, the same fixture in both directions. Classification
// already read the same both ways; what this pins is the REASON the line gives, because the two
// directions had to share one sentence and the climb out therefore printed its own evidence as a
// falsehood. Measured live 2026-09-19: "an underground counterpart mapsquare (mapsquareZ 54 is
// >=100 ...)" — 54 is not >=100, and a watcher whose justification is plainly untrue is a watcher
// nobody can act on.
func TestTheDungeonLineQuotesTheRightHalfOfThePairInBothDirections(t *testing.T) {
	down, ok := ClassifyDisplacement(3096, 3468, 0, 3096, 9868, 0, pollInterval)
	if !ok || down.Kind != DisplacementUnderground {
		t.Fatalf("climbing down classified as %q (moved %v), want %q", down.Kind, ok, DisplacementUnderground)
	}
	if line := down.String(); !strings.Contains(line, "mapsquareZ 154 is >=100") {
		t.Errorf("the climb-down line does not quote the underground square: %s", line)
	}

	up, ok := ClassifyDisplacement(3096, 9868, 0, 3096, 3468, 0, pollInterval)
	if !ok || up.Kind != DisplacementUnderground {
		t.Fatalf("climbing out classified as %q (moved %v), want %q", up.Kind, ok, DisplacementUnderground)
	}
	line := up.String()
	if strings.Contains(line, ">=100") {
		t.Errorf("the climb-out line still claims its surface destination is >=100: %s", line)
	}
	if !strings.Contains(line, "154 and 54") || !strings.Contains(line, "100 apart") {
		t.Errorf("the climb-out line does not name the counterpart pair that justifies it: %s", line)
	}
}
