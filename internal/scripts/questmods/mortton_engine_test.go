package questmods

// Shades of Mort'ton through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (chompy_engine_test.go): the module's own Decide runs
// against snapshots built from a fake pack, the account's tile and levels and the quest tab's colour, and every step it asks for is performed by a stub that changes that world
// the way the server would. What is proven is the state machine under the engine's bookkeeping (Ran and Failed, the watchdog, the park requests, the death cap and the
// completion record): the quest reaches Complete in the order the content makes it, a healthy run is never parked, a hunt and a build that cannot be done park the quest with
// their reasons, and an account the model says loses is not sent. What is not proven here is each custom's own live execution (the talks, the shades, the walls and the pyre):
// those are the real-data tests and the live logs.

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// moRig is the quest's state on the rig: r.stage is %morttonquest and the tab follows it.
type moRig struct {
	*csRig
	provided map[string]int
	// herbs counts the searches of the smashed table (it gives once), kills the shades killed, spent the doses of serum a villager has been cured with, built whether the
	// temple was rebuilt, burnt the logs burnt.
	herbs, kills, spent, burnt int
	built                      bool
}

// newMoRig is a strong account at Varrock; over replaces levels before they are seeded (Hitpoints is the one level a later seeding cannot lower: the maximum seen stays).
func newMoRig(t *testing.T, stage int, over ...map[string]int) *moRig {
	t.Helper()
	stack := map[int32]bool{questplan.CoinsID: true, questplan.ObjMtPaste: true}
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool { return stack[id] }}, nil)
	sim.StandAt(questplan.VarrockEastBank.X, questplan.VarrockEastBank.Z)
	base := &csRig{t: t, sim: sim}
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.Components(),
		Log:   func(s string) { base.log = append(base.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{Counts: map[int]int{}, Known: true, At: time.Now().Add(time.Hour)}}
	base.e = &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	r := &moRig{csRig: base, provided: map[string]int{}}
	r.stageKnown = true
	r.setStage(stage)
	levels := map[string]int{"attack": 60, "strength": 60, "defence": 50, "hitpoints": 60, "herblore": 20, "crafting": 25, "firemaking": 5}
	for _, o := range over {
		for skill, level := range o {
			levels[skill] = level
		}
	}
	for skill, level := range levels {
		tfSeed(base, skill, level)
	}
	return r
}

func (r *moRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch n {
	case questplan.MtNotStarted:
		status = quests.NotStarted
	case questplan.MtComplete:
		status = quests.Complete
	}
	r.setTab(questplan.MorttonName, status)
}

func (r *moRig) at(at tile.Tile) { r.sim.StandAtLevel(at.X, at.Z, at.Level) }

// stock fills the pack the way the kit round does when the bank and the logs are there.
func (r *moRig) stock() {
	for id, n := range map[int]int{questplan.CoinsID: 5000, questplan.ObjTinderbox: 1, questplan.ObjMtLogs: 3, questplan.ObjShark: 8} {
		if r.held(id) < n {
			r.give(id, n-r.held(id))
		}
	}
}

// used is the slots the pack takes.
func (r *moRig) used() int {
	n := 0
	for _, s := range r.sim.Pack() {
		if s.Count > 0 {
			n++
		}
	}
	return n
}

// doses is the doses of serum 207 in the pack, and spend takes one from the vial with the fewest (the executor's own order).
func (r *moRig) doses() int {
	return 3*r.held(questplan.ObjMtSerum3) + 2*r.held(questplan.ObjMtSerum2) + r.held(questplan.ObjMtSerum1)
}

func (r *moRig) spend() {
	r.spent++
	switch {
	case r.held(questplan.ObjMtSerum1) > 0:
		r.take(questplan.ObjMtSerum1, 1)
	case r.held(questplan.ObjMtSerum2) > 0:
		r.take(questplan.ObjMtSerum2, 1)
		r.give(questplan.ObjMtSerum1, 1)
	case r.held(questplan.ObjMtSerum3) > 0:
		r.take(questplan.ObjMtSerum3, 1)
		r.give(questplan.ObjMtSerum2, 1)
	default:
		r.t.Errorf("a villager was talked to with no serum in the pack (stage %d)", r.stage)
	}
}

// buyMaterial buys sets of planks, bricks and paste, and the hammer and oil asked for first.
func (r *moRig) buyMaterial(sets int) {
	for i := 0; i < sets; i++ {
		r.give(questplan.ObjMtPlank, 1)
		r.give(questplan.ObjMtBrick, 1)
	}
	r.give(questplan.ObjMtPaste, sets*questplan.MtPastePerSet)
}

