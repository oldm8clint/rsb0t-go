package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"rsb0t-go/internal/scripts/tile"
)

// ---- the journal ----

// The text the client renders for each stage of haunted_journal.rs2, as the quest tab shows it (the "|" is the client's own line
// break, "@str@" a struck-through line and "@dre@" a coloured word): the later stages hold the earlier ones' struck lines, and
// only the started stage says "He went into".
var ernestJournalFixtures = map[int]string{
	ErnestNotStarted: "@dbl@I can start this quest by speaking to @dre@Veronica@dbl@ who is outside @dre@Draynor Manor||@dbl@There aren't any requirements for this quest",
	ErnestStarted:    "@str@I have spoken to Veronica||@dbl@I need to find @dre@Ernest|@dbl@He went into @dre@Draynor Manor@dbl@ and hasn't returned",
	ErnestSpokeOddenstein: "@str@I have spoken to Veronica||@str@I've spoken to Dr Oddenstein, and discovered Ernest is a|@str@chicken||" +
		"@dbl@I need to bring @dre@Dr Oddenstein@dbl@ parts for his machine|@dre@1 Oil Can|@dre@1 Pressure Gauge|@dre@1 Rubber Tube",
	ErnestComplete: "@str@I have spoken to Veronica||@str@I have collected all the parts for the machine||@str@Dr Oddenstein thanked me for helping fix his machine|" +
		"@str@We turned Ernest back to normal and he rewarded me|@red@QUEST COMPLETE!",
}

func TestParseErnestChickenJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, text := range ernestJournalFixtures {
		for _, lines := range [][]string{{text}, strings.Split(text, "|")} {
			got, ok := ParseErnestChickenJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d: read as %d (ok %v) from %q", stage, got, ok, lines)
			}
		}
	}
	// Stage 2 renders the parts the pack holds struck through: the same stage.
	held := strings.NewReplacer("@dre@1 Oil Can", "@str@1 Oil Can", "@dre@1 Pressure Gauge", "@str@1 Pressure Gauge", "@dre@1 Rubber Tube", "@str@1 Rubber Tube")
	if got, ok := ParseErnestChickenJournal([]string{held.Replace(ernestJournalFixtures[ErnestSpokeOddenstein])}); !ok || got != ErnestSpokeOddenstein {
		t.Errorf("stage 2 with every part struck through read as %d (ok %v)", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"I can start this quest by talking to Veronica"}, {"@dbl@Something else entirely."}} {
		if got, ok := ParseErnestChickenJournal(lines); ok {
			t.Errorf("%q read as stage %d: an unrecognised journal is unknown, never a stage", lines, got)
		}
	}
	if len(ernestJournalFixtures) != 4 {
		t.Errorf("%d rendered stages, the content has 4", len(ernestJournalFixtures))
	}
}

// ---- the small rules ----

func TestTheClosetSkeletonHuntsAccountsOfFiftyAndUnder(t *testing.T) {
	for combat, want := range map[int]bool{0: true, 3: true, 25: true, 50: true, 51: false, 63: false, 126: false} {
		if got := ErnestSkeletonHunts(combat); got != want {
			t.Errorf("combat %d: hunted %v, want %v", combat, got, want)
		}
	}
	// Four of its blows, three fifths of the account's hitpoints, never over the maximum.
	for _, c := range []struct{ max, want int }{{0, 0}, {10, 10}, {12, 12}, {20, 12}, {30, 18}, {53, 31}, {99, 59}} {
		if got := ErnestClosetFloor(c.max); got != c.want {
			t.Errorf("max %d: floor %d, want %d", c.max, got, c.want)
		}
	}
	if FoeManorSkeleton.MaxHit() != ErnestSkeletonPeakHit || FoeManorSkeleton.PeakHit() != ErnestSkeletonPeakHit {
		t.Errorf("the skeleton hits for %d at most, the plan says %d", FoeManorSkeleton.PeakHit(), ErnestSkeletonPeakHit)
	}
	if f, ok := FoeByNPC(NpcManorSkeleton); !ok || f != FoeManorSkeleton {
		t.Errorf("the foe table does not hold the skeleton: %+v %v", f, ok)
	}
}

