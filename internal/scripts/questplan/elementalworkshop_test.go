package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Elemental Workshop (W13, 19 Oct 2026): the journal parser over the pages the content renders, every state of Decide, the kit through
// the real planner, the earth elemental's odds, and whole simulated quests (a world that answers every step the way the content's scripts
// do).

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

// ewJournalPages are the pages elemental_workshop_journal.rs2 renders, every string of a branch joined in order (the journal is
// `~quest_journal("Elemental Workshop", $text)` over exactly that text, `|` the client's line break).
const (
	ewBook   = "@str@I have found a battered book in a house in Seers Village.|@str@It tells of magic ore and a workshop created to fashion it.||"
	ewCut    = "@str@Cutting open the spine of the book with a blade,|@str@I found a key hidden under the leather binding.||"
	ewDoor   = "@str@I have found a secret door in the Seers Village smithy.||"
	ewWhere  = "@dbl@Where is this workshop and how do I get in?"
	ewWhereS = "@str@Where is this workshop and how do I get in?||"
	ewWater  = "@str@I managed to get the waterwheel in the northern water|@str@area going by turning some valves and pulling a lever.||"
	ewBell   = "@str@The bellows in the eastern air area are now fixed,|@str@I just needed to stitch some leather over that hole.||"
	ewLavaP  = "@str@I lit the furnace with lava taken from the trough,|@str@using the stone bowl I found.||"
	ewLots   = "@dbl@There is obviously lots to do here."
	ewBarP   = "@str@I have made a bar from the elemental ore.|@dbl@Now I just need to make something from it.||All I need to do now is make the shield.|"
)

func ewEntered(flags ...string) string {
	return ewBook + ewCut + ewDoor + ewWhereS + strings.Join(flags, "") + ewLots
}

var ewJournalFixtures = map[int]string{
	EwNotStarted:          "@dbl@I can start this quest by reading a|@dre@book@dbl@ found in @dre@Seers village@dbl@.||@dbl@Minimum requirements:|@str@Level 20 Mining|@str@Level 20 Smithing|@str@Level 20 Crafting",
	EwRead:                ewBook + ewWhere,
	EwSlashed:             ewBook + ewCut + ewWhere,
	EwEntered:             ewEntered(),
	EwEntered | EwWater:   ewEntered(ewWater),
	EwEntered | EwBellows: ewEntered(ewBell),
	EwEntered | EwWater | EwBellows | EwFurnace: ewEntered(ewWater, ewBell, ewLavaP),
	// With the bar in the pack the page ends differently, and the machinery's flags stay.
	EwEntered | EwWater | EwBellows | EwFurnace | EwBar: ewBook + ewCut + ewDoor + ewWhereS + ewWater + ewBell + ewLavaP + ewBarP,
	EwComplete: ewBook + "@str@After fixing up the old workshop machinery, collecting ore|@str@and smelting it I was able to create an Elemental Shield.||@red@QUEST COMPLETE!",
}

func TestParseElementalWorkshopJournalReadsEveryRenderedPage(t *testing.T) {
	for want, page := range ewJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseElementalWorkshopJournal(lines)
			if !ok || got != want {
				t.Errorf("stage %d page %.70q: parsed %d %v", want, page, got, ok)
			}
		}
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseElementalWorkshopJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
	// The stage and the flags separate cleanly.
	s := EwEntered | EwWater | EwFurnace
	if EwStageOf(s) != EwEntered || !EwHas(s, EwWater) || EwHas(s, EwBellows) || !EwHas(s, EwFurnace) || EwHas(s, EwBar) {
		t.Errorf("stage %d: base %d", s, EwStageOf(s))
	}
	// The flags are read only on the entered page: a page that has not taken the stairs carries none (the water paragraph cannot appear).
	if got, _ := ParseElementalWorkshopJournal([]string{ewBook + ewCut + ewWhere + ewWater}); got != EwSlashed {
		t.Errorf("a pre-entry page with a water paragraph parsed %d", got)
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

// ewWorld is the Elemental Workshop as the content runs it: the book's bits, the machinery's, the crates' once-only gifts, the furnace's
// rules, and where the account stands.
type ewWorld struct {
	*kcWorld
	// stage is what the journal parser returns: the book and the key in the low bits, the machinery in the high.
	stage  int
	levels map[string]int
	// needleGiven and leatherGiven are the crates' once-only bits.
	needleGiven, leatherGiven bool
	// ores counts the earth elementals fought; blocked is the kits whose every source is dead.
	ores    int
	blocked map[string]bool
}

func newEwWorld(t *testing.T, stage int, inv, bank map[int]int) *ewWorld {
	w := &ewWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, blocked: map[string]bool{},
		levels: map[string]int{"attack": 45, "strength": 45, "defence": 35, "hitpoints": 50, "mining": 25, "smithing": 25, "crafting": 25}}
	w.maxHP = 50
	w.tile = SeersBank
	w.shop[ObjHammer] = true
	w.shop[ObjBronzePickaxe] = true
	w.shop[ObjEwThread] = true
	return w
}

