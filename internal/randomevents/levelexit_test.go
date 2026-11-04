package randomevents

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/clientprot"
)

// The Wizards' Tower on the real map, where a ScarabSwarm account idled in the top room (3103,3162 level 2) after
// Imp Catcher and a Swarm event monster killed it in nine minutes fifty-one seconds, forty "cornered ... fighting
// back" passes at food 0 (17 Oct 2026 08:34). The real-pack tests need RSB0T_COLLISION_PACK.

// towerExits are the tower's hops as webwalk's graph holds them (the graph is checked against these in
// internal/webwalk/levelexits_test.go): the staircase down from the top room, the two stands of the staircase down
// from the middle floor, and the staircase up from the ground floor.
var towerExits = []LevelExit{
	{Stand: StrandTile{3104, 3161, 2}, Land: StrandTile{3103, 3158, 1}, LocID: 1740, LocX: 3104, LocZ: 3160, Label: "Climb-down"},
	{Stand: StrandTile{3103, 3161, 1}, Land: StrandTile{3105, 3160, 0}, LocID: 1739, LocX: 3103, LocZ: 3159, Label: "Climb-down"},
	{Stand: StrandTile{3105, 3160, 1}, Land: StrandTile{3102, 3159, 0}, LocID: 1739, LocX: 3103, LocZ: 3159, Label: "Climb-down"},
	{Stand: StrandTile{3103, 3161, 1}, Land: StrandTile{3104, 3161, 2}, LocID: 1739, LocX: 3103, LocZ: 3159, Label: "Climb-up"},
	{Stand: StrandTile{3105, 3160, 0}, Land: StrandTile{3105, 3160, 1}, LocID: 1738, LocX: 3103, LocZ: 3159, Label: "Climb-up"},
}

// towerRooms are accounts in the three rooms, each with a Swarm one tile away. The top room is 33 tiles
// (3103-3107 by 3156-3165), the middle one 31.
var towerRooms = []struct {
	name   string
	origin pathfind.NavPoint
	swarm  pathfind.NavPoint
	want   int // the staircase's loc id
	steps  int // at most
}{
	{"the top room, where the Imp Catcher module leaves the account", pathfind.NavPoint{X: 3103, Z: 3162, Level: 2}, pathfind.NavPoint{X: 3103, Z: 3163, Level: 2}, 1740, 5},
	{"the top room, on the quest's other tile", pathfind.NavPoint{X: 3104, Z: 3163, Level: 2}, pathfind.NavPoint{X: 3104, Z: 3164, Level: 2}, 1740, 5},
	{"the top room, in its far corner", pathfind.NavPoint{X: 3107, Z: 3157, Level: 2}, pathfind.NavPoint{X: 3106, Z: 3157, Level: 2}, 1740, 8},
	{"the middle floor", pathfind.NavPoint{X: 3104, Z: 3162, Level: 1}, pathfind.NavPoint{X: 3105, Z: 3162, Level: 1}, 1739, 5},
}

// stateAt is an account at origin with an event monster of monsterType at monster (a Swarm facing it).
func towerScene(t *testing.T, origin pathfind.NavPoint, monsterType int, monster pathfind.NavPoint) *overlapSession {
	t.Helper()
	st := overlapState()
	s := &overlapSession{state: st}
	setExactTile(st, origin.X, origin.Z, origin.Level)
	if x, z, l, _ := st.Tile(); x != origin.X || z != origin.Z || l != origin.Level {
		t.Fatalf("the scene put the account at %d,%d,%d, not %v", x, z, l, origin)
	}
	addFacingNPCs(st, facingNPC{npcSpec{77, monsterType, monster.X - origin.X, monster.Z - origin.Z}, playerFaceBase + testSelfSlot})
	return s
}

