package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
)

// The five spirit-tree jumps of the 14 Oct 2026 log (build d5362bb2, 04:05-04:45, 22 ScarabSwarm accounts): every one read
// "UNEXPLAINED: not a catalogued fail-teleport destination". The tiles are the log's own.
var spiritTreeJumps = []struct {
	name                   string
	fromX, fromZ, toX, toZ int
}{
	{"Khazard young spirit -> Village", 2555, 3259, 2542, 3169},
	{"Village spirit -> Varrock forest", 2542, 3169, 3179, 3507},
	{"Varrock young spirit -> Village", 3179, 3507, 2542, 3169},
	{"Village spirit -> Khazard", 2542, 3169, 2555, 3259},
	{"Village spirit -> Stronghold", 2542, 3169, 2461, 3444},
}

func TestSpiritTreeHopsReadAsExpectedWhenAnnounced(t *testing.T) {
	for _, j := range spiritTreeJumps {
		// The control: unannounced, the jump reads as the log read it.
		r := newTeleportRig(t, j.fromX, j.fromZ)
		if line := r.jump(j.toX, j.toZ); !strings.Contains(line, "UNEXPLAINED") {
			t.Fatalf("%s: an unannounced jump reads %q, want UNEXPLAINED (the fixture of this test)", j.name, line)
		}
		// Announced the way Walker.announceHop does it (the edge's own From and To).
		r = newTeleportRig(t, j.fromX, j.fromZ)
		ExpectHop(r.s, "hop "+j.name, j.fromX, j.fromZ, 0, j.toX, j.toZ, 0, 45*time.Second)
		line := r.jump(j.toX+1, j.toZ-1) // the engine sets a teleport down within two tiles
		if !strings.Contains(line, "expected: hop "+j.name) || strings.Contains(line, "UNEXPLAINED") {
			t.Errorf("%s: an announced jump reads %q", j.name, line)
		}
	}
}

// Twelve accounts landing unexplained in one mapsquare within 90 seconds teach the Guardian a twelve-hour strand site there, and the
// site's ladder then casts a teleport on accounts that merely arrived. Spirit-tree hops announced never count.
func TestAnnouncedSpiritTreeHopsNeverReachTheMassLearner(t *testing.T) {
	t.Cleanup(func() { mass = newMassTracker() })
	mass = newMassTracker()
	run := func(announce bool) (learned bool) {
		for i := 0; i < massThreshold+1; i++ {
			r := newTeleportRig(t, 3179, 3507)
			if announce {
				ExpectHop(r.s, "hop Varrock young spirit -> Village", 3179, 3507, 0, 2542, 3169, 0, 45*time.Second)
			}
			r.jump(2542, 3169)
			for _, l := range r.logs {
				if strings.Contains(l, "MASS DISPLACEMENT") {
					learned = true
				}
			}
		}
		return learned
	}
	if run(true) {
		t.Fatal("13 announced spirit-tree hops taught the Guardian a strand site")
	}
	if _, ok := learnedSiteAt(2542, 3169, 0, time.Now()); ok {
		t.Fatal("a strand site was learned from announced hops")
	}
	if !run(false) {
		t.Error("13 unannounced hops did not reach the mass learner: the test no longer tests the bypass")
	}
}

// A hop the classifier already explains keeps its own, more exact, wording: ExpectHop announces only what would read as alarming.
func TestExpectHopLeavesOrdinaryHopsToTheirOwnWording(t *testing.T) {
	registered := func(r *teleportRig) int {
		expectedTeleports.mu.Lock()
		defer expectedTeleports.mu.Unlock()
		return len(expectedTeleports.by[r.s])
	}
	// Brimhaven -> Ardougne on the ship: the deck, read as a ship crossing whatever the announcer says.
	r := newTeleportRig(t, 2772, 3231)
	if d, moved := ClassifyDisplacement(2772, 3231, 0, 2683, 3268, 1, pollInterval); !moved || d.Kind != DisplacementDeck {
		t.Fatalf("the ship crossing classifies as %q (moved %v), want %q", d.Kind, moved, DisplacementDeck)
	}
	ExpectHop(r.s, "hop Brimhaven->Ardougne ship", 2772, 3231, 0, 2683, 3268, 1, 45*time.Second)
	if n := registered(r); n != 0 {
		t.Errorf("%d expectations were registered for a ship crossing", n)
	}
	// The sewer manhole: an ordinary dungeon entrance, with its own wording.
	r = newTeleportRig(t, 3237, 3459)
	ExpectHop(r.s, "hop Climb down Manhole", 3237, 3459, 0, 3237, 9859, 0, 45*time.Second)
	if n := registered(r); n != 0 {
		t.Errorf("%d expectations were registered for a dungeon entrance", n)
	}
	if line := r.jump(3237, 9859); strings.Contains(line, "expected:") || !strings.Contains(line, "dungeon") {
		t.Errorf("a manhole reads %q, want the dungeon wording", line)
	}
	// A hop a walk could cover is nothing to announce.
	r = newTeleportRig(t, 3000, 3000)
	ExpectHop(r.s, "hop Open Door", 3000, 3000, 0, 3002, 3000, 0, 45*time.Second)
	if n := registered(r); n != 0 {
		t.Errorf("%d expectations were registered for a two-tile hop", n)
	}
}

