package questplan

import "testing"

func TestParseScorpionCatcherJournal(t *testing.T) {
	cases := []struct {
		lines []string
		want  int
	}{
		{[]string{"@dbl@I can start this quest by speaking to @dre@Thormac@dbl@ who is in the"}, 0},
		{[]string{"@str@I've spoken to Thormac in the Sorcerer's Tower south-west", "", "@dbl@I need to go to the @dre@Seers' Village"}, ScorpStarted},
		{[]string{"@str@I've spoken to Thormac in the Sorcerer's Tower south-west", "@str@I've spoken to a Seer and been given the location of one"}, ScorpHint},
		{[]string{"@str@I've spoken to a Seer and been given the location of one", "@dbl@The second @dre@Kharid Scorpion@dbl@ has been in a"}, ScorpHint2},
		{[]string{"@str@pet Kharid Scorpions.", "", "@red@QUEST COMPLETE!"}, ScorpComplete},
	}
	for _, c := range cases {
		if got, ok := ParseScorpionCatcherJournal(c.lines); !ok || got != c.want {
			t.Errorf("%q: %d (ok %v), want %d", c.lines, got, ok, c.want)
		}
	}
}

func TestScorpionCatcherSteps(t *testing.T) {
	snap := func(stage, cage int) Snapshot {
		s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}}
		if cage != 0 {
			s.InvIDs[cage] = 1
		}
		return s
	}
	ns := snap(0, 0)
	ns.Journal = StatusNotStarted
	kitDone := snap(ScorpHint, CageBC)
	kitDone.Ran["provision:"+KitScorpA] = true
	kitDone.WornIDs[ObjAntiDragon] = 1
	deep := snap(ScorpHint, CageFull)
	deep.Tile, deep.TileKnown = ScorpionATile, true
	// The live failure of 15-16 Oct 2026: a started quest whose cage sits in the bank, the bank not read yet this run.
	unread := snap(ScorpStarted, 0)
	unread.BankKnown, unread.BankIDs = false, nil
	banked := snap(ScorpHint, 0)
	banked.BankIDs[CageEmpty] = 1
	bankedAB := snap(ScorpHint, 0)
	bankedAB.BankIDs[CageAB] = 1
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started: Thormac", ns, StepTalk, func(s Step) bool { return s.Stop.ID == NpcThormac }},
		{"no cage, bank read and empty: Thormac again", snap(ScorpStarted, 0), StepTalk, func(s Step) bool { return s.Stop.ID == NpcThormac }},
		{"no cage, bank unread: read the Seers' bank before asking Thormac", unread, StepScanBank, func(s Step) bool { return s.Bank == SeersBank && s.BankSet }},
		{"no cage, an empty cage banked: draw it", banked, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 1 && s.Items[0].ID == CageEmpty && s.Items[0].Qty == 1 && s.Bank == SeersBank && s.BankSet
		}},
		{"no cage, a part-filled cage banked: draw that one", bankedAB, StepWithdraw, func(s Step) bool {
			return len(s.Items) == 1 && s.Items[0].ID == CageAB
		}},
		{"stage 1: a Seer", snap(ScorpStarted, CageEmpty), StepTalk, func(s Step) bool { return s.Stop.ID == NpcSeer }},
		{"hint, empty cage: B", snap(ScorpHint, CageEmpty), StepCustom, func(s Step) bool { return s.Custom == CustomScorpB }},
		{"B caught: C", snap(ScorpHint, CageB), StepCustom, func(s Step) bool { return s.Custom == CustomScorpC }},
		{"B and C: the Taverley kit", snap(ScorpHint, CageBC), StepProvision, func(s Step) bool { return s.KitName == KitScorpA }},
		{"kit on: A", kitDone, StepCustom, func(s Step) bool { return s.Custom == CustomScorpA }},
		{"full cage in the deep dungeon: out", deep, StepCustom, func(s Step) bool { return s.Custom == CustomScorpLeave }},
		{"full cage: Thormac", snap(ScorpHint, CageFull), StepTalk, func(s Step) bool { return s.Stop.ID == NpcThormac }},
	}
	for _, c := range cases {
		st := DecideScorpionCatcher(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

// Every cage state names exactly what it holds (quest_scorpcatcher.rs2's swaps).
func TestCageHolds(t *testing.T) {
	count := map[int]int{}
	for _, id := range scorpAllCages {
		a, b, c := CageHolds(id)
		for _, x := range []bool{a, b, c} {
			if x {
				count[id]++
			}
		}
	}
	if count[CageEmpty] != 0 || count[CageA] != 1 || count[CageBC] != 2 || count[CageFull] != 3 {
		t.Fatalf("%v", count)
	}
}

// A cage the bank holds never reaches Thormac: his script offers the lost-cage line only when neither the pack nor
// the bank holds one, and answers an account with a banked cage with "go and speak to the Seers" (thormac.rs2
// thormac_how_goes). So once the bank has been read the decision may talk to him only when it really holds none, and
// before the bank is read it must not talk to him at all, at any journal stage.
func TestScorpionCatcherNeverAsksThormacWithAnUnreadBank(t *testing.T) {
	for stage := ScorpStarted; stage <= ScorpHint2; stage++ {
		s := Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, Ran: map[string]bool{}, Failed: map[string]bool{}}
		if st := DecideScorpionCatcher(s); st.Kind != StepScanBank {
			t.Errorf("stage %d, no cage held, bank unread: %s, want the bank read first", stage, st.Describe())
		}
		s.BankKnown, s.BankIDs = true, map[int]int{}
		if st := DecideScorpionCatcher(s); st.Kind != StepTalk || st.Stop.ID != NpcThormac {
			t.Errorf("stage %d, no cage held, bank read and empty: %s, want Thormac", stage, st.Describe())
		}
	}
}
