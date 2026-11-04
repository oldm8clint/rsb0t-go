package questmods

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ---- the route loop, over a fake world ----

// bkFake is a world whose crossings teleport: the account stands in a tile of the route and a crossing moves it to the leg's landing
// if it stands in the leg's region.
type bkFake struct {
	at        tile.Tile
	regions   map[tile.Tile]string
	crossed   []string
	approach  int
	parks     []string
	log       []string
	failAt    int // the nth crossing (1-based) fails; 0 never
	dieAfter  int // the account is dead after this many crossings; 0 never
	outAfter  int // out of food after this many crossings; 0 never
	stuck     bool
	approachX bool // Approach fails
	// lateAfter and shortFrom model the run's clock: Late is true after this many crossings, Short while this is set.
	lateAfter int
	short     bool
	yields    int
	// waypoints counts the walks to the west waypoint, and waypointX makes them fail.
	waypoints int
	waypointX bool
}

func newBKFake(at tile.Tile) *bkFake {
	w := &bkFake{at: at, regions: map[tile.Tile]string{}}
	for region, ts := range questplan.BKRegionTiles() {
		for _, t := range ts {
			w.regions[t] = region
		}
	}
	return w
}

func (w *bkFake) Tile() tile.Tile { return w.at }
func (w *bkFake) Locate(t tile.Tile) (string, bkWhere) {
	if r, ok := w.regions[t]; ok {
		return r, bkAt
	}
	if !questplan.BKInside(t) {
		if questplan.BKNearFortress(t) {
			return questplan.BKOutside, bkAt
		}
		return "", bkFar
	}
	return "", bkLost
}
func (w *bkFake) Cross(leg questplan.BKLeg) bool {
	if r, _ := w.Locate(w.at); r != leg.From {
		panic(fmt.Sprintf("crossing %q from %q, the account is in %q", leg.Name, leg.From, r))
	}
	w.crossed = append(w.crossed, leg.Name)
	if w.failAt > 0 && len(w.crossed) == w.failAt {
		return false
	}
	if !w.stuck {
		w.at = leg.Land
	}
	return true
}
func (w *bkFake) Approach() bool {
	w.approach++
	if w.approachX {
		return false
	}
	w.at = questplan.BKDoorApproach
	return true
}
func (w *bkFake) Waypoint() bool {
	w.waypoints++
	if w.waypointX {
		return false
	}
	w.at = questplan.BKWestWaypoint
	return true
}
func (w *bkFake) Late() bool  { return w.lateAfter > 0 && len(w.crossed) >= w.lateAfter }
func (w *bkFake) Short() bool { return w.short }
func (w *bkFake) Yield()      { w.yields++ }
func (w *bkFake) Died() bool  { return w.dieAfter > 0 && len(w.crossed) >= w.dieAfter }
func (w *bkFake) Out() bool   { return w.outAfter > 0 && len(w.crossed) >= w.outAfter }
func (w *bkFake) Park(reason string) {
	w.parks = append(w.parks, reason)
}
func (w *bkFake) Logf(f string, args ...any) { w.log = append(w.log, fmt.Sprintf(f, args...)) }

func TestBKRouteTakesTheWholePlanFromAnywhere(t *testing.T) {
	regions := questplan.BKRegionTiles()
	for from, ts := range regions {
		for to := range regions {
			plan := questplan.BKPlan(from, to)
			if from != to && plan == nil {
				continue // no way (none expected)
			}
			w := newBKFake(ts[0])
			if from == questplan.BKOutside {
				w.at = questplan.BKDoorApproach // the open ground is near-fortress tiles, not the door-line tile only
			}
			if bkRoute(w, to) != bkArrived {
				t.Errorf("%q to %q: the route failed: %v", from, to, w.log)
				continue
			}
			if got := len(w.crossed); got != len(plan) {
				t.Errorf("%q to %q: %d crossings, the plan has %d (%v)", from, to, got, len(plan), w.crossed)
			}
			for i, name := range w.crossed {
				if name != plan[i].Name {
					t.Errorf("%q to %q: crossing %d is %q, the plan says %q", from, to, i, name, plan[i].Name)
				}
			}
		}
	}
}