// Climbing out of Sedridor's basement lands every Rune Mysteries account in the Wizards' Tower west room, a catalogued strand site: the log
// called it a place "the server summons whole cohorts" into, twelve times in forty minutes. A hop the walker announces is its own doing.
func TestAHopIntoAStrandSiteIsTheWalkersOwnDoing(t *testing.T) {
	r := newTeleportRig(t, 3103, 9577)
	if line := r.jump(3105, 3162); !strings.Contains(line, "STRAND SITE") {
		t.Fatalf("unannounced it reads %q (the fixture of this test)", line)
	}
	r = newTeleportRig(t, 3103, 9577)
	ExpectHop(r.s, `hop Climb-up "Ladder"`, 3103, 9577, 0, 3105, 3162, 0, 45*time.Second)
	if line := r.jump(3105, 3162); !strings.Contains(line, `expected: hop Climb-up "Ladder"`) || strings.Contains(line, "STRAND SITE") {
		t.Errorf("announced it reads %q", line)
	}
}

// A withdrawn announcement excuses nothing: the hop failed, so a jump that then appears is somebody else's.
func TestAWithdrawnAnnouncementExcusesNothing(t *testing.T) {
	r := newTeleportRig(t, 2542, 3169)
	withdraw := ExpectHop(r.s, "hop Village spirit -> Varrock forest", 2542, 3169, 0, 3179, 3507, 0, 45*time.Second)
	withdraw()
	if line := r.jump(3179, 3507); !strings.Contains(line, "UNEXPLAINED") {
		t.Errorf("a withdrawn announcement still excused the jump: %q", line)
	}
	// Withdrawing after the jump was seen is harmless.
	r = newTeleportRig(t, 2542, 3169)
	withdraw = ExpectHop(r.s, "hop Village spirit -> Varrock forest", 2542, 3169, 0, 3179, 3507, 0, 45*time.Second)
	if line := r.jump(3179, 3507); !strings.Contains(line, "expected:") {
		t.Fatalf("announced jump reads %q", line)
	}
	withdraw()
}

// The essence mine's teleporters set an account down on one of 22 spots of mapsquare 45,75, and its portal on the doorstep of whichever
// teleporter sent it in: announcements with boxes and with several landings.
func TestMineCrossingsAreExpectedByRegion(t *testing.T) {
	// In: the Distentor's teleport, 2596,3087 -> 2897,4808 in the log; the box is the mapsquare.
	r := newTeleportRig(t, 2596, 3087)
	ExpectLandings(r.s, "essence mine: Wizard Distentor's teleport", 30*time.Second, LandingSquare(45, 75))
	if line := r.jump(2897, 4808); !strings.Contains(line, "expected: essence mine: Wizard Distentor's teleport") {
		t.Errorf("the teleport in reads %q", line)
	}
	// Out: the portal, 2931,4854 -> 2590,3086; three doorsteps are expected and the first match uses the whole announcement up.
	r = newTeleportRig(t, 2931, 4854)
	ExpectLandings(r.s, "essence mine: the portal out", 25*time.Second,
		LandingNear(3253, 3402, 7), LandingNear(2594, 3089, 7), LandingNear(2683, 3326, 7))
	if line := r.jump(2590, 3086); !strings.Contains(line, "expected: essence mine: the portal out") {
		t.Errorf("the portal out reads %q", line)
	}
	setTileExact(r.s.state, 2931, 4854, 0)
	r.g.watchDisplacement()
	if line := r.jump(3253, 3401); strings.Contains(line, "expected:") {
		t.Errorf("a second landing on another doorstep was excused by the same announcement: %q", line)
	}
	// A landing outside every box is not excused.
	r = newTeleportRig(t, 2931, 4854)
	ExpectLandings(r.s, "essence mine: the portal out", 25*time.Second, LandingNear(2594, 3089, 7))
	if line := r.jump(3222, 3218); strings.Contains(line, "expected:") {
		t.Errorf("a landing on the Lumbridge spawn tile was excused by the Yanille doorstep: %q", line)
	}
}

// Runecraft's own crossings, with the temple table's tiles: the ruins put the account on Enter, the portal on Exit.
func TestRunecraftCrossingsAreExpected(t *testing.T) {
	cases := []struct {
		name                   string
		fromX, fromZ, toX, toZ int
	}{
		{"Air ruins", 2984, 3294, 2841, 4830},
		{"Air portal", 2841, 4829, 2983, 3288},
		{"Body ruins", 3052, 3447, 2523, 4838},
		{"Body portal", 2523, 4826, 3051, 3441},
	}
	for _, c := range cases {
		r := newTeleportRig(t, c.fromX, c.fromZ)
		if line := r.jump(c.toX, c.toZ); strings.Contains(line, "expected:") {
			t.Fatalf("%s: unannounced it reads %q", c.name, line)
		}
		r = newTeleportRig(t, c.fromX, c.fromZ)
		ExpectLandings(r.s, "runecraft: "+c.name, 20*time.Second, LandingNear(c.toX, c.toZ, 3))
		if line := r.jump(c.toX, c.toZ); !strings.Contains(line, "expected: runecraft: "+c.name) {
			t.Errorf("%s: announced it reads %q", c.name, line)
		}
	}
}

// Expectations of sessions that announced once and stopped do not pile up: any announcement sweeps every session's expired ones.
func TestExpiredExpectationsOfOtherSessionsAreSwept(t *testing.T) {
	gone := &safeRefSession{state: gamestate.New()}
	ExpectLandings(gone, "x", time.Millisecond, LandingNear(1, 1, 1))
	time.Sleep(5 * time.Millisecond)
	expectedTeleports.mu.Lock()
	expectedTeleports.lastSweep = time.Time{}
	expectedTeleports.mu.Unlock()
	other := &safeRefSession{state: gamestate.New()}
	defer ForgetTeleports(other)
	ExpectLandings(other, "y", time.Minute, LandingNear(2, 2, 1))
	expectedTeleports.mu.Lock()
	_, kept := expectedTeleports.by[gone]
	expectedTeleports.mu.Unlock()
	if kept {
		t.Error("an expired expectation of another session survived the sweep")
	}
}
