package questplan

import (
	"fmt"
	"strings"
	"testing"
)

// Manni's trial passes two quest items through the pack, a keg of beer from the longhall's table and a low alcohol keg
// from the poison salesman. Content issues neither to an account that owns one ANYWHERE, and the fleet's between-run
// bank dump puts both into the bank: 27 of the 28 "no keg of beer on the table" lines of 13-14 Oct 2026 are one account,
// the only bank in the fleet holding a keg of beer, and 190 low alcohol kegs (about 47,500 gp) lay in 29 banks on
// 16 Oct. These tests pin the decision that takes them out of the bank, and the facts it rests on to the content.

func mannisTrialStarted() Snapshot {
	var f FremFacts
	f.Status[TrialManni] = TrialStarted
	return fremSnap(f)
}

func withBank(s Snapshot, ids map[int]int) Snapshot {
	s.BankKnown = true
	s.BankIDs = ids
	return s
}

// A pack without the keg of beer reads the bank before anything is bought or picked up, and an unread bank is not an
// empty one.
func TestMannisTrialReadsTheBankBeforeBuyingOrTakingAKeg(t *testing.T) {
	s := mannisTrialStarted()
	st := DecideFremennik(s)
	if st.Kind != StepScanBank || st.Bank != SeersBank || !st.BankSet {
		t.Fatalf("started, no keg, bank unread: %+v, want a scan of the Seers bank", st)
	}
	if why := ValidateStep(st); why != "" {
		t.Errorf("the scan is malformed: %s", why)
	}
	// Both kegs in the pack: nothing to look for.
	s.InvIDs[ObjBeerKeg], s.InvIDs[ObjLowAlcKeg] = 1, 1
	s.Ran["provision:"+KitFremManni] = true
	if st := DecideFremennik(s); st.Kind != StepCustom || st.Custom != CustomFremManni {
		t.Errorf("both kegs held: %+v, want Manni's step", st)
	}
	// A trial not yet started has no kegs anywhere, whatever the bank says.
	var f FremFacts
	fresh := fremSnap(f)
	if st := DecideFremennik(fresh); st.Kind != StepProvision || st.KitName != KitFremManni {
		t.Errorf("not started: %+v, want the kit first", st)
	}
}

// The account of the live log: a keg of beer and fifteen low alcohol kegs in the bank, nothing in the pack.
func TestMannisTrialTakesBothKegsOutOfTheBank(t *testing.T) {
	s := withBank(mannisTrialStarted(), map[int]int{ObjBeerKeg: 1, ObjLowAlcKeg: 15, ObjBeer: 5, ObjTinderbox: 1})
	st := DecideFremennik(s)
	if st.Kind != StepWithdraw || st.Bank != SeersBank || !st.BankSet {
		t.Fatalf("both kegs banked: %+v, want a withdrawal at the Seers bank", st)
	}
	if why := ValidateStep(st); why != "" {
		t.Errorf("the withdrawal is malformed: %s", why)
	}
	got := map[int]int{}
	for _, it := range st.Items {
		got[it.ID] += it.Qty
		if it.Name == "" {
			t.Errorf("item %d has no name for the log", it.ID)
		}
	}
	if len(st.Items) != 2 || got[ObjBeerKeg] != 1 || got[ObjLowAlcKeg] != 1 {
		t.Errorf("withdraws %+v, want one keg of beer and ONE low alcohol keg (the step uses one)", st.Items)
	}
	// Making room for them must not bank them again.
	keep := map[int]bool{}
	for _, id := range st.KeepIDs {
		keep[id] = true
	}
	for _, id := range []int{ObjBeerKeg, ObjLowAlcKeg, CoinsID, ObjBeer, ObjTinderbox} {
		if !keep[id] {
			t.Errorf("the withdrawal's room-making deposit would bank object %d", id)
		}
	}

	// Only the low alcohol keg banked: only it comes out, and the keg of beer is the table's.
	s = withBank(mannisTrialStarted(), map[int]int{ObjLowAlcKeg: 3})
	st = DecideFremennik(s)
	if st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != ObjLowAlcKeg {
		t.Errorf("one low alcohol keg banked: %+v", st)
	}
	// Only the keg of beer banked.
	s = withBank(mannisTrialStarted(), map[int]int{ObjBeerKeg: 1})
	st = DecideFremennik(s)
	if st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != ObjBeerKeg {
		t.Errorf("the keg of beer banked: %+v", st)
	}
	// A low alcohol keg already in the pack is not drawn a second time.
	s = withBank(mannisTrialStarted(), map[int]int{ObjBeerKeg: 1, ObjLowAlcKeg: 3})
	s.InvIDs[ObjLowAlcKeg] = 1
	st = DecideFremennik(s)
	if st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != ObjBeerKeg {
		t.Errorf("a low alcohol keg held: %+v, want only the keg of beer", st)
	}
	// Nothing banked: on to the kit, then Manni.
	s = withBank(mannisTrialStarted(), map[int]int{ObjBeer: 5})
	if st := DecideFremennik(s); st.Kind != StepProvision || st.KitName != KitFremManni {
		t.Errorf("bank read, no kegs in it: %+v, want the kit", st)
	}
	s.Ran["provision:"+KitFremManni] = true
	if st := DecideFremennik(s); st.Kind != StepCustom || st.Custom != CustomFremManni {
		t.Errorf("bank read, kitted: %+v, want Manni's step", st)
	}
	// And once both are in the pack the bank is left alone, whatever it still holds.
	s = withBank(mannisTrialStarted(), map[int]int{ObjBeerKeg: 1, ObjLowAlcKeg: 14})
	s.InvIDs[ObjBeerKeg], s.InvIDs[ObjLowAlcKeg] = 1, 1
	s.Ran["provision:"+KitFremManni] = true
	if st := DecideFremennik(s); st.Kind != StepCustom || st.Custom != CustomFremManni {
		t.Errorf("kegs withdrawn: %+v, want Manni's step", st)
	}
}

