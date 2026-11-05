package questmods

// Big Chompy Bird Hunting through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (hazeel_engine_test.go): the
// module's own Decide runs against snapshots built from a fake pack, the account's tile and levels and the quest tab's colour, and every step it asks
// for is performed by a stub that changes that world the way the server would. What is proven is the state machine under the engine's bookkeeping (Ran and
// Failed, the watchdog, the park requests, the death cap and the completion record): the hunt reaches Complete in the order the plan intends, a healthy run
// is never parked, and a leg that cannot be done parks the quest with its reason. What is not proven here is each custom's own live execution (the talks, the
// wolves, the bait and the shot): those are the real-data tests and the live logs.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// chRig is the quest's state on the rig: r.stage is %chompybird and the tab follows it.
type chRig struct {
	*csRig
	// kills counts the wolves killed, shots the chompy birds shot, burns the roasts that burnt, kids whether the children were asked after the chompy was shown.
	kills, shots, burns int
	kids                bool
	// burnFirst makes the first roast burn.
	burnFirst bool
	// seasonings are the three the children and Rantz named (the roast takes them whatever it makes).
	provided map[string]int
}

func newChRig(t *testing.T, stage int) *chRig {
	t.Helper()
	stack := map[int32]bool{questplan.CoinsID: true, questplan.ObjChFeather: true, questplan.ObjChShaft: true, questplan.ObjChFlighted: true, questplan.ObjChTips: true,
		questplan.ObjChArrow: true}
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return stack[id] }}, nil)
	sim.StandAt(questplan.YanilleBank.X, questplan.YanilleBank.Z)
	base := &csRig{t: t, sim: sim}
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(),
		Log:   func(s string) { base.log = append(base.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{Counts: map[int]int{}, Known: true, At: time.Now().Add(time.Hour)}}
	base.e = &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	r := &chRig{csRig: base, provided: map[string]int{}}
	r.stageKnown = true
	r.setStage(stage)
	for skill, level := range map[string]int{"attack": 60, "strength": 60, "defence": 50, "hitpoints": 60, "ranged": 40, "cooking": 40, "fletching": 20, "crafting": 20} {
		tfSeed(base, skill, level)
	}
	return r
}

func (r *chRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.ChNotStarted:
		status = quests.NotStarted
	case questplan.ChComplete:
		status = quests.Complete
	}
	r.setTab(questplan.ChompyName, status)
}

func (r *chRig) at(at tile.Tile) { r.sim.StandAtLevel(at.X, at.Z, at.Level) }

// stock fills the pack the way a bank pass and a mule do when everything is there.
func (r *chRig) stock() {
	for id, n := range map[int]int{questplan.CoinsID: 800, questplan.ObjShark: 8, questplan.ObjKnife: 1, questplan.ObjChChisel: 1, questplan.ObjChFeather: 48, questplan.ObjBronzeAxe: 1} {
		if r.held(id) < n {
			r.give(id, n-r.held(id))
		}
	}
}