func TestBKRouteFromFarWalksToTheDoorFirst(t *testing.T) {
	w := newBKFake(tile.Tile{X: 2960, Z: 3340, Level: 2})
	if bkRoute(w, questplan.BKGrillShaft) != bkArrived {
		t.Fatalf("failed: %v", w.log)
	}
	if w.approach != 1 || len(w.crossed) != 11 || w.crossed[0] != "the front door, in" || w.crossed[10] != "the grill ladder, down" {
		t.Errorf("approach %d, crossings %v", w.approach, w.crossed)
	}
	// Leaving from far away has nothing to do.
	w = newBKFake(tile.Tile{X: 2960, Z: 3340, Level: 2})
	if bkRoute(w, questplan.BKOutside) != bkArrived || w.approach != 0 || len(w.crossed) != 0 {
		t.Errorf("leave from far: approach %d crossings %v", w.approach, w.crossed)
	}
	// An approach that fails fails the route.
	w = newBKFake(tile.Tile{X: 2960, Z: 3340, Level: 2})
	w.approachX = true
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed || len(w.crossed) != 0 {
		t.Error("a failed approach crossed anyway")
	}
}

func TestBKRouteStopsWhenACrossingFailsDiesOrGetsLost(t *testing.T) {
	// A failed crossing ends the route there: the engine's next decision resumes from the room the account stands in.
	w := newBKFake(questplan.BKDoorApproach)
	w.failAt = 3
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed || len(w.crossed) != 3 {
		t.Errorf("failed at 3: %v", w.crossed)
	}
	if r, _ := w.Locate(w.at); r != questplan.BKAlcove0 {
		t.Errorf("the account stands in %q after two crossings, want the alcove", r)
	}
	// ...and resumes there: the same loop from that tile takes the rest.
	w.failAt, w.crossed = 0, nil
	if bkRoute(w, questplan.BKGrillShaft) != bkArrived || len(w.crossed) != 9 || w.crossed[0] != "the alcove ladder, up" {
		t.Errorf("resumed: %v", w.crossed)
	}
	// Death ends it, whatever is left.
	w = newBKFake(questplan.BKDoorApproach)
	w.dieAfter = 4
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed || len(w.crossed) != 4 {
		t.Errorf("died after 4: %v", w.crossed)
	}
	// An account on floor the route does not name stops without a crossing.
	w = newBKFake(tile.Tile{X: 3028, Z: 3508, Level: 0})
	if bkRoute(w, questplan.BKOutside) != bkFailed || len(w.crossed) != 0 || !strings.Contains(strings.Join(w.log, "|"), "on no part of the route") {
		t.Errorf("lost: %v %v", w.crossed, w.log)
	}
	// No way: a goal no region reaches.
	w = newBKFake(questplan.BKDoorApproach)
	if bkRoute(w, "the moon") != bkFailed || len(w.crossed) != 0 {
		t.Errorf("moon: %v", w.crossed)
	}
	// A crossing that reports success and moves nobody is tried bkMaxLegs times and no more.
	w = newBKFake(questplan.BKDoorApproach)
	w.stuck = true
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed || len(w.crossed) != bkMaxLegs {
		t.Errorf("stuck: %d crossings", len(w.crossed))
	}
}

func TestBKRouteTurnsRoundWhenTheFoodIsGoneAndParksOnArrival(t *testing.T) {
	// Out of food after the front door and the wall: the nearest way out is the two crossings back.
	w := newBKFake(questplan.BKDoorApproach)
	w.outAfter = 2
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed {
		t.Fatal("an account with no food got to the grill")
	}
	want := []string{"the front door, in", "the secret wall, in", "the secret wall, out", "the front door, out"}
	if !slices.Equal(w.crossed, want) {
		t.Errorf("crossings %v, want %v", w.crossed, want)
	}
	if len(w.parks) != 1 || !strings.Contains(w.parks[0], "out of food") {
		t.Errorf("parks %v", w.parks)
	}
	// Already heading out: nothing to turn round for, no park.
	w = newBKFake(t3(3022, 3517, 1)) // the north room
	w.outAfter = 1
	if bkRoute(w, questplan.BKOutside) != bkArrived || len(w.parks) != 0 {
		t.Errorf("leaving with no food: parks %v crossed %v", w.parks, w.crossed)
	}
	// Retreat from far away is just stopping.
	w = newBKFake(tile.Tile{X: 2960, Z: 3340})
	w.outAfter = 1
	w.crossed = []string{"x"}
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed || w.approach != 0 {
		t.Error("a starved account far away approached the fortress")
	}
}

