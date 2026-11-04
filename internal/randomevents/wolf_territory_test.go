package randomevents

import (
	"os"
	"sync"
	"testing"

	"rsb0t-go/internal/pathfind"
)

func TestWolfTerritoryCoversLiveDeathsButNotTheCamp(t *testing.T) {
	for _, c := range []struct {
		name string
		p    pathfind.NavPoint
		want bool
	}{
		{"android died 2026-09-19 02:40", pathfind.NavPoint{X: 2861, Z: 3494}, true},
		{"craft died 2026-09-19 03:25", pathfind.NavPoint{X: 2849, Z: 3508}, true},
		{"west edge of the pack's reach", pathfind.NavPoint{X: 2814, Z: 3500}, true},
		{"live flee landing west of the pack (its route was the danger)", pathfind.NavPoint{X: 2809, Z: 3497}, false},
		{"Taverley foot sentry", pathfind.NavPoint{X: 2866, Z: 3438}, true},
		{"Catherby fishing camp", pathfind.NavPoint{X: 2846, Z: 3430}, false},
		{"Catherby bank", pathfind.NavPoint{X: 2809, Z: 3441}, false},
		{"Taverley", pathfind.NavPoint{X: 2895, Z: 3428}, false},
		{"upstairs is never the mountain", pathfind.NavPoint{X: 2861, Z: 3494, Level: 1}, false},
	} {
		if got := inWolfTerritory(c.p); got != c.want {
			t.Errorf("%s %v: inWolfTerritory = %v, want %v", c.name, c.p, got, c.want)
		}
	}
}

func TestRouteExposureCountsOnlyTilesInTerritory(t *testing.T) {
	origin := pathfind.NavPoint{X: 2846, Z: 3430}
	// 2846,3431 .. 2846,3456: the sentry at 2842,3450 reaches z 3445..3455.
	route := []pathfind.NavPoint{{X: 2846, Z: 3456}}
	if got := routeExposure(origin, route); got != 11 {
		t.Fatalf("exposure = %d, want 11", got)
	}
	if got := routeSteps(origin, route); got != 26 {
		t.Fatalf("steps = %d, want 26", got)
	}
	if routeExposure(origin, []pathfind.NavPoint{{X: 2809, Z: 3441}}) != 0 {
		t.Fatal("camp to bank must not count as wolf exposure")
	}
}

func TestVisibleWolfAvoidanceKnowsEveryMountainHostile(t *testing.T) {
	for _, typ := range []int{96, 97, 111, 125, 141, 142} {
		s := overlapState()
		addNPCFacing(s, 1, typ, 4, 0, -1)
		avoid := visibleWolfAvoidance(&overlapSession{state: s}, pathfind.NavPoint{X: 3200, Z: 3200})
		if avoid == nil || !avoid(3201, 3200, 0) || avoid(3199, 3200, 0) {
			t.Fatalf("type %d: wolf avoidance must block approach and allow escape", typ)
		}
	}
}

// The Ice giant (111) and Ice warrior (125) fill the Asgarnian ice dungeon too, and the rule is keyed on the
// type wherever it stands: a flee that ignores a visible warrior walks through the herd (the rune pen's
// safespot is a pocket among them). Live 16-17 Oct 2026 the same refusal also boxed three accounts in, which
// is why a Shade, a Strange plant or a Zombie is left by its leash first (event_leash.go) and this rule only
// meets threats distance cannot shake (herd_flee_realpack_test.go).
func TestVisibleWolfAvoidanceStillAppliesToTheSameTypesInTheIceCave(t *testing.T) {
	home := pathfind.NavPoint{X: 3054, Z: 9578}
	for _, typ := range []int{111, 125} {
		s := overlapState()
		setExactTile(s, home.X, home.Z, 0)
		addNPCFacing(s, 1, typ, 4, 0, -1)
		avoid := visibleWolfAvoidance(&overlapSession{state: s}, home)
		if avoid == nil || !avoid(home.X+1, home.Z, 0) || avoid(home.X-1, home.Z, 0) {
			t.Fatalf("type %d in the ice dungeon: the herd rule must block approach and allow escape", typ)
		}
	}
}

var (
	realPackOnce sync.Once
	realPackVal  *pathfind.Pack
)

func realCollisionPack(t *testing.T) *pathfind.Pack {
	t.Helper()
	path := os.Getenv("RSB0T_COLLISION_PACK")
	if path == "" {
		t.Skip("RSB0T_COLLISION_PACK not set")
	}
	realPackOnce.Do(func() {
		pk, err := pathfind.Load(path)
		if err == nil {
			realPackVal = pk
		}
	})
	if realPackVal == nil {
		t.Fatal("collision pack did not load")
	}
	return realPackVal
}