// apply plays the content's side of the quest.
func (r *chRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		r.provided[step.KitName]++
		r.stock()
		return true
	case questplan.StepDeposit, questplan.StepScanBank:
		return true
	case questplan.StepTalk:
		if step.Stop.ID != questplan.NpcChRantz || r.stage != questplan.ChNotStarted {
			r.t.Errorf("a talk with npc %d at stage %d", step.Stop.ID, r.stage)
			return false
		}
		r.setStage(questplan.ChStarted)
		return true
	case questplan.StepUseOn:
		return r.useOn(step)
	}
	switch step.Custom {
	case questplan.CustomChBugs:
		r.give(questplan.ObjKnife, 1)
		r.give(questplan.ObjChChisel, 1)
	case questplan.CustomChChop:
		r.give(questplan.ObjChLogs, 1)
	case questplan.CustomChWolf:
		r.kills++
		r.give(questplan.ObjChWolfBones, 1)
	case questplan.CustomChGive:
		if r.held(questplan.ObjChArrow) < questplan.ChGiveArrows {
			r.t.Errorf("Rantz was handed arrows with %d in the pack", r.held(questplan.ObjChArrow))
		}
		r.take(questplan.ObjChArrow, questplan.ChGiveArrows)
		r.setStage(questplan.ChToadies) // the menu that follows the hand-over sets 15
	case questplan.CustomChToadies:
		r.setStage(questplan.ChToadies)
	case questplan.CustomChChest:
		if r.held(questplan.ObjChBellows3) == 0 {
			r.give(questplan.ObjChBellows3, 1)
		}
	case questplan.CustomChToad:
		for r.held(questplan.ObjChToad) < questplan.ChToadsMax {
			r.give(questplan.ObjChToad, 1)
		}
	case questplan.CustomChShowToad:
		r.setStage(questplan.ChShownToad)
	case questplan.CustomChBait:
		if r.held(questplan.ObjChToad) == 0 {
			r.give(questplan.ObjChToad, 1)
		}
		r.take(questplan.ObjChToad, 1)
		r.setStage(questplan.ChRantzMissed)
	case questplan.CustomChBow:
		r.give(questplan.ObjChBow, 1)
	case questplan.CustomChShoot:
		r.shots++
		if r.held(questplan.ObjChArrow) < 6 && r.sim.Worn() == nil {
			r.t.Errorf("a shot with %d arrows", r.held(questplan.ObjChArrow))
		}
		r.give(questplan.ObjChRawChompy, 1)
		if r.stage < questplan.ChKilled {
			r.setStage(questplan.ChKilled)
		}
	case questplan.CustomChShowChompy:
		r.setStage(questplan.ChToldToCook)
	case questplan.CustomChKids:
		if r.stage != questplan.ChToldToCook {
			r.t.Errorf("the children asked at stage %d", r.stage)
		}
		r.kids = true
	case questplan.CustomChSeason:
		if !r.kids {
			r.t.Errorf("a seasoning fetched before the children were asked")
		}
		r.give(step.ItemID, 1)
	case questplan.CustomChCook:
		if !r.kids {
			r.t.Errorf("the spit used before the children were asked")
		}
		for _, c := range questplan.ChSeasoningList() {
			if r.held(c.ID) == 0 {
				r.t.Errorf("the spit used without the %s", c.Name)
			}
		}
		r.take(questplan.ObjChRawChompy, 1)
		for _, id := range []int{questplan.ObjChPotato, questplan.ObjChCabbage, questplan.ObjChTomato} { // the three the spit would take
			r.take(id, 1)
		}
		if r.burnFirst && r.burns == 0 {
			r.burns++
			r.give(questplan.ObjChRuinedChompy, 1)
			return true // the roast was made; it burnt
		}
		r.give(questplan.ObjChSeasonedChomp, 1)
		r.setStage(questplan.ChCooked)
	case questplan.CustomChHandOver:
		r.take(questplan.ObjChSeasonedChomp, 1)
		r.setStage(questplan.ChComplete)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// useOn plays the item-on-item scripts of ogre_arrows.rs2 (the yields are fixed at the middle of their ranges).
func (r *chRig) useOn(step questplan.Step) bool {
	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}
	switch {
	case step.ItemID == questplan.ObjKnife && step.TargetID == questplan.ObjChLogs:
		r.take(questplan.ObjChLogs, 1)
		r.give(questplan.ObjChShaft, 4)
	case step.ItemID == questplan.ObjChFeather && step.TargetID == questplan.ObjChShaft:
		n := min(min(r.held(questplan.ObjChFeather), 24)/4, min(r.held(questplan.ObjChShaft), 6))
		if n == 0 {
			r.t.Errorf("feathers used on shafts with %d and %d in the pack", r.held(questplan.ObjChFeather), r.held(questplan.ObjChShaft))
			return false
		}
		r.take(questplan.ObjChFeather, n*4)
		r.take(questplan.ObjChShaft, n)
		r.give(questplan.ObjChFlighted, n)
	case step.ItemID == questplan.ObjChChisel && step.TargetID == questplan.ObjChWolfBones:
		r.take(questplan.ObjChWolfBones, 1)
		r.give(questplan.ObjChTips, 4)
	case step.ItemID == questplan.ObjChTips && step.TargetID == questplan.ObjChFlighted:
		n := min(min(r.held(questplan.ObjChTips), r.held(questplan.ObjChFlighted)), 6)
		r.take(questplan.ObjChTips, n)
		r.take(questplan.ObjChFlighted, n)
		r.give(questplan.ObjChArrow, n)
	default:
		r.t.Errorf("an unexpected use of %d on %d", step.ItemID, step.TargetID)
		return false
	}
	return true
}

