package questplan

import "testing"

func TestGrandTreeSteps(t *testing.T) {
	snap := func(stage int) Snapshot {
		return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true, InvIDs: map[int]int{}, WornIDs: map[int]int{},
			FreeSlots: 20, BankKnown: true, BankIDs: map[int]int{}, Ran: map[string]bool{"provision:" + KitGTDemon: true},
			Failed: map[string]bool{}, Tile: NarnodeAnchor, TileKnown: true}
	}
	with := func(s Snapshot, ids ...int) Snapshot {
		for _, id := range ids {
			s.InvIDs[id] = 1
		}
		return s
	}
	ns := snap(0)
	ns.Journal = StatusNotStarted
	karamja := snap(GTReleased)
	karamja.Tile = GTKaramjaCrash
	outside := snap(GTLumber)
	outside.Tile = FemiAnchor
	inside := snap(GTLumber)
	inside.Tile = CharlieAnchor
	noKit := snap(GTTwigs)
	noKit.Ran = map[string]bool{}
	kitFailed := snap(GTTrapdoor)
	kitFailed.Ran = map[string]bool{}
	kitFailed.Failed = map[string]bool{"provision:" + KitGTDemon: true}
	kitFailedArmed := with(snap(GTTrapdoor), 385, 562)
	kitFailedArmed.Ran = map[string]bool{}
	kitFailedArmed.Failed = map[string]bool{"provision:" + KitGTDemon: true}
	kitRanArmed := with(snap(GTTrapdoor), 385, 562)
	// The kit ran (its lines are all optional, so a round that found nothing reads SATISFIED) and the
	// pack holds nothing for the demon: live on 5 Oct 2026 acct9 and emod went down like this.
	kitRanEmpty := snap(GTTrapdoor)
	kitRanOnlyFood := with(snap(GTTrapdoor), 385)
	cases := []struct {
		name  string
		snap  Snapshot
		kind  StepKind
		check func(Step) bool
	}{
		{"not started", ns, StepCustom, func(s Step) bool { return s.Custom == CustomGTStart }},
		{"10 with the bark: Hazelmere", with(snap(GTStarted), ObjGTBark), StepTalk, func(s Step) bool { return s.Stop.ID == NpcHazelmere }},
		{"20: the King", snap(GTHazelmere), StepTalk, func(s Step) bool { return s.Stop.ID == NpcNarnode }},
		{"30: Glough", snap(GTRelayed), StepTalk, func(s Step) bool { return s.Stop.ID == NpcGlough }},
		{"50: Charlie", snap(GTPrisoner), StepTalk, func(s Step) bool { return s.Stop.ID == NpcCharlie }},
		{"60: the cupboard", snap(GTSpokenChar), StepCustom, func(s Step) bool { return s.Custom == CustomGTCupboard }},
		{"70: jailed", snap(GTJournal), StepCustom, func(s Step) bool { return s.Custom == CustomGTJailed }},
		{"80 at home: fly", snap(GTReleased), StepCustom, func(s Step) bool { return s.Custom == CustomGTFly }},
		{"80 in Karamja: foreman", karamja, StepCustom, func(s Step) bool { return s.Custom == CustomGTForeman }},
		{"90 outside: Femi", outside, StepCustom, func(s Step) bool { return s.Custom == CustomGTFemi }},
		{"90 inside: Charlie", inside, StepTalk, func(s Step) bool { return s.Stop.ID == NpcCharlie }},
		{"100: Anita", snap(GTClue), StepCustom, func(s Step) bool { return s.Custom == CustomGTAnita }},
		{"100 with the key: the chest", with(snap(GTClue), ObjGTKey), StepCustom, func(s Step) bool { return s.Custom == CustomGTChest }},
		{"110 with plans: the King", with(snap(GTPlans), ObjGTPlans), StepTalk, func(s Step) bool { return s.Stop.ID == NpcNarnode }},
		{"120 without the kit: the kit", noKit, StepProvision, func(s Step) bool { return s.KitName == KitGTDemon }},
		{"120 with a twig: the pillars", with(snap(GTTwigs), 790), StepCustom, func(s Step) bool { return s.Custom == CustomGTTwig }},
		{"130: the demon", kitRanArmed, StepCustom, func(s Step) bool { return s.Custom == CustomGTDemon }},
		{"130, the kit ran and found nothing: wait", kitRanEmpty, StepWait, func(s Step) bool { return true }},
		{"130, the kit ran and found food but no runes: wait", kitRanOnlyFood, StepWait, func(s Step) bool { return true }},
		{"130, the kit failed and the pack is empty: wait", kitFailed, StepWait, func(s Step) bool { return true }},
		{"130, the kit failed but sharks and chaos are held: the demon", kitFailedArmed, StepCustom, func(s Step) bool { return s.Custom == CustomGTDemon }},
		{"150 outside the caves: back in", snap(GTDaconia), StepCustom, func(s Step) bool { return s.Custom == CustomGTDemon }},
	}
	for _, c := range cases {
		st := decideGrandTree(c.snap, func(Snapshot) GTDemonVerdict { return GTDemonVerdict{OK: true} })
		if why := ValidateStep(st); why != "" {
			t.Errorf("%s: malformed: %s", c.name, why)
		}
		if st.Kind != c.kind || !c.check(st) {
			t.Errorf("%s: got %s", c.name, st.Describe())
		}
	}
}

func TestParseGrandTreeJournalNewestFirst(t *testing.T) {
	lines := []string{"@str@He has asked me to take a bark sample", "@str@I then need to give the message", "@dbl@The King wants me to tell"}
	if st, ok := ParseGrandTreeJournal(lines); !ok || st != GTRelayed {
		t.Fatalf("got %d %v", st, ok)
	}
	if len(GTRoots) != 15 || len(GTPillars) != 4 {
		t.Fatalf("roots %d pillars %d", len(GTRoots), len(GTPillars))
	}
}