// boxedInByWolves is visibleWolfAvoidance's predicate for every mountain wolf spawn within
// view of origin — the live situation in which the old search found nothing.
func boxedInByWolves(origin pathfind.NavPoint) func(int, int, int) bool {
	type ex struct{ x, z, r int }
	var ws []ex
	for _, d := range wolfDens {
		if d.r > 5 {
			continue
		}
		if dist := chebyshev(origin.X, origin.Z, d.x, d.z); dist <= 15 {
			r := dist
			if r > 12 {
				r = 12
			}
			if r < 1 {
				r = 1
			}
			ws = append(ws, ex{d.x, d.z, r})
		}
	}
	return func(x, z, level int) bool {
		for _, w := range ws {
			if chebyshev(x, z, w.x, w.z) < w.r {
				return true
			}
		}
		return false
	}
}

// Live 2026-09-19 (android, craft) and cmd/labfleettest -mode wolf-mountain: attacked on the
// pass, the old search stood still ("no reachable flee candidate") or ran through the pack.
func TestMountainFleeLeavesTerritoryInsteadOfStandingStill(t *testing.T) {
	pk := realCollisionPack(t)
	for _, origin := range []pathfind.NavPoint{{X: 2861, Z: 3494}, {X: 2849, Z: 3508}, {X: 2861, Z: 3460}, {X: 2856, Z: 3468}, {X: 2865, Z: 3445}} {
		dest, ok := localEscapeAvoiding(pk, origin, origin.X, origin.Z, fleeDistFor(1), boxedInByWolves(origin), CatherbyBankRef)
		if !ok {
			t.Errorf("%v: no flee found — the account would stand still in combat", origin)
			continue
		}
		if inWolfTerritory(dest) || inDangerZone(dest) {
			t.Errorf("%v: flee lands at %v, still in reach of the wolves", origin, dest)
		}
		route := pk.FindPath(origin, dest)
		if !route.OK || routeSteps(origin, route.Waypoints) > maxFleeDetour(chebyshev(origin.X, origin.Z, dest.X, dest.Z)) {
			t.Errorf("%v: flee to %v is a detour around a barrier", origin, dest)
		}
	}
}

// From the Taverley foot the old search chose the Catherby beach 20 tiles west, a 179-step
// route over the whole pass; the way back into Taverley enters no wolf territory.
func TestTaverleyFootFleeDoesNotCrossThePass(t *testing.T) {
	pk := realCollisionPack(t)
	origin := pathfind.NavPoint{X: 2875, Z: 3428}
	dest, ok := localEscapeAvoiding(pk, origin, origin.X, origin.Z, fleeDistFor(1), nil, CatherbyBankRef)
	if !ok {
		t.Fatal("no flee from the Taverley foot")
	}
	route := pk.FindPath(origin, dest)
	if e := routeExposure(origin, route.Waypoints); e != 0 || dest.X < origin.X {
		t.Fatalf("flee to %v (exposure %d) heads over the pass instead of back into Taverley", dest, e)
	}
}

// The Catherby picks the local lab regressions rely on are unchanged: these are the old
// search's own results (origin/main 35a78e5) for the bank, the beach and the camp.
func TestCatherbyFleePicksUnchanged(t *testing.T) {
	pk := realCollisionPack(t)
	for _, c := range []struct {
		origin, threat pathfind.NavPoint
		dist           int
		want           pathfind.NavPoint
	}{
		{pathfind.NavPoint{X: 2809, Z: 3441}, pathfind.NavPoint{X: 2810, Z: 3441}, 20, pathfind.NavPoint{X: 2789, Z: 3461}},
		{pathfind.NavPoint{X: 2846, Z: 3430}, pathfind.NavPoint{X: 2847, Z: 3430}, 20, pathfind.NavPoint{X: 2810, Z: 3465}},
		{pathfind.NavPoint{X: 2846, Z: 3430}, pathfind.NavPoint{X: 2846, Z: 3430}, 40, pathfind.NavPoint{X: 2806, Z: 3470}},
		{pathfind.NavPoint{X: 2838, Z: 3432}, pathfind.NavPoint{X: 2838, Z: 3432}, 36, pathfind.NavPoint{X: 2802, Z: 3468}},
		{pathfind.NavPoint{X: 2853, Z: 3425}, pathfind.NavPoint{X: 2853, Z: 3425}, 20, pathfind.NavPoint{X: 2811, Z: 3468}},
		{pathfind.NavPoint{X: 3222, Z: 3218}, pathfind.NavPoint{X: 3222, Z: 3218}, 20, pathfind.NavPoint{X: 3222, Z: 3238}},
	} {
		got, ok := localEscape(pk, c.origin, c.threat.X, c.threat.Z, c.dist, CatherbyBankRef)
		if !ok || got != c.want {
			t.Errorf("%v threat %v dist %d: got %v %v, want %v", c.origin, c.threat, c.dist, got, ok, c.want)
		}
	}
}