func TestWizardsTowerRoomsHaveNoLeashTileButAStaircase(t *testing.T) {
	pack := realCollisionPack(t)
	for _, c := range towerRooms {
		t.Run(c.name, func(t *testing.T) {
			// The reproduction: no tile of the floor is far enough from the Swarm, so the leash walk (and with it the
			// whole of HandleEvade's plan before the cornered path) finds nothing.
			breakDist := leashBreakFor(swarmLeash, 1)
			if res, ok := leashPlan(pack, c.origin, c.swarm.X, c.swarm.Z, c.swarm.X, c.swarm.Z, breakDist, nil, nil); ok {
				t.Fatalf("control: the leash walk found %v from %v, the reproduction no longer holds", res.Dest, c.origin)
			}
			plan, ok := planLevelExit(pack, c.origin, towerExits, c.swarm.X, c.swarm.Z, breakDist, nil)
			if !ok {
				t.Fatalf("no level exit from %v", c.origin)
			}
			if plan.Exit.LocID != c.want || plan.Exit.Label != "Climb-down" {
				t.Fatalf("the exit is %s on loc %d, want Climb-down on loc %d", plan.Exit.Label, plan.Exit.LocID, c.want)
			}
			if plan.Steps > c.steps {
				t.Fatalf("the stand is %d steps away, want at most %d", plan.Steps, c.steps)
			}
			if plan.Route[0] != c.origin {
				t.Fatalf("the route starts at %v, not the account's tile %v", plan.Route[0], c.origin)
			}
			stand := pathfind.NavPoint{X: plan.Exit.Stand.X, Z: plan.Exit.Stand.Z, Level: plan.Exit.Stand.Level}
			if last := plan.Route[len(plan.Route)-1]; last != stand {
				t.Fatalf("the route ends at %v, not the stand %v", last, stand)
			}
			routeWalkable(t, pack, plan.Route)
			t.Logf("%s: %d steps to %v, then %s on %d", c.name, plan.Steps, stand, plan.Exit.Label, plan.Exit.LocID)
		})
	}
}

// Going UP is a way out too, but only when there is no way down: the top room's staircase is the down one, and the
// ground floor, with no stair down, goes up.
func TestLevelExitPrefersDownAndFallsBackToUp(t *testing.T) {
	pack := realCollisionPack(t)
	both := []LevelExit{
		{Stand: StrandTile{3103, 3161, 1}, Land: StrandTile{3104, 3161, 2}, LocID: 1739, LocX: 3103, LocZ: 3159, Label: "Climb-up"},
		{Stand: StrandTile{3105, 3160, 1}, Land: StrandTile{3102, 3159, 0}, LocID: 1739, LocX: 3103, LocZ: 3159, Label: "Climb-down"},
	}
	// The account stands on the up stair's stand (0 steps) and the down stair's is 3 away: down still wins, the
	// up stair costing the building's depth in penalty.
	origin := pathfind.NavPoint{X: 3103, Z: 3161, Level: 1}
	plan, ok := planLevelExit(pack, origin, both, 3105, 3161, leashBreakFor(swarmLeash, 1), nil)
	if !ok || plan.Exit.Label != "Climb-down" {
		t.Fatalf("plan = %+v ok %v, want the way down", plan.Exit, ok)
	}
	plan, ok = planLevelExit(pack, origin, both[:1], 3105, 3161, leashBreakFor(swarmLeash, 1), nil)
	if !ok || plan.Exit.Label != "Climb-up" {
		t.Fatalf("with only the way up: plan = %+v ok %v", plan.Exit, ok)
	}
	// An account on the stand needs no walk.
	stand := pathfind.NavPoint{X: 3103, Z: 3161, Level: 1}
	if plan, ok = planLevelExit(pack, stand, both[:1], 3104, 3161, leashBreakFor(swarmLeash, 1), nil); !ok || len(plan.Route) != 1 || plan.Steps != 0 {
		t.Fatalf("on the stand: plan %+v ok %v, want a one-point route", plan, ok)
	}
}

