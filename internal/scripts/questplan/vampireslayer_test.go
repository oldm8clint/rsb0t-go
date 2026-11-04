package questplan

import "testing"

func TestVampireSlayerSteps(t *testing.T) {
	s := Snapshot{Journal: StatusInProgress, InvIDs: map[int]int{CoinsID: 10}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}}
	step := func() Step {
		st := DecideVampireSlayer(s)
		if why := ValidateStep(st); why != "" {
			t.Fatalf("malformed %s: %s", st.Describe(), why)
		}
		return st
	}
	if st := step(); st.Kind != StepProvision {
		t.Fatalf("first the kit: %s", st.Describe())
	}
	s.Ran["provision:"+KitVampire] = true
	s.Stage, s.StageKnown = 1, true
	if st := step(); st.Kind != StepTalk || st.Stop.ID != NpcHarlow {
		t.Fatalf("stage 1: Harlow %s", st.Describe())
	}
	s.Stage = 2
	if st := step(); st.Kind != StepTalk || st.Stop.ID != NpcJollyBoarBar {
		t.Fatalf("stage 2, no beer: the bartender %s", st.Describe())
	}
	s.InvIDs[ObjBeer] = 1
	if st := step(); st.Kind != StepTalk || st.Stop.ID != NpcHarlow || st.UntilItemID != ObjStake {
		t.Fatalf("stage 2 with a beer: the stake %s", st.Describe())
	}
	delete(s.InvIDs, ObjBeer)
	s.InvIDs[ObjStake] = 1
	if st := step(); st.Kind != StepProvision || len(st.Kit) != 1 || st.Kit[0].ID != ObjHammer {
		t.Fatalf("no hammer: buy one %s", st.Describe())
	}
	s.InvIDs[ObjHammer] = 1
	if st := step(); st.Kind != StepCustom || st.Custom != CustomVampireGarlic {
		t.Fatalf("garlic next %s", st.Describe())
	}
	s.Failed[CustomVampireGarlic] = true
	if st := step(); st.Kind != StepCustom || st.Custom != CustomVampireCount {
		t.Fatalf("the Count, garlic being optional %s", st.Describe())
	}
	s.Journal = StatusNotStarted
	if st := step(); st.Kind != StepTalk || st.Stop.ID != NpcMorgan {
		t.Fatalf("not started: Morgan %s", st.Describe())
	}
}

// A8: with an EMPTY mule the hammer falls to the general store.
func TestVampireKitWithAnEmptyMule(t *testing.T) {
	p := PlanProvision(vampireKit(), ProvisionState{BankKnown: true, MuleKnown: true, FreeSlots: 20,
		Pack: map[int]int{CoinsID: 10}})
	if len(p.MuleAsk) != 0 || p.Phase != PhaseSource || p.Tasks[0].Line.ID != ObjHammer {
		t.Fatalf("plan %s", p.Summary())
	}
}
