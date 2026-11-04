package randomevents

import (
	"testing"
	"time"
)

func TestVoidHopHeadsStraightThenTurnsWhenStuck(t *testing.T) {
	// Direct: at most 10 tiles on each axis toward the target.
	if x, z := VoidHop(3080, 10280, 3100, 10040, 0); x != 3090 || z != 10270 {
		t.Fatalf("direct hop = %d,%d, want 3090,10270", x, z)
	}
	// Close to the target the hop lands on it.
	if x, z := VoidHop(3098, 10045, 3100, 10040, 0); x != 3100 || z != 10040 {
		t.Fatalf("final hop = %d,%d, want the target", x, z)
	}
	// At the target there is nothing to do.
	if x, z := VoidHop(3100, 10040, 3100, 10040, 3); x != 3100 || z != 10040 {
		t.Fatalf("at target = %d,%d", x, z)
	}
	// Stuck: each further failure turns away from the direct line, and every heading differs.
	seen := map[[2]int]bool{}
	for stuck := 0; stuck < 6; stuck++ {
		x, z := VoidHop(3080, 10280, 3080, 10040, stuck)
		if x == 3080 && z == 10280 {
			t.Fatalf("stuck %d: no hop", stuck)
		}
		seen[[2]int{x, z}] = true
	}
	if len(seen) != 6 {
		t.Fatalf("want 6 distinct headings over 6 stuck counts, got %d: %v", len(seen), seen)
	}
}

func TestVoidSitesAreCatalogued(t *testing.T) {
	s, ok := StrandSiteAt(3090, 10280, 0)
	if !ok || s.VoidWalkTo == nil || len(s.Steps) != 0 {
		t.Fatalf("48,160 = %+v %v, want the void site with a void walk and no steps", s.Name, ok)
	}
	if w, ok := StrandSiteAt(3040, 10336, 0); !ok || w.VoidWalkTo == nil {
		t.Fatalf("the Wilderness dungeon section must fall back to the void walk")
	}
	// The void walk's target lies in the underground Wilderness catch-all: an account there waits for
	// its own Genie or Old Man to fail-teleport it out.
	if c, ok := StrandSiteAt(voidWalkTarget.X, voidWalkTarget.Z, 0); !ok || c.Name != "the underground Wilderness (catch-all)" {
		t.Fatalf("the void walk target = %q %v, want the catch-all", c.Name, ok)
	}
	// 3064,10321 (left the first box) is the Wilderness dungeon section now.
	if w, ok := StrandSiteAt(3064, 10321, 0); !ok || w.Name != "the gated Wilderness dungeon section below 3040,3936" {
		t.Fatalf("3064,10321 = %q %v", w.Name, ok)
	}
	// The Varrock underground summon pocket waits for a fail-teleport; the moss giants rung is clear.
	if v, ok := StrandSiteAt(3217, 9910, 0); !ok || v.VoidWalkTo == nil {
		t.Fatalf("3217,9910 = %q %v, want the Varrock pocket", v.Name, ok)
	}
	if m, ok := StrandSiteAt(3159, 9900, 0); ok {
		t.Fatalf("the moss giants rung must not be a strand site, got %q", m.Name)
	}
	// The Edgeville dungeon proper (the Giants) is not a strand site.
	if g, ok := StrandSiteAt(3114, 9833, 0); ok {
		t.Fatalf("the Giants rung must not be a strand site, got %q", g.Name)
	}
	// The KBD lever pocket and its ladder stay their own site.
	if k, ok := StrandSiteAt(3067, 10254, 0); !ok || k.VoidWalkTo != nil {
		t.Fatalf("KBD lever pocket = %q %v, want its own ladder site", k.Name, ok)
	}
}

// The Wizards' Tower west room (3 Oct): escapees land there and the walker cannot cross its
// diagonal door. The site opens the door and steps out, but only after MinStay, because Rune
// Mysteries uses the room's ladder on purpose.
func TestWizardsTowerWestRoomWaitsBeforeActing(t *testing.T) {
	s, ok := StrandSiteAt(3106, 3161, 0)
	if !ok || s.Name != "the Wizards' Tower west room" {
		t.Fatalf("3106,3161 = %q %v", s.Name, ok)
	}
	// Long enough for an account going down to reach the ladder (about 10 s), short enough that an
	// account coming up from Sedridor is out before its walk to Duke Horacio times out.
	if s.MinStay < 30*time.Second || s.MinStay > time.Minute {
		t.Fatalf("MinStay = %v, want 30 s to 1 minute", s.MinStay)
	}
	st := s.StepFor(3106, 3161, 0)
	if st == nil || st.LocID != 1536 || st.Kind != StepDoor || st.Beyond != (StrandTile{X: 3108, Z: 3162}) {
		t.Fatalf("step = %+v", st)
	}
	// Outside the door is not the site.
	if _, ok := StrandSiteAt(3108, 3162, 0); ok {
		t.Fatal("the tile past the door must not be in the site")
	}
}
