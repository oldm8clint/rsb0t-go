package questmods

// Monk's Friend and the Clock Tower through the real engine (Engine.Run) on a simulated session, in the manner of the fillers'
// (cooksheep_test.go): the module's own Decide runs against snapshots built from a fake pack and the quest tab's colour, and every
// step it asks for is performed by a stub that changes that world the way the server would. What is proven is the state machine
// under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death caps and the completion record):
// each scenario reaches Complete in the order the plan intends, a healthy run is never parked, and a leg that cannot be done
// parks the quest with its reason. What is not proven here is each custom's own live execution (the ladder under the ring of
// stones, the pen, the ogres): those are the real-data tests and the live logs.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

var mcAtBank = questplan.ArdougneSouthBank

// ---------------------------------------------------------------------------------------------------------------------
// Monk's Friend
// ---------------------------------------------------------------------------------------------------------------------

// mfApply plays the content's side of Monk's Friend on the rig: r.stage is %drunkmonkquest (50 and 60 as 60), the tab follows it.
func mfApply(r *csRig, answers map[string]func() bool) func(step questplan.Step) bool {
	setStage := func(n int) {
		r.stage = n
		if n == 60 {
			r.stage = questplan.MonkMendingCart // the journal renders 50 and 60 alike
		}
		status := quests.InProgress
		switch n {
		case questplan.MonkNotStarted:
			status = quests.NotStarted
		case questplan.MonkComplete:
			status = quests.Complete
		}
		r.setTab(questplan.MonksFriendName, status)
	}
	return func(step questplan.Step) bool {
		key := step.Custom
		switch step.Kind {
		case questplan.StepProvision:
			key = "provision:" + step.KitName
		case questplan.StepTalk:
			switch step.Stop.ID {
			case questplan.NpcBrotherOmad:
				switch r.stage {
				case questplan.MonkNotStarted:
					setStage(questplan.MonkSpokeOmad)
				case questplan.MonkSpokeOmad:
					if r.held(questplan.ObjChildsBlanket) > 0 {
						r.take(questplan.ObjChildsBlanket, 1)
						setStage(questplan.MonkRetrievedBlanket)
					}
				case questplan.MonkRetrievedBlanket:
					setStage(questplan.MonkLookingCedric)
				}
			case questplan.NpcBrotherCedric:
				switch r.stage {
				case questplan.MonkLookingCedric:
					setStage(questplan.MonkFindingWater)
				case questplan.MonkFindingWater:
					if r.held(questplan.ObjJugOfWater) > 0 {
						r.take(questplan.ObjJugOfWater, 1)
						setStage(questplan.MonkMendingCart)
					}
				case questplan.MonkMendingCart:
					if r.held(questplan.ObjLogs) > 0 {
						r.take(questplan.ObjLogs, 1)
						setStage(questplan.MonkFixedCart)
					}
				}
			}
			return true
		case questplan.StepScanBank, questplan.StepDeposit:
			return true
		}
		if key == questplan.CustomMonkParty {
			setStage(questplan.MonkComplete)
			return true
		}
		f, ok := answers[key]
		if !ok {
			r.t.Errorf("the scenario has no answer for the step %q", key)
			return false
		}
		return f()
	}
}

// The whole quest in the content's order, every executor a stub: the blanket from the cave, the jug and the log from the kits.
func TestMonksFriendReachesCompleteThroughTheEngine(t *testing.T) {
	r := newCSRig(t, mcAtBank)
	r.setTab(questplan.MonksFriendName, quests.NotStarted)
	r.stageKnown = true
	res := r.run(MonksFriend(), mfApply(r, map[string]func() bool{
		questplan.CustomMonkBlanket:         func() bool { r.give(questplan.ObjChildsBlanket, 1); return true },
		"provision:" + questplan.KitMonkJug: func() bool { r.give(questplan.ObjJug, 1); return true },
		questplan.CustomMonkJug: func() bool {
			r.take(questplan.ObjJug, 1)
			r.give(questplan.ObjJugOfWater, 1)
			return true
		},
		"provision:" + questplan.KitMonkLogs: func() bool { r.give(questplan.ObjLogs, 1); return true },
	}))
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	// (The rig collapses a repeat of one description: Omad's hand-in and his ask about Cedric are one entry.)
	r.wantSteps("talk to Brother Omad", "monk-blanket", "talk to Brother Omad", "talk to Brother Cedric",
		"provision monk-jug", "monk-jug", "talk to Brother Cedric", "provision monk-logs", "talk to Brother Cedric", "monk-party")
	if got := r.e.Store.Record("drunkmonk").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjChildsBlanket, questplan.ObjJug, questplan.ObjJugOfWater, questplan.ObjLogs} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the party", id)
		}
	}
}