// The run's clock: an account with too little of the run left does not go in, and one inside with the run nearly over goes out. Neither
// is a failure (the quest is not marked, the run is ended) and neither parks the quest.
func TestBKRouteDoesNotEnterOrStayInsideWhenTheRunIsAlmostOver(t *testing.T) {
	// Far away, short of time: no approach, the run yields.
	w := newBKFake(tile.Tile{X: 2960, Z: 3340, Level: 2})
	w.short = true
	if bkRoute(w, questplan.BKGrillShaft) != bkYielded || w.yields != 1 || w.approach != 0 || len(w.crossed) != 0 {
		t.Errorf("far, short: yields %d approach %d crossings %v", w.yields, w.approach, w.crossed)
	}
	// At the door, short of time: not in.
	w = newBKFake(questplan.BKDoorApproach)
	w.short = true
	if bkRoute(w, questplan.BKGrillShaft) != bkYielded || w.yields != 1 || len(w.crossed) != 0 {
		t.Errorf("at the door, short: yields %d crossings %v", w.yields, w.crossed)
	}
	// Inside with the run nearly over (after the fourth crossing): the way out, and the run ends with the account outside.
	w = newBKFake(questplan.BKDoorApproach)
	w.lateAfter = 4
	if got := bkRoute(w, questplan.BKGrillShaft); got != bkYielded || w.yields != 1 || len(w.parks) != 0 {
		t.Errorf("late: result %v, yields %d, parks %v: %v", got, w.yields, w.parks, w.crossed)
	}
	want := []string{"the front door, in", "the secret wall, in", "the alcove ladder, up", "the alcove's upper ladder, up",
		"the alcove's upper ladder, down", "the alcove ladder, down", "the secret wall, out", "the front door, out"}
	if !slices.Equal(w.crossed, want) {
		t.Errorf("crossings %v, want %v", w.crossed, want)
	}
	if r, _ := w.Locate(w.at); r != questplan.BKOutside {
		t.Errorf("the account ends in %q", r)
	}
	// Leaving never yields for want of time, and a short run does not stop a leaving account.
	w = newBKFake(t3(3022, 3517, 1))
	w.short, w.lateAfter = true, 1
	if bkRoute(w, questplan.BKOutside) != bkArrived || w.yields != 0 {
		t.Errorf("leaving: yields %d crossings %v", w.yields, w.crossed)
	}
}