func TestTheRecordTheStopsAndTheModuleAreWellFormed(t *testing.T) {
	m := ErnestChicken
	if m.Record.ID != ErnestChickenID || m.Record.Name != ErnestChickenName || m.Record.QuestPoints != 4 || len(m.Record.Requirements.Skills) != 0 {
		t.Errorf("record %+v", m.Record)
	}
	if QuestTabName(ErnestChickenID) != ErnestChickenName {
		t.Errorf("QuestTabName = %q", QuestTabName(ErnestChickenID))
	}
	if m.Decide == nil || m.ReadStage == nil || m.MaxDeaths <= 0 || !m.NoRetaliate || len(m.Sustain.FoodIDs) == 0 || m.Sustain.EatAt <= 0 {
		t.Errorf("module %+v lacks a decision, a journal reader, a death cap, no-retaliate or a sustain", m)
	}
	for _, stop := range []NpcStop{VeronicaStop, OddensteinStop} {
		if stop.ID == 0 || stop.Leash <= 0 || len(stop.Prefer) == 0 || stop.Anchor == (tile.Tile{}) {
			t.Errorf("stop %+v is not well formed", stop)
		}
	}
	// Tools are never food (EatFloor leaves a quest's tools alone) and food is kept by every bank visit.
	for _, id := range ernestTools {
		if slices.Contains(ernestFoods, id) {
			t.Errorf("tool %d is also food", id)
		}
	}
	keep := ErnestKeepIDs()
	for _, id := range append(ErnestToolIDs(), ernestFoods...) {
		if !slices.Contains(keep, id) {
			t.Errorf("a bank visit would bank %d", id)
		}
	}
	for _, id := range ernestFoods {
		if FoodBiteHeal(id) == 0 {
			t.Errorf("food %d is not in the food table", id)
		}
	}
	// The ladder row says what the module does.
	f := LadderFacts[ErnestChickenID]
	if !f.Filler || f.QP != 4 || f.Name != ErnestChickenName || f.EstMinutes <= 0 || f.Region == "" || f.Source == "" || len(f.Gates) != 0 || len(f.Prereqs) != 0 || f.QPNeeded != 0 || f.Finale || len(f.XP) != 0 {
		t.Errorf("ladder row %+v", f)
	}
	if files := ModuleFiles[ErnestChickenID]; len(files) < 2 {
		t.Errorf("ModuleFiles %v", files)
	}
	if len(ErnestCustomNames()) != 10 {
		t.Errorf("%d custom names", len(ErnestCustomNames()))
	}
}

// ---- the kit ----

func kitIDs(kit []KitLine) []int {
	var ids []int
	for _, l := range kit {
		ids = append(ids, l.ID)
	}
	return ids
}

func TestTheKitAsksOnlyForWhatTheAccountLacks(t *testing.T) {
	snap := func(held ...int) Snapshot {
		s := Snapshot{InvIDs: map[int]int{}}
		for _, id := range held {
			s.InvIDs[id] = 1
		}
		return s
	}
	all := ernestKit(snap())
	for _, id := range []int{ObjOilCan, ObjPressureGauge, ObjPoisonedFishFood, ObjFishFood, ObjPoison, ObjRubberTube, ObjClosetKey, ObjSpade} {
		if !slices.Contains(kitIDs(all), id) {
			t.Errorf("the empty-handed kit lacks %d: %v", id, kitIDs(all))
		}
	}
	for _, l := range all {
		if !l.Optional {
			t.Errorf("kit line %s is not optional: a short bank must not block the quest", l.Name)
		}
		if len(l.Sources) != 0 {
			t.Errorf("kit line %s lists sources %v: the manor's spawns are walked by the module, not the provisioner", l.Name, l.Sources)
		}
		if why := ValidateKitLine(l); why != "" {
			t.Error(why)
		}
	}
	if k := kitIDs(ernestKit(snap(ObjOilCan, ObjPressureGauge, ObjRubberTube))); len(k) != 0 {
		t.Errorf("a pack holding every part asks for %v", k)
	}
	if k := kitIDs(ernestKit(snap(ObjOilCan, ObjRubberTube))); !slices.Equal(k, []int{ObjPressureGauge, ObjPoisonedFishFood, ObjFishFood, ObjPoison}) {
		t.Errorf("a pack lacking the gauge asks for %v", k)
	}
	if k := kitIDs(ernestKit(snap(ObjOilCan, ObjPressureGauge))); !slices.Equal(k, []int{ObjRubberTube, ObjClosetKey, ObjSpade}) {
		t.Errorf("a pack lacking the tube asks for %v", k)
	}

	// The spade and the key serve the tube, the ingredients serve the gauge: a tube in the bank leaves the plan with no spade to
	// find, a gauge in the bank no poison.
	state := ProvisionState{Pack: map[int]int{}, Bank: map[int]int{ObjRubberTube: 1, ObjPressureGauge: 1, ObjOilCan: 1}, BankKnown: true, FreeSlots: 28}
	plan := PlanProvision(ernestKit(snap()), state)
	if plan.Phase != PhaseBank || len(plan.Withdraw) != 3 {
		t.Fatalf("a bank holding the three parts: %s", plan.Summary())
	}
	state.Pack = map[int]int{ObjRubberTube: 1, ObjPressureGauge: 1, ObjOilCan: 1}
	state.Bank = map[int]int{}
	if plan := PlanProvision(ernestKit(snap(ObjRubberTube, ObjPressureGauge, ObjOilCan)), state); plan.Phase != PhaseSatisfied {
		t.Errorf("a pack holding the three parts: %s", plan.Summary())
	}
	// The mule's spades (it holds 11) are asked for the tube's key, one, and nothing else of the mule's is.
	state = ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 28,
		Mule: map[int]int{ObjSpade: 11, 1511: 500}, MuleKnown: true}
	plan = PlanProvision(ernestKit(snap()), state)
	if plan.Phase != PhaseMule || len(plan.MuleAsk) != 1 || plan.MuleAsk[0].ID != ObjSpade || plan.MuleAsk[0].Count != 1 {
		t.Errorf("an empty bank and a mule with spades: %s", plan.Summary())
	}
	// With nothing anywhere the pass is satisfied, not blocked: every line is optional and the module's own steps go on.
	state = ProvisionState{Pack: map[int]int{}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 28}
	if plan := PlanProvision(ernestKit(snap()), state); plan.Phase != PhaseSatisfied {
		t.Errorf("nothing banked: %s", plan.Summary())
	}
}

