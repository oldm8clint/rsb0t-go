package combatrunner

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The tests here are the Barbarian Village long hall, Falador castle's courtyard and the Al Kharid palace on the real
// collision pack, loc index and webwalk graph: the three rungs whose monsters stand behind doors the pack has shut
// (live, 14 October 2026, 04:05-04:45: 1,049 "no route from here to the ..." warnings and 399 waits for "one we
// can reach", every one from an account that had arrived on the wrong side of a wall).

func rungByKey(t *testing.T, key string) combatplan.Rung {
	t.Helper()
	for _, r := range combatplan.SpreadLadder() {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no rung %q on the spread ladder", key)
	return combatplan.Rung{}
}

func tileOf(p webwalk.Point) tile.Tile { return tile.Tile{X: p.X, Z: p.Z, Level: p.Level} }

// walkableBeside is t when the pack can stand on it, else the nearest tile a loc on it can be used from (the
// Barbarian rung's stand is a tile in the hall wall, and a bank's is a booth).
func walkableBeside(pk *pathfind.Pack, t tile.Tile) tile.Tile {
	if pk.Walkable(t.X, t.Z, t.Level) {
		return t
	}
	best := tile.Tile{}
	bestD := 1 << 30
	for _, s := range pk.InteractTiles(navPoint(t)) {
		if d := tile.Chebyshev(tile.Tile{X: s.X, Z: s.Z, Level: s.Level}, t); d < bestD {
			best, bestD = tile.Tile{X: s.X, Z: s.Z, Level: s.Level}, d
		}
	}
	return best
}

// routeTiles expands a planned route's corners into the tiles the walk steps on.
func routeTiles(r webwalk.Route) []webwalk.Point {
	var out []webwalk.Point
	for i, w := range r.Waypoints {
		if i == 0 || w.Hop != nil || len(out) == 0 || out[len(out)-1].Level != w.Level {
			out = append(out, w.Point)
			continue
		}
		x, z := out[len(out)-1].X, out[len(out)-1].Z
		for x != w.X || z != w.Z {
			switch {
			case x < w.X:
				x++
			case x > w.X:
				x--
			}
			switch {
			case z < w.Z:
				z++
			case z > w.Z:
				z--
			}
			out = append(out, webwalk.Point{X: x, Z: z, Level: w.Level})
		}
	}
	return out
}

// bankArrival is where TravelTo(stand, 3) leaves an account coming from the rung's own bank: the first tile of the
// planned walk within three tiles of the stand, which is wherever the route first gets that close, wall or no wall.
func bankArrival(t *testing.T, rd realData, rung combatplan.Rung) tile.Tile {
	t.Helper()
	// A bank is a booth: the tiles it can be used from include the keeper's side of the counter, which no walk leaves,
	// so the first one with a walk to the stand is where the account banks.
	stands := rd.pack.InteractTiles(navPoint(rung.Bank))
	for _, from := range stands {
		route := rd.planner.Plan(webwalk.Point{X: from.X, Z: from.Z, Level: from.Level}, webwalk.Point{X: rung.Stand.X, Z: rung.Stand.Z, Level: rung.Stand.Level}, webwalk.Options{MaxExpansions: 400000})
		if !route.OK {
			continue
		}
		for _, p := range routeTiles(route) {
			if tile.Chebyshev(tileOf(p), rung.Stand) <= 3 {
				return tileOf(p)
			}
		}
	}
	t.Fatalf("rung %q: no walk from any of the %d tiles its bank is used from comes within 3 tiles of the stand", rung.Key, len(stands))
	return tile.Tile{}
}

// palaceDoors are the leaves of the palace's west double door, as the server has them: operating either opens both.
func palaceDoors() []*simDoor {
	return []*simDoor{
		{id: 1506, x: 3287, z: 3172, open: openPalaceDoor, group: "west"},
		{id: 1508, x: 3287, z: 3171, open: openPalaceDoor, group: "west"},
	}
}

// doorWorld is a simulated account at start with the real map under it, and its Deps.
type doorWorld struct {
	w      *simWorld
	c      *simClient
	d      *Deps
	logged *[]string
}

func newDoorWorld(rd realData, start tile.Tile, doors bool) *doorWorld {
	w := newSimWorld(start.X, start.Z, start.Level)
	c := &simClient{w: w, now: time.Unix(1_000_000, 0)}
	var logged []string
	d := &Deps{
		Session: &simSession{st: w.st}, Config: rd.cfg, Pack: rd.pack, Locs: rd.locs,
		Walker: webwalk.NewWalker(rd.planner, c),
		Delay:  func(dur time.Duration) bool { c.now = c.now.Add(dur); return true },
		Log:    func(s string) { logged = append(logged, s) },
		Doors:  doors,
	}
	return &doorWorld{w: w, c: c, d: d, logged: &logged}
}

func (dw *doorWorld) logs() string { return strings.Join(*dw.logged, "\n") }

// TestRungSpawnsBehindDoorsAreReachedThroughThem is the proof asked for: from the rung's stand and from where the
// walk from its own bank arrives, every spawn of the three rungs is reachable once doors count, the ones the pack
// calls "no route" are exactly the ones a door stands in front of, and the route is short and only doors.
func TestRungSpawnsBehindDoorsAreReachedThroughThem(t *testing.T) {
	rd := loadReal(t)
	// Spawns no route reaches even through doors, and why: the Barbarian at 3076,3414 stands in a hut whose only door is
	// the diagonal one at 3077,3415 (a shape-9 wall), which the webwalk graph has no edge for. The trainer passes over
	// it as before (a warning, a minute's pass-over); the other 15 of that rung's 16 spawns are reached.
	unreachable := map[string]map[tile.Tile]string{
		"barbarians": {{X: 3076, Z: 3414}: "a hut behind a diagonal door the graph has no edge for"},
	}
	for _, key := range []string{"alkharid", "whiteknights", "barbarians"} {
		rung := rungByKey(t, key)
		starts := map[string]tile.Tile{
			"the stand": walkableBeside(rd.pack, rung.Stand),
			"the radius-3 arrival at the stand coming from the bank": bankArrival(t, rd, rung),
		}
		legacyBlocked, doorRouted := 0, 0
		for name, start := range starts {
			legacy := newDoorWorld(rd, start, false)
			dw := newDoorWorld(rd, start, true)
			for _, sp := range rung.Spawns {
				want, expectNone := unreachable[key][sp]
				was := ReachFor(legacy.d, sp)
				got := ReachFor(dw.d, sp)
				if !was.OK {
					legacyBlocked++
				}
				switch {
				case expectNone:
					if got.OK {
						t.Errorf("rung %q from %s (%v): the spawn at %v is reachable now (%s) — drop it from the known-unreachable list", key, name, start, sp, want)
					}
				case !got.OK:
					t.Errorf("rung %q from %s (%v): no route to the spawn at %v even through doors: %s", key, name, start, sp, got.Why)
				case got.Doors > 0:
					doorRouted++
					if got.Cost > combatplan.DoorRouteMaxCost || got.First == "" || got.Landing == (tile.Tile{}) {
						t.Errorf("rung %q from %s: door route to %v is %+v", key, name, sp, got)
					}
				case !was.OK:
					t.Errorf("rung %q from %s: %v read no route on the pack and a plain walk with doors on, but no door was crossed (%+v)", key, name, sp, got)
				}
			}
		}
		if legacyBlocked == 0 {
			t.Errorf("rung %q: the baked pack reads every spawn reachable from both starts, so the reproduction does not hold", key)
		}
		if doorRouted == 0 {
			t.Errorf("rung %q: no spawn was reached through a door, so the door route was never exercised", key)
		}
		t.Logf("rung %q: %d spawn readings blocked on the baked pack, %d reached through doors", key, legacyBlocked, doorRouted)
	}
}

// TestAlKharidStandAndItsWestWing pins the exact geometry the live log showed: the warriors at 3282,3176 and
// 3286,3170 stand in the palace's west wing, behind the Large door at 3287,3172, and an account that walked in from
// the west arrives within three tiles of the stand while still in that wing.
func TestAlKharidStandAndItsWestWing(t *testing.T) {
	rd := loadReal(t)
	stand := tile.Tile{X: 3288, Z: 3172}
	west := tile.Tile{X: 3285, Z: 3172}
	if tile.Chebyshev(west, stand) > 3 {
		t.Fatal("fixture: 3285,3172 is not within the stand's radius of 3")
	}
	if rd.pack.ReachableWithin(navPoint(west), navPoint(stand), ApproachRadius).OK {
		t.Fatal("control: the stand is a plain walk from the west wing on the baked pack, so the door is not in the way")
	}
	dw := newDoorWorld(rd, west, true)
	r := ReachFor(dw.d, stand)
	if !r.OK || r.Doors != 1 || !strings.Contains(r.First, "Large door at 3287,317") {
		t.Fatalf("west wing to the stand: %+v", r)
	}
	if r.Landing.X != 3287 || (r.Landing.Z != 3171 && r.Landing.Z != 3172) {
		t.Errorf("the landing is %v, want a doorway tile of the Large door at x 3287", r.Landing)
	}
	// The far wing, past the east door, takes two.
	east := ReachFor(dw.d, tile.Tile{X: 3301, Z: 3174})
	if !east.OK || east.Doors != 2 {
		t.Errorf("west wing to the east wing: %+v (want two doors)", east)
	}
}

// TestACrossedDoorStandsOpenForTheNextCheck is the live-door half: once the server has opened a door, the pack with
// the doors as the server has them (LivePack) calls the room behind it a plain walk away, so nothing crosses it
// again and nothing plans through it. The Barbarian hall's south door 3082,3427 is opened the way open_door does:
// loc_del, then the open leaf one tile out turned a quarter.
func TestACrossedDoorStandsOpenForTheNextCheck(t *testing.T) {
	rd := loadReal(t)
	street := tile.Tile{X: 3082, Z: 3425}
	inside := tile.Tile{X: 3083, Z: 3429}
	dw := newDoorWorld(rd, street, true)
	if r := ReachFor(dw.d, inside); !r.OK || r.Doors != 1 {
		t.Fatalf("control: the hall is not a door away from the street before it is opened: %+v", r)
	}
	if LivePack(dw.d) != dw.d.Pack {
		t.Fatal("control: the live pack differs from the baked one before any door has changed")
	}
	dw.w.locChange(3082, 3427, -1, 0, 3)   // the closed leaf goes
	dw.w.locChange(3082, 3426, 1531, 0, 0) // the open leaf, door_open (0,-1) from a south-facing wall, angle 3+1
	if LivePack(dw.d) == dw.d.Pack {
		t.Fatal("the live pack ignored an opened door")
	}
	if r := ReachFor(dw.d, inside); !r.OK || r.Doors != 0 {
		t.Errorf("with the door open: %+v (want a plain walk)", r)
	}
	// And not for a caller that has not asked: the baked reading stands.
	off := newDoorWorld(rd, street, false)
	off.w.locChange(3082, 3427, -1, 0, 3)
	off.w.locChange(3082, 3426, 1531, 0, 0)
	if r := ReachFor(off.d, inside); r.OK {
		t.Errorf("a caller with Doors off read an opened door: %+v", r)
	}
}

// TestAnOpenedLargeDoorPairIsNoWall plays the palace's double door back the way open_double_doors_right does (both
// leaves go, 1507 and 1511 land a tile west turned a quarter either way) and checks the wing behind it opens up.
func TestAnOpenedLargeDoorPairIsNoWall(t *testing.T) {
	rd := loadReal(t)
	west := tile.Tile{X: 3285, Z: 3172}
	main := tile.Tile{X: 3291, Z: 3168}
	dw := newDoorWorld(rd, west, true)
	if r := ReachFor(dw.d, main); !r.OK || r.Doors == 0 {
		t.Fatalf("control: %+v", r)
	}
	openPalaceDoor(dw.w)
	if r := ReachFor(dw.d, main); !r.OK || r.Doors != 0 {
		t.Errorf("with the Large doors open: %+v (want a plain walk)", r)
	}
}

func openPalaceDoor(w *simWorld) {
	w.locChange(3287, 3172, -1, 0, 0)
	w.locChange(3286, 3172, 1507, 0, 1)
	w.locChange(3287, 3171, -1, 0, 0)
	w.locChange(3286, 3171, 1511, 0, 3)
}

// TestCrossDoorsOpensTheDoorAndLandsBeyondIt runs the whole crossing on the real Walker over a simulated server: the
// route is found, the door is operated, the account ends past it, and the next reading is a plain walk.
func TestCrossDoorsOpensTheDoorAndLandsBeyondIt(t *testing.T) {
	rd := loadReal(t)
	start := tile.Tile{X: 3285, Z: 3172}
	target := tile.Tile{X: 3291, Z: 3168}
	dw := newDoorWorld(rd, start, true)
	dw.c.doors = palaceDoors()
	r := ReachFor(dw.d, target)
	if !r.OK || r.Doors != 1 {
		t.Fatalf("reach: %+v", r)
	}
	if !CrossDoors(dw.d, r) {
		t.Fatalf("CrossDoors failed; log:\n%s", dw.logs())
	}
	if dw.c.ops != 1 || !dw.c.doors[0].opened || !dw.c.doors[1].opened {
		t.Errorf("the door was operated %d times (opened=%v,%v), want once, both leaves", dw.c.ops, dw.c.doors[0].opened, dw.c.doors[1].opened)
	}
	x, z, _, _ := dw.w.st.Tile()
	if (tile.Tile{X: x, Z: z}) != r.Landing {
		t.Errorf("the account stands at %d,%d, want the landing %v", x, z, r.Landing)
	}
	if again := ReachFor(dw.d, target); !again.OK || again.Doors != 0 {
		t.Errorf("after the crossing: %+v (want a plain walk)", again)
	}
	if !strings.Contains(dw.logs(), "door route: crossed 1 door(s)") {
		t.Errorf("no proof line in the log:\n%s", dw.logs())
	}
}

// TestCrossDoorsReportsADoorThatWillNotOpen: a door the server never opens ends the crossing false, in bounded time,
// and says so. Nothing in the world changes, so the next reading is the same door route.
func TestCrossDoorsReportsADoorThatWillNotOpen(t *testing.T) {
	rd := loadReal(t)
	dw := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, true)
	dw.c.doors = []*simDoor{{id: 1506, x: 3287, z: 3172}, {id: 1508, x: 3287, z: 3171}} // no open func: the server ignores the click
	r := ReachFor(dw.d, tile.Tile{X: 3291, Z: 3168})
	if !r.OK || r.Doors != 1 {
		t.Fatalf("reach: %+v", r)
	}
	start := dw.c.now
	if CrossDoors(dw.d, r) {
		t.Fatal("CrossDoors reported a crossing of a door that never opened")
	}
	if took := dw.c.now.Sub(start); took > 2*DoorCrossTimeout {
		t.Errorf("the failed crossing took %s of (simulated) time, past twice its %s bound", took, DoorCrossTimeout)
	}
	if !strings.Contains(dw.logs(), "door route: did not cross 1 door(s)") {
		t.Errorf("the failure is not in the log:\n%s", dw.logs())
	}
}

