package questplan

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// Druidic Ritual (W22, 18 Oct 2026): the journal parser over the pages the content renders, the decisions over whole simulated
// quests (a world that answers every step the way the content's scripts do), every state of Decide, the kit through the real
// planner, and the prices (the bear, the door's suits, the dungeon).

// druidJournalFixtures are the pages druid_journal.rs2 renders, every string of a branch joined in order (the journal is
// `~quest_journal(title, $text)` over exactly that text, `|` the client's line break).
var druidJournalFixtures = map[int]string{
	DruidNotStarted: "@dbl@I can start this quest by speaking to @dre@Kaqemeex@dbl@ who is at|the @dre@Druids Circle@dbl@ just @dre@North@dbl@ of @dre@Taverley",
	DruidStarted: "@str@I told Kaqemeex I would help them prepare their ceremony.|" +
		"@dbl@I should speak to @dre@Sanfew@dbl@ in the village to the @dre@South",
	DruidSpokenSanfew: "@str@I told Kaqemeex I would help them prepare their ceremony.||" +
		"@dre@Sanfew@dbl@ told me for the ritual they would need me to place|@dre@raw bear meat@dbl@, @dre@raw chicken@dbl@, @dre@raw rat meat@dbl@ and @dre@raw beef@dbl@ in|" +
		"the@dre@ Cauldron of Thunder@dbl@ in the @dre@dungeon South@dbl@ of @dre@Taverley",
	DruidGivenIngredients: "@str@I told Kaqemeex I would help them prepare their ceremony.|" +
		"@str@The ceremony required various meats being placed in the|@str@Cauldron of Thunder. I did this and gave them to Sanfew.|" +
		"@dbl@I should speak to @dre@Kaqemeex@dbl@ again and claim my @dre@reward",
	DruidComplete: "@str@I told Kaqemeex I would help them prepare their ceremony.|@str@The ceremony required various meats being placed in the|" +
		"@str@Cauldron of Thunder. I did this and gave them to Sanfew.|@str@Kaqemeex then taught me the basics of the skill Herblore.||@red@QUEST COMPLETE!",
}

func TestParseDruidicRitualJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, page := range druidJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseDruidicRitualJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d page %q: parsed %d %v", stage, page[:40], got, ok)
			}
		}
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseDruidicRitualJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

var (
	drLumbridge = tile.Tile{X: 3220, Z: 3218}
	drCorridor  = tile.Tile{X: 2886, Z: 9815}
)

// drWorld is Druidic Ritual as the content runs it: %druidquest and the scripts' answers to each step.
type drWorld struct {
	*kcWorld
	stage int
	// hunts says which animals can be found; a hunt of any other fails.
	hunts map[string]bool
	// doorOK is whether the way down and through the Prison door works.
	doorOK bool
	// dipped counts the dips.
	dipped int
}

func newDRWorld(t *testing.T, stage int, at tile.Tile, inv, bank map[int]int) *drWorld {
	w := &drWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, hunts: map[string]bool{"bear": true, "rat": true, "chicken": true, "beef": true}, doorOK: true}
	w.tile = at
	w.maxHP = 50
	return w
}

func (w *drWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case DruidNotStarted:
		j = StatusNotStarted
	case DruidComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage, true
	s.Levels = map[string]int{"attack": 50, "strength": 50, "defence": 50, "hitpoints": w.maxHP, "magic": 30}
	return s
}

func (w *drWorld) held(id int) int { return w.inv[id] }

func (w *drWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepProvision:
		key := "provision:" + st.KitName
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
		} else {
			w.failed[key] = true
		}
		w.log = append(w.log, key)
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepTalk:
		w.tile = st.Stop.Anchor
		switch st.Stop.ID {
		case NpcKaqemeex:
			switch w.stage {
			case DruidNotStarted:
				w.stage = DruidStarted
			case DruidGivenIngredients:
				w.stage = DruidComplete
			}
		case NpcSanfew:
			switch w.stage {
			case DruidStarted:
				w.stage = DruidSpokenSanfew
			case DruidSpokenSanfew:
				all := true
				for _, m := range DruidMeats {
					all = all && w.inv[m.Enchanted] > 0
				}
				if all {
					for _, m := range DruidMeats {
						w.inv[m.Enchanted]--
					}
					w.stage = DruidGivenIngredients
				}
			}
		}
		w.log = append(w.log, "talk "+st.Stop.Npc)
	case StepCustom:
		w.log = append(w.log, st.Custom)
		ok := false
		switch {
		case strings.HasPrefix(st.Custom, "druid-hunt-"):
			m, found := DruidMeatByRaw(st.ItemID)
			if found && st.Custom == CustomDruidHunt(m) && w.hunts[m.Name] {
				w.inv[m.Raw]++
				w.tile = m.Field
				ok = true
			}
		case st.Custom == CustomDruidEnter:
			if w.doorOK {
				w.tile = CauldronStand
				ok = true
			}
		case st.Custom == CustomDruidDip:
			if InCauldronRoom(w.tile) {
				for _, m := range DruidMeats {
					if w.inv[m.Raw] > 0 && w.stage == DruidSpokenSanfew {
						w.inv[m.Raw]--
						w.inv[m.Enchanted]++
						w.dipped++
					}
				}
				ok = true
			}
		default:
			t.Fatalf("a custom step the plan never names: %q", st.Custom)
		}
		if ok {
			delete(w.failed, st.Custom)
		} else {
			w.failed[st.Custom] = true
		}
	default:
		t.Fatalf("a step the quest never asks for: %s", st.Describe())
	}
}

