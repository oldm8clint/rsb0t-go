package questmods

// Plague City through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the module which of its kits and
// legs have run, the park requests, the completion record) on a simulated account, in the manner of the fillers' (fillers_engine_test.go)
// and of Monk's Friend and the Clock Tower's (monkcog_engine_test.go). The module's own Decide runs against snapshots built from the
// simulated pack, the simulated position and a journal the rig keeps, and every step it asks for is performed by a stub that plays the
// content's rule for it: the stage the scripts move %elenaquest to (the journal renders 3 to 6, 20 and 21, and 24 and 25 alike, and the
// rig hands the module the lowest of each group, as ParsePlagueJournal does), the items given and taken, and the side of the wall
// the account is left on. The provisioner's kits go through the real questplan.PlanProvision over a simulated bank. A stub that is asked
// for a step the content would not allow (a dig from the sewer, a crossing without a mask) fails the test: it is the decision that is
// wrong. What is proven is the state machine; the executors' own live execution is the real-data tests', the live logs' and the
// executors' tests below.

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

var pwBank = tile.Tile{X: 2616, Z: 3332}

// pwRig is a simulated account and the world around the quest.
type pwRig struct {
	*fillerRig
	raw     int // %elenaquest as the scripts hold it
	order   []string
	count   map[string]int
	fail    map[string]bool
	keeps   [][]int
	pending questplan.Step
	extra   func(*questplan.Snapshot)
	e       *questrunner.Engine
}

