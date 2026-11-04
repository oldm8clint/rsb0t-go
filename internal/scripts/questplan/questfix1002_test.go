package questplan

import (
	"strings"
	"testing"
)

// The 2 Oct quest fixes' pure halves: Murder Mystery's items back out of the bank, Priest in
// Peril's dead-end kits that wait instead of looping, Vampire Slayer's beer money.

func inProgress(stage int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
		FreeSlots: 20, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}, Blocked: map[string]bool{}}
}

// ---------------------------------------------------------------------------
// Murder Mystery
// ---------------------------------------------------------------------------

// Live, 29 Sep - 2 Oct: acct5 and 100 others parked on "murder-window (a thread again ...)"
// because the trainer had banked the thread and the window will not give a second ("You have
// already taken the thread": the content counts the bank). A run that begins with the thread bit
// set and has not read the bank this run reads it before it walks to the window.
func TestMurderReadsTheBankBeforeTheWindowAgain(t *testing.T) {
	s := inProgress(MurderStarted + MurderThread) // bank unread, nothing held
	st := DecideMurderMystery(s)
	if st.Kind != StepProvision || st.KitName != KitMurderEvidence {
		t.Fatalf("an unread bank and no thread: got %s", st.Describe())
	}
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed: %s", why)
	}
	if st.Bank != SeersBank {
		t.Errorf("the kit's bank is the Seers bank beside the mansion, got %v", st.Bank)
	}
	for _, l := range st.Kit {
		if !l.Optional || !l.Untradeable || l.Qty != 1 {
			t.Errorf("%s: an evidence line is optional, untradeable and one: %+v", l.Name, l)
		}
	}

	// The kit asks for every item the quest keeps except coins and the empty pot (its own kit).
	want := map[int]bool{}
	for _, id := range murderTools {
		want[id] = id != CoinsID && id != ObjEmptyPot
	}
	got := map[int]bool{}
	for _, l := range st.Kit {
		got[l.ID] = true
	}
	for id, w := range want {
		if w != got[id] {
			t.Errorf("item %d: in the kit %v, want %v", id, got[id], w)
		}
	}

	// With the bank read and the thread in it the kit is still the answer (it frees the pack too).
	s.BankKnown = true
	s.BankIDs[ObjThreadGreen] = 1
	if st := DecideMurderMystery(s); st.Kind != StepProvision || st.KitName != KitMurderEvidence {
		t.Fatalf("a banked thread: got %s", st.Describe())
	}
	// With the bank read and nothing of the quest's in it, no bank trip is spent.
	s.BankIDs = map[int]int{ObjBronzePickaxe: 3}
	if st := DecideMurderMystery(s); st.Kind != StepCustom || st.Custom != CustomMurderWindow {
		t.Fatalf("a read bank with none of the quest's items: the window, got %s", st.Describe())
	}
	// A banked dagger with the thread in the pack: the dagger comes out too.
	s = with(inProgress(MurderStarted+MurderThread), ObjThreadRed)
	s.BankKnown = true
	s.BankIDs[ObjMurderDagger] = 1
	if st := DecideMurderMystery(s); st.Kind != StepProvision || st.KitName != KitMurderEvidence {
		t.Fatalf("a banked dagger: got %s", st.Describe())
	}
}

func with(s Snapshot, ids ...int) Snapshot {
	for _, id := range ids {
		s.InvIDs[id]++
	}
	return s
}

// The kit runs once a run, never when the run took the thread itself, and not again after it
// ended blocked; a kit that failed on the way is tried again.
func TestMurderEvidenceKitRunsOncePerRun(t *testing.T) {
	s := inProgress(MurderStarted + MurderThread)
	s.Ran["provision:"+KitMurderEvidence] = true
	if st := DecideMurderMystery(s); st.Kind == StepProvision && st.KitName == KitMurderEvidence {
		t.Fatal("the kit ran this run and was asked for again")
	}
	s = inProgress(MurderStarted + MurderThread)
	s.Ran[CustomMurderWindow] = true // this run took the thread at the window
	if st := DecideMurderMystery(s); st.Kind == StepProvision && st.KitName == KitMurderEvidence {
		t.Fatal("a run that just took the thread from the window looked in the bank for it")
	}
	s = inProgress(MurderStarted + MurderThread)
	s.Failed["provision:"+KitMurderEvidence] = true
	s.Blocked["provision:"+KitMurderEvidence] = true
	if st := DecideMurderMystery(s); st.Kind == StepProvision && st.KitName == KitMurderEvidence {
		t.Fatal("a blocked kit was asked for again")
	}
	s = inProgress(MurderStarted + MurderThread)
	s.Failed["provision:"+KitMurderEvidence] = true // a walk to the bank timed out
	if st := DecideMurderMystery(s); st.Kind != StepProvision || st.KitName != KitMurderEvidence {
		t.Fatalf("a kit that failed on the way is tried again, got %s", st.Describe())
	}
	// Before the thread is found there is nothing to fetch; after the poison is proved the guard
	// needs none of it.
	if st := DecideMurderMystery(inProgress(MurderStarted)); st.Kind != StepCustom || st.Custom != CustomMurderWindow {
		t.Fatalf("no thread bit: the window, got %s", st.Describe())
	}
	if st := DecideMurderMystery(inProgress(MurderStarted + MurderThread + MurderPoison)); st.Kind != StepTalk {
		t.Fatalf("poison proved: the guard, got %s", st.Describe())
	}
}