// TestReachTargetCrossesThenRechecks is FightOne's first step on the simulated palace: a warrior in the main hall seen
// from the west wing is crossed to, the result says how many doors, and a caller with Doors off is refused as before.
func TestReachTargetCrossesThenRechecks(t *testing.T) {
	rd := loadReal(t)
	target := tile.Tile{X: 3291, Z: 3168}
	dw := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, true)
	dw.c.doors = palaceDoors()
	crossed, refused := reachTarget(dw.d, target)
	if refused != nil || crossed != 1 {
		t.Fatalf("reachTarget = %d, %+v; log:\n%s", crossed, refused, dw.logs())
	}

	off := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, false)
	crossed, refused = reachTarget(off.d, target)
	if refused == nil || !refused.Unreachable || refused.DoorFailed || crossed != 0 {
		t.Errorf("with Doors off: %d, %+v (want the plain refusal)", crossed, refused)
	}

	stuck := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, true)
	stuck.c.doors = []*simDoor{{id: 1506, x: 3287, z: 3172}, {id: 1508, x: 3287, z: 3171}}
	_, refused = reachTarget(stuck.d, target)
	if refused == nil || !refused.DoorFailed || !refused.Unreachable {
		t.Errorf("a door that will not open: %+v (want DoorFailed)", refused)
	}
}