func TestLevelExitRefusesLandingsThatAreNoWayOut(t *testing.T) {
	cases := []struct {
		name  string
		e     LevelExit
		spawn [2]int
		want  bool
	}{
		{"another level", LevelExit{Stand: StrandTile{3104, 3161, 2}, Land: StrandTile{3103, 3158, 1}}, [2]int{3103, 3163}, true},
		{"the same level, a few tiles on", LevelExit{Stand: StrandTile{3104, 3161, 0}, Land: StrandTile{3110, 3161, 0}}, [2]int{3103, 3163}, false},
		{"the same level, past the leash (a ladder out of a dungeon)", LevelExit{Stand: StrandTile{3104, 9576, 0}, Land: StrandTile{3104, 3163, 0}}, [2]int{3104, 9577}, true},
		{"into the Wilderness from outside it (the Mage Bank's ladder)", LevelExit{Stand: StrandTile{2539, 4712, 0}, Land: StrandTile{3090, 3956, 0}}, [2]int{2539, 4713}, false},
		{"within the Wilderness, to a shallower tile", LevelExit{Stand: StrandTile{3100, 3900, 0}, Land: StrandTile{3100, 3600, 1}}, [2]int{3100, 3901}, true},
		{"into White Wolf Mountain's den", LevelExit{Stand: StrandTile{2870, 3430, 1}, Land: StrandTile{2860, 3475, 0}}, [2]int{2870, 3431}, false},
	}
	for _, c := range cases {
		if got := exitLeavesMonster(c.e, c.spawn[0], c.spawn[1], leashBreakFor(swarmLeash, 1)); got != c.want {
			t.Errorf("%s: exitLeavesMonster = %v, want %v", c.name, got, c.want)
		}
	}
}

// locResolver is foodResolver with loc options, which the exit reads its option number from.
type locResolver struct {
	foodResolver
	locOps map[int][5]string
}

func (l locResolver) Loc(id int) (cachecfg.Loc, bool) {
	ops, ok := l.locOps[id]
	return cachecfg.Loc{ID: id, Name: "Staircase", Ops: ops}, ok
}

// HandleEvade end to end on the real map: the Swarm in the top room, the account without food, the leash walk
// finding nothing. It walks to the stand, climbs down (the fake server carries the account to the landing on the
// click), and never attacks.
func TestHandleEvadeTakesTheStaircaseOutOfTheTowerTopRoom(t *testing.T) {
	pack := realCollisionPack(t)
	withResolver(t, locResolver{
		foodResolver: foodResolver{fakeResolver: fakeResolver{objNames: map[int]string{365: "Bass"}}, objOps: map[int][5]string{365: {"Eat"}}},
		locOps:       map[int][5]string{1740: {"Climb-down"}},
	})
	SetLevelExitSource(func() []LevelExit { return towerExits })
	t.Cleanup(func() { SetLevelExitSource(nil) })
	shortenLevelExitClocks(t)

	origin := pathfind.NavPoint{X: 3103, Z: 3162, Level: 2}
	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3103, Z: 3163, Level: 2})
	g := NewGuardian(s, pack)
	t.Cleanup(g.unregister)
	setHP(s.state, 54, 54)
	underAttackBy(s, 77, swarmNpcType)

	walks, climbs, attacks := 0, 0, 0
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			walks++
			setExactTile(s.state, 3104, 3161, 2) // the walk arrives at the stand
		case clientprot.OPLOC1:
			climbs++
			setExactTile(s.state, 3103, 3158, 1) // the staircase carries the account down
		case clientprot.OPNPC2:
			attacks++
		}
	}
	ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 77, NpcType: swarmNpcType, ThreatX: 3103, ThreatZ: 3163}
	var logs []string
	start := time.Now()
	if err := HandleEvade(s, pack, ev, 1, func(l string) { logs = append(logs, l) }); err != nil {
		t.Fatalf("HandleEvade: %v\n%s", err, strings.Join(logs, "\n"))
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
	if _, _, l, _ := s.state.Tile(); l != 1 {
		t.Fatalf("the account is on level %d, want 1\n%s", l, strings.Join(logs, "\n"))
	}
	if walks != 1 || climbs != 1 || attacks != 0 {
		t.Fatalf("walks %d climbs %d attacks %d, want 1, 1 and 0\n%s", walks, climbs, attacks, strings.Join(logs, "\n"))
	}
	if g.cornered() {
		t.Fatal("the cornered window was opened")
	}
	joined := strings.Join(logs, "\n")
	for _, want := range []string{"outside the leash of event monster", "level exit: Climb-down on loc 1740 at 3104,3160", "evade: level exit: left 3103,3162,2 for 3103,3158,1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no log line with %q:\n%s", want, joined)
		}
	}
}