// run plays Decide against the world until Done, a Wait (returned) or n steps.
func (w *drWorld) run(n int) (Step, []string) {
	var seq []string
	for i := 0; i < n; i++ {
		st := DecideDruidicRitual(w.snapshot())
		switch st.Kind {
		case StepDone, StepWait:
			return st, seq
		}
		seq = append(seq, w.describe(st))
		w.apply(st)
	}
	return Step{}, seq
}

func (w *drWorld) describe(st Step) string {
	switch st.Kind {
	case StepTalk:
		return "talk " + st.Stop.Npc
	case StepProvision:
		return "provision " + st.KitName
	case StepDeposit:
		return "deposit"
	}
	return st.Custom
}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

var drFood = map[int]int{385: 5}

// A fresh account in Falador (nearer Wydin's than the fields) with food and coins, a bank holding nothing and a shop that has the raw
// beef and the raw chicken: the shelf covers two meats, the hunts the other two, and the whole quest is done in the content's order.
// (Without coins the shelf is out of reach and all four are hunted.)
func TestDruidicRitualRunsToCompletionFromTheStart(t *testing.T) {
	w := newDRWorld(t, DruidNotStarted, FaladorWestBank, map[int]int{385: 5, CoinsID: 300}, nil)
	w.shop[ObjRawBeef], w.shop[ObjRawChicken] = true, true
	st, seq := w.run(40)
	if st.Kind != StepDone {
		t.Fatalf("the run ended on %v (%s) after %v", st.Kind, st.Reason, seq)
	}
	want := []string{"talk Kaqemeex", "talk Sanfew", "provision druid-meats", "druid-hunt-bear", "druid-hunt-rat", "druid-enter", "druid-dip", "talk Sanfew", "talk Kaqemeex"}
	if !slices.Equal(seq, want) {
		t.Errorf("steps\n  %s\nwant\n  %s", strings.Join(seq, "\n  "), strings.Join(want, "\n  "))
	}
	if w.dipped != 4 {
		t.Errorf("%d meats dipped, want four", w.dipped)
	}
	// Nothing of the quest is left in the pack: the four enchanted meats went to Sanfew.
	for _, m := range DruidMeats {
		if w.held(m.Raw) != 0 || w.held(m.Enchanted) != 0 {
			t.Errorf("the %s meat is still held (raw %d, enchanted %d)", m.Name, w.held(m.Raw), w.held(m.Enchanted))
		}
	}
}

// With no shop to buy from (an account in the Lumbridge region lists none; and an empty shelf fails) every meat is hunted, in the
// route's order.
func TestDruidicRitualHuntsAllFourWhenNothingElseGivesThem(t *testing.T) {
	w := newDRWorld(t, DruidSpokenSanfew, drLumbridge, drFood, nil)
	st, seq := w.run(40)
	if st.Kind != StepDone && !(st.Kind == StepWait) {
		// The world stops at Sanfew's hand-in being the last step of the stage; the stage then reads 3 and Kaqemeex finishes.
		t.Fatalf("the run ended on %v after %v", st.Kind, seq)
	}
	want := []string{"provision druid-meats", "druid-hunt-bear", "druid-hunt-rat", "druid-hunt-chicken", "druid-hunt-beef", "druid-enter", "druid-dip", "talk Sanfew", "talk Kaqemeex"}
	if !slices.Equal(seq, want) {
		t.Errorf("steps\n  %s\nwant\n  %s", strings.Join(seq, "\n  "), strings.Join(want, "\n  "))
	}
}

// A half-finished run: the Stage 5 trainer banked the two enchanted meats and a raw one between two runs. One bundled pass draws
// them back and the quest hunts only what is still missing, then dips the one raw meat.
func TestDruidicRitualDrawsBackWhatAHalfFinishedRunBanked(t *testing.T) {
	bank := map[int]int{ObjEnchantedBearMeat: 1, ObjEnchantedRatMeat: 1, ObjRawChicken: 1}
	w := newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, drFood, bank)
	w.bankKnown = false
	st, seq := w.run(40)
	if st.Kind != StepDone && st.Kind != StepWait {
		t.Fatalf("the run ended on %v after %v", st.Kind, seq)
	}
	want := []string{"provision druid-meats", "druid-hunt-beef", "druid-enter", "druid-dip", "talk Sanfew", "talk Kaqemeex"}
	if !slices.Equal(seq, want) {
		t.Errorf("steps\n  %s\nwant\n  %s", strings.Join(seq, "\n  "), strings.Join(want, "\n  "))
	}
	if w.dipped != 2 {
		t.Errorf("%d meats dipped: the chicken and the beef were the raw ones", w.dipped)
	}
}

