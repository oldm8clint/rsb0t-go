package randomevents

import "testing"

// The 3 Oct 00:13 summon put 272 accounts at 3040,10336. Measured on the real pack (collision.lcnav)
// the arrival's walkable component holds no ladder, lever or gate stand; Staircase 1734 at
// 3044,10324 is reached from 3043-3046,10327. The site must be catalogued with that exit, refuse a
// teleport (Wilderness level 52) and stay clear of the KBD lever's pocket next door.
func TestWildDungeonSectionIsCatalogued(t *testing.T) {
	s, ok := StrandSiteAt(3040, 10336, 0)
	if !ok || s.Name != "the gated Wilderness dungeon section below 3040,3936" {
		t.Fatalf("3040,10336 = %+v %v, want the Wilderness dungeon section", s.Name, ok)
	}
	if !s.NoTeleport {
		t.Fatal("a teleport spell cannot leave Wilderness level 52")
	}
	step := s.StepFor(3040, 10336, 0)
	if step == nil || step.LocID != 1734 || step.LocX != 3044 || step.LocZ != 10324 || step.OpLabel != "Climb-up" {
		t.Fatalf("exit step = %+v, want Staircase 1734 Climb-up at 3044,10324", step)
	}
	if k, ok := StrandSiteAt(3067, 10254, 0); !ok || k.Name != "the KBD lever's pocket in the Wilderness dungeon" {
		t.Fatalf("the KBD lever pocket must stay its own site, got %q %v", k.Name, ok)
	}
}