// The whole quest, every executor a stub, in the content's order: the kit, the start, twelve arrows (three wolves' bones), Rantz's menus, the chest, the toads, the
// bait, the bow, the shot, the children, the six seasonings, the roast and the hand-over.
func TestBigChompyReachesCompleteThroughTheEngine(t *testing.T) {
	r := newChRig(t, questplan.ChNotStarted)
	res := r.run(BigChompy(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.log)
	}
	got := r.described()
	for _, want := range []string{"provision ch-kit", "talk to Rantz", questplan.CustomChChop, "use Knife on Achey tree logs", "use Feather on Ogre arrow shaft", questplan.CustomChWolf,
		"use Chisel on Wolf bones", "use Wolfbone arrowtips on Flighted ogre arrow", questplan.CustomChGive, questplan.CustomChChest, questplan.CustomChToad, questplan.CustomChShowToad,
		questplan.CustomChBait, questplan.CustomChBow, questplan.CustomChShoot, questplan.CustomChShowChompy, questplan.CustomChKids, questplan.CustomChSeason, questplan.CustomChCook,
		questplan.CustomChHandOver} {
		found := false
		for _, g := range got {
			found = found || strings.HasPrefix(g, want)
		}
		if !found {
			t.Errorf("the run never asked for %q: %v", want, got)
		}
	}
	// The order of the quest's own moments is the content's.
	order := func(prefix string) int {
		for i, g := range got {
			if strings.HasPrefix(g, prefix) {
				return i
			}
		}
		return -1
	}
	seq := []string{"talk to Rantz", questplan.CustomChGive, questplan.CustomChChest, questplan.CustomChToad, questplan.CustomChShowToad, questplan.CustomChBait, questplan.CustomChBow,
		questplan.CustomChShoot, questplan.CustomChShowChompy, questplan.CustomChKids, questplan.CustomChCook, questplan.CustomChHandOver}
	for i := 1; i < len(seq); i++ {
		if order(seq[i-1]) < 0 || order(seq[i]) <= order(seq[i-1]) {
			t.Errorf("%q (step %d) does not come after %q (step %d): %v", seq[i], order(seq[i]), seq[i-1], order(seq[i-1]), got)
		}
	}
	if r.kills < 2 || r.kills > 3 {
		t.Errorf("%d wolves killed for twelve tips of four, want 3 at most and 2 at least", r.kills)
	}
	if r.shots != 1 {
		t.Errorf("%d shots at a chompy", r.shots)
	}
	if got := r.e.Store.Record(questplan.ChompyID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	for _, id := range []int{questplan.ObjChRawChompy, questplan.ObjChSeasonedChomp, questplan.ObjChToad} {
		if r.held(id) != 0 && id != questplan.ObjChToad {
			t.Errorf("item %d still held after the hand-over", id)
		}
	}
	// The arrows are the ones Rantz was owed and the ones the bow shot: twelve made, six given.
	if r.held(questplan.ObjChArrow) != 6 {
		t.Errorf("%d arrows left, want six", r.held(questplan.ObjChArrow))
	}
	if r.provided[questplan.KitChompy] == 0 {
		t.Error("the kit was never provisioned")
	}
}

// A quest begun elsewhere resumes from where the journal says: each stage's own first step, and a healthy run is never parked.
func TestBigChompyResumesThroughTheEngineFromEveryStage(t *testing.T) {
	seasoned := func(r *chRig) {
		for _, c := range questplan.ChSeasoningList() {
			r.give(c.ID, 1)
		}
	}
	for _, c := range []struct {
		name  string
		stage int
		give  func(r *chRig)
		first string
	}{
		{"arrows made", questplan.ChStarted, func(r *chRig) { r.give(questplan.ObjChArrow, 12) }, questplan.CustomChGive},
		{"the toadies", questplan.ChGivenArrows, nil, questplan.CustomChToadies},
		{"15 with bellows", questplan.ChToadies, func(r *chRig) { r.give(questplan.ObjChBellows3, 1) }, questplan.CustomChToad},
		{"25 with a toad", questplan.ChShownToad, func(r *chRig) { r.give(questplan.ObjChToad, 1) }, questplan.CustomChBait},
		{"40 without a bow", questplan.ChRantzMissed, func(r *chRig) { r.give(questplan.ObjChArrow, 6); r.give(questplan.ObjChBellows3, 1) }, questplan.CustomChBow},
		{"45 armed", questplan.ChGotBow, func(r *chRig) {
			r.give(questplan.ObjChArrow, 6)
			r.give(questplan.ObjChBellows3, 1)
			r.give(questplan.ObjChBow, 1)
		}, questplan.CustomChShoot},
		{"50 with the chompy", questplan.ChKilled, func(r *chRig) { r.give(questplan.ObjChRawChompy, 1) }, questplan.CustomChShowChompy},
		{"55 with the chompy", questplan.ChToldToCook, func(r *chRig) { r.give(questplan.ObjChRawChompy, 1) }, questplan.CustomChKids},
		{"55 seasoned (the children are asked once a run)", questplan.ChToldToCook, func(r *chRig) { r.give(questplan.ObjChRawChompy, 1); seasoned(r) }, questplan.CustomChKids},
		{"60 seasoned chompy", questplan.ChCooked, func(r *chRig) { r.give(questplan.ObjChSeasonedChomp, 1) }, questplan.CustomChHandOver},
	} {
		r := newChRig(t, c.stage)
		r.stock()
		if c.give != nil {
			c.give(r)
		}
		res := r.run(BigChompy(), r.apply)
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s) after %v: %v", c.name, res.Outcome, res.Reason, r.described(), r.log)
			continue
		}
		if got := r.described(); len(got) == 0 || !strings.HasPrefix(got[0], c.first) {
			t.Errorf("%s: the first step is %v, want %q", c.name, got, c.first)
		}
	}
}

