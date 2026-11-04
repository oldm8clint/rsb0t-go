package questplan

import (
	"strings"
	"testing"
)

// pipAtEssence is a Priest in Peril snapshot at stage 10 (bring Drezel the essence) with an empty pack.
func pipAtEssence(rmDone bool) Snapshot {
	s := inProgress(10)
	s.QuestsDone = map[string]bool{RuneMysteriesID: rmDone}
	return s
}

// Live 14 Oct 2026, 04:06-04:45: 18 of 18 essence rounds put "mine-essence (mining 0)" in the plan for
// accounts whose Rune Mysteries was not done, walked to Aubury (a six-minute walk) and were refused
// ("Aubury refuses the teleport: Rune Mysteries is not complete", 13 lines). Without the quest the
// kit has no mining line and no pickaxe; with it, the mine and the pickaxe.
func TestPipEssenceStepIsMiningOnlyOnceRuneMysteriesIsDone(t *testing.T) {
	for _, done := range []bool{false, true} {
		st := DecidePriestInPeril(pipAtEssence(done))
		if st.Kind != StepProvision || st.KitName != KitPipEssence {
			t.Fatalf("rm done %v: want the essence kit, got %s", done, st.Describe())
		}
		if why := ValidateStep(st); why != "" {
			t.Fatalf("rm done %v: malformed: %s", done, why)
		}
		var mining, market, pickaxe bool
		for _, l := range st.Kit {
			pickaxe = pickaxe || l.ID == ObjBronzePickaxe
			for _, src := range l.Sources {
				mining = mining || (src.Kind == SourceSkill && src.Method == MethodMineEssence)
				market = market || src.Kind == SourceMarket
			}
		}
		if mining != done || pickaxe != done || market == done {
			t.Errorf("rm done %v: mining %v, pickaxe %v, market %v in %+v", done, mining, pickaxe, market, st.Kit)
		}
	}
}

// An account whose Rune Mysteries tab has not been read (or read red/yellow) is not "done".
func TestPipEssenceTreatsAnUnreadRuneMysteriesAsNotDone(t *testing.T) {
	s := inProgress(10) // QuestsDone is nil: the engine read nothing complete
	st := DecidePriestInPeril(s)
	for _, l := range st.Kit {
		if l.ID == ObjBronzePickaxe {
			t.Fatalf("a pickaxe asked for without Rune Mysteries: %+v", st.Kit)
		}
	}
}

// A dead-end essence kit parks the quest for the fixed retry, and the reason says what it waits for;
// with the quest done, it names no quest.
func TestPipBlockedEssenceKitParksAndNamesWhatItWaitsFor(t *testing.T) {
	for _, done := range []bool{false, true} {
		s := pipAtEssence(done)
		s.Blocked["provision:"+KitPipEssence] = true
		st := DecidePriestInPeril(s)
		if st.Kind != StepWait || st.ParkFor != PipEssenceRetry || ValidateStep(st) != "" {
			t.Fatalf("rm done %v: got %s (park %s)", done, st.Describe(), st.ParkFor)
		}
		waits := strings.Contains(st.Reason, PipEssenceNeedsRuneMysteries)
		if waits == done {
			t.Errorf("rm done %v: reason %q", done, st.Reason)
		}
		if got := ParkAwaitedQuest(PriestInPerilID, st.Reason); (got == RuneMysteriesID) != !done {
			t.Errorf("rm done %v: the park would wait on %q", done, got)
		}
	}
}

// What a park waits on: both spellings Priest in Peril has written, nothing for any other park.
func TestParkAwaitedQuest(t *testing.T) {
	cases := []struct {
		id, reason, want string
	}{
		{PriestInPerilID, `waited 15 ticks on "no unnoted rune essence from the bank, the mule, the Seers market or mining (Aubury's teleport needs Rune Mysteries)" without the world ever saying more`, RuneMysteriesID},
		{PriestInPerilID, "no unnoted rune essence from the bank, the mule or the Seers market, and " + PipEssenceNeedsRuneMysteries + ": waiting for the quest or for essence at the mule", RuneMysteriesID},
		{PriestInPerilID, `no progress for 8 ticks on "pip-mausoleum (the monuments for the iron key, the well for water)"`, ""},
		{PriestInPerilID, "no unnoted rune essence from the bank, the mule or mining", ""},
		{"vampire", "Aubury's teleport needs Rune Mysteries", ""},
	}
	for _, c := range cases {
		if got := ParkAwaitedQuest(c.id, c.reason); got != c.want {
			t.Errorf("%s %q: waits on %q, want %q", c.id, c.reason, got, c.want)
		}
	}
}

func TestMineEssenceRefusalLetsOnlyACompleteQuestIn(t *testing.T) {
	if why := MineEssenceRefusal(StatusComplete); why != "" {
		t.Errorf("a complete quest was refused: %s", why)
	}
	for _, s := range []Status{StatusNotStarted, StatusInProgress, StatusUnknown} {
		if why := MineEssenceRefusal(s); !strings.Contains(why, "Rune Mysteries") {
			t.Errorf("status %q: refusal %q", s, why)
		}
	}
}

func TestQuestTabNameIsTheTabsOwnSpelling(t *testing.T) {
	if got := QuestTabName(RuneMysteriesID); got != RuneMysteriesName {
		t.Errorf("Rune Mysteries: %q", got)
	}
	if got := QuestTabName(PriestInPerilID); got != PriestInPerilName {
		t.Errorf("Priest in Peril: %q", got)
	}
}

