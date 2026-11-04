package randomevents

import (
	"strings"
	"testing"
	"time"
)

// The tiles below are read straight off run/fleet.log (2026-09-29 20:15 to 2026-10-02 17:07), the
// "[guardian] displaced: A -> B" lines whose label this change corrects.

func TestStrandSiteAtTheArrivalTilesThatWereMeasured(t *testing.T) {
	for _, c := range []struct {
		name           string
		x, z, level    int
		wantSite       string
		wantNoTeleport bool
	}{
		{"KBD lair, the tile 175 accounts landed on", 2718, 9816, 0, "King Black Dragon's lair", false},
		{"KBD lair, beside the lever", 2717, 9802, 0, "King Black Dragon's lair", false},
		{"KBD lair, the corner the long-lived ones stood in", 2710, 9807, 0, "King Black Dragon's lair", false},
		{"the lair's own lever lands here", 3067, 10254, 0, "the KBD lever's pocket in the Wilderness dungeon", true},
		{"Rogues' Castle roof", 3282, 3936, 3, "Rogues' Castle", true},
		{"Rogues' Castle roof, beside the stair", 3281, 3937, 3, "Rogues' Castle", true},
		{"Rogues' Castle second floor", 3282, 3936, 2, "Rogues' Castle", true},
		{"Rogues' Castle first floor", 3282, 3936, 1, "Rogues' Castle", true},
		{"Rogues' Castle ground-floor room", 3282, 3936, 0, "Rogues' Castle", true},
	} {
		site, ok := StrandSiteAt(c.x, c.z, c.level)
		if !ok || !strings.Contains(site.Name, c.wantSite) {
			t.Errorf("%s: StrandSiteAt(%d,%d,%d) = %q, %v; want a site named %q", c.name, c.x, c.z, c.level, site.Name, ok, c.wantSite)
			continue
		}
		if site.NoTeleport != c.wantNoTeleport {
			t.Errorf("%s: NoTeleport = %v, want %v (content refuses a teleport above Wilderness level 20 and nowhere else here)", c.name, site.NoTeleport, c.wantNoTeleport)
		}
	}
}

func TestStrandSiteAtLeavesOrdinaryGroundAlone(t *testing.T) {
	for _, c := range []struct {
		name        string
		x, z, level int
	}{
		{"outside the castle door", 3279, 3940, 0},
		{"the Keep lever", 3153, 3923, 0},
		{"Lumbridge respawn", 3222, 3218, 0},
		{"the Mage Bank is the trip's own, not a strand", 2543, 4713, 0},
		{"the ruined house", 3092, 3957, 0},
		{"a castle floor's tile in the wrong level", 3282, 3936, 4},
		{"the Ardougne market", 2668, 3312, 0},
		{"the Mime's own square", 2008, 4764, 0},
	} {
		if site, ok := StrandSiteAt(c.x, c.z, c.level); ok {
			t.Errorf("%s: %d,%d,%d is in %q, which would hand an ordinary account to the escape ladder", c.name, c.x, c.z, c.level, site.Name)
		}
	}
}

// The Mage Bank is a pocket for the walker (its way out enters the Wilderness zone) without being a
// strand site for the Guardian (a trip legitimately stands there).
func TestMageBankIsAPocketButNotAStrandSite(t *testing.T) {
	if _, ok := StrandSiteAt(2543, 4713, 0); ok {
		t.Fatal("the Mage Bank is in the Guardian's catalogue: the ladder would take ScarabSwarm off a trip mid-purchase")
	}
	if name, ok := PocketAt(2543, 4713, 0); !ok || !strings.Contains(name, "Mage Bank") {
		t.Fatalf("PocketAt(Mage Bank) = %q, %v", name, ok)
	}
	if _, ok := PocketAt(3222, 3218, 0); ok {
		t.Fatal("Lumbridge is not a pocket")
	}
	if _, owned := StrandOwner(2543, 4713, 0); owned {
		t.Fatal("the Mage Bank has no Guardian-owned exit")
	}
	if name, owned := StrandOwner(3282, 3936, 3); !owned || !strings.Contains(name, "Rogues") {
		t.Fatalf("StrandOwner(castle roof) = %q, %v", name, owned)
	}
}

