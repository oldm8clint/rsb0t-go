package randomevents

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
)

// TestFleeWaypointsDropsTheOriginTile pins walkFleeLeg's origin drop so nobody "fixes" it back.
//
// The lab engine's MoveClickHandler (clientRoutefinder on) queues EVERY waypoint the client sends,
// path[0] included, and walks them in order. Flee routes are planned from the tile the client last
// saw, and a hit account has often already stepped on by the time the click lands, so a route that
// still began with that tile walked it BACK there before fleeing. Dropping path[0] is safe only
// because the engine's other check on path[0] -- it rejects the whole click when path[0] is more
// than 104 tiles from the player -- can never fire on a flee: localEscapeAvoiding confines every
// route to 96 tiles of origin, so the first kept waypoint is always inside the limit.
func TestFleeWaypointsDropsTheOriginTile(t *testing.T) {
	const engineStartLimit = 104 // MoveClickHandler: path[0] farther than this from the player is refused
	const fleeWindow = 96        // localEscapeAvoiding's localAvoid radius
	if fleeWindow >= engineStartLimit {
		t.Fatalf("flee window %d must stay inside the engine's %d-tile start limit", fleeWindow, engineStartLimit)
	}
	origin := pathfind.NavPoint{X: 3200, Z: 3200}
	route := []pathfind.NavPoint{origin, {X: 3205, Z: 3200}, {X: 3205, Z: 3210}}
	got := fleeWaypoints(route)
	if len(got) != 2 || got[0] != route[1] || got[1] != route[2] {
		t.Fatalf("fleeWaypoints(%v) = %v, want the route without its origin", route, got)
	}
	for _, p := range got {
		if chebyshev(p.X, p.Z, origin.X, origin.Z) > fleeWindow {
			t.Fatalf("waypoint %v outside the flee window", p)
		}
	}
	// A one-tile route is kept: sending nothing would not move the account at all.
	one := []pathfind.NavPoint{{X: 3201, Z: 3200}}
	if got := fleeWaypoints(one); len(got) != 1 || got[0] != one[0] {
		t.Fatalf("fleeWaypoints(%v) = %v, want it unchanged", one, got)
	}
}

