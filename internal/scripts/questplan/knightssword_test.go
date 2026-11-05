package questplan

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Knight's Sword (W22, 20 Oct 2026): the journal parser over the pages the content renders, every state of Decide, the pie's kits through
// the real planner, the dungeon's price, and whole simulated quests (a world that answers every step the way the content's scripts do).

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

// sqJournalPages are the pages squire_journal.rs2 renders, every string of a branch joined in order (the journal is
// `~quest_journal("The Knight's Sword", $text)` over exactly that text, `|` the client's line break).
const (
	sqHead     = "@str@I told the Squire I would help him to replace the sword he had|@str@lost. It could only be made by an Imcando dwarf.|"
	sqThurgoS  = "@str@I found an Imcando dwarf named Thurgo thanks to|@str@information provided by Reldo. He wasn't very talkative|@str@until I gave him a Redberry pie, which he gobbled up.|"
	sqPicture  = "@str@Turgo needed a picture of the sword to replace.|"
	sqPictureS = "@str@Turgo needed a picture of the sword before he could start|@str@work on a replacement. I took him a portrait of it.|"
)

var sqJournalFixtures = map[int]string{
	SqNotStarted: "@dbl@I can start this quest by talking to the @dre@Squire @dbl@in the|@dbl@courtyard of the @dre@White Knights' Castle@dbl@.|@str@I have a mining level of 10|@dbl@I would have an advantage if I had a combat level of @dre@20.",
	SqStarted:    sqHead + "@dbl@The Squire suggests I speak to @dre@Reldo @dbl@in the @dre@Varrock Palace|@dre@Library @dbl@for information about the @dre@Imcando Dwarves.",
	SqSpokenReldo: sqHead + "@dbl@Reldo couldn't give me much information about the @dre@Imcando|@dbl@except a few live on the @dre@southern peninsula of Asgarnia,|" +
		"@dbl@they dislike strangers, and LOVE @dre@redberry pies.",
	SqGivenPie: sqHead + sqThurgoS + "@dbl@He will help me now I have gained his trust through @dre@pie.",
	SqSpokenThurgo: sqHead + sqThurgoS + "@dre@Thurgo @dbl@needs a @dre@picture of the sword @dbl@before he can help.|" +
		"@dbl@I should probably ask the @dre@Squire @dbl@about obtaining one.",
	SqLookingPortrait: sqHead + sqThurgoS + sqPicture + "@dbl@The Squire told me about a @dre@portrait @dbl@of Sir Vyvin's father|" +
		"@dbl@which has a @dre@picture of the sword @dbl@in @dre@Sir Vyvin's room.|",
	SqLookingBlurite: sqHead + sqThurgoS + sqPictureS + "@dbl@According to @dre@Thurgo @dbl@to make a @dre@replica sword @dbl@he will need @dre@two|" +
		"@dre@Iron Bars @dbl@and some @dre@Blurite Ore@dbl@. @dre@Blurite Ore @dbl@can only be|@dbl@found @dre@deep in the caves below Thurgo's house@dbl@. I should|" +
		"@dbl@prepare myself to fend off Ice giants.",
	SqComplete: sqHead + sqThurgoS + sqPictureS + "@str@After bringing Thurgo two iron bars and some blurite ore|@str@he made me a fine replica of Sir Vyvin's sword, which I|" +
		"@str@returned to the Squire for a reward.||@red@QUEST COMPLETE!",
}

