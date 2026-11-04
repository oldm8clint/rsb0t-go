package randomevents

import (
	"strings"
	"testing"
	"time"
)

// The live incident this whole file exists for, asserted as a fixture rather than described in
// a comment: the bankspin -mode marathon arm had the account at 3114,9835 in the Edgeville
// dungeon and, about seventy seconds later, at 3278,3500 level 1, with no guardian line
// anywhere. 3278,3500 level 1 decodes to macro_event_fail_teleports_free val=0 (1_51_54_14_44),
// the Jolly Boar Inn.
func TestClassifyDisplacementNamesTheLiveJollyBoarExcursion(t *testing.T) {
	d, moved := ClassifyDisplacement(3114, 9835, 0, 3278, 3500, 1, 72*time.Second)
	if !moved {
		t.Fatal("the live excursion was not reported as a displacement at all")
	}
	if d.Kind != DisplacementFailTeleport {
		t.Fatalf("kind = %q, want %q", d.Kind, DisplacementFailTeleport)
	}
	if !strings.Contains(d.Name, "Jolly Boar") {
		t.Fatalf("name = %q, want the Jolly Boar Inn", d.Name)
	}
	if line := d.String(); !strings.Contains(line, "fail-teleport destination") || !strings.Contains(line, "3278,3500,1") {
		t.Fatalf("the log line does not explain itself: %q", line)
	}
}

// The enum's own coordinate arithmetic, checked independently of the table: world tile is
// mapsquareX*64+localX, mapsquareZ*64+localZ.
func TestJollyBoarCoordDecodesFromTheEnum(t *testing.T) {
	x, z := 51*64+14, 54*64+44
	if x != 3278 || z != 3500 {
		t.Fatalf("1_51_54_14_44 decoded to %d,%d, want 3278,3500", x, z)
	}
	if name, ok := KnownFailTeleportPocket(x, z, 1); !ok || !strings.Contains(name, "Jolly Boar") {
		t.Fatalf("KnownFailTeleportPocket(%d,%d,1) = %q,%v", x, z, name, ok)
	}
}

func TestClassifyDisplacementIgnoresOrdinaryWalking(t *testing.T) {
	// A poll's worth of running, on one level: not a displacement at any tick rate.
	if _, moved := ClassifyDisplacement(3200, 3200, 0, 3210, 3208, 0, pollInterval); moved {
		t.Fatal("ordinary running was reported as a displacement")
	}
	// Standing still.
	if _, moved := ClassifyDisplacement(3200, 3200, 0, 3200, 3200, 0, pollInterval); moved {
		t.Fatal("standing still was reported as a displacement")
	}
	// A long gap has to buy a proportionally longer walk, or a descheduled poll loop would
	// manufacture displacements out of a legitimate cross-town run.
	if _, moved := ClassifyDisplacement(3200, 3200, 0, 3320, 3200, 0, 30*time.Second); moved {
		t.Fatal("a long walk across a long gap was reported as a displacement")
	}
}

func TestClassifyDisplacementSeparatesDungeonsFromTheUnexplained(t *testing.T) {
	// The Edgeville trapdoor telejumps 6400 tiles south in z, staying on level 0. That is an
	// ordinary entrance and must not read as unexplained.
	d, moved := ClassifyDisplacement(3097, 3468, 0, 3097, 9868, 0, pollInterval)
	if !moved || d.Kind != DisplacementUnderground {
		t.Fatalf("the Edgeville trapdoor read as moved=%v kind=%q, want an underground entrance", moved, d.Kind)
	}
	// A staircase: the level moves, the tile barely does.
	d, moved = ClassifyDisplacement(2800, 3400, 0, 2802, 3401, 1, pollInterval)
	if !moved || d.Kind != DisplacementLevelChange {
		t.Fatalf("a staircase read as moved=%v kind=%q, want a level change", moved, d.Kind)
	}
	// Far, same level, not catalogued and not underground: the case worth a loud line.
	d, moved = ClassifyDisplacement(3200, 3200, 0, 2500, 3200, 0, pollInterval)
	if !moved || d.Kind != DisplacementUnexplained {
		t.Fatalf("an uncatalogued long jump read as moved=%v kind=%q, want unexplained", moved, d.Kind)
	}
}

