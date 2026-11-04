package questplan

import "testing"

func TestParseMurderMysteryJournal(t *testing.T) {
	cases := []struct {
		lines []string
		want  int
	}{
		{[]string{"@dbl@I can start this quest by speaking to one of the @dre@Guards@dbl@ at"}, 0},
		{[]string{"@str@Lord Sinclair, a prominent nobleman, had been horribly", "@dbl@One of the @dre@guards@dbl@ has asked me for my help in solving the"}, MurderStarted},
		{[]string{"@str@Lord Sinclair, a prominent nobleman, had been horribly", "@dbl@I have found some @dre@coloured thread@dbl@. It might be useful."}, MurderStarted + MurderThread},
		{[]string{"@str@Lord Sinclair, a prominent nobleman, had been horribly", "@dbl@I have @dre@indisputable evidence@dbl@ of who the murderer must be."}, MurderStarted + MurderThread + MurderPoison},
	}
	for _, c := range cases {
		if got, ok := ParseMurderMysteryJournal(c.lines); !ok || got != c.want {
			t.Errorf("%q: %d (ok %v), want %d", c.lines, got, ok, c.want)
		}
	}
}

func TestMurderMysterySteps(t *testing.T) {
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{}, Failed: map[string]bool{}}
	}
	with := func(s Snapshot, ids ...int) Snapshot {
		for _, id := range ids {
			s.InvIDs[id] = 1
		}
		return s
	}
	ns := snap(0)
	ns.Journal = StatusNotStarted
	banked := snap(MurderStarted + MurderThread)
	banked.BankIDs[ObjThreadBlue] = 1
	// The evidence kit has run (or a window step took the thread this run): the old direct
	// withdrawal is what is left for a thread still sitting in the bank.
	bankedAfterKit := snap(MurderStarted + MurderThread)
	bankedAfterKit.BankIDs[ObjThreadBlue] = 1
	bankedAfterKit.Ran["provision:"+KitMurderEvidence] = true
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started: the guard", ns, StepTalk, func(s Step) bool { return s.Stop.Prefer[0] == "Sure, I'll help." }},
		{"no thread bit: the window", snap(MurderStarted), StepCustom, func(s Step) bool { return s.Custom == CustomMurderWindow }},
		{"thread banked: the evidence kit first", banked, StepProvision, func(s Step) bool { return s.KitName == KitMurderEvidence }},
		{"thread banked, kit already run: withdraw it", bankedAfterKit, StepWithdraw, func(s Step) bool { return s.Items[0].ID == ObjThreadBlue }},
		{"no pot: the pot kit", with(snap(MurderStarted+MurderThread), ObjThreadRed), StepProvision, func(s Step) bool { return s.KitName == KitMurderPot }},
		{"a pot: the prints", with(snap(MurderStarted+MurderThread), ObjThreadRed, ObjEmptyPot), StepCustom, func(s Step) bool { return s.Custom == CustomMurderPrints }},
		{"printed: the poison", with(snap(MurderStarted+MurderThread), ObjThreadRed, ObjKillersPrint), StepCustom, func(s Step) bool { return s.Custom == CustomMurderPoison }},
		{"proved: the guard", snap(MurderStarted + MurderThread + MurderPoison), StepTalk, func(s Step) bool { return s.Stop.Prefer[0] == "I know who did it!" }},
	}
	for _, c := range cases {
		st := DecideMurderMystery(c.snap)
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

// The thread narrows six suspects to two (get_murder_thread), and a matched suspect print names
// the culprit.
func TestMurderDeduction(t *testing.T) {
	if c := ThreadCandidates(ObjThreadGreen); len(c) != 2 || Suspects[c[0]].Name != "Anna" || Suspects[c[1]].Name != "David" {
		t.Fatalf("green: %v", c)
	}
	if c := ThreadCandidates(ObjThreadRed); Suspects[c[0]].Name != "Bob" || Suspects[c[1]].Name != "Carol" {
		t.Fatalf("red: %v", c)
	}
	s := Snapshot{InvIDs: map[int]int{ObjKillersPrint: 1, Suspects[4].Print: 1}}
	if Culprit(s) != 4 {
		t.Fatalf("culprit %d, want Elizabeth", Culprit(s))
	}
}
