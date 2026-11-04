package questmods

// Biohazard through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the module which of its kits and
// legs have run, the park requests, the completion record) on a simulated account, in the manner of Plague City's
// (plague_engine_test.go). The module's own Decide runs against snapshots built from the simulated pack, the simulated position and a
// journal the rig keeps, and every step it asks for is performed by a stub that plays the content's rule for it: the stage the scripts
// move %biohazard to, the items given, taken and refused (the cupboards, the crate, Elena's two-slot rule, the errand boys' pairings,
// the gate guard's confiscation), and the side of the wall, the headquarters' doors and Varrock's gate the account is left on. The
// provisioner's kits go through the real questplan.PlanProvision over a simulated bank. A stub that is asked for a step the content
// would not allow (the headquarters' door without the gown, the gate with no key, the Chemist without the sample) fails the test: it
// is the decision that is wrong. What is proven is the state machine; the executors' own live execution is the real-data tests', the
// live logs' and the executors' tests below.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

var (
	bhBank  = tile.Tile{X: 2616, Z: 3332}
	bhSewer = tile.Tile{X: 2562, Z: 9737}
)

// bhRig is a simulated account and the world around the quest.
type bhRig struct {
	*fillerRig
	raw     int // %biohazard as the scripts hold it
	order   []string
	count   map[string]int
	fail    map[string]bool
	keeps   [][]int
	pending questplan.Step
	extra   func(*questplan.Snapshot)
	e       *questrunner.Engine
	// boys is the vials the errand boys hold (the %bioerrand bits), by vial id.
	boys map[int]bool
	// spoilt is the vials a boy has spoilt (a vial given to the wrong boy is gone): never handed back.
	spoilt map[int]bool
	// gateTakes counts the vials the gate guard has taken.
	gateTakes int
	// keyFights counts the fights with the sick mourner.
	keyFights int
}