// Every tile in a catalogued site has an exit step, so the ladder is never left standing on a tile of
// its own site with nothing to do; every step's stands lie inside the step's own area and on its level;
// and every arrival tile is inside the site.
func TestCatalogueIsInternallyConsistent(t *testing.T) {
	for _, site := range strandSites {
		if len(site.Areas) == 0 || site.Name == "" || site.Why == "" {
			t.Errorf("site %q needs areas, a name and its evidence", site.Name)
		}
		for _, a := range site.Arrivals {
			if !site.Contains(a.X, a.Z, a.Level) {
				t.Errorf("%s: arrival %v is outside the site", site.Name, a)
			}
		}
		if len(site.Steps) == 0 && site.VoidWalkTo != nil {
			// A void site has no loc to operate: the void walk is its exit from every tile.
			continue
		}
		if site.Escape != nil {
			// An escape site has no fixed steps: its exit is planned (strand_escape_test.go proves it from every tile).
			if len(site.Steps) != 0 {
				t.Errorf("%s: an Escape site must not also list fixed steps", site.Name)
			}
			if cat, err := site.Escape(); err != nil || cat == nil {
				t.Errorf("%s: the escape catalogue does not load: %v", site.Name, err)
			}
			continue
		}
		for _, a := range site.Areas {
			for x := a.MinX; x <= a.MaxX; x++ {
				for z := a.MinZ; z <= a.MaxZ; z++ {
					if site.StepFor(x, z, a.Level) == nil {
						t.Fatalf("%s: tile %d,%d,%d is in the site and has no exit step", site.Name, x, z, a.Level)
					}
				}
			}
		}
		for _, st := range site.Steps {
			if len(st.Stands) == 0 {
				t.Errorf("%s / %s: no stand tile", site.Name, st.Name)
			}
			for _, s := range st.Stands {
				if !st.Where.Contains(s.X, s.Z, s.Level) {
					t.Errorf("%s / %s: stand %v is outside the step's own area %v", site.Name, st.Name, s, st.Where)
				}
			}
			if st.Op < 1 || st.Op > 5 || st.OpLabel == "" || st.LocID <= 0 {
				t.Errorf("%s / %s: needs a loc id, an option number 1-5 and its label", site.Name, st.Name)
			}
			switch st.Kind {
			case StepDoor:
				if st.Beyond == (StrandTile{}) || site.Contains(st.Beyond.X, st.Beyond.Z, st.Beyond.Level) {
					t.Errorf("%s / %s: a door's far side must be set and outside the site, got %v", site.Name, st.Name, st.Beyond)
				}
			case StepLoc:
			default:
				t.Errorf("%s / %s: unknown step kind %q", site.Name, st.Name, st.Kind)
			}
		}
	}
}

