package questmods

// The Tourist Trap's floors, through the real engine on the strict rig of touristtrap_engine_test.go: the replay that is run once and not for ever, the
// hitpoints a fall needs, the food the guards are met with, and the pack room the camp's strip needs. What each proves is named on the test.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/levels"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// trapSetHP sets the account's current hitpoints, as the server's UPDATE_STAT does (the experience keeps the base level at 70).
func trapSetHP(r *trapRig, cur int) {
	r.t.Helper()
	p := packet.NewWriter(6)
	p.P1(byte(gamestate.HitpointsStat))
	p.P4(int32(levels.XPAtLevel(70)))
	p.P1(byte(cur))
	r.sim.State().Handle(serverprot.UPDATE_STAT, p.Data[:p.Pos])
}

// runCounting runs the module with apply, counting each custom and kit the plan asked for (a repeat of one step counts every time, as the engine runs it).
func (r *trapRig) runCounting(apply func(step questplan.Step) bool) (questrunner.Result, map[string]int) {
	counts := map[string]int{}
	res := r.run(TouristTrap(), func(step questplan.Step) bool {
		switch step.Kind {
		case questplan.StepCustom:
			counts[step.Custom]++
		case questplan.StepProvision:
			counts["provision:"+step.KitName]++
		default:
			counts[string(step.Kind)]++
		}
		return apply(step)
	})
	return res, counts
}

// rescueCamp is an account in the camp's yard at the rescue's stage with the lift holding Ana: the winch is the next piece of work.
func rescueCamp(t *testing.T) *trapRig {
	t.Helper()
	r := newTrapRig(t, questplan.TtRescue, tile.Tile{X: 3290, Z: 3025})
	r.give(questplan.CoinsID, 700)
	r.give(questplan.ObjTtMetalKey, 1)
	r.give(questplan.ObjTtSkin4, 3)
	r.give(385, 6)
	for _, id := range []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots} {
		r.give(id, 1)
		r.wear(id)
	}
	r.ana = "lift"
	return r
}

// A winch that comes up empty is answered by the replay, which ends with the winch itself; the old winch flag (which the engine keeps for the whole run)
// does not send the account round the mines again, and the rest of the rescue runs.
func TestTouristTrapAFailedWinchIsReplayedOnceAndTheRunCompletes(t *testing.T) {
	r := rescueCamp(t)
	r.winchFails = 1
	res, n := r.runCounting(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.tail(8))
	}
	if r.recaptures != 1 || n[questplan.CustomTtRecapture] != 1 {
		t.Errorf("%d replays (engine %d), want exactly one", r.recaptures, n[questplan.CustomTtRecapture])
	}
	if n[questplan.CustomTtWinch] != 1 {
		t.Errorf("the winch was asked for %d times, want once (the replay ends with it)", n[questplan.CustomTtWinch])
	}
	r.wantInOrder(questplan.CustomTtWinch, questplan.CustomTtRecapture, questplan.CustomTtEscape, "talk to Irena")
}

// A cart that took Ana's barrel and did not go is answered by the replay (nothing to find at the winch), which re-arms the cart: the ride is tried again
// and the run completes, the old failure flag notwithstanding.
func TestTouristTrapACartThatTookAnaIsReplayedAndRetried(t *testing.T) {
	r := rescueCamp(t)
	r.ana = ""
	r.give(questplan.ObjTtAnaBarrel, 1)
	r.escapeFails = 1
	res, n := r.runCounting(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.tail(8))
	}
	if n[questplan.CustomTtEscape] != 2 || n[questplan.CustomTtRecapture] != 1 || n[questplan.CustomTtWinch] != 0 {
		t.Errorf("escape %d, replay %d, winch %d: want 2, 1, 0", n[questplan.CustomTtEscape], n[questplan.CustomTtRecapture], n[questplan.CustomTtWinch])
	}
}

