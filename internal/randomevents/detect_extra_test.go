package randomevents

import (
	"testing"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
)

// These extend detect_test.go's coverage of Detect() itself (that file only checked
// the empty-state case and the two pure helper functions it depends on) with the
// numeric-id-range boundaries COLLAB.md's task calls out as fully unit-testable:
// hazard/whirlpool/evade ids and the mime/maze mapsquare detection, each driven through
// real State.Handle packets via state_helpers_test.go rather than hand-poking private
// fields gamestate deliberately keeps unexported.

func TestDetectHazardByLocType(t *testing.T) {
	cases := []struct {
		name    string
		locType int
		want    string // "" means Detect should return nil
	}{
		{"gas chest exact id", gasChestLocID, "poisonous gas"},
		{"smoking rock min boundary", smokingRockIDMin, "smoking rock"},
		{"smoking rock max boundary", smokingRockIDMax, "smoking rock"},
		{"just below smoking rock range", smokingRockIDMin - 1, ""},
		{"just above smoking rock range", smokingRockIDMax + 1, ""},
		{"unrelated loc id", 1, ""},
	}
	for _, tc := range cases {
		s := gamestate.New()
		setTile(s, 3200, 3200, 0) // the loc lands on the account's own tile
		addLoc(s, tc.locType, 10)
		got := Detect(s)
		if tc.want == "" {
			if got != nil {
				t.Errorf("%s: Detect() = %+v, want nil", tc.name, got)
			}
			continue
		}
		if got == nil || got.Kind != KindHazard || got.Name != tc.want {
			t.Errorf("%s: Detect() = %+v, want Kind=Hazard Name=%q", tc.name, got, tc.want)
		}
	}
}

// A smoking rock only hurts the account mining it, so one in view but not beside the account is
// somebody else's (live 4 Oct: wario fled one 56 times while fighting beside it).
func TestDetectSmokingRockOnlyBesideAccount(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addLoc(s, smokingRockIDMin, 10)
	locs := s.Locs()
	if len(locs) != 1 {
		t.Fatalf("locs = %+v, want one", locs)
	}
	x, z, _, _ := s.Tile()
	if locs[0].X != x || locs[0].Z != z {
		t.Fatalf("rock at %d,%d, account at %d,%d: the fixture no longer puts them together", locs[0].X, locs[0].Z, x, z)
	}
	if !besideSelf(s, x+1, z+1, 0) || besideSelf(s, x+2, z, 0) || besideSelf(s, x, z, 1) {
		t.Fatal("besideSelf: want adjacent tiles only, on the same level")
	}
	far := gamestate.New()
	addLoc(far, smokingRockIDMin, 10) // no known tile: never this account's event
	if got := Detect(far); got != nil {
		t.Fatalf("rock with no known account tile: Detect() = %+v, want nil", got)
	}
}

func TestDetectWhirlpoolWithinEngageDistance(t *testing.T) {
	for _, typ := range []int{403, 404, 405, 406} {
		s := gamestate.New()
		setTile(s, 3200, 3200, 0)
		addNPC(s, 7, typ, 3, 0) // Chebyshev distance 3, the whirlpool threshold
		got := Detect(s)
		if got == nil || got.Kind != KindHazard || got.Name != "whirlpool" || got.NpcIndex != 7 {
			t.Errorf("whirlpool npc type %d at dist 3: Detect() = %+v", typ, got)
		}
	}
}

func TestDetectWhirlpoolJustOutsideEngageDistance(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 7, 403, 4, 0) // dist 4 > threshold 3
	if got := Detect(s); got != nil {
		t.Fatalf("whirlpool at dist 4 (just past threshold): Detect() = %+v, want nil", got)
	}
}

func TestDetectEvadeWithinEngageDistance(t *testing.T) {
	// One id from each hostile range in buildHostileIDs, at the exact threshold.
	for _, typ := range []int{391, 411, 413, 419, 425, 431, 438} {
		s := gamestate.New()
		setTile(s, 3200, 3200, 0)
		s.SelfSlot = 1
		addNPCFacing(s, 3, typ, hostileEngageDist, 0, 32769)
		got := Detect(s)
		if got == nil || got.Kind != KindEvade || got.NpcIndex != 3 {
			t.Errorf("hostile npc type %d at dist %d: Detect() = %+v", typ, hostileEngageDist, got)
		}
	}
}