// TestWalkNearGoesThroughAShutDoor: the walk the loot and the approach use finds its way through a door it has to
// open, a bounded number of times, and a caller with Doors off still fails with "no path" as it always did.
func TestWalkNearGoesThroughAShutDoor(t *testing.T) {
	rd := loadReal(t)
	dest := tile.Tile{X: 3290, Z: 3169}
	dw := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, true)
	dw.c.doors = palaceDoors()
	// The simulated account teleports to the end of whatever path it is sent, but WalkNear's own clicks go through the
	// session, which sends nowhere: standing at the landing is within the radius, so what is under test is the crossing.
	if !WalkNear(dw.d, dest, 4, 10*time.Second) {
		t.Fatalf("WalkNear did not get through the door; log:\n%s", dw.logs())
	}
	if !dw.c.doors[0].opened {
		t.Error("WalkNear arrived without the door having been opened")
	}
	off := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, false)
	if WalkNear(off.d, dest, 1, 2*time.Second) {
		t.Error("WalkNear with Doors off walked through a shut door")
	}
	if !strings.Contains(off.logs(), "no path from 3285,3172,0") {
		t.Errorf("the old refusal is not in the log:\n%s", off.logs())
	}
}

// TestReachStandCrossesToTheStandsSide: the walled-in recovery. An account on the wrong side of the Large door is walked
// through it; one already on the stand's side is left where it is (the old travel to within two tiles did neither).
func TestReachStandCrossesToTheStandsSide(t *testing.T) {
	rd := loadReal(t)
	stand := tile.Tile{X: 3288, Z: 3172}
	dw := newDoorWorld(rd, tile.Tile{X: 3285, Z: 3172}, true)
	dw.c.doors = palaceDoors()
	if !ReachStand(dw.d, stand, 30*time.Second) || !dw.c.doors[0].opened {
		t.Fatalf("ReachStand from the west wing did not cross; log:\n%s", dw.logs())
	}
	here := newDoorWorld(rd, tile.Tile{X: 3290, Z: 3172}, true)
	here.c.doors = palaceDoors()
	if !ReachStand(here.d, stand, 30*time.Second) || here.c.ops != 0 {
		t.Errorf("ReachStand moved an account that was on the stand's side (ops=%d)", here.c.ops)
	}
}