func TestParseKnightsSwordJournalReadsEveryRenderedPage(t *testing.T) {
	for want, page := range sqJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseKnightsSwordJournal(lines)
			if !ok || got != want {
				t.Errorf("stage %d page %.70q: parsed %d %v", want, page, got, ok)
			}
		}
	}
	// With the sword smithed the page says so, and is still stage 6.
	smithed := sqHead + sqThurgoS + sqPictureS + "@str@Thurgo has now smithed me a replica of Sir Vyvin's sword.|@dbl@I should return it to the @dre@Squire @dbl@for my @dre@reward."
	if got, ok := ParseKnightsSwordJournal([]string{smithed}); !ok || got != SqLookingBlurite {
		t.Errorf("the smithed page parsed %d %v", got, ok)
	}
	// Under Mining 10 the first page says so; it is still stage 0.
	low := "@dbl@I can start this quest by talking to the @dre@Squire @dbl@in the|@dbl@courtyard of the @dre@White Knights' Castle@dbl@.|@dbl@I need a mining level of @dre@10|@dbl@I would have an advantage if I had a combat level of @dre@20."
	if got, ok := ParseKnightsSwordJournal([]string{low}); !ok || got != SqNotStarted {
		t.Errorf("the Mining-short first page parsed %d %v", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseKnightsSwordJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

// sqWorld is the Knight's Sword as the content runs it: %squire and the scripts' answers to each talk and each executor, the mule's stock, the
// range's rolls and Sir Vyvin's refusals.
type sqWorld struct {
	*kcWorld
	stage  int
	levels map[string]int
	worn   map[int]int
	// mule is the mule's published bank; muleAsked is this round's one ask.
	mule      map[int]int
	muleAsked bool
	// blocked are the kits whose every source is dead.
	blocked map[string]bool
	// cooks are the range's outcomes in order (true a pie, false a burnt one); cooked counts them.
	cooks  []bool
	cooked int
	// refusals are the searches Sir Vyvin refuses before one lands; searches counts the attempts.
	refusals, searches int
	// trips counts the visits to the dungeon, dungeonFails the visits that come out with no ore.
	trips, dungeonFails int
}

func newSqWorld(t *testing.T, stage int, inv, bank map[int]int) *sqWorld {
	w := &sqWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, mule: map[int]int{}, blocked: map[string]bool{}, worn: map[int]int{},
		levels: map[string]int{"attack": 60, "strength": 60, "defence": 40, "hitpoints": 62, "mining": 12, "cooking": 14, "prayer": 20, "magic": 40, "ranged": 20}}
	w.maxHP = 62
	w.tile = SqSquireAnchor
	w.bankKnown = true
	w.cooks = []bool{true}
	return w
}

func (w *sqWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case SqNotStarted:
		j = StatusNotStarted
	case SqComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage, true
	for k, v := range w.levels {
		s.Levels[k] = v
	}
	s.Levels["hitpoints"] = w.maxHP
	for k, v := range w.blocked {
		s.Blocked[k] = v
	}
	for id, n := range w.worn {
		if n > 0 {
			s.WornIDs[id] = n
		}
	}
	return s
}

// provision plays the real planner over the world, mule included: the bank's lines are drawn, the mule's bundle arrives with its one ask, a
// ground or shop task's quantity arrives if the world has that source; an unsourced line blocks the kit.
func (w *sqWorld) provision(st Step) bool {
	exhausted := map[string]bool{}
	for pass := 0; pass < 24; pass++ {
		pack := map[int]int{}
		for id, n := range w.inv {
			if n > 0 {
				pack[id] = n
			}
		}
		bank := map[int]int{}
		for id, n := range w.bank {
			bank[id] = n
		}
		plan := PlanProvision(st.Kit, ProvisionState{Pack: pack, Worn: map[int]int{}, Bank: bank, BankKnown: w.bankKnown, Mule: w.mule, MuleKnown: true,
			MuleAsked: w.muleAsked, FreeSlots: 28 - w.used(), Levels: map[string]int{"mining": w.levels["mining"]}, Exhausted: exhausted})
		switch plan.Phase {
		case PhaseSatisfied:
			return true
		case PhaseBank:
			w.bankKnown = true
			moved := false
			for _, c := range plan.Withdraw {
				n := min(c.Count, w.bank[c.ID])
				if c.ID == CoinsID {
					n = c.Count
				}
				if n > 0 {
					w.bank[c.ID] -= n
					w.inv[c.ID] += n
					moved = true
				}
			}
			if !moved && len(plan.Withdraw) > 0 {
				return false
			}
		case PhaseMule:
			w.muleAsked = true
			for _, c := range plan.MuleAsk {
				n := min(c.Count, w.mule[c.ID])
				w.mule[c.ID] -= n
				w.inv[c.ID] += n
			}
		case PhaseSource:
			task := plan.Tasks[0]
			ok := false
			switch task.Source.Kind {
			case SourceGround:
				ok = w.ground[task.Line.ID]
			case SourceShop:
				ok = w.shop[task.Line.ID]
			}
			if !ok {
				// A source that fails is skipped for the rest of the round, as the runner's Exhausted does.
				exhausted[SourceKey(task.Line, task.Source)] = true
				continue
			}
			w.inv[task.Line.ID] += task.Qty
		default:
			return false
		}
	}
	return false
}

func (w *sqWorld) apply(st Step) {
	t := w.t
	t.Helper()
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed step %s: %s", st.Describe(), why)
	}
	switch st.Kind {
	case StepTalk:
		w.talk(st)
	case StepProvision:
		key := "provision:" + st.KitName
		w.muleAsked = false
		if w.provision(st) {
			w.ran[key] = true
			delete(w.failed, key)
			delete(w.blocked, key)
		} else {
			w.failed[key] = true
			w.blocked[key] = true
		}
		w.log = append(w.log, key)
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepScanBank:
		w.bankKnown = true
		w.log = append(w.log, "scan bank")
	case StepWithdraw:
		for _, it := range st.Items {
			if w.bank[it.ID] < it.Qty {
				t.Fatalf("withdrawing %d of %d from a bank that holds %d", it.Qty, it.ID, w.bank[it.ID])
			}
			w.bank[it.ID] -= it.Qty
			w.inv[it.ID] += it.Qty
		}
		w.log = append(w.log, "withdraw "+st.Items[0].Name)
	case StepWalk:
		w.tile = st.Anchor
		w.log = append(w.log, "walk")
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *sqWorld) talk(st Step) {
	t := w.t
	t.Helper()
	w.tile = st.Stop.Anchor
	switch st.Stop.ID {
	case NpcSquire:
		switch {
		case w.stage == SqNotStarted:
			w.stage = SqStarted
		case w.stage == SqSpokenThurgo:
			w.stage = SqLookingPortrait
		case w.stage == SqLookingBlurite && w.inv[ObjSqSword] > 0:
			w.inv[ObjSqSword]--
			w.stage = SqComplete
		default:
			t.Fatalf("a talk with the Squire at stage %d with the sword %d", w.stage, w.inv[ObjSqSword])
		}
	case NpcReldo:
		if w.stage != SqStarted {
			t.Fatalf("a talk with Reldo at stage %d", w.stage)
		}
		w.stage = SqSpokenReldo
	case NpcThurgo:
		switch {
		case w.stage == SqSpokenReldo && w.inv[ObjSqRedberryPie] > 0:
			w.inv[ObjSqRedberryPie]--
			w.stage = SqGivenPie
		case w.stage == SqGivenPie:
			w.stage = SqSpokenThurgo
		case w.stage == SqLookingPortrait && w.inv[ObjSqPortrait] > 0:
			w.inv[ObjSqPortrait]--
			w.stage = SqLookingBlurite
		case w.stage == SqLookingBlurite && w.inv[ObjSqBluriteOre] > 0 && w.inv[ObjSqIronBar] >= 2:
			w.inv[ObjSqBluriteOre]--
			w.inv[ObjSqIronBar] -= 2
			w.inv[ObjSqSword]++
		default:
			t.Fatalf("a talk with Thurgo at stage %d with a pie %d, a portrait %d, an ore %d, %d bars", w.stage, w.inv[ObjSqRedberryPie], w.inv[ObjSqPortrait],
				w.inv[ObjSqBluriteOre], w.inv[ObjSqIronBar])
		}
	default:
		t.Fatalf("a talk with npc %d", st.Stop.ID)
	}
	w.log = append(w.log, "talk "+st.Stop.Npc)
}

func (w *sqWorld) custom(st Step) {
	t := w.t
	t.Helper()
	name := st.Custom
	have := func(id int) bool { return w.inv[id] > 0 }
	switch name {
	case CustomSquireFill:
		if !have(ObjSqBucket) {
			t.Fatalf("filling with no bucket")
		}
		w.inv[ObjSqBucket]--
		w.inv[ObjSqBucketWater]++
		w.tile = SqNearestWater(w.tile).Loc
	case CustomSquireDough:
		if !have(ObjSqPotOfFlour) || !have(ObjSqBucketWater) {
			t.Fatalf("dough with no flour or no water")
		}
		w.inv[ObjSqPotOfFlour]--
		w.inv[ObjSqBucketWater]--
		w.inv[ObjSqEmptyPot]++
		w.inv[ObjSqBucket]++
		w.inv[ObjSqPastryDough]++
	case CustomSquireShell:
		if st.ItemID != ObjSqPastryDough || st.TargetID != ObjSqPieDish || st.ExpectItemID != ObjSqPieShell || !have(ObjSqPastryDough) || !have(ObjSqPieDish) {
			t.Fatalf("a shell step %+v with dough %d and a dish %d", st, w.inv[ObjSqPastryDough], w.inv[ObjSqPieDish])
		}
		w.inv[ObjSqPastryDough]--
		w.inv[ObjSqPieDish]--
		w.inv[ObjSqPieShell]++
	case CustomSquireBerries:
		if st.ItemID != ObjSqRedberries || st.TargetID != ObjSqPieShell || st.ExpectItemID != ObjSqUncookedPie || !have(ObjSqRedberries) || !have(ObjSqPieShell) {
			t.Fatalf("a berries step %+v with berries %d and a shell %d", st, w.inv[ObjSqRedberries], w.inv[ObjSqPieShell])
		}
		if w.levels["cooking"] < SqCooking {
			t.Fatalf("berries into a shell at Cooking %d", w.levels["cooking"])
		}
		w.inv[ObjSqRedberries]--
		w.inv[ObjSqPieShell]--
		w.inv[ObjSqUncookedPie]++
	case CustomSquireCook:
		if !have(ObjSqUncookedPie) || w.levels["cooking"] < SqCooking {
			t.Fatalf("cooking with no uncooked pie, or below Cooking 10")
		}
		w.inv[ObjSqUncookedPie]--
		ok := w.cooked < len(w.cooks) && w.cooks[w.cooked]
		if w.cooked >= len(w.cooks) {
			ok = true
		}
		w.cooked++
		if ok {
			w.inv[ObjSqRedberryPie]++
		} else {
			w.inv[ObjSqBurntPie]++
		}
		w.tile = SqNearestRange(w.tile).Loc
	case CustomSquireEmpty:
		if st.ItemID != ObjSqBurntPie || !have(ObjSqBurntPie) {
			t.Fatalf("emptying with no burnt pie")
		}
		w.inv[ObjSqBurntPie]--
		w.inv[ObjSqPieDish]++
	case CustomSquirePortrait:
		if w.stage < SqLookingPortrait || have(ObjSqPortrait) {
			t.Fatalf("a search at stage %d with a portrait %d", w.stage, w.inv[ObjSqPortrait])
		}
		w.searches++
		if w.searches <= w.refusals {
			w.failed[name] = true
			w.log = append(w.log, name+" refused")
			return
		}
		delete(w.failed, name)
		w.inv[ObjSqPortrait]++
		w.tile = SqVyvinApproaches[0]
	case CustomSquireUnwield:
		if w.worn[ObjSqSword] == 0 {
			t.Fatalf("taking off a sword that is not worn")
		}
		w.worn[ObjSqSword]--
		w.inv[ObjSqSword]++
	case CustomSquireBlurite:
		w.trips++
		if InSquireDungeon(w.tile) {
			// A resumed visit: the account is inside and takes up the visit where it stands.
			w.tile = SqLadderTop
			if w.dungeonFails > 0 {
				w.dungeonFails--
				w.failed[name] = true
				w.log = append(w.log, name+" empty")
				return
			}
			w.inv[ObjSqBluriteOre]++
			delete(w.failed, name)
			w.log = append(w.log, name+" resumed")
			return
		}
		snap := w.snapshot()
		trip := SquireTripPrice(snap)
		if ok, why := sqSimGoesDown(snap, trip); !ok {
			t.Fatalf("the module sent an unprepared account down: %s", why)
		}
		if w.dungeonFails > 0 {
			w.dungeonFails--
			w.failed[name] = true
			w.tile = SqLadderTop
			w.log = append(w.log, name+" empty")
			return
		}
		w.inv[ObjSqBluriteOre]++
		delete(w.failed, name)
		w.tile = SqLadderTop
	default:
		t.Fatalf("unexpected custom %q", name)
	}
	w.log = append(w.log, name)
}

// sqSimGoesDown says whether an account is ready for the dungeon the way the executor checks it: the levels, a pickaxe, the food and the
// pack room, and not already inside.
func sqSimGoesDown(s Snapshot, trip SqTrip) (bool, string) {
	switch {
	case SquireLevelsShort(s, false) != "":
		return false, SquireLevelsShort(s, false)
	case !trip.OK:
		return false, trip.Why
	case DoricUsablePickaxe(s) == 0:
		return false, "no pickaxe"
	case SqFoodHeld(s) < trip.FoodWanted:
		return false, fmt.Sprintf("%d hitpoints of food for a trip that wants %d", SqFoodHeld(s), trip.FoodWanted)
	case s.HeldID(ObjSqIronBar) < 2:
		return false, "no bars"
	case s.FreeSlots < 1:
		return false, "no room for the ore"
	}
	return true, ""
}

func (w *sqWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 120; i++ {
		st := DecideKnightsSword(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile, w.worn, w.mule, w.trips)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile, w.worn, w.mule, w.trips) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("120 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// sqFullBank is a bank that holds everything the quest can ask of it.
func sqFullBank() map[int]int {
	return map[int]int{ObjSqPieDish: 1, ObjSqRedberries: 5, ObjSqPotOfFlour: 3, ObjSqBucket: 1, ObjSqIronBar: 4, ObjShark: 30, ObjBronzePickaxe: 1, CoinsID: 1000}
}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

// A fresh account with a full bank does the whole quest in the content's order: the Squire, Reldo, the pie's kit and its rungs, Thurgo
// twice, the Squire, the cupboard, Thurgo, the dungeon's kit, the dungeon, Thurgo and the Squire.
func TestAFreshAccountDoesTheKnightsSwordInOrder(t *testing.T) {
	w := newSqWorld(t, SqNotStarted, map[int]int{CoinsID: 50}, sqFullBank())
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"talk Squire", "talk Reldo", "provision:" + KitSquirePie, CustomSquireFill, CustomSquireDough, CustomSquireShell, CustomSquireBerries,
		CustomSquireCook, "talk Thurgo", "talk Thurgo", "talk Squire", CustomSquirePortrait, "talk Thurgo", "provision:" + KitSquireOre, CustomSquireBlurite,
		"talk Thurgo", "talk Squire"}
	// The bank was unread at the start of the pie: a scan comes first when the world has not read it.
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.stage != SqComplete || w.trips != 1 {
		t.Errorf("stage %d after %d trips", w.stage, w.trips)
	}
	for _, id := range []int{ObjSqSword, ObjSqPortrait, ObjSqBluriteOre, ObjSqRedberryPie} {
		if w.inv[id] != 0 {
			t.Errorf("item %d is still in the pack after the hand-in", id)
		}
	}
}

// An account whose bank holds nothing buys what the shops sell (the bucket, the flour, the berries), takes the dish from the kitchen floor
// and the bars and the sharks from the mule, and does the whole quest.
func TestAnEmptyBankIsSuppliedByTheShopsTheGroundAndTheMule(t *testing.T) {
	w := newSqWorld(t, SqSpokenReldo, map[int]int{CoinsID: 200, ObjBronzePickaxe: 1}, nil)
	w.tile = SqReldoAnchor
	w.shop[ObjSqPotOfFlour], w.shop[ObjSqRedberries], w.shop[ObjSqBucket] = true, true, true
	w.ground[ObjSqPieDish] = true
	w.mule = map[int]int{ObjSqIronBar: 14000, ObjShark: 80000, ObjSqPieDish: 260, ObjSqRedberries: 70}
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.stage != SqComplete {
		t.Fatalf("stage %d after %v", w.stage, w.log)
	}
	// The mule gave the dish and the berries (the item rule puts it before the ground and the shops); the shops gave the flour and the bucket.
	if w.mule[ObjSqPieDish] != 259 || w.mule[ObjSqRedberries] >= 70 || w.mule[ObjSqIronBar] != 13998 {
		t.Errorf("the mule gave %d dishes, %d berries and %d bars", 260-w.mule[ObjSqPieDish], 70-w.mule[ObjSqRedberries], 14000-w.mule[ObjSqIronBar])
	}
	if w.inv[ObjShark] == 0 {
		t.Errorf("no sharks left after the dungeon")
	}
}

// The range burns the pie: the dish comes back, and the chain begins again from the flour (the burnt dough is gone), twice, and ends with a pie.
func TestABurntPieIsEmptiedAndTheChainBeginsAgain(t *testing.T) {
	w := newSqWorld(t, SqSpokenReldo, map[int]int{CoinsID: 100}, sqFullBank())
	w.bank[ObjSqPotOfFlour], w.bank[ObjSqRedberries] = 6, 6
	w.cooks = []bool{false, false, true}
	why, _ := w.run()
	if why != "" || w.stage != SqComplete {
		t.Fatalf("waited %q at stage %d after %v", why, w.stage, w.log)
	}
	if w.cooked != 3 {
		t.Errorf("%d pies cooked", w.cooked)
	}
	empties := 0
	for _, step := range w.log {
		if step == CustomSquireEmpty {
			empties++
		}
	}
	if empties != 2 {
		t.Errorf("%d dishes emptied: %v", empties, w.log)
	}
	// A burnt pie costs its dough and its berries and nothing the account cannot get back: one dish served all three bakes.
	if w.inv[ObjSqPieDish] != 0 {
		t.Errorf("%d dishes left", w.inv[ObjSqPieDish])
	}
}

// Sir Vyvin keeps the cupboard: a refused search is the executor's failure, the module waits (a park of its own, with the reason) and the
// next attempt takes the portrait.
func TestSirVyvinsRefusalParksTheQuestAndTheNextTryTakesThePortrait(t *testing.T) {
	w := newSqWorld(t, SqLookingPortrait, map[int]int{CoinsID: 50}, sqFullBank())
	w.refusals = 1
	why, park := w.run()
	if park != SqRetryParkFor || !strings.Contains(why, "Sir Vyvin") {
		t.Fatalf("parked %s with %q after %v", park, why, w.log)
	}
	delete(w.failed, CustomSquirePortrait) // the next run starts afresh
	if why, _ := w.run(); why != "" || w.stage != SqComplete {
		t.Fatalf("waited %q at stage %d after %v", why, w.stage, w.log)
	}
	if w.searches != 2 {
		t.Errorf("%d searches", w.searches)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The gates
// ---------------------------------------------------------------------------------------------------------------------

func TestTheQuestIsNotStartedBelowItsLevels(t *testing.T) {
	for _, c := range []struct {
		name  string
		skill string
		level int
		want  string
	}{
		{"Mining 9", "mining", 9, "Mining 9/10"}, {"Cooking 9", "cooking", 9, "Cooking 9/10"}, {"Hitpoints 59", "hitpoints", 59, "Hitpoints 59/60"},
	} {
		w := newSqWorld(t, SqNotStarted, nil, sqFullBank())
		if c.skill == "hitpoints" {
			w.maxHP = c.level
		} else {
			w.levels[c.skill] = c.level
		}
		why, park := w.run()
		if park != SqLevelParkFor || !strings.Contains(why, c.want) {
			t.Errorf("%s: parked %s with %q", c.name, park, why)
		}
		if len(w.log) != 0 {
			t.Errorf("%s: steps ran: %v", c.name, w.log)
		}
	}
	// Combat 57 is refused and 58 is not: the unarmed Hobgoblins hunt 56 and under, and a combat read can be a level stale.
	w := newSqWorld(t, SqNotStarted, nil, sqFullBank())
	w.levels["attack"], w.levels["strength"], w.levels["defence"] = 45, 45, 35
	if c := DemonCombat(w.snapshot()); c >= SqMinCombat {
		t.Fatalf("the fixture is combat %d", c)
	}
	if why, park := w.run(); park != SqLevelParkFor || !strings.Contains(why, "combat") {
		t.Errorf("a combat-%d account: parked %s with %q", DemonCombat(w.snapshot()), park, why)
	}
	// The levels the snapshot has not read are not judged.
	s := Snapshot{Journal: StatusNotStarted}
	if SquireLevelsShort(s, true) != "" {
		t.Errorf("an unread snapshot is short of %q", SquireLevelsShort(s, true))
	}
	if st := DecideKnightsSword(s); st.Kind != StepTalk || st.Stop.ID != NpcSquire {
		t.Errorf("an unread account is sent to %s", st.Describe())
	}
}

func TestTheQuestWaitsAtTheRockForTheLevelsAndTheDungeonsOwnGates(t *testing.T) {
	// Mining 9 at stage 6, the bars and the food in hand: parked 6 hours, naming the level, with no step of the dungeon asked for.
	w := newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqIronBar: 2, ObjShark: 14, ObjBronzePickaxe: 1}, nil)
	w.levels["mining"] = 9
	why, park := w.run()
	if park != SqLevelParkFor || !strings.Contains(why, "Mining 9/10") || w.trips != 0 {
		t.Fatalf("parked %s with %q after %d trips", park, why, w.trips)
	}
	// Cooking does not count at stage 6: the pie is done.
	w = newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqIronBar: 2, ObjShark: 14, ObjBronzePickaxe: 1}, nil)
	w.levels["cooking"] = 1
	if why, park := w.run(); why != "" || w.trips != 1 {
		t.Fatalf("a Cooking-1 account at stage 6 waited %s %q after %d trips", park, why, w.trips)
	}
}