// ---- a world to run the module in ----

// ernestWorld is the account and the manor, with the content's own rules for each step (the scripts named in the comments).
type ernestWorld struct {
	t      *testing.T
	stage  int
	inv    map[int]int
	bank   map[int]int
	ran    map[string]bool
	failed map[string]bool
	log    []string
	tile   tile.Tile
	// poisoned is %haunted_manor_fountain_poisoned.
	poisoned bool
	// bites counts the piranhas' bites, empty the custom steps whose spawn or part cannot be had, free the free slots to
	// hold back.
	bites int
	empty map[string]bool
}

func newErnestWorld(t *testing.T, stage int, inv, bank map[int]int) *ernestWorld {
	if inv == nil {
		inv = map[int]int{}
	}
	if bank == nil {
		bank = map[int]int{}
	}
	return &ernestWorld{t: t, stage: stage, inv: inv, bank: bank, ran: map[string]bool{}, failed: map[string]bool{}, empty: map[string]bool{},
		tile: ErnestFishFoodSpawn}
}

func (w *ernestWorld) used() int {
	n := 0
	for id, c := range w.inv {
		if c > 0 {
			if id == CoinsID {
				n++
			} else {
				n += c
			}
		}
	}
	return n
}

func (w *ernestWorld) snapshot() Snapshot {
	s := Snapshot{InvIDs: map[int]int{}, Inv: map[string]int{}, WornIDs: map[int]int{}, Worn: map[string]int{},
		Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}, Tile: w.tile, TileKnown: true,
		BankKnown: true, BankIDs: map[int]int{}}
	for id, n := range w.inv {
		if n > 0 {
			s.InvIDs[id] = n
		}
	}
	for id, n := range w.bank {
		s.BankIDs[id] = n
	}
	s.FreeSlots = 28 - w.used()
	for k, v := range w.ran {
		s.Ran[k] = v
	}
	for k, v := range w.failed {
		s.Failed[k] = v
	}
	switch {
	case w.stage == ErnestNotStarted:
		s.Journal = StatusNotStarted
	case w.stage >= ErnestComplete:
		s.Journal = StatusComplete
	default:
		s.Journal, s.Stage, s.StageKnown = StatusInProgress, w.stage, true
	}
	return s
}

// provision plays the bank and mule rungs of the real planner over the world (the module lists no ground source: PhaseSource is a bug).
func (w *ernestWorld) provision(st Step) bool {
	for pass := 0; pass < 16; pass++ {
		s := w.snapshot()
		plan := PlanProvision(st.Kit, ProvisionState{Pack: s.InvIDs, Bank: w.bank, BankKnown: true, FreeSlots: s.FreeSlots,
			Levels: map[string]int{}, Exhausted: map[string]bool{}})
		switch plan.Phase {
		case PhaseSatisfied:
			return true
		case PhaseBank:
			moved := false
			for _, c := range plan.Withdraw {
				n := minInt(c.Count, w.bank[c.ID])
				if n > 0 {
					w.bank[c.ID] -= n
					w.inv[c.ID] += n
					moved = true
				}
			}
			if !moved {
				return false
			}
		default:
			w.t.Fatalf("the kit's plan went to %s: %s", plan.Phase, plan.Summary())
		}
	}
	return false
}

func (w *ernestWorld) take(id int) bool {
	if w.inv[id] <= 0 {
		return false
	}
	w.inv[id]--
	if w.inv[id] == 0 {
		delete(w.inv, id)
	}
	return true
}

func (w *ernestWorld) has(ids ...int) bool {
	for _, id := range ids {
		if w.inv[id] <= 0 {
			return false
		}
	}
	return true
}

// custom plays one custom step the way the content answers its executor, and reports the executor's success.
func (w *ernestWorld) custom(name string) bool {
	if w.empty[name] {
		return false
	}
	switch name {
	case CustomErnestFishFood:
		w.inv[ObjFishFood]++ // the spawn upstairs
		return true
	case CustomErnestPoison:
		w.inv[ObjPoison]++
		return true
	case CustomErnestSpade:
		w.inv[ObjSpade]++
		return true
	case CustomErnestCan:
		w.inv[ObjOilCan]++ // the maze, then out
		w.tile = ErnestBookcaseStand
		return true
	case CustomErnestProbe:
		// [oploc1,hauntedfountain]: the gauge from a poisoned fountain, a bite for 1 from a live one; either way the search resolved.
		if w.poisoned {
			w.inv[ObjPressureGauge]++
		} else {
			w.bites++
		}
		w.tile = ErnestFountainStand
		return true
	case CustomErnestPour:
		if !w.take(ObjPoisonedFishFood) {
			return false
		}
		w.poisoned = true
		return true
	case CustomErnestGauge:
		if !w.poisoned {
			w.bites++
			return false
		}
		w.inv[ObjPressureGauge]++
		return true
	case CustomErnestKey:
		if !w.has(ObjSpade) || w.has(ObjClosetKey) { // [oplocu,hauntedcompostheap]: a spade, and a key only while the pack holds none
			return false
		}
		w.inv[ObjClosetKey]++
		return true
	case CustomErnestTube:
		if w.tile != ErnestClosetInside && !w.has(ObjClosetKey) {
			return false
		}
		w.inv[ObjRubberTube]++
		w.tile = ErnestClosetOutside
		return true
	case CustomErnestHandIn:
		if !w.has(ObjRubberTube, ObjOilCan, ObjPressureGauge) {
			return false
		}
		w.take(ObjRubberTube)
		w.take(ObjOilCan)
		w.take(ObjPressureGauge)
		w.stage = ErnestComplete
		return true
	}
	w.t.Fatalf("an unknown custom step %q", name)
	return false
}

