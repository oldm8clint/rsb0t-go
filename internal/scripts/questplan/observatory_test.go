package questplan

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/tile"
)

// The Observatory Quest (W13, 18 Oct 2026): the journal parser over the pages the content renders, every state of Decide, the kits
// through the real planner, the goblin guard's odds, and whole simulated quests (a world that answers every step the way the content's
// scripts do).

// ---------------------------------------------------------------------------------------------------------------------
// The journal
// ---------------------------------------------------------------------------------------------------------------------

// obsJournalPages are the pages itgronigen_journal.rs2 renders, every string of a branch joined in order (the journal is
// `~quest_journal(title, $text)` over exactly that text, `|` the client's line break).
const (
	obsHead = "@str@I can start this quest by talking to the professor, in the|@str@Observatory reception, south-west of Ardougne.|"
	obsAsk  = "@dbl@Seems the observatory telescope needs repairing, due to the nearby goblins. The @dre@professor@dbl@ wants me to help by getting the following, with the help of his @dre@assistant:||"
	obsAsk4 = "@dbl@Seems the observatory telescope needs repairing due to the nearby goblins. The @dre@professor@dbl@ wants me to help by getting the following, with the help of his @dre@assistant:||"
	obsAll  = "@str@Seems the observatory telescope needs repairing, due to|@str@the nearby goblins. The professor wants me to help by|@str@getting the following, with the help of his assistant:||" +
		"@str@3 wooden planks|@str@1 bronze bar|@str@1 molten glass|@str@1 lens mould|"
	obsLens = "@str@The professor was pleased to have all the pieces needed|@str@to fix the telescope. Apparently, the professor's last|" +
		"@str@attempt at Crafting ended in disaster. So, he wants me to|@str@create the lens by using the molten glass with the mould.|@str@Fine by me!|"
)

var obsJournalFixtures = map[int]string{
	ObsNotStarted:  "@dbl@I can start this quest by talking to the @dre@professor@dbl@, in the @dre@Observatory reception, south-west of Ardougne.",
	ObsStarted:     obsHead + obsAsk + "@dre@3 wooden planks|",
	ObsGivenPlanks: obsHead + obsAsk + "@str@3 wooden planks|" + "@dre@1 bronze bar|",
	ObsGivenBronze: obsHead + obsAsk + "@str@3 wooden planks|@str@1 bronze bar|" + "@dre@1 molten glass|",
	ObsGivenGlass:  obsHead + obsAsk4 + "@str@3 wooden planks|@str@1 bronze bar|@str@1 molten glass|" + "@dre@1 lens mould|",
	ObsGivenMould: obsHead + obsAll + "@dbl@The @dre@professor@dbl@ was pleased to have all the pieces needed to fix the telescope. Apparently, the professor's last attempt at Crafting ended in disaster. So, he wants me to create the @dre@lens@dbl@ by using the @dre@molten glass@dbl@ with the @dre@mould.|" +
		"Fine by me!|",
	ObsSentTelescope: obsHead + obsAll + obsLens + "@dbl@The @dre@professor@dbl@ has gone ahead to the @dre@Observatory.@dbl@ He wants me to meet him there by travelling through the @dre@dungeon@dbl@ below it.|",
	ObsComplete: obsHead + obsAll + obsLens + "@str@The professor has eagerly gone ahead to the Observatory.|@str@He wants me to meet him there by travelling through the|" +
		"@str@dungeon below it. I hope I get to look through the|@str@telescope!|@dre@QUEST COMPLETE!|@dbl@I should probably see what the @dre@assistant@dbl@ thinks of all this.|He should be pleased.",
}

