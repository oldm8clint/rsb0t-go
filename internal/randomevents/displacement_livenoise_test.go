package randomevents

import (
	"testing"
	"time"
)

// The four live false positives the root session reported for the fleet on 2026-09-20: of seven
// "UNEXPLAINED" displacement lines in the first fifteen minutes, only one was a real
// fail-teleport. Each reading below is quoted from that report, so the test fails if the
// classifier ever goes back to calling them unexplained.
func TestTheLiveFalsePositivesAreNoLongerUnexplained(t *testing.T) {
	cases := []struct {
		what                    string
		fromX, fromZ, fromLevel int
		toX, toZ, toLevel       int
		want                    DisplacementKind
	}{
		{"Port Sarim ship", 3026, 3218, 0, 2956, 3143, 1, DisplacementDeck},
		{"Brimhaven to Ardougne ship", 2772, 3232, 0, 2683, 3268, 1, DisplacementDeck},
		{"leaving Tutorial Island", 3095, 3106, 0, 3222, 3222, 0, DisplacementSpawnPoint},
		{"a death respawn", 3120, 3254, 0, 3221, 3220, 0, DisplacementSpawnPoint},
	}
	for _, c := range cases {
		d, moved := ClassifyDisplacement(c.fromX, c.fromZ, c.fromLevel, c.toX, c.toZ, c.toLevel, pollInterval)
		if !moved {
			t.Errorf("%s: did not register as a displacement at all", c.what)
			continue
		}
		if d.Kind != c.want {
			t.Errorf("%s: classified %q, want %q — line was: %s", c.what, d.Kind, c.want, d.String())
		}
		if d.Kind == DisplacementUnexplained {
			t.Errorf("%s: still reads as UNEXPLAINED", c.what)
		}
	}
}

// The controls that matter more than the relabels: silencing noise must not silence the thing the
// watcher exists for. A catalogued fail-teleport keeps its label even though it also ends one
// level up, because the pocket lookup runs before the deck rule and the distance is far outside
// the deck cap.
func TestSilencingTheNoiseDoesNotSilenceAFailTeleport(t *testing.T) {
	d, moved := ClassifyDisplacement(3114, 9835, 0, 3278, 3500, 1, 72*time.Second)
	if !moved || d.Kind != DisplacementFailTeleport {
		t.Fatalf("the live Jolly Boar fail-teleport classified as %q (moved %v), want %q", d.Kind, moved, DisplacementFailTeleport)
	}
	if d.Name == "" {
		t.Error("the fail-teleport lost the destination name from the enum")
	}
}

// A dungeon entrance and an ordinary staircase keep their own labels too.
func TestSilencingTheNoiseLeavesTheDungeonAndStaircaseLabelsAlone(t *testing.T) {
	down, moved := ClassifyDisplacement(3096, 3468, 0, 3096, 9868, 0, pollInterval)
	if !moved || down.Kind != DisplacementUnderground {
		t.Errorf("the Edgeville trapdoor classified as %q, want %q", down.Kind, DisplacementUnderground)
	}
	stair, moved := ClassifyDisplacement(2800, 3400, 0, 2802, 3401, 1, pollInterval)
	if !moved || stair.Kind != DisplacementLevelChange {
		t.Errorf("an ordinary staircase classified as %q, want %q", stair.Kind, DisplacementLevelChange)
	}
}

// And a long jump to a level-1 tile that is NOT catalogued still reads as unexplained, because
// the deck rule is capped rather than open-ended.
func TestALongJumpToAnUpperFloorIsStillUnexplained(t *testing.T) {
	d, moved := ClassifyDisplacement(2500, 3200, 0, 2900, 3900, 1, pollInterval)
	if !moved {
		t.Fatal("a 700-tile jump did not register as a displacement")
	}
	if d.Kind == DisplacementDeck {
		t.Errorf("a 700-tile jump to level 1 was excused as boarding: %s", d.String())
	}
}