func (w *ewWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch EwStageOf(w.stage) {
	case EwNotStarted:
		j = StatusNotStarted
	case EwComplete:
		j = StatusComplete
	}
	s := w.base(j)
	s.Stage, s.StageKnown = w.stage, true
	for k, v := range w.levels {
		s.Levels[k] = v
	}
	for k, v := range w.blocked {
		s.Blocked[k] = v
	}
	return s
}

func (w *ewWorld) apply(st Step) {
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
			delete(w.blocked, key)
		} else {
			w.failed[key] = true
			w.blocked[key] = true
		}
		w.log = append(w.log, key)
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

func (w *ewWorld) inWorkshop() bool { return EwRegionOf(w.tile) == EwWorkshop }

func (w *ewWorld) custom(st Step) {
	t := w.t
	t.Helper()
	name := st.Custom
	inside := w.inWorkshop()
	needInside := func() {
		if !inside {
			t.Fatalf("%s outside the workshop, at %v", name, w.tile)
		}
	}
	switch name {
	case CustomEwBook:
		if inside || w.inv[ObjEwBook] > 0 {
			t.Fatalf("the bookcase from inside, or with a book held")
		}
		w.inv[ObjEwBook]++
	case CustomEwRead:
		if w.inv[ObjEwBook] == 0 {
			t.Fatalf("a read without a book")
		}
		w.stage = EwRead
	case CustomEwSlash:
		if w.inv[ObjEwBook] == 0 || w.inv[ObjKnife] == 0 || w.stage == EwNotStarted {
			t.Fatalf("a cut without a book read or a knife")
		}
		w.inv[ObjEwKey]++
		w.stage = w.stage&^EwStageMask | max(EwStageOf(w.stage), EwSlashed)
	case CustomEwEnter:
		if inside {
			t.Fatalf("entering from inside")
		}
		if w.inv[ObjEwKey] == 0 && EwRegionOf(w.tile) != EwSecretRoom {
			t.Fatalf("entering with no key, from %v", w.tile)
		}
		w.tile = EwArrive
		w.stage = w.stage&^EwStageMask | EwEntered
	case CustomEwLeave:
		w.tile = EwWallOutside
	case CustomEwSupplies:
		needInside()
		switch st.ItemID {
		case ObjEwNeedle:
			if !w.needleGiven && w.inv[ObjEwNeedle] == 0 {
				w.needleGiven = true
				w.inv[ObjEwNeedle]++
			}
		case ObjEwLeather:
			if !w.leatherGiven && w.inv[ObjEwLeather] == 0 {
				w.leatherGiven = true
				w.inv[ObjEwLeather]++
			}
		case ObjEwBowl:
			if w.inv[ObjEwBowl] == 0 {
				w.inv[ObjEwBowl]++
			}
		default:
			t.Fatalf("supplies for item %d", st.ItemID)
		}
		name += fmt.Sprintf("(%d)", st.ItemID)
	case CustomEwWater:
		needInside()
		w.stage |= EwWater
	case CustomEwBellows:
		needInside()
		if w.inv[ObjEwNeedle] == 0 || w.inv[ObjEwLeather] == 0 || w.inv[ObjEwThread] == 0 {
			t.Fatalf("the bellows without a needle, a leather and a thread")
		}
		w.inv[ObjEwLeather]--
		w.inv[ObjEwThread]--
		w.stage |= EwBellows
	case CustomEwLava:
		needInside()
		if w.inv[ObjEwBowl]+w.inv[ObjEwBowlFull] == 0 {
			t.Fatalf("lava without a bowl")
		}
		w.stage |= EwFurnace
	case CustomEwOre:
		needInside()
		if EwPickaxeFor(w.inv, nil, w.levels["mining"]) == 0 {
			t.Fatalf("the rock with no pickaxe")
		}
		w.ores++
		w.inv[ObjEwOre]++
	case CustomEwSmelt:
		needInside()
		if all := EwWater | EwBellows | EwFurnace; w.inv[ObjEwOre] == 0 || w.inv[ObjEwCoal] < EwCoalNeeded || w.stage&all != all {
			t.Fatalf("a smelt with the ore %d, the coal %d, the stage %d", w.inv[ObjEwOre], w.inv[ObjEwCoal], w.stage)
		}
		w.inv[ObjEwOre]--
		w.inv[ObjEwCoal] -= EwCoalNeeded
		w.inv[ObjEwBar]++
		w.stage |= EwBar
	case CustomEwShield:
		needInside()
		if w.inv[ObjEwBar] == 0 || w.inv[ObjHammer] == 0 || w.inv[ObjEwBook] == 0 {
			t.Fatalf("a shield without the bar, the hammer and the book")
		}
		w.inv[ObjEwBar]--
		w.inv[ObjEwShield]++
		w.stage = EwComplete
	default:
		t.Fatalf("unexpected custom %q", name)
	}
	w.log = append(w.log, name)
}