// apply plays the content's side of the quest.
func (r *moRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		r.provided[step.KitName]++
		r.stock()
		return true
	case questplan.StepDeposit, questplan.StepScanBank:
		return true
	case questplan.StepWalk:
		r.at(step.Anchor)
		return true
	}
	switch step.Custom {
	case questplan.CustomMtBurn:
		if r.held(questplan.ObjMtLogs) < step.Qty || r.held(questplan.ObjTinderbox) == 0 {
			r.t.Errorf("a burn of %d logs with %d logs and %d tinderboxes", step.Qty, r.held(questplan.ObjMtLogs), r.held(questplan.ObjTinderbox))
			return false
		}
		r.take(questplan.ObjMtLogs, step.Qty)
		r.burnt += step.Qty
		if step.Radius > 0 {
			r.give(questplan.ObjAshes, step.Radius)
		}
	case questplan.CustomMtTakeDiary:
		r.give(questplan.ObjMtDiary, 1)
	case questplan.CustomMtReadDiary:
		if r.held(questplan.ObjMtDiary) == 0 {
			r.t.Error("the diary read with none in the pack")
		}
		r.setStage(questplan.MtReadDiary)
	case questplan.CustomMtHerbs:
		r.herbs++
		if r.herbs > 1 {
			return false // the table gives once per account
		}
		r.give(questplan.ObjMtUnidTarromin, 2)
		r.give(questplan.ObjMtRoguesPurse, 1)
	case questplan.CustomMtIdentify:
		r.take(questplan.ObjMtUnidTarromin, 1)
		r.give(questplan.ObjMtTarromin, 1)
	case questplan.CustomMtVial:
		r.give(questplan.ObjMtVial, 1)
	case questplan.CustomMtWater:
		r.take(questplan.ObjMtVial, 1)
		r.give(questplan.ObjMtVialWater, 1)
	case questplan.CustomMtMixUnf:
		r.take(questplan.ObjMtTarromin, 1)
		r.take(questplan.ObjMtVialWater, 1)
		r.give(questplan.ObjMtUnfTarromin, 1)
	case questplan.CustomMtMixSerum:
		r.take(questplan.ObjMtUnfTarromin, 1)
		r.take(questplan.ObjAshes, 1)
		r.give(questplan.ObjMtSerum3, 1)
		if r.stage == questplan.MtReadDiary {
			r.setStage(questplan.MtMadeSerum)
		}
	case questplan.CustomMtDropJunk:
		dropped := 0
		for _, id := range []int{questplan.ObjMtDiary, questplan.ObjMtRoguesPurse, questplan.ObjMtRottenFood} {
			if n := r.held(id); n > 0 {
				r.take(id, n)
				dropped += n
			}
		}
		return dropped > 0
	case questplan.CustomMtSend:
		r.spend()
		r.setStage(questplan.MtKillShades)
	case questplan.CustomMtHunt:
		r.kills++
		r.give(questplan.ObjMtRemains, 1)
		if r.stage >= questplan.MtKillShades && r.stage < questplan.MtKilled5 {
			r.setStage(r.stage + 5)
		}
	case questplan.CustomMtHandOver:
		if r.held(questplan.ObjMtRemains) < questplan.MtRemainsWanted {
			r.t.Errorf("Razmire was handed %d sets of remains", r.held(questplan.ObjMtRemains))
			return false
		}
		r.spend()
		r.take(questplan.ObjMtRemains, 2)
		r.setStage(questplan.MtShadesToRazmire)
		reserve := 0
		if r.held(questplan.ObjHammer) == 0 {
			reserve++
		}
		if questplan.MtOliveDoses(questplan.Snapshot{InvIDs: questrunner.PackCounts(r.e.D)}) < questplan.MtPyreSacredDose {
			reserve++
		}
		r.buyMaterial(questplan.MtSetsThatFit(28 - r.used() - reserve))
		if r.held(questplan.ObjHammer) == 0 {
			r.give(questplan.ObjHammer, 1)
		}
		if reserve > 0 && r.held(questplan.ObjMtOil3) == 0 {
			r.give(questplan.ObjMtOil3, 1)
		}
	case questplan.CustomMtGeneral:
		r.spend()
		r.give(step.ItemID, 1)
	case questplan.CustomMtBuilders:
		r.spend()
		r.buyMaterial(step.Qty)
	case questplan.CustomMtShow:
		r.spend()
		r.take(questplan.ObjMtRemains, 1)
		r.setStage(questplan.MtShadesToUlsquire)
	case questplan.CustomMtTempleTlk:
		r.setStage(questplan.MtUlsquireTemple)
	case questplan.CustomMtTemple:
		if r.held(questplan.ObjHammer) == 0 {
			r.t.Error("the temple built with no hammer")
			return false
		}
		if r.held(questplan.ObjMtPlank) == 0 && !r.built {
			r.t.Error("the temple built with no material")
			return false
		}
		for _, id := range []int{questplan.ObjMtPlank, questplan.ObjMtBrick, questplan.ObjMtPaste} {
			if n := r.held(id); n > 0 {
				r.take(id, n)
			}
		}
		r.built = true
		r.setStage(questplan.MtCanLightAltar)
	case questplan.CustomMtAltar:
		oil := questplan.ObjMtOil3
		if r.held(oil) == 0 {
			r.t.Error("the oil poured with none in the pack")
			return false
		}
		r.take(oil, 1)
		r.give(questplan.ObjMtSacred3, 1)
		r.setStage(questplan.MtSacredOil)
	case questplan.CustomMtPyreLogs:
		if r.held(questplan.ObjMtLogs) == 0 || r.held(questplan.ObjMtSacred3) == 0 {
			r.t.Errorf("pyre logs with %d logs and %d sacred oil", r.held(questplan.ObjMtLogs), r.held(questplan.ObjMtSacred3))
			return false
		}
		r.take(questplan.ObjMtSacred3, 1)
		r.give(questplan.ObjMtSacred1, 1)
		r.take(questplan.ObjMtLogs, 1)
		r.give(questplan.ObjMtPyreLogs, 1)
		r.setStage(questplan.MtPyreLogs)
	case questplan.CustomMtCremate:
		if r.held(questplan.ObjMtPyreLogs) == 0 || r.held(questplan.ObjMtRemains) == 0 || r.held(questplan.ObjTinderbox) == 0 {
			r.t.Error("a cremation without pyre logs, remains or a tinderbox")
			return false
		}
		r.take(questplan.ObjMtPyreLogs, 1)
		r.take(questplan.ObjMtRemains, 1)
		r.setStage(questplan.MtLitPyre)
	case questplan.CustomMtFinish:
		r.spend()
		r.setStage(questplan.MtComplete)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// moCustoms is the custom steps the run asked for, in order, a repeat collapsed.
func (r *moRig) customs() []string {
	var out []string
	for _, st := range r.steps {
		if st.Kind == questplan.StepCustom && (len(out) == 0 || out[len(out)-1] != st.Custom) {
			out = append(out, st.Custom)
		}
	}
	return out
}

// inOrder reports whether want is a subsequence of got.
func inOrder(got, want []string) (int, bool) {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i, i == len(want)
}

// The whole quest, every executor a stub, in the content's order: the kit and the ashes in Varrock, the walk, the diary, the table's herbs, two serums, Razmire, five shades,
// the hand-over and its shopping, Ulsquire twice, the temple, the altar, the pyre logs, the cremation and the last talk.
func TestShadesOfMorttonReachesCompleteThroughTheEngine(t *testing.T) {
	r := newMoRig(t, questplan.MtNotStarted)
	res := r.run(MorttonShades(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.log)
	}
	got := r.customs()
	want := []string{questplan.CustomMtBurn, questplan.CustomMtTakeDiary, questplan.CustomMtReadDiary, questplan.CustomMtHerbs, questplan.CustomMtIdentify, questplan.CustomMtVial,
		questplan.CustomMtWater, questplan.CustomMtMixUnf, questplan.CustomMtMixSerum, questplan.CustomMtSend, questplan.CustomMtHunt, questplan.CustomMtHandOver,
		questplan.CustomMtShow, questplan.CustomMtTempleTlk, questplan.CustomMtTemple, questplan.CustomMtAltar, questplan.CustomMtPyreLogs, questplan.CustomMtCremate,
		questplan.CustomMtFinish}
	if n, ok := inOrder(got, want); !ok {
		t.Errorf("the run asked for %v: step %d of the content's order, %q, is missing or out of place", got, n, want[n])
	}
	// The crossing came first: a provision, the ashes burnt in Varrock, and only then the walk to the town.
	desc := r.described()
	order := func(prefix string) int {
		for i, g := range desc {
			if strings.HasPrefix(g, prefix) {
				return i
			}
		}
		return -1
	}
	if order("provision mt-kit") != 0 || order(questplan.CustomMtBurn) < 0 || order("walk") < order(questplan.CustomMtBurn) {
		t.Errorf("the crossing's order: %v", desc)
	}
	if r.kills != 5 {
		t.Errorf("%d shades killed, want five", r.kills)
	}
	if r.spent < 4 || r.spent > 6 {
		t.Errorf("%d doses spent: the villagers' four, and a trip or two for what was missing", r.spent)
	}
	if r.burnt != 2 {
		t.Errorf("%d logs burnt for the ashes, want two", r.burnt)
	}
	if r.herbs != 1 {
		t.Errorf("the table searched %d times, it gives once", r.herbs)
	}
	if got := r.e.Store.Record(questplan.MorttonID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	if r.provided[questplan.KitMorttonOut] == 0 {
		t.Error("the kit was never provisioned")
	}
	// The pack at the end holds the tools it started with, not the junk of the way.
	for _, id := range []int{questplan.ObjMtDiary, questplan.ObjMtRoguesPurse} {
		if r.held(id) != 0 {
			t.Errorf("item %d still held after the run", id)
		}
	}
}

// A quest begun elsewhere resumes from where the journal says: each stage's own first step, and a healthy run is never parked.
func TestShadesOfMorttonResumesThroughTheEngineFromEveryStage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		give  func(r *moRig)
		first string
	}{
		{"diary read, no serum", questplan.MtReadDiary, func(r *moRig) { r.give(questplan.ObjAshes, 2); r.herbs = 0 }, questplan.CustomMtHerbs},
		{"serum made", questplan.MtMadeSerum, func(r *moRig) { r.give(questplan.ObjMtSerum3, 2) }, questplan.CustomMtSend},
		{"hunting", questplan.MtKillShades, func(r *moRig) { r.give(questplan.ObjMtSerum3, 2) }, questplan.CustomMtHunt},
		{"two shades down", questplan.MtKilled2, func(r *moRig) { r.give(questplan.ObjMtSerum3, 2); r.give(questplan.ObjMtRemains, 2) }, questplan.CustomMtHunt},
		{"five down", questplan.MtKilled5, func(r *moRig) { r.give(questplan.ObjMtSerum3, 2); r.give(questplan.ObjMtRemains, 5) }, questplan.CustomMtHandOver},
		{"remains to Razmire", questplan.MtShadesToRazmire, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 2)
			r.give(questplan.ObjMtRemains, 3)
			r.give(questplan.ObjHammer, 1)
			r.give(questplan.ObjMtOil3, 1)
			r.buyMaterial(3)
		}, questplan.CustomMtShow},
		{"no hammer yet", questplan.MtShadesToRazmire, func(r *moRig) { r.give(questplan.ObjMtSerum3, 2); r.give(questplan.ObjMtRemains, 3) }, questplan.CustomMtGeneral},
		{"Ulsquire's temple", questplan.MtShadesToUlsquire, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 2)
			r.give(questplan.ObjMtRemains, 2)
			r.give(questplan.ObjHammer, 1)
			r.give(questplan.ObjMtOil3, 1)
			r.buyMaterial(3)
		}, questplan.CustomMtTempleTlk},
		{"the temple", questplan.MtUlsquireTemple, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 2)
			r.give(questplan.ObjMtRemains, 2)
			r.give(questplan.ObjHammer, 1)
			r.give(questplan.ObjMtOil3, 1)
			r.buyMaterial(3)
		}, questplan.CustomMtTemple},
		{"the altar", questplan.MtCanLightAltar, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 2)
			r.give(questplan.ObjMtRemains, 2)
			r.give(questplan.ObjHammer, 1)
			r.give(questplan.ObjMtOil3, 1)
			r.built = true
		}, questplan.CustomMtAltar},
		{"the pyre logs", questplan.MtSacredOil, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 1)
			r.give(questplan.ObjMtRemains, 2)
			r.give(questplan.ObjMtSacred3, 1)
		}, questplan.CustomMtPyreLogs},
		{"the cremation", questplan.MtPyreLogs, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 1)
			r.give(questplan.ObjMtRemains, 2)
			r.give(questplan.ObjMtPyreLogs, 1)
		}, questplan.CustomMtCremate},
		{"a pyre that went out", questplan.MtLogsOnPyre, func(r *moRig) {
			r.give(questplan.ObjMtSerum3, 1)
			r.give(questplan.ObjMtRemains, 2)
			r.give(questplan.ObjMtPyreLogs, 1)
		}, questplan.CustomMtCremate},
		{"the last talk", questplan.MtLitPyre, func(r *moRig) { r.give(questplan.ObjMtSerum3, 1) }, questplan.CustomMtFinish},
	} {
		r := newMoRig(t, c.stage)
		r.stock()
		r.at(questplan.MtTown)
		if c.stage == questplan.MtReadDiary {
			r.herbs = 0
		}
		if c.give != nil {
			c.give(r)
		}
		res := r.run(MorttonShades(), r.apply)
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s) after %v: %v", c.name, res.Outcome, res.Reason, r.described(), r.log)
			continue
		}
		if got := r.customs(); len(got) == 0 || got[0] != c.first {
			t.Errorf("%s: the first step is %v, want %q", c.name, got, c.first)
		}
	}
}