// A level exit that does not work is given up after its clicks, and the handler goes on to the cornered path (here,
// no food: no attack either).
func TestHandleEvadeGivesUpAStaircaseThatDoesNothing(t *testing.T) {
	pack := realCollisionPack(t)
	withResolver(t, locResolver{
		foodResolver: foodResolver{fakeResolver: fakeResolver{objNames: map[int]string{365: "Bass"}}, objOps: map[int][5]string{365: {"Eat"}}},
		locOps:       map[int][5]string{1740: {"Climb-down"}},
	})
	SetLevelExitSource(func() []LevelExit { return towerExits })
	t.Cleanup(func() { SetLevelExitSource(nil) })
	shortenLevelExitClocks(t)

	origin := pathfind.NavPoint{X: 3104, Z: 3161, Level: 2} // already on the stand
	s := towerScene(t, origin, swarmNpcType, pathfind.NavPoint{X: 3104, Z: 3162, Level: 2})
	g := NewGuardian(s, pack)
	t.Cleanup(g.unregister)
	setHP(s.state, 54, 54)
	underAttackBy(s, 77, swarmNpcType)
	climbs, attacks := 0, 0
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.OPLOC1:
			climbs++
		case clientprot.OPNPC2:
			attacks++
		}
	}
	ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 77, NpcType: swarmNpcType, ThreatX: 3104, ThreatZ: 3162}
	if _, found, _ := evadeEscape(pack, origin, ev.ThreatX, ev.ThreatZ, 1, safeRefFor(s)); found {
		t.Fatal("control: the ring search finds a way out of the top room, so the cornered path this test reaches is not the live one")
	}
	var logs []string
	_ = HandleEvade(s, pack, ev, 1, func(l string) { logs = append(logs, l) })
	if climbs != levelExitClicks {
		t.Fatalf("clicked the staircase %d times, want %d\n%s", climbs, levelExitClicks, strings.Join(logs, "\n"))
	}
	if attacks != 0 {
		t.Fatalf("attacked the Swarm %d times with nothing to eat\n%s", attacks, strings.Join(logs, "\n"))
	}
	if !g.cornered() {
		t.Fatalf("the cornered window was not opened after the exit failed\n%s", strings.Join(logs, "\n"))
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "nothing to eat: not attacking it") {
		t.Fatalf("no 'nothing to eat' line:\n%s", joined)
	}
}