func TestDetectEvadeJustOutsideEngageDistance(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 3, 411, hostileEngageDist+1, 0)
	if got := Detect(s); got != nil {
		t.Fatalf("hostile just past threshold: Detect() = %+v, want nil", got)
	}
}

// TestDetectEvadeSkipsHostileTargetingAnotherPlayer guards the real fix this session:
// hostileEventNpcIDs "only ever spawn as a player's own personal random event, never as
// a shared world mob" (see that var's own doc comment) — at a busy shared fishing camp
// with many fleet accounts within sight of each other, this NPC being visible and within
// range says nothing about whether it's actually a threat to THIS account; it's very
// often someone else's own event monster. Before this fix, Detect() raised KindEvade
// purely on type+distance, sending every nearby account fleeing to the bank over a
// monster that was never going to attack them.
func TestDetectEvadeSkipsHostileTargetingAnotherPlayer(t *testing.T) {
	const self, other = 5, 9
	s := gamestate.New()
	s.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(uint16(self)); p.P1(0) }))
	setTile(s, 3200, 3200, 0)
	addNPCFacing(s, 3, 411, 5, 0, 32768+other) // Swarm, confirmed facing a DIFFERENT player
	if got := Detect(s); got != nil {
		t.Fatalf("hostile confirmed targeting another player: Detect() = %+v, want nil", got)
	}
}

// TestDetectEvadeStillFiresWhenTargetingSelfOrUnknown checks the fix above didn't
// overshoot: a hostile confirmed facing THIS account, or one that hasn't picked a
// target yet (FaceEntity -1, e.g. a couple of ticks after it first spawns, before it
// locks onto its own owner), must still raise KindEvade exactly as before — only a
// CONFIRMED different target is excluded, never an unknown one.
func TestDetectEvadeStillFiresWhenTargetingSelfOrUnknown(t *testing.T) {
	const self = 5
	for _, tc := range []struct {
		name       string
		faceEntity int
	}{
		{"targets self", 32768 + self},
		{"targets nobody yet", -1},
	} {
		s := gamestate.New()
		s.Handle(serverprot.UPDATE_PID, payload(func(p *packet.Packet) { p.P2(uint16(self)); p.P1(0) }))
		setTile(s, 3200, 3200, 0)
		addNPCFacing(s, 3, 411, 5, 0, tc.faceEntity)
		got := Detect(s)
		if got == nil || got.Kind != KindEvade || got.NpcIndex != 3 {
			t.Errorf("%s: Detect() = %+v, want KindEvade on npc 3", tc.name, got)
		}
	}
}

func TestDetectIgnoresNonHostileNpcsNearby(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 3, 999, 1, 0) // an ordinary NPC id, right next to the player
	if got := Detect(s); got != nil {
		t.Fatalf("ordinary nearby npc: Detect() = %+v, want nil", got)
	}
}

// HostileEventNpcNearby is the real-separation check added to close the lc2b_40/
// wfixtest2 mobile-hostile re-engagement pattern (see its own doc comment): unlike
// Detect(), which only ever reports the SAME npc it would dispatch as a KindEvade
// event, this is called from fleeFromCombatWalking/guardian.go's kick() purely to ask
// "is a chasing hostile type still nearby", against an arbitrary x,z (not necessarily
// the player's own current tile — flee legs check candidate/current positions that
// move independently of state.Tile() across a poll).

func TestHostileEventNpcNearbyWithinRadius(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	s.SelfSlot = 1
	addNPCFacing(s, 3, 411, 10, 0, 32769) // Swarm chasing us, Chebyshev distance 10
	if !HostileEventNpcNearby(s, 3200, 3200, 15) {
		t.Fatal("HostileEventNpcNearby at dist 10, radius 15: want true")
	}
}