func (w *ernestWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		ok := w.provision(st)
		key := "provision:" + st.KitName
		if ok {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
		w.log = append(w.log, key)
	case StepDeposit:
		keep := map[int]bool{}
		for _, id := range st.KeepIDs {
			keep[id] = true
		}
		for id, n := range w.inv {
			if !keep[id] && n > 0 {
				w.bank[id] += n
				delete(w.inv, id)
			}
		}
		w.log = append(w.log, "deposit")
	case StepTalk:
		switch st.Stop.ID {
		case NpcVeronica:
			if w.stage == ErnestNotStarted {
				w.stage = ErnestStarted // haunted_start
			}
			w.log = append(w.log, fmt.Sprintf("veronica%d", w.stage))
		case NpcOddenstein:
			if w.stage == ErnestStarted {
				w.stage = ErnestSpokeOddenstein // oddenstein_not_easy
			}
			w.log = append(w.log, fmt.Sprintf("oddenstein%d", w.stage))
		default:
			t.Fatalf("talk to an unexpected npc %d", st.Stop.ID)
		}
	case StepUseOn:
		// [opheldu,poison] and [opheldu,fish_food]: the poisoned fish food, one of each used up.
		if !w.has(ObjPoison, ObjFishFood) || st.ItemID != ObjPoison || st.TargetID != ObjFishFood || st.TargetKind != TargetItem {
			t.Fatalf("a use-on that is not the poison on the fish food, or without both: %+v", st)
		}
		w.take(ObjPoison)
		w.take(ObjFishFood)
		w.inv[ObjPoisonedFishFood]++
		w.log = append(w.log, "combine")
	case StepCustom:
		ok := w.custom(st.Custom)
		if ok {
			w.ran[st.Custom] = true
			delete(w.failed, st.Custom)
		} else {
			w.failed[st.Custom] = true
		}
		w.log = append(w.log, st.Custom)
		if !ok {
			w.log[len(w.log)-1] += "!"
		}
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

// run plays the module to Done or a Wait; waited is the wait's reason.
func (w *ernestWorld) run() (waited string) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideErnestChicken(w.snapshot())
		switch st.Kind {
		case StepDone:
			return ""
		case StepWait:
			return st.Reason
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.ran, w.failed, w.tile, w.poisoned)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.ran, w.failed, w.tile, w.poisoned) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("80 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return ""
}

func (w *ernestWorld) runsTo(want ...string) {
	w.t.Helper()
	if why := w.run(); why != "" {
		w.t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if fmt.Sprint(w.log) != fmt.Sprint(want) {
		w.t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
}

// ---- the whole quest, from several starts ----

func TestAFreshAccountDoesErnestTheChickenInTheOrderThatCrossesTheDoorFewestTimes(t *testing.T) {
	w := newErnestWorld(t, ErnestNotStarted, map[int]int{CoinsID: 10, 385: 6}, nil)
	w.runsTo("provision:ernest", "veronica1", "oddenstein2",
		// inside the manor: the fish food upstairs, the poison below, the two used on each other, the can behind the bookcase,
		CustomErnestFishFood, CustomErnestPoison, "combine", CustomErnestCan,
		// the spade beside the back door, out to the fountain (bitten: pour, search again), the compost heap's key, the closet's tube,
		CustomErnestSpade, CustomErnestProbe, CustomErnestPour, CustomErnestGauge, CustomErnestKey, CustomErnestTube,
		// and the Professor.
		CustomErnestHandIn)
	if w.bites != 1 {
		t.Errorf("%d bites, a bitten fountain costs exactly one hitpoint", w.bites)
	}
	if w.stage != ErnestComplete || !w.poisoned {
		t.Errorf("stage %d, poisoned %v", w.stage, w.poisoned)
	}
	for _, id := range []int{ObjRubberTube, ObjOilCan, ObjPressureGauge, ObjPoisonedFishFood, ObjFishFood, ObjPoison} {
		if w.inv[id] != 0 {
			t.Errorf("%d of obj %d left in the pack", w.inv[id], id)
		}
	}
	if w.inv[385] != 6 || w.inv[CoinsID] != 10 {
		t.Errorf("the food and the coins were touched: %v", w.inv)
	}
}

// Parts the trainer banked between two passes are drawn before any leg is walked, and an account that holds them all needs no bank.
func TestBankedPartsAreDrawnBeforeAnyLegIsWalked(t *testing.T) {
	w := newErnestWorld(t, ErnestSpokeOddenstein, nil, map[int]int{ObjRubberTube: 1, ObjOilCan: 1, ObjPressureGauge: 1})
	w.runsTo("provision:ernest", CustomErnestHandIn)
	if len(w.bank) != 0 && (w.bank[ObjRubberTube] != 0 || w.bank[ObjOilCan] != 0 || w.bank[ObjPressureGauge] != 0) {
		t.Errorf("the bank still holds %v", w.bank)
	}

	w = newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjRubberTube: 1, ObjOilCan: 1, ObjPressureGauge: 1}, nil)
	w.runsTo(CustomErnestHandIn)

	// Two banked, the tube to find: the can and the gauge are not walked for, and the tube's key and spade are.
	w = newErnestWorld(t, ErnestSpokeOddenstein, nil, map[int]int{ObjOilCan: 1, ObjPressureGauge: 1})
	w.runsTo("provision:ernest", CustomErnestSpade, CustomErnestKey, CustomErnestTube, CustomErnestHandIn)

	// A banked key and spade: no compost heap, no spade spawn.
	w = newErnestWorld(t, ErnestSpokeOddenstein, nil, map[int]int{ObjOilCan: 1, ObjPressureGauge: 1, ObjClosetKey: 1})
	w.runsTo("provision:ernest", CustomErnestTube, CustomErnestHandIn)
	w = newErnestWorld(t, ErnestSpokeOddenstein, nil, map[int]int{ObjOilCan: 1, ObjPressureGauge: 1, ObjSpade: 1})
	w.runsTo("provision:ernest", CustomErnestKey, CustomErnestTube, CustomErnestHandIn)

	// The poisoned fish food in the bank: neither ingredient is gathered, and it is poured as soon as the fountain has bitten.
	w = newErnestWorld(t, ErnestSpokeOddenstein, nil, map[int]int{ObjPoisonedFishFood: 1, ObjOilCan: 1})
	w.runsTo("provision:ernest", CustomErnestSpade, CustomErnestProbe, CustomErnestPour, CustomErnestGauge, CustomErnestKey, CustomErnestTube, CustomErnestHandIn)
}

// The varp is unreadable, so the fountain is searched first: one an earlier run poisoned hands the gauge over, and the pour is
// never made. The ingredients gathered for it were speculative and are left in the pack.
func TestAFountainAnEarlierRunPoisonedGivesTheGaugeAtOnce(t *testing.T) {
	w := newErnestWorld(t, ErnestSpokeOddenstein, nil, nil)
	w.poisoned = true
	w.runsTo("provision:ernest", CustomErnestFishFood, CustomErnestPoison, "combine", CustomErnestCan, CustomErnestSpade, CustomErnestProbe,
		CustomErnestKey, CustomErnestTube, CustomErnestHandIn)
	if w.bites != 0 {
		t.Errorf("%d bites from a poisoned fountain", w.bites)
	}
	if w.inv[ObjPoisonedFishFood] != 1 {
		t.Errorf("the speculative poisoned fish food was used or lost: %v", w.inv)
	}
}

// Ingredients that cannot be had do not hold the can and the tube up, and a poisoned fountain needs none; a live one parks the
// quest with a plain reason.
func TestMissingIngredientsWaitOnlyForTheFountainThatBites(t *testing.T) {
	w := newErnestWorld(t, ErnestSpokeOddenstein, nil, nil)
	w.poisoned = true
	w.empty[CustomErnestFishFood], w.empty[CustomErnestPoison] = true, true
	w.runsTo("provision:ernest", CustomErnestFishFood+"!", CustomErnestPoison+"!", CustomErnestCan, CustomErnestSpade, CustomErnestProbe,
		CustomErnestKey, CustomErnestTube, CustomErnestHandIn)

	w = newErnestWorld(t, ErnestSpokeOddenstein, nil, nil)
	w.empty[CustomErnestFishFood] = true
	why := w.run()
	if !strings.Contains(why, "fountain bites") || !strings.Contains(why, "fish food") {
		t.Fatalf("a live fountain and no fish food: waited %q (log %v)", why, w.log)
	}
	if w.inv[ObjOilCan] != 1 || w.inv[ObjSpade] != 1 || w.bites != 1 {
		t.Errorf("the can, the spade and one bite came first: %v bites %d", w.inv, w.bites)
	}
	// Poison but no fish food: the same wait; fish food but no poison: the same.
	w = newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjPoison: 1}, nil)
	w.empty[CustomErnestFishFood] = true
	if why := w.run(); !strings.Contains(why, "fountain bites") {
		t.Errorf("poison alone: waited %q", why)
	}
	w = newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjFishFood: 1}, nil)
	w.empty[CustomErnestPoison] = true
	if why := w.run(); !strings.Contains(why, "fountain bites") {
		t.Errorf("fish food alone: waited %q", why)
	}
}

