package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ---- the factory ----

func TestDemonSlayerFactoryBuildsEveryExecutorTheModuleNames(t *testing.T) {
	q := DemonSlayer()
	if q.ID() != questplan.DemonSlayerID || q.Module.Record.Name != "Demon Slayer" || q.Module.Record.QuestPoints != 3 || q.Module.Decide == nil || q.Finish == nil {
		t.Errorf("built %q %q %d QP, finish %v", q.ID(), q.Module.Record.Name, q.Module.Record.QuestPoints, q.Finish != nil)
	}
	for _, name := range []string{questplan.CustomDemonTraiborn, questplan.CustomDemonWater, questplan.CustomDemonDrain, questplan.CustomDemonSewer,
		questplan.CustomDemonWield, questplan.CustomDemonDelrith} {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	// The bones' skill source is registered with the provisioner.
	if SkillMethods()[questplan.MethodDemonBones] == nil {
		t.Errorf("no skill method %q", questplan.MethodDemonBones)
	}
	// Every custom step the module can return names a registered executor.
	for name := range q.Customs {
		found := false
		for _, c := range []string{questplan.CustomDemonTraiborn, questplan.CustomDemonWater, questplan.CustomDemonDrain, questplan.CustomDemonSewer,
			questplan.CustomDemonWield, questplan.CustomDemonDelrith} {
			found = found || c == name
		}
		if !found {
			t.Errorf("executor %q is not a step the module returns", name)
		}
	}
}

// ---- the cows ----

func cow(index, x, z int, mods ...func(*entityquery.Entity)) entityquery.Entity {
	e := entityquery.Entity{Kind: entityquery.KindNpc, Index: index, ID: 81, Name: "Cow", X: x, Z: z, FaceEntity: -1}
	for _, m := range mods {
		m(&e)
	}
	return e
}

func TestPickCowChoosesTheNearestCowByTheWalkAndSkipsTheRest(t *testing.T) {
	now := time.Now()
	steps := func(t tile.Tile) int { return tile.Chebyshev(t, tile.Tile{X: 3259, Z: 3270}) }
	busy := func(e *entityquery.Entity) { e.InCombat, e.FaceEntity = true, 32768+9 }
	mine := func(e *entityquery.Entity) { e.InCombat, e.FaceEntity = true, 32768+3 }
	named := func(name string) func(*entityquery.Entity) { return func(e *entityquery.Entity) { e.Name = name } }
	for _, c := range []struct {
		name string
		npcs []entityquery.Entity
		skip map[int]time.Time
		walk func(tile.Tile) int
		want int // the index picked, -1 for none
	}{
		{"the nearest by the walk", []entityquery.Entity{cow(1, 3270, 3270), cow(2, 3262, 3270), cow(3, 3266, 3270)}, nil, steps, 2},
		{"a tie goes to the lower index", []entityquery.Entity{cow(7, 3262, 3270), cow(5, 3256, 3270)}, nil, steps, 5},
		{"one another player is fighting is passed over", []entityquery.Entity{cow(1, 3260, 3270, busy), cow(2, 3270, 3270)}, nil, steps, 2},
		{"one fighting the account is not busy", []entityquery.Entity{cow(1, 3260, 3270, mine), cow(2, 3270, 3270)}, nil, steps, 1},
		{"one given up on is passed over until its time", []entityquery.Entity{cow(1, 3260, 3270), cow(2, 3270, 3270)}, map[int]time.Time{1: now.Add(time.Minute)}, steps, 2},
		{"one whose skip has run out is back", []entityquery.Entity{cow(1, 3260, 3270), cow(2, 3270, 3270)}, map[int]time.Time{1: now.Add(-time.Second)}, steps, 1},
		{"one no walk reaches is passed over", []entityquery.Entity{cow(1, 3260, 3270), cow(2, 3270, 3270)}, nil,
			func(t tile.Tile) int {
				if t.X == 3260 {
					return -1
				}
				return 4
			}, 2},
		{"a calf is not a cow", []entityquery.Entity{cow(1, 3260, 3270, named("Cow calf")), cow(2, 3270, 3270)}, nil, steps, 2},
		{"a dairy cow is not a cow", []entityquery.Entity{cow(1, 3260, 3270, named("Dairy cow"))}, nil, steps, -1},
		{"no cows", nil, nil, steps, -1},
	} {
		got, ok := pickCow(c.npcs, 3, now, c.skip, c.walk)
		switch {
		case c.want < 0 && ok:
			t.Errorf("%s: picked cow %d, want none", c.name, got.Index)
		case c.want >= 0 && (!ok || got.Index != c.want):
			t.Errorf("%s: picked %d (%v), want %d", c.name, got.Index, ok, c.want)
		}
	}
}

func TestCowGiveUp(t *testing.T) {
	began := time.Now().Add(-time.Minute)
	now := time.Now()
	busy := func(e *entityquery.Entity) { e.InCombat, e.FaceEntity = true, 32768+9 }
	hit := func(e *entityquery.Entity) { e.TotalHealth = 8 }
	for _, c := range []struct {
		name  string
		cur   entityquery.Entity
		began time.Time
		steps int
		why   string
		skip  time.Duration
	}{
		{"all well", cow(1, 3260, 3270, hit), began, 3, "", 0},
		{"no walk reaches it", cow(1, 3260, 3270, hit), began, -1, "open ground", demonCowSkipFor},
		{"another player is fighting it", cow(1, 3260, 3270, hit, busy), began, 3, "another player", demonCowSkipBusy},
		{"another player, but only just begun", cow(1, 3260, 3270, hit, busy), now.Add(-time.Second), 3, "", 0},
		{"no blow in half a minute", cow(1, 3260, 3270), began, 3, "no blow", demonCowSkipFor},
		{"no blow, but only just begun", cow(1, 3260, 3270), now.Add(-time.Second), 3, "", 0},
	} {
		why, skip := cowGiveUp(c.cur, 3, c.began, now, c.steps)
		if (c.why == "") != (why == "") || !strings.Contains(why, c.why) || skip != c.skip {
			t.Errorf("%s: %q for %s, want %q for %s", c.name, why, skip, c.why, c.skip)
		}
	}
}

// ---- the sword ----

const (
	demonOldSword = 1291 // a bronze longsword
)

// swordWorld is a simulated account with the tabs shown, Silverlight in the pack and an old sword in its right hand.
func swordWorld(t *testing.T, withOld bool) (*execWorld, *questrunner.Engine) {
	t.Helper()
	w := newExecWorld(actionsim.Stack{ID: questplan.ObjSilverlight, Count: 1})
	w.sim.SetWearPos(questplan.ObjSilverlight, actionsim.RightHand)
	w.sim.SetWearPos(demonOldSword, actionsim.RightHand)
	if withOld {
		w.sim.Wear(actionsim.RightHand, demonOldSword)
	}
	w.e.Store = questplan.NewStore()
	w.e.D.Components = actionsim.TabComponents()
	return w, w.e
}

func wornHas(w *execWorld, id int) bool {
	for _, s := range w.sim.Worn() {
		if int(s.ID) == id {
			return true
		}
	}
	return false
}

func TestWieldRemembersTheWeaponAndFinishPutsItBack(t *testing.T) {
	w, e := swordWorld(t, true)
	if !demonWield(e, questplan.Step{}) {
		t.Fatalf("the wield failed: %v", *w.log)
	}
	if !wornHas(w, questplan.ObjSilverlight) || wornHas(w, demonOldSword) || w.held(demonOldSword) != 1 {
		t.Errorf("worn %+v, pack %+v: want Silverlight worn and the old sword in the pack", w.sim.Worn(), w.sim.Pack())
	}
	hints := e.Store.Record(questplan.DemonSlayerID).Hints
	if len(hints) != 1 || !hints["weapon:1291"] {
		t.Errorf("hints %v, want the old sword remembered", hints)
	}
	// A second wield is a no-op.
	if !demonWield(e, questplan.Step{}) || len(e.Store.Record(questplan.DemonSlayerID).Hints) != 1 {
		t.Error("a second wield disturbed the hints")
	}
	// The quest ends: the old sword goes back on and the hint goes.
	demonFinish(e)
	if !wornHas(w, demonOldSword) || w.held(demonOldSword) != 0 {
		t.Errorf("worn %+v, pack %+v: want the old sword back in the hand", w.sim.Worn(), w.sim.Pack())
	}
	if len(e.Store.Record(questplan.DemonSlayerID).Hints) != 0 {
		t.Errorf("hints %v after the finish", e.Store.Record(questplan.DemonSlayerID).Hints)
	}
	// And the finish with nothing left to do does nothing.
	demonFinish(e)
}

func TestFinishLeavesAWeaponTheTrainerBankedWhereItIs(t *testing.T) {
	w, e := swordWorld(t, true)
	if !demonWield(e, questplan.Step{}) {
		t.Fatalf("the wield failed: %v", *w.log)
	}
	w.sim.Remove(demonOldSword, 1) // banked by the trainer between the wield and the end
	demonFinish(e)
	if wornHas(w, demonOldSword) || len(e.Store.Record(questplan.DemonSlayerID).Hints) != 0 {
		t.Errorf("worn %+v, hints %v", w.sim.Worn(), e.Store.Record(questplan.DemonSlayerID).Hints)
	}
	if !strings.Contains(strings.Join(*w.log, "\n"), "not in the pack") {
		t.Errorf("the log does not say the weapon stays: %v", *w.log)
	}
}

func TestWieldWithNothingInTheHandRemembersNothing(t *testing.T) {
	w, e := swordWorld(t, false)
	if !demonWield(e, questplan.Step{}) || !wornHas(w, questplan.ObjSilverlight) {
		t.Fatalf("the wield failed: %v", *w.log)
	}
	if len(e.Store.Record(questplan.DemonSlayerID).Hints) != 0 {
		t.Errorf("hints %v for an empty hand", e.Store.Record(questplan.DemonSlayerID).Hints)
	}
	demonFinish(e) // nothing to put back
}

func TestWieldWithoutTheSwordFails(t *testing.T) {
	w := newExecWorld()
	w.e.Store = questplan.NewStore()
	if demonWield(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no Silverlight") {
		t.Errorf("a wield with no sword: %v", *w.log)
	}
}

// ---- the early returns that must not send an account anywhere ----

func TestDemonExecutorsRefuseWhatTheyCannotDo(t *testing.T) {
	w := newExecWorld()
	// Traiborn's key in the pack is done; bones are needed otherwise.
	if demonTraiborn(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no bones") {
		t.Errorf("Traiborn with no bones: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjKeyTraiborn, Count: 1})
	if !demonTraiborn(w.e, questplan.Step{}) {
		t.Errorf("Traiborn with the key in hand: %v", *w.log)
	}
	// The water: nothing to fill, or already filled.
	w = newExecWorld()
	if demonWater(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no bucket") {
		t.Errorf("the sink with no bucket: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjBucketOfWater, Count: 1})
	if !demonWater(w.e, questplan.Step{}) {
		t.Errorf("the sink with the water in hand: %v", *w.log)
	}
	// The drain: no bucket of water is a refusal; the key in hand needs no pour and, outside the sewer, no walk.
	w = newExecWorld()
	if demonDrain(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no bucket of water") {
		t.Errorf("the drain with no water: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjKeyDrain, Count: 1})
	if !demonDrain(w.e, questplan.Step{}) {
		t.Errorf("the drain with the key in hand: %v", *w.log)
	}
	// The sewer: an account outside it is out.
	w = newExecWorld()
	if !demonSewer(w.e, questplan.Step{}) || !demonSewerLeave(w.e.D) {
		t.Errorf("the sewer step outside the sewer: %v", *w.log)
	}
	// Delrith: no Silverlight worn is a refusal at once, without a step of walking.
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjSilverlight, Count: 1})
	if demonDelrith(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "not worn") {
		t.Errorf("Delrith with the sword in the pack: %v", *w.log)
	}
}

// ---- Traiborn's conversation driver ----

// A hand-in is a tick a bone with no dialogue open, so a conversation is not over while the bones are still going: the driver waits
// until the pack has stopped changing for demonQuietSteps looks, and not longer.
func TestTheTraibornDriverWaitsOutTheHandInAndStopsWhenItIsQuiet(t *testing.T) {
	w := newExecWorld(actionsim.Stack{ID: questplan.ObjBones, Count: 25})
	looks := 0
	w.e.D.Delay = func(time.Duration) bool {
		looks++
		if looks <= 25 {
			w.sim.Remove(questplan.ObjBones, 1)
		}
		return true
	}
	// Never done: the key does not come.
	if demonDriveUntilQuiet(w.e.D, questplan.TraibornStop.Prefer, func() bool { return false }, time.Hour) {
		t.Error("the driver reported done")
	}
	if want := 25 + demonQuietSteps; looks < want || looks > want+2 {
		t.Errorf("the driver looked %d times, want about %d (25 bones, then %d quiet looks)", looks, want, demonQuietSteps)
	}
	// Done at once: no look at all.
	looks = 0
	if !demonDriveUntilQuiet(w.e.D, questplan.TraibornStop.Prefer, func() bool { return true }, time.Hour) || looks != 0 {
		t.Errorf("a satisfied driver looked %d times", looks)
	}
	// Done when the pack is empty of bones: the key's arrival ends it at once, mid-quiet.
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjBones, Count: 3})
	looks = 0
	w.e.D.Delay = func(time.Duration) bool {
		looks++
		if looks <= 3 {
			w.sim.Remove(questplan.ObjBones, 1)
		} else if looks == 5 {
			w.sim.Give(questplan.ObjKeyTraiborn, 1)
		}
		return true
	}
	if !demonDriveUntilQuiet(w.e.D, questplan.TraibornStop.Prefer, func() bool { return w.held(questplan.ObjKeyTraiborn) > 0 }, time.Hour) || looks > 6 {
		t.Errorf("the driver looked %d times and did not see the key", looks)
	}
	// The budget ends it whatever happens.
	w = newExecWorld(actionsim.Stack{ID: questplan.ObjBones, Count: 25})
	w.e.D.Delay = func(time.Duration) bool {
		time.Sleep(2 * time.Millisecond)
		w.sim.Give(questplan.ObjBones, 1)
		w.sim.Remove(questplan.ObjBones, 1)
		return true
	}
	start := time.Now()
	if demonDriveUntilQuiet(w.e.D, nil, func() bool { return false }, 50*time.Millisecond) || time.Since(start) > 2*time.Second {
		t.Error("the budget did not end the driver")
	}
}

// ---- the sewer's rectangle is the planner's ----

func TestTheSewerRectangleIsTheSpotsTheStepsWalk(t *testing.T) {
	for _, at := range []tile.Tile{questplan.DemonSewerLanding, questplan.DemonSewerKey, questplan.DemonSewerLadder} {
		if !questplan.InVarrockSewer(at) {
			t.Errorf("%v is not in the sewer", at)
		}
	}
	if questplan.InVarrockSewer(questplan.DemonManholeStand) {
		t.Error("the street above the manhole is in the sewer")
	}
}