// An unassigned, just-spawned monster only counts within freshOwnerRadius: content spawns
// every hostile event one tile from its owner, so one further away is someone else's.
func TestHostileEventNpcNearbyFreshUnassignedNeedsToBeClose(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 3, 411, freshOwnerRadius, 0)
	if !HostileEventNpcNearby(s, 3200, 3200, 15) {
		t.Fatal("fresh unassigned swarm beside us: want true")
	}
	addNPC(s, 3, 411, 10, 0)
	if HostileEventNpcNearby(s, 3200, 3200, 15) {
		t.Fatal("fresh unassigned acct 10 tiles away is another account's: want false")
	}
}

func TestHostileEventNpcNearbyOutsideRadius(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 3, 411, 15, 0) // dist 15
	if HostileEventNpcNearby(s, 3200, 3200, 10) {
		t.Fatal("HostileEventNpcNearby at dist 15, radius 10: want false")
	}
}

func TestHostileEventNpcNearbyIgnoresNonHostileTypes(t *testing.T) {
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 3, 999, 1, 0) // an ordinary NPC id, right next to the reference point
	if HostileEventNpcNearby(s, 3200, 3200, 15) {
		t.Fatal("HostileEventNpcNearby with only a non-hostile npc nearby: want false")
	}
}

func TestHostileEventNpcNearbyCheckedAgainstGivenPointNotJustPlayerTile(t *testing.T) {
	// The whole point of taking x,z as parameters instead of reading state.Tile()
	// itself: a caller mid-flee-leg needs to ask "is the hostile near MY CURRENT
	// tile", which may differ from where the player was when the NPC was last seen
	// relative to setTile's own local-offset bookkeeping. Confirms the distance math
	// is genuinely relative to the given x,z, not hardcoded to the player's own tile.
	s := gamestate.New()
	setTile(s, 3200, 3200, 0)
	addNPC(s, 3, 411, 0, 0) // npc sits exactly on the player's own tile, (3200,3200)
	if !HostileEventNpcNearby(s, 3200, 3200, 5) {
		t.Fatal("HostileEventNpcNearby at the npc's own tile: want true")
	}
	if HostileEventNpcNearby(s, 3300, 3300, 5) {
		t.Fatal("HostileEventNpcNearby far from the npc's actual tile: want false")
	}
}

func TestDetectTrappedByMapsquare(t *testing.T) {
	cases := []struct {
		name       string
		x, z       int
		level      int
		wantName   string
		wantNilWhy string
	}{
		{"mime square", mimeSquareMX * 64, mimeSquareMZ * 64, 0, "mime", ""},
		{"maze square", mazeSquareMX * 64, mazeSquareMZ * 64, 0, "maze", ""},
		{"just outside mime square (x)", (mimeSquareMX - 1) * 64, mimeSquareMZ * 64, 0, "", "different mapsquare"},
		{"mime square but on level 1", mimeSquareMX * 64, mimeSquareMZ * 64, 1, "", "not level 0"},
	}
	for _, tc := range cases {
		s := gamestate.New()
		setTile(s, tc.x, tc.z, tc.level)
		got := Detect(s)
		if tc.wantName == "" {
			if got != nil {
				t.Errorf("%s (%s): Detect() = %+v, want nil", tc.name, tc.wantNilWhy, got)
			}
			continue
		}
		if got == nil || got.Kind != KindTrapped || got.Name != tc.wantName {
			t.Errorf("%s: Detect() = %+v, want Kind=Trapped Name=%q", tc.name, got, tc.wantName)
		}
	}
}

func TestDetectTrappedTakesPriorityOverHazardsAndNpcs(t *testing.T) {
	s := gamestate.New()
	setTile(s, mazeSquareMX*64, mazeSquareMZ*64, 0)
	addLoc(s, gasChestLocID, 10)
	addNPC(s, 1, 411, 1, 0)
	got := Detect(s)
	if got == nil || got.Kind != KindTrapped || got.Name != "maze" {
		t.Fatalf("trapped should take priority: Detect() = %+v", got)
	}
}