// A blanket that never comes (the cave's ladder never grows, the thieves drive the account out) is the quest's own: the engine
// parks it on the watchdog and the side store has the park.
func TestMonksFriendABlanketThatNeverComesParks(t *testing.T) {
	r := newCSRig(t, mcAtBank)
	r.setTab(questplan.MonksFriendName, quests.InProgress)
	r.stage, r.stageKnown = questplan.MonkSpokeOmad, true
	res := r.run(MonksFriend(), mfApply(r, map[string]func() bool{questplan.CustomMonkBlanket: func() bool { return false }}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "no progress") {
		t.Fatalf("the run ended %s (%s), want a park on no progress: %v", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("drunkmonk", time.Now()); !parked {
		t.Error("the park is not in the side store")
	}
}

// A custom that knows the way will not mend (the ladder never grew) asks for a long park and is run once.
func TestMonksFriendARequestedParkRunsTheLegOnce(t *testing.T) {
	r := newCSRig(t, mcAtBank)
	r.setTab(questplan.MonksFriendName, quests.InProgress)
	r.stage, r.stageKnown = questplan.MonkSpokeOmad, true
	runs := 0
	res := r.run(MonksFriend(), mfApply(r, map[string]func() bool{questplan.CustomMonkBlanket: func() bool {
		runs++
		r.e.RequestPark("the ladder under the ring of stones never took the account into the thieves' cave", 30*time.Minute)
		return false
	}}))
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "ring of stones") {
		t.Fatalf("outcome %s after %d runs: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("drunkmonk", time.Now().Add(29*time.Minute)); !parked {
		t.Error("not parked for the half hour")
	}
	if parked, _ := r.e.Store.Parked("drunkmonk", time.Now().Add(31*time.Minute)); parked {
		t.Error("parked past the half hour")
	}
}

// Two deaths in the cave park the quest for a day (MaxDeaths 2).
func TestMonksFriendTwoDeathsParkItForADay(t *testing.T) {
	r := newCSRig(t, mcAtBank)
	r.e.D.Chat = chatlog.New()
	r.setTab(questplan.MonksFriendName, quests.InProgress)
	r.stage, r.stageKnown = questplan.MonkSpokeOmad, true
	r.e.Store.NoteDeath("drunkmonk", time.Now().Add(-time.Hour))
	res := r.run(MonksFriend(), mfApply(r, map[string]func() bool{questplan.CustomMonkBlanket: func() bool {
		r.e.D.Chat.Record("Oh dear, you are dead!")
		return false
	}}))
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "the quest's cap is 2") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("drunkmonk", time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The Clock Tower
// ---------------------------------------------------------------------------------------------------------------------

// ctRig is the Clock Tower's state on the rig: r.stage is the mask of the colours placed (CogAllPlaced when all four are).
type ctRig struct {
	*csRig
	mask      int
	started   bool
	poisoned  bool
	fetches   []string
	redRuns   int
	killOnRed bool
}

func newCTRig(t *testing.T) *ctRig {
	r := &ctRig{csRig: newCSRig(t, questplan.KojoAnchor), started: false}
	r.stageKnown = true
	r.setTab(questplan.ClockTowerName, quests.NotStarted)
	return r
}

func (r *ctRig) sync() {
	r.stage = r.mask
	status := quests.InProgress
	if !r.started {
		status = quests.NotStarted
	}
	if r.mask == 15 {
		r.stage = questplan.CogAllPlaced
	}
	r.setTab(questplan.ClockTowerName, status)
}

func (r *ctRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepTalk:
		switch {
		case !r.started:
			r.started = true
			r.sync()
		case r.mask == 15:
			r.setTab(questplan.ClockTowerName, quests.Complete)
		}
		return true
	case questplan.StepProvision:
		switch step.KitName {
		case questplan.KitCogFood:
			r.give(questplan.ObjShark, 3)
		case questplan.KitCogBucket:
			r.give(questplan.ObjBucket, 1)
		case questplan.KitCogFoodRed:
		default:
			r.t.Errorf("unexpected kit %q", step.KitName)
		}
		return true
	case questplan.StepUseOn:
		r.take(questplan.ObjBucket, 1)
		r.give(questplan.ObjBucketOfWater, 1)
		return true
	case questplan.StepGrabGround:
		r.give(questplan.ObjRatPoison, 1)
		return true
	case questplan.StepOpHeld:
		r.take(step.ItemID, 1)
		return true
	case questplan.StepScanBank, questplan.StepDeposit:
		return true
	}
	fetch := func(name string) bool {
		c, _ := questplan.CogByName(name)
		r.give(c.Obj, 1)
		r.fetches = append(r.fetches, name)
		return true
	}
	switch step.Custom {
	case questplan.CustomCogBlue:
		return fetch("blue")
	case questplan.CustomCogBlack:
		r.take(questplan.ObjBucketOfWater, 1)
		r.give(questplan.ObjBucket, 1)
		return fetch("black")
	case questplan.CustomCogWhite:
		if !r.poisoned {
			r.take(questplan.ObjRatPoison, 1)
			r.poisoned = true
		}
		return fetch("white")
	case questplan.CustomCogRed:
		r.redRuns++
		if r.killOnRed {
			r.e.D.Chat.Record("Oh dear, you are dead!")
			return false
		}
		return fetch("red")
	case questplan.CustomCogPlace:
		c, _ := questplan.CogByObj(step.ItemID)
		r.take(c.Obj, 1)
		r.mask |= c.Bit
		r.sync()
		return true
	}
	r.t.Errorf("unexpected step %q", step.Describe())
	return false
}

// The whole quest in the plan's order: the food once, blue, then black with its bucket and well, white with its poison, red, and
// Kojo's last talk.
func TestClockTowerReachesCompleteThroughTheEngine(t *testing.T) {
	r := newCTRig(t)
	res := r.run(ClockTower(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	r.wantSteps("talk to Brother Kojo", "provision cog-food", "cog-blue", "cog-place", "provision cog-bucket", "use Bucket on Well",
		"cog-black", "cog-place", "grab Rat poison", "cog-white", "cog-place", "cog-red", "cog-place", "talk to Brother Kojo")
	if !slices.Equal(r.fetches, []string{"blue", "black", "white", "red"}) {
		t.Errorf("fetches %v", r.fetches)
	}
	if got := r.e.Store.Record("cog").Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	// The food pass asks for three sharks, as an optional kit with the quest's keep list.
	kit := r.stepNamed("provision cog-food")
	if len(kit.Kit) != 1 || !kit.Kit[0].Optional || kit.Kit[0].Qty != 3 || !kit.BankSet || kit.Bank != questplan.ArdougneSouthBank || len(kit.KeepIDs) == 0 {
		t.Errorf("the food pass %+v", kit)
	}
}

// The red leg is not run without food: a pack with sharks goes in, and one without parks the quest at the ogres for two hours with
// its reason, the three safe cogs done.
func TestClockTowerWithoutFoodDoesTheSafeCogsAndParksAtTheOgres(t *testing.T) {
	r := newCTRig(t)
	// The food kit finds nothing: the sharks never arrive.
	apply := func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && (step.KitName == questplan.KitCogFood || step.KitName == questplan.KitCogFoodRed) {
			return true
		}
		return r.apply(step)
	}
	res := r.run(ClockTower(), apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "ogres") {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.mask != questplan.CogBitBlue|questplan.CogBitBlack|questplan.CogBitWhite || r.redRuns != 0 {
		t.Errorf("mask %04b, %d red runs", r.mask, r.redRuns)
	}
	if parked, _ := r.e.Store.Parked("cog", time.Now().Add(119*time.Minute)); !parked {
		t.Error("not parked for the two hours")
	}
	if parked, _ := r.e.Store.Parked("cog", time.Now().Add(121*time.Minute)); parked {
		t.Error("parked past the two hours")
	}
}

// A red leg that cannot be done (the ogres never stand off the cog) is run once and parks the quest on its own request; a death
// in it parks the quest for a day, the step's death cap being one.
func TestClockTowerTheOgreCaveParksOnARequestAndOnADeath(t *testing.T) {
	r := newCTRig(t)
	r.mask = questplan.CogBitBlue | questplan.CogBitBlack | questplan.CogBitWhite
	r.started = true
	r.sync()
	r.give(questplan.ObjShark, 3)
	r.e.D.Chat = chatlog.New()
	res := r.run(ClockTower(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomCogRed {
			r.redRuns++
			r.e.RequestPark("ogres stand by the red cog", 30*time.Minute)
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || r.redRuns != 1 || !strings.Contains(res.Reason, "ogres stand by the red cog") {
		t.Fatalf("outcome %s after %d red runs: %q (%v)", res.Outcome, r.redRuns, res.Reason, r.log)
	}

	r = newCTRig(t)
	r.mask = questplan.CogBitBlue | questplan.CogBitBlack | questplan.CogBitWhite
	r.started = true
	r.sync()
	r.give(questplan.ObjShark, 3)
	r.e.D.Chat = chatlog.New()
	r.killOnRed = true
	res = r.run(ClockTower(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || r.redRuns != 1 || !strings.Contains(res.Reason, `death(s) on "cog-red"`) {
		t.Fatalf("outcome %s after %d red runs: %q (%v)", res.Outcome, r.redRuns, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked("cog", time.Now().Add(23*time.Hour)); !parked {
		t.Error("not parked for the day")
	}
	if parked, _ := r.e.Store.Parked("cog", time.Now().Add(25*time.Hour)); parked {
		t.Error("parked past the day")
	}
}

// A spare cog (the spindle already filled) is dropped, not carried round and not fetched for.
func TestClockTowerDropsASpareCog(t *testing.T) {
	r := newCTRig(t)
	r.started = true
	r.mask = questplan.CogBitRed
	r.sync()
	r.give(questplan.ObjCogRed, 1)
	r.give(questplan.ObjShark, 3)
	res := r.run(ClockTower(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision && step.KitName == questplan.KitCogFood {
			return true
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.redRuns != 0 {
		t.Errorf("the red cog was fetched again (%d runs)", r.redRuns)
	}
	r.stepNamed("Drop Cog")
}

// Every custom the two plans can ask for has an executor in the quest the factory builds, and no executor is orphaned.
func TestMonkCogEveryCustomStepHasAnExecutor(t *testing.T) {
	for _, c := range []struct {
		q    questrunner.Quest
		want []string
	}{
		{MonksFriend(), []string{questplan.CustomMonkBlanket, questplan.CustomMonkJug, questplan.CustomMonkParty}},
		{ClockTower(), []string{questplan.CustomCogBlue, questplan.CustomCogBlack, questplan.CustomCogWhite, questplan.CustomCogRed, questplan.CustomCogPlace}},
	} {
		if len(c.q.Customs) != len(c.want) {
			t.Errorf("%s has %d executors, want %d", c.q.ID(), len(c.q.Customs), len(c.want))
		}
		for _, name := range c.want {
			if c.q.Customs[name] == nil {
				t.Errorf("%s has no executor for %q", c.q.ID(), name)
			}
		}
	}
}

// The jug is filled at the sink nearest the account, and the table the order is taken from is left as it was.
func TestMonkSinksAreTriedNearestFirst(t *testing.T) {
	before := append([]tile.Tile(nil), questplan.MonkSinks...)
	for _, c := range []struct {
		from tile.Tile
		want tile.Tile
	}{
		{questplan.ArdougneSouthBank, tile.Tile{X: 2658, Z: 3273}}, // the town's own sink, ten tiles from the bank
		{questplan.CedricAnchor, tile.Tile{X: 2658, Z: 3273}},      // forty-four, against sixty-four to the monastery's
		{questplan.OmadAnchor, tile.Tile{X: 2610, Z: 3195}},        // fourteen from the monastery's
		{tile.Tile{X: 2591, Z: 3180}, tile.Tile{X: 2591, Z: 3188}}, // beside the guard house's
	} {
		got := monkSinkOrder(c.from)
		if len(got) != len(before) || got[0] != c.want {
			t.Errorf("from %v the sinks are %v, want %v first", c.from, got, c.want)
		}
	}
	if !slices.Equal(questplan.MonkSinks, before) {
		t.Errorf("the table was sorted in place: %v", questplan.MonkSinks)
	}
}
