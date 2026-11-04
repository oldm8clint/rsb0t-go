package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The executors' paths that need no map and no npc: the early returns that must not send an account anywhere, the refusals that
// must say why, and the pure rules of the hunt (which animal to fight and when to leave it) and of the island's walks.

func TestDruidExecutorsRefuseOrFinishAtOnceWhereTheyStand(t *testing.T) {
	// Dipping outside the room does not move the account.
	w := newExecWorld(actionsim.Stack{ID: questplan.ObjRawBeef, Count: 1})
	w.sim.StandAt(questplan.DruidHuntHome.X, questplan.DruidHuntHome.Z)
	if druidDip(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "not in the cauldron's room") {
		t.Errorf("a dip outside the room: %v", *w.log)
	}
	// Entering from inside the room is done; so is a hunt for a meat the pack holds, in either form.
	w = newExecWorld()
	w.sim.StandAt(questplan.CauldronStand.X, questplan.CauldronStand.Z)
	if !druidEnter(w.e, questplan.Step{}) {
		t.Errorf("entering from the room failed: %v", *w.log)
	}
	for _, id := range []int{questplan.ObjRawRatMeat, questplan.ObjEnchantedRatMeat} {
		w = newExecWorld(actionsim.Stack{ID: int32(id), Count: 1})
		if !druidHunt(w.e, questplan.DruidMeats[1]) {
			t.Errorf("a hunt for a meat held as %d did not finish at once: %v", id, *w.log)
		}
	}
	// A full pack cannot take a meat: the hunt says so and does not walk.
	var full []actionsim.Stack
	for i := 0; i < 28; i++ {
		full = append(full, actionsim.Stack{ID: int32(1000 + i), Count: 1})
	}
	w = newExecWorld(full...)
	if druidHunt(w.e, questplan.DruidMeats[0]) || !strings.Contains(strings.Join(*w.log, "\n"), "no room in the pack") {
		t.Errorf("a hunt with a full pack: %v", *w.log)
	}
}

func TestJungleExecutorsRefuseOrFinishAtOnceWhereTheyStand(t *testing.T) {
	// A herb already in the pack is picked; a herb that is not one of the five is refused with its number.
	w := newExecWorld(actionsim.Stack{ID: int32(questplan.JungleHerbs[2].Unid), Count: 1})
	if !junglePick(w.e, questplan.Step{Qty: 2}) {
		t.Errorf("a pick of a herb the pack holds failed: %v", *w.log)
	}
	if junglePick(w.e, questplan.Step{Qty: 7}) || junglePick(w.e, questplan.Step{Qty: -1}) {
		t.Error("a pick of a herb that does not exist succeeded")
	}
	// Off the island's cave, leaving it is done; in it and already there, entering is done.
	w = newExecWorld()
	w.sim.StandAt(questplan.TrufitusAnchor.X, questplan.TrufitusAnchor.Z)
	if !jungleLeaveCave(w.e.D) {
		t.Error("leaving a cave the account is not in failed")
	}
	w = newExecWorld()
	w.sim.StandAt(questplan.JungleCaveArrival.X, questplan.JungleCaveArrival.Z)
	if !jungleEnterCave(w.e.D) {
		t.Error("entering a cave the account is in failed")
	}
	// Already within the radius of where a walk goes, the walk is made (the account is not sent round for nothing).
	w = newExecWorld()
	w.sim.StandAt(questplan.TrufitusAnchor.X, questplan.TrufitusAnchor.Z)
	if !jungleGo(w.e.D, questplan.TrufitusAnchor, 3) {
		t.Error("a walk to where the account stands failed")
	}
}