func newPW(t *testing.T, raw int, at tile.Tile, pack, bank map[int]int) *pwRig {
	t.Helper()
	fr := newFillerRig(t, raw, pack, bank)
	fr.sim.ShowTabs()
	fr.sim.StandAtLevel(at.X, at.Z, at.Level)
	w := &pwRig{fillerRig: fr, raw: raw, count: map[string]int{}, fail: map[string]bool{}}
	d := &questrunner.Deps{
		Session: fr.sim, Components: actionsim.TabComponents(),
		Log:   func(s string) { w.log = append(w.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	w.e = &questrunner.Engine{D: d, Prov: &questrunner.Provisioner{Bank: &questrunner.BankView{}}, Store: questplan.NewStore(),
		Account: "Test Player", Build: strings.Repeat("a", 40)}
	return w
}

func (w *pwRig) here() tile.Tile {
	x, z, l, _ := w.sim.State().Tile()
	return tile.Tile{X: x, Z: z, Level: l}
}
func (w *pwRig) area() questplan.PlagueArea { return questplan.PlagueAreaOf(w.here()) }
func (w *pwRig) east() bool                 { return w.area() == questplan.PlagueEast }
func (w *pwRig) at(t tile.Tile)             { w.sim.StandAtLevel(t.X, t.Z, t.Level) }

func (w *pwRig) wornCount(id int) int {
	n := 0
	for _, row := range w.sim.Worn() {
		if int(row.ID) == id {
			n += row.Count
		}
	}
	return n
}

// total is the pack, the worn panel and the bank: obj_gettotal.
func (w *pwRig) total(id int) int { return w.held(id) + w.wornCount(id) + w.bank[id] }

// need fails the test unless cond: the decision asked for a step the content would refuse.
func (w *pwRig) need(cond bool, format string, args ...any) bool {
	w.t.Helper()
	if !cond {
		w.t.Errorf("the decision asked for a step the content would refuse: "+format+" (raw stage %d, at %v)", append(args, w.raw, w.here())...)
	}
	return cond
}

// pwJournal is the stage the journal tells the module: the lowest of the stages it renders alike.
func pwJournal(raw int) int {
	switch {
	case raw >= 3 && raw <= 6:
		return 3
	case raw == 21:
		return 20
	case raw == 25:
		return 24
	}
	return raw
}

// quest is the real module over the rig's world: the real Decide, every executor a stub.
func (w *pwRig) quest() questrunner.Quest {
	q := PlagueCity()
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch {
		case w.raw == 0:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusNotStarted, 0, false
		case w.raw >= questplan.PlagueComplete:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, pwJournal(w.raw), true
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
			w.t.Fatalf("the module asked for a %s step, which no leg of Plague City is", st.Kind)
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
	for _, n := range questplan.PlagueCustomNames() {
		stub(n, func() bool { return w.apply(n) })
	}
	for _, kit := range []string{questplan.KitPlagueBerries, questplan.KitPlagueBasics, questplan.KitPlagueMask, questplan.KitPlagueBook,
		questplan.KitPlagueCureBank, questplan.KitPlagueCureShops, questplan.KitPlagueFood} {
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
func (w *pwRig) run() questrunner.Result { return w.e.Run(w.quest(), 30*time.Second) }

// apply is the content's side of every custom step the module can ask for.
func (w *pwRig) apply(name string) bool {
	w.t.Helper()
	set := func(n int) bool { w.raw = n; return true }
	switch name {
	case questplan.CustomPlagueStart:
		return w.need(w.east() && w.raw == 0, "Edmond's first talk") && set(1)
	case questplan.CustomPlagueBerries:
		if !w.need(w.east() && w.raw == 1, "the dwellberries") {
			return false
		}
		w.give(questplan.PlagueObjDwellberries, 1)
		return true
	case questplan.CustomPlagueAlrena:
		if !w.need(w.east() && w.raw == 1 && w.held(questplan.PlagueObjDwellberries) > 0, "Alrena's dwellberries") {
			return false
		}
		w.take(questplan.PlagueObjDwellberries, 1)
		w.give(questplan.PlagueObjGasMask, 1)
		return set(2)
	case questplan.CustomPlagueEdmond:
		return w.need(w.east() && w.raw == 2, "Edmond about the digging") && set(3)
	case questplan.CustomPlagueCupboard:
		if !w.need(w.east() && w.raw >= questplan.PlaguePipeOpen, "Alrena's cupboard") {
			return false
		}
		if w.total(questplan.PlagueObjGasMask) == 0 {
			w.give(questplan.PlagueObjGasMask, 1)
		}
		return true
	case questplan.CustomPlagueWater:
		if !w.need(w.east() && w.raw >= 3 && w.raw <= 6 && w.held(questplan.ObjBucket)+w.held(questplan.ObjBucketOfWater) > 0, "the water") {
			return false
		}
		return set(questplan.PlagueSoft)
	case questplan.CustomPlagueDig:
		if !w.need(w.east() && w.raw >= questplan.PlagueSoft && w.held(questplan.ObjSpade) > 0, "the dig") {
			return false
		}
		if w.raw == questplan.PlagueSoft {
			w.raw = questplan.PlagueTunnel
		}
		w.at(questplan.PlagueSewerLanding)
		return true
	case questplan.CustomPlagueRope:
		if !w.need(w.area() == questplan.PlagueSewer && w.raw == questplan.PlagueTunnel && w.held(questplan.ObjRope) > 0, "the rope") {
			return false
		}
		w.take(questplan.ObjRope, 1)
		return set(questplan.PlagueRope)
	case questplan.CustomPlagueGrill:
		return w.need(w.area() == questplan.PlagueSewer && w.raw == questplan.PlagueRope, "Edmond pulls the grill") && set(questplan.PlaguePipeOpen)
	case questplan.CustomPlagueCross:
		if !w.need(w.area() == questplan.PlagueSewer && w.raw >= questplan.PlaguePipeOpen && w.raw <= questplan.PlagueCured &&
			w.held(questplan.PlagueObjGasMask)+w.wornCount(questplan.PlagueObjGasMask) > 0, "the pipe needs the mask on") {
			return false
		}
		if w.wornCount(questplan.PlagueObjGasMask) == 0 {
			w.take(questplan.PlagueObjGasMask, 1)
			w.sim.Wear(0, questplan.PlagueObjGasMask)
		}
		w.at(questplan.PlagueWestLanding)
		return true
	case questplan.CustomPlagueHome:
		if !w.need(w.area() != questplan.PlagueEast, "the way home from the east") {
			return false
		}
		w.at(questplan.PlagueGarden)
		return true
	case questplan.CustomPlagueJethick:
		if !w.need(w.area() == questplan.PlagueWest && (w.raw == questplan.PlaguePipeOpen || w.raw == 20 || w.raw == 21) &&
			w.held(questplan.PlagueObjPicture) > 0, "Jethick and the picture") {
			return false
		}
		if w.raw == questplan.PlaguePipeOpen {
			w.raw = questplan.PlagueShown
		}
		if w.total(questplan.PlagueObjBook) == 0 {
			w.give(questplan.PlagueObjBook, 1)
		}
		return true
	case questplan.CustomPlagueBook:
		if !w.need(w.area() == questplan.PlagueWest && (w.raw == 20 || w.raw == 21), "the book to the Rehnisons") {
			return false
		}
		if w.raw == 20 {
			if !w.need(w.held(questplan.PlagueObjBook) > 0, "the book (stage 20)") {
				return false
			}
			w.take(questplan.PlagueObjBook, 1)
			w.raw = 21
		}
		w.at(questplan.PlagueTedAnchor)
		return true
	case questplan.CustomPlagueParents:
		if !w.need(w.area() == questplan.PlagueWest && (w.raw == 20 || w.raw == 21), "the Rehnisons") {
			return false
		}
		if w.raw == 20 {
			return false // the door stays shut to an account without the book
		}
		w.at(questplan.PlagueTedAnchor)
		return set(questplan.PlagueSpokenParents)
	case questplan.CustomPlagueMilli:
		if !w.need((w.area() == questplan.PlagueWest || w.area() == questplan.PlagueUpstairs) && w.raw == questplan.PlagueSpokenParents, "Milli") {
			return false
		}
		w.at(questplan.PlagueMilliAnchor)
		w.raw = questplan.PlagueSpokenMilli
		w.at(questplan.PlagueTedAnchor)
		return true
	case questplan.CustomPlagueDown:
		if !w.need(w.area() == questplan.PlagueUpstairs, "the stairs down") {
			return false
		}
		w.at(questplan.PlagueTedAnchor)
		return true
	case questplan.CustomPlagueMourner:
		if !w.need(w.area() == questplan.PlagueWest && w.raw == questplan.PlagueSpokenMilli, "the mourner") {
			return false
		}
		w.at(questplan.PlagueHouseDoor)
		return set(questplan.PlagueNeedClearance)
	case questplan.CustomPlagueAudience:
		if !w.need(w.area() == questplan.PlagueWest && (w.raw == 24 || w.raw == 25), "the clerk and Bravek") {
			return false
		}
		w.give(questplan.PlagueObjNote, 1)
		w.at(questplan.PlagueBravekAnchor)
		return set(questplan.PlagueSpokenBravek)
	case questplan.CustomPlagueCure:
		if !w.need(w.area() == questplan.PlagueWest, "Bravek") {
			return false
		}
		switch {
		case w.raw == questplan.PlagueSpokenBravek && w.held(questplan.PlagueObjHangoverCure) > 0:
			w.take(questplan.PlagueObjHangoverCure, 1)
			w.give(questplan.PlagueObjWarrant, 1)
			w.at(questplan.PlagueBravekAnchor)
			return set(questplan.PlagueCured)
		case w.raw == questplan.PlagueCured && w.held(questplan.PlagueObjWarrant) == 0:
			w.give(questplan.PlagueObjWarrant, 1)
			return true
		}
		return w.need(false, "the cure to Bravek")
	case questplan.CustomPlagueHouse:
		if !w.need((w.area() == questplan.PlagueWest || w.area() == questplan.PlagueCellar) && w.raw == questplan.PlagueCured, "the plague house") {
			return false
		}
		if w.area() == questplan.PlagueWest {
			if !w.need(w.held(questplan.PlagueObjWarrant) > 0, "the warrant for the plague house's door") {
				return false
			}
			if w.total(questplan.PlagueObjKey) == 0 {
				w.give(questplan.PlagueObjKey, 1)
			}
		}
		if !w.need(w.held(questplan.PlagueObjKey) > 0, "the key for the cell") {
			return false
		}
		w.at(questplan.PlagueElenaAnchor)
		return set(questplan.PlagueFreed)
	case questplan.CustomPlagueFinish:
		if !w.need(w.east() && w.raw == questplan.PlagueFreed, "Edmond told") {
			return false
		}
		// quest_elena_complete: the picture, key, warrant, note and book are taken back, the scroll is given.
		for _, id := range []int{questplan.PlagueObjPicture, questplan.PlagueObjKey, questplan.PlagueObjWarrant, questplan.PlagueObjNote, questplan.PlagueObjBook} {
			w.sim.Remove(int32(id), w.held(id))
		}
		w.give(questplan.PlagueObjScroll, 1)
		return set(questplan.PlagueComplete)
	case questplan.CustomPlagueScroll:
		if !w.need(w.east() && w.held(questplan.PlagueObjScroll) > 0, "the scroll") {
			return false
		}
		w.take(questplan.PlagueObjScroll, 1)
		return true
	case questplan.CustomPlagueMilk:
		if !w.need(w.east() && w.held(questplan.ObjBucket) > 0, "milking needs an empty bucket") {
			return false
		}
		w.take(questplan.ObjBucket, 1)
		w.give(questplan.ObjBucketOfMilk, 1)
		return true
	case questplan.CustomPlagueGrass:
		if !w.need(w.east(), "the hobgoblins' trail is on the mainland") {
			return false
		}
		w.give(questplan.PlagueObjSnapeGrass, 1)
		return true
	case questplan.CustomPlagueBrew:
		made := 0
		for i := 0; i < 4; i++ {
			use, on, ok := questplan.PlagueNextMix(func(id int) bool { return w.held(id) > 0 })
			if !ok {
				break
			}
			switch use {
			case questplan.PlagueObjPestle: // the pestle stays
				w.take(on, 1)
				w.give(questplan.PlagueObjChocolateDust, 1)
			case questplan.PlagueObjChocolateDust:
				w.take(use, 1)
				w.take(on, 1)
				w.give(questplan.PlagueObjChocolateMilk, 1)
			case questplan.PlagueObjSnapeGrass:
				w.take(use, 1)
				w.take(on, 1)
				w.give(questplan.PlagueObjHangoverCure, 1)
			}
			made++
		}
		return w.need(made > 0, "a mix")
	}
	w.t.Fatalf("no content rule for the step %q", name)
	return false
}

// ran is the position of a step in the order the run took them, the first at or after from (-1 when none).
func (w *pwRig) ran(name string, from int) int {
	for i := from; i < len(w.order); i++ {
		if w.order[i] == name {
			return i
		}
	}
	return -1
}

// inOrder reports whether want is a subsequence of what the run did, and the first one that is missing.
func (w *pwRig) inOrder(want ...string) (string, bool) {
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

func pwPack(items ...int) map[int]int {
	m := map[int]int{questplan.CoinsID: 500}
	for i := 0; i+1 < len(items); i += 2 {
		m[items[i]] += items[i+1]
	}
	return m
}

func (w *pwRig) wantComplete(res questrunner.Result) {
	w.t.Helper()
	if res.Outcome != questrunner.OutcomeComplete {
		w.t.Fatalf("the run ended %s (%s)\nsteps: %v\nlog: %v", res.Outcome, res.Reason, w.order, w.log)
	}
	if w.raw < questplan.PlagueComplete {
		w.t.Errorf("complete at stage %d", w.raw)
	}
	// A run that found the quest done at once is a completion that was not earned (Ticks 1); any other did work.
	if cs := w.e.Store.Record(questplan.PlagueCityID).Completions; len(cs) != 1 || cs[0].Earned() != (len(w.order) > 0) || cs[0].Account != "Test Player" {
		w.t.Errorf("completions %+v after %d steps, want one, earned only if the run did work", cs, len(w.order))
	}
	if w.held(questplan.PlagueObjScroll) != 0 && w.east() {
		w.t.Errorf("the scroll was not read")
	}
}

// ---------------------------------------------------------------------------------------------------------------------

// The whole quest from nothing, in the content's order and the geography's: Edmond and Alrena, the water, the whole of the cure
// assembled on the mainland (the bank pass, the shops' parts, the mix, the hobgoblins' grass, the cow's milk, the rest of the mix)
// before the first crossing, the dig, the rope, the pipe, Jethick, the Rehnisons, Milli, the mourner, the clerk and Bravek, the
// warrant, Elena, home, Edmond and the scroll.
func TestPlagueCityRunsToCompleteThroughTheEngine(t *testing.T) {
	w := newPW(t, 0, pwBank, pwPack(), nil)
	res := w.run()
	w.wantComplete(res)
	if miss, ok := w.inOrder(
		questplan.CustomPlagueStart, "provision:"+questplan.KitPlagueBerries, questplan.CustomPlagueBerries, questplan.CustomPlagueAlrena,
		questplan.CustomPlagueEdmond, "provision:"+questplan.KitPlagueBasics, questplan.CustomPlagueWater,
		"provision:"+questplan.KitPlagueCureBank, "provision:"+questplan.KitPlagueCureShops, questplan.CustomPlagueBrew, questplan.CustomPlagueGrass,
		questplan.CustomPlagueMilk, questplan.CustomPlagueBrew, questplan.CustomPlagueDig, questplan.CustomPlagueRope, questplan.CustomPlagueGrill,
		questplan.CustomPlagueCross, questplan.CustomPlagueJethick, questplan.CustomPlagueBook, questplan.CustomPlagueParents,
		questplan.CustomPlagueMilli, questplan.CustomPlagueMourner, questplan.CustomPlagueAudience, questplan.CustomPlagueCure,
		questplan.CustomPlagueHouse, questplan.CustomPlagueHome, questplan.CustomPlagueFinish, questplan.CustomPlagueScroll); !ok {
		t.Errorf("%q is out of order or missing in\n%s", miss, strings.Join(w.order, "\n"))
	}
	// One crossing, one way home: a cure made before the first crossing is not made twice, and nobody is walked out of a pocket for nothing.
	for name, want := range map[string]int{questplan.CustomPlagueCross: 1, questplan.CustomPlagueDig: 1, questplan.CustomPlagueHome: 1,
		questplan.CustomPlagueGrass: 1, questplan.CustomPlagueMilk: 1, questplan.CustomPlagueBrew: 2, questplan.CustomPlagueScroll: 1} {
		if w.count[name] != want {
			t.Errorf("%s ran %d times, want %d\n%s", name, w.count[name], want, strings.Join(w.order, "\n"))
		}
	}
	// The quest's own items are taken back at the end (the picture, key, warrant, note and book); the spade, buckets and pestle are the account's.
	for _, id := range []int{questplan.PlagueObjHangoverCure, questplan.PlagueObjWarrant, questplan.PlagueObjBook, questplan.PlagueObjSnapeGrass,
		questplan.PlagueObjChocolateMilk, questplan.PlagueObjChocolateDust, questplan.ObjRope, questplan.PlagueObjScroll, questplan.PlagueObjPicture,
		questplan.PlagueObjKey, questplan.PlagueObjNote} {
		if w.held(id) != 0 {
			t.Errorf("item %d is still in the pack at the end", id)
		}
	}
	if w.wornCount(questplan.PlagueObjGasMask) != 1 {
		t.Errorf("the gas mask is not worn at the end")
	}
	// The module's keep list reaches every bank and deposit step: the quest's items, the food, the coins.
	if len(w.keeps) == 0 {
		t.Fatal("no provision or deposit step was asked for")
	}
	for i, keep := range w.keeps {
		for _, id := range append(questplan.PlagueToolIDs(), questplan.PlagueFoodIDs()...) {
			if !slices.Contains(keep, id) {
				t.Errorf("step %d's keep list lacks item %d", i, id)
			}
		}
	}
	if cs := w.e.Store.Record(questplan.PlagueCityID); len(cs.Completions) != 1 {
		t.Errorf("record %+v", cs)
	}
}

// An account at any stage, wherever the last run left it, runs on from there to the end: the module remembers nothing but the
// journal, the position and the pack. Each row names where the account stands and what it holds; the stubs refuse any step the
// content would (see need), so a decision that sends an account to a door it cannot open fails here.
func TestPlagueCityResumesFromEveryStageAndPlace(t *testing.T) {
	mask := questplan.PlagueObjGasMask
	pic := questplan.PlagueObjPicture
	for _, c := range []struct {
		name string
		raw  int
		at   tile.Tile
		pack map[int]int
		worn bool // the mask is on
		bank map[int]int
		// require lists steps the run must make; avoid the ones it must not.
		require, avoid []string
	}{
		{name: "asked, no berries yet", raw: 1, at: pwBank, pack: pwPack(), require: []string{questplan.CustomPlagueBerries}},
		{name: "a dwellberry in the bank", raw: 1, at: pwBank, pack: pwPack(), bank: map[int]int{questplan.PlagueObjDwellberries: 1},
			require: []string{"provision:" + questplan.KitPlagueBerries}, avoid: []string{questplan.CustomPlagueBerries}},
		{name: "the mask made", raw: 2, at: pwBank, pack: pwPack(mask, 1), require: []string{questplan.CustomPlagueEdmond}},
		{name: "the soil wants water", raw: 3, at: questplan.PlagueGarden, pack: pwPack(mask, 1), require: []string{questplan.CustomPlagueWater}},
		{name: "the soil is soft, everything in hand", raw: 7, at: questplan.PlagueGarden,
			pack: pwPack(mask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, pic, 1, questplan.ObjBucket, 4), avoid: []string{"provision:" + questplan.KitPlagueBasics}},
		{name: "dug, in the sewer", raw: 8, at: questplan.PlagueSewerLanding, pack: pwPack(mask, 1, questplan.ObjRope, 1, pic, 1, questplan.ObjBucket, 4),
			require: []string{questplan.CustomPlagueRope}},
		{name: "dug, at the grill with a rope", raw: 9, at: questplan.PlagueSewerPipeEnd, pack: pwPack(mask, 1, pic, 1), require: []string{questplan.CustomPlagueGrill}},
		{name: "the pipe open, no cure: crossed anyway and back for the cure", raw: 10, at: questplan.PlagueSewerPipeEnd, pack: pwPack(mask, 1, pic, 1),
			require: []string{questplan.CustomPlagueCross, questplan.CustomPlagueJethick, questplan.CustomPlagueHome, questplan.CustomPlagueBrew}},
		{name: "the pipe open, the mask on", raw: 10, at: questplan.PlagueSewerPipeEnd, pack: pwPack(pic, 1), worn: true,
			require: []string{questplan.CustomPlagueCross}},
		{name: "Jethick has spoken, book in hand", raw: 20, at: questplan.PlagueWestLanding, pack: pwPack(pic, 1, questplan.PlagueObjBook, 1), worn: true,
			require: []string{questplan.CustomPlagueBook}},
		{name: "Jethick has spoken, the book lost", raw: 20, at: questplan.PlagueWestLanding, pack: pwPack(pic, 1), worn: true,
			require: []string{questplan.CustomPlagueParents, questplan.CustomPlagueJethick, questplan.CustomPlagueBook}},
		{name: "the book returned (stage 21)", raw: 21, at: questplan.PlagueTedAnchor, pack: pwPack(pic, 1), worn: true,
			require: []string{questplan.CustomPlagueParents}, avoid: []string{questplan.CustomPlagueBook}},
		{name: "the Rehnisons have spoken", raw: 22, at: questplan.PlagueTedAnchor, pack: pwPack(pic, 1), worn: true, require: []string{questplan.CustomPlagueMilli}},
		{name: "upstairs with Milli", raw: 22, at: questplan.PlagueMilliAnchor, pack: pwPack(pic, 1), worn: true, require: []string{questplan.CustomPlagueMilli}},
		{name: "upstairs, Milli done", raw: 23, at: questplan.PlagueMilliAnchor, pack: pwPack(pic, 1), worn: true,
			require: []string{questplan.CustomPlagueDown, questplan.CustomPlagueMourner}},
		{name: "Milli has spoken", raw: 23, at: questplan.PlagueTedAnchor, pack: pwPack(pic, 1), worn: true, require: []string{questplan.CustomPlagueMourner}},
		{name: "the mourner has spoken (stage 24)", raw: 24, at: questplan.PlagueHouseDoor, pack: pwPack(pic, 1), worn: true, require: []string{questplan.CustomPlagueAudience}},
		{name: "the clerk has called Bravek (stage 25)", raw: 25, at: questplan.PlagueClerkAnchor, pack: pwPack(pic, 1), worn: true, require: []string{questplan.CustomPlagueAudience}},
		{name: "the recipe known, the cure in hand", raw: 26, at: questplan.PlagueBravekAnchor, pack: pwPack(pic, 1, questplan.PlagueObjHangoverCure, 1), worn: true,
			require: []string{questplan.CustomPlagueCure, questplan.CustomPlagueHouse}},
		{name: "the recipe known, no cure: home, the cure, and back", raw: 26, at: questplan.PlagueBravekAnchor, pack: pwPack(pic, 1), worn: true,
			require: []string{questplan.CustomPlagueHome, questplan.CustomPlagueBrew, questplan.CustomPlagueDig, questplan.CustomPlagueCross, questplan.CustomPlagueCure}},
		{name: "the warrant given", raw: 27, at: questplan.PlagueBravekAnchor, pack: pwPack(pic, 1, questplan.PlagueObjWarrant, 1), worn: true,
			require: []string{questplan.CustomPlagueHouse}, avoid: []string{questplan.CustomPlagueCure}},
		{name: "cured but the warrant lost", raw: 27, at: questplan.PlagueBravekAnchor, pack: pwPack(pic, 1), worn: true,
			require: []string{questplan.CustomPlagueCure, questplan.CustomPlagueHouse}},
		{name: "in the cellar", raw: 27, at: questplan.PlagueCellarLanding, pack: pwPack(pic, 1, questplan.PlagueObjWarrant, 1, questplan.PlagueObjKey, 1), worn: true,
			require: []string{questplan.CustomPlagueHouse, questplan.CustomPlagueFinish}},
		{name: "freed, still in the cellar", raw: 28, at: questplan.PlagueElenaAnchor, pack: pwPack(), worn: true,
			require: []string{questplan.CustomPlagueHome, questplan.CustomPlagueFinish}},
		{name: "freed, in the garden", raw: 28, at: questplan.PlagueGarden, pack: pwPack(), worn: true, require: []string{questplan.CustomPlagueFinish}},
		{name: "done, the scroll unread", raw: 29, at: questplan.PlagueGarden, pack: pwPack(questplan.PlagueObjScroll, 1), require: []string{questplan.CustomPlagueScroll}},
		{name: "done, left in the city", raw: 29, at: questplan.PlagueWestLanding, pack: pwPack(questplan.PlagueObjScroll, 1),
			avoid: []string{questplan.CustomPlagueScroll, questplan.CustomPlagueHome}},
		{name: "done, nothing to do", raw: 29, at: pwBank, pack: pwPack(), avoid: []string{questplan.CustomPlagueScroll}},
		{name: "the cure in the bank", raw: 7, at: questplan.PlagueGarden, pack: pwPack(mask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, pic, 1, questplan.ObjBucket, 2),
			bank: map[int]int{questplan.PlagueObjHangoverCure: 1}, require: []string{"provision:" + questplan.KitPlagueCureBank},
			avoid: []string{questplan.CustomPlagueGrass, questplan.CustomPlagueMilk, questplan.CustomPlagueBrew, "provision:" + questplan.KitPlagueCureShops}},
		{name: "the chocolaty milk and grass in the bank", raw: 7, at: questplan.PlagueGarden,
			pack:    pwPack(mask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, pic, 1, questplan.ObjBucket, 2),
			bank:    map[int]int{questplan.PlagueObjChocolateMilk: 1, questplan.PlagueObjSnapeGrass: 1},
			require: []string{questplan.CustomPlagueBrew}, avoid: []string{questplan.CustomPlagueGrass, questplan.CustomPlagueMilk, "provision:" + questplan.KitPlagueCureShops}},
		{name: "a bucket of milk and a bar in the bank", raw: 7, at: questplan.PlagueGarden,
			pack:    pwPack(mask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, pic, 1, questplan.ObjBucket, 2),
			bank:    map[int]int{questplan.ObjBucketOfMilk: 1, questplan.PlagueObjChocolateBar: 1, questplan.PlagueObjPestle: 1},
			require: []string{questplan.CustomPlagueBrew, questplan.CustomPlagueGrass}, avoid: []string{questplan.CustomPlagueMilk, "provision:" + questplan.KitPlagueCureShops}},
		{name: "Jethick's book in the bank, the account home", raw: 20, at: questplan.PlagueGarden, pack: pwPack(mask, 1, pic, 1),
			bank:    map[int]int{questplan.PlagueObjBook: 1},
			require: []string{"provision:" + questplan.KitPlagueBook, questplan.CustomPlagueBook}},
		{name: "the mask in the bank, the account home", raw: 10, at: questplan.PlagueGarden,
			pack: pwPack(questplan.ObjSpade, 1, pic, 1), bank: map[int]int{questplan.PlagueObjGasMask: 1},
			require: []string{"provision:" + questplan.KitPlagueMask}, avoid: []string{questplan.CustomPlagueCupboard}},
		{name: "no mask anywhere, the account home", raw: 10, at: questplan.PlagueGarden, pack: pwPack(questplan.ObjSpade, 1, pic, 1),
			require: []string{"provision:" + questplan.KitPlagueMask, questplan.CustomPlagueCupboard}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPW(t, c.raw, c.at, c.pack, c.bank)
			if c.worn {
				w.sim.Wear(0, int32(mask))
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

// A pack with no room is emptied of everything that is not the quest's (the deposit's keep list) before the work, and the quest's
// items stay in it.
func TestPlagueCityDepositsJunkAndKeepsTheQuestsItems(t *testing.T) {
	pack := pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.PlagueObjPicture, 1, questplan.ObjRope, 1)
	for id := 3000; id < 3022; id++ {
		pack[id] = 1
	}
	w := newPW(t, questplan.PlagueGasMask+1, questplan.PlagueGarden, pack, nil)
	w.wantComplete(w.run())
	if w.ran("step:deposit", 0) < 0 {
		t.Fatalf("a full pack was not deposited:\n%s", strings.Join(w.order, "\n"))
	}
	for id := 3000; id < 3022; id++ {
		if w.bank[id] != 1 {
			t.Errorf("junk item %d: %d in the bank", id, w.bank[id])
		}
	}
	if w.ran(questplan.CustomPlagueWater, 0) < w.ran("step:deposit", 0) {
		t.Errorf("the water was fetched before the pack was emptied:\n%s", strings.Join(w.order, "\n"))
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Parks and refusals.
// ---------------------------------------------------------------------------------------------------------------------

func pwLevels(attack, strength, defence, hp int) func(*questplan.Snapshot) {
	return func(s *questplan.Snapshot) {
		s.Levels = map[string]int{"attack": attack, "strength": strength, "defence": defence, "hitpoints": hp, "prayer": 1, "ranged": 1, "magic": 1}
		s.HP, s.MaxHP = hp, hp
	}
}

// A leg that fails is a verdict for the run: the module waits, the engine parks the quest with the leg named, and the next run
// starts it afresh.
func TestPlagueCityALegThatFailsParksTheQuestWithItsName(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  int
		at   tile.Tile
		pack map[int]int
		fail string
		want string
	}{
		{"the grass", 7, questplan.PlagueGarden, pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, questplan.PlagueObjPicture, 1, questplan.ObjBucket, 2),
			questplan.CustomPlagueGrass, questplan.CustomPlagueGrass},
		{"the milk", 7, questplan.PlagueGarden, pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, questplan.PlagueObjPicture, 1, questplan.ObjBucket, 2),
			questplan.CustomPlagueMilk, questplan.CustomPlagueMilk},
		{"the dig", 7, questplan.PlagueGarden, pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, questplan.PlagueObjPicture, 1, questplan.ObjBucket, 2,
			questplan.PlagueObjHangoverCure, 1), questplan.CustomPlagueDig, questplan.CustomPlagueDig},
		{"the berries", 1, pwBank, pwPack(), questplan.CustomPlagueBerries, questplan.CustomPlagueBerries},
		{"the mourner", 23, questplan.PlagueTedAnchor, pwPack(questplan.PlagueObjPicture, 1), questplan.CustomPlagueMourner, "mourner"},
		{"the plague house", 27, questplan.PlagueBravekAnchor, pwPack(questplan.PlagueObjWarrant, 1), questplan.CustomPlagueHouse, "plague house"},
		{"the way home", 28, questplan.PlagueElenaAnchor, pwPack(), questplan.CustomPlagueHome, "no way out"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newPW(t, c.raw, c.at, c.pack, nil)
			w.sim.Wear(0, int32(questplan.PlagueObjGasMask))
			w.fail[c.fail] = true
			res := w.run()
			if res.Outcome != questrunner.OutcomeParked {
				t.Fatalf("the run ended %s (%s), want a park\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
			}
			if !strings.Contains(res.Reason, c.want) {
				t.Errorf("the park reason %q does not name %q", res.Reason, c.want)
			}
			if w.count[c.fail] != 1 {
				t.Errorf("%s ran %d times: a failed leg is not retried within the run", c.fail, w.count[c.fail])
			}
			if parked, _ := w.e.Store.Parked(questplan.PlagueCityID, time.Now()); !parked {
				t.Error("the park is not in the side store")
			}
		})
	}
}

// An account the guard dogs or the hobgoblins could kill is not sent to them: it is given food once and then the quest is left alone
// for six hours, with the reason, instead of looping or dying.
func TestPlagueCityWeakAccountIsNotSentToTheDogsOrTheHobgoblins(t *testing.T) {
	t.Run("the dogs", func(t *testing.T) {
		w := newPW(t, 1, pwBank, pwPack(), map[int]int{questplan.PlagueObjShark: 3})
		w.extra = pwLevels(1, 1, 1, 10)
		res := w.run()
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "McGrubor's Wood") {
			t.Fatalf("the run ended %s (%s), want a park on McGrubor's Wood\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
		}
		if w.count[questplan.CustomPlagueBerries] != 0 {
			t.Error("the account went into the wood")
		}
		if w.count["provision:"+questplan.KitPlagueFood] != 1 {
			t.Errorf("food was fetched %d times, want once", w.count["provision:"+questplan.KitPlagueFood])
		}
		parked, why := w.e.Store.Parked(questplan.PlagueCityID, time.Now())
		until := time.UnixMilli(w.e.Store.Record(questplan.PlagueCityID).BackoffUntil)
		if !parked || !strings.Contains(why, "McGrubor's Wood") || time.Until(until) < 5*time.Hour {
			t.Errorf("parked %v (%q) until %v, want about six hours", parked, why, until)
		}
	})
	t.Run("the hobgoblins", func(t *testing.T) {
		w := newPW(t, 7, questplan.PlagueGarden, pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, questplan.PlagueObjPicture, 1, questplan.ObjBucket, 2),
			map[int]int{questplan.PlagueObjShark: 3})
		w.extra = pwLevels(5, 5, 5, 14)
		res := w.run()
		if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "hobgoblins") {
			t.Fatalf("the run ended %s (%s), want a park on the hobgoblins\n%s", res.Outcome, res.Reason, strings.Join(w.order, "\n"))
		}
		if w.count[questplan.CustomPlagueGrass] != 0 {
			t.Error("the account went onto the trail")
		}
		// What can be had without the trail was had first: the shops' parts and the first mix.
		if w.ran("provision:"+questplan.KitPlagueCureShops, 0) < 0 || w.ran(questplan.CustomPlagueBrew, 0) < 0 {
			t.Errorf("the shops' parts were not fetched before the refusal:\n%s", strings.Join(w.order, "\n"))
		}
	})
	t.Run("a strong account goes", func(t *testing.T) {
		w := newPW(t, 7, questplan.PlagueGarden, pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, questplan.PlagueObjPicture, 1, questplan.ObjBucket, 2), nil)
		w.extra = pwLevels(70, 70, 70, 70)
		w.wantComplete(w.run())
		if w.count["provision:"+questplan.KitPlagueFood] != 0 {
			t.Error("a strong account asked for food")
		}
	})
	t.Run("a weak one with food in hand goes, the model willing", func(t *testing.T) {
		w := newPW(t, 7, questplan.PlagueGarden, pwPack(questplan.PlagueObjGasMask, 1, questplan.ObjSpade, 1, questplan.ObjRope, 1, questplan.PlagueObjPicture, 1, questplan.ObjBucket, 2,
			questplan.PlagueObjShark, 4), nil)
		w.extra = pwLevels(40, 40, 40, 45) // combat 49: hunted, 45 hitpoints and four sharks
		res := w.run()
		w.wantComplete(res)
	})
}

// Two deaths park the quest for a day; a death on the trail or in the wood parks the legs for a day. The module declares both.
func TestPlagueCityDeathPolicy(t *testing.T) {
	m := questplan.PlagueCity
	if m.MaxDeaths != 2 || !m.NoRetaliate {
		t.Errorf("MaxDeaths %d, NoRetaliate %v", m.MaxDeaths, m.NoRetaliate)
	}
	for _, leg := range []string{questplan.CustomPlagueGrass, questplan.CustomPlagueBerries} {
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
func TestPlagueCityEveryCustomHasAnExecutor(t *testing.T) {
	q := PlagueCity()
	names := questplan.PlagueCustomNames()
	for _, n := range names {
		if q.Customs[n] == nil {
			t.Errorf("no executor for %q", n)
		}
	}
	if len(q.Customs) != len(names) {
		t.Errorf("%d executors for %d custom steps", len(q.Customs), len(names))
	}
	if q.ID() != questplan.PlagueCityID {
		t.Errorf("the factory built %q", q.ID())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The executors that need no map.
// ---------------------------------------------------------------------------------------------------------------------

func TestPlagueBrewMakesTheCureInThreeMixes(t *testing.T) {
	w := newExecWorld(actionsim.Stack{ID: questplan.PlagueObjPestle, Count: 1}, actionsim.Stack{ID: questplan.PlagueObjChocolateBar, Count: 1},
		actionsim.Stack{ID: questplan.ObjBucketOfMilk, Count: 1}, actionsim.Stack{ID: questplan.PlagueObjSnapeGrass, Count: 1})
	w.sim.Combine(questplan.PlagueObjPestle, questplan.PlagueObjChocolateBar, questplan.PlagueObjChocolateDust)
	w.sim.Combine(questplan.PlagueObjChocolateDust, questplan.ObjBucketOfMilk, questplan.PlagueObjChocolateMilk)
	w.sim.Combine(questplan.PlagueObjSnapeGrass, questplan.PlagueObjChocolateMilk, questplan.PlagueObjHangoverCure)
	if !plagueBrew(w.e, questplan.Step{}) {
		t.Fatalf("the brew failed: %v", *w.log)
	}
	if w.held(questplan.PlagueObjHangoverCure) != 1 || len(w.sim.Pack()) != 1 {
		t.Errorf("pack %+v, want the cure and nothing else", w.sim.Pack())
	}
	// With the cure made there is nothing to mix, and the step says it made nothing.
	if plagueBrew(w.e, questplan.Step{}) {
		t.Error("a second brew claimed to make something")
	}
}

func TestPlagueBrewStopsWhereThePackRunsOut(t *testing.T) {
	// Dust and milk but no grass: the chocolaty milk is made and the step succeeds, the cure is the grass leg's to finish.
	w := newExecWorld(actionsim.Stack{ID: questplan.PlagueObjChocolateDust, Count: 1}, actionsim.Stack{ID: questplan.ObjBucketOfMilk, Count: 1})
	w.sim.Combine(questplan.PlagueObjChocolateDust, questplan.ObjBucketOfMilk, questplan.PlagueObjChocolateMilk)
	if !plagueBrew(w.e, questplan.Step{}) || w.held(questplan.PlagueObjChocolateMilk) != 1 {
		t.Errorf("the brew made %+v: %v", w.sim.Pack(), *w.log)
	}
	// A rule that is not the content's (the sim has no combine): the use does nothing and the step fails rather than loops.
	w = newExecWorld(actionsim.Stack{ID: questplan.PlagueObjSnapeGrass, Count: 1}, actionsim.Stack{ID: questplan.PlagueObjChocolateMilk, Count: 1})
	if plagueBrew(w.e, questplan.Step{}) {
		t.Errorf("a mix the world did not make succeeded: %+v", w.sim.Pack())
	}
	// Nothing to mix.
	w = newExecWorld()
	if plagueBrew(w.e, questplan.Step{}) {
		t.Error("an empty pack brewed")
	}
}

func TestPlagueScrollAndGrassReturnAtOnceWhenThereIsNothingToDo(t *testing.T) {
	w := newExecWorld()
	if !plagueScroll(w.e, questplan.Step{}) {
		t.Errorf("no scroll in the pack is not a failure: %v", *w.log)
	}
	w = newExecWorld(actionsim.Stack{ID: questplan.PlagueObjSnapeGrass, Count: 1})
	if !plagueGrass(w.e, questplan.Step{}) {
		t.Errorf("the grass leg failed with the grass in the pack: %v", *w.log)
	}
	// A pack with no room for the grass does not walk to the trail.
	var full []actionsim.Stack
	for id := 3000; id < 3028; id++ {
		full = append(full, actionsim.Stack{ID: int32(id), Count: 1})
	}
	w = newExecWorld(full...)
	if plagueGrass(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no room") {
		t.Errorf("a full pack went for the grass: %v", *w.log)
	}
}

func TestPlagueRopeAndCrossRefuseWithoutTheirItems(t *testing.T) {
	w := newExecWorld()
	if plagueRope(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "no rope") {
		t.Errorf("a rope leg without a rope: %v", *w.log)
	}
	w = newExecWorld()
	if plagueCross(w.e, questplan.Step{}) || !strings.Contains(strings.Join(*w.log, "\n"), "gas mask") {
		t.Errorf("the pipe without a mask: %v", *w.log)
	}
}