// A weak account is not sent among the shades: it parks for six hours with the shades named and walks nowhere, and one the swamp's ghasts would beat is not sent in.
func TestAWeakAccountIsParkedBeforeTheShadesAndBeforeTheSwamp(t *testing.T) {
	r := newMoRig(t, questplan.MtKillShades)
	r.stock()
	r.at(questplan.MtTown)
	r.give(questplan.ObjMtSerum3, 2)
	tfSeed(r.csRig, "attack", 10)
	tfSeed(r.csRig, "strength", 10)
	tfSeed(r.csRig, "defence", 5)
	tfSeed(r.csRig, "hitpoints", 20)
	res := r.run(MorttonShades(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "shade") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if len(r.steps) != 0 {
		t.Errorf("the weak account was sent on: %v", r.described())
	}
	if parked, _ := r.e.Store.Parked(questplan.MorttonID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
	// At the gate of the swamp: parked, not walked in (20 hitpoints are under the floor of 25 and over the line the engine eats at).
	w := newMoRig(t, questplan.MtNotStarted, map[string]int{"hitpoints": 20})
	w.stock()
	res = w.run(MorttonShades(), w.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Mort Myre") {
		t.Fatalf("a 20-hitpoint account ended %s (%s) after %v", res.Outcome, res.Reason, w.described())
	}
	for _, st := range w.steps {
		if st.Kind == questplan.StepWalk {
			t.Errorf("the account was walked toward the swamp: %v", w.described())
		}
	}
}

// An account under a level is parked with the level named, and so is one whose pack the kit cannot fill.
func TestAnAccountUnderALevelIsParkedForSixHoursAtMorttonAndAKitThatNeverFillsParks(t *testing.T) {
	r := newMoRig(t, questplan.MtNotStarted)
	tfSeed(r.csRig, "herblore", 12)
	res := r.run(MorttonShades(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Herblore 12/15") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if parked, _ := r.e.Store.Parked(questplan.MorttonID, time.Now().Add(5*time.Hour)); !parked {
		t.Error("not parked for the six hours")
	}
	r = newMoRig(t, questplan.MtNotStarted)
	res = r.run(MorttonShades(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepProvision {
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked {
		t.Fatalf("a kit that never fills ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	for _, st := range r.steps {
		if st.Kind == questplan.StepWalk {
			t.Errorf("the account crossed with an empty pack: %v", r.described())
		}
	}
}

// A hunt that finds nothing parks the quest after one try, a temple that cannot be built does too, and a table that gave nothing parks the serum leg.
func TestAHuntOrATempleThatFailsParksTheQuest(t *testing.T) {
	r := newMoRig(t, questplan.MtKillShades)
	r.stock()
	r.at(questplan.MtTown)
	r.give(questplan.ObjMtSerum3, 2)
	runs := 0
	res := r.run(MorttonShades(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomMtHunt {
			runs++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || runs != 1 || !strings.Contains(res.Reason, "shade") {
		t.Fatalf("outcome %s after %d hunts: %q (%v)", res.Outcome, runs, res.Reason, r.log)
	}
	if parked, _ := r.e.Store.Parked(questplan.MorttonID, time.Now().Add(179*time.Minute)); !parked {
		t.Error("not parked for the three hours")
	}
	r = newMoRig(t, questplan.MtUlsquireTemple)
	r.stock()
	r.at(questplan.MtTown)
	r.give(questplan.ObjMtSerum3, 2)
	r.give(questplan.ObjMtRemains, 2)
	r.give(questplan.ObjHammer, 1)
	r.give(questplan.ObjMtOil3, 1)
	r.buyMaterial(2)
	builds := 0
	res = r.run(MorttonShades(), func(step questplan.Step) bool {
		if step.Kind == questplan.StepCustom && step.Custom == questplan.CustomMtTemple {
			builds++
			return false
		}
		return r.apply(step)
	})
	if res.Outcome != questrunner.OutcomeParked || builds != 1 || !strings.Contains(res.Reason, "temple") {
		t.Fatalf("outcome %s after %d builds: %q (%v)", res.Outcome, builds, res.Reason, r.log)
	}
	// A table that gave nothing: the account holds no serum, the herbs are not there, and the quest parks naming the table.
	r = newMoRig(t, questplan.MtMadeSerum)
	r.stock()
	r.at(questplan.MtTown)
	r.herbs = 1
	res = r.run(MorttonShades(), r.apply)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "table") {
		t.Fatalf("outcome %s: %q (%v)", res.Outcome, res.Reason, r.log)
	}
}

// A death at the hunt or at the temple parks the quest for a day at once (StepDeathCaps), not after the escalating backoff.
func TestADeathAtTheShadesParksMorttonForADay(t *testing.T) {
	for _, leg := range []string{questplan.CustomMtHunt, questplan.CustomMtTemple} {
		r := newMoRig(t, questplan.MtKillShades)
		stage := questplan.MtKillShades
		if leg == questplan.CustomMtTemple {
			stage = questplan.MtUlsquireTemple
			r.setStage(stage)
		}
		r.stock()
		r.at(questplan.MtTown)
		r.e.D.Chat = chatlog.New()
		r.give(questplan.ObjMtSerum3, 2)
		r.give(questplan.ObjMtRemains, 2)
		r.give(questplan.ObjHammer, 1)
		r.give(questplan.ObjMtOil3, 1)
		r.buyMaterial(2)
		tries := 0
		res := r.run(MorttonShades(), func(step questplan.Step) bool {
			if step.Kind == questplan.StepCustom && step.Custom == leg {
				tries++
				r.e.D.Chat.Record("Oh dear, you are dead!")
				return false
			}
			return r.apply(step)
		})
		if res.Outcome != questrunner.OutcomeParked || tries != 1 || !strings.Contains(res.Reason, "its cap is 1") {
			t.Fatalf("%s: the run ended %s (%s) after %d tries: %v", leg, res.Outcome, res.Reason, tries, r.log)
		}
		if parked, _ := r.e.Store.Parked(questplan.MorttonID, time.Now().Add(23*time.Hour)); !parked {
			t.Errorf("%s: not parked for the day", leg)
		}
	}
	if m := questplan.MorttonShades; m.MaxDeaths != 2 || len(m.StepDeathCaps) != 2 {
		t.Errorf("the death caps: %+v (%d deaths)", m.StepDeathCaps, m.MaxDeaths)
	}
}

// The quest's wiring: every custom the plan can ask for has an executor, and the factory builds the module the ladder lists.
func TestShadesOfMorttonIsWired(t *testing.T) {
	q := MorttonShades()
	if q.ID() != "mortton" || q.Module.Record.QuestPoints != 3 {
		t.Fatalf("built %q %d QP", q.ID(), q.Module.Record.QuestPoints)
	}
	names := []string{questplan.CustomMtBurn, questplan.CustomMtTakeDiary, questplan.CustomMtReadDiary, questplan.CustomMtHerbs, questplan.CustomMtIdentify, questplan.CustomMtVial,
		questplan.CustomMtWater, questplan.CustomMtMixUnf, questplan.CustomMtMixSerum, questplan.CustomMtDropJunk, questplan.CustomMtSend, questplan.CustomMtHunt,
		questplan.CustomMtHandOver, questplan.CustomMtGeneral, questplan.CustomMtBuilders, questplan.CustomMtShow, questplan.CustomMtTempleTlk, questplan.CustomMtTemple,
		questplan.CustomMtAltar, questplan.CustomMtPyreLogs, questplan.CustomMtCremate, questplan.CustomMtFinish}
	for _, name := range names {
		if q.Customs[name] == nil {
			t.Errorf("no executor for %q", name)
		}
	}
	if len(q.Customs) != len(names) {
		t.Errorf("%d customs registered, want %d", len(q.Customs), len(names))
	}
}