// No bars from the bank or the mule parks the quest for the item park with the reason said; it never smelts.
func TestNoBarsParkTheQuestAndNothingIsSmelted(t *testing.T) {
	w := newSqWorld(t, SqLookingBlurite, map[int]int{ObjShark: 14, ObjBronzePickaxe: 1}, map[int]int{DoricIronOre: 28, CoinsID: 100})
	why, park := w.run()
	if park != SqItemParkFor || !strings.Contains(why, "iron bar") || !strings.Contains(why, "never smelted") || w.trips != 0 {
		t.Fatalf("parked %s with %q after %d trips", park, why, w.trips)
	}
	for _, step := range w.log {
		if strings.Contains(step, "smelt") {
			t.Errorf("a smelting step ran: %v", w.log)
		}
	}
	// With the ore in hand and no bars the account asks for the bars alone.
	w = newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqBluriteOre: 1}, nil)
	if why, park := w.run(); park != SqItemParkFor || !strings.Contains(why, "iron bars") {
		t.Fatalf("with the ore: parked %s with %q", park, why)
	}
}

// Food short of the price parks the quest with the reason; a bank of bass tops up a bank short of sharks; and a mule that holds sharks is
// the first source.
func TestFoodIsAskedForInTwoKindsAndParksWhenShort(t *testing.T) {
	// Nothing to eat anywhere: parked, never down.
	w := newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqIronBar: 2, ObjBronzePickaxe: 1}, nil)
	why, park := w.run()
	if park != SqItemParkFor || !strings.Contains(why, "hitpoints of food") || w.trips != 0 {
		t.Fatalf("no food: parked %s with %q after %d trips", park, why, w.trips)
	}
	// A few sharks and a pile of bass: the second round makes up the difference.
	w = newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqIronBar: 2, ObjBronzePickaxe: 1}, map[int]int{ObjShark: 2, ObjBass: 40})
	if why, _ := w.run(); why != "" || w.trips != 1 || w.stage != SqComplete {
		t.Fatalf("sharks and bass: waited %q, %d trips, stage %d, log %v", why, w.trips, w.stage, w.log)
	}
	if w.inv[ObjShark]+w.inv[ObjBass] == 0 {
		t.Errorf("no food in the pack")
	}
	if !slices.Contains(w.log, "provision:"+KitSquireFood2) {
		t.Errorf("the second kind of food was not asked for: %v", w.log)
	}
}