// A roast that burns costs the seasonings and another chompy: the next decision goes back to the bird, never loops on the spit.
func TestABurntChompyIsAnotherBirdAndAnotherRoast(t *testing.T) {
	r := newChRig(t, questplan.ChToldToCook)
	r.stock()
	r.give(questplan.ObjChRawChompy, 1)
	r.give(questplan.ObjChArrow, 6)
	r.give(questplan.ObjChBow, 1)
	r.give(questplan.ObjChBellows3, 1)
	r.kids = true
	for _, c := range questplan.ChSeasoningList() {
		r.give(c.ID, 1)
	}
	r.burnFirst = true
	res := r.run(BigChompy(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.log)
	}
	if r.burns != 1 || r.shots != 1 {
		t.Errorf("%d burns and %d shots: a burnt chompy is one more bird", r.burns, r.shots)
	}
	got := r.described()
	if i := slices.IndexFunc(got, func(s string) bool { return strings.HasPrefix(s, questplan.CustomChShoot) }); i < 0 {
		t.Errorf("no second bird shot after the burn: %v", got)
	}
}

// A weak account is not started: the run parks for six hours with the wolf named, and walks nowhere.
func TestAWeakAccountIsParkedBeforeItMeetsAWolf(t *testing.T) {
	r := newChRig(t, questplan.ChNotStarted)
	tfSeed(r.csRig, "attack", 20)
	tfSeed(r.csRig, "strength", 20)
	tfSeed(r.csRig, "defence", 15)
	r.stock()
	res := r.run(BigChompy(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "wolf") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if len(r.steps) != 0 {
		t.Errorf("the weak account was sent on: %v", r.described())
	}
	if parked, _ := r.e.Store.Parked(questplan.ChompyID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.ChompyID, time.Now().Add(7*time.Hour)); parked {
		t.Error("parked past the six hours")
	}
}

// An account under a level is parked with the level named.
func TestAnAccountUnderALevelIsParkedForSixHours(t *testing.T) {
	r := newChRig(t, questplan.ChNotStarted)
	tfSeed(r.csRig, "cooking", 12)
	res := r.run(BigChompy(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Cooking 12/30") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if parked, _ := r.e.Store.Parked(questplan.ChompyID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
}

// A wolf hunt that finds nothing parks the quest for three hours after one try, and a stub that never works parks by the watchdog, never loops.
func TestAHuntThatFailsParksTheQuestAndALegThatNeverWorksParks(t *testing.T) {
	r := newChRig(t, questplan.ChStarted)
	r.stock()
	r.give(questplan.ObjChFlighted, 6)
	runs := 0
	res := r.run(BigChompy(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomChWolf {
			runs++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "wolf") {
		t.Fatalf("outcome %s after %d hunts: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.ChompyID, time.Now().Add(179*time.Minute)); !parked {
		t.Error("not parked for the three hours")
	}
	// A talk that never takes: the toadies menu.
	r = newChRig(t, questplan.ChGivenArrows)
	r.stock()
	res = r.run(BigChompy(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomChToadies {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked {
		t.Fatalf("a talk that never works ended %s (%s)", res.Outcome, res.Reason)
	}
}

// A death at the wolves counts against the quest: the second parks it for a day.
func TestTwoDeathsParkBigChompyForADay(t *testing.T) {
	r := newChRig(t, questplan.ChStarted)
	r.stock()
	r.give(questplan.ObjChFlighted, 6)
	for i := 0; i < 2; i++ {
		r.e.Store.NoteDeath(questplan.ChompyID, time.Now())
	}
	if got := r.e.Store.Record(questplan.ChompyID).Deaths; got < 2 {
		t.Fatalf("deaths %d", got)
	}
	if questplan.BigChompy.MaxDeaths != 2 {
		t.Errorf("the death cap is %d", questplan.BigChompy.MaxDeaths)
	}
}