// A replay that fails parks the quest with the reason (for hours), where the old plan went round it again.
func TestTouristTrapAReplayThatFailsParksTheQuest(t *testing.T) {
	r := rescueCamp(t)
	r.winchFails, r.recaptureFails = 1, 1
	res, n := r.runCounting(r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "replay") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if n[questplan.CustomTtRecapture] != 1 || n[questplan.CustomTtWinch] != 1 {
		t.Errorf("replay %d, winch %d: want one of each", n[questplan.CustomTtRecapture], n[questplan.CustomTtWinch])
	}
	if parked, _ := r.e.Store.Parked(questplan.TouristTrapID, time.Now().Add(2*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
}

// Ana lost again after the replay (the cart took her barrel and the ride failed) parks the quest, not a second replay.
func TestTouristTrapAnaLostAfterTheReplayParksTheQuest(t *testing.T) {
	r := rescueCamp(t)
	r.winchFails, r.escapeFails = 1, 1
	res, n := r.runCounting(r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "lost again") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if n[questplan.CustomTtRecapture] != 1 {
		t.Errorf("%d replays, want one", n[questplan.CustomTtRecapture])
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Hitpoints under a fall.
// ---------------------------------------------------------------------------------------------------------------

// A climb or a squeeze that can kill waits for hitpoints above the fall and a margin, eating if it can, and does not try below them.
func TestTouristTrapRestsForHitpointsBeforeAFall(t *testing.T) {
	old := ttRestLimit
	ttRestLimit = 30 * time.Millisecond
	defer func() { ttRestLimit = old }()
	r := newTrapRig(t, questplan.TtGivenPine, tile.Tile{X: 3295, Z: 9420})
	d := r.e.D
	trapSetHP(r, 3)
	before := len(r.sim.Sent())
	// The lower cart (a miss costs 2) at 3 hitpoints: no attempt is made (no loc is found: there is no loc index in this rig, which would panic if it were).
	if ttCartRide(d, true) {
		t.Error("the cart was ridden at 3 hitpoints")
	}
	// The cell's rock (a slip costs 7), from a tile east of the first rock.
	if ttClimbNextRock(d, tile.Tile{X: 3283, Z: 3032}) {
		t.Error("a rock was climbed at 3 hitpoints")
	}
	if got := len(r.sim.Sent()); got != before {
		t.Errorf("%d packets were sent by attempts that should not have been made", got-before)
	}
	// Hitpoints that come back: the rest ends when they are above the floor, after the resting (a point a delay in this rig).
	hp, calls := 3, 0
	d.Delay = func(time.Duration) bool { hp++; calls++; trapSetHP(r, hp); return true }
	if !ttRestAbove(d, ttFallDamage+ttHPMargin, time.Minute) {
		t.Error("the rest did not end with the hitpoints back")
	}
	if got, _ := questrunner.Hitpoints(d); got <= ttFallDamage+ttHPMargin || calls != 8 {
		t.Errorf("rested to %d hitpoints in %d delays, want 11 in 8", got, calls)
	}
	// Hitpoints that are above the floor are not waited on.
	calls = 0
	if !ttRestAbove(d, ttFallDamage+ttHPMargin, time.Minute) || calls != 0 {
		t.Errorf("a healthy account waited %d delays", calls)
	}
}

// A cart step that cannot be risked parks the quest with the hitpoints in the reason, through the engine (the plan alone would send it back to the cart).
func TestTouristTrapParksAnAccountTooWeakForTheCart(t *testing.T) {
	old := ttRestLimit
	ttRestLimit = 20 * time.Millisecond
	defer func() { ttRestLimit = old }()
	r := newTrapRig(t, questplan.TtGivenPine, tile.Tile{X: 3295, Z: 9420})
	r.give(questplan.CoinsID, 700)
	r.give(questplan.ObjTtMetalKey, 1)
	r.give(questplan.ObjTtSkin4, 3)
	r.give(questplan.ObjTtBarrel, 1) // no food in the pack: the rig has no object config to eat by
	for _, id := range []int{questplan.ObjTtSlaveShirt, questplan.ObjTtSlaveRobe, questplan.ObjTtSlaveBoots} {
		r.give(id, 1)
		r.wear(id)
	}
	trapSetHP(r, 3)
	// The real cart executor under the engine, with the eating switched off (the rig has no object config to eat by): a decision that asks for it once.
	q := TouristTrap()
	q.Module.Sustain = questplan.Sustain{}
	q.Module.Decide = func(questplan.Snapshot) questplan.Step {
		return questplan.Step{Kind: questplan.StepCustom, Custom: questplan.CustomTtCart, Qty: 1, Reason: "the lower cart"}
	}
	res := r.e.Run(q, time.Minute)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "hitpoints") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.tail(6))
	}
	if parked, _ := r.e.Store.Parked(questplan.TouristTrapID, time.Now().Add(2*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
}

// ---------------------------------------------------------------------------------------------------------------
// Food.
// ---------------------------------------------------------------------------------------------------------------

// provisionWithoutFood is the bank, the mule and the shelf with everything but food.
func (r *trapRig) provisionWithoutFood(step questplan.Step) bool {
	var kit []questplan.KitLine
	for _, l := range step.Kit {
		if l.ID != 385 && l.ID != 365 {
			kit = append(kit, l)
		}
	}
	step.Kit = kit
	r.provision(step)
	return false // the kit's food lines had no source: the round ends blocked
}

// No food in the bank or the mule parks the account at the shop, with the food in the reason, before it crosses.
func TestTouristTrapParksAtTheShopWhenNothingCanFeedIt(t *testing.T) {
	r := newTrapRig(t, questplan.TtNotStarted, tile.Tile{X: 3269, Z: 3167})
	r.startKit(900)
	res := r.run(TouristTrap(), func(step questplan.Step) bool {
		switch {
		case step.Kind == questplan.StepProvision:
			return r.provisionWithoutFood(step)
		case step.Kind == questplan.StepCustom && step.Custom == questplan.CustomTtDress:
			r.dress(step.Qty)
		default:
			r.t.Errorf("the unfed account was sent on: %s", step.Describe())
			return false
		}
		return true
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no food") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if r.here().Z < 3117 {
		t.Errorf("the account crossed the pass: at %v", r.here())
	}
}

// An account south of the pass that runs out of food turns for the bank (the camp's strip left it bare-handed for the guards) and goes on with the
// food it fetched; with none to be had it parks, on the shop side.
func TestTouristTrapTurnsForFoodSouthOfThePass(t *testing.T) {
	setup := func() *trapRig {
		r := newTrapRig(t, questplan.TtEnteredCmp, tile.Tile{X: 3290, Z: 3025})
		r.give(questplan.CoinsID, 700)
		r.give(questplan.ObjTtMetalKey, 1)
		r.give(questplan.ObjTtSkin4, 3)
		r.give(questplan.ObjTtPass, 1)
		for _, id := range []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots} {
			r.give(id, 2)
			r.wear(id)
		}
		return r
	}
	r := setup()
	res, n := r.runCounting(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	r.wantInOrder(questplan.CustomTtGateOut, questplan.CustomTtNorth, "provision tt-trip", questplan.CustomTtSouth)
	if n["provision:"+questplan.KitTtTrip] < 1 {
		t.Error("no food kit was asked for")
	}
	// With nothing to eat anywhere, the account that turned back parks at the shop.
	r = setup()
	res = r.run(TouristTrap(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision {
			return r.provisionWithoutFood(step)
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no food") {
		t.Fatalf("the unfed run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
}

// An account whose food has dwindled below what the camp's guards are priced on restocks before the gate and goes on when it has: the first crossing
// has the same food floor and the same pricing, so the order is the shop's kit, the pass, the gate.
func TestTouristTrapRestocksFoodBeforeTheCampWhenTheGuardsAreNotFit(t *testing.T) {
	r := newTrapRig(t, questplan.TtKilledCapt, tile.Tile{X: 3272, Z: 3029})
	for skill, lvl := range map[string]int{"attack": 40, "strength": 40, "defence": 40, "hitpoints": 45} {
		tfSeed(r.csRig, skill, lvl)
	}
	r.give(questplan.CoinsID, 700)
	r.give(questplan.ObjTtMetalKey, 1)
	r.give(questplan.ObjTtSkin4, 3)
	r.give(385, 1) // one shark: a level 40 account is not priced for a guard on one
	for _, id := range []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots} {
		r.give(id, 2)
		r.wear(id)
	}
	// The premise: the model has this account lose to, or leave no bite over against, one rowdy guard on that food, and is content with six sharks.
	if ok, _ := questplan.TtCampFit(r.e.Snapshot(0)); ok {
		t.Fatal("the account is priced for a guard on one shark: the test's premise is wrong")
	}
	r.give(385, 5)
	if ok, why := questplan.TtCampFit(r.e.Snapshot(0)); !ok {
		t.Fatalf("the account is not priced for a guard on six sharks either: %s", why)
	}
	r.take(385, 5)
	res := r.run(TouristTrap(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.tail(8))
	}
	r.wantInOrder(questplan.CustomTtNorth, "provision tt-trip", questplan.CustomTtSouth, questplan.CustomTtGateIn)
}

// ---------------------------------------------------------------------------------------------------------------
// The pack's room for the strip.
// ---------------------------------------------------------------------------------------------------------------

// A full set of gear (ten pieces) worn at the shop: the first kit leaves the strip its room (no bars and no hammer, less food), the run reaches the camp,
// the strip fits, the plans are fetched, and the papers the dart wants come on a second trip. The rig refuses a strip with no room.
func TestTouristTrapAFullSetOfGearFitsTheStripAndFetchesThePapersLater(t *testing.T) {
	r := newTrapRig(t, questplan.TtNotStarted, tile.Tile{X: 3269, Z: 3167})
	r.give(questplan.CoinsID, 900)
	gear := []int{1333, 1127, 1163, 1052, 1712, 1201, 1079, 7462, 4131, 2550}
	for _, id := range gear {
		r.give(id, 1)
		r.wear(id)
	}
	res, n := r.runCounting(r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.tail(10))
	}
	// The first kit left out the bars and the hammer (they were fetched on the second trip, after the plans).
	first := r.stepNamed("provision tt-trip")
	for _, l := range first.Kit {
		if l.ID == questplan.ObjBronzeBar || l.ID == questplan.ObjHammer {
			t.Errorf("the first kit, with ten pieces of gear worn, asked for %s", l.Name)
		}
	}
	if n["provision:"+questplan.KitTtTrip] < 2 {
		t.Errorf("the papers were not fetched on a second trip: %v", r.described())
	}
	// All the gear came off at the camp and none of it was lost: the second trip's bank visit banked what the strip had left in the pack.
	for _, id := range gear {
		if r.held(id)+r.banked[id] < 1 && !r.wornID(id) {
			t.Errorf("gear %d is gone", id)
		}
	}
}