// TestWaterfallFireGiantRoomHasNoFleeButHasTheDoorBack reproduces lane Q's Waterfall death
// (2026-09-24, run 11) on the real pack: shut in the fire-giant room with the giant at 2564,9882,
// the ordinary 15-tile flee finds nothing (the control -- this is why the account stood still for
// thirty-five retries), while the Large door the account came in by is within fleeDoorRadius.
//
// Needs RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG.
func TestWaterfallFireGiantRoomHasNoFleeButHasTheDoorBack(t *testing.T) {
	pack := realCollisionPack(t)
	ixPath, cfgPath := os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if ixPath == "" || cfgPath == "" {
		t.Skip("set RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	ix, err := locindex.Load(ixPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cachecfg.LoadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	const threatX, threatZ = 2564, 9882
	// The account's tile: just inside the door it crossed, beside the giant.
	var origin pathfind.NavPoint
	found := false
	for _, c := range []pathfind.NavPoint{{X: 2565, Z: 9880}, {X: 2564, Z: 9880}, {X: 2566, Z: 9880}, {X: 2565, Z: 9879}} {
		if pack.Walkable(c.X, c.Z, 0) {
			origin, found = c, true
			break
		}
	}
	if !found {
		t.Fatal("no walkable tile just inside the fire-giant room's door")
	}
	t.Logf("account at %d,%d", origin.X, origin.Z)

	// Control: the flee the account actually ran finds nothing.
	if dest, ok := localEscapeAvoiding(pack, origin, threatX, threatZ, 15, nil, origin); ok {
		t.Fatalf("control: expected no 15-tile flee from the shut room, got %v -- the reproduction no longer holds", dest)
	}

	// The fallback: the door the account came through is right there.
	m, ok := fleeDoorCandidate(ix, cfg, nil, origin.X, origin.Z, 0)
	if !ok {
		t.Fatal("no door within fleeDoorRadius of the account")
	}
	l, _ := cfg.Loc(m.ID)
	t.Logf("door back: %s id %d at %d,%d op %d", l.Name, m.ID, m.X, m.Z, m.Op)
	if m.ID != 1516 && m.ID != 1519 {
		t.Errorf("door id %d, want the Large door pair 1516/1519", m.ID)
	}
	if !strings.Contains(strings.ToLower(l.Name), "door") {
		t.Errorf("door name %q", l.Name)
	}

	// The next rung: a shorter flee inside the room. Reported, and required to stay in the room.
	if plan, _, ok := newFleePlanner(pack, origin, threatX, threatZ, nil, origin).shorter(15); ok {
		dest := plan.Dest
		t.Logf("shorter flee: %d,%d (%d from the giant)", dest.X, dest.Z, chebyshev(dest.X, dest.Z, threatX, threatZ))
		if dest.X < 2564 || dest.X > 2581 || dest.Z < 9861 || dest.Z > 9882 {
			t.Errorf("shorter flee %v left the shut room", dest)
		}
	} else {
		t.Log("shorter flee: none; the account falls through to eat-or-fight-back")
	}
}

// holdView is a cornered account at 20/30 hitpoints, combat 30, holding a trout and a poisoned
// chalice, attacked by a level-20 guard. Each test changes one thing.
func holdView() holdGroundView {
	return holdGroundView{
		hp: 20, max: 30, hpKnown: true,
		food: []holdGroundFood{
			{id: 1, slot: 0, name: "Poison chalice", op: "Eat"},
			{id: 333, slot: 1, name: "Trout", op: "Eat"},
		},
		attacker:     gamestate.NPC{Index: 7, Type: 9},
		attackerNpc:  cachecfg.Npc{ID: 9, Name: "Guard", Level: 20, Ops: [5]string{"", "Attack"}},
		haveAttacker: true,
		ownLevel:     30, ownKnown: true,
	}
}

func TestHoldGroundFightsOnlyWhatItCanBeat(t *testing.T) {
	v := holdView()
	if c := holdGroundChoice(v); c.kind != "fight" || c.op != 2 {
		t.Fatalf("level 20 against combat 30 = %+v, want fight with op 2", c)
	}
	v.attackerNpc.Level = 64
	if c := holdGroundChoice(v); c.kind != "" {
		t.Fatalf("a level-64 wolf against combat 30 = %+v, want nothing", c)
	}
	v = holdView()
	v.ownKnown = false
	if c := holdGroundChoice(v); c.kind != "" {
		t.Fatalf("own level unknown = %+v, want nothing", c)
	}
}

func TestHoldGroundNeverAttacksAnEventMonster(t *testing.T) {
	var eventType int
	for id := range hostileEventNpcIDs {
		eventType = id
		break
	}
	v := holdView()
	v.attacker.Type, v.attackerNpc.ID, v.attackerNpc.Level = eventType, eventType, 1
	if c := holdGroundChoice(v); c.kind != "" {
		t.Fatalf("event npc %d = %+v, want nothing", eventType, c)
	}
}

func TestHoldGroundEatsRealFoodButNotDuringADialogue(t *testing.T) {
	v := holdView()
	v.hp = 10
	if c := holdGroundChoice(v); c.kind != "eat" || c.food.name != "Trout" {
		t.Fatalf("hurt = %+v, want the trout, never the poison", c)
	}
	v.dialogueOpen = true
	if c := holdGroundChoice(v); c.kind == "eat" {
		t.Fatalf("ate with a dialogue open: %+v", c)
	}
}

type fleeLocTypes map[int]cachecfg.Loc

func (f fleeLocTypes) Loc(id int) (cachecfg.Loc, bool) { v, ok := f[id]; return v, ok }

func TestFleeDoorCandidateSkipsTrapdoorsAndDanger(t *testing.T) {
	types := fleeLocTypes{
		1: {ID: 1, Name: "Door", Width: 1, Length: 1, Ops: [5]string{"Open"}},
		2: {ID: 2, Name: "Trapdoor", Width: 1, Length: 1, Ops: [5]string{"Open"}},
	}
	ix, err := locindex.New([]locindex.Loc{
		{ID: 2, X: 3200, Z: 3200, Level: 0},
		{ID: 1, X: 3202, Z: 3200, Level: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := fleeDoorCandidate(ix, types, nil, 3200, 3200, 0)
	if !ok || m.ID != 1 {
		t.Fatalf("got %+v %v, want the Door two tiles away, never the Trapdoor underfoot", m, ok)
	}
	// White Wolf Mountain: a door there is never a way out.
	wolfIx, _ := locindex.New([]locindex.Loc{{ID: 1, X: 2842, Z: 3451, Level: 0}})
	if m, ok := fleeDoorCandidate(wolfIx, types, nil, 2842, 3450, 0); ok {
		t.Fatalf("picked a door in wolf territory: %+v", m)
	}
}

func TestFleeDoorIsNotReusedWithinItsMemory(t *testing.T) {
	var s overlapSession
	if recentlyFledThrough(&s, 1, 2, 0) {
		t.Fatal("remembered a door never used")
	}
	noteFledThrough(&s, 1, 2, 0)
	if !recentlyFledThrough(&s, 1, 2, 0) {
		t.Fatal("the door just fled through is usable again at once")
	}
	if recentlyFledThrough(&s, 5, 2, 0) {
		t.Fatal("a different door was blocked")
	}
	fledDoors.Store(&s, fledDoor{1, 2, 0, time.Now().Add(-2 * fleeDoorMemory)})
	if recentlyFledThrough(&s, 1, 2, 0) {
		t.Fatal("the memory never expires")
	}
}

// TestFleeStopsAfterARespawnOrTeleport pins fleeRelocated: an ordinary flee leg is still the same
// flee, a respawn from the Waterfall dungeon to Lumbridge or a level change is not.
func TestFleeStopsAfterARespawnOrTeleport(t *testing.T) {
	if fleeRelocated(2566, 9879, 0, 2580, 9870, 0) {
		t.Fatal("a 14-tile flee leg counted as relocated")
	}
	if fleeRelocated(3200, 3200, 0, 3266, 3200, 0) {
		t.Fatal("the furthest a 20s running flee can reach (66 tiles) counted as relocated")
	}
	if !fleeRelocated(2566, 9879, 0, 3222, 3218, 0) {
		t.Fatal("a respawn at Lumbridge is still the same flee")
	}
	if !fleeRelocated(3200, 3200, 0, 3200, 3200, 1) {
		t.Fatal("a level change is still the same flee")
	}
}