// Every way a walk can end with the account on the open ground by the front door walks it west, to the waypoint, before the step
// returns: the walker's own way from the door to Falador or a bank runs south past the three sentry knights, and the engine (the
// park, the end of the run) or the next step sends the account from wherever it stands.
func TestBKRouteWalksClearOfTheSentriesOnEveryEndAtTheDoor(t *testing.T) {
	atWaypoint := func(t *testing.T, name string, w *bkFake) {
		t.Helper()
		if w.waypoints != 1 || w.at != questplan.BKWestWaypoint {
			t.Errorf("%s: %d waypoint walks, the account ends at %v, want one walk and %v: %v", name, w.waypoints, w.at, questplan.BKWestWaypoint, w.log)
		}
	}
	notWalked := func(t *testing.T, name string, w *bkFake) {
		t.Helper()
		if w.waypoints != 0 {
			t.Errorf("%s: %d waypoint walks, want none (the account is not on the open ground by the door): %v", name, w.waypoints, w.log)
		}
	}

	// The retreat for want of food: out through the door, parked, and then west.
	w := newBKFake(questplan.BKDoorApproach)
	w.outAfter = 2
	if got := bkRoute(w, questplan.BKGrillShaft); got != bkFailed || len(w.parks) != 1 {
		t.Fatalf("retreat: result %v, parks %v", got, w.parks)
	}
	atWaypoint(t, "the retreat park", w)

	// The run ending with the account inside: out through the door, the yield, and then west.
	w = newBKFake(questplan.BKDoorApproach)
	w.lateAfter = 4
	if got := bkRoute(w, questplan.BKGrillShaft); got != bkYielded || w.yields != 1 {
		t.Fatalf("late: result %v, yields %d", got, w.yields)
	}
	atWaypoint(t, "the late yield", w)

	// The run ending at the door: not in, the yield, and then west.
	w = newBKFake(questplan.BKDoorApproach)
	w.short = true
	if got := bkRoute(w, questplan.BKGrillShaft); got != bkYielded || w.yields != 1 || len(w.crossed) != 0 {
		t.Fatalf("short at the door: result %v, yields %d, crossings %v", got, w.yields, w.crossed)
	}
	atWaypoint(t, "the short yield at the door", w)

	// A first crossing that would not land leaves the account at the door.
	w = newBKFake(questplan.BKDoorApproach)
	w.failAt = 1
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed {
		t.Fatal("a failed first crossing did not fail the route")
	}
	atWaypoint(t, "a failed first crossing", w)

	// Too weak to go near the knights at all, at the door.
	w = newBKFake(questplan.BKDoorApproach)
	w.outAfter = 1
	w.crossed = []string{"x"}
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed || len(w.parks) != 0 {
		t.Fatalf("starved at the door: parks %v", w.parks)
	}
	atWaypoint(t, "a starved account at the door", w)

	// The way out: from inside to the open ground and then west (the old bkLeave tail), once; from the open ground, only west; from
	// the west waypoint itself, nothing to do (the walk is the walker's own no-op there, not a second walk).
	w = newBKFake(t3(3022, 3517, 1))
	if bkRoute(w, questplan.BKOutside) != bkArrived {
		t.Fatalf("leave: %v", w.log)
	}
	atWaypoint(t, "leaving from inside", w)
	w = newBKFake(questplan.BKDoorApproach)
	if bkRoute(w, questplan.BKOutside) != bkArrived || len(w.crossed) != 0 {
		t.Fatalf("leave from the door: %v", w.crossed)
	}
	atWaypoint(t, "leaving from the door", w)

	// A walk west that fails fails the way out, and does not hide the end of the others.
	w = newBKFake(t3(3022, 3517, 1))
	w.waypointX = true
	if bkRoute(w, questplan.BKOutside) != bkFailed || !strings.Contains(strings.Join(w.log, "|"), "clear of the knights") {
		t.Errorf("leave with no way west: %v", w.log)
	}
	w = newBKFake(questplan.BKDoorApproach)
	w.lateAfter, w.waypointX = 4, true
	if got := bkRoute(w, questplan.BKGrillShaft); got != bkYielded || w.yields != 1 {
		t.Errorf("late yield with no way west: result %v, yields %d", got, w.yields)
	}
	w = newBKFake(questplan.BKDoorApproach)
	w.outAfter, w.waypointX = 2, true
	if got := bkRoute(w, questplan.BKGrillShaft); got != bkFailed || len(w.parks) != 1 {
		t.Errorf("retreat park with no way west: result %v, parks %v", got, w.parks)
	}

	// Not on the open ground by the door: no walk. Far away; inside; arrived at the goal inside; lost; dead at the door.
	w = newBKFake(tile.Tile{X: 2960, Z: 3340, Level: 2})
	w.short = true
	if bkRoute(w, questplan.BKGrillShaft) != bkYielded {
		t.Fatal("far and short did not yield")
	}
	notWalked(t, "far away and short of time", w)
	w = newBKFake(tile.Tile{X: 2960, Z: 3340, Level: 2})
	if bkRoute(w, questplan.BKOutside) != bkArrived {
		t.Fatal("leaving from far away")
	}
	notWalked(t, "leaving from far away", w)
	w = newBKFake(questplan.BKDoorApproach)
	if bkRoute(w, questplan.BKGrillShaft) != bkArrived {
		t.Fatalf("grill: %v", w.log)
	}
	notWalked(t, "arrived at the grill", w)
	w = newBKFake(questplan.BKDoorApproach)
	w.failAt = 3
	if bkRoute(w, questplan.BKGrillShaft) != bkFailed {
		t.Fatal("a failed crossing inside did not fail the route")
	}
	notWalked(t, "a crossing that failed inside", w)
	w = newBKFake(tile.Tile{X: 3028, Z: 3508, Level: 0})
	if bkRoute(w, questplan.BKOutside) != bkFailed {
		t.Fatal("lost did not fail")
	}
	notWalked(t, "lost inside", w)
	w = newBKFake(t3(3022, 3517, 1))
	north, _ := w.Locate(w.at)
	if w.dieAfter = len(questplan.BKPlan(north, questplan.BKOutside)); w.dieAfter == 0 {
		t.Fatal("no plan out of the north room")
	}
	if bkRoute(w, questplan.BKOutside) != bkFailed {
		t.Fatal("a death did not fail the route")
	}
	notWalked(t, "dead at the door", w)
}

func t3(x, z, l int) tile.Tile { return tile.Tile{X: x, Z: z, Level: l} }

// ---- the module through the engine ----

type bkRig struct {
	*fillerRig
	worn   map[int]bool
	inside bool
	// at, when set, is where the account stands (the open ground by the door, say) whatever inside says.
	at tile.Tile
	qp int
	hp int
	// noChain makes the provisioner come back without an iron chainbody (every source dry).
	noChain bool
	// levels are the account's base levels.
	levels map[string]int
	coins  int
}