func (w *ewWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideElementalWorkshop(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.tile) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("80 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// ewFullBank is a bank that holds everything the quest can ask of it.
func ewFullBank() map[int]int {
	return map[int]int{ObjKnife: 1, ObjHammer: 1, ObjEwCoal: 12, ObjEwThread: 5, ObjShark: 30, ObjBronzePickaxe: 1, 1269: 1, CoinsID: 1000}
}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

// A fresh account with a full bank does the whole quest in the content's order: the book, read, the knife through its spine, one
// provisioning round, the stairs, the machinery (the needle, the leather and the bowl from the crates), the rock, the furnace, the bench.
func TestAFreshAccountDoesTheElementalWorkshopInOrder(t *testing.T) {
	w := newEwWorld(t, EwNotStarted, map[int]int{CoinsID: 50}, ewFullBank())
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{CustomEwBook, CustomEwRead, "provision:" + KitEwKnife, CustomEwSlash, "provision:" + KitEwKit, CustomEwEnter, CustomEwWater,
		CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwNeedle), CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwLeather), CustomEwBellows,
		CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwBowl), CustomEwLava, CustomEwOre, CustomEwSmelt, CustomEwShield}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.ores != 1 || w.inv[ObjEwShield] != 1 || w.inv[ObjEwCoal] != 0 || w.inv[ObjEwOre] != 0 || w.inv[ObjEwBar] != 0 {
		t.Errorf("%d fights, %d shields, %d coal, %d ore, %d bars", w.ores, w.inv[ObjEwShield], w.inv[ObjEwCoal], w.inv[ObjEwOre], w.inv[ObjEwBar])
	}
	// The best usable pickaxe the bank holds was drawn (steel at Mining 25), not the bronze one from the shop.
	if w.inv[1269] != 1 || w.inv[ObjBronzePickaxe] != 0 {
		t.Errorf("pickaxes: steel %d, bronze %d", w.inv[1269], w.inv[ObjBronzePickaxe])
	}
}

// An account with an empty bank buys what the shops sell (the hammer, a bronze pickaxe, the thread) and is stopped by what no shop does
// (the coal): it parks for the item park with the reason said, before any step of the workshop.
func TestAnEmptyBankBuysAtTheShopsAndParksForTheCoal(t *testing.T) {
	w := newEwWorld(t, EwSlashed, map[int]int{CoinsID: 100, ObjEwKey: 1, ObjEwBook: 1, ObjShark: 8}, nil)
	w.bankKnown = true
	why, park := w.run()
	if park != EwItemParkFor || !strings.Contains(why, "coal") {
		t.Fatalf("parked %s with %q after %v", park, why, w.log)
	}
	// The shops' lines were bought on the way: the hammer, a pickaxe and the thread are in the pack, the coal is not.
	if w.inv[ObjHammer] != 1 || w.inv[ObjBronzePickaxe] != 1 || w.inv[ObjEwThread] != 1 || w.inv[ObjEwCoal] != 0 {
		t.Errorf("hammer %d, pickaxe %d, thread %d, coal %d after %v", w.inv[ObjHammer], w.inv[ObjBronzePickaxe], w.inv[ObjEwThread], w.inv[ObjEwCoal], w.log)
	}
	for _, step := range w.log {
		if strings.HasPrefix(step, "ew-") && step != CustomEwEnter {
			t.Errorf("a workshop step %q ran for an account that cannot be supplied", step)
		}
	}
}