// The evidence kit's plan: an unread bank is read (and the pack cleared of what is not kit), a
// bank holding the thread and the dagger yields exactly those two withdrawals, and a bank holding
// none of it leaves the plan satisfied rather than blocked.
func TestMurderEvidenceKitPlans(t *testing.T) {
	kit := murderEvidenceKit()
	p := PlanProvision(kit, ProvisionState{FreeSlots: 5, Depositable: 20, Pack: map[int]int{CoinsID: 100}})
	if p.Phase != PhaseBank || len(p.Withdraw) != 0 {
		t.Fatalf("an unread bank: %s", p.Summary())
	}
	p = PlanProvision(kit, ProvisionState{FreeSlots: 5, Depositable: 20, BankKnown: true,
		Bank: map[int]int{ObjThreadBlue: 1, ObjMurderDagger: 1, ObjBronzePickaxe: 1}, MuleKnown: true, Mule: map[int]int{ObjThreadBlue: 5}})
	if p.Phase != PhaseBank || len(p.Withdraw) != 2 || len(p.MuleAsk) != 0 {
		t.Fatalf("a banked thread and dagger: %s", p.Summary())
	}
	ids := map[int]bool{}
	for _, w := range p.Withdraw {
		ids[w.ID] = true
	}
	if !ids[ObjThreadBlue] || !ids[ObjMurderDagger] {
		t.Fatalf("withdrawals %v", p.Withdraw)
	}
	p = PlanProvision(kit, ProvisionState{FreeSlots: 20, BankKnown: true, Bank: map[int]int{ObjBronzePickaxe: 1}})
	if p.Phase != PhaseSatisfied {
		t.Fatalf("a bank without the quest's items must not block the quest: %s", p.Summary())
	}
}

// ---------------------------------------------------------------------------
// Priest in Peril
// ---------------------------------------------------------------------------

// Live, 29 Sep - 2 Oct: 423 rounds of "no mule, market failed this round, mining failed this
// round" for the essence, each a bank trip and a refused teleport until the slot ran out. A kit
// that ended BLOCKED waits (and the quest parks, freeing the one-at-a-time slot); one that failed
// on the way (a walk that timed out) is provisioned again.
func TestPipWaitsOnADeadEndKitInsteadOfLooping(t *testing.T) {
	s := inProgress(10)
	st := DecidePriestInPeril(s)
	if st.Kind != StepProvision || st.KitName != KitPipEssence {
		t.Fatalf("stage 10 with no essence: got %s", st.Describe())
	}
	s.Failed["provision:"+KitPipEssence] = true // a travel failure
	if st := DecidePriestInPeril(s); st.Kind != StepProvision {
		t.Fatalf("a kit that failed on the way is tried again, got %s", st.Describe())
	}
	s.Blocked["provision:"+KitPipEssence] = true
	st = DecidePriestInPeril(s)
	if st.Kind != StepWait || !strings.Contains(st.Reason, "Rune Mysteries") {
		t.Fatalf("a blocked essence kit waits and says why, got %s", st.Describe())
	}
	// It parks for a fixed PipEssenceRetry at once (not after fifteen ticks and the escalating backoff).
	if st.ParkFor != PipEssenceRetry {
		t.Fatalf("a blocked essence kit parks for %s, got %s", PipEssenceRetry, st.ParkFor)
	}
	// With essence in the pack the blocked flag is irrelevant: hand it over.
	s.InvIDs[ObjRuneEssence] = 24
	if st := DecidePriestInPeril(s); st.Kind != StepCustom || st.Custom != CustomPipEssence {
		t.Fatalf("essence held: hand it over, got %s", st.Describe())
	}

	for _, stage := range []int{5, 6} {
		b := inProgress(stage)
		b.InvIDs[ObjGoldenKey] = 1
		if stage == 6 {
			b.InvIDs[ObjGoldenKey] = 0
		}
		if st := DecidePriestInPeril(b); st.Kind != StepProvision || st.KitName != KitPipBucket {
			t.Fatalf("stage %d with no bucket: got %s", stage, st.Describe())
		}
		b.Blocked["provision:"+KitPipBucket] = true
		if st := DecidePriestInPeril(b); st.Kind != StepWait || !strings.Contains(st.Reason, "bucket") {
			t.Fatalf("stage %d: a blocked bucket kit waits, got %s", stage, st.Describe())
		}
		b.InvIDs[ObjBucket] = 1
		if st := DecidePriestInPeril(b); st.Kind == StepWait {
			t.Fatalf("stage %d: with a bucket held nothing waits, got %s", stage, st.Describe())
		}
	}
}