func newBKRig(t *testing.T, stage int, pack, bank map[int]int) *bkRig {
	r := &bkRig{fillerRig: newFillerRig(t, stage, pack, bank), worn: map[int]bool{}, qp: 20, hp: 40,
		levels: map[string]int{"attack": 20, "strength": 20, "defence": 15, "hitpoints": 40, "prayer": 1, "magic": 20, "ranged": 1}}
	return r
}

func (r *bkRig) wornIDs() map[int]int {
	out := map[int]int{}
	for id, ok := range r.worn {
		if ok {
			out[id] = 1
		}
	}
	return out
}

// provision is the real planner over the rig, with the worn disguise counted and a dry chainbody if asked.
func (r *bkRig) provision(kit []questplan.KitLine) bool {
	for pass := 0; pass < 16; pass++ {
		pack := r.pack()
		used := 0
		for id, n := range pack {
			if id == questplan.CoinsID {
				used++
			} else {
				used += n
			}
		}
		plan := questplan.PlanProvision(kit, questplan.ProvisionState{Pack: pack, Worn: r.wornIDs(), Bank: r.bank, BankKnown: true, FreeSlots: 28 - used,
			Levels: map[string]int{}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case questplan.PhaseSatisfied:
			return true
		case questplan.PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				n := min(c.Count, r.bank[c.ID])
				if n > 0 {
					r.bank[c.ID] -= n
					r.give(c.ID, n)
					moved = true
				}
			}
			if !moved {
				return false
			}
		case questplan.PhaseSource:
			task := plan.Tasks[0]
			if r.noChain && task.Line.ID == questplan.ObjIronChainbody {
				return false
			}
			r.give(task.Line.ID, task.Qty)
		default:
			return false
		}
	}
	return false
}

func (r *bkRig) journal() questplan.Status {
	switch {
	case r.stage == 0:
		return questplan.StatusNotStarted
	case r.stage >= questplan.BKComplete:
		return questplan.StatusComplete
	}
	return questplan.StatusInProgress
}

// wrap turns the module into one whose steps are stubs: the real Decide over a snapshot that carries the rig's quest state and
// account, every step but the three fortress customs answered by a stub that plays the content's rule for it.
func (r *bkRig) wrap(q questrunner.Quest, stubs map[string]questrunner.CustomFunc) questrunner.Quest {
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		s.Journal = r.journal()
		s.StageKnown = s.Journal == questplan.StatusInProgress
		s.Stage = r.stage
		s.QuestPoints, s.HP, s.MaxHP, s.Levels = r.qp, r.hp, r.levels["hitpoints"], r.levels
		s.WornIDs = r.wornIDs()
		switch {
		case r.at != (tile.Tile{}):
			s.Tile, s.TileKnown = r.at, true
		case r.inside:
			s.Tile, s.TileKnown = tile.Tile{X: 3020, Z: 3516}, true
		default:
			s.Tile, s.TileKnown = tile.Tile{X: 2960, Z: 3340, Level: 2}, true
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait:
			return st
		}
		st.Reason = st.Describe()
		switch {
		case st.Kind == questplan.StepCustom:
			// keeps its own name: bk-spy, bk-sabotage, bk-leave
		case st.Kind == questplan.StepProvision:
			st.Kind, st.Custom = questplan.StepCustom, "provision:"+st.KitName
		default:
			st.Kind, st.Custom = questplan.StepCustom, "stub:"+string(st.Kind)
		}
		return st
	}
	q.Customs = stubs
	return q
}

