package bot

// The CombatTrainer's pick of a target when the monsters stand in rooms (combatplan/doors.go): on the real Al Kharid
// palace, the account in its west wing and warriors on both sides of the Large door. Live, 14 October 2026,
// 04:05-04:45, such accounts logged "no route from here to the Al-Kharid warrior" 287 times and waited for "one we
// can reach" 166 times with the monsters a door away.

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/webwalk"
)

var doorTestData struct {
	once    sync.Once
	pk      *pathfind.Pack
	ix      *locindex.Index
	cfg     *cachecfg.Config
	planner *webwalk.Planner
	err     error
}

func loadDoorTestData(t *testing.T) {
	t.Helper()
	pp, lp, cp := os.Getenv("RSB0T_COLLISION_PACK"), os.Getenv("RSB0T_LOC_INDEX"), os.Getenv("RSB0T_CONFIG_JAG")
	if pp == "" || lp == "" || cp == "" {
		t.Skip("set RSB0T_COLLISION_PACK, RSB0T_LOC_INDEX and RSB0T_CONFIG_JAG")
	}
	doorTestData.once.Do(func() {
		var err error
		if doorTestData.pk, err = pathfind.Load(pp); err != nil {
			doorTestData.err = err
			return
		}
		if doorTestData.ix, err = locindex.Load(lp); err != nil {
			doorTestData.err = err
			return
		}
		if doorTestData.cfg, err = cachecfg.LoadFile(cp); err != nil {
			doorTestData.err = err
			return
		}
		g, err := webwalk.Default()
		if err != nil {
			doorTestData.err = err
			return
		}
		doorTestData.planner = webwalk.NewPlanner(doorTestData.pk, g)
	})
	if doorTestData.err != nil {
		t.Fatal(doorTestData.err)
	}
}

// planOnlyClient is the webwalk.Client of a walker that only plans: the tile is the session's, nothing moves, and a
// walk that is tried runs out its (simulated) timeout instead of hanging.
type planOnlyClient struct {
	st  *gamestate.State
	now time.Time
}

func (c *planOnlyClient) Tile() (webwalk.Point, bool) {
	x, z, level, ok := c.st.Tile()
	return webwalk.Point{X: x, Z: z, Level: level}, ok
}
func (*planOnlyClient) Walk([]webwalk.Point, bool) error             { return nil }
func (*planOnlyClient) FindLocs(webwalk.LocQuery) []webwalk.LocMatch { return nil }
func (*planOnlyClient) OpLoc(int, int, int, int) error               { return nil }
func (*planOnlyClient) CantReachSince(time.Time) bool                { return false }
func (c *planOnlyClient) Delay(d time.Duration) bool                 { c.now = c.now.Add(d); return true }
func (c *planOnlyClient) Now() time.Time                             { return c.now }

// doorNPC places NPCs of one type on absolute tiles within 15 of the player in one NPC_INFO.
func placeDoorNPCs(st *gamestate.State, typeID, playerX, playerZ int, tiles ...[3]int) {
	fields := [][2]uint32{{8, 0}}
	for _, n := range tiles {
		fields = append(fields, [2]uint32{14, uint32(n[0])}, [2]uint32{11, uint32(typeID)},
			[2]uint32{5, uint32((n[1] - playerX) & 0x1f)}, [2]uint32{5, uint32((n[2] - playerZ) & 0x1f)},
			[2]uint32{1, 0}, [2]uint32{1, 0})
	}
	fields = append(fields, [2]uint32{14, 16383})
	st.Handle(serverprot.NPC_INFO, bits(fields...))
}

// doorTrainer is a CombatTrainer on the Al Kharid rung, its account at x,z, with the real map under it.
func doorTrainer(t *testing.T, x, z int) (*combatTrainerBot, *Context, *combatrunner.Deps, *apiSession, *[]string) {
	t.Helper()
	loadDoorTestData(t)
	sess := newAPISession()
	rebuildAt(sess.st, x/8, z/8)
	placeAt(sess.st, 0, x-(x/8-6)*8, z-(z/8-6)*8)
	var logged []string
	ctx := apiContext(sess, func(s string) { logged = append(logged, s) })
	rung, ok := combatplan.RungByKey(combatplan.MeleeLadder(), "alkharid")
	if !ok {
		t.Fatal("the ladder has no alkharid rung")
	}
	b := &combatTrainerBot{
		rung: rung, haveRung: true,
		warnedAt:    map[string]time.Time{},
		unreachable: map[int]time.Time{},
		said:        map[string]bool{},
	}
	d := &combatrunner.Deps{
		Session: sess, Config: doorTestData.cfg, Pack: doorTestData.pk, Locs: doorTestData.ix,
		Walker: webwalk.NewWalker(doorTestData.planner, &planOnlyClient{st: sess.st, now: time.Unix(1_000_000, 0)}),
		Delay:  func(time.Duration) bool { return true },
		Doors:  true,
	}
	return b, ctx, d, sess, &logged
}