// The bucket has two sources (the Varrock castle spawn, then the general store) and the essence
// kit lists mining after the market: the facts the task asked to be kept.
func TestPipKitsKeepTheirSources(t *testing.T) {
	b := pipBucketKit()
	if len(b) != 1 || len(b[0].Sources) != 2 || b[0].Sources[0].Kind != SourceGround || b[0].Sources[0].Tile.X != 3221 ||
		b[0].Sources[0].Tile.Z != 3497 || b[0].Sources[0].Tile.Level != 1 || b[0].Sources[1].Kind != SourceShop {
		t.Fatalf("bucket kit %+v", b)
	}
	// The essence line lists the market without Rune Mysteries and the mine with it, never both.
	sources := func(done bool) (skill, market bool) {
		kit := pipEssenceKit(done)
		for _, src := range kit[len(kit)-1].Sources {
			skill = skill || (src.Kind == SourceSkill && src.Method == MethodMineEssence)
			market = market || (src.Kind == SourceMarket && src.Method == MethodMarketEssence)
		}
		return skill, market
	}
	if skill, market := sources(false); skill || !market {
		t.Fatalf("without Rune Mysteries the essence line lists market %v and mining %v", market, skill)
	}
	if skill, market := sources(true); !skill || market {
		t.Fatalf("with Rune Mysteries the essence line lists market %v and mining %v", market, skill)
	}
}

// ---------------------------------------------------------------------------
// Vampire Slayer
// ---------------------------------------------------------------------------

// A Stage 5 account at stage 2 with no coins in the pack draws them from the bank rather than
// parking; the bank is read first when this run has not read it.
func TestVampireDrawsBeerMoneyFromTheBank(t *testing.T) {
	s := inProgress(2)
	s.Ran["provision:"+KitVampire] = true
	s.InvIDs = map[int]int{}
	st := DecideVampireSlayer(s)
	if st.Kind != StepScanBank || st.Bank != VarrockEastBank {
		t.Fatalf("no coins and an unread bank: read it, got %s", st.Describe())
	}
	s.BankKnown = true
	s.BankIDs[CoinsID] = 5000
	st = DecideVampireSlayer(s)
	if st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != CoinsID || st.Items[0].Qty != vampireBeerFloat {
		t.Fatalf("a banked fortune: draw %d coins, got %s %+v", vampireBeerFloat, st.Describe(), st.Items)
	}
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed: %s", why)
	}
	s.BankIDs[CoinsID] = 3
	if st = DecideVampireSlayer(s); st.Kind != StepWithdraw || st.Items[0].Qty != 3 {
		t.Fatalf("a bank with 3 coins: draw 3, got %s %+v", st.Describe(), st.Items)
	}
	s.BankIDs[CoinsID] = 0
	st = DecideVampireSlayer(s)
	if st.Kind != StepWait || !strings.HasPrefix(st.Reason, "no coins for Dr Harlow's beer") {
		t.Fatalf("no coins anywhere: wait, got %s", st.Describe())
	}
	// With coins in the pack the bartender is next, as before.
	s.InvIDs[CoinsID] = 10
	if st = DecideVampireSlayer(s); st.Kind != StepTalk || st.Stop.ID != NpcJollyBoarBar {
		t.Fatalf("coins held: the bartender, got %s", st.Describe())
	}
}
