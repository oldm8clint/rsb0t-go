package randomevents

import (
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

func TestPickPvPTargetNearestFreeUnrefused(t *testing.T) {
	now := time.Now()
	ps := []gamestate.Player{
		{Index: 5, Name: "far", X: 3100 + 20, Z: 10040},
		{Index: 7, Name: "busy", X: 3101, Z: 10040, InCombat: true},
		{Index: 9, Name: "upstairs", X: 3101, Z: 10040, Level: 1},
		{Index: 3, Name: "refused", X: 3100, Z: 10041},
		{Index: 11, Name: "ok-b", X: 3102, Z: 10040},
		{Index: 4, Name: "ok-a", X: 3098, Z: 10040},
	}
	refused := map[int]time.Time{3: now.Add(time.Minute)}
	p, ok := PickPvPTarget(3100, 10040, 0, 0, ps, refused, now)
	if !ok || p.Name != "ok-a" {
		t.Fatalf("picked %+v %v, want ok-a (nearest free, ties by index)", p, ok)
	}
	refused[4] = now.Add(time.Minute)
	if p, _ = PickPvPTarget(3100, 10040, 0, 0, ps, refused, now); p.Name != "ok-b" {
		t.Fatalf("picked %q, want ok-b", p.Name)
	}
	// A refusal lapses.
	if p, _ = PickPvPTarget(3100, 10040, 0, 0, ps, refused, now.Add(2*time.Minute)); p.Name != "refused" {
		t.Fatalf("after the refusal lapsed picked %q, want the nearest again", p.Name)
	}
	if _, ok := PickPvPTarget(3100, 10040, 0, 0, nil, nil, now); ok {
		t.Fatal("no players, no target")
	}
}

func TestPvPOnlyInWildernessSites(t *testing.T) {
	for _, c := range []struct {
		x, z int
		want bool
	}{{3100, 10040, true}, {3040, 10336, true}, {3100, 10100, true}, {3217, 9910, true}, {3106, 3161, false}, {2718, 9816, false}} {
		s, ok := StrandSiteAt(c.x, c.z, 0)
		if (ok && s.PvP) != c.want {
			t.Fatalf("%d,%d: site %q pvp=%v, want %v", c.x, c.z, s.Name, ok && s.PvP, c.want)
		}
	}
}

func TestWildernessLevelMatchesTheContentZones(t *testing.T) {
	for _, c := range []struct{ x, z, level, want int }{
		{3100, 3519, 0, 0},   // south of the ditch
		{3100, 3520, 0, 1},   // first row
		{3100, 3527, 2, 1},   // every plane on the surface
		{3100, 3528, 0, 2},   // eight tiles per level
		{3100, 9919, 0, 0},   // Edgeville dungeon south of the gate
		{3100, 9920, 0, 1},   // first underground row
		{3100, 9984, 0, 9},   // where the accounts sat on 3 Oct
		{3100, 10040, 0, 16}, // the void walk target
		{3088, 10278, 0, 45}, // pvpRally
		{3100, 10040, 1, 0},  // underground is plane 0 only
		{2943, 10040, 0, 0},  // west of the zone
		{3392, 10040, 0, 0},  // east of the zone
		{3217, 9910, 0, 0},   // the Varrock pocket
	} {
		if got := WildernessLevel(c.x, c.z, c.level); got != c.want {
			t.Errorf("WildernessLevel(%d,%d,%d) = %d, want %d", c.x, c.z, c.level, got, c.want)
		}
	}
}

func TestPvPAllowedIsTheServersLevelCheck(t *testing.T) {
	for _, c := range []struct {
		self, selfWild, other, otherWild int
		want                             bool
	}{
		{40, 9, 49, 9, true},   // nine apart at level 9
		{40, 9, 50, 9, false},  // ten apart at level 9
		{40, 20, 50, 9, false}, // the lower of the two levels counts
		{40, 0, 40, 9, false},  // one of them outside the Wilderness
		{0, 5, 90, 5, true},    // own level unknown: let the server decide
		{40, 5, 0, 5, true},    // theirs unknown
		{2, 45, 47, 45, true},  // the rally covers a wide gap
	} {
		if got := PvPAllowed(c.self, c.selfWild, c.other, c.otherWild); got != c.want {
			t.Errorf("PvPAllowed(%d,%d,%d,%d) = %v, want %v", c.self, c.selfWild, c.other, c.otherWild, got, c.want)
		}
	}
}

// At Wilderness level 9 (z 9984) a combat-40 account skips the nearer combat-60 player the server would
// refuse and takes the combat-45 one.
func TestPickPvPTargetSkipsPlayersOutOfLevelRange(t *testing.T) {
	now := time.Now()
	ps := []gamestate.Player{
		{Index: 1, Name: "too-high", X: 3101, Z: 9984, CombatLevel: 60},
		{Index: 2, Name: "in-range", X: 3104, Z: 9984, CombatLevel: 45},
	}
	if p, ok := PickPvPTarget(3100, 9984, 0, 40, ps, nil, now); !ok || p.Name != "in-range" {
		t.Fatalf("picked %+v %v, want in-range", p, ok)
	}
	if _, ok := PickPvPTarget(3100, 9984, 0, 40, ps[:1], nil, now); ok {
		t.Fatal("picked a player twenty levels apart at Wilderness level 9")
	}
	// Further north the same pair may fight.
	ps[0].Z = 10100
	if p, ok := PickPvPTarget(3100, 10100, 0, 40, ps[:1], nil, now); !ok || p.Name != "too-high" {
		t.Fatalf("at level 23 picked %+v %v, want too-high", p, ok)
	}
}

// From the moment an account is in a PvP site its scripts are held (Busy, and EmergencyFlee steps
// aside), even on a poll where a script still holds the action lock; leaving the site lets them go.
func TestPvPSiteHoldsTheAccountEvenWhenTheLockIsBusy(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	setTileExact(sess.state, 3100, 9984, 0)
	unlock := g.Lock()
	if g.recoverStrand() {
		t.Fatal("took the poll under a script's lock")
	}
	if !strandHolds(sess) || !g.Busy() {
		t.Fatal("not holding the account in the underground Wilderness")
	}
	unlock()
	if !g.recoverStrand() {
		t.Fatal("the PvP escape did not take the poll")
	}
	setTileExact(sess.state, 3222, 3218, 0)
	g.recoverStrand()
	if strandHolds(sess) || g.Busy() {
		t.Fatal("still holding after the account reached Lumbridge")
	}
}

// With nobody to fight, the account hops north toward pvpRally.
func TestPvPWithNobodyInRangeHeadsForTheRally(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	setTileExact(sess.state, 3100, 9984, 0)
	if !g.recoverStrand() {
		t.Fatal("the PvP escape did not take the poll")
	}
	if sess.count() != 1 {
		t.Fatalf("sent %d packets, want one walk hop", sess.count())
	}
	if g.void.lastZ != 9984 || !g.pvp.rally {
		t.Fatalf("void state %+v rally %v, want a hop from z 9984 toward the rally", g.void, g.pvp.rally)
	}
}

// The dungeons south of the underground Wilderness hold the moss giants rung, so they hold an
// account only once its drift check has found no walk home, and keep holding it after that.
func TestNoWayHomeSiteNeedsAFailedWalkHome(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	setTileExact(sess.state, 3155, 9919, 0)
	if _, ok := StrandSiteAt(3155, 9919, 0); ok {
		t.Fatal("the package-level lookup returned a NoWayHomeOnly site")
	}
	if g.recoverStrand() || strandHolds(sess) {
		t.Fatal("held an account that has a walk home")
	}
	NoteNoWayHome(sess)
	if !g.recoverStrand() || !strandHolds(sess) {
		t.Fatal("did not hold an account with no walk home")
	}
	g.mu.Lock()
	g.noWayHome = time.Time{}
	g.mu.Unlock()
	if !g.recoverStrand() || !strandHolds(sess) {
		t.Fatal("let the account go while it is still inside the site")
	}
}

// Every stranded account walks to the rally before it fights.
func TestPvPFarFromTheRallyWalksInsteadOfFighting(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	setTileExact(sess.state, 3100, 10200, 0)
	if !g.recoverStrand() {
		t.Fatal("the PvP escape did not take the poll")
	}
	if !g.pvp.rally || g.pvp.target != -1 || g.void.lastZ != 10200 {
		t.Fatalf("rally %v target %d void %+v, want a hop toward the rally", g.pvp.rally, g.pvp.target, g.void)
	}
}

func TestStrandedAccountsHandleOnlyTheBoxAndTrappedEvents(t *testing.T) {
	for _, c := range []struct {
		ev   Event
		want bool
	}{
		{Event{Kind: KindBoxLamp, Name: "Strange box"}, true},
		{Event{Kind: KindBoxLamp, Name: "Lamp"}, false},
		{Event{Kind: KindTrapped, Name: "Maze"}, true},
		{Event{Kind: KindDialogNpc, Name: "Genie"}, false},
		{Event{Kind: KindEvade, Name: "Evil Chicken"}, false},
		{Event{Kind: KindHazard}, false},
	} {
		if got := strandHandles(&c.ev); got != c.want {
			t.Fatalf("%+v: handled %v, want %v", c.ev, got, c.want)
		}
	}
}

// An account whose hops toward the rally a wall keeps turning back fights where it stands.
func TestPvPWalksToTheRallyUntilAWallStopsIt(t *testing.T) {
	for _, c := range []struct {
		x, z, stuck int
		want        bool
	}{{3155, 9950, 0, true}, {3155, 9950, voidStuckSlow - 1, true}, {3155, 9950, voidStuckSlow, false}, {3090, 10270, 0, false}, {3095, 10040, 0, false}, {3100, 10200, 0, true}} {
		if got := pvpWalksToRally(c.x, c.z, c.stuck); got != c.want {
			t.Fatalf("%d,%d stuck %d: walks %v, want %v", c.x, c.z, c.stuck, got, c.want)
		}
	}
}

func TestLegWithinCutsTheRouteAtTheBox(t *testing.T) {
	p := func(x, z int) pathfind.NavPoint { return pathfind.NavPoint{X: x, Z: z} }
	got := LegWithin([]pathfind.NavPoint{p(100, 100), p(110, 100), p(140, 130)}, 20)
	want := []pathfind.NavPoint{p(100, 100), p(110, 100), p(120, 110)}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if got := LegWithin([]pathfind.NavPoint{p(0, 0), p(5, 5)}, 20); len(got) != 2 {
		t.Fatalf("short route cut: %v", got)
	}
}

func TestSouthernAccountsRallyOnRealMap(t *testing.T) {
	if pvpRallyFor(10047) != pvpRallySouth || pvpRallyFor(10048) != pvpRally {
		t.Fatal("wrong rally either side of the void edge")
	}
	if WildernessLevel(pvpRallySouth.X, pvpRallySouth.Z, 0) != 16 {
		t.Fatalf("southern rally at Wilderness level %d", WildernessLevel(pvpRallySouth.X, pvpRallySouth.Z, 0))
	}
}

// A stranded account on another world asks once to hop to pvpWorld, and not again for pvpHopEvery.
func TestPvPHopsStrandedAccountsToOneWorld(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	world, hops := 3, 0
	g.World = func() int { return world }
	g.HopWorld = func(w int, _ string) error {
		if w != pvpWorld {
			t.Fatalf("hop to world %d, want %d", w, pvpWorld)
		}
		hops++
		return nil
	}
	setTileExact(sess.state, 3092, 10044, 0)
	g.recoverStrand()
	g.recoverStrand()
	if hops != 1 {
		t.Fatalf("%d hop requests, want 1", hops)
	}
	world = pvpWorld
	g.pvpHopAt = time.Time{}
	g.recoverStrand()
	if hops != 1 {
		t.Fatal("asked to hop while already on the PvP world")
	}
}

// An account with nobody to fight at its rally for pvpIdleNorth heads for pvpDeep.
func TestPvPIdleAtTheRallyHeadsNorth(t *testing.T) {
	sess := newRecordingSession()
	g := NewGuardian(sess, nil)
	defer g.Stop()
	setTileExact(sess.state, pvpRallySouth.X, pvpRallySouth.Z, 0)
	g.recoverStrand()
	if g.pvp.north || g.pvp.idleSince.IsZero() {
		t.Fatalf("north %v idleSince %v after the first idle poll", g.pvp.north, g.pvp.idleSince)
	}
	g.pvp.idleSince = time.Now().Add(-pvpIdleNorth)
	g.recoverStrand()
	if !g.pvp.north || g.rallyFor(pvpRallySouth.Z) != pvpDeep {
		t.Fatal("did not head north after standing idle at the rally")
	}
	if WildernessLevel(pvpDeep.X, pvpDeep.Z, 0) < 55 {
		t.Fatalf("pvpDeep is Wilderness level %d", WildernessLevel(pvpDeep.X, pvpDeep.Z, 0))
	}
}