// With food the cornered fight-back is as it was; at food 0 it is not made. The Swarm is the monster that
// corneredCanFight allows.
func TestACorneredAccountWithNothingToEatDoesNotAttack(t *testing.T) {
	const bass = 365
	withResolver(t, foodResolver{fakeResolver: fakeResolver{objNames: map[int]string{bass: "Bass"}}, objOps: map[int][5]string{bass: {"Eat"}}})
	for _, c := range []struct {
		name    string
		food    bool
		attacks int
	}{{"with food", true, 1}, {"without", false, 0}} {
		t.Run(c.name, func(t *testing.T) {
			s := combatSession()
			g := NewGuardian(s, nil)
			t.Cleanup(g.unregister)
			if c.food {
				setFullInventory(s.state, 3214, [2]int32{bass, 4})
			}
			addNPCFacing(s.state, 77, swarmNpcType, 1, 0, playerFaceBase+testSelfSlot)
			ev := &Event{Kind: KindEvade, Name: "event monster", NpcIndex: 77, NpcType: swarmNpcType}
			attacks := 0
			s.onSend = func(op byte) {
				if int(op) == clientprot.OPNPC2 {
					attacks++
				}
			}
			var said []string
			if !fightBackCornered(s, ev, func(l string) { said = append(said, l) }) {
				t.Fatal("fightBackCornered did not hand the account over")
			}
			if attacks != c.attacks {
				t.Fatalf("attacks = %d, want %d (%v)", attacks, c.attacks, said)
			}
			if !g.cornered() {
				t.Fatal("no window: the script is not released to eat or break off")
			}
		})
	}
}

// shortenLevelExitClocks makes the execution's waits short for a test and restores them.
func shortenLevelExitClocks(t *testing.T) {
	t.Helper()
	poll, walk, click, clicks := levelExitPoll, levelExitWalkWait, levelExitClickWait, levelExitClicks
	levelExitPoll, levelExitWalkWait, levelExitClickWait = 5*time.Millisecond, 500*time.Millisecond, 60*time.Millisecond
	t.Cleanup(func() {
		levelExitPoll, levelExitWalkWait, levelExitClickWait, levelExitClicks = poll, walk, click, clicks
	})
}

// The walking flee takes the staircase too, for an attacker that is no event monster: a Jail guard, a ghost, anything that hits
// a room whose only door is shut or guarded. The Swarm test above goes through HandleEvade; this one through the flee every
// "in combat with no tracked event" and every EmergencyFlee runs.
func TestTheWalkingFleeTakesTheStaircaseOutOfAnUpperRoom(t *testing.T) {
	pack := realCollisionPack(t)
	withResolver(t, locResolver{
		foodResolver: foodResolver{fakeResolver: fakeResolver{objNames: map[int]string{365: "Bass"}}, objOps: map[int][5]string{365: {"Eat"}}},
		locOps:       map[int][5]string{1740: {"Climb-down"}},
	})
	SetLevelExitSource(func() []LevelExit { return towerExits })
	t.Cleanup(func() { SetLevelExitSource(nil) })
	shortenLevelExitClocks(t)

	const ghostType = 1234
	origin := pathfind.NavPoint{X: 3105, Z: 3158, Level: 2}
	s := towerScene(t, origin, ghostType, pathfind.NavPoint{X: 3105, Z: 3159, Level: 2})
	g := NewGuardian(s, pack)
	t.Cleanup(g.unregister)
	setHP(s.state, 40, 54)
	underAttackBy(s, 77, ghostType)
	climbs := 0
	s.onSend = func(op byte) {
		switch int(op) {
		case clientprot.MOVE_GAMECLICK:
			setExactTile(s.state, 3104, 3161, 2)
		case clientprot.OPLOC1:
			climbs++
			setExactTile(s.state, 3103, 3158, 1)
		}
	}
	var logs []string
	stuck, err := fleeWalkingFrom(s, pack, 20, 1, func(l string) { logs = append(logs, l) })
	if err != nil || stuck {
		t.Fatalf("stuck %v err %v\n%s", stuck, err, strings.Join(logs, "\n"))
	}
	if _, _, l, _ := s.state.Tile(); l != 1 || climbs != 1 {
		t.Fatalf("level %d after %d climbs\n%s", l, climbs, strings.Join(logs, "\n"))
	}
	if joined := strings.Join(logs, "\n"); !strings.Contains(joined, "nowhere to walk on this floor — level exit: Climb-down on loc 1740") {
		t.Fatalf("no level exit line:\n%s", joined)
	}
}