func warriorType(t *testing.T) int {
	t.Helper()
	ids := doorTestData.cfg.NpcIDs("Al-Kharid warrior")
	if len(ids) == 0 || ids[0] > 2047 {
		t.Fatalf("Al-Kharid warrior ids %v", ids)
	}
	return ids[0]
}

// A monster on this side of every door beats a nearer one behind a door; with none on this side, the one behind the
// door is taken, and says so.
func TestPickTargetTakesThisSideOfTheDoorsFirst(t *testing.T) {
	b, ctx, d, sess, logged := doorTrainer(t, 3285, 3172)
	typ := warriorType(t)
	// The far wing's warrior at 3288,3173 is nearer in a straight line than the west wing's at 3283,3176 and a
	// Large door away; the west one is a walk.
	placeDoorNPCs(sess.st, typ, 3285, 3172, [3]int{101, 3288, 3173}, [3]int{102, 3283, 3176})
	n, ok := b.pickTarget(ctx, d)
	if !ok || n.Index != 102 || b.pickedDoors != 0 {
		t.Fatalf("picked %+v (ok=%v, doors=%d), want the west wing's warrior 102 on a plain walk", n, ok, b.pickedDoors)
	}

	// The west wing's warrior is gone: the one in the hall is a door away.
	placeDoorNPCs(sess.st, typ, 3285, 3172, [3]int{101, 3288, 3173})
	n, ok = b.pickTarget(ctx, d)
	if !ok || n.Index != 101 || b.pickedDoors != 1 {
		t.Fatalf("picked %+v (ok=%v, doors=%d), want warrior 101 through one door", n, ok, b.pickedDoors)
	}
	if got := strings.Join(*logged, "\n"); !strings.Contains(got, "going through 1 door(s)") || !strings.Contains(got, "Large door") {
		t.Errorf("the door pick is not in the log:\n%s", got)
	}
}

// With the old reading (Doors off, the baked pack's every-door-shut) the same account passes over the warrior in the
// hall for a minute and waits: the reproduction.
func TestPickTargetWithoutDoorsPassesOverTheWarriorInTheHall(t *testing.T) {
	b, ctx, d, sess, logged := doorTrainer(t, 3285, 3172)
	d.Doors = false
	typ := warriorType(t)
	placeDoorNPCs(sess.st, typ, 3285, 3172, [3]int{101, 3288, 3173})
	if n, ok := b.pickTarget(ctx, d); ok {
		t.Fatalf("picked %+v with Doors off, want none (the hall is behind a shut door on the baked pack)", n)
	}
	if got := strings.Join(*logged, "\n"); !strings.Contains(got, "no route from here to the Al-Kharid warrior at 3288,3173,0") {
		t.Errorf("the old warning is not in the log:\n%s", got)
	}
}

// The doors are a bounded detour: DoorStrikeLimit door targets without a kill hold them shut, and a kill gives them
// back.
func TestDoorTargetsAreBoundedBetweenKills(t *testing.T) {
	b, ctx, d, sess, logged := doorTrainer(t, 3285, 3172)
	typ := warriorType(t)
	placeDoorNPCs(sess.st, typ, 3285, 3172, [3]int{101, 3288, 3173}, [3]int{102, 3289, 3171})
	for i := 1; i < combatplan.DoorStrikeLimit; i++ {
		if _, ok := b.pickTarget(ctx, d); !ok || b.pickedDoors == 0 {
			t.Fatalf("door target %d was not taken (doors=%d)", i, b.pickedDoors)
		}
		b.noteDoorTarget(ctx, gamestate.NPC{Index: 101, X: 3288, Z: 3173}, combatrunner.FightResult{DoorFailed: true})
		// The retire the loop applies to a target it could not attack, lapsed: the same two are weighed again.
		b.unreachable = map[int]time.Time{}
	}
	// The last one of the budget.
	if _, ok := b.pickTarget(ctx, d); !ok {
		t.Fatal("the last door target of the budget was refused")
	}
	b.noteDoorTarget(ctx, gamestate.NPC{Index: 101, X: 3288, Z: 3173, Level: 0}, combatrunner.FightResult{DoorFailed: true})
	b.unreachable = map[int]time.Time{}
	if got := strings.Join(*logged, "\n"); !strings.Contains(got, "door targets in a row without a kill") || !strings.Contains(got, "keeping to this side of the doors for 5m0s") {
		t.Fatalf("the hold is not in the log:\n%s", got)
	}
	if n, ok := b.pickTarget(ctx, d); ok {
		t.Fatalf("picked %+v with the doors held shut", n)
	}
	// A kill clears the count: the hold was for a door that led nowhere, but the lapse is the clock's. Held means held.
	b.doors.Kill()
	if b.doors.Allowed(time.Now()) {
		t.Fatal("a kill lifted a hold that has not run out")
	}
	if b.doors.Allowed(time.Now().Add(combatplan.DoorHold+time.Second)) == false {
		t.Fatal("the doors stayed held past the hold")
	}
}