func (r *bkRig) stubs() map[string]questrunner.CustomFunc {
	m := map[string]questrunner.CustomFunc{}
	m["provision:"+questplan.KitBlackKnight] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.log = append(r.log, "stub provision "+st.KitName)
		return r.provision(st.Kit)
	}
	m["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		if st.Stop.ID != questplan.NpcSirAmik {
			return false
		}
		switch r.stage {
		case questplan.BKNotStarted:
			if r.qp < 12 {
				return false
			}
			r.stage = questplan.BKStarted
		case questplan.BKSabotaged:
			r.stage = questplan.BKComplete
			r.give(questplan.CoinsID, 2500)
			r.qp += 3
		}
		r.stubLog("talk Sir Amik -> stage %d", r.stage)
		return true
	}
	m["stub:equip"] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(st.ItemID, 1) {
			return false
		}
		r.worn[st.ItemID] = true
		r.stubLog("wear %d", st.ItemID)
		return true
	}
	m["stub:pickLoc"] = func(e *questrunner.Engine, st questplan.Step) bool {
		if st.LocID != questplan.LocCabbage {
			return false
		}
		r.give(questplan.ObjCabbage, 1)
		r.stubLog("pick a cabbage")
		return true
	}
	m["stub:deposit"] = func(e *questrunner.Engine, st questplan.Step) bool { r.stubLog("deposit"); return true }
	m[questplan.CustomBKSpy] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !(r.worn[questplan.ObjIronChainbody] && r.worn[questplan.ObjBronzeMedHelm]) && !r.inside {
			return false
		}
		r.inside, r.stage = true, questplan.BKListened
		r.stubLog("listen at the grill")
		return true
	}
	m[questplan.CustomBKSabotage] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(questplan.ObjCabbage, 1) {
			return false
		}
		r.inside, r.stage = true, questplan.BKSabotaged
		r.stubLog("cabbage down the hole")
		return true
	}
	m[questplan.CustomBKLeave] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.inside = false
		r.stubLog("out of the fortress")
		return true
	}
	return m
}

func (r *bkRig) run(t *testing.T) (questrunner.Result, *questrunner.Engine) {
	q := r.wrap(BlackKnightsFortress(), r.stubs())
	if q.ID() != questplan.BlackKnightsFortressID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	return e.Run(q, 20*time.Second), e
}

func TestBlackKnightsFortressRunsToCompleteFromAnEmptyAccount(t *testing.T) {
	// The bank holds food and nothing else: the disguise comes from the shops (the rig's provisioner answers a shop task with the
	// item), the cabbage from the field.
	r := newBKRig(t, 0, map[int]int{questplan.CoinsID: 2000}, map[int]int{questplan.ObjShark: 8})
	res, e := r.run(t)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.BKComplete || r.qp != 23 || r.held(questplan.CoinsID) != 4500 {
		t.Errorf("stage %d, quest points %d, coins %d", r.stage, r.qp, r.held(questplan.CoinsID))
	}
	if !r.worn[questplan.ObjIronChainbody] || !r.worn[questplan.ObjBronzeMedHelm] {
		t.Errorf("the disguise was not worn: %v", r.worn)
	}
	cs := e.Store.Record(questplan.BlackKnightsFortressID).Completions
	if len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	joined := strings.Join(r.log, "\n")
	order := []string{"talk Sir Amik -> stage 1", "stub provision blackknight", "wear 1101", "wear 1139", "pick a cabbage", "listen at the grill",
		"cabbage down the hole", "out of the fortress", "talk Sir Amik -> stage 4"}
	last := -1
	for _, want := range order {
		i := strings.Index(joined, want)
		if i < 0 || i < last {
			t.Errorf("%q is missing or out of order in the log:\n%s", want, joined)
			break
		}
		last = i
	}
}

func TestBlackKnightsFortressTakesWhatTheBankHoldsAndPicksNothing(t *testing.T) {
	bank := map[int]int{questplan.ObjIronChainbody: 1, questplan.ObjBronzeMedHelm: 1, questplan.ObjCabbage: 1, questplan.ObjShark: 6, questplan.ObjBass: 6}
	r := newBKRig(t, 1, map[int]int{questplan.CoinsID: 10}, bank)
	res, _ := r.run(t)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if strings.Contains(strings.Join(r.log, "\n"), "pick a cabbage") {
		t.Errorf("a cabbage was picked although the bank held one: %v", r.log)
	}
	if r.held(questplan.ObjShark) != 4 || r.held(questplan.ObjBass) != 4 {
		t.Errorf("food drawn: %d sharks %d bass, want 4 and 4", r.held(questplan.ObjShark), r.held(questplan.ObjBass))
	}
}