func TestALegThatFailedWaitsInsteadOfBeingWalkedAgain(t *testing.T) {
	for _, c := range []struct {
		name string
		inv  map[int]int
		bank map[int]int
	}{
		{CustomErnestCan, nil, nil},
		{CustomErnestKey, map[int]int{ObjOilCan: 1, ObjPressureGauge: 1, ObjSpade: 1}, nil},
		{CustomErnestTube, map[int]int{ObjOilCan: 1, ObjPressureGauge: 1, ObjClosetKey: 1}, nil},
		{CustomErnestSpade, map[int]int{ObjOilCan: 1, ObjPressureGauge: 1}, nil},
	} {
		w := newErnestWorld(t, ErnestSpokeOddenstein, c.inv, c.bank)
		w.empty[c.name] = true
		why := w.run()
		if why == "" {
			t.Errorf("%s failed and the quest went on (log %v)", c.name, w.log)
			continue
		}
		if !strings.Contains(why, c.name) && c.name != CustomErnestSpade {
			t.Errorf("%s failed: the wait says %q", c.name, why)
		}
		if n := strings.Count(strings.Join(w.log, ","), c.name); n != 1 {
			t.Errorf("%s was walked %d times (log %v)", c.name, n, w.log)
		}
	}
	// The probe and the gauge: a probe that cannot resolve the fountain, and a search after the pour that still bites.
	w := newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjOilCan: 1, ObjRubberTube: 1}, nil)
	w.empty[CustomErnestProbe] = true
	if why := w.run(); !strings.Contains(why, CustomErnestProbe) {
		t.Errorf("probe failed: waited %q (log %v)", why, w.log)
	}
	w = newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjOilCan: 1, ObjRubberTube: 1}, nil)
	w.empty[CustomErnestGauge] = true
	if why := w.run(); !strings.Contains(why, CustomErnestGauge) {
		t.Errorf("gauge failed: waited %q (log %v)", why, w.log)
	}
	// A hand-in that failed falls back to talking to the Professor, once.
	w = newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjOilCan: 1, ObjRubberTube: 1, ObjPressureGauge: 1}, nil)
	snap := w.snapshot()
	snap.Failed[CustomErnestHandIn] = true
	if st := DecideErnestChicken(snap); st.Kind != StepTalk || st.Stop.ID != NpcOddenstein {
		t.Errorf("after a failed hand-in: %s", st.Describe())
	}
}

