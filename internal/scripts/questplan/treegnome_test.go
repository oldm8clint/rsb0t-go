package questplan

import "testing"

func TestTreeGnomeVillageSteps(t *testing.T) {
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}}
	}
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"stage 1: Montai", snap(1), StepTalk, func(s Step) bool { return s.Stop.ID == NpcMontai }},
		{"stage 2 without logs: the kit", snap(2), StepProvision, func(s Step) bool { return s.KitName == KitTreeLogs }},
		{"stage 4: the ballista", snap(4), StepCustom, func(s Step) bool { return s.Custom == CustomTreeBallista }},
		// The orb step carries food first (treegnome_stronghold_test.go has the rest of the gate).
		{"stage 5: the orb kit", snap(5), StepProvision, func(s Step) bool { return s.KitName == KitTreeOrb }},
		{"stage 7: the fight kit", snap(7), StepProvision, func(s Step) bool { return s.KitName == KitTreeFight }},
		{"stage 8 with the orbs: the ceremony", func() Snapshot { s := snap(8); s.InvIDs[ObjOrbsOfProt] = 1; return s }(), StepCustom,
			func(s Step) bool { return s.Custom == CustomTreeCeremony }},
	}
	for _, c := range cases {
		st := DecideTreeGnomeVillage(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
	s := snap(2)
	s.InvIDs[ObjLogs] = 6
	if st := DecideTreeGnomeVillage(s); st.Kind != StepTalk || st.Stop.ID != NpcMontai {
		t.Errorf("six logs held: Montai, got %s", st.Describe())
	}
	s = snap(6)
	s.BankIDs[ObjOrbOfProt] = 1
	if st := DecideTreeGnomeVillage(s); st.Kind != StepWithdraw {
		t.Errorf("a banked orb is withdrawn (the chest refuses while one is banked), got %s", st.Describe())
	}
}

// The ballista cannot be a preference list: the first preference would win all three identical
// menus, which is why it is a custom step driving the answers in order.
func TestBallistaAnswersAreNotAPreferenceList(t *testing.T) {
	opts := []string{"0001", "0002", "0003", "0004", "0005"}
	for i := 0; i < 3; i++ {
		if pick, _ := PickPreferred(opts, BallistaAnswers); pick != "0004" {
			t.Fatalf("expected a preference list to pick 0004 every time, got %s", pick)
		}
	}
}

func TestTreeLogsKitWithAnEmptyMuleChops(t *testing.T) {
	// treegnome-empty-1: the axe is a priced shop line, so the bank's coins are drawn for it,
	// and it comes before the chopping.
	p := PlanProvision(treeLogsKit(Snapshot{}), ProvisionState{Bank: map[int]int{CoinsID: 3000}, BankKnown: true, MuleKnown: true,
		FreeSlots: 20, Levels: map[string]int{"woodcutting": 1}})
	if p.Phase == PhaseBlocked || len(p.MuleAsk) != 0 || len(p.Tasks) != 2 ||
		p.Tasks[0].Source.Kind != SourceShop || p.Tasks[1].Source.Method != MethodChopLogs {
		t.Fatalf("plan %s", p.Summary())
	}
	// An owned axe is drawn from the bank, never bought.
	s := Snapshot{BankKnown: true, BankIDs: map[int]int{1353: 1}, InvIDs: map[int]int{}, WornIDs: map[int]int{}}
	if k := treeLogsKit(s); k[0].ID != 1353 || len(k[0].Sources) != 0 {
		t.Fatalf("owned axe: %+v", k[0])
	}
}