// The island's rectangles reach the walker as its own type, one for each hunter, on the ground level.
func TestJungleAvoidRectsAreTheWalkersOwn(t *testing.T) {
	rects := jungleAvoidRects()
	want := questplan.JungleAvoid()
	if len(rects) != len(want) {
		t.Fatalf("%d walker rectangles for %d plan rectangles", len(rects), len(want))
	}
	for i, r := range rects {
		w := want[i]
		if r.MinX != w.MinX || r.MaxX != w.MaxX || r.MinZ != w.MinZ || r.MaxZ != w.MaxZ || r.Level != w.Level {
			t.Errorf("rectangle %d: %+v, plan %+v", i, r, w)
		}
	}
	// A spawn is inside its own rectangle and a tile a pad and a half away is not.
	h := questplan.JungleHostiles()[0]
	pt := func(x, z int) webwalk.Point { return webwalk.Point{X: x, Z: z, Level: 0} }
	if !rects[0].Contains(pt(h.Tile.X, h.Tile.Z)) || rects[0].Contains(pt(h.Tile.X+2*questplan.JungleSpiderPad, h.Tile.Z)) {
		t.Errorf("rectangle 0 %+v for %v", rects[0], h)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The hunt's rules
// ---------------------------------------------------------------------------------------------------------------------

func TestMeatHuntGivesUpOnAnAnimalItCannotReachOrHasStrayedOrIsAnotherPlayers(t *testing.T) {
	m := questplan.DruidMeats[0] // the bear
	h := &meatHunt{m: m, skip: map[int]time.Time{}}
	now := time.Now()
	at := func(dx int) entityquery.Entity {
		return entityquery.Entity{X: m.Field.X + dx, Z: m.Field.Z, Level: m.Field.Level, TotalHealth: 5}
	}
	self := 7
	for _, c := range []struct {
		name  string
		cur   entityquery.Entity
		steps int
		since time.Duration
		want  string
		skip  time.Duration
	}{
		{"fine", at(2), 4, time.Minute, "", 0},
		{"strayed off the field", at(m.Radius + 2*druidStray + 1), 4, time.Minute, "wandered off", druidSkipFor},
		{"no walk reaches it", at(2), -1, time.Minute, "no walk over open ground", druidSkipFor},
		{"another player is fighting it", func() entityquery.Entity { e := at(2); e.InCombat, e.FaceEntity = true, 32768+9; return e }(), 4, time.Minute, "another player", druidSkipBusy},
		{"it faces the account itself", func() entityquery.Entity { e := at(2); e.InCombat, e.FaceEntity = true, 32768+self; return e }(), 4, time.Minute, "", 0},
		{"another player's, but only just begun", func() entityquery.Entity { e := at(2); e.InCombat, e.FaceEntity = true, 32768+9; return e }(), 4, time.Second, "", 0},
		{"no blow has landed", func() entityquery.Entity { e := at(2); e.TotalHealth = 0; return e }(), 4, time.Minute, "no blow", druidSkipFor},
		{"no blow yet, but only just begun", func() entityquery.Entity { e := at(2); e.TotalHealth = 0; return e }(), 4, 5 * time.Second, "", 0},
	} {
		why, skip := h.giveUp(c.cur, self, now.Add(-c.since), now, c.steps)
		if (c.want == "") != (why == "") || !strings.Contains(why, c.want) || skip != c.skip {
			t.Errorf("%s: %q for %s, want %q for %s", c.name, why, skip, c.want, c.skip)
		}
	}
}

// The pick is the nearest animal of the meat's kind by a walk over open ground (here the distance, there being no pack), not one
// another player is fighting, nor one given up on, nor one off the field; between equals the lower index.
func TestMeatHuntPickIsTheNearestReachableFreeAnimalOfItsKind(t *testing.T) {
	d := newExecWorld().e.D
	h := &meatHunt{d: d, m: questplan.DruidMeats[2], skip: map[int]time.Time{}} // the chicken
	// No npcs in a simulated session: nothing to pick, and the hunt waits for one.
	if _, ok := h.pick(); ok {
		t.Error("an animal was picked from an empty scene")
	}
	// The same rules as pickGoblin's, on the tile helper: stepsTo falls back to distance without a pack.
	here := tile.Tile{X: 3188, Z: 3278}
	if n := h.stepsTo(here, tile.Tile{X: 3190, Z: 3278}); n != 2 {
		t.Errorf("without a pack the steps are the distance: %d", n)
	}
}

// The skeletons that followed the account to the Prison door are fought back, one at a time and up to the cap, the door's suit and
// anything the account is not within ten combat levels of are not, and the cap allows the price's five and a little over.
func TestDruidFightFollowerFightsTheSkeletonsAndNothingElse(t *testing.T) {
	skeleton := questplan.NpcManorSkeleton
	maxFights := len(questplan.DruidCorridorSkeletons) + druidDoorSpare
	if maxFights <= len(questplan.DruidCorridorSkeletons) {
		t.Fatalf("the cap %d does not cover the %d skeletons the price counts", maxFights, len(questplan.DruidCorridorSkeletons))
	}
	for name, c := range map[string]struct {
		fights, npcID, npcCombat, own int
		ownKnown                      bool
		want                          bool
	}{
		"the first skeleton":                {0, skeleton, 25, 40, true, true},
		"the last the price counts":         {len(questplan.DruidCorridorSkeletons) - 1, skeleton, 25, 40, true, true},
		"the spare fight":                   {maxFights - 1, skeleton, 25, 40, true, true},
		"past the cap":                      {maxFights, skeleton, 25, 40, true, false},
		"the door's suit":                   {0, questplan.NpcSuitOfArmour, 19, 40, true, false},
		"a poison spider, level 64":         {0, 134, 64, 40, true, false},
		"a foe ten levels over, fought":     {0, skeleton, 50, 40, true, true},
		"a foe eleven levels over, not":     {0, skeleton, 51, 40, true, false},
		"an account whose level is unread":  {0, skeleton, 25, 0, false, false},
		"an attacker whose level is unread": {0, skeleton, 0, 40, true, false},
	} {
		if got := druidFightFollower(c.fights, maxFights, c.npcID, c.npcCombat, c.own, c.ownKnown); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