// An account standing in one of the manor's sealed places finishes that leg first, whatever else the pack says.
func TestAnAccountInAPocketFinishesThatLegFirst(t *testing.T) {
	for _, c := range []struct {
		name string
		at   tile.Tile
		want string
	}{
		{"the closet", ErnestClosetInside, CustomErnestTube},
		{"the closet's far end", ErnestTubeSpawn, CustomErnestTube},
		{"the alcove", ErnestAlcoveLever, CustomErnestCan},
		{"the maze's entry", ErnestBasementLanding, CustomErnestCan},
		{"room 9", ErnestOilCanSpawn, CustomErnestCan},
	} {
		for _, inv := range []map[int]int{nil, {ObjOilCan: 1, ObjRubberTube: 1, ObjPressureGauge: 1}, {ObjClosetKey: 1}} {
			w := newErnestWorld(t, ErnestSpokeOddenstein, inv, nil)
			w.tile = c.at
			if st := DecideErnestChicken(w.snapshot()); st.Kind != StepCustom || st.Custom != c.want {
				t.Errorf("%s with %v: %s, want %s", c.name, inv, st.Describe(), c.want)
			}
		}
	}
	// Outside the pockets the same account does the ordinary thing.
	w := newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjOilCan: 1, ObjRubberTube: 1, ObjPressureGauge: 1}, nil)
	w.tile = ErnestClosetOutside
	if st := DecideErnestChicken(w.snapshot()); st.Custom != CustomErnestHandIn {
		t.Errorf("outside the closet: %s", st.Describe())
	}
	// The pockets only matter at stage 2: the same tile at another stage is the run's pass, then a talk.
	w = newErnestWorld(t, ErnestStarted, nil, nil)
	w.tile = ErnestClosetInside
	w.ran["provision:ernest"] = true
	if st := DecideErnestChicken(w.snapshot()); st.Kind != StepTalk || st.Stop.ID != NpcOddenstein {
		t.Errorf("stage 1 in the closet: %s", st.Describe())
	}
}

// A death clears what the engine remembers of the run (Ran, Failed) and the pack: the module starts the stage over, with the
// fountain searched again (the pour it made is in the world and the search hands the gauge over).
func TestADeathStartsTheStageOverAndTheFountainRemembersThePour(t *testing.T) {
	w := newErnestWorld(t, ErnestSpokeOddenstein, nil, nil)
	for i := 0; i < 20 && !strings.Contains(strings.Join(w.log, ","), CustomErnestPour); i++ {
		st := DecideErnestChicken(w.snapshot())
		w.apply(st)
	}
	if !w.poisoned {
		t.Fatalf("the pour never happened (log %v)", w.log)
	}
	w.inv, w.ran, w.failed, w.log = map[int]int{CoinsID: 3}, map[string]bool{}, map[string]bool{}, nil
	w.runsTo("provision:ernest", CustomErnestFishFood, CustomErnestPoison, "combine", CustomErnestCan, CustomErnestSpade, CustomErnestProbe,
		CustomErnestKey, CustomErnestTube, CustomErnestHandIn)
	if w.bites != 1 {
		t.Errorf("%d bites: the poisoned fountain must give the gauge on the probe", w.bites)
	}
}