// A visit that comes out empty is not tried again at once: the module waits, with the reason, and the next run tries.
func TestAnEmptyVisitIsNotRepeatedAtOnce(t *testing.T) {
	w := newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqIronBar: 2, ObjShark: 20, ObjBronzePickaxe: 1}, nil)
	w.dungeonFails = 1
	why, park := w.run()
	if park != SqTripRetryFor || !strings.Contains(why, "no ore") || w.trips != 1 {
		t.Fatalf("parked %s with %q after %d trips", park, why, w.trips)
	}
	delete(w.failed, CustomSquireBlurite)
	if why, _ := w.run(); why != "" || w.trips != 2 {
		t.Fatalf("the second run waited %q after %d trips", why, w.trips)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Resuming
// ---------------------------------------------------------------------------------------------------------------------

func TestTheSwordIsHandedOverFromTheBankTheWornPanelOrThePack(t *testing.T) {
	for _, c := range []struct {
		name string
		mk   func() *sqWorld
		want []string
	}{
		{"in the pack", func() *sqWorld { return newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqSword: 1}, nil) }, []string{"talk Squire"}},
		{"worn", func() *sqWorld {
			w := newSqWorld(t, SqLookingBlurite, nil, nil)
			w.worn[ObjSqSword] = 1
			return w
		}, []string{CustomSquireUnwield, "talk Squire"}},
		{"in the bank", func() *sqWorld { return newSqWorld(t, SqLookingBlurite, nil, map[int]int{ObjSqSword: 1}) }, []string{"withdraw Blurite sword", "talk Squire"}},
	} {
		w := c.mk()
		if why, _ := w.run(); why != "" || w.stage != SqComplete {
			t.Errorf("%s: waited %q at stage %d after %v", c.name, why, w.stage, w.log)
		}
		if !slices.Equal(w.log, c.want) {
			t.Errorf("%s: ran %v, want %v", c.name, w.log, c.want)
		}
	}
}