func TestBlackKnightsFortressResumesFromInsideAtEachStage(t *testing.T) {
	// Listened already (stage 2), inside, cabbage in the pack: the hole, out, Sir Amik.
	r := newBKRig(t, 2, map[int]int{questplan.ObjCabbage: 1}, nil)
	r.inside = true
	res, _ := r.run(t)
	if res.Outcome != questrunner.OutcomeComplete || r.stage != questplan.BKComplete {
		t.Fatalf("stage 2 inside: %s (%s) stage %d: %v", res.Outcome, res.Reason, r.stage, r.log)
	}
	if strings.Contains(strings.Join(r.log, "\n"), "stub provision") {
		t.Errorf("an account inside provisioned: %v", r.log)
	}
	// Potion ruined (stage 3), still inside: out first, then Sir Amik.
	r = newBKRig(t, 3, nil, nil)
	r.inside = true
	res, _ = r.run(t)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("stage 3 inside: %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	j := strings.Join(r.log, "\n")
	if strings.Index(j, "out of the fortress") > strings.Index(j, "talk Sir Amik -> stage 4") || !strings.Contains(j, "out of the fortress") {
		t.Errorf("the account talked to Sir Amik from inside the fortress: %v", r.log)
	}
	// Inside at stage 2 with the cabbage gone: out, and the field's cabbage again.
	r = newBKRig(t, 2, map[int]int{questplan.ObjShark: 4}, nil)
	r.inside = true
	r.worn[questplan.ObjIronChainbody], r.worn[questplan.ObjBronzeMedHelm] = true, true
	res, _ = r.run(t)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("stage 2 inside with no cabbage: %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	j = strings.Join(r.log, "\n")
	if strings.Index(j, "out of the fortress") > strings.Index(j, "pick a cabbage") || !strings.Contains(j, "pick a cabbage") {
		t.Errorf("no way out to the field: %v", r.log)
	}
}

func TestBlackKnightsFortressParksOnItsGates(t *testing.T) {
	for _, c := range []struct {
		name  string
		mk    func() *bkRig
		reuse string
	}{
		{"11 quest points", func() *bkRig { r := newBKRig(t, 0, nil, nil); r.qp = 11; return r }, "12 quest points"},
		{"29 hitpoints, not started", func() *bkRig {
			r := newBKRig(t, 0, nil, nil)
			r.hp = 29
			r.levels["hitpoints"] = 29
			return r
		}, "29 hitpoints"},
		{"29 hitpoints, started", func() *bkRig {
			r := newBKRig(t, 1, map[int]int{questplan.CoinsID: 10}, map[int]int{questplan.ObjIronChainbody: 1, questplan.ObjBronzeMedHelm: 1})
			r.hp = 29
			r.levels["hitpoints"] = 29
			return r
		}, "29 hitpoints"},
		{"no iron chainbody from any source", func() *bkRig {
			r := newBKRig(t, 1, map[int]int{questplan.CoinsID: 10}, nil)
			r.noChain = true
			return r
		}, "iron chainbody"},
		{"nothing to eat", func() *bkRig {
			r := newBKRig(t, 1, map[int]int{questplan.CoinsID: 10, questplan.ObjCabbage: 1, questplan.ObjIronChainbody: 1, questplan.ObjBronzeMedHelm: 1}, nil)
			r.worn[questplan.ObjIronChainbody], r.worn[questplan.ObjBronzeMedHelm] = true, true
			r.take(questplan.ObjIronChainbody, 1)
			r.take(questplan.ObjBronzeMedHelm, 1)
			return r
		}, "not going into the fortress"},
	} {
		r := c.mk()
		res, _ := r.run(t)
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, c.reuse) {
			t.Errorf("%s: the run ended %s (%s), want a park naming %q: %v", c.name, res.Outcome, res.Reason, c.reuse, r.log)
		}
		if strings.Contains(strings.Join(r.log, "\n"), "listen at the grill") || strings.Contains(strings.Join(r.log, "\n"), "talk Sir Amik -> stage 1") && r.qp < 12 {
			t.Errorf("%s: went on anyway: %v", c.name, r.log)
		}
	}
}

// One failed walk through the fortress takes the account out and parks the quest: it is not decided again, and the account is not
// left inside when the run ends.
func TestBlackKnightsFortressWalkThatFailsLeadsOutAndParks(t *testing.T) {
	bank := map[int]int{questplan.ObjIronChainbody: 1, questplan.ObjBronzeMedHelm: 1, questplan.ObjCabbage: 1, questplan.ObjShark: 6, questplan.ObjBass: 6}
	r := newBKRig(t, 1, map[int]int{questplan.CoinsID: 10}, bank)
	stubs := r.stubs()
	spies := 0
	stubs[questplan.CustomBKSpy] = func(e *questrunner.Engine, st questplan.Step) bool {
		spies++
		r.inside = true // it got in and a ladder would not take it
		r.stubLog("walk failed at a ladder")
		return false
	}
	q := r.wrap(BlackKnightsFortress(), stubs)
	res := r.engine().Run(q, 20*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "failed on its last try") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if spies != 1 {
		t.Errorf("the walk was tried %d times, want once", spies)
	}
	if r.inside {
		t.Errorf("the account was left inside the fortress: %v", r.log)
	}
	j := strings.Join(r.log, "\n")
	if strings.Index(j, "walk failed at a ladder") > strings.Index(j, "out of the fortress") || !strings.Contains(j, "out of the fortress") {
		t.Errorf("no way out after the failed walk: %v", r.log)
	}
}

// A walk that fails with the account on the open ground by the door (the walk itself walks clear on every way it ends there; this is
// the engine's second line, for the walk west that could not) is walked west once, then the quest waits; and a walk west that fails
// is not decided again.
func TestBlackKnightsFortressFailedWalkAtTheDoorGoesWestOnceThenParks(t *testing.T) {
	bank := map[int]int{questplan.ObjIronChainbody: 1, questplan.ObjBronzeMedHelm: 1, questplan.ObjCabbage: 1, questplan.ObjShark: 6, questplan.ObjBass: 6}
	for _, c := range []struct {
		name      string
		leaveOK   bool
		wantLeave int
		wantAt    tile.Tile
	}{
		{"the walk west works", true, 1, questplan.BKWestWaypoint},
		{"the walk west fails", false, 1, questplan.BKDoorApproach},
	} {
		r := newBKRig(t, 1, map[int]int{questplan.CoinsID: 10}, bank)
		stubs := r.stubs()
		spies, leaves := 0, 0
		stubs[questplan.CustomBKSpy] = func(e *questrunner.Engine, st questplan.Step) bool {
			spies++
			r.at = questplan.BKDoorApproach // the first crossing would not land
			r.stubLog("walk failed at the front door")
			return false
		}
		stubs[questplan.CustomBKLeave] = func(e *questrunner.Engine, st questplan.Step) bool {
			leaves++
			if c.leaveOK {
				r.at = questplan.BKWestWaypoint
			}
			r.stubLog("west of the fortress")
			return c.leaveOK
		}
		q := r.wrap(BlackKnightsFortress(), stubs)
		res := r.engine().Run(q, 20*time.Second)
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "failed on its last try") {
			t.Fatalf("%s: the run ended %s (%s): %v", c.name, res.Outcome, res.Reason, r.log)
		}
		if spies != 1 || leaves != c.wantLeave || r.at != c.wantAt {
			t.Errorf("%s: the walk was tried %d times (want 1), west %d times (want %d), the account ends at %v (want %v): %v",
				c.name, spies, leaves, c.wantLeave, r.at, c.wantAt, r.log)
		}
	}
}