func TestTheStartingStagesTalkAndWait(t *testing.T) {
	w := newErnestWorld(t, ErnestNotStarted, nil, nil)
	w.ran["provision:ernest"] = true // the run's pass has been made: see TestTheRunsOnePassComesBeforeAnyTalk
	if st := DecideErnestChicken(w.snapshot()); st.Kind != StepTalk || st.Stop.ID != NpcVeronica {
		t.Errorf("not started: %s", st.Describe())
	}
	// Stage 0 in the journal of an in-progress tab is still Veronica, stage 1 the Professor.
	s := w.snapshot()
	s.Journal, s.Stage, s.StageKnown = StatusInProgress, ErnestStarted, true
	if st := DecideErnestChicken(s); st.Kind != StepTalk || st.Stop.ID != NpcOddenstein {
		t.Errorf("stage 1: %s", st.Describe())
	}
	s.Stage = ErnestNotStarted
	if st := DecideErnestChicken(s); st.Kind != StepTalk || st.Stop.ID != NpcVeronica {
		t.Errorf("stage 0 in progress: %s", st.Describe())
	}
	s.StageKnown = false
	if st := DecideErnestChicken(s); st.Kind != StepWait {
		t.Errorf("an unread stage: %s", st.Describe())
	}
	s.StageKnown, s.Stage = true, 9
	if st := DecideErnestChicken(s); st.Kind != StepWait {
		t.Errorf("stage 9: %s", st.Describe())
	}
	s.Journal = StatusUnknown
	if st := DecideErnestChicken(s); st.Kind != StepWait {
		t.Errorf("an uncoloured tab: %s", st.Describe())
	}
	s.Journal = StatusComplete
	if st := DecideErnestChicken(s); st.Kind != StepDone {
		t.Errorf("a complete tab: %s", st.Describe())
	}
}

// The run's one bank-and-mule pass comes first, before Veronica is spoken to (the account is wherever the quest before left it,
// often a bank's neighbourhood; from the Professor's room it would be two walks of three hundred tiles), once per run, and a pass
// that failed is not asked for again.
func TestTheRunsOnePassComesBeforeAnyTalk(t *testing.T) {
	for _, stage := range []int{ErnestNotStarted, ErnestStarted, ErnestSpokeOddenstein} {
		w := newErnestWorld(t, stage, nil, nil)
		st := DecideErnestChicken(w.snapshot())
		if st.Kind != StepProvision || st.KitName != KitErnest || !st.BankSet || len(st.Kit) == 0 {
			t.Errorf("stage %d: %s, want the pass", stage, st.Describe())
		}
		w.ran["provision:ernest"] = true
		if st := DecideErnestChicken(w.snapshot()); st.Kind == StepProvision {
			t.Errorf("stage %d: the pass was asked for twice", stage)
		}
		w = newErnestWorld(t, stage, nil, nil)
		w.failed["provision:ernest"] = true
		if st := DecideErnestChicken(w.snapshot()); st.Kind == StepProvision {
			t.Errorf("stage %d: a failed pass was asked for again", stage)
		}
	}
	// The pass is the nearest bank to where the account stands: Varrock's after Romeo & Juliet, the Draynor bank before a position
	// packet has arrived, never Al Kharid's behind its toll.
	w := newErnestWorld(t, ErnestNotStarted, nil, nil)
	w.tile = VarrockWestBank
	if st := DecideErnestChicken(w.snapshot()); st.Bank != VarrockWestBank {
		t.Errorf("an account in Varrock banks at %v", st.Bank)
	}
	s := w.snapshot()
	s.TileKnown = false
	if st := DecideErnestChicken(s); st.Bank != DraynorBank {
		t.Errorf("an account with no position banks at %v", st.Bank)
	}
	s.Tile, s.TileKnown = tile.Tile{X: 3269, Z: 3170}, true
	if st := DecideErnestChicken(s); st.Bank != DraynorBank {
		t.Errorf("an account at Al Kharid banks at %v", st.Bank)
	}
	// An account that holds all three parts needs no pass at all.
	w = newErnestWorld(t, ErnestSpokeOddenstein, map[int]int{ObjRubberTube: 1, ObjOilCan: 1, ObjPressureGauge: 1}, nil)
	if st := DecideErnestChicken(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomErnestHandIn {
		t.Errorf("all three parts held: %s", st.Describe())
	}
}

// A full pack deposits first, keeping the tools and the food.
func TestAFullPackBanksBeforeTheNextLeg(t *testing.T) {
	inv := map[int]int{CoinsID: 5, 385: 3}
	for id := 1000; id < 1030; id++ {
		inv[id] = 1
	}
	w := newErnestWorld(t, ErnestSpokeOddenstein, inv, nil)
	w.ran["provision:ernest"] = true
	st := DecideErnestChicken(w.snapshot())
	if st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, 385) || !slices.Contains(st.KeepIDs, ObjSpade) || !st.BankSet {
		t.Errorf("a full pack: %s %+v", st.Describe(), st)
	}
}

// ---- every state ----

var ernestCustomSet = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range ErnestCustomNames() {
		m[n] = true
	}
	return m
}()