func TestKnownFixtureNamesWhatTheLogCalledUnexplained(t *testing.T) {
	for _, c := range []struct {
		name           string
		fx, fz, tx, tz int
		want           string
	}{
		{"Ardougne lever", 2561, 3311, 3154, 3924, "Ardougne lever"},
		{"Keep lever", 3153, 3923, 2562, 3311, "Deserted Keep lever"},
		{"Mage Bank ladder down", 3092, 3957, 2543, 4713, "ladder down"},
		{"Mage Bank ladder up", 2543, 4713, 3092, 3957, "ladder up"},
		{"mime exit to the market", 2008, 4762, 2669, 3310, "Mime or maze"},
		{"mime exit to wherever", 2008, 4762, 3269, 3169, "Mime or maze"},
		{"maze exit", 2906, 4584, 2618, 3315, "Mime or maze"},
		{"waterfall door", 2575, 9861, 2511, 3463, "Waterfall dungeon door"},
		{"waterfall washout from the mound", 2511, 3463, 2527, 3413, "washout"},
		{"waterfall washout from the river", 2512, 3472, 2527, 3413, "washout"},
	} {
		got, ok := KnownFixture(c.fx, c.fz, c.tx, c.tz)
		if !ok || !strings.Contains(got, c.want) {
			t.Errorf("%s: KnownFixture(%d,%d -> %d,%d) = %q, %v; want a name containing %q", c.name, c.fx, c.fz, c.tx, c.tz, got, ok, c.want)
		}
	}
	for _, c := range []struct {
		name           string
		fx, fz, tx, tz int
	}{
		{"the castle summon is not a fixture", 3094, 3489, 3282, 3936},
		{"the KBD summon is not a fixture", 2669, 3310, 2718, 9816},
		{"a ladder from somewhere else to the Mage Bank", 3000, 3957, 2543, 4713},
		{"a random pair", 3115, 9836, 3500, 3500},
		{"a jump inside the maze is not its end", 2906, 4584, 2920, 4590},
		{"arriving at the Mime is not leaving it", 2669, 3310, 2008, 4764},
	} {
		if got, ok := KnownFixture(c.fx, c.fz, c.tx, c.tz); ok {
			t.Errorf("%s: KnownFixture named %q", c.name, got)
		}
	}
}

// The real lines, through ClassifyDisplacement: what each one said before and says now.
func TestClassifyDisplacementOnTheRealLogLines(t *testing.T) {
	gap := 600 * time.Millisecond
	for _, c := range []struct {
		name                   string
		fx, fz, fl, tx, tz, tl int
		want                   DisplacementKind
		contains               string
	}{
		{"KBD summon from the Edgeville dungeon", 3117, 9836, 0, 2718, 9816, 0, DisplacementStrandSite, "King Black Dragon"},
		{"KBD summon from the Ardougne market (was 'underground')", 2669, 3310, 0, 2718, 9816, 0, DisplacementStrandSite, "King Black Dragon"},
		{"KBD summon from the surface counterpart of its mapsquare", 2700, 3403, 0, 2718, 9816, 0, DisplacementStrandSite, "King Black Dragon"},
		{"castle summon from the Edgeville dungeon", 3118, 9842, 0, 3282, 3936, 3, DisplacementStrandSite, "Rogues' Castle"},
		{"castle summon from Edgeville bank", 3094, 3489, 0, 3282, 3936, 3, DisplacementStrandSite, "Rogues' Castle"},
		{"the stair down from the roof is still a stair", 3281, 3936, 3, 3282, 3936, 2, DisplacementLevelChange, ""},
		{"Ardougne lever", 2561, 3311, 0, 3154, 3924, 0, DisplacementFixture, "Ardougne lever"},
		{"Mage Bank ladder down", 3092, 3957, 0, 2543, 4713, 0, DisplacementFixture, "ladder down"},
		{"mime exit", 2008, 4762, 0, 2669, 3310, 0, DisplacementFixture, "Mime or maze"},
		{"Edgeville trapdoor is still an underground pair", 3096, 9868, 0, 3096, 3468, 0, DisplacementUnderground, ""},
		{"a stranger from nowhere is still UNEXPLAINED", 3115, 9836, 0, 3500, 3500, 0, DisplacementUnexplained, ""},
	} {
		d, moved := ClassifyDisplacement(c.fx, c.fz, c.fl, c.tx, c.tz, c.tl, gap)
		if !moved {
			t.Errorf("%s: not even a displacement", c.name)
			continue
		}
		if d.Kind != c.want {
			t.Errorf("%s: kind %q, want %q (%s)", c.name, d.Kind, c.want, d.String())
		}
		if c.contains != "" && !strings.Contains(d.String(), c.contains) {
			t.Errorf("%s: line %q does not say %q", c.name, d.String(), c.contains)
		}
		if c.want != DisplacementUnexplained && strings.Contains(d.String(), "UNEXPLAINED") {
			t.Errorf("%s: still says UNEXPLAINED: %s", c.name, d.String())
		}
	}
}