func TestAPortraitOrAPieInTheBankIsFetchedBeforeAnythingIsMade(t *testing.T) {
	w := newSqWorld(t, SqLookingPortrait, nil, map[int]int{ObjSqPortrait: 1})
	if _, _ = w.run(); !slices.Equal(w.log[:2], []string{"withdraw Portrait", "talk Thurgo"}) {
		t.Errorf("a banked portrait: %v", w.log)
	}
	w = newSqWorld(t, SqSpokenReldo, nil, map[int]int{ObjSqRedberryPie: 1})
	if _, _ = w.run(); !slices.Equal(w.log[:3], []string{"withdraw Redberry pie", "talk Thurgo", "talk Thurgo"}) {
		t.Errorf("a banked pie: %v", w.log)
	}
	// With the bank unread, the pie's chain begins with a read of it.
	w = newSqWorld(t, SqSpokenReldo, nil, map[int]int{ObjSqRedberryPie: 1})
	w.bankKnown = false
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepScanBank {
		t.Errorf("an unread bank at the pie: %s", st.Describe())
	}
	// A chain already begun does not wait for it.
	w = newSqWorld(t, SqSpokenReldo, map[int]int{ObjSqPastryDough: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, nil)
	w.bankKnown = false
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomSquireShell {
		t.Errorf("a begun chain with an unread bank: %s", st.Describe())
	}
}

func TestEveryRungOfThePieIsResumedFromWhereTheChainStands(t *testing.T) {
	cooking := 14
	for _, c := range []struct {
		name string
		inv  map[int]int
		want string
	}{
		{"burnt", map[int]int{ObjSqBurntPie: 1}, CustomSquireEmpty},
		{"uncooked", map[int]int{ObjSqUncookedPie: 1}, CustomSquireCook},
		{"shell and berries", map[int]int{ObjSqPieShell: 1, ObjSqRedberries: 1}, CustomSquireBerries},
		{"dough, dish and berries", map[int]int{ObjSqPastryDough: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, CustomSquireShell},
		{"flour, water, dish and berries", map[int]int{ObjSqPotOfFlour: 1, ObjSqBucketWater: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, CustomSquireDough},
		{"flour, a bucket, dish and berries", map[int]int{ObjSqPotOfFlour: 1, ObjSqBucket: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, CustomSquireFill},
	} {
		w := newSqWorld(t, SqSpokenReldo, c.inv, nil)
		w.levels["cooking"] = cooking
		st := DecideKnightsSword(w.snapshot())
		if st.Kind != StepCustom || st.Custom != c.want {
			t.Errorf("%s: %s, want %s", c.name, st.Describe(), c.want)
		}
	}
	// Dough with no dish asks for the dish only (and the berries), not for flour and water again.
	w := newSqWorld(t, SqSpokenReldo, map[int]int{ObjSqPastryDough: 1}, nil)
	st := DecideKnightsSword(w.snapshot())
	if st.Kind != StepProvision || len(st.Kit) != 3 || st.Kit[0].ID != ObjSqPieDish || st.Kit[1].ID != ObjSqRedberries || st.Kit[2].ID != CoinsID || !st.Kit[2].Optional {
		t.Errorf("dough only: %s %+v", st.Describe(), st.Kit)
	}
	// With the coins in the pack the shops are paid from it and no coin line is asked.
	w = newSqWorld(t, SqSpokenReldo, map[int]int{ObjSqPastryDough: 1, CoinsID: SqPieCoins}, nil)
	if st := DecideKnightsSword(w.snapshot()); len(st.Kit) != 2 {
		t.Errorf("dough only with the coins held: %s %+v", st.Describe(), st.Kit)
	}
	// A pie shell with Cooking 9: the level is waited for, not the berries bought.
	w = newSqWorld(t, SqSpokenReldo, map[int]int{ObjSqPieShell: 1, ObjSqRedberries: 1}, nil)
	w.levels["cooking"] = 9
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepWait || st.ParkFor != SqLevelParkFor || !strings.Contains(st.Reason, "Cooking 10") {
		t.Errorf("a shell at Cooking 9: %s", st.Describe())
	}
}

// A rung of the chain that failed on its last try is not tried again in a loop: the quest is parked with the rung named, for the retry time.
func TestARungThatFailedParksTheQuestWithItsReason(t *testing.T) {
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		worn  bool
		fail  string
		match string
	}{
		{"empty", SqSpokenReldo, map[int]int{ObjSqBurntPie: 1}, false, CustomSquireEmpty, "burnt pie"},
		{"cook", SqSpokenReldo, map[int]int{ObjSqUncookedPie: 1}, false, CustomSquireCook, "range"},
		{"berries", SqSpokenReldo, map[int]int{ObjSqPieShell: 1, ObjSqRedberries: 1}, false, CustomSquireBerries, "redberries"},
		{"shell", SqSpokenReldo, map[int]int{ObjSqPastryDough: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, false, CustomSquireShell, "pastry dough"},
		{"dough", SqSpokenReldo, map[int]int{ObjSqPotOfFlour: 1, ObjSqBucketWater: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, false, CustomSquireDough, "pastry dough"},
		{"fill", SqSpokenReldo, map[int]int{ObjSqPotOfFlour: 1, ObjSqBucket: 1, ObjSqPieDish: 1, ObjSqRedberries: 1}, false, CustomSquireFill, "bucket"},
		{"unwield", SqLookingBlurite, nil, true, CustomSquireUnwield, "sword"},
	} {
		w := newSqWorld(t, c.stage, c.inv, nil)
		w.levels["cooking"] = 14
		if c.worn {
			w.worn[ObjSqSword] = 1
		}
		if st := DecideKnightsSword(w.snapshot()); st.Kind != StepCustom || st.Custom != c.fail {
			t.Errorf("%s: before the failure the step is %s", c.name, st.Describe())
		}
		w.failed[c.fail] = true
		st := DecideKnightsSword(w.snapshot())
		if st.Kind != StepWait || st.ParkFor != SqRetryParkFor || !strings.Contains(st.Reason, c.match) || !strings.Contains(st.Reason, "last try") {
			t.Errorf("%s: after the failure the step is %s (%q)", c.name, st.Describe(), st.Reason)
		}
	}
}

func TestTheDungeonIsResumedFromInsideAndLeftWithTheOre(t *testing.T) {
	in := tile.Tile{X: 3040, Z: 9570}
	// Inside with no ore: the visit's own step, whatever the pack holds (it takes its own measure).
	w := newSqWorld(t, SqLookingBlurite, nil, nil)
	w.tile = in
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomSquireBlurite {
		t.Errorf("inside with no ore: %s", st.Describe())
	}
	// Inside after a failed visit: out by the ladder.
	w.failed[CustomSquireBlurite] = true
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepWalk || st.Anchor != SqLadderTop {
		t.Errorf("inside after a failed visit: %s", st.Describe())
	}
	// Inside with the ore: out by the ladder, whatever else is short.
	w = newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqBluriteOre: 1}, nil)
	w.tile = in
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepWalk || st.Anchor != SqLadderTop {
		t.Errorf("inside with the ore: %s", st.Describe())
	}
	// Outside with the ore and the bars: to Thurgo.
	w = newSqWorld(t, SqLookingBlurite, map[int]int{ObjSqBluriteOre: 1, ObjSqIronBar: 2}, nil)
	if st := DecideKnightsSword(w.snapshot()); st.Kind != StepTalk || st.Stop.ID != NpcThurgo {
		t.Errorf("with the ore and the bars: %s", st.Describe())
	}
	// A bank stage is never asked for from underground, where the map's z means nothing.
	if sqBank(Snapshot{Tile: in, TileKnown: true}) != NearestBank(SqSquireAnchor).Tile {
		t.Errorf("an underground account banks at %v", sqBank(Snapshot{Tile: in, TileKnown: true}))
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The price
// ---------------------------------------------------------------------------------------------------------------------

func sqAccount(def, mining int, pick int) Snapshot {
	s := Snapshot{Levels: map[string]int{"defence": def, "mining": mining, "hitpoints": 60, "attack": 60, "strength": 60}, InvIDs: map[int]int{}, WornIDs: map[int]int{}}
	if pick != 0 {
		s.InvIDs[pick] = 1
	}
	return s
}

func TestTheTripIsPricedFromDefenceMiningAndThePickaxe(t *testing.T) {
	base := SquireTripPrice(sqAccount(40, 10, 0))
	if !base.OK || base.WalkTicks != 37 || base.MineTicks != SqMineTicks(10, 7) || base.FoodWanted != int(math.Ceil(2*base.Damage)) {
		t.Fatalf("the Defence 40, Mining 10 trip is %+v", base)
	}
	// The hits and the time: 2*37 + 59 ticks at a quarter attack a tick, at the model's per-attack price.
	if got := float64(2*37+59) / 4 * base.PerAttack; got != base.Damage {
		t.Errorf("damage %v, want %v", base.Damage, got)
	}
	if base.Damage < 55 || base.Damage > 90 || base.FoodWanted < 110 || base.FoodWanted > 180 {
		t.Errorf("a Defence 40 account at Mining 10 is priced %.1f hitpoints and %d of food", base.Damage, base.FoodWanted)
	}
	// More Defence, a better pickaxe and a higher Mining level each make it cheaper, and never dearer.
	prev := base
	for _, s := range []Snapshot{sqAccount(60, 10, 0), sqAccount(60, 20, 1269), sqAccount(60, 41, 1275), sqAccount(90, 41, 1275)} {
		p := SquireTripPrice(s)
		if p.Damage > prev.Damage || p.FoodWanted > prev.FoodWanted {
			t.Errorf("Defence %d Mining %d: %+v is dearer than %+v", s.Level("defence"), s.Level("mining"), p, prev)
		}
		prev = p
	}
	// Defence 1 is the dearest an account can be, and the cap holds it (230 hitpoints of food is under 280).
	worst := SquireTripPrice(sqAccount(1, 10, 0))
	if !worst.OK || worst.FoodWanted <= 200 || worst.FoodWanted > SqMaxFoodHeal {
		t.Errorf("Defence 1: %+v", worst)
	}
	// An unread Defence is the Defence 1 price.
	if got := SquireTripPrice(Snapshot{Levels: map[string]int{"mining": 10}}); got.FoodWanted != worst.FoodWanted {
		t.Errorf("an unread Defence is priced %d, Defence 1 %d", got.FoodWanted, worst.FoodWanted)
	}
	// A pickaxe the Mining level cannot use is not counted: a rune pickaxe at Mining 10 is the bronze rate.
	if got := SquireTripPrice(sqAccount(40, 10, 1275)); got.MineTicks != base.MineTicks {
		t.Errorf("a rune pickaxe at Mining 10 prices %d ticks, the bronze %d", got.MineTicks, base.MineTicks)
	}
	// A refused price says why, and the cap is the number it is compared with.
	if SqMaxFoodHeal != SqMaxFoodItems*20 {
		t.Errorf("SqMaxFoodHeal %d", SqMaxFoodHeal)
	}
}

func TestTheRockAndTheBreakOffAreTheNumbersTheCommentsSay(t *testing.T) {
	if got := SqRockChance(10) * 256; got != 119 {
		t.Errorf("Mining 10: %v in 256", got)
	}
	if got := SqRockChance(99) * 256; got != 256 {
		t.Errorf("Mining 99: %v in 256", got)
	}
	if got := SqRockChance(0); got != SqRockChance(1) || got <= 0 {
		t.Errorf("Mining 0: %v", got)
	}
	if SqMineTicks(99, 2) != 3+2 {
		t.Errorf("a sure roll takes %d ticks", SqMineTicks(99, 2))
	}
	for _, c := range []struct {
		hp, max, food int
		want          bool
	}{{20, 60, 0, true}, {21, 60, 0, false}, {10, 60, 40, false}, {0, 0, 0, false}, {5, 60, 0, true}} {
		if got := SqBreakOff(c.hp, c.max, c.food); got != c.want {
			t.Errorf("SqBreakOff(%d, %d, %d) = %v", c.hp, c.max, c.food, got)
		}
	}
	if SqStartHP(60) != 54 || SqStartHP(0) != 0 {
		t.Errorf("SqStartHP(60) = %d", SqStartHP(60))
	}
	// The eat line is two hits and more above the warrior's hardest, and a bite of 40 percent of the minimum hitpoints.
	if SqEatAt < 2*FoeIceWarrior.PeakHit() || SqEatAt*100 > 40*SqMinHitpoints {
		t.Errorf("the eat line is %d", SqEatAt)
	}
	if got := sqSustain.VsFoe(FoeIceWarrior, 60).EatAt; got != SqEatAt {
		t.Errorf("the line against a warrior is %d", got)
	}
}

func TestTheStandsAreTheEastRocksAndTheHeldTilesAreTheWorstOfThem(t *testing.T) {
	if len(SqStands) != 2 || SqHeldWalkTiles() != 37 {
		t.Fatalf("stands %v, held %d", SqStands, SqHeldWalkTiles())
	}
	for _, st := range SqStands {
		if st.Rock.X != 3067 || st.Stand.X != 3066 || st.Rock.Level != 0 || !InSquireDungeon(st.Stand) || !InSquireDungeon(st.Rock) {
			t.Errorf("stand %+v", st)
		}
	}
	if InSquireDungeon(SqLadderTop) || !InSquireDungeon(SqLanding) || InSquireDungeon(tile.Tile{X: 3040, Z: 9300}) {
		t.Errorf("the dungeon box: top %v landing %v", InSquireDungeon(SqLadderTop), InSquireDungeon(SqLanding))
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The helpers
// ---------------------------------------------------------------------------------------------------------------------

func TestTheNearestWaterAndRangeAreChosenByDistance(t *testing.T) {
	for _, c := range []struct {
		from  tile.Tile
		water string
		rng   string
	}{
		{WydinShop.Anchor, "Rimmington's sink", "Port Sarim's range"},
		{tile.Tile{X: 2970, Z: 3200}, "Rimmington's sink", "Rimmington's range"},
		{SqReldoAnchor, "the Varrock palace kitchen's sink", "the Varrock palace kitchen's range"},
		{SqSquireAnchor, "Falador's fountain", "Falador's range"},
	} {
		if got := SqNearestWater(c.from).Name; got != c.water {
			t.Errorf("from %v the water is %s, want %s", c.from, got, c.water)
		}
		if got := SqNearestRange(c.from).Name; got != c.rng {
			t.Errorf("from %v the range is %s, want %s", c.from, got, c.rng)
		}
	}
}

func TestSirVyvinsProximityAndTheApproachFarthestFromHim(t *testing.T) {
	a, b := SqVyvinApproaches[0], SqVyvinApproaches[1]
	for _, c := range []struct {
		vyvin tile.Tile
		near  bool
	}{{a, true}, {tile.Tile{X: a.X + 1, Z: a.Z + 1, Level: 2}, true}, {tile.Tile{X: a.X + 2, Z: a.Z, Level: 2}, false}, {tile.Tile{X: a.X, Z: a.Z, Level: 1}, false}} {
		if got := SqVyvinTooClose(a, c.vyvin); got != c.near {
			t.Errorf("Sir Vyvin at %v from %v: %v", c.vyvin, a, got)
		}
	}
	// With him east of both approaches the west one is the far one; with him west of both the east one; unknown, the first.
	if got := SqBestApproach(tile.Tile{X: a.X + 4, Z: a.Z, Level: 2}, true); got != b {
		t.Errorf("Sir Vyvin to the east: %v", got)
	}
	if got := SqBestApproach(tile.Tile{X: b.X - 4, Z: b.Z, Level: 2}, true); got != a {
		t.Errorf("Sir Vyvin to the west: %v", got)
	}
	if got := SqBestApproach(tile.Tile{}, false); got != a {
		t.Errorf("Sir Vyvin unknown: %v", got)
	}
	// The search is put off for his position for at most SqMaxSkips passes, and only when he is known and close.
	if !SqShouldWaitOut(0, a, a, true) || !SqShouldWaitOut(SqMaxSkips-1, a, a, true) || SqShouldWaitOut(SqMaxSkips, a, a, true) || SqShouldWaitOut(0, a, a, false) {
		t.Errorf("the wait-out rule")
	}
}

func TestEveryStepTheModuleCanAskForIsWellFormed(t *testing.T) {
	// The module walks every stage from a fresh world; ValidateStep is asserted by apply. The kits are valid lines of the provisioner.
	for _, kit := range [][]KitLine{sqPieKitLines(Snapshot{InvIDs: map[int]int{}}), sqPieKitLines(Snapshot{InvIDs: map[int]int{ObjSqPastryDough: 1}}),
		sqPieKitLines(Snapshot{InvIDs: map[int]int{ObjSqPieShell: 1}}), sqBerryFallbackKit(), sqOreKitLines(sqAccount(40, 10, 0), SquireTripPrice(sqAccount(40, 10, 0)), true),
		sqOreKitLines(sqAccount(40, 10, 0), SquireTripPrice(sqAccount(40, 10, 0)), false)} {
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("%s", why)
			}
		}
	}
	m := KnightsSword
	if m.Decide == nil || m.ReadStage == nil || !m.NoRetaliate || m.MaxDeaths != 1 || m.Record.QuestPoints != 1 || m.Record.ID != "squire" {
		t.Errorf("the module: %+v", m)
	}
	if c, ok := m.StepDeathCaps[CustomSquireBlurite]; !ok || c.Max != 1 || c.Skip != 72*time.Hour {
		t.Errorf("the dungeon's death cap: %+v", c)
	}
	// The pie, the berries and the sword are the module's tools: the floor bite never eats the pie, and a bank visit leaves them in the pack.
	for _, id := range []int{ObjSqRedberryPie, ObjSqRedberries, ObjSqSword, ObjSqPortrait, ObjSqBluriteOre, ObjSqIronBar, ObjSqBucketWater} {
		if !slices.Contains(SqToolIDs(), id) || !slices.Contains(SqKeepIDs(), id) {
			t.Errorf("item %d is not a tool of the module", id)
		}
	}
	for _, id := range SqFoodIDs() {
		if slices.Contains(SqToolIDs(), id) || !slices.Contains(SqKeepIDs(), id) {
			t.Errorf("food %d: a tool %v or missing from the keep list", id, slices.Contains(SqToolIDs(), id))
		}
	}
	// Every pickaxe is kept.
	for _, p := range doricPickaxes {
		if !slices.Contains(SqToolIDs(), p.ID) {
			t.Errorf("pickaxe %d is not kept", p.ID)
		}
	}
}