// The mule path (the runecraft essence supply will feed the mule): a mule that publishes Rune essence
// is asked for the lot in ONE bundle, whether or not Rune Mysteries is done, and a mule that gave only
// part of it leaves the account to hand that part to Drezel before anything is provisioned again.
func TestPipEssenceIsAskedOfAMuleThatPublishesIt(t *testing.T) {
	for _, done := range []bool{false, true} {
		pack := map[int]int{}
		if done {
			pack[ObjBronzePickaxe] = 1
		}
		p := PlanProvision(pipEssenceKit(done), ProvisionState{
			Pack: pack, BankKnown: true, FreeSlots: 26, Levels: map[string]int{"mining": 1},
			Mule: map[int]int{ObjRuneEssence: 500, 1265: 3}, MuleKnown: true})
		if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjRuneEssence || p.MuleAsk[0].Count != 24 {
			t.Fatalf("rm done %v: plan %s", done, p.Summary())
		}
	}
	// A mule with less than the lot gives what it has.
	p := PlanProvision(pipEssenceKit(false), ProvisionState{
		BankKnown: true, FreeSlots: 26, Mule: map[int]int{ObjRuneEssence: 10}, MuleKnown: true})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].Count != 10 {
		t.Fatalf("a mule with 10: plan %s", p.Summary())
	}
	// The one ask spent and the lot not whole: the plan is blocked (the market is not available), and
	// the Decide hands over what the pack holds first.
	s := pipAtEssence(false)
	s.InvIDs[ObjRuneEssence] = 10
	s.Blocked["provision:"+KitPipEssence] = true
	if st := DecidePriestInPeril(s); st.Kind != StepCustom || st.Custom != CustomPipEssence {
		t.Fatalf("10 essence held: hand it over, got %s", st.Describe())
	}
}

// The pickaxe serves the mine: essence the mule (or the bank) supplied needs no pickaxe, so an account
// that has none does not walk to Lumbridge to buy one once the essence is in its pack.
func TestPipPickaxeIsWantedOnlyWhileTheEssenceIsShort(t *testing.T) {
	kit := pipEssenceKit(true)
	// Nothing held, a mule with essence: the ask comes first, and the pickaxe errand with it is planned
	// only behind it.
	p := PlanProvision(kit, ProvisionState{BankKnown: true, FreeSlots: 26, Levels: map[string]int{"mining": 1},
		Mule: map[int]int{ObjRuneEssence: 500}, MuleKnown: true})
	if p.Phase != PhaseMule || len(p.MuleAsk) != 1 || p.MuleAsk[0].ID != ObjRuneEssence {
		t.Fatalf("plan %s", p.Summary())
	}
	// The essence arrived: nothing is short, no pickaxe is bought.
	p = PlanProvision(kit, ProvisionState{Pack: map[int]int{ObjRuneEssence: 24}, BankKnown: true, FreeSlots: 3,
		Levels: map[string]int{"mining": 1}, Mule: map[int]int{ObjRuneEssence: 476}, MuleKnown: true, MuleAsked: true})
	if p.Phase != PhaseSatisfied || len(p.Tasks) != 0 || len(p.Short) != 0 {
		t.Fatalf("essence held, no pickaxe: plan %s", p.Summary())
	}
	// Only part of it arrived: the rest is mined, and that needs the pickaxe.
	p = PlanProvision(kit, ProvisionState{Pack: map[int]int{ObjRuneEssence: 10}, BankKnown: true, FreeSlots: 17,
		Levels: map[string]int{"mining": 1}, MuleKnown: true, MuleAsked: true})
	if p.Phase != PhaseSource || len(p.Tasks) != 2 || p.Tasks[0].Source.Kind != SourceShop || p.Tasks[1].Source.Method != MethodMineEssence {
		t.Fatalf("part of the essence held: plan %s", p.Summary())
	}
	// A banked pickaxe is still drawn while the essence is short.
	p = PlanProvision(kit, ProvisionState{Bank: map[int]int{ObjBronzePickaxe: 1}, BankKnown: true, FreeSlots: 26,
		Levels: map[string]int{"mining": 1}, MuleKnown: true})
	if p.Phase != PhaseBank || len(p.Withdraw) != 1 || p.Withdraw[0].ID != ObjBronzePickaxe {
		t.Fatalf("banked pickaxe: plan %s", p.Summary())
	}
}

// A mule that publishes nothing for the account to use leaves the kit blocked, naming the mule.
func TestPipEssenceKitWithAnEmptyMuleIsBlockedOnTheMarketOnly(t *testing.T) {
	kit := pipEssenceKit(false)
	key := SourceKey(kit[0], kit[0].Sources[0])
	p := PlanProvision(kit, ProvisionState{
		BankKnown: true, FreeSlots: 26, Mule: map[int]int{}, MuleKnown: true,
		Exhausted: map[string]bool{key: true}, Unavailable: map[string]string{key: "player markets are off"}})
	joined := strings.Join(p.Blocked, ";")
	if p.Phase != PhaseBlocked || !strings.Contains(joined, "the mule publishes none") || !strings.Contains(joined, "player markets are off") {
		t.Fatalf("plan %s", p.Summary())
	}
}
