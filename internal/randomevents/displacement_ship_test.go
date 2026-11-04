package randomevents

import (
	"strings"
	"testing"
	"time"
)

// The misreading this fixes, as a fixture rather than a description: the Karamja (Musa Point)
// to Port Sarim crossing was reported as a Genie/Mysterious Old Man fail-teleport three times
// off the live fleet.
//
// Content is the authority for the arrival tile, not a log: areas/area_karamja/scripts/
// customs_officer.rs2:84 calls ~set_sail with 1_47_50_24_17, which decodes to 3032,3217 level
// 1. Mapsquare 47,50 level 1 is ALSO the fail-teleport enum's "Port Sarim, above Brian's
// Battleaxe Bazaar", and KnownFailTeleportPocket matches by mapsquare — which is the entire
// mechanism of the false reading.
func TestKaramjaToPortSarimCrossingIsNotAFailTeleport(t *testing.T) {
	// The gap is one the watcher actually sees, not one picked to make this pass: Guardian polls
	// every pollInterval (600ms), and a telejump moves the account between two consecutive polls,
	// so two polls is a realistic worst case. walkAllowance(1.2s) is 2*10+32 = 52 tiles, and this
	// crossing is 76, which is what keeps it out of the ordinary-level-change branch above.
	d, moved := ClassifyDisplacement(2956, 3143, 0, 3032, 3217, 1, 1200*time.Millisecond)
	if !moved {
		t.Fatal("the crossing was not reported as a displacement at all")
	}
	if d.Kind != DisplacementDeck {
		t.Fatalf("kind = %q, want %q", d.Kind, DisplacementDeck)
	}
	if !strings.Contains(d.Name, "Karamja to Port Sarim") {
		t.Fatalf("name = %q, want the Karamja to Port Sarim crossing", d.Name)
	}
	if line := d.String(); strings.Contains(line, "fail-teleport") {
		t.Fatalf("the log line still calls an ordinary crossing a fail-teleport: %q", line)
	}
}

// The other half of the reported asymmetry, asserted so it cannot regress into the fix: this
// one was ALWAYS classified correctly, because its arrival mapsquare is not in the enum.
// customs_officer.rs2:82 sails to 1_41_51_59_4 = 2683,3268 level 1, and mapsquare 41,51 has no
// fail-teleport entry, so it reached the deck branch unaided.
func TestBrimhavenToArdougneCrossingStillReadsAsBoarding(t *testing.T) {
	// Same realistic two-poll gap as the Karamja case; this crossing is 89 tiles against the same
	// 52-tile allowance.
	d, moved := ClassifyDisplacement(2772, 3232, 0, 2683, 3268, 1, 1200*time.Millisecond)
	if !moved {
		t.Fatal("the crossing was not reported as a displacement at all")
	}
	if d.Kind != DisplacementDeck {
		t.Fatalf("kind = %q, want %q", d.Kind, DisplacementDeck)
	}
	if _, pocket := KnownFailTeleportPocket(2683, 3268, 1); pocket {
		t.Fatal("mapsquare 41,51 level 1 is in the fail-teleport enum after all — this test's premise is wrong")
	}
}

// The arithmetic behind the table, checked independently of it, the same way
// TestJollyBoarCoordDecodesFromTheEnum checks the other one.
func TestShipArrivalCoordsDecodeFromContent(t *testing.T) {
	for _, c := range []struct {
		mx, mz, lx, lz int
		wantX, wantZ   int
	}{
		{47, 50, 24, 17, 3032, 3217},
		{47, 50, 40, 31, 3048, 3231},
		{41, 51, 59, 4, 2683, 3268},
		{44, 52, 18, 3, 2834, 3331},
	} {
		x, z := c.mx*64+c.lx, c.mz*64+c.lz
		if x != c.wantX || z != c.wantZ {
			t.Fatalf("%d_%d_%d_%d decoded to %d,%d, want %d,%d", c.mx, c.mz, c.lx, c.lz, x, z, c.wantX, c.wantZ)
		}
		if _, ok := KnownShipArrival(x, z, 1); !ok {
			t.Fatalf("KnownShipArrival(%d,%d,1) did not recognise a content ~set_sail destination", x, z)
		}
	}
}

// The guard that keeps this fix from swallowing the thing it sits in front of: the enum drops
// an ignored-event passenger at Brian's Battleaxe Bazaar, a DIFFERENT tile in the same
// mapsquare as the Port Sarim ship arrival. Matching ships by exact tile keeps that reading.
func TestARealFailTeleportIntoThePortSarimSquareStillReadsAsOne(t *testing.T) {
	if _, ok := KnownShipArrival(3040, 3224, 1); ok {
		t.Fatal("an ordinary tile in mapsquare 47,50 was treated as a ship arrival")
	}
	d, moved := ClassifyDisplacement(3114, 9835, 0, 3040, 3224, 1, 72*time.Second)
	if !moved {
		t.Fatal("the jump was not reported as a displacement at all")
	}
	if d.Kind != DisplacementFailTeleport {
		t.Fatalf("kind = %q, want %q", d.Kind, DisplacementFailTeleport)
	}
	if !strings.Contains(d.Name, "Port Sarim") {
		t.Fatalf("name = %q, want the Port Sarim entry", d.Name)
	}
}