// The kit asks for the keg's price while the account has to buy one, and for nothing once it holds one. Thirteen of the
// 29 banks holding low alcohol kegs held no coins at all: a kit that asks 300 gp of such an account blocks at the coins
// and the account never gets as far as the keg it owns.
func TestMannisKitAsksForCoinsOnlyWhileTheKegIsToBeBought(t *testing.T) {
	coins := func(kit []KitLine) int {
		for _, l := range kit {
			if l.ID == CoinsID {
				return l.Qty
			}
		}
		return 0
	}
	s := mannisTrialStarted()
	if got := coins(fremKit(TrialManni, s)); got != ManniKegPrice || ManniKegPrice != 250 {
		t.Errorf("no keg held: the kit asks %d gp, want the salesman's %d", got, ManniKegPrice)
	}
	s.InvIDs[ObjLowAlcKeg] = 1
	if got := coins(fremKit(TrialManni, s)); got != 0 {
		t.Errorf("a low alcohol keg held: the kit still asks %d gp", got)
	}

	// An account with no coins anywhere (pack, bank, mule), a banked low alcohol keg, a beer and a tinderbox: without the
	// keg the kit is blocked on the coins; with it the kit is satisfied.
	st := ProvisionState{Pack: map[int]int{ObjBeer: 1, ObjTinderbox: 1}, Bank: map[int]int{}, BankKnown: true, FreeSlots: 20}
	if p := PlanProvision(fremKit(TrialManni, mannisTrialStarted()), st); p.Phase != PhaseBlocked {
		t.Fatalf("no keg, no coins: phase %s, want blocked (this is why the bank step comes before the kit)", p.Phase)
	}
	st.Pack[ObjLowAlcKeg] = 1
	held := mannisTrialStarted()
	held.InvIDs[ObjLowAlcKeg] = 1
	if p := PlanProvision(fremKit(TrialManni, held), st); p.Phase != PhaseSatisfied {
		t.Errorf("keg held, no coins: phase %s (%s), want satisfied", p.Phase, p.Summary())
	}

	// The whole decision for that account, tick by tick: the bank is read, the keg comes out, the kit is met, Manni goes.
	s = withBank(mannisTrialStarted(), map[int]int{ObjLowAlcKeg: 15, ObjBeer: 5, ObjTinderbox: 1})
	if got := DecideFremennik(s); got.Kind != StepWithdraw {
		t.Fatalf("tick 1: %+v, want the withdrawal", got)
	}
	s.InvIDs[ObjLowAlcKeg] = 1 // withdrawn
	got := DecideFremennik(s)
	if got.Kind != StepProvision || got.KitName != KitFremManni || coins(got.Kit) != 0 {
		t.Fatalf("tick 2: %+v, want the kit without coins", got)
	}
}

// ---------------------------------------------------------------------------
// The facts the decision rests on, pinned to the content checkout (skipped without one).
// ---------------------------------------------------------------------------