// A quest begun elsewhere resumes from where the journal and the pack say: each stage's first step.
func TestTheElementalWorkshopResumesFromEveryStage(t *testing.T) {
	kit := map[int]int{ObjEwKey: 1, ObjEwBook: 1, ObjHammer: 1, ObjBronzePickaxe: 1, ObjEwCoal: 4, ObjEwThread: 1, ObjEwNeedle: 1, ObjEwLeather: 1, ObjShark: 6}
	sup := func(extra map[int]int) map[int]int {
		m := map[int]int{}
		for id, n := range kit {
			m[id] = n
		}
		for id, n := range extra {
			m[id] = n
		}
		return m
	}
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		at    tile.Tile
		want  []string
	}{
		{"the book read, no key", EwRead, map[int]int{ObjEwBook: 1, ObjKnife: 1}, SeersBank, []string{CustomEwSlash, "provision:" + KitEwKit, CustomEwEnter,
			CustomEwWater, CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwNeedle), CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwLeather), CustomEwBellows,
			CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwBowl), CustomEwLava, CustomEwOre, CustomEwSmelt, CustomEwShield}},
		{"the key cut, the kit held", EwSlashed, sup(nil), SeersBank, []string{CustomEwEnter, CustomEwWater, CustomEwBellows,
			CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwBowl), CustomEwLava, CustomEwOre, CustomEwSmelt, CustomEwShield}},
		{"in the secret room with the kit", EwEntered - 1, sup(nil), tile.Tile{X: 2710, Z: 3497}, []string{CustomEwEnter, CustomEwWater, CustomEwBellows,
			CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwBowl), CustomEwLava, CustomEwOre, CustomEwSmelt, CustomEwShield}},
		{"inside, the wheel runs", EwEntered | EwWater, sup(nil), EwArrive, []string{CustomEwBellows, CustomEwSupplies + fmt.Sprintf("(%d)", ObjEwBowl), CustomEwLava,
			CustomEwOre, CustomEwSmelt, CustomEwShield}},
		{"inside, the machinery done", EwEntered | EwWater | EwBellows | EwFurnace, sup(nil), EwArrive, []string{CustomEwOre, CustomEwSmelt, CustomEwShield}},
		{"inside, the ore held", EwEntered | EwWater | EwBellows | EwFurnace, sup(map[int]int{ObjEwOre: 1}), EwArrive, []string{CustomEwSmelt, CustomEwShield}},
		{"inside, the bar held", EwEntered | EwWater | EwBellows | EwFurnace | EwBar, sup(map[int]int{ObjEwBar: 1, ObjEwCoal: 0}), EwArrive, []string{CustomEwShield}},
		{"outside after the bar", EwEntered | EwWater | EwBellows | EwFurnace | EwBar, map[int]int{ObjEwKey: 1, ObjEwBook: 1, ObjHammer: 1, ObjEwBar: 1}, SeersBank,
			[]string{CustomEwEnter, CustomEwShield}},
	} {
		w := newEwWorld(t, c.stage, c.inv, ewFullBank())
		w.tile = c.at
		w.bankKnown = true
		if why, _ := w.run(); why != "" {
			t.Errorf("%s: waited: %s (log %v)", c.name, why, w.log)
			continue
		}
		if !slices.Equal(w.log, c.want) {
			t.Errorf("%s: the quest ran\n  %v\nwant\n  %v", c.name, w.log, c.want)
		}
	}
}