// TestADoorRouteCostsWhatThePlannerSays prints, for the log of a reader, what the three rungs cost: the figures
// combatplan.DoorRouteMaxCost is set against.
func TestADoorRouteCostsWhatThePlannerSays(t *testing.T) {
	rd := loadReal(t)
	worst := 0
	for _, key := range []string{"alkharid", "whiteknights", "barbarians"} {
		rung := rungByKey(t, key)
		dw := newDoorWorld(rd, walkableBeside(rd.pack, rung.Stand), true)
		var costs []string
		for _, sp := range rung.Spawns {
			if r := ReachFor(dw.d, sp); r.OK && r.Doors > 0 {
				costs = append(costs, fmt.Sprintf("%d,%d:%d/%dd", sp.X, sp.Z, r.Cost, r.Doors))
				worst = max(worst, r.Cost)
			}
		}
		t.Logf("%s from the stand: %s", key, strings.Join(costs, " "))
	}
	if worst >= combatplan.DoorRouteMaxCost {
		t.Errorf("the dearest door route from a stand costs %d, at or past DoorRouteMaxCost %d", worst, combatplan.DoorRouteMaxCost)
	}
}

// TestBankStandsNoWalkReachesAreNotTried: the Falador east bank's booth at 3011,3354 is used from 3011,3353 and 3011,3355,
// and from the street at 3007,3348 only the second has a walk (the first is the keeper's side of the counter). The
// log of 14 October 2026 had "no path from 3007,3348,0 to 3011,3353,0: unreachable" 37 times in 40 minutes.
func TestBankStandsNoWalkReachesAreNotTried(t *testing.T) {
	rd := loadReal(t)
	booth := navPoint(tile.Tile{X: 3011, Z: 3354})
	all := rd.pack.InteractTiles(booth)
	if len(all) != 2 {
		t.Fatalf("fixture: the booth at 3011,3354 is used from %v, want two tiles", all)
	}
	dw := newDoorWorld(rd, tile.Tile{X: 3007, Z: 3348}, false)
	got := reachableStands(dw.d, all)
	if len(got) != 1 {
		t.Fatalf("reachable stands from the street = %v out of %v, want exactly one", got, all)
	}
	if got[0] != (pathfind.NavPoint{X: 3011, Z: 3355}) {
		t.Errorf("the reachable stand is %v, want the customer's side 3011,3355", got[0])
	}
	// A loc the pack calls walled in leaves the old approach its tiles.
	if len(reachableStands(newDoorWorld(rd, tile.Tile{X: 3007, Z: 3348}, false).d, []pathfind.NavPoint{{X: 3011, Z: 3353}})) != 1 {
		t.Error("with nothing reachable the list was emptied")
	}
}