// The precedence this file got wrong first time round, pinned so it cannot regress.
// KnownFailTeleportPocket matches by MAPSQUARE, and about thirty of the enum's destinations are
// ordinary places a bot walks through. 3200,3200 level 1 is mapsquare 50,50 — the catalogued
// "Lumbridge Castle (northern gate tower / spinning wheel room)" — so an innocent staircase
// there was being reported as a fail-teleport. A real one never lands the account beside itself.
func TestOrdinaryStaircaseInsideACataloguedMapsquareIsNotAFailTeleport(t *testing.T) {
	if name, ok := KnownFailTeleportPocket(3200, 3200, 1); !ok || name == "" {
		t.Fatal("this fixture is pointless unless 3200,3200,1 really is a catalogued mapsquare")
	}
	d, moved := ClassifyDisplacement(3200, 3200, 0, 3202, 3201, 1, pollInterval)
	if !moved || d.Kind != DisplacementLevelChange {
		t.Fatalf("a staircase inside a catalogued mapsquare read as kind=%q, want a level change", d.Kind)
	}
	// ...but a genuine long-range landing in that same mapsquare still reads as what it is.
	d, moved = ClassifyDisplacement(2500, 3200, 0, 3200, 3200, 1, pollInterval)
	if !moved || d.Kind != DisplacementFailTeleport {
		t.Fatalf("a real long-range landing read as kind=%q, want a fail-teleport", d.Kind)
	}
}

// The Edgeville trapdoor, BOTH ways. Climbing out was reported as a fail-teleport ten times in
// one live window, because 3096,3468 is mapsquare 48,54 and the enum catalogues "Edgeville".
// The trapdoor's own p_telejump keeps the player on level 0, so the ordinary level-change guard
// never sees it; only the surface/underground counterpart pair does.
func TestDungeonEntranceReadsTheSameInBothDirections(t *testing.T) {
	down, ok := ClassifyDisplacement(3096, 3468, 0, 3096, 9868, 0, pollInterval)
	if !ok || down.Kind != DisplacementUnderground {
		t.Fatalf("climbing down read as kind=%q, want an underground entrance", down.Kind)
	}
	up, ok := ClassifyDisplacement(3096, 9868, 0, 3096, 3468, 0, pollInterval)
	if !ok || up.Kind != DisplacementUnderground {
		t.Fatalf("climbing back out read as kind=%q, want an underground entrance", up.Kind)
	}
	// The guard must not swallow a real fail-teleport that happens to land on the surface: this
	// pair is not a counterpart, because the mapsquares differ in x as well.
	real, ok := ClassifyDisplacement(3113, 9833, 0, 3278, 3500, 1, pollInterval)
	if !ok || real.Kind != DisplacementFailTeleport {
		t.Fatalf("a genuine fail-teleport out of a dungeon read as kind=%q, want a fail-teleport", real.Kind)
	}
}

// The other unexplained reading from the same session: the account stood at 2434,3346 while the
// bones it had just approached lay at 3113,9833. That is mapsquare 38,52 — the members table's
// own "The Outpost, Ardougne".
func TestClassifyDisplacementNamesTheArdougneOutpostReading(t *testing.T) {
	d, moved := ClassifyDisplacement(3113, 9833, 0, 2434, 3346, 0, 2*time.Second)
	if !moved || d.Kind != DisplacementFailTeleport {
		t.Fatalf("moved=%v kind=%q, want a fail-teleport", moved, d.Kind)
	}
	if !strings.Contains(d.Name, "Outpost") {
		t.Fatalf("name = %q, want the Ardougne Outpost", d.Name)
	}
}