// Every custom step the module can decide has an executor, and the factory's executors are exactly the module's customs.
func TestBlackKnightsFortressEveryCustomHasAnExecutor(t *testing.T) {
	q := BlackKnightsFortress()
	want := []string{questplan.CustomBKSpy, questplan.CustomBKSabotage, questplan.CustomBKLeave}
	got := make([]string, 0, len(q.Customs))
	for name := range q.Customs {
		got = append(got, name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("executors %v, want %v", got, want)
	}
	for name, fn := range q.Customs {
		if fn == nil {
			t.Errorf("executor %s is nil", name)
		}
	}
	// And Decide returns no other custom, across the states the planning tests sweep.
	names := map[string]bool{}
	for stage := 0; stage <= 4; stage++ {
		for _, inside := range []bool{false, true} {
			for _, cab := range []int{0, 1} {
				for _, worn := range []bool{false, true} {
					s := questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: stage, StageKnown: stage > 0, TileKnown: true,
						Tile: t3(2960, 3340, 0), InvIDs: map[int]int{questplan.ObjCabbage: cab, questplan.ObjShark: 6}, WornIDs: map[int]int{},
						Ran: map[string]bool{"provision:" + questplan.KitBlackKnight: true}, Failed: map[string]bool{},
						Levels: map[string]int{"defence": 30, "hitpoints": 50, "attack": 30, "strength": 30}, MaxHP: 50, QuestPoints: 20, FreeSlots: 10}
					if inside {
						s.Tile = t3(3020, 3516, 0)
					}
					if worn {
						s.WornIDs = map[int]int{questplan.ObjIronChainbody: 1, questplan.ObjBronzeMedHelm: 1}
					}
					if st := questplan.BlackKnightsFortress.Decide(s); st.Kind == questplan.StepCustom {
						names[st.Custom] = true
					}
				}
			}
		}
	}
	for name := range names {
		if q.Customs[name] == nil {
			t.Errorf("Decide returns the custom %q, which has no executor", name)
		}
	}
	if len(names) != 3 {
		t.Errorf("the sweep reached %v", names)
	}
}