func newBH(t *testing.T, raw int, at tile.Tile, pack, bank map[int]int) *bhRig {
	t.Helper()
	fr := newFillerRig(t, raw, pack, bank)
	fr.sim.ShowTabs()
	fr.sim.StandAtLevel(at.X, at.Z, at.Level)
	w := &bhRig{fillerRig: fr, raw: raw, count: map[string]int{}, fail: map[string]bool{}, boys: map[int]bool{}, spoilt: map[int]bool{}}
	d := &questrunner.Deps{
		Session: fr.sim, Components: actionsim.TabComponents(),
		Log:   func(s string) { w.log = append(w.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	w.e = &questrunner.Engine{D: d, Prov: &questrunner.Provisioner{Bank: &questrunner.BankView{}}, Store: questplan.NewStore(),
		Account: "Test Player", Build: strings.Repeat("a", 40)}
	return w
}

func (w *bhRig) here() tile.Tile {
	x, z, l, _ := w.sim.State().Tile()
	return tile.Tile{X: x, Z: z, Level: l}
}
func (w *bhRig) area() questplan.BioArea { return questplan.BioAreaOf(w.here()) }
func (w *bhRig) at(t tile.Tile)          { w.sim.StandAtLevel(t.X, t.Z, t.Level) }

func (w *bhRig) wornCount(id int) int {
	n := 0
	for _, row := range w.sim.Worn() {
		if int(row.ID) == id {
			n += row.Count
		}
	}
	return n
}

// total is the pack, the worn panel and the bank: obj_gettotal.
func (w *bhRig) total(id int) int { return w.held(id) + w.wornCount(id) + w.bank[id] }

// slots is the pack slots in use.
func (w *bhRig) slots() int {
	n := 0
	for id, c := range w.pack() {
		if id == questplan.CoinsID {
			n++
		} else {
			n += c
		}
	}
	return n
}

// need fails the test unless cond: the decision asked for a step the content would refuse.
func (w *bhRig) need(cond bool, format string, args ...any) bool {
	w.t.Helper()
	if !cond {
		w.t.Errorf("the decision asked for a step the content would refuse: "+format+" (raw stage %d, at %v)", append(args, w.raw, w.here())...)
	}
	return cond
}

// bhSlot is the worn slot an item goes in: the doctors' and the priest's gown are torso pieces (slot 4), the priest's robe legs (slot 7).
func bhSlot(id int) int {
	if id == questplan.BioObjPriestRobe {
		return 7
	}
	return 4
}

// wear puts a held item on.
func (w *bhRig) wear(id int) bool {
	if w.wornCount(id) > 0 {
		return true
	}
	if !w.take(id, 1) {
		return false
	}
	w.sim.Wear(bhSlot(id), int32(id))
	return true
}

// quest is the real module over the rig's world: the real Decide, every executor a stub.
func (w *bhRig) quest() questrunner.Quest {
	q := Biohazard()
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch {
		case w.raw == 0:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusNotStarted, 0, false
		case w.raw >= questplan.BioComplete:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, w.raw, true
		}
		if w.extra != nil {
			w.extra(&s)
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait:
			return st
		}
		if why := questplan.ValidateStep(st); why != "" {
			w.t.Fatalf("the module asked for a malformed step: %s (%s)", why, st.Describe())
		}
		name := ""
		switch st.Kind {
		case questplan.StepCustom:
			name = st.Custom
		case questplan.StepProvision:
			name = "provision:" + st.KitName
			w.keeps = append(w.keeps, st.KeepIDs)
		case questplan.StepDeposit:
			name = "step:deposit"
			w.keeps = append(w.keeps, st.KeepIDs)
		default:
			w.t.Fatalf("the module asked for a %s step, which no leg of Biohazard is", st.Kind)
		}
		w.pending = st
		return questplan.Step{Kind: questplan.StepCustom, Custom: name, Reason: st.Describe()}
	}
	customs := map[string]questrunner.CustomFunc{}
	stub := func(name string, f func() bool) {
		customs[name] = func(*questrunner.Engine, questplan.Step) bool {
			w.order = append(w.order, name)
			w.count[name]++
			if w.fail[name] {
				return false
			}
			return f()
		}
	}
	for _, n := range questplan.BiohazardCustomNames() {
		stub(n, func() bool { return w.apply(n) })
	}
	for _, kit := range []string{questplan.KitBioStashEarly, questplan.KitBioStashHQ, questplan.KitBioStashVarrock, questplan.KitBioFood,
		questplan.KitBioCoins, questplan.KitBioSuit} {
		stub("provision:"+kit, func() bool { return w.provision(w.pending.Kit) })
	}
	stub("step:deposit", func() bool {
		keep := map[int]bool{}
		for _, id := range w.pending.KeepIDs {
			keep[id] = true
		}
		for id, n := range w.pack() {
			if !keep[id] {
				w.take(id, n)
				w.bank[id] += n
			}
		}
		return true
	})
	q.Customs = customs
	return q
}

// run plays the quest to its end.
func (w *bhRig) run() questrunner.Result { return w.e.Run(w.quest(), 30*time.Second) }

func (w *bhRig) ran(name string, from int) int {
	for i := from; i < len(w.order); i++ {
		if w.order[i] == name {
			return i
		}
	}
	return -1
}

func (w *bhRig) inOrder(want ...string) (string, bool) {
	at := 0
	for _, name := range want {
		i := w.ran(name, at)
		if i < 0 {
			return name, false
		}
		at = i + 1
	}
	return "", true
}

// bhFull is a pack with junk filling n slots besides the coins and the items given.
func bhFull(n int, items ...int) map[int]int {
	m := bhPack(items...)
	for i := 0; i < n; i++ {
		m[3000+i] = 1
	}
	return m
}

// bhPack is a pack with 500 coins and the items given.
func bhPack(items ...int) map[int]int {
	m := map[int]int{questplan.CoinsID: 500}
	for i := 0; i+1 < len(items); i += 2 {
		m[items[i]] += items[i+1]
	}
	return m
}

func (w *bhRig) wantComplete(res questrunner.Result) {
	w.t.Helper()
	if res.Outcome != questrunner.OutcomeComplete {
		w.t.Fatalf("the run ended %s (%s)\nsteps: %v\nlog: %v", res.Outcome, res.Reason, w.order, w.log)
	}
	if w.raw < questplan.BioComplete {
		w.t.Errorf("complete at stage %d", w.raw)
	}
	if cs := w.e.Store.Record(questplan.BiohazardID).Completions; len(cs) != 1 || cs[0].Earned() != (len(w.order) > 0) || cs[0].Account != "Test Player" {
		w.t.Errorf("completions %+v after %d steps, want one, earned only if the run did work", cs, len(w.order))
	}
}

// apply is the content's side of every custom step the module can ask for.
func (w *bhRig) apply(name string) bool {
	w.t.Helper()
	set := func(n int) bool { w.raw = n; return true }
	east := func() bool { return w.area() == questplan.BioEast }
	west := func() bool { return w.area() == questplan.BioWest }
	switch name {
	case questplan.CustomBioStart:
		return w.need(east() && w.raw == 0, "Elena's first talk") && set(questplan.BioStarted)
	case questplan.CustomBioJerico:
		return w.need(east() && w.raw == questplan.BioStarted, "Jerico") && set(questplan.BioSpokenJerico)
	case questplan.CustomBioBirdfeed:
		// jericoscupboardopen: the bag only from stage 2, only while neither the pack nor the bank holds one.
		if !w.need(east() && w.raw == questplan.BioSpokenJerico && w.total(questplan.BioObjBirdfeed) == 0, "Jerico's cupboard (a bag held or banked, or the wrong stage)") {
			return false
		}
		w.give(questplan.BioObjBirdfeed, 1)
		return true
	case questplan.CustomBioFeed:
		if !w.need(east() && w.raw == questplan.BioSpokenJerico && w.held(questplan.BioObjBirdfeed) > 0, "the bag on the watch tower") {
			return false
		}
		w.take(questplan.BioObjBirdfeed, 1)
		return set(questplan.BioUsedBirdfeed)
	case questplan.CustomBioPigeons:
		if !w.need(east() && w.raw == questplan.BioUsedBirdfeed && w.held(questplan.BioObjPigeons) == 0, "a pigeon cage from the ground") {
			return false
		}
		w.give(questplan.BioObjPigeons, 1)
		return true
	case questplan.CustomBioRelease:
		if !w.need(east() && w.raw == questplan.BioUsedBirdfeed && w.held(questplan.BioObjPigeons) > 0, "the cage opened in the box") {
			return false
		}
		w.take(questplan.BioObjPigeons, 1)
		w.give(questplan.BioObjEmptyCage, 1)
		w.at(tile.Tile{X: 2564, Z: 3303})
		return set(questplan.BioReleasedPigeons)
	case questplan.CustomBioCross:
		// Omart offers the ladder at stages 4 to 7 only.
		if !w.need(east() && w.raw >= questplan.BioReleasedPigeons && w.raw <= questplan.BioFoundDistillator, "Omart's ladder") {
			return false
		}
		w.at(questplan.BioWestLanding)
		if w.raw == questplan.BioReleasedPigeons {
			w.raw = questplan.BioClimbedLadder
		}
		return true
	case questplan.CustomBioApples:
		if !w.need(west() && w.raw == questplan.BioClimbedLadder, "the rotten apples are on the ground in the city at stage 5") {
			return false
		}
		w.give(questplan.BioObjRottenApples, 1)
		w.at(questplan.BioApples)
		return true
	case questplan.CustomBioStew:
		if !w.need(west() && w.raw == questplan.BioClimbedLadder && w.held(questplan.BioObjRottenApples) > 0, "the apples in the cauldron") {
			return false
		}
		w.take(questplan.BioObjRottenApples, 1)
		w.at(questplan.BioCauldronAt)
		return set(questplan.BioPoisonedStew)
	case questplan.CustomBioGown:
		// bionursescupboard: the gown from stage 6, only while none is owned anywhere.
		if !w.need(west() && w.raw >= questplan.BioPoisonedStew && w.total(questplan.BioObjDoctorGown) == 0, "the nurse's cupboard (a gown owned or banked, or before stage 6)") {
			return false
		}
		w.give(questplan.BioObjDoctorGown, 1)
		w.at(questplan.BioNurseCupboard)
		return true
	case questplan.CustomBioHQ:
		// "In you go doc." at stages 6 and 7 to an account wearing the gown.
		if !w.need(west() && (w.raw == questplan.BioPoisonedStew || w.raw == questplan.BioFoundDistillator), "the headquarters' door opens at stages 6 and 7") {
			return false
		}
		if !w.need(w.wear(questplan.BioObjDoctorGown), "the gown to put on") {
			return false
		}
		w.at(questplan.BioHQDoorInside)
		return true
	case questplan.CustomBioUp:
		if !w.need(w.area() == questplan.BioHQ && w.wornCount(questplan.BioObjDoctorGown) > 0, "the stairs and the door upstairs need the gown, from the hall") {
			return false
		}
		w.at(questplan.BioSickRoom)
		return true
	case questplan.CustomBioKey:
		if !w.need(w.area() == questplan.BioHQUp && w.wornCount(questplan.BioObjDoctorGown) > 0, "the sick mourner, upstairs, in the gown") {
			return false
		}
		w.keyFights++
		// The mourner talks (and turns on the account, whose death-drop is the key) only at stage 6; at stage 7 he is attacked outright
		// and drops nothing the module can count on.
		if w.raw != questplan.BioPoisonedStew {
			return false
		}
		if !w.need(w.slotsFree() > 0, "a free slot for the key") {
			return false
		}
		w.give(questplan.BioObjKey, 1)
		return true
	case questplan.CustomBioCrate:
		if !w.need(w.area() == questplan.BioHQUp && w.held(questplan.BioObjKey) > 0, "the crate behind the gate needs the key") {
			return false
		}
		w.at(questplan.BioCrateStand)
		switch {
		case w.raw == questplan.BioPoisonedStew:
			w.give(questplan.BioObjDistillator, 1)
			return set(questplan.BioFoundDistillator)
		case w.raw == questplan.BioFoundDistillator && w.total(questplan.BioObjDistillator) == 0:
			w.give(questplan.BioObjDistillator, 1)
			return true
		}
		return w.need(false, "the crate is empty at this stage")
	case questplan.CustomBioLeaveHQ:
		if !w.need(w.area() == questplan.BioHQ || w.area() == questplan.BioHQUp, "out of the headquarters") {
			return false
		}
		w.at(questplan.BioHQDoorOutside)
		return true
	case questplan.CustomBioHome:
		if !w.need(w.area() != questplan.BioEast && w.area() != questplan.BioQuarter, "the way home from the east") {
			return false
		}
		w.at(questplan.BioEastLanding)
		return true
	case questplan.CustomBioHandBack:
		if !w.need(east() && w.raw == questplan.BioFoundDistillator && w.held(questplan.BioObjDistillator) > 0, "the distillator to Elena") {
			return false
		}
		w.take(questplan.BioObjDistillator, 1)
		// Each add is skipped unless two slots are free.
		for _, id := range []int{questplan.BioObjLiquidHoney, questplan.BioObjEthenea, questplan.BioObjBroline, questplan.BioObjSample} {
			if w.slotsFree() > 1 {
				w.give(id, 1)
			}
		}
		w.at(questplan.BioElenaAnchor)
		return set(questplan.BioGivenDistillator)
	case questplan.CustomBioReplace:
		if !w.need(east() && (w.raw == questplan.BioGivenDistillator || w.raw == questplan.BioSpokenChemist), "Elena's replacements are offered at stages 10 and 12") {
			return false
		}
		for _, id := range []int{questplan.BioObjLiquidHoney, questplan.BioObjEthenea, questplan.BioObjBroline, questplan.BioObjSample} {
			if w.held(id) == 0 {
				w.give(id, 1)
			}
		}
		w.at(questplan.BioElenaAnchor)
		return true
	case questplan.CustomBioChemist:
		if !w.need(east() && (w.raw == questplan.BioGivenDistillator || w.raw == questplan.BioSpokenChemist), "the Chemist's touch paper") {
			return false
		}
		w.at(questplan.BioChemist)
		if w.raw == questplan.BioGivenDistillator {
			// With no sample he is closing; with it, the right menu line moves the stage and gives the paper.
			if !w.need(w.held(questplan.BioObjSample) > 0, "the Chemist is closing for an account without the sample") {
				return false
			}
			w.give(questplan.BioObjTouchPaper, 1)
			return set(questplan.BioSpokenChemist)
		}
		if w.total(questplan.BioObjTouchPaper) == 0 {
			w.give(questplan.BioObjTouchPaper, 1)
		}
		return true
	case questplan.CustomBioErrands:
		if !w.need(east() && w.raw == questplan.BioSpokenChemist, "the errand boys") {
			return false
		}
		w.at(questplan.BioHopsRim)
		any := false
		for _, v := range questplan.BioVials() {
			if w.held(v) > 0 && !w.boys[v] {
				w.take(v, 1)
				w.boys[v] = true
				any = true
			}
		}
		return any
	case questplan.CustomBioGateIn:
		if !w.need(east() && w.raw == questplan.BioSpokenChemist, "the east gate, inward") {
			return false
		}
		// The guard takes every vial.
		for _, v := range questplan.BioVials() {
			if w.held(v) > 0 {
				w.take(v, w.held(v))
				w.gateTakes++
			}
		}
		w.at(questplan.BioGateInside)
		return true
	case questplan.CustomBioCollect:
		if !w.need(w.area() == questplan.BioQuarter && w.raw == questplan.BioSpokenChemist, "the boys at the Dancing Donkey") {
			return false
		}
		w.at(questplan.BioDonkey)
		for _, v := range questplan.BioVials() {
			if w.boys[v] && w.held(v) == 0 {
				w.boys[v] = false
				w.give(v, 1)
			}
		}
		for _, v := range questplan.BioVials() {
			if w.held(v) == 0 {
				return false
			}
		}
		return true
	case questplan.CustomBioGuidor:
		if !w.need(w.area() == questplan.BioQuarter && w.raw == questplan.BioSpokenChemist, "Guidor") {
			return false
		}
		if !w.need(w.held(questplan.BioObjPriestGown)+w.wornCount(questplan.BioObjPriestGown) > 0 && w.held(questplan.BioObjPriestRobe)+w.wornCount(questplan.BioObjPriestRobe) > 0,
			"both halves of the priest's suit for Guidor's wife") {
			return false
		}
		for _, id := range append([]int{questplan.BioObjSample, questplan.BioObjTouchPaper}, questplan.BioVials()...) {
			if !w.need(w.held(id) > 0, "item %d for Guidor's test", id) {
				return false
			}
		}
		for _, id := range append([]int{questplan.BioObjSample, questplan.BioObjTouchPaper}, questplan.BioVials()...) {
			w.take(id, 1)
		}
		w.wear(questplan.BioObjPriestGown)
		w.wear(questplan.BioObjPriestRobe)
		w.at(questplan.BioGuidor)
		return set(questplan.BioFoundSecret)
	case questplan.CustomBioGateOut:
		if !w.need(w.area() == questplan.BioQuarter, "out of the quarter") {
			return false
		}
		w.at(questplan.BioGateOutside)
		return true
	case questplan.CustomBioReport:
		if !w.need(east() && w.raw == questplan.BioFoundSecret, "Elena told") {
			return false
		}
		w.at(questplan.BioElenaAnchor)
		return set(questplan.BioReportedElena)
	case questplan.CustomBioLathas:
		if !w.need(east() && w.raw == questplan.BioReportedElena, "King Lathas") {
			return false
		}
		w.at(questplan.BioLathas)
		return set(questplan.BioComplete)
	}
	w.t.Fatalf("no content rule for the step %q", name)
	return false
}

// slotsFree is the pack slots free.
func (w *bhRig) slotsFree() int { return 28 - w.slots() }

// ---------------------------------------------------------------------------------------------------------------------

// The whole quest from nothing, in the content's order and the geography's.
func TestBiohazardRunsToCompleteThroughTheEngine(t *testing.T) {
	w := newBH(t, 0, bhBank, bhPack(), nil)
	res := w.run()
	w.wantComplete(res)
	if miss, ok := w.inOrder(
		questplan.CustomBioStart, questplan.CustomBioJerico, "provision:"+questplan.KitBioStashEarly, questplan.CustomBioBirdfeed, questplan.CustomBioFeed,
		questplan.CustomBioPigeons, questplan.CustomBioRelease, questplan.CustomBioCross, questplan.CustomBioApples, questplan.CustomBioStew,
		questplan.CustomBioGown, questplan.CustomBioHQ, questplan.CustomBioUp, questplan.CustomBioKey, questplan.CustomBioCrate, questplan.CustomBioLeaveHQ,
		questplan.CustomBioHome, questplan.CustomBioHandBack, "provision:"+questplan.KitBioStashVarrock, questplan.CustomBioChemist,
		questplan.CustomBioErrands, "provision:"+questplan.KitBioSuit, questplan.CustomBioGateIn, questplan.CustomBioCollect, questplan.CustomBioGuidor,
		questplan.CustomBioGateOut, questplan.CustomBioReport, questplan.CustomBioLathas); !ok {
		t.Errorf("%q is out of order or missing in\n%s", miss, strings.Join(w.order, "\n"))
	}
	// One of everything: one crossing, one fight, one way home, one gate in, one gate out.
	for name, want := range map[string]int{questplan.CustomBioCross: 1, questplan.CustomBioKey: 1, questplan.CustomBioHome: 1, questplan.CustomBioGateIn: 1,
		questplan.CustomBioGateOut: 1, questplan.CustomBioGown: 1, questplan.CustomBioCrate: 1, questplan.CustomBioHQ: 1, questplan.CustomBioStew: 1,
		questplan.CustomBioErrands: 1, questplan.CustomBioCollect: 1, questplan.CustomBioChemist: 1, questplan.CustomBioGuidor: 1, questplan.CustomBioLathas: 1} {
		if w.count[name] != want {
			t.Errorf("%s ran %d times, want %d\n%s", name, w.count[name], want, strings.Join(w.order, "\n"))
		}
	}
	// No vial was ever taken from the account at the gate.
	if w.gateTakes != 0 {
		t.Errorf("the gate guard took %d vials from an account that had given them to the boys", w.gateTakes)
	}
	// The module's keep list reaches every bank and deposit step: the quest's items, the food, the coins.
	if len(w.keeps) == 0 {
		t.Fatal("no provision or deposit step was asked for")
	}
	for i, keep := range w.keeps {
		for _, id := range append(questplan.BioToolIDs(), questplan.BioFoodIDs()...) {
			if !slices.Contains(keep, id) {
				t.Errorf("step %d's keep list lacks item %d", i, id)
			}
		}
	}
	// The suit is worn at the end, the quest items the scripts take are gone (the key stays), and there was one fight, not two.
	if w.wornCount(questplan.BioObjPriestGown) == 0 || w.wornCount(questplan.BioObjPriestRobe) == 0 {
		t.Errorf("the priest's suit is not worn at the end")
	}
	for _, id := range append([]int{questplan.BioObjSample, questplan.BioObjTouchPaper, questplan.BioObjDistillator, questplan.BioObjBirdfeed}, questplan.BioVials()...) {
		if w.held(id) != 0 {
			t.Errorf("item %d is still in the pack at the end", id)
		}
	}
	if w.keyFights != 1 {
		t.Errorf("%d fights with the sick mourner, want one", w.keyFights)
	}
}

// An account at any stage, wherever the last run left it, runs on from there to the end: the module remembers nothing but the
// journal, the position and the pack. Each row names where the account stands and what it holds; the stubs refuse any step the
// content would, so a decision that sends an account to a door it cannot open fails here.
func TestBiohazardResumesFromEveryStageAndPlace(t *testing.T) {
	gown, key, dist := questplan.BioObjDoctorGown, questplan.BioObjKey, questplan.BioObjDistillator
	kit := bhPack(questplan.BioObjSample, 1, questplan.BioObjEthenea, 1, questplan.BioObjLiquidHoney, 1, questplan.BioObjBroline, 1)
	suit := questplan.BioObjPriestGown
	robe := questplan.BioObjPriestRobe
	for _, c := range []struct {
		name string
		raw  int
		at   tile.Tile
		pack map[int]int
		worn map[int]int
		bank map[int]int
		boys bool // the errand boys hold the three vials
		// require lists steps the run must make; avoid the ones it must not.
		require, avoid []string
	}{
		{name: "asked, Jerico next", raw: 1, at: bhBank, pack: bhPack(), require: []string{questplan.CustomBioJerico}},
		{name: "the bag in the bank", raw: 2, at: bhBank, pack: bhPack(), bank: map[int]int{questplan.BioObjBirdfeed: 1},
			require: []string{"provision:" + questplan.KitBioStashEarly, questplan.CustomBioFeed}, avoid: []string{questplan.CustomBioBirdfeed}},
		{name: "the bag in hand", raw: 2, at: bhBank, pack: bhPack(questplan.BioObjBirdfeed, 1), require: []string{questplan.CustomBioFeed}, avoid: []string{questplan.CustomBioBirdfeed}},
		{name: "the bag thrown, the cage in the bank", raw: 3, at: bhBank, pack: bhPack(), bank: map[int]int{questplan.BioObjPigeons: 1},
			require: []string{questplan.CustomBioRelease}, avoid: []string{questplan.CustomBioPigeons}},
		{name: "the pigeons sent", raw: 4, at: bhBank, pack: bhPack(), require: []string{questplan.CustomBioCross}},
		{name: "over the wall", raw: 5, at: questplan.BioWestLanding, pack: bhPack(), require: []string{questplan.CustomBioApples, questplan.CustomBioStew}},
		{name: "over the wall with the apples", raw: 5, at: questplan.BioWestLanding, pack: bhPack(questplan.BioObjRottenApples, 1),
			require: []string{questplan.CustomBioStew}, avoid: []string{questplan.CustomBioApples}},
		{name: "over the wall, back in the east for the bank (the gown there)", raw: 5, at: bhBank, pack: bhPack(), bank: map[int]int{gown: 1},
			require: []string{"provision:" + questplan.KitBioStashHQ, questplan.CustomBioCross, questplan.CustomBioHQ}, avoid: []string{questplan.CustomBioGown}},
		{name: "the stew poisoned, in the yard", raw: 6, at: questplan.BioCauldronAt, pack: bhPack(), require: []string{questplan.CustomBioGown, questplan.CustomBioHQ, questplan.CustomBioKey}},
		{name: "the gown in hand", raw: 6, at: questplan.BioWestLanding, pack: bhPack(gown, 1), avoid: []string{questplan.CustomBioGown},
			require: []string{questplan.CustomBioHQ, questplan.CustomBioUp, questplan.CustomBioKey, questplan.CustomBioCrate}},
		{name: "the gown worn", raw: 6, at: questplan.BioWestLanding, pack: bhPack(), worn: map[int]int{gown: 1}, avoid: []string{questplan.CustomBioGown},
			require: []string{questplan.CustomBioHQ}},
		{name: "inside the hall", raw: 6, at: questplan.BioHQDoorInside, pack: bhPack(), worn: map[int]int{gown: 1}, avoid: []string{questplan.CustomBioHQ, questplan.CustomBioGown},
			require: []string{questplan.CustomBioUp, questplan.CustomBioKey}},
		{name: "upstairs", raw: 6, at: questplan.BioSickRoom, pack: bhPack(), worn: map[int]int{gown: 1}, avoid: []string{questplan.CustomBioHQ, questplan.CustomBioUp},
			require: []string{questplan.CustomBioKey, questplan.CustomBioCrate}},
		{name: "upstairs with the key", raw: 6, at: questplan.BioSickRoom, pack: bhPack(key, 1), worn: map[int]int{gown: 1}, avoid: []string{questplan.CustomBioKey},
			require: []string{questplan.CustomBioCrate, questplan.CustomBioLeaveHQ, questplan.CustomBioHome, questplan.CustomBioHandBack}},
		{name: "the key and the gown banked", raw: 6, at: bhBank, pack: bhPack(), bank: map[int]int{gown: 1, key: 1}, avoid: []string{questplan.CustomBioKey, questplan.CustomBioGown},
			require: []string{"provision:" + questplan.KitBioStashHQ, questplan.CustomBioCross, questplan.CustomBioHQ, questplan.CustomBioCrate}},
		{name: "the distillator found, in the headquarters", raw: 7, at: questplan.BioCrateStand, pack: bhPack(key, 1, dist, 1), worn: map[int]int{gown: 1},
			require: []string{questplan.CustomBioLeaveHQ, questplan.CustomBioHome, questplan.CustomBioHandBack}},
		{name: "the distillator found, on the street", raw: 7, at: questplan.BioHQDoorOutside, pack: bhPack(key, 1, dist, 1), worn: map[int]int{gown: 1},
			require: []string{questplan.CustomBioHome, questplan.CustomBioHandBack}, avoid: []string{questplan.CustomBioLeaveHQ}},
		{name: "the distillator found, home", raw: 7, at: bhBank, pack: bhPack(dist, 1), require: []string{questplan.CustomBioHandBack}, avoid: []string{questplan.CustomBioCross}},
		{name: "the distillator found, in the bank", raw: 7, at: bhBank, pack: bhPack(), bank: map[int]int{dist: 1}, require: []string{"provision:" + questplan.KitBioStashHQ, questplan.CustomBioHandBack},
			avoid: []string{questplan.CustomBioCross}},
		{name: "the distillator lost, the key kept: back for the crate", raw: 7, at: bhBank, pack: bhPack(key, 1), worn: map[int]int{gown: 1},
			require: []string{questplan.CustomBioCross, questplan.CustomBioHQ, questplan.CustomBioCrate, questplan.CustomBioHandBack}, avoid: []string{questplan.CustomBioKey}},
		{name: "a full pack at Elena's: banked first", raw: 7, at: bhBank, pack: bhFull(22, dist, 1), require: []string{"step:deposit", questplan.CustomBioHandBack}},
		{name: "the chemicals in hand", raw: 10, at: bhBank, pack: kit, require: []string{questplan.CustomBioChemist, questplan.CustomBioErrands, questplan.CustomBioGateIn, questplan.CustomBioGuidor}},
		{name: "the hand-back came short: Elena replaces it", raw: 10, at: bhBank, pack: bhPack(questplan.BioObjLiquidHoney, 1),
			require: []string{questplan.CustomBioReplace, questplan.CustomBioChemist, questplan.CustomBioGuidor}},
		{name: "the sample in the bank", raw: 10, at: bhBank, pack: bhPack(questplan.BioObjEthenea, 1, questplan.BioObjLiquidHoney, 1, questplan.BioObjBroline, 1),
			bank: map[int]int{questplan.BioObjSample: 1}, require: []string{"provision:" + questplan.KitBioStashVarrock, questplan.CustomBioChemist}, avoid: []string{questplan.CustomBioReplace}},
		{name: "the paper given", raw: 12, at: questplan.BioChemist, pack: bhPack(questplan.BioObjSample, 1, questplan.BioObjTouchPaper, 1, questplan.BioObjEthenea, 1, questplan.BioObjLiquidHoney, 1, questplan.BioObjBroline, 1),
			require: []string{questplan.CustomBioErrands, "provision:" + questplan.KitBioSuit, questplan.CustomBioGateIn, questplan.CustomBioCollect, questplan.CustomBioGuidor}, avoid: []string{questplan.CustomBioChemist}},
		{name: "the boys have the vials", boys: true, raw: 12, at: questplan.BioHopsRim, pack: bhPack(questplan.BioObjSample, 1, questplan.BioObjTouchPaper, 1),
			require: []string{"provision:" + questplan.KitBioSuit, questplan.CustomBioGateIn, questplan.CustomBioCollect, questplan.CustomBioGuidor}, avoid: []string{questplan.CustomBioErrands}},
		{name: "the paper lost", boys: true, raw: 12, at: bhBank, pack: bhPack(questplan.BioObjSample, 1), require: []string{questplan.CustomBioChemist, questplan.CustomBioGateIn}},
		{name: "the suit in the bank", boys: true, raw: 12, at: bhBank, pack: bhPack(questplan.BioObjSample, 1, questplan.BioObjTouchPaper, 1), bank: map[int]int{suit: 1, robe: 1},
			require: []string{"provision:" + questplan.KitBioStashVarrock, questplan.CustomBioGateIn}, avoid: []string{"provision:" + questplan.KitBioSuit}},
		{name: "in the quarter", boys: true, raw: 12, at: questplan.BioDonkey, pack: bhPack(questplan.BioObjSample, 1, questplan.BioObjTouchPaper, 1, suit, 1, robe, 1),
			require: []string{questplan.CustomBioCollect, questplan.CustomBioGuidor}, avoid: []string{questplan.CustomBioGateIn}},
		{name: "Guidor told, still inside", raw: 14, at: questplan.BioGuidor, pack: bhPack(), worn: map[int]int{suit: 1, robe: 1},
			require: []string{questplan.CustomBioGateOut, questplan.CustomBioReport, questplan.CustomBioLathas}},
		{name: "Guidor told, outside", raw: 14, at: questplan.BioGateOutside, pack: bhPack(), require: []string{questplan.CustomBioReport, questplan.CustomBioLathas}, avoid: []string{questplan.CustomBioGateOut}},
		{name: "Elena told", raw: 15, at: bhBank, pack: bhPack(), require: []string{questplan.CustomBioLathas}},
		{name: "done", raw: 16, at: bhBank, pack: bhPack(), avoid: []string{questplan.CustomBioLathas}},
		{name: "stranded in the city before the ladder", raw: 4, at: questplan.BioWestLanding, pack: bhPack(), require: []string{questplan.CustomBioHome, questplan.CustomBioCross}},
		{name: "in the sewer at stage 5", raw: 5, at: bhSewer, pack: bhPack(), require: []string{questplan.CustomBioHome, questplan.CustomBioCross}},
		{name: "in the city at stage 10", raw: 10, at: questplan.BioWestLanding, pack: kit, require: []string{questplan.CustomBioHome, questplan.CustomBioChemist}},
		{name: "in the hall at stage 5", raw: 5, at: questplan.BioHQDoorInside, pack: bhPack(), require: []string{questplan.CustomBioLeaveHQ, questplan.CustomBioApples}, avoid: []string{questplan.CustomBioCross}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newBH(t, c.raw, c.at, c.pack, c.bank)
			for id, n := range c.worn {
				for ; n > 0; n-- {
					w.sim.Wear(bhSlot(id), int32(id))
				}
			}
			if c.boys {
				for _, v := range questplan.BioVials() {
					w.boys[v] = true
				}
			}
			res := w.run()
			w.wantComplete(res)
			for _, name := range c.require {
				if w.ran(name, 0) < 0 {
					t.Errorf("the run never made %q:\n%s", name, strings.Join(w.order, "\n"))
				}
			}
			for _, name := range c.avoid {
				if w.ran(name, 0) >= 0 {
					t.Errorf("the run made %q, which it had no need to:\n%s", name, strings.Join(w.order, "\n"))
				}
			}
		})
	}
}

// A pack with no room is emptied of everything that is not the quest's (the deposit's keep list) before the work, and the quest's items
// stay in it.
func TestBiohazardDepositsJunkAndKeepsTheQuestsItems(t *testing.T) {
	pack := bhPack(questplan.BioObjDoctorGown, 1, questplan.BioObjRottenApples, 1)
	for id := 3000; id < 3024; id++ {
		pack[id] = 1
	}
	w := newBH(t, questplan.BioReleasedPigeons, bhBank, pack, nil)
	w.wantComplete(w.run())
	if w.ran("step:deposit", 0) < 0 {
		t.Fatalf("a full pack was not deposited:\n%s", strings.Join(w.order, "\n"))
	}
	for id := 3000; id < 3024; id++ {
		if w.bank[id] != 1 {
			t.Errorf("junk item %d: %d in the bank", id, w.bank[id])
		}
	}
	if w.ran(questplan.CustomBioCross, 0) < w.ran("step:deposit", 0) {
		t.Errorf("the wall was crossed before the pack was emptied:\n%s", strings.Join(w.order, "\n"))
	}
}

// The gate takes every vial an account carries across it; the module hands them to the boys first, and an account that did not (the
// boys refused) still goes on and gets them back from the boys at the inn.
func TestBiohazardVialsAreNeverCarriedThroughTheGateWhenTheBoysTakeThem(t *testing.T) {
	w := newBH(t, questplan.BioSpokenChemist, bhBank, bhPack(questplan.BioObjSample, 1, questplan.BioObjTouchPaper, 1, questplan.BioObjEthenea, 1,
		questplan.BioObjLiquidHoney, 1, questplan.BioObjBroline, 1), nil)
	w.wantComplete(w.run())
	if w.gateTakes != 0 || w.count[questplan.CustomBioErrands] != 1 || w.count[questplan.CustomBioCollect] != 1 {
		t.Errorf("the gate took %d vials; errands ran %d times, collect %d", w.gateTakes, w.count[questplan.CustomBioErrands], w.count[questplan.CustomBioCollect])
	}
	// The boys refuse (they already hold a vial): the errands leg fails, the account goes on, loses the vials at the gate and has the
	// boys' own back at the inn.
	w = newBH(t, questplan.BioSpokenChemist, bhBank, bhPack(questplan.BioObjSample, 1, questplan.BioObjTouchPaper, 1, questplan.BioObjEthenea, 1,
		questplan.BioObjLiquidHoney, 1, questplan.BioObjBroline, 1), nil)
	for _, v := range questplan.BioVials() {
		w.boys[v] = true
	}
	w.wantComplete(w.run())
	if w.gateTakes != 3 || w.count[questplan.CustomBioErrands] != 1 {
		t.Errorf("the gate took %d vials; errands ran %d times", w.gateTakes, w.count[questplan.CustomBioErrands])
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Parks and refusals.
// ---------------------------------------------------------------------------------------------------------------------

func bhLevels(attack, strength, defence, hp int) func(*questplan.Snapshot) {
	return func(s *questplan.Snapshot) {
		s.Levels = map[string]int{"attack": attack, "strength": strength, "defence": defence, "hitpoints": hp, "prayer": 1, "ranged": 1, "magic": 1}
		s.HP, s.MaxHP = hp, hp
	}
}

// A leg that fails is a verdict for the run: the module waits, the engine parks the quest with the leg named, and the next run starts
// it afresh.
func TestBiohazardALegThatFailsParksTheQuestWithItsName(t *testing.T) {
	gown, key := questplan.BioObjDoctorGown, questplan.BioObjKey
	sample, paper := questplan.BioObjSample, questplan.BioObjTouchPaper
	vials := []int{questplan.BioObjEthenea, 1, questplan.BioObjLiquidHoney, 1, questplan.BioObjBroline, 1}
	suit := []int{questplan.BioObjPriestGown, 1, questplan.BioObjPriestRobe, 1}
	with := func(parts ...[]int) map[int]int {
		var items []int
		for _, p := range parts {
			items = append(items, p...)
		}
		return bhPack(items...)
	}
	for _, c := range []struct {
		name   string
		raw    int
		at     tile.Tile
		pack   map[int]int
		leg    string // the custom step that fails
		reason string // what the park names
	}{
		{"the first talk", 0, bhBank, bhPack(), questplan.CustomBioStart, "Elena"},
		{"Jerico", questplan.BioStarted, bhBank, bhPack(), questplan.CustomBioJerico, "Jerico"},
		{"the cupboard", questplan.BioSpokenJerico, bhBank, bhPack(), questplan.CustomBioBirdfeed, "bio-birdfeed"},
		{"the tower", questplan.BioSpokenJerico, bhBank, bhPack(questplan.BioObjBirdfeed, 1), questplan.CustomBioFeed, "bio-feed"},
		{"the pigeons", questplan.BioUsedBirdfeed, bhBank, bhPack(), questplan.CustomBioPigeons, "bio-pigeons"},
		{"the box", questplan.BioUsedBirdfeed, bhBank, bhPack(questplan.BioObjPigeons, 1), questplan.CustomBioRelease, "bio-release"},
		{"the ladder", questplan.BioReleasedPigeons, bhBank, bhPack(), questplan.CustomBioCross, "bio-cross"},
		{"the apples", questplan.BioClimbedLadder, questplan.BioWestLanding, bhPack(), questplan.CustomBioApples, "bio-apples"},
		{"the cauldron", questplan.BioClimbedLadder, questplan.BioWestLanding, bhPack(questplan.BioObjRottenApples, 1), questplan.CustomBioStew, "bio-stew"},
		{"the nurse's cupboard", questplan.BioPoisonedStew, questplan.BioWestLanding, bhPack(), questplan.CustomBioGown, "bio-gown"},
		{"the door", questplan.BioPoisonedStew, questplan.BioWestLanding, bhPack(gown, 1), questplan.CustomBioHQ, "bio-hq"},
		{"the stairs", questplan.BioPoisonedStew, questplan.BioHQDoorInside, bhPack(gown, 1), questplan.CustomBioUp, "bio-up"},
		{"the sick mourner", questplan.BioPoisonedStew, questplan.BioSickRoom, bhPack(), questplan.CustomBioKey, "bio-key"},
		{"the crate", questplan.BioPoisonedStew, questplan.BioSickRoom, bhPack(key, 1), questplan.CustomBioCrate, "bio-crate"},
		{"the way out of the headquarters", questplan.BioFoundDistillator, questplan.BioSickRoom, bhPack(questplan.BioObjDistillator, 1), questplan.CustomBioLeaveHQ, "headquarters' door"},
		{"the way home", questplan.BioFoundDistillator, questplan.BioWestLanding, bhPack(questplan.BioObjDistillator, 1), questplan.CustomBioHome, "no way out"},
		{"the hand-back", questplan.BioFoundDistillator, bhBank, bhPack(questplan.BioObjDistillator, 1), questplan.CustomBioHandBack, "Elena"},
		{"the Chemist", questplan.BioGivenDistillator, bhBank, with([]int{sample, 1}, vials), questplan.CustomBioChemist, "Chemist"},
		{"the errands", questplan.BioSpokenChemist, bhBank, with([]int{sample, 1, paper, 1}, vials), questplan.CustomBioErrands, "bio-collect"}, // the boys took nothing, the account went on to the gate and the inn, and the inn is where it parks
		{"the gate", questplan.BioSpokenChemist, bhBank, with([]int{sample, 1, paper, 1}, suit), questplan.CustomBioGateIn, "bio-gate-in"},
		{"the vials at the inn", questplan.BioSpokenChemist, questplan.BioDonkey, with([]int{sample, 1, paper, 1}, suit), questplan.CustomBioCollect, "bio-collect"},
		{"Guidor", questplan.BioSpokenChemist, questplan.BioDonkey, with([]int{sample, 1, paper, 1}, suit, vials), questplan.CustomBioGuidor, "bio-guidor"},
		{"the way out of the quarter", questplan.BioFoundSecret, questplan.BioGuidor, bhPack(), questplan.CustomBioGateOut, "east gate"},
		{"Elena", questplan.BioFoundSecret, bhBank, bhPack(), questplan.CustomBioReport, "Elena"},
		{"King Lathas", questplan.BioReportedElena, bhBank, bhPack(), questplan.CustomBioLathas, "King Lathas"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newBH(t, c.raw, c.at, c.pack, nil)
			w.fail[c.leg] = true
			res := w.run()
			if res.Outcome != questrunner.OutcomeParked {
				t.Fatalf("the run ended %s (%s), want a park\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
			}
			if !strings.Contains(res.Reason, c.reason) {
				t.Errorf("the park reason %q does not name %q", res.Reason, c.reason)
			}
			if w.count[c.leg] != 1 {
				t.Errorf("%s ran %d times: a failed leg is not retried within the run", c.leg, w.count[c.leg])
			}
			if parked, _ := w.e.Store.Parked(questplan.BiohazardID, time.Now()); !parked {
				t.Error("the park is not in the side store")
			}
		})
	}
}

// An account the sick mourner could beat is given food once and then, if the price still says no, the quest is left alone for six
// hours with the reason, instead of being sent across the wall to a fight it loses.
func TestBiohazardWeakAccountIsNotSentToTheSickMourner(t *testing.T) {
	t.Run("before the wall", func(t *testing.T) {
		w := newBH(t, questplan.BioReleasedPigeons, bhBank, bhPack(), map[int]int{questplan.PlagueObjShark: 3})
		w.extra = bhLevels(5, 5, 5, 10)
		res := w.run()
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "sick mourner") {
			t.Fatalf("the run ended %s (%s), want a park on the sick mourner\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
		}
		if w.count[questplan.CustomBioCross] != 0 {
			t.Error("the account crossed the wall")
		}
		if w.count["provision:"+questplan.KitBioFood] != 1 {
			t.Errorf("food was fetched %d times, want once", w.count["provision:"+questplan.KitBioFood])
		}
		parked, why := w.e.Store.Parked(questplan.BiohazardID, time.Now())
		until := time.UnixMilli(w.e.Store.Record(questplan.BiohazardID).BackoffUntil)
		if !parked || !strings.Contains(why, "sick mourner") || time.Until(until) < 5*time.Hour {
			t.Errorf("parked %v (%q) until %v, want about six hours", parked, why, until)
		}
	})
	t.Run("a strong account goes", func(t *testing.T) {
		w := newBH(t, questplan.BioReleasedPigeons, bhBank, bhPack(), nil)
		w.extra = bhLevels(70, 70, 70, 70)
		w.wantComplete(w.run())
		if w.count["provision:"+questplan.KitBioFood] != 0 {
			t.Error("a strong account asked for food")
		}
	})
	t.Run("a middling one with food goes, the model willing", func(t *testing.T) {
		w := newBH(t, questplan.BioReleasedPigeons, bhBank, bhPack(), map[int]int{questplan.PlagueObjShark: 4})
		w.extra = bhLevels(15, 15, 10, 25)
		w.wantComplete(w.run())
		if w.count["provision:"+questplan.KitBioFood] != 1 {
			t.Errorf("food was fetched %d times, want once", w.count["provision:"+questplan.KitBioFood])
		}
	})
	t.Run("a weak account that is already in the city goes home for food and is then refused", func(t *testing.T) {
		w := newBH(t, questplan.BioPoisonedStew, questplan.BioWestLanding, bhPack(questplan.BioObjDoctorGown, 1), map[int]int{questplan.PlagueObjShark: 3})
		w.extra = bhLevels(5, 5, 5, 10)
		res := w.run()
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "sick mourner") {
			t.Fatalf("the run ended %s (%s)\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
		}
		if w.count[questplan.CustomBioHQ] != 0 || w.count[questplan.CustomBioHome] != 1 {
			t.Errorf("the headquarters ran %d times, the way home %d: want 0 and 1\n%s", w.count[questplan.CustomBioHQ], w.count[questplan.CustomBioHome], strings.Join(w.order, "\n"))
		}
	})
	t.Run("a weak account with the key already goes on", func(t *testing.T) {
		w := newBH(t, questplan.BioPoisonedStew, questplan.BioWestLanding, bhPack(questplan.BioObjDoctorGown, 1, questplan.BioObjKey, 1), nil)
		w.extra = bhLevels(5, 5, 5, 10)
		w.wantComplete(w.run())
		if w.keyFights != 0 {
			t.Errorf("%d fights with an account that held the key", w.keyFights)
		}
	})
}

// A mourner the content would not talk to is no way to a key: the key leg at stage 7, with the distillator and key both gone, fails and
// the quest parks naming it rather than looping the headquarters.
func TestBiohazardLostKeyAndDistillatorAtStageSevenParks(t *testing.T) {
	w := newBH(t, questplan.BioFoundDistillator, questplan.BioSickRoom, bhPack(), nil)
	w.sim.Wear(bhSlot(questplan.BioObjDoctorGown), int32(questplan.BioObjDoctorGown))
	res := w.run()
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, questplan.CustomBioKey) {
		t.Fatalf("the run ended %s (%s)\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
	}
	if w.count[questplan.CustomBioKey] != 1 {
		t.Errorf("the key leg ran %d times", w.count[questplan.CustomBioKey])
	}
}

// Two deaths park the quest for a day; a death on the key leg or at the nurse's cupboard parks the legs for a day. The module declares
// both.
func TestBiohazardDeathPolicy(t *testing.T) {
	m := questplan.Biohazard
	if m.MaxDeaths != 2 || !m.NoRetaliate {
		t.Errorf("MaxDeaths %d, NoRetaliate %v", m.MaxDeaths, m.NoRetaliate)
	}
	for _, leg := range []string{questplan.CustomBioKey, questplan.CustomBioGown} {
		c, ok := m.StepDeathCaps[leg]
		if !ok || c.Max != 1 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour {
			t.Errorf("%s: death cap %+v (declared %v)", leg, c, ok)
		}
	}
	if len(m.StepDeathCaps) != 2 {
		t.Errorf("%d step death caps, want the two", len(m.StepDeathCaps))
	}
}

// Every custom step the module can ask for has an executor, and nothing else does.
func TestBiohazardEveryCustomHasAnExecutor(t *testing.T) {
	q := Biohazard()
	names := questplan.BiohazardCustomNames()
	for _, n := range names {
		if q.Customs[n] == nil {
			t.Errorf("no executor for %q", n)
		}
	}
	if len(q.Customs) != len(names) {
		t.Errorf("%d executors for %d custom steps", len(q.Customs), len(names))
	}
	if q.ID() != questplan.BiohazardID {
		t.Errorf("the factory built %q", q.ID())
	}
}