// The longhall table's keg is refused to an account owning one in the pack, the bank or the worn items.
func TestTheLonghallRefusesAKegToAnAccountThatOwnsOneAnywhere(t *testing.T) {
	script := scorpContentFile(t, "scripts", "quests", "quest_viking", "scripts", "viking_reveller.rs2")
	_, body, ok := strings.Cut(script, "[opobj3,viking_beerkeg]")
	if !ok {
		t.Fatal("viking_reveller.rs2 has no [opobj3,viking_beerkeg]")
	}
	body, _, _ = strings.Cut(body, "\n[")
	guard := "if(~obj_gettotal(viking_beerkeg) > 0) {"
	refusal := fmt.Sprintf("mes(%q);", strings.TrimSpace(ManniKegRefusal))
	_, after, found := strings.Cut(body, guard)
	if !found {
		t.Fatalf("the keg pickup is no longer guarded by %q:\n%s", guard, body)
	}
	first, _, _ := strings.Cut(after, "}")
	if !strings.Contains(first, refusal) || !strings.Contains(first, "return;") {
		t.Errorf("the guarded branch is not the refusal %s:\n%s", refusal, body)
	}

	procs := scorpContentFile(t, "scripts", "general", "scripts", "misc", "inv_procs.rs2")
	_, proc, ok := strings.Cut(procs, "[proc,obj_gettotal](obj $obj)(int)")
	if !ok {
		t.Fatal("inv_procs.rs2 has no obj_gettotal")
	}
	proc, _, _ = strings.Cut(proc, "\n[")
	for _, part := range []string{"inv_total(inv, $obj)", "inv_total(bank, $obj)", "inv_total(worn, $obj)"} {
		if !strings.Contains(proc, part) {
			t.Errorf("obj_gettotal no longer counts %s:\n%s", part, proc)
		}
	}
}

// The table's keg comes back two ticks after a pickup, which is why the pickup does not wait a minute for it.
func TestTheLonghallKegIsATwoTickRespawnAndAQuestItem(t *testing.T) {
	cfg := scorpContentFile(t, "scripts", "quests", "quest_viking", "configs", "viking.obj")
	for _, name := range []string{"viking_beerkeg", "viking_low_alcahol_beerkeg"} {
		_, def, ok := strings.Cut(cfg, "["+name+"]")
		if !ok {
			t.Fatalf("viking.obj has no [%s]", name)
		}
		def, _, _ = strings.Cut(def, "\n[")
		if !strings.Contains(def, "tradeable=no") {
			t.Errorf("%s is tradeable: a mule could carry it, and the kit says it cannot", name)
		}
		if name == "viking_beerkeg" && !strings.Contains(def, "respawnrate=2\n") {
			t.Errorf("the keg of beer no longer respawns in two ticks:\n%s", def)
		}
	}
	pack := scorpContentFile(t, "pack", "obj.pack")
	for id, name := range map[int]string{ObjBeerKeg: "viking_beerkeg", ObjLowAlcKeg: "viking_low_alcahol_beerkeg"} {
		if !strings.Contains("\n"+pack, fmt.Sprintf("\n%d=%s\n", id, name)) {
			t.Errorf("obj.pack does not say %d=%s", id, name)
		}
	}
}

// The salesman sells for 250 gp to anyone who can pay and carry it, and looks nowhere else: he does not know the bank
// holds fifteen.
func TestThePoisonSalesmanSellsAKegWhateverTheBankHolds(t *testing.T) {
	script := scorpContentFile(t, "scripts", "areas", "area_seers", "scripts", "poison_salesman.rs2")
	_, label, ok := strings.Cut(script, "[label,poison_salesman_beerkeg]")
	if !ok {
		t.Fatal("poison_salesman.rs2 has no poison_salesman_beerkeg")
	}
	label, _, _ = strings.Cut(label, "\n[")
	price := fmt.Sprint(ManniKegPrice)
	for _, want := range []string{"inv_total(inv, coins) < " + price, "inv_del(inv, coins, " + price + ")", "inv_add(inv, viking_low_alcahol_beerkeg, 1)"} {
		if !strings.Contains(label, want) {
			t.Errorf("the sale no longer contains %q:\n%s", want, label)
		}
	}
	for _, bad := range []string{"bank", "obj_gettotal"} {
		if strings.Contains(label, bad) {
			t.Errorf("the sale now looks at %q: a banked keg may no longer need to be withdrawn first", bad)
		}
	}
}