// TestTheBarbarianStandIsReachedFromTheStreetToo: the rung's stand 3080,3427 is not a tile anyone can stand on (a loc in
// the hall's south wall row), and the pack reaches it from the street as well as from inside. So a walk to within three
// tiles of it can end outside the hall, and then the barbarians in the hall (the log's top "no route" targets: 3082,3427,
// 3081,3428, 3083,3427) are a door away while the stand reads as reached: only the door-aware pick gets them.
func TestTheBarbarianStandIsReachedFromTheStreetToo(t *testing.T) {
	rd := loadReal(t)
	rung := rungByKey(t, "barbarians")
	if rd.pack.Walkable(rung.Stand.X, rung.Stand.Z, rung.Stand.Level) {
		t.Fatal("fixture: the Barbarian stand is walkable, so this is not the case under test")
	}
	street := newDoorWorld(rd, tile.Tile{X: 3082, Z: 3424}, true)
	if r := ReachFor(street.d, rung.Stand); !r.OK || r.Doors != 0 {
		t.Fatalf("from the street to the stand: %+v (want a plain walk: the stand is reached from outside)", r)
	}
	for _, in := range []tile.Tile{{X: 3081, Z: 3428}, {X: 3083, Z: 3428}} {
		r := ReachFor(street.d, in)
		if !r.OK || r.Doors != 1 || !strings.Contains(r.First, "Door at 3082,3427") {
			t.Errorf("from the street to the barbarian at %v inside the hall: %+v (want one door, the hall's south door)", in, r)
		}
	}
}