// The mass tracker learns a site from a burst and forgets it: twelve distinct accounts inside the window
// make one, the same account twice is one, the window and the TTL both run out.
func TestMassTrackerLearnsABurstOnceAndForgetsIt(t *testing.T) {
	m := newMassTracker()
	t0 := time.Date(2026, 10, 3, 4, 0, 0, 0, time.UTC)
	type acct struct{ id int }
	accts := make([]*acct, 20)
	for i := range accts {
		accts[i] = &acct{i}
	}
	learnedAt := -1
	for i := 0; i < massThreshold+3; i++ {
		count, learned := m.note(t0.Add(time.Duration(i)*time.Second), accts[i], 3300, 3900, 0)
		if learned {
			if learnedAt >= 0 {
				t.Fatalf("learned twice, at arrival %d and %d", learnedAt, i)
			}
			learnedAt = i
			if count != massThreshold {
				t.Errorf("learned at %d accounts, want exactly the threshold %d", count, massThreshold)
			}
		}
	}
	if learnedAt != massThreshold-1 {
		t.Fatalf("learned at arrival %d, want %d (the %dth account)", learnedAt, massThreshold-1, massThreshold)
	}
	now := t0.Add(time.Minute)
	site, ok := m.siteAt(3301, 3901, 0, now)
	if !ok || !site.Learned || !strings.Contains(site.Name, "mapsquare 51,60") {
		t.Fatalf("learned site = %+v, %v", site, ok)
	}
	if _, ok := m.siteAt(3301, 3901, 1, now); ok {
		t.Error("a learned site must be per level")
	}
	if _, ok := m.siteAt(3200, 3200, 0, now); ok {
		t.Error("a learned site must be per mapsquare")
	}
	if _, ok := m.siteAt(3301, 3901, 0, t0.Add(massTTL+time.Minute)); ok {
		t.Error("a learned site must be forgotten after the TTL")
	}

	// One account repeating itself is one account.
	m2 := newMassTracker()
	for i := 0; i < 50; i++ {
		if _, learned := m2.note(t0.Add(time.Duration(i)*time.Second), accts[0], 3300, 3900, 0); learned {
			t.Fatal("one account arriving fifty times made a mass site")
		}
	}
	// Twelve accounts spread over longer than the window are not a burst.
	m3 := newMassTracker()
	for i := 0; i < massThreshold*2; i++ {
		if _, learned := m3.note(t0.Add(time.Duration(i)*massWindow), accts[i%len(accts)], 3300, 3900, 0); learned {
			t.Fatal("arrivals a whole window apart made a mass site")
		}
	}
}

// A learned site has no steps, so the ladder can only cast from it, and gives up (releasing the scripts)
// when it cannot.
func TestLearnedSiteGivesUpWhenNothingIsCastable(t *testing.T) {
	site := StrandSite{Name: "learned", Learned: true, Areas: []StrandRect{{MinX: 0, MaxX: 63, MinZ: 0, MaxZ: 63, Level: 0}}}
	var l StrandLadder
	t0 := time.Unix(1_000_000, 0)
	l.Next(StrandObs{Now: t0, X: 5, Z: 5, Site: &site})
	act := l.Next(StrandObs{Now: t0.Add(strandGrace + time.Second), X: 5, Z: 5, Site: &site})
	if act.Rung != RungGiveUp {
		t.Fatalf("a learned site with nothing castable chose %q, want give-up", act.Rung)
	}
	if l.Active(t0.Add(time.Hour)) {
		t.Error("a ladder that gave up must not keep the scripts waiting")
	}
}