func TestParseObservatoryJournalReadsEveryRenderedStage(t *testing.T) {
	for stage, page := range obsJournalFixtures {
		for _, lines := range [][]string{{page}, strings.Split(page, "|")} {
			got, ok := ParseObservatoryJournal(lines)
			if !ok || got != stage {
				t.Errorf("stage %d page %.60q: parsed %d %v", stage, page, got, ok)
			}
		}
	}
	// The wine page (%itgronigen 8, after the assistant's gift) reads complete too: it carries QUEST COMPLETE! and the struck wine lines.
	wine := obsJournalFixtures[ObsComplete][:len(obsJournalFixtures[ObsComplete])-len("@dbl@I should probably see what the @dre@assistant@dbl@ thinks of all this.|He should be pleased.")] +
		"@str@I should probably see what the assistant thinks of all this.|@str@He should be pleased.|@str@I had a word with the professor's assistant and he gave me|" +
		"@str@some wine! What a pleasant chap. He also mentioned about|@str@Scorpius and a grave - most cryptic indeed!"
	if got, ok := ParseObservatoryJournal([]string{wine}); !ok || got != ObsComplete {
		t.Errorf("the wine page parsed %d %v, want complete", got, ok)
	}
	for _, lines := range [][]string{nil, {""}, {"@dbl@Something else entirely"}} {
		if got, ok := ParseObservatoryJournal(lines); ok {
			t.Errorf("%q parsed as %d: an unrecognised journal is unknown, never stage 0", lines, got)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The simulated world
// ---------------------------------------------------------------------------------------------------------------------

// obsWorld is the Observatory Quest as the content runs it: %itgronigen, the professor's answers to each hand-over and what each
// custom step and use-on does.
type obsWorld struct {
	*kcWorld
	stage   int
	levels  map[string]int
	blocked map[string]bool
	// mouldTrips counts the dungeon trips, telescope whether the last custom was the telescope.
	mouldTrips int
	// underground puts the account in the cavern (a half-finished trip).
	underground bool
}

func newObsWorld(t *testing.T, stage int, inv, bank map[int]int) *obsWorld {
	w := &obsWorld{kcWorld: newKCWorld(t, inv, bank), stage: stage, blocked: map[string]bool{},
		levels: map[string]int{"attack": 45, "strength": 45, "defence": 45, "hitpoints": 55, "magic": 1}}
	w.maxHP = 55
	w.tile = EastArdougneNorth
	return w
}

func (w *obsWorld) snapshot() Snapshot {
	j := StatusInProgress
	switch w.stage {
	case ObsNotStarted:
		j = StatusNotStarted
	case ObsComplete:
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
	if w.underground {
		s.Tile = ObsCavernLanding
	}
	return s
}

func (w *obsWorld) apply(st Step) {
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
	case StepScanBank:
		w.bankKnown = true
		w.log = append(w.log, "scanbank")
	case StepWithdraw:
		for _, it := range st.Items {
			if w.bank[it.ID] >= it.Qty {
				w.bank[it.ID] -= it.Qty
				w.inv[it.ID] += it.Qty
			}
		}
		w.log = append(w.log, "withdraw")
	case StepDeposit:
		w.deposit(st)
		w.log = append(w.log, "deposit")
	case StepTalk:
		w.talk()
	case StepUseOn:
		w.useOn(st)
	case StepCustom:
		w.custom(st)
	default:
		t.Fatalf("unexpected step %s", st.Describe())
	}
}

// talk is every hand-over to the reception professor (observatory_professor.rs2).
func (w *obsWorld) talk() {
	switch w.stage {
	case ObsNotStarted:
		w.stage = ObsStarted // professor_initial, the telescope option, "Sounds interesting"
	case ObsStarted:
		if w.inv[ObjPlank] >= 3 {
			w.inv[ObjPlank] -= 3
			w.stage = ObsGivenPlanks
		}
	case ObsGivenPlanks:
		if w.inv[ObjBronzeBar] > 0 {
			w.inv[ObjBronzeBar]--
			w.stage = ObsGivenBronze
		}
	case ObsGivenBronze:
		if w.inv[ObjMoltenGlass] > 0 {
			w.inv[ObjMoltenGlass]--
			w.stage = ObsGivenGlass
		}
	case ObsGivenGlass:
		// professor_mould: the glass comes back, and only into a pack with room.
		if w.inv[ObjLensMould] > 0 && w.used() < 28 {
			w.inv[ObjMoltenGlass]++
			w.stage = ObsGivenMould
		}
	case ObsGivenMould:
		if w.inv[ObjLens] > 0 {
			w.inv[ObjLens]--
			delete(w.inv, ObjLensMould)
			w.stage = ObsSentTelescope
		}
	default:
		w.t.Fatalf("a talk with the professor at stage %d", w.stage)
	}
	w.log = append(w.log, fmt.Sprintf("professor%d", w.stage))
}

func (w *obsWorld) useOn(st Step) {
	switch {
	case st.ItemID == ObjCopperOre && st.TargetID == LocObsFurnace:
		if w.inv[ObjCopperOre] > 0 && w.inv[ObjTinOre] > 0 {
			w.inv[ObjCopperOre]--
			w.inv[ObjTinOre]--
			w.inv[ObjBronzeBar]++
		}
		w.log = append(w.log, "smelt-bronze")
	case st.ItemID == ObjBucketOfSand && st.TargetID == LocObsFurnace:
		if w.inv[ObjBucketOfSand] > 0 && w.inv[ObjSodaAsh] > 0 {
			w.inv[ObjBucketOfSand]--
			w.inv[ObjSodaAsh]--
			w.inv[ObjMoltenGlass]++
		}
		w.log = append(w.log, "smelt-glass")
	case st.ItemID == ObjLensMould && st.TargetKind == TargetItem && st.TargetID == ObjMoltenGlass:
		if w.inv[ObjMoltenGlass] > 0 && w.inv[ObjLensMould] > 0 && w.inv[ObjLens] == 0 {
			w.inv[ObjMoltenGlass]--
			w.inv[ObjLens]++
		}
		w.log = append(w.log, "lens")
	default:
		w.t.Fatalf("unexpected use-on %s", st.Describe())
	}
}

func (w *obsWorld) custom(st Step) {
	switch st.Custom {
	case CustomObsMould:
		w.mouldTrips++
		w.inv[ObjLensMould]++
		w.underground = false
		w.tile = ObsProfessorAnchor
	case CustomObsAsh:
		if w.inv[ObjSeaweed] > 0 {
			w.inv[ObjSeaweed]--
			w.inv[ObjSodaAsh]++
		}
	case CustomObsTelescope:
		w.stage = ObsComplete
	case CustomObsLeave:
		w.underground = false
		w.tile = ObsProfessorAnchor
	default:
		w.t.Fatalf("unexpected custom %q", st.Custom)
	}
	w.log = append(w.log, st.Custom)
}

func (w *obsWorld) run() (waited string, parkFor time.Duration) {
	w.t.Helper()
	for i := 0; i < 80; i++ {
		st := DecideObservatory(w.snapshot())
		switch st.Kind {
		case StepDone:
			return "", 0
		case StepWait:
			return st.Reason, st.ParkFor
		}
		before := fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.underground)
		w.apply(st)
		if fmt.Sprint(w.stage, w.inv, w.bank, w.bankKnown, w.ran, w.failed, w.underground) == before {
			w.t.Fatalf("step %d, %s, changed nothing: the module would loop (log %v)", i, st.Describe(), w.log)
		}
	}
	w.t.Fatalf("80 steps and the quest is not done (stage %d, log %v)", w.stage, w.log)
	return "", 0
}

// ---------------------------------------------------------------------------------------------------------------------
// Whole quests
// ---------------------------------------------------------------------------------------------------------------------

// A fresh account whose bank holds the parts does the whole quest in the content's order: one bundled pass first, then a hand-over per
// stage, the dungeon trip for the mould, the lens, the telescope.
func TestAFreshAccountDoesTheObservatoryInOrder(t *testing.T) {
	w := newObsWorld(t, ObsNotStarted, map[int]int{CoinsID: 100}, map[int]int{ObjPlank: 3, ObjBronzeBar: 2, ObjMoltenGlass: 1, ObjShark: 20})
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"professor1", "provision:" + KitObsBundle, "professor2", "professor3", "professor4",
		"provision:" + KitObsFood, CustomObsMould, "professor5", "lens", "professor6", CustomObsTelescope}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
	if w.mouldTrips != 1 {
		t.Errorf("%d dungeon trips", w.mouldTrips)
	}
	for _, id := range []int{ObjPlank, ObjBronzeBar, ObjMoltenGlass, ObjLensMould, ObjLens} {
		if w.inv[id] != 0 {
			t.Errorf("%d of obj %d left in the pack", w.inv[id], id)
		}
	}
}

// An account with an empty bank: the planks come off the outpost's ground, the bar is smelted from the ores the mule would give, the
// glass is made from a sand off the ground and a seaweed burnt to ash.
func TestAnEmptyBankMakesTheBarAndTheGlassAndFetchesThePlanks(t *testing.T) {
	w := newObsWorld(t, ObsStarted, map[int]int{CoinsID: 50, ObjCopperOre: 1, ObjTinOre: 1, ObjSeaweed: 1, ObjShark: 8}, nil)
	w.ground[ObjPlank] = true
	w.ground[ObjBucketOfSand] = true
	w.bankKnown = true
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	want := []string{"provision:" + KitObsBundle, "provision:" + KitObsPlanks, "professor2", "smelt-bronze", "professor3",
		"provision:" + KitObsSand, CustomObsAsh, "smelt-glass", "professor4", "provision:" + KitObsFood, CustomObsMould, "professor5", "lens", "professor6",
		CustomObsTelescope}
	if !slices.Equal(w.log, want) {
		t.Fatalf("the quest ran\n  %v\nwant\n  %v", w.log, want)
	}
}

// Each thing no source can give parks the quest with its reason for the item park, not a retry.
func TestEachMissingThingParksTheQuestWithItsReason(t *testing.T) {
	cases := []struct {
		name  string
		stage int
		inv   map[int]int
		want  string
		park  time.Duration
	}{
		{"planks", ObsStarted, nil, "no planks from the bank, the mule or the Barbarian Outpost", ObsItemParkFor},
		{"bar", ObsGivenPlanks, nil, "no bronze bar", ObsItemParkFor},
		{"sand", ObsGivenBronze, map[int]int{ObjSeaweed: 1}, "no bucket of sand", ObsItemParkFor},
		{"seaweed", ObsGivenBronze, map[int]int{ObjBucketOfSand: 1}, "no soda ash or seaweed", ObsItemParkFor},
	}
	for _, c := range cases {
		w := newObsWorld(t, c.stage, c.inv, nil)
		w.bankKnown = true
		why, park := w.run()
		if !strings.Contains(why, c.want) || park != c.park {
			t.Errorf("%s: waited %q for %s (log %v), want %q for %s", c.name, why, park, w.log, c.want, c.park)
		}
	}
}

// The mould and the key the bank holds are drawn before the dungeon: the content's sack and chest both refuse the account while the
// bank holds one (obj_gettotal counts the bank), so a trip that ignored the bank would loop on the refusal.
func TestTheBankedMouldIsDrawnAndNoTripIsMade(t *testing.T) {
	w := newObsWorld(t, ObsGivenGlass, nil, map[int]int{ObjLensMould: 1})
	if why, _ := w.run(); why != "" {
		t.Fatalf("waited: %s (log %v)", why, w.log)
	}
	if w.mouldTrips != 0 {
		t.Errorf("a dungeon trip was made with the mould in the bank (log %v)", w.log)
	}
	if w.stage != ObsComplete || w.log[0] != "provision:"+KitObsBundle {
		t.Errorf("stage %d after %v", w.stage, w.log)
	}
}

// The keep key in the bank is drawn before the descent (the chest would refuse a second); the descent is not made without it.
func TestTheBankedKeyIsDrawnBeforeTheDescent(t *testing.T) {
	w := newObsWorld(t, ObsGivenGlass, map[int]int{ObjShark: 8}, nil)
	w.bank[ObjObsKeepKey] = 1
	w.bankKnown = true
	s := w.snapshot()
	s.Ran["provision:"+KitObsBundle] = true
	st := DecideObservatory(s)
	if st.Kind != StepWithdraw || st.Items[0].ID != ObjObsKeepKey {
		t.Fatalf("with the key in the bank the quest decided %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The goblin guard
// ---------------------------------------------------------------------------------------------------------------------

// The guard's stats are the content's (attack 32, defence 37, strength 37, 43 hitpoints, attack bonus 8, strength bonus 5): a hardest
// hit of 5, from the foe table too, so the fight loops raise their eat line for him.
func TestTheGoblinGuardIsPricedFromTheContentsStats(t *testing.T) {
	if got := FoeGoblinGuard.MaxHit(); got != 5 {
		t.Errorf("the goblin guard's hardest hit is %d, want 5", got)
	}
	foe, ok := FoeByNPC(NpcGoblinGuard)
	if !ok || foe != FoeGoblinGuard {
		t.Errorf("FoeByNPC(%d) = %+v %v", NpcGoblinGuard, foe, ok)
	}
	if line := FoeEatLine(obsSustain.EatAt, FoeGoblinGuard.PeakHit(), 50); line != 14 {
		t.Errorf("the eat line against the guard at 50 hitpoints is %d, want the module's 14 (twice 5 is 10)", line)
	}
}

// obsAccount is a snapshot of an account with these levels and this many sharks.
func obsAccount(att, str, def, hp, sharks int) Snapshot {
	return Snapshot{InvIDs: map[int]int{ObjShark: sharks}, HP: hp, MaxHP: hp,
		Levels: map[string]int{"attack": att, "strength": str, "defence": def, "hitpoints": hp, "magic": 1}}
}

// The model sends an account that wins with the margin, parks one that does not, and parks one with no food whatever its levels: the
// 25-level account that the model has eating all eight sharks and leaving none for the way out is the line.
func TestTheGuardIsFoughtOnlyByAccountsTheModelSaysWin(t *testing.T) {
	for _, c := range []struct {
		name                      string
		att, str, def, hp, sharks int
		ok                        bool
		why                       string
	}{
		{"a level 30 melee account with eight sharks", 30, 30, 30, 40, 8, true, ""},
		{"a level 50 account with eight sharks", 50, 50, 50, 60, 8, true, ""},
		{"the same level 30 account with two sharks", 30, 30, 30, 40, 2, false, "lose to Goblin guard"},
		{"a level 25 account eats all eight and has none left", 25, 25, 25, 35, 8, false, "keeps none for the way out"},
		{"a level 10 account", 10, 10, 10, 20, 8, false, "lose to Goblin guard"},
		{"a level 1 account", 1, 1, 1, 10, 8, false, "lose to Goblin guard"},
		{"a level 70 account with no food", 70, 70, 70, 80, 0, false, "no food in the pack for the goblin guard"},
		{"a level 20 account with twenty sharks", 20, 20, 20, 30, 20, true, ""},
	} {
		v := ObsGuardReady(obsAccount(c.att, c.str, c.def, c.hp, c.sharks))
		if v.OK != c.ok || (c.why != "" && !strings.Contains(v.Why, c.why)) {
			t.Errorf("%s: ok %v (%q), want %v %q", c.name, v.OK, v.Why, c.ok, c.why)
		}
		if !v.OK && !strings.Contains(v.Why, "Attack") && c.sharks > 0 {
			t.Errorf("%s: the reason %q does not name the account's levels", c.name, v.Why)
		}
	}
	// An account whose combat levels are not read yet is not judged (the engine reads them before any decision a run makes).
	if v := ObsGuardReady(Snapshot{InvIDs: map[int]int{ObjShark: 8}}); !v.OK {
		t.Errorf("an account with unread levels was refused: %s", v.Why)
	}
}

// A weak account parks for the guard's six hours, from the surface; underground it is walked out first. A strong one goes down, and
// one already in the keep goes on without the odds (the guard is behind it).
func TestTheMouldTripIsGatedOnTheOddsFromTheSurfaceAndWalkedOutFromBelow(t *testing.T) {
	weak := newObsWorld(t, ObsGivenGlass, map[int]int{ObjShark: 8}, nil)
	weak.levels = map[string]int{"attack": 10, "strength": 10, "defence": 10, "hitpoints": 20}
	weak.maxHP = 20
	weak.bankKnown = true
	weak.ran["provision:"+KitObsBundle] = true
	weak.ran["provision:"+KitObsFood] = true
	st := DecideObservatory(weak.snapshot())
	if st.Kind != StepWait || st.ParkFor != ObsGuardParkFor || !strings.Contains(st.Reason, "not going down to the goblin guard") {
		t.Fatalf("a weak account decided %s (park %s)", st.Describe(), st.ParkFor)
	}
	weak.underground = true
	st = DecideObservatory(weak.snapshot())
	if st.Kind != StepCustom || st.Custom != CustomObsLeave {
		t.Fatalf("a weak account below decided %s", st.Describe())
	}
	strong := newObsWorld(t, ObsGivenGlass, map[int]int{ObjShark: 8}, nil)
	strong.bankKnown = true
	strong.ran["provision:"+KitObsBundle] = true
	strong.ran["provision:"+KitObsFood] = true
	if st := DecideObservatory(strong.snapshot()); st.Kind != StepCustom || st.Custom != CustomObsMould {
		t.Fatalf("a strong account decided %s", st.Describe())
	}
	// In the keep: no odds, whatever the levels.
	weak.tile = ObsSackStand
	weak.underground = false
	if st := DecideObservatory(weak.snapshot()); st.Kind != StepCustom || st.Custom != CustomObsMould {
		t.Fatalf("an account in the keep decided %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// Every state of Decide
// ---------------------------------------------------------------------------------------------------------------------

func TestObservatoryDecideOutsideTheJournalStages(t *testing.T) {
	w := newObsWorld(t, ObsNotStarted, nil, nil)
	if st := DecideObservatory(w.snapshot()); st.Kind != StepTalk || st.Stop.ID != NpcObsProfessor {
		t.Errorf("not started decided %s", st.Describe())
	}
	s := w.snapshot()
	s.Journal = StatusUnknown
	if st := DecideObservatory(s); st.Kind != StepWait || strings.Contains(st.Reason, "start") {
		t.Errorf("an uncoloured tab decided %s: it is not a quest to start", st.Describe())
	}
	w.stage = ObsComplete
	if st := DecideObservatory(w.snapshot()); st.Kind != StepDone {
		t.Errorf("complete decided %s", st.Describe())
	}
	// In progress with no stage read, or a stage the tab contradicts.
	s = newObsWorld(t, ObsStarted, nil, nil).snapshot()
	s.StageKnown = false
	if st := DecideObservatory(s); st.Kind != StepWait {
		t.Errorf("an unread stage decided %s", st.Describe())
	}
	s = newObsWorld(t, ObsNotStarted, nil, nil).snapshot()
	s.Journal = StatusInProgress
	if st := DecideObservatory(s); st.Kind != StepWait || !strings.Contains(st.Reason, "disagree") {
		t.Errorf("a started tab over stage 0 decided %s", st.Describe())
	}
}

func TestObservatoryDecideEveryHandOver(t *testing.T) {
	ran := map[string]bool{"provision:" + KitObsBundle: true}
	for _, c := range []struct {
		name  string
		stage int
		inv   map[int]int
		bank  map[int]int
		want  func(Step) bool
	}{
		{"three planks held", ObsStarted, map[int]int{ObjPlank: 3}, nil, func(s Step) bool { return s.Kind == StepTalk }},
		{"two planks held", ObsStarted, map[int]int{ObjPlank: 2}, nil, func(s Step) bool { return s.Kind == StepProvision && s.KitName == KitObsPlanks }},
		{"the bar held", ObsGivenPlanks, map[int]int{ObjBronzeBar: 1}, nil, func(s Step) bool { return s.Kind == StepTalk }},
		{"both ores held", ObsGivenPlanks, map[int]int{ObjCopperOre: 1, ObjTinOre: 1}, nil, func(s Step) bool {
			return s.Kind == StepUseOn && s.ItemID == ObjCopperOre && s.TargetID == LocObsFurnace && s.TargetKind == TargetLoc
		}},
		{"one ore held", ObsGivenPlanks, map[int]int{ObjCopperOre: 1}, nil, func(s Step) bool { return s.Kind == StepWait && s.ParkFor == ObsItemParkFor }},
		{"the glass held", ObsGivenBronze, map[int]int{ObjMoltenGlass: 1}, nil, func(s Step) bool { return s.Kind == StepTalk }},
		{"sand and ash held", ObsGivenBronze, map[int]int{ObjBucketOfSand: 1, ObjSodaAsh: 1}, nil, func(s Step) bool {
			return s.Kind == StepUseOn && s.ItemID == ObjBucketOfSand && s.TargetID == LocObsFurnace
		}},
		{"sand and seaweed held", ObsGivenBronze, map[int]int{ObjBucketOfSand: 1, ObjSeaweed: 1}, nil, func(s Step) bool {
			return s.Kind == StepCustom && s.Custom == CustomObsAsh
		}},
		{"no sand", ObsGivenBronze, map[int]int{ObjSeaweed: 1}, nil, func(s Step) bool { return s.Kind == StepProvision && s.KitName == KitObsSand }},
		{"the lens held", ObsGivenMould, map[int]int{ObjLens: 1}, nil, func(s Step) bool { return s.Kind == StepTalk }},
		{"the mould and the glass held", ObsGivenMould, map[int]int{ObjLensMould: 1, ObjMoltenGlass: 1}, nil, func(s Step) bool {
			return s.Kind == StepUseOn && s.ItemID == ObjLensMould && s.TargetKind == TargetItem && s.TargetID == ObjMoltenGlass
		}},
	} {
		w := newObsWorld(t, c.stage, c.inv, c.bank)
		w.bankKnown = true
		for k, v := range ran {
			w.ran[k] = v
		}
		if st := DecideObservatory(w.snapshot()); !c.want(st) {
			t.Errorf("%s: decided %s", c.name, st.Describe())
		}
	}
}

// The professor refuses the glass he hands back into a full pack, so a full pack is banked first, keeping the quest's items.
func TestAFullPackIsBankedBeforeTheMouldIsHandedIn(t *testing.T) {
	inv := map[int]int{ObjLensMould: 1}
	for i := 0; i < 27; i++ {
		inv[2000+i] = 1
	}
	w := newObsWorld(t, ObsGivenGlass, inv, nil)
	w.bankKnown = true
	w.ran["provision:"+KitObsBundle] = true
	st := DecideObservatory(w.snapshot())
	if st.Kind != StepDeposit || !slices.Contains(st.KeepIDs, ObjLensMould) {
		t.Fatalf("a full pack decided %s (keep %v)", st.Describe(), st.KeepIDs)
	}
	w.apply(st)
	if st := DecideObservatory(w.snapshot()); st.Kind != StepTalk {
		t.Fatalf("after the deposit the quest decided %s", st.Describe())
	}
	// The mould in hand and the account still underground: it climbs out before it talks.
	w = newObsWorld(t, ObsGivenGlass, map[int]int{ObjLensMould: 1}, nil)
	w.underground = true
	w.bankKnown = true
	w.ran["provision:"+KitObsBundle] = true
	if st := DecideObservatory(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomObsLeave {
		t.Fatalf("underground with the mould decided %s", st.Describe())
	}
}

// Stage 6 wants two free slots for the sapphire and the constellation's gift.
func TestTheTelescopeWantsRoomForTheRewards(t *testing.T) {
	inv := map[int]int{}
	for i := 0; i < 27; i++ {
		inv[2000+i] = 1
	}
	w := newObsWorld(t, ObsSentTelescope, inv, nil)
	w.bankKnown = true
	if st := DecideObservatory(w.snapshot()); st.Kind != StepDeposit {
		t.Fatalf("a pack with one free slot decided %s", st.Describe())
	}
	w = newObsWorld(t, ObsSentTelescope, nil, nil)
	w.bankKnown = true
	if st := DecideObservatory(w.snapshot()); st.Kind != StepCustom || st.Custom != CustomObsTelescope {
		t.Fatalf("an empty pack decided %s", st.Describe())
	}
}

// ---------------------------------------------------------------------------------------------------------------------
// The kits
// ---------------------------------------------------------------------------------------------------------------------

// The bundle is optional lines with no source of their own (a source on an optional line is worked, which would make the bundle a trip),
// asks the mule for nothing untradeable, and names exactly what the stage still needs.
func TestTheBundleIsOptionalWithNoSourcesAndAsksTheMuleForNothingUntradeable(t *testing.T) {
	for stage := ObsStarted; stage <= ObsGivenMould; stage++ {
		w := newObsWorld(t, stage, nil, nil)
		kit := obsBundle(w.snapshot())
		if len(kit) == 0 {
			t.Errorf("stage %d: an empty bundle", stage)
		}
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("stage %d: %s", stage, why)
			}
			if !l.Optional || len(l.Sources) != 0 {
				t.Errorf("stage %d: %s is not an optional line without sources", stage, l.Name)
			}
			if (l.ID == ObjLensMould || l.ID == ObjLens || l.ID == ObjObsKeepKey) != l.Untradeable {
				t.Errorf("stage %d: %s untradeable %v", stage, l.Name, l.Untradeable)
			}
		}
	}
	has := func(kit []KitLine, id int) bool {
		for _, l := range kit {
			if l.ID == id {
				return true
			}
		}
		return false
	}
	at1 := obsBundle(newObsWorld(t, ObsStarted, nil, nil).snapshot())
	for _, id := range []int{ObjPlank, ObjBronzeBar, ObjCopperOre, ObjTinOre, ObjMoltenGlass, ObjBucketOfSand, ObjSodaAsh, ObjSeaweed, ObjLensMould} {
		if !has(at1, id) {
			t.Errorf("the bundle at stage 1 lacks obj %d", id)
		}
	}
	at4 := obsBundle(newObsWorld(t, ObsGivenGlass, nil, nil).snapshot())
	if has(at4, ObjPlank) || has(at4, ObjBronzeBar) || has(at4, ObjMoltenGlass) || !has(at4, ObjLensMould) || !has(at4, ObjObsKeepKey) {
		t.Errorf("the bundle at stage 4: %+v", at4)
	}
	// Food is not in the bundle: it is asked for at the dungeon, not carried through the planks.
	if has(at1, ObjShark) || has(at4, ObjShark) {
		t.Error("the bundle carries sharks")
	}
	// The ores serve the bar and the sand, ash and seaweed the glass: once the bank has given the bar and the glass, the mule is asked for
	// none of them (the provisioner draws the bank first and plans again with the pack it has).
	w := newObsWorld(t, ObsStarted, nil, map[int]int{ObjBronzeBar: 1, ObjMoltenGlass: 1})
	w.bankKnown = true
	bundle := obsBundle(w.snapshot())
	mule := map[int]int{ObjCopperOre: 5000, ObjTinOre: 5000, ObjBucketOfSand: 2, ObjSodaAsh: 3, ObjSeaweed: 600000}
	plan := PlanProvision(bundle, ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: w.bank, BankKnown: true, FreeSlots: 28,
		Levels: map[string]int{}, Exhausted: map[string]bool{}})
	if len(plan.Withdraw) != 2 || plan.Phase != PhaseBank {
		t.Errorf("the first pass is %s", plan.Summary())
	}
	plan = PlanProvision(bundle, ProvisionState{Pack: map[int]int{ObjBronzeBar: 1, ObjMoltenGlass: 1}, Worn: map[int]int{}, Bank: map[int]int{}, BankKnown: true,
		Mule: mule, MuleKnown: true, FreeSlots: 26, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	for _, c := range plan.MuleAsk {
		if c.ID == ObjCopperOre || c.ID == ObjTinOre || c.ID == ObjBucketOfSand || c.ID == ObjSodaAsh || c.ID == ObjSeaweed {
			t.Errorf("the mule is asked for obj %d with the bar and the glass in the pack", c.ID)
		}
	}
	// The mule is asked once for the bar, the ores' stand-in, the seaweed and the sand, and never for the mould or the key.
	plan = PlanProvision(obsBundle(newObsWorld(t, ObsStarted, nil, nil).snapshot()), ProvisionState{Pack: map[int]int{}, Worn: map[int]int{}, Bank: map[int]int{},
		BankKnown: true, Mule: map[int]int{ObjBronzeBar: 2000, ObjSeaweed: 600000, ObjBucketOfSand: 2, ObjLensMould: 5, ObjObsKeepKey: 5}, MuleKnown: true,
		FreeSlots: 28, Levels: map[string]int{}, Exhausted: map[string]bool{}})
	asked := map[int]int{}
	for _, c := range plan.MuleAsk {
		asked[c.ID] = c.Count
	}
	if asked[ObjBronzeBar] != 1 || asked[ObjSeaweed] != 1 || asked[ObjBucketOfSand] != 1 || asked[ObjLensMould] != 0 || asked[ObjObsKeepKey] != 0 {
		t.Errorf("the mule ask is %v", asked)
	}
}

// The three required kits are valid and carry their sources: four outpost spawns for the planks, the goblin huts' spawn for the sand.
func TestTheRequiredKitsCarryTheirGroundSources(t *testing.T) {
	for name, kit := range map[string][]KitLine{KitObsPlanks: obsPlankKit(), KitObsSand: obsSandKit(), KitObsFood: obsFoodKit()} {
		for _, l := range kit {
			if why := ValidateKitLine(l); why != "" {
				t.Errorf("%s: %s", name, why)
			}
		}
	}
	planks := obsPlankKit()[0]
	if planks.Qty != ObsPlanksNeeded || len(planks.Sources) != 2 {
		t.Errorf("the plank kit: %+v", planks)
	}
	for i, s := range planks.Sources {
		if s.Kind != SourceGround {
			t.Errorf("plank source %d: %+v", i, s)
		}
	}
	// The ground-source runner takes what lies within a tile of a source: the middle tile covers three of the four spawns, and the fourth
	// is the second source.
	covered := 0
	for _, sp := range ObsPlankSpawns {
		if tile.Chebyshev(sp, planks.Sources[0].Tile) <= 1 {
			covered++
		}
	}
	if planks.Sources[0].Tile != ObsPlankCentre || covered != 3 || planks.Sources[1].Tile != ObsPlankSpawns[3] || tile.Chebyshev(ObsPlankSpawns[3], ObsPlankCentre) <= 1 {
		t.Errorf("the middle tile %v covers %d spawns, the second source is %v", planks.Sources[0].Tile, covered, planks.Sources[1].Tile)
	}
	sand := obsSandKit()[0]
	if len(sand.Sources) != 1 || sand.Sources[0].Tile != ObsSandSpawn {
		t.Errorf("the sand kit: %+v", sand)
	}
	if food := obsFoodKit()[0]; !food.Optional || len(food.Sources) != 0 || food.ID != ObjShark || food.Qty != 8 {
		t.Errorf("the food kit: %+v", food)
	}
}

// The module's keep list protects every item of the quest through a bank visit and the food it eats.
func TestTheObservatoryKeepsItsOwnItems(t *testing.T) {
	keep := ObsKeepIDs()
	for _, id := range []int{ObjPlank, ObjBronzeBar, ObjMoltenGlass, ObjLensMould, ObjLens, ObjObsKeepKey, ObjSeaweed, ObjSodaAsh, ObjBucketOfSand, ObjCopperOre, ObjTinOre, CoinsID, ObjShark} {
		if !slices.Contains(keep, id) {
			t.Errorf("obj %d is not in the keep list", id)
		}
	}
	keep[0] = -1
	if ObsKeepIDs()[0] == -1 {
		t.Error("ObsKeepIDs returned the live slice")
	}
	// The food is kept through a bank step but is no tool: the floor bite never eats a tool, and the sharks are what it should eat.
	if !slices.Equal(Observatory.ToolIDs, ObsToolIDs()) {
		t.Error("the module's tools are not the tool list")
	}
	for _, id := range obsFoodIDs {
		if slices.Contains(Observatory.ToolIDs, id) {
			t.Errorf("food %d is a tool: the floor bite would leave it", id)
		}
		if !slices.Contains(ObsKeepIDs(), id) {
			t.Errorf("food %d is not kept through a bank step", id)
		}
	}
	for _, id := range ObsToolIDs() {
		if !slices.Contains(ObsKeepIDs(), id) {
			t.Errorf("tool %d is not kept through a bank step", id)
		}
	}
}

// The fleet slot is given up once the lens is made: the planks, the sand, the guard, the key and the sack are all behind the account.
func TestTheObservatorySlotIsGivenUpFromTheLensOn(t *testing.T) {
	for stage := ObsStarted; stage <= ObsSentTelescope; stage++ {
		s := newObsWorld(t, stage, nil, nil).snapshot()
		if got, want := Observatory.ExclusiveDone(s), stage >= ObsGivenMould; got != want {
			t.Errorf("stage %d: past the crowded steps %v, want %v", stage, got, want)
		}
	}
	if s := newObsWorld(t, ObsComplete, nil, nil).snapshot(); !Observatory.ExclusiveDone(s) {
		t.Error("a complete quest holds its slot")
	}
	if s := (Snapshot{Journal: StatusInProgress}); Observatory.ExclusiveDone(s) {
		t.Error("an unread stage gave the slot up")
	}
}

// The module's regions are the ones the content has them: the keep is the column behind the gate, the dome the walled pocket.
func TestTheObservatoryRegions(t *testing.T) {
	for _, c := range []struct {
		at                 tile.Tile
		keep, dome, cavern bool
	}{
		{ObsSackStand, true, false, true}, {ObsGateSouth, true, false, true}, {ObsGateNorth, false, false, true},
		{ObsCavernLanding, false, false, true}, {ObsKeyChestStand, false, false, true}, {ObsDomeLadderFoot, false, false, true},
		{ObsDomeArrive, false, true, false}, {ObsTelescopeAt, false, true, false}, {ObsProfessorAnchor, false, false, false},
		{EastArdougneNorth, false, false, false},
	} {
		if InObsKeep(c.at) != c.keep || InObsDome(c.at) != c.dome || InObsCavern(c.at) != c.cavern {
			t.Errorf("%v: keep %v dome %v cavern %v", c.at, InObsKeep(c.at), InObsDome(c.at), InObsCavern(c.at))
		}
	}
}
