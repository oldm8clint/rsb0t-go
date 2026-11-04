package randomevents

import "testing"

// TestKnownFailTeleportPocketMatchesLiveIncident is the exact real-world reproduction of the
// production stranding this file was built from: account "acct24", frozen at
// x=3212,z=9615,level=0 (mapsquare 50,150). See failteleport.go's own doc comment for the
// full ground-truthing (content/scripts/macro events/configs/macro_events_teleports.enum
// entry #22, content/maps/m50_150.jm2).
func TestKnownFailTeleportPocketMatchesLiveIncident(t *testing.T) {
	name, ok := KnownFailTeleportPocket(3212, 9615, 0)
	if !ok {
		t.Fatalf("KnownFailTeleportPocket(3212,9615,0) = false, want true (the live acct24 incident tile)")
	}
	if name == "" {
		t.Fatalf("KnownFailTeleportPocket(3212,9615,0) returned an empty name")
	}
	t.Logf("matched: %s", name)
}

// TestKnownFailTeleportPocketMatchesWizardsTowerBasement checks the OTHER confirmed
// interior-pocket destination in the same enum (mapsquare 48,149) — the second data point
// that established the mapsquareZ+100 "underground counterpart" convention this file's own
// LooksLikeUndergroundMapsquare generalizes from.
func TestKnownFailTeleportPocketMatchesWizardsTowerBasement(t *testing.T) {
	// mapsquare 48,149 -> world tile anywhere in [48*64, 49*64) x [149*64, 150*64).
	x, z := 48*64+25, 149*64+31
	name, ok := KnownFailTeleportPocket(x, z, 0)
	if !ok {
		t.Fatalf("KnownFailTeleportPocket(%d,%d,0) = false, want true (Wizards' Tower basement)", x, z)
	}
	t.Logf("matched: %s", name)
}

// TestKnownFailTeleportPocketDistinguishesFaladorShieldShopFromGeneralStoreTower guards the
// gap this session's own audit against the source enum found: the members table's val=14
// entry (Falador Shield Shop, 1_46_52_31_54) has a DIFFERENT level (1) than the free
// table's val=2 "Falador general store tower" at the same mapsquare (2, 46, 52) — before
// this fix, the Shield Shop entry was simply never carried over when this table was first
// built, so this tile matched nothing at all.
func TestKnownFailTeleportPocketDistinguishesFaladorShieldShopFromGeneralStoreTower(t *testing.T) {
	x, z := 46*64+31, 52*64+54
	name, ok := KnownFailTeleportPocket(x, z, 1)
	if !ok {
		t.Fatalf("KnownFailTeleportPocket(%d,%d,1) = false, want true (Falador Shield Shop)", x, z)
	}
	if name != "Falador Shield Shop" {
		t.Errorf("KnownFailTeleportPocket(%d,%d,1) = %q, want \"Falador Shield Shop\"", x, z, name)
	}
	// The level-2 destination at the identical mapsquare must still resolve to the OTHER
	// name — confirming the dedup key really is (level, mx, mz), not just (mx, mz).
	name, ok = KnownFailTeleportPocket(x, z, 2)
	if !ok || name != "Falador general store tower" {
		t.Errorf("KnownFailTeleportPocket(%d,%d,2) = %q,%v, want \"Falador general store tower\",true", x, z, name, ok)
	}
}

// TestKnownFailTeleportPocketIgnoresOrdinaryLocations confirms this doesn't misfire on the
// account's own normal working tiles — including LocalRescueTile itself (3222,3218,0,
// Lumbridge courtyard, the freshstartrunner rescue/home reference) and a handful of other
// unrelated overworld tiles nowhere near any fail-teleport destination.
func TestKnownFailTeleportPocketIgnoresOrdinaryLocations(t *testing.T) {
	cases := []struct{ x, z, level int }{
		{3222, 3218, 0}, // Lumbridge courtyard (freshstartrunner.LocalRescueTile)
		{2809, 3441, 0}, // Catherby bank (CatherbyBankRef)
		{3200, 3200, 0}, // generic Lumbridge-area tile
		{0, 0, 0},
	}
	for _, tc := range cases {
		if name, ok := KnownFailTeleportPocket(tc.x, tc.z, tc.level); ok {
			t.Fatalf("KnownFailTeleportPocket(%d,%d,%d) = true (%q), want false", tc.x, tc.z, tc.level, name)
		}
	}
}

// TestLooksLikeUndergroundMapsquare exercises the general mz>=100 fallback against a few
// synthetic z+6400-style coordinates beyond the two fixed-list entries above, per this
// task's own request to check the threshold generalizes rather than only matching the one
// live incident tile.
func TestLooksLikeUndergroundMapsquare(t *testing.T) {
	cases := []struct {
		name string
		mz   int
		want bool
	}{
		{"ordinary Lumbridge-area mapsquare", 50, false},
		{"ordinary Varrock-area mapsquare", 53, false},
		{"just below the threshold", 99, false},
		{"exactly the threshold", 100, true},
		{"lubmy basement's own mapsquare", 150, true},
		{"Wizards' Tower basement's own mapsquare", 149, true},
		{"a hypothetical further-out underground mapsquare", 200, true},
	}
	for _, tc := range cases {
		if got := LooksLikeUndergroundMapsquare(tc.mz); got != tc.want {
			t.Errorf("%s: LooksLikeUndergroundMapsquare(%d) = %v, want %v", tc.name, tc.mz, got, tc.want)
		}
	}
}

// TestDescribeStrandedLocation checks the combined helper freshstartrunner's STRANDED branch
// actually calls: the fixed-list name takes priority, the generic underground note covers an
// uncatalogued underground mapsquare, and an ordinary tile yields "".
func TestDescribeStrandedLocation(t *testing.T) {
	if got := DescribeStrandedLocation(3212, 9615, 0); got == "" {
		t.Fatalf("DescribeStrandedLocation(3212,9615,0) = \"\", want the live incident's known name")
	}
	// mapsquare 60,200 (level 0): not in the fixed list, but mz=200 >= 100 -> generic note.
	x, z := 60*64+10, 200*64+10
	got := DescribeStrandedLocation(x, z, 0)
	if got == "" {
		t.Fatalf("DescribeStrandedLocation(%d,%d,0) = \"\", want the generic underground-mapsquare note", x, z)
	}
	if _, ok := KnownFailTeleportPocket(x, z, 0); ok {
		t.Fatalf("test setup bug: (%d,%d,0) unexpectedly matched the fixed list too", x, z)
	}
	if got := DescribeStrandedLocation(3222, 3218, 0); got != "" {
		t.Fatalf("DescribeStrandedLocation(3222,3218,0) = %q, want \"\" (an ordinary tile)", got)
	}
}