// Every meat enchanted already (a pack the Stage 5 trainer did not bank): straight to Sanfew, from the cauldron's room too.
func TestDruidicRitualHandsInAtOnceWithAllFourEnchanted(t *testing.T) {
	inv := map[int]int{}
	for _, m := range DruidMeats {
		inv[m.Enchanted] = 1
	}
	for _, at := range []tile.Tile{FaladorWestBank, CauldronStand, drCorridor} {
		s := newDRWorld(t, DruidSpokenSanfew, at, inv, nil).snapshot()
		if st := DecideDruidicRitual(s); st.Kind != StepTalk || st.Stop.ID != NpcSanfew {
			t.Errorf("at %v with four enchanted meats: %v", at, st.Describe())
		}
	}
	// Three are not enough.
	delete(inv, ObjEnchantedBeef)
	inv[ObjRawBeef] = 1
	s := newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, inv, nil).snapshot()
	if st := DecideDruidicRitual(s); st.Kind == StepTalk {
		t.Errorf("three enchanted meats and a raw one go to Sanfew: %v", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The early stages
// ---------------------------------------------------------------------------------------------------------------------

func TestDruidicRitualEarlyStagesAndTheTab(t *testing.T) {
	w := newDRWorld(t, DruidNotStarted, FaladorWestBank, nil, nil)
	for _, c := range []struct {
		name  string
		snap  func() Snapshot
		kind  StepKind
		npc   int
		wait  string
		extra func(Step) bool
	}{
		{"complete tab", func() Snapshot { s := w.snapshot(); s.Journal = StatusComplete; return s }, StepDone, 0, "", nil},
		{"tab not coloured", func() Snapshot { s := w.snapshot(); s.Journal = StatusUnknown; return s }, StepWait, 0, "coloured", nil},
		{"not started", func() Snapshot { return w.snapshot() }, StepTalk, NpcKaqemeex, "", func(st Step) bool {
			return slices.Equal(st.Stop.Prefer, []string{"I'm in search of a quest.", "Ok, I will try and help."})
		}},
		{"in progress, stage unread", func() Snapshot { s := w.snapshot(); s.Journal, s.StageKnown = StatusInProgress, false; return s }, StepWait, 0, "stage has not been read", nil},
		{"started", func() Snapshot { s := w.snapshot(); s.Journal, s.Stage = StatusInProgress, DruidStarted; return s }, StepTalk, NpcSanfew, "", func(st Step) bool {
			return slices.Equal(st.Stop.Prefer, []string{"I've been sent to help purify the Varrock stone circle.", "Ok, I'll do that then."})
		}},
		{"ingredients given", func() Snapshot {
			s := w.snapshot()
			s.Journal, s.Stage = StatusInProgress, DruidGivenIngredients
			return s
		}, StepTalk, NpcKaqemeex, "", nil},
		{"stage nobody knows", func() Snapshot { s := w.snapshot(); s.Journal, s.Stage = StatusInProgress, 9; return s }, StepWait, 0, "unrecognised", nil},
	} {
		st := DecideDruidicRitual(c.snap())
		if st.Kind != c.kind || (c.npc != 0 && st.Stop.ID != c.npc) || (c.wait != "" && !strings.Contains(st.Reason, c.wait)) || (c.extra != nil && !c.extra(st)) {
			t.Errorf("%s: %s (%+v)", c.name, st.Describe(), st)
		}
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: %s", c.name, why)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The kit
// ---------------------------------------------------------------------------------------------------------------------

func kitStep(t *testing.T, s Snapshot) Step {
	t.Helper()
	st := DecideDruidicRitual(s)
	if st.Kind != StepProvision || st.KitName != KitDruidMeats {
		t.Fatalf("want the meats kit, got %s", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatalf("the kit is malformed: %s", why)
	}
	return st
}

func lineOf(kit []KitLine, id int) (KitLine, bool) {
	for _, l := range kit {
		if l.ID == id {
			return l, true
		}
	}
	return KitLine{}, false
}

// The kit names, for each meat missing, its enchanted form (untradeable, optional) and its raw one (wanted while the enchanted is
// not held), the shop only for the raw beef and the raw chicken and only for an account nearer Port Sarim than the fields; and
// sharks. Every line is optional: the hunts make what the bank, the mule and the shelf cannot.
func TestDruidicRitualTheKit(t *testing.T) {
	falador := kitStep(t, newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, nil, nil).snapshot())
	lumbridge := kitStep(t, newDRWorld(t, DruidSpokenSanfew, drLumbridge, nil, nil).snapshot())
	if falador.Bank != DraynorBank && falador.Bank != NearestBank(FaladorWestBank).Tile {
		t.Errorf("the kit's bank is %v", falador.Bank)
	}
	for _, m := range DruidMeats {
		for where, st := range map[string]Step{"Falador": falador, "Lumbridge": lumbridge} {
			ench, ok := lineOf(st.Kit, m.Enchanted)
			if !ok || !ench.Untradeable || !ench.Optional || ench.Qty != 1 || ench.Name != m.EnchantedName {
				t.Errorf("%s: the enchanted %s line is %+v", where, m.Name, ench)
			}
			raw, ok := lineOf(st.Kit, m.Raw)
			if !ok || !raw.Optional || raw.Untradeable || raw.OnlyIfShort != m.Enchanted || raw.Qty != 1 || raw.Name != m.RawName {
				t.Errorf("%s: the raw %s line is %+v", where, m.Name, raw)
			}
			shop := len(raw.Sources) == 1 && raw.Sources[0].Kind == SourceShop && raw.Sources[0].Shop == WydinShop
			if want := m.Wydin && where == "Falador"; shop != want {
				t.Errorf("%s: the raw %s line has the shop %v, want %v (%+v)", where, m.Name, shop, want, raw.Sources)
			}
			if !m.Wydin && len(raw.Sources) != 0 {
				t.Errorf("%s: the raw %s line has sources %+v: the world holds it only in its animals", where, m.Name, raw.Sources)
			}
		}
	}
	// Food: eight sharks (the corridor's skeletons both ways and the door's suits are priced on them), optional (one kind only, so a
	// stocked bank does not fill the pack with every food it holds).
	for _, st := range []Step{falador, lumbridge} {
		shark, ok := lineOf(st.Kit, ObjShark)
		if !ok || !shark.Optional || shark.Qty != DruidSharks || DruidSharks != 8 || shark.OnlyIfShort != 0 {
			t.Errorf("the shark line is %+v", shark)
		}
		for _, id := range []int{373, 379} {
			if _, ok := lineOf(st.Kit, id); ok {
				t.Errorf("food line %d: a second kind of food", id)
			}
		}
	}
	// Only the missing meats are asked for: a bear meat in the pack leaves it out.
	s := newDRWorld(t, DruidSpokenSanfew, drLumbridge, map[int]int{ObjRawBearMeat: 1}, nil).snapshot()
	st := kitStep(t, s)
	if _, ok := lineOf(st.Kit, ObjRawBearMeat); ok {
		t.Error("the kit asks for a bear meat the pack holds")
	}
	if len(st.Kit) != 3*2+1 {
		t.Errorf("%d kit lines for three missing meats, want %d", len(st.Kit), 3*2+1)
	}
	// Its keep list protects every form of every meat, the coins and the food.
	for _, m := range DruidMeats {
		for _, id := range []int{m.Raw, m.Enchanted} {
			if !slices.Contains(st.KeepIDs, id) {
				t.Errorf("the keep list lacks %d", id)
			}
		}
	}
	for _, id := range append([]int{CoinsID}, DruidFoodIDs...) {
		if !slices.Contains(st.KeepIDs, id) {
			t.Errorf("the keep list lacks %d", id)
		}
	}
}

// The kit through the real planner: the bank's enchanted meat is drawn (and the raw one for the same meat left); the mule is asked
// for nothing it does not publish; Wydin's shelf is a shop task only for the beef and the chicken, in the item rule's order after
// the bank.
func TestDruidicRitualTheKitThroughThePlanner(t *testing.T) {
	st := kitStep(t, newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, nil, nil).snapshot())
	state := ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: map[int]int{ObjEnchantedBearMeat: 1, ObjRawRatMeat: 1, 385: 9},
		BankKnown: true, FreeSlots: 28, Levels: map[string]int{}, Exhausted: map[string]bool{}}
	plan := PlanProvision(st.Kit, state)
	if plan.Phase != PhaseBank {
		t.Fatalf("with a stocked bank the phase is %s: %s", plan.Phase, plan.Summary())
	}
	got := map[int]int{}
	for _, c := range plan.Withdraw {
		got[c.ID] += c.Count
	}
	if got[ObjEnchantedBearMeat] != 1 || got[ObjRawRatMeat] != 1 || got[385] != DruidSharks || got[ObjRawBearMeat] != 0 {
		t.Errorf("the bank gives %v, want the enchanted bear, the raw rat and %d sharks", got, DruidSharks)
	}
	// An empty bank and no mule: the shelf for the beef and the chicken, nothing for the rat and the bear (the lines are optional).
	state.Bank = map[int]int{}
	plan = PlanProvision(st.Kit, state)
	var shopped []int
	for _, task := range plan.Tasks {
		if task.Source.Kind != SourceShop {
			t.Errorf("a %s task for %s", task.Source.Kind, task.Line.Name)
		}
		shopped = append(shopped, task.Line.ID)
	}
	slices.Sort(shopped)
	if want := []int{ObjRawBeef, ObjRawChicken}; !slices.Equal(shopped, want) {
		t.Errorf("shop tasks for %v, want %v: %s", shopped, want, plan.Summary())
	}
	// With no shelf on the way, the kit is satisfied by nothing and still not blocked: the hunts make the meats.
	st = kitStep(t, newDRWorld(t, DruidSpokenSanfew, drLumbridge, nil, nil).snapshot())
	plan = PlanProvision(st.Kit, state)
	if plan.Phase != PhaseSatisfied || len(plan.Blocked) != 0 {
		t.Errorf("an empty bank and no shop: %s (blocked %v)", plan.Phase, plan.Blocked)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The hunts and the prices
// ---------------------------------------------------------------------------------------------------------------------

func TestDruidicRitualHuntsInTheRoutesOrderAndSkipsWhatItHolds(t *testing.T) {
	ran := map[string]bool{"provision:" + KitDruidMeats: true}
	for _, c := range []struct {
		inv  map[int]int
		want string
	}{
		{map[int]int{385: 5}, "druid-hunt-bear"},
		{map[int]int{385: 5, ObjRawBearMeat: 1}, "druid-hunt-rat"},
		{map[int]int{385: 5, ObjEnchantedBearMeat: 1, ObjRawRatMeat: 1}, "druid-hunt-chicken"},
		{map[int]int{385: 5, ObjRawBearMeat: 1, ObjEnchantedRatMeat: 1, ObjRawChicken: 1}, "druid-hunt-beef"},
		{map[int]int{385: 5, ObjRawBearMeat: 1, ObjRawRatMeat: 1, ObjEnchantedChicken: 1}, "druid-hunt-beef"},
		{map[int]int{385: 5, ObjRawRatMeat: 1, ObjRawChicken: 1, ObjRawBeef: 1}, "druid-hunt-bear"},
	} {
		w := newDRWorld(t, DruidSpokenSanfew, drLumbridge, c.inv, nil)
		w.ran = ran
		st := DecideDruidicRitual(w.snapshot())
		if st.Kind != StepCustom || st.Custom != c.want {
			t.Errorf("pack %v: %s, want %s", c.inv, st.Describe(), c.want)
			continue
		}
		if m, ok := DruidMeatByRaw(st.ItemID); !ok || CustomDruidHunt(m) != st.Custom {
			t.Errorf("pack %v: the hunt's item id %d is not its meat's", c.inv, st.ItemID)
		}
	}
	// A hunt that failed parks the quest for half an hour with the reason, not a retry loop.
	w := newDRWorld(t, DruidSpokenSanfew, drLumbridge, map[int]int{385: 5, ObjRawBearMeat: 1}, nil)
	w.ran = ran
	w.failed["druid-hunt-rat"] = true
	st := DecideDruidicRitual(w.snapshot())
	if st.Kind != StepWait || st.ParkFor != DruidHuntPark || !strings.Contains(st.Reason, "rat") {
		t.Errorf("a failed rat hunt: %s (park %v)", st.Describe(), st.ParkFor)
	}
	// A failed kit does not stop the hunts: the shelf and the bank are the first rungs, not the only ones.
	w = newDRWorld(t, DruidSpokenSanfew, drLumbridge, map[int]int{385: 5}, nil)
	w.failed["provision:"+KitDruidMeats] = true
	if st := DecideDruidicRitual(w.snapshot()); st.Custom != "druid-hunt-bear" {
		t.Errorf("after a failed kit: %s", st.Describe())
	}
	// The pack must have room for the meats it is still to carry.
	w = newDRWorld(t, DruidSpokenSanfew, drLumbridge, map[int]int{385: 5}, nil)
	w.ran = ran
	s := w.snapshot()
	s.FreeSlots = 3
	if st := DecideDruidicRitual(s); st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, ObjRawBearMeat) || !slices.Contains(st.KeepIDs, ObjEnchantedBeef) {
		t.Errorf("a nearly full pack: %s keeping %v", st.Describe(), st.KeepIDs)
	}
}

// The bear is priced on what the account fights it with and the food it carries: a healthy account with sharks goes, an account
// without food does not (the model keeps a bite for the way out), nor does one that would lose; both park for hours with the reason.
func TestDruidicRitualPricesTheBear(t *testing.T) {
	ran := map[string]bool{"provision:" + KitDruidMeats: true}
	mk := func(inv map[int]int, attack, str, def, hp int) Snapshot {
		w := newDRWorld(t, DruidSpokenSanfew, drLumbridge, inv, nil)
		w.ran, w.maxHP = ran, hp
		s := w.snapshot()
		s.Levels = map[string]int{"attack": attack, "strength": str, "defence": def, "hitpoints": hp, "magic": 1}
		return s
	}
	if st := DecideDruidicRitual(mk(drFood, 50, 50, 50, 50)); st.Custom != "druid-hunt-bear" {
		t.Errorf("a 50/50/50 account with five sharks: %s", st.Describe())
	}
	if st := DecideDruidicRitual(mk(drFood, 30, 30, 30, 30)); st.Custom != "druid-hunt-bear" {
		t.Errorf("a 30/30/30 account with five sharks: %s", st.Describe())
	}
	for name, s := range map[string]Snapshot{
		"no food":                          mk(nil, 50, 50, 50, 50),
		"1/1/1, hp 31":                     mk(drFood, 1, 1, 1, 31),
		"10/10/10, hp 30 and three sharks": mk(map[int]int{385: 3}, 10, 10, 10, 30),
	} {
		st := DecideDruidicRitual(s)
		if st.Kind != StepWait || st.ParkFor != DruidOddsPark || !strings.Contains(st.Reason, "bear") {
			t.Errorf("%s: %s (park %v)", name, st.Describe(), st.ParkFor)
		}
	}
	// A bear meat from the bank is never priced: the fight is not going to happen.
	s := mk(nil, 1, 1, 1, 31)
	s.InvIDs = map[int]int{ObjRawBearMeat: 1}
	if st := DecideDruidicRitual(s); st.Kind != StepCustom || st.Custom != "druid-hunt-rat" {
		t.Errorf("a weak account holding the bear meat: %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The cauldron
// ---------------------------------------------------------------------------------------------------------------------

func allRaw() map[int]int {
	inv := map[int]int{385: 5}
	for _, m := range DruidMeats {
		inv[m.Raw] = 1
	}
	return inv
}

func TestDruidicRitualTheCauldronAndItsDoor(t *testing.T) {
	// Every meat held, raw: down the dungeon from anywhere outside it, with the door and its suits the executor's.
	for _, at := range []tile.Tile{FaladorWestBank, drLumbridge, SanfewAnchor} {
		st := DecideDruidicRitual(newDRWorld(t, DruidSpokenSanfew, at, allRaw(), nil).snapshot())
		if st.Kind != StepCustom || st.Custom != CustomDruidEnter {
			t.Errorf("at %v: %s", at, st.Describe())
		}
	}
	// In the corridor the odds are asked again, on what is left of the trip (the skeletons not yet passed, the suits and the way out): a
	// healthy account goes on to the door and a weak one is not sent on, whatever it set out with.
	if st := DecideDruidicRitual(newDRWorld(t, DruidSpokenSanfew, drCorridor, allRaw(), nil).snapshot()); st.Custom != CustomDruidEnter {
		t.Errorf("a 50/50/50 account in the corridor: %s", st.Describe())
	}
	weak := newDRWorld(t, DruidSpokenSanfew, drCorridor, allRaw(), nil)
	weak.maxHP = 12
	ws := weak.snapshot()
	ws.Levels = map[string]int{"attack": 1, "strength": 1, "defence": 1, "hitpoints": 12}
	if st := DecideDruidicRitual(ws); st.Kind != StepWait || st.ParkFor != DruidOddsPark || !strings.Contains(st.Reason, "Taverley dungeon") {
		t.Errorf("a 1/1/1 account in the corridor: %s (park %v)", st.Describe(), st.ParkFor)
	}
	// In the room: dip.
	if st := DecideDruidicRitual(newDRWorld(t, DruidSpokenSanfew, CauldronStand, allRaw(), nil).snapshot()); st.Custom != CustomDruidDip {
		t.Errorf("in the room: %s", st.Describe())
	}
	// A weak account, or one with no food, is not sent down: it parks for hours with the model's reason.
	for name, s := range map[string]Snapshot{
		"no food": newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, func() map[int]int { m := allRaw(); delete(m, 385); return m }(), nil).snapshot(),
		"weak": func() Snapshot {
			w := newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, allRaw(), nil)
			w.maxHP = 31
			s := w.snapshot()
			s.Levels = map[string]int{"attack": 1, "strength": 1, "defence": 1, "hitpoints": 31}
			return s
		}(),
	} {
		st := DecideDruidicRitual(s)
		if st.Kind != StepWait || st.ParkFor != DruidOddsPark || !strings.Contains(st.Reason, "Taverley dungeon") {
			t.Errorf("%s: %s (park %v)", name, st.Describe(), st.ParkFor)
		}
	}
	// A way down that did not open, and a dip that did not take, park for half an hour and are not repeated at once.
	for name, c := range map[string]struct {
		at     tile.Tile
		failed string
	}{"door": {drCorridor, CustomDruidEnter}, "dip": {CauldronStand, CustomDruidDip}} {
		w := newDRWorld(t, DruidSpokenSanfew, c.at, allRaw(), nil)
		w.failed[c.failed] = true
		st := DecideDruidicRitual(w.snapshot())
		if st.Kind != StepWait || st.ParkFor != DruidDungeonPark {
			t.Errorf("%s failed: %s (park %v)", name, st.Describe(), st.ParkFor)
		}
	}
	// The door is priced as the chain it is (DruidDungeonFoes): the corridor's skeletons, the suits, the skeletons again on the way
	// out. An account of 40/40/40 with 30 hitpoints passes on eight sharks and not on five, the 30/30/30 account with five sharks the
	// first version of this price let through does not pass on eight either, one of 20/20/20 with hitpoints 25 does not, and neither is
	// judged without its hitpoints.
	mk := func(level, hp, sharks int) Snapshot {
		s := newDRWorld(t, DruidSpokenSanfew, FaladorWestBank, map[int]int{385: sharks}, nil).snapshot()
		s.Levels = map[string]int{"attack": level, "strength": level, "defence": level, "hitpoints": hp}
		s.HP, s.MaxHP = hp, hp
		return s
	}
	if ok, why := DruidDungeonOdds(mk(40, 30, DruidSharks)); !ok {
		t.Errorf("a 40/40/40 account with %d sharks: %s", DruidSharks, why)
	}
	if ok, _ := DruidDungeonOdds(mk(40, 30, 5)); ok {
		t.Error("a 40/40/40 account with five sharks passes the dungeon: the gauntlet is five skeletons each way")
	}
	for _, sharks := range []int{5, DruidSharks} {
		if ok, _ := DruidDungeonOdds(mk(30, 30, sharks)); ok {
			t.Errorf("a 30/30/30 account with %d sharks passes the dungeon: it ends the three fights the first price counted at 17 hitpoints and has none for the other nine", sharks)
		}
	}
	if ok, _ := DruidDungeonOdds(mk(20, 25, DruidSharks)); ok {
		t.Error("a 20/20/20 account with hitpoints 25 passes the dungeon")
	}
	unread := mk(20, 25, DruidSharks)
	unread.MaxHP = 0
	if ok, _ := DruidDungeonOdds(unread); !ok {
		t.Error("an account whose hitpoints are unread is judged")
	}
}

// The trip is priced as what the planned line meets (DruidCorridorSkeletons: the spawns it passes within two tiles of, on the real pack
// in TestDruidDungeonRouteMeetsTheSkeletons), both ways, with the door's two suits between; from inside the corridor only the
// skeletons not yet passed count on the way in; and the food line is the one the price needs.
func TestDruidDungeonIsPricedOnWhatTheRouteMeets(t *testing.T) {
	n := len(DruidCorridorSkeletons)
	if n != 5 {
		t.Fatalf("%d corridor skeletons: the plan's comments, the food line and the route test say five", n)
	}
	count := func(foes []MeleeFoe, of MeleeFoe) int {
		c := 0
		for _, f := range foes {
			if f == of {
				c++
			}
		}
		return c
	}
	for name, c := range map[string]struct {
		at        tile.Tile
		known     bool
		ahead     int
		wantTotal int
	}{
		"the bank":                   {FaladorWestBank, true, n, 2*n + 2},
		"an unknown tile":            {tile.Tile{}, false, n, 2*n + 2},
		"the ladder's foot":          {DruidDungeonFoot, true, n, 2*n + 2},
		"the corridor, two passed":   {tile.Tile{X: 2885, Z: 9820}, true, n - 2, 2*n + 2 - 2},
		"the door":                   {CauldronDoorOutside, true, 0, n + 2},
		"the corridor, none passed":  {tile.Tile{X: 2885, Z: 9800}, true, n, 2*n + 2},
		"a tile of another level":    {tile.Tile{X: 2885, Z: 9820, Level: 1}, true, n, 2*n + 2},
		"a tile outside the dungeon": {tile.Tile{X: 2885, Z: 3400}, true, n, 2*n + 2},
	} {
		foes := DruidDungeonFoes(c.at, c.known)
		if len(foes) != c.wantTotal || count(foes, FoeManorSkeleton) != c.ahead+n || count(foes, FoeSuitOfArmour) != 2 {
			t.Errorf("%s: %d fights, %d skeletons and %d suits; want %d, %d and 2", name, len(foes), count(foes, FoeManorSkeleton), count(foes, FoeSuitOfArmour), c.wantTotal, c.ahead+n)
			continue
		}
		// The suits come between the skeletons ahead and the skeletons on the way out.
		for i, f := range foes {
			if isSuit := f == FoeSuitOfArmour; isSuit != (i >= c.ahead && i < c.ahead+2) {
				t.Errorf("%s: fight %d is %s", name, i, f.Name)
			}
		}
	}
	// A skeleton two tiles behind the account still counts (it is within the reach), one three behind does not.
	first := DruidCorridorSkeletons[0].Z
	if got := count(DruidDungeonFoes(tile.Tile{X: 2885, Z: first + DruidCorridorReach}, true), FoeManorSkeleton); got != n+n {
		t.Errorf("two tiles past the first skeleton: %d skeletons, want %d", got, n+n)
	}
	if got := count(DruidDungeonFoes(tile.Tile{X: 2885, Z: first + DruidCorridorReach + 1}, true), FoeManorSkeleton); got != n-1+n {
		t.Errorf("three tiles past the first skeleton: %d skeletons, want %d", got, n-1+n)
	}
	// The skeleton is the unarmed one the closet of the manor holds (npc 90), whose stats TestQuestFoesMatchTheContent pins.
	if f, ok := FoeByNPC(NpcManorSkeleton); !ok || f != FoeManorSkeleton {
		t.Errorf("npc %d is %+v", NpcManorSkeleton, f)
	}
	// An account inside the corridor is priced on what is left: the pack that falls short of the whole trip from the bank carries the
	// half of it that is left at the door.
	atDoor := newDRWorld(t, DruidSpokenSanfew, CauldronDoorOutside, map[int]int{385: DruidSharks}, nil).snapshot()
	atDoor.Levels = map[string]int{"attack": 30, "strength": 30, "defence": 30, "hitpoints": 30}
	atDoor.HP, atDoor.MaxHP = 30, 30
	if ok, why := DruidDungeonOdds(atDoor); !ok {
		t.Errorf("a 30/30/30 account with %d sharks at the door: %s", DruidSharks, why)
	}
	fromBank := atDoor
	fromBank.Tile = FaladorWestBank
	if ok, _ := DruidDungeonOdds(fromBank); ok {
		t.Error("the same account passes from the bank: the whole trip is more than the door's half")
	}
}

// The slot is held through the hunts and given back when every meat is in the pack: the cauldron and the dungeon are the
// account's own.
func TestDruidPastTheHunt(t *testing.T) {
	mk := func(j Status, stage int, inv map[int]int) Snapshot {
		return Snapshot{Journal: j, Stage: stage, StageKnown: true, InvIDs: inv}
	}
	for name, c := range map[string]struct {
		s    Snapshot
		want bool
	}{
		"complete":                {mk(StatusComplete, 0, nil), true},
		"not started":             {mk(StatusNotStarted, 0, nil), false},
		"stage 1":                 {mk(StatusInProgress, DruidStarted, nil), false},
		"stage 2, nothing":        {mk(StatusInProgress, DruidSpokenSanfew, nil), false},
		"stage 2, three held":     {mk(StatusInProgress, DruidSpokenSanfew, map[int]int{ObjRawBearMeat: 1, ObjRawRatMeat: 1, ObjEnchantedChicken: 1}), false},
		"stage 2, four in a form": {mk(StatusInProgress, DruidSpokenSanfew, map[int]int{ObjRawBearMeat: 1, ObjRawRatMeat: 1, ObjEnchantedChicken: 1, ObjRawBeef: 1}), true},
		"stage 3":                 {mk(StatusInProgress, DruidGivenIngredients, nil), true},
		"stage unread":            {Snapshot{Journal: StatusInProgress}, false},
	} {
		if got := DruidPastTheHunt(c.s); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Every state
// ---------------------------------------------------------------------------------------------------------------------

// Every step Decide returns, over every stage, tab and pack shape the quest can meet, is well formed; a custom step is one the
// executors register; a wait that parks says why; and nothing panics on an empty snapshot.
func TestDruidicRitualEveryStateGivesAWellFormedStep(t *testing.T) {
	known := map[string]bool{CustomDruidEnter: true, CustomDruidDip: true}
	for _, m := range DruidMeats {
		known[CustomDruidHunt(m)] = true
	}
	positions := []tile.Tile{FaladorWestBank, drLumbridge, SanfewAnchor, KaqemeexAnchor, drCorridor, CauldronStand, DruidDungeonFoot}
	packs := []map[int]int{nil, drFood, allRaw(), {ObjEnchantedBearMeat: 1, ObjEnchantedRatMeat: 1}, {ObjRawChicken: 1, ObjEnchantedBeef: 1, 385: 2},
		{ObjEnchantedBearMeat: 1, ObjEnchantedRatMeat: 1, ObjEnchantedChicken: 1, ObjEnchantedBeef: 1}}
	n := 0
	for _, j := range []Status{StatusUnknown, StatusNotStarted, StatusInProgress, StatusComplete} {
		for stage := -1; stage <= 5; stage++ {
			for _, at := range positions {
				for _, inv := range packs {
					for _, mark := range []string{"", "ran", "failed-kit", "failed-enter", "failed-dip", "failed-hunt"} {
						for _, free := range []int{28, 4, 0} {
							w := newDRWorld(t, stage, at, inv, nil)
							s := w.snapshot()
							s.Journal, s.FreeSlots = j, free
							switch mark {
							case "ran":
								s.Ran["provision:"+KitDruidMeats] = true
							case "failed-kit":
								s.Failed["provision:"+KitDruidMeats] = true
							case "failed-enter":
								s.Failed[CustomDruidEnter] = true
							case "failed-dip":
								s.Failed[CustomDruidDip] = true
							case "failed-hunt":
								for _, m := range DruidMeats {
									s.Failed[CustomDruidHunt(m)] = true
								}
							}
							st := DecideDruidicRitual(s)
							n++
							if why := ValidateStep(st); why != "" {
								t.Fatalf("journal %s stage %d at %v pack %v mark %q free %d: %s (%s)", j, stage, at, inv, mark, free, why, st.Describe())
							}
							if st.Kind == StepCustom && !known[st.Custom] {
								t.Fatalf("an unregistered custom step %q", st.Custom)
							}
							if st.Kind == StepWait && st.ParkFor < 0 {
								t.Fatalf("a negative park: %+v", st)
							}
						}
					}
				}
			}
		}
	}
	if st := DecideDruidicRitual(Snapshot{}); st.Kind != StepWait {
		t.Errorf("an empty snapshot: %s", st.Describe())
	}
	t.Logf("%d states", n)
}

// The module's shape: the record, the sustain, the caps, the tools and the stage reader.
func TestDruidicRitualModule(t *testing.T) {
	m := DruidicRitual
	r := m.Record
	if r.ID != "druid" || r.Name != "Druidic Ritual" || r.QuestPoints != 4 || QuestTabName("druid") != "Druidic Ritual" {
		t.Errorf("record %+v", r)
	}
	if len(r.Requirements.Skills) != 1 || r.Requirements.Skills[0].Skill != "hitpoints" || r.Requirements.Skills[0].Level != DruidMinHitpoints || len(r.Requirements.Quests) != 0 {
		t.Errorf("requirements %+v", r.Requirements)
	}
	if m.MaxDeaths != 1 || m.NoRetaliate || m.Decide == nil || m.ReadStage == nil || m.ExclusiveDone == nil || m.Sustain.EatAt <= 0 || len(m.Sustain.FoodIDs) == 0 {
		t.Errorf("module %+v", m)
	}
	for _, id := range append(druidMeatIDsForTest(), CoinsID) {
		if !slices.Contains(m.ToolIDs, id) {
			t.Errorf("the tools lack %d", id)
		}
	}
	if m.Bank != FaladorWestBank || !m.BankSet || len(m.Hops) != 2 {
		t.Errorf("bank %v hops %v", m.Bank, m.Hops)
	}
	// The sustain names only foods the fight model knows.
	for _, id := range m.Sustain.FoodIDs {
		if FoodBiteHeal(id) == 0 {
			t.Errorf("food %d is not in the fight model's table", id)
		}
	}
	if d := DruidDungeonPark; d != 30*time.Minute || DruidOddsPark < time.Hour {
		t.Errorf("parks %v %v", d, DruidOddsPark)
	}
}

func druidMeatIDsForTest() []int {
	var out []int
	for _, m := range DruidMeats {
		out = append(out, m.Raw, m.Enchanted)
	}
	return out
}