// Inside the workshop, the item the rest of the work cannot do without sends the account up (the leave is the pocket's own way out),
// and the surface supplies it before the account goes back down.
func TestAnItemLostInsideTheWorkshopSendsTheAccountUpForIt(t *testing.T) {
	flags := EwEntered | EwWater | EwBellows | EwFurnace
	base := map[int]int{ObjEwKey: 1, ObjEwBook: 1, ObjHammer: 1, ObjEwCoal: 4, ObjBronzePickaxe: 1, ObjShark: 6}
	for _, c := range []struct {
		name    string
		stage   int
		drop    int
		reason  string
		surface StepKind
		custom  string
	}{
		{"the hammer", flags, ObjHammer, "book or the hammer", StepProvision, ""}, {"the book", flags, ObjEwBook, "book or the hammer", StepCustom, CustomEwBook},
		{"the coal", flags, ObjEwCoal, "four coal", StepProvision, ""}, {"the pickaxe", flags, ObjBronzePickaxe, "pickaxe", StepProvision, ""},
		{"the thread", EwEntered | EwWater, ObjEwThread, "thread", StepProvision, ""},
	} {
		inv := map[int]int{ObjEwThread: 1, ObjEwNeedle: 1, ObjEwLeather: 1}
		for id, n := range base {
			inv[id] = n
		}
		delete(inv, c.drop)
		w := newEwWorld(t, c.stage, inv, nil)
		w.tile = EwArrive
		st := DecideElementalWorkshop(w.snapshot())
		if st.Kind != StepCustom || st.Custom != CustomEwLeave || !strings.Contains(st.Reason, c.reason) {
			t.Errorf("without %s the decision is %s (%+v)", c.name, st.Describe(), st)
			continue
		}
		// Up there the same item is what the kit asks for; with it held, the way back down.
		w.tile = SeersBank
		w.bankKnown = true
		up := DecideElementalWorkshop(w.snapshot())
		if up.Kind != c.surface || (c.surface == StepProvision && up.KitName != KitEwKit) || (c.custom != "" && up.Custom != c.custom) {
			t.Errorf("without %s, on the surface: %s", c.name, up.Describe())
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The earth elemental
// ---------------------------------------------------------------------------------------------------------------------

func ewFighter(w *ewWorld, att, str, def, hp int) {
	w.levels["attack"], w.levels["strength"], w.levels["defence"], w.levels["hitpoints"] = att, str, def, hp
	w.maxHP = hp
}

// The model's verdicts: the elemental (35 hitpoints, a hit of up to 4 every 6 ticks) is beaten by an account of modest levels with food and
// not by a level-10 one, and an account with no food is not sent.
func TestTheEarthElementalIsPricedOnTheAccountsLevelsAndFood(t *testing.T) {
	if FoeEarthElementalRock.MaxHit() != 4 || FoeEarthElementalRock.PeakHit() != 4 || FoeEarthElementalRock.AttackTicks != 6 {
		t.Fatalf("the elemental hits up to %d every %d ticks", FoeEarthElementalRock.MaxHit(), FoeEarthElementalRock.AttackTicks)
	}
	if foe, ok := FoeByNPC(NpcEwEarthRock); !ok || foe != FoeEarthElementalRock {
		t.Errorf("the shared fight step prices npc %d as %+v (%v)", NpcEwEarthRock, foe, ok)
	}
	for _, c := range []struct {
		name              string
		att, str, def, hp int
		sharks            int
		want              bool
		mustSay           string
	}{
		{"strong, six sharks", 50, 50, 40, 50, 6, true, ""},
		{"mid, six sharks", 30, 30, 20, 30, 6, true, ""},
		{"low but fed", 25, 25, 15, 28, 10, true, ""},
		{"level ten", 10, 10, 5, 20, 6, false, "the model has the account lose to Earth elemental"},
		{"mid with no food", 30, 30, 20, 30, 0, false, "no food in the pack"},
	} {
		w := newEwWorld(t, EwSlashed, map[int]int{ObjShark: c.sharks}, nil)
		ewFighter(w, c.att, c.str, c.def, c.hp)
		v := EwElementalReady(w.snapshot())
		if v.OK != c.want || (!v.OK && !strings.Contains(v.Why, c.mustSay)) {
			t.Errorf("%s: ready %v (%s), want %v", c.name, v.OK, v.Why, c.want)
		}
	}
	// An account whose combat levels have not been read is not judged (it is held by the record's skill gate and the first look).
	w := newEwWorld(t, EwSlashed, map[int]int{ObjShark: 6}, nil)
	s := w.snapshot()
	s.MaxHP = 0
	if v := EwElementalReady(s); !v.OK {
		t.Errorf("an unread account is judged: %s", v.Why)
	}
}

// A weak account is not sent down: the quest parks for the fight's six hours with the model's reason before it enters, and again if it is
// inside and the pack no longer covers the fight.
func TestAWeakAccountIsParkedBeforeTheDescent(t *testing.T) {
	inv := map[int]int{ObjEwKey: 1, ObjEwBook: 1, ObjHammer: 1, ObjEwCoal: 4, ObjBronzePickaxe: 1, ObjShark: 6, ObjEwThread: 1, ObjEwNeedle: 1, ObjEwLeather: 1}
	w := newEwWorld(t, EwSlashed, inv, nil)
	ewFighter(w, 10, 10, 5, 20)
	why, park := w.run()
	if park != EwFightParkFor || !strings.Contains(why, "not going down to the rock's earth elemental") || len(w.log) != 0 {
		t.Fatalf("parked %s with %q after %v", park, why, w.log)
	}
	// Inside, with the machinery done and the ore still to fight for, the same verdict sends it up.
	w = newEwWorld(t, EwEntered|EwWater|EwBellows|EwFurnace, inv, nil)
	ewFighter(w, 10, 10, 5, 20)
	w.tile = EwArrive
	st := DecideElementalWorkshop(w.snapshot())
	if st.Kind != StepCustom || st.Custom != CustomEwLeave || !strings.Contains(st.Reason, "cannot take the earth elemental") {
		t.Errorf("inside, a weak account is met with %s", st.Describe())
	}
	// With the ore already in the pack the fight is behind it: no gate.
	inv[ObjEwOre] = 1
	w = newEwWorld(t, EwEntered|EwWater|EwBellows|EwFurnace, inv, nil)
	ewFighter(w, 10, 10, 5, 20)
	w.tile = EwArrive
	if st := DecideElementalWorkshop(w.snapshot()); st.Custom != CustomEwSmelt {
		t.Errorf("a weak account holding the ore is met with %s", st.Describe())
	}
}

// The skills are the content's own: under 20 in any of the three parks the quest (a day's worth of reasons, said) and, inside, brings
// the account up first.
func TestTheWorkshopWantsMiningSmithingAndCrafting20(t *testing.T) {
	for _, skill := range []string{"mining", "smithing", "crafting"} {
		w := newEwWorld(t, EwSlashed, map[int]int{ObjEwKey: 1, ObjEwBook: 1}, nil)
		w.levels[skill] = 19
		why, park := w.run()
		if park != EwFightParkFor || !strings.Contains(why, strings.ToUpper(skill[:1])+skill[1:]+" 19/20") || len(w.log) != 0 {
			t.Errorf("%s 19: parked %s with %q after %v", skill, park, why, w.log)
		}
		w = newEwWorld(t, EwNotStarted, nil, nil)
		w.levels[skill] = 19
		if why, park := w.run(); park != EwFightParkFor || len(w.log) != 0 {
			t.Errorf("%s 19, not started: %q %s %v", skill, why, park, w.log)
		}
	}
	// An unread level is not judged.
	w := newEwWorld(t, EwNotStarted, nil, nil)
	delete(w.levels, "mining")
	if st := DecideElementalWorkshop(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomEwBook {
		t.Errorf("an unread Mining level is met with %s", st.Describe())
	}
	// The record states all three, the ladder row advises them.
	req := ElementalWorkshopRecord.Requirements
	if len(req.Skills) != 3 {
		t.Fatalf("the record asks %+v", req)
	}
	for _, r := range req.Skills {
		if r.Level != EwSkill {
			t.Errorf("%s %d", r.Skill, r.Level)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The kit and the module
// ---------------------------------------------------------------------------------------------------------------------

func ewLine(kit []KitLine, id int) (KitLine, bool) {
	for _, l := range kit {
		if l.ID == id {
			return l, true
		}
	}
	return KitLine{}, false
}

func TestTheKitIsWhatTheRestOfTheWorkNeeds(t *testing.T) {
	// At the start of the workshop: the hammer, a pickaxe, the coal, the thread (the needle and the leather optional) and the food.
	w := newEwWorld(t, EwSlashed, map[int]int{ObjEwKey: 1, ObjEwBook: 1}, nil)
	s := w.snapshot()
	kit := ewKitLines(s)
	for _, id := range []int{ObjHammer, ObjBronzePickaxe, ObjEwCoal, ObjEwThread, ObjEwNeedle, ObjEwLeather, ObjShark} {
		if _, ok := ewLine(kit, id); !ok {
			t.Errorf("the kit lacks obj %d: %+v", id, kit)
		}
	}
	for id, optional := range map[int]bool{ObjHammer: false, ObjBronzePickaxe: false, ObjEwCoal: false, ObjEwThread: false, ObjShark: false, ObjEwNeedle: true, ObjEwLeather: true} {
		if l, _ := ewLine(kit, id); l.Optional != optional {
			t.Errorf("obj %d optional %v, want %v", id, l.Optional, optional)
		}
	}
	for _, l := range kit {
		if why := ValidateKitLine(l); why != "" {
			t.Errorf("%s: %s", l.Name, why)
		}
	}
	// The shops: the hammer and the pickaxe at Arhein's, the thread at Dommik's; the coal and the food have no shop.
	if l, _ := ewLine(kit, ObjHammer); len(l.Sources) != 1 || l.Sources[0].Shop.Npc != "Arhein" {
		t.Errorf("the hammer's sources %+v", l.Sources)
	}
	if l, _ := ewLine(kit, ObjEwThread); len(l.Sources) != 1 || l.Sources[0].Shop.Npc != "Dommik" || !l.Stackable {
		t.Errorf("the thread's sources %+v", l.Sources)
	}
	for _, id := range []int{ObjEwCoal, ObjShark} {
		if l, _ := ewLine(kit, id); len(l.Sources) != 0 {
			t.Errorf("obj %d has sources %+v: only the bank and the mule give it", id, l.Sources)
		}
	}
	// With everything held there is no kit; the lines go as the work does.
	w = newEwWorld(t, EwEntered|EwWater|EwBellows|EwFurnace|EwBar, map[int]int{ObjHammer: 1, ObjEwBar: 1, ObjEwBook: 1}, nil)
	if kit := ewKitLines(w.snapshot()); len(kit) != 0 {
		t.Errorf("the bar is made: the kit is %+v", kit)
	}
	w = newEwWorld(t, EwEntered|EwWater|EwBellows|EwFurnace, map[int]int{ObjHammer: 1, ObjEwOre: 1}, nil)
	kit = ewKitLines(w.snapshot())
	if _, ok := ewLine(kit, ObjEwCoal); !ok || len(kit) != 1 {
		t.Errorf("the ore is held: the kit is %+v, want the coal alone", kit)
	}
	// A pickaxe the account can use (pack or worn) needs no line; one it cannot is no pickaxe (a mithril pickaxe wants Mining 21).
	w = newEwWorld(t, EwSlashed, map[int]int{ObjEwKey: 1, 1273: 1}, nil)
	w.levels["mining"] = 20
	if _, ok := ewLine(ewKitLines(w.snapshot()), ObjBronzePickaxe); !ok {
		t.Error("a mithril pickaxe at Mining 20 was taken for a pickaxe")
	}
	w.levels["mining"] = 21
	if _, ok := ewLine(ewKitLines(w.snapshot()), ObjBronzePickaxe); ok {
		t.Error("a mithril pickaxe at Mining 21 was not taken for a pickaxe")
	}
	// A banked pickaxe is drawn in preference to a shop's.
	w = newEwWorld(t, EwSlashed, map[int]int{ObjEwKey: 1}, map[int]int{1269: 1})
	w.bankKnown = true
	if l, ok := ewLine(ewKitLines(w.snapshot()), 1269); !ok || len(l.Sources) != 0 {
		t.Errorf("the banked steel pickaxe: %+v %v", l, ok)
	}
	// Food the account already carries (cake bites) replaces the shark line.
	w = newEwWorld(t, EwSlashed, map[int]int{ObjEwKey: 1, 1891: 2}, nil)
	if _, ok := ewLine(ewKitLines(w.snapshot()), ObjShark); ok {
		t.Error("two cakes (six bites) are food enough: the sharks are not asked for")
	}
}

func TestTheElementalWorkshopModuleIsWhatTheEngineNeeds(t *testing.T) {
	m := ElementalWorkshop
	if m.Record.ID != "elemental_workshop" || m.Record.Name != "Elemental Workshop" || m.Record.QuestPoints != 1 {
		t.Errorf("record %+v", m.Record)
	}
	if m.Decide == nil || m.ReadStage == nil || m.NoRetaliate || m.MaxDeaths != 2 || !m.BankSet || m.Bank != SeersBank {
		t.Errorf("module: it fights once, parks after two deaths and reads its stage from the journal")
	}
	if c, ok := m.StepDeathCaps[CustomEwOre]; !ok || c.Max != 1 || c.Window != 24*time.Hour || c.Skip != 24*time.Hour {
		t.Errorf("the rock's death cap is %+v", c)
	}
	if !slices.Contains(m.Sustain.FoodIDs, ObjShark) || m.Sustain.EatAt <= 0 {
		t.Errorf("sustain %+v", m.Sustain)
	}
	for _, id := range []int{ObjEwBook, ObjEwKey, ObjEwBowl, ObjEwBowlFull, ObjEwOre, ObjEwBar, ObjHammer, ObjEwCoal, ObjEwThread, ObjEwNeedle, ObjEwLeather, ObjKnife, 1265, 1275} {
		if !slices.Contains(m.ToolIDs, id) || !slices.Contains(EwKeepIDs(), id) {
			t.Errorf("obj %d is not among the module's tools and the keep list", id)
		}
	}
	// The keep list carries the food (a deposit must not bank the fight's sharks), the tool list does not.
	if !slices.Contains(EwKeepIDs(), ObjShark) || slices.Contains(EwToolIDs(), ObjShark) {
		t.Error("the sharks belong in the keep list and not in the tools")
	}
	// The regions.
	for _, c := range []struct {
		at   tile.Tile
		want EwRegion
	}{
		{SeersBank, EwSurface}, {EwBookcaseAt, EwSurface}, {EwWallOutside, EwSurface}, {EwWallInside, EwSecretRoom}, {EwSecretArrive, EwSecretRoom}, {EwStairsDownStand, EwSecretRoom},
		{EwArrive, EwWorkshop}, {EwRockAt, EwWorkshop}, {EwValveFirstAt, EwWorkshop}, {EwBellowsAt, EwWorkshop}, {EwFurnaceAt, EwWorkshop}, {EwWorkbenchAt, EwWorkshop},
		// The house above the workshop and the village north of the smithy are the surface.
		{tile.Tile{X: 2712, Z: 3499}, EwSurface}, {tile.Tile{X: 2716, Z: 3481, Level: 1}, EwSurface},
	} {
		if got := EwRegionOf(c.at); got != c.want {
			t.Errorf("%v is region %d, want %d", c.at, got, c.want)
		}
	}
	if !InEwPocket(EwArrive) || !InEwPocket(EwWallInside) || InEwPocket(EwWallOutside) {
		t.Error("the pocket is not the workshop and the secret room")
	}
	// The preconditions the decisions wait on.
	s := newEwWorld(t, EwSlashed, nil, nil).snapshot()
	s.Journal = StatusUnknown
	if st := DecideElementalWorkshop(s); st.Kind != StepWait {
		t.Errorf("an uncoloured tab: %s", st.Describe())
	}
	s = newEwWorld(t, EwSlashed, nil, nil).snapshot()
	s.StageKnown = false
	if st := DecideElementalWorkshop(s); st.Kind != StepWait {
		t.Errorf("an unread stage: %s", st.Describe())
	}
	s = newEwWorld(t, EwSlashed, nil, nil).snapshot()
	s.TileKnown = false
	if st := DecideElementalWorkshop(s); st.Kind != StepWait {
		t.Errorf("an unread position: %s", st.Describe())
	}
	s = newEwWorld(t, EwNotStarted, nil, nil).snapshot()
	s.Journal = StatusInProgress
	if st := DecideElementalWorkshop(s); st.Kind != StepWait || !strings.Contains(st.Reason, "disagree") {
		t.Errorf("a yellow tab on a stage-0 journal: %s", st.Describe())
	}
	s = newEwWorld(t, EwSlashed, nil, nil).snapshot()
	s.Journal = StatusComplete
	if st := DecideElementalWorkshop(s); st.Kind != StepDone {
		t.Errorf("a green tab: %s", st.Describe())
	}
	// A full pack banks before the bookcase gives its book.
	full := map[int]int{}
	for i := 0; i < 28; i++ {
		full[10000+i] = 1
	}
	w := newEwWorld(t, EwNotStarted, full, nil)
	if st := DecideElementalWorkshop(w.snapshot()); st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, ObjEwBook) || st.Bank != SeersBank {
		t.Errorf("a full pack is met with %s", st.Describe())
	}
	// The ore's wait: the shield is made and the quest scroll is awaited.
	w = newEwWorld(t, EwEntered|EwWater|EwBellows|EwFurnace, map[int]int{ObjEwShield: 1}, nil)
	w.tile = EwArrive
	if st := DecideElementalWorkshop(w.snapshot()); st.Kind != StepWait || !strings.Contains(st.Reason, "quest scroll") {
		t.Errorf("a shield in the pack: %s", st.Describe())
	}
}