// Every state of the stage-2 decision yields a well formed step, a registered custom step, and never a step whose own
// precondition the snapshot breaks: the pour needs the poisoned food, the key the spade, the tube the key (or the closet), the
// hand-in all three parts, the combination both ingredients, the gauge a pour.
func TestEveryStateOfTheDecisionYieldsAnExecutableStepThatHasWhatItNeeds(t *testing.T) {
	items := []int{ObjRubberTube, ObjOilCan, ObjPressureGauge, ObjClosetKey, ObjSpade, ObjPoison, ObjFishFood, ObjPoisonedFishFood}
	rans := []string{"provision:" + KitErnest, CustomErnestProbe, CustomErnestPour}
	fails := []string{"provision:" + KitErnest, CustomErnestCan, CustomErnestFishFood, CustomErnestPoison, CustomErnestSpade, CustomErnestProbe,
		CustomErnestPour, CustomErnestGauge, CustomErnestKey, CustomErnestTube, CustomErnestHandIn}
	tiles := []tile.Tile{ErnestBookcaseStand, ErnestClosetInside, ErnestAlcoveLever, ErnestOilCanSpawn, OddensteinAnchor}
	// The failure sets: none, each flag alone, each flag with the kit's, and all of them.
	failSets := []int{0, 1<<len(fails) - 1}
	for i := range fails {
		failSets = append(failSets, 1<<i, 1<<i|1)
	}
	n := 0
	for mask := 0; mask < 1<<len(items); mask++ {
		held := map[int]int{}
		for i, id := range items {
			if mask>>i&1 == 1 {
				held[id] = 1
			}
		}
		for rm := 0; rm < 1<<len(rans); rm++ {
			for _, fm := range failSets {
				for _, free := range []int{0, 20} {
					for _, at := range tiles {
						s := Snapshot{Journal: StatusInProgress, Stage: ErnestSpokeOddenstein, StageKnown: true, InvIDs: held, TileKnown: true, Tile: at,
							FreeSlots: free, Ran: map[string]bool{}, Failed: map[string]bool{}}
						for i, k := range rans {
							s.Ran[k] = rm>>i&1 == 1
						}
						for i, k := range fails {
							s.Failed[k] = fm>>uint(i)&1 == 1
						}
						st := DecideErnestChicken(s)
						n++
						if why := ValidateStep(st); why != "" {
							t.Fatalf("held %v ran %v failed %v at %v: %s: %+v", held, s.Ran, s.Failed, at, why, st)
						}
						if st.Kind == StepDone {
							t.Fatalf("done while the journal is in progress: %v", held)
						}
						if st.Kind == StepTalk && !(st.Stop.ID == NpcOddenstein && held[ObjRubberTube] > 0 && held[ObjOilCan] > 0 && held[ObjPressureGauge] > 0) {
							t.Fatalf("a talk to %s at stage 2 without the three parts: held %v", st.Stop.Npc, held)
						}
						if st.Kind == StepUseOn && !(held[ObjPoison] > 0 && held[ObjFishFood] > 0) {
							t.Fatalf("a combination without both ingredients: held %v", held)
						}
						if st.Kind != StepCustom {
							continue
						}
						if !ernestCustomSet[st.Custom] {
							t.Fatalf("an unregistered custom step %q", st.Custom)
						}
						inCloset, inPocket := InErnestCloset(at), InErnestAlcove(at) || InErnestBasement(at)
						switch st.Custom {
						case CustomErnestPour:
							if held[ObjPoisonedFishFood] == 0 {
								t.Fatalf("a pour without the poisoned fish food: held %v", held)
							}
						case CustomErnestKey:
							if held[ObjSpade] == 0 || held[ObjClosetKey] > 0 {
								t.Fatalf("a dig without a spade, or holding the key already: held %v", held)
							}
						case CustomErnestTube:
							if held[ObjClosetKey] == 0 && !inCloset {
								t.Fatalf("the closet without the key: held %v", held)
							}
						case CustomErnestHandIn:
							if held[ObjRubberTube] == 0 || held[ObjOilCan] == 0 || held[ObjPressureGauge] == 0 {
								t.Fatalf("a hand-in without the three parts: held %v", held)
							}
						case CustomErnestGauge:
							if !s.Ran[CustomErnestProbe] || !s.Ran[CustomErnestPour] {
								t.Fatalf("a second search before the pour: ran %v", s.Ran)
							}
						case CustomErnestProbe:
							if held[ObjPressureGauge] > 0 || s.Ran[CustomErnestProbe] {
								t.Fatalf("a probe with the gauge in hand, or twice: held %v ran %v", held, s.Ran)
							}
						case CustomErnestCan:
							if held[ObjOilCan] > 0 && !inPocket {
								t.Fatalf("the can fetched with the can in hand outside the pockets: held %v", held)
							}
						case CustomErnestFishFood, CustomErnestPoison:
							if held[ObjPressureGauge] > 0 || s.Ran[CustomErnestPour] || held[ObjPoisonedFishFood] > 0 {
								t.Fatalf("an ingredient gathered for a gauge already in hand, a pour already made or a poisoned food already held: held %v", held)
							}
						case CustomErnestSpade:
							if held[ObjRubberTube] > 0 || held[ObjClosetKey] > 0 || held[ObjSpade] > 0 {
								t.Fatalf("a spade fetched for nothing: held %v", held)
							}
						}
					}
				}
			}
		}
	}
	t.Logf("%d states of the stage-2 decision checked", n)
}
