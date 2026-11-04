package questplan

import "testing"

func ghostSnap(journal Status) Snapshot {
	return Snapshot{Journal: journal, InvIDs: map[int]int{}, WornIDs: map[int]int{}, FreeSlots: 20,
		BankKnown: true, BankIDs: map[int]int{}}
}

func TestRestlessGhostEveryStage(t *testing.T) {
	cases := []struct {
		name string
		snap func() Snapshot
		kind StepKind
		want string // npc, custom name, or "" for any
	}{
		{"complete", func() Snapshot { return ghostSnap(StatusComplete) }, StepDone, ""},
		{"unknown waits", func() Snapshot { return ghostSnap(StatusUnknown) }, StepWait, ""},
		{"not started: Aereck", func() Snapshot { return ghostSnap(StatusNotStarted) }, StepTalk, "Father Aereck"},
		{"no amulet anywhere: Urhney", func() Snapshot { return ghostSnap(StatusInProgress) }, StepTalk, "Father Urhney"},
		{"amulet held: wear it", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.InvIDs[ObjGhostspeak] = 1
			return s
		}, StepEquip, ""},
		{"amulet banked: withdraw", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.BankIDs[ObjGhostspeak] = 1
			return s
		}, StepWithdraw, ""},
		{"bank unread: scan it before asking Urhney again", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.BankKnown = false
			return s
		}, StepScanBank, ""},
		{"worn, stage 2: the ghost", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			s.Stage, s.StageKnown = GhostSpokeUrhny, true
			return s
		}, StepCustom, CustomGhostTalk},
		{"worn, stage unknown: the ghost", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			return s
		}, StepCustom, CustomGhostTalk},
		{"worn, stage 3: the skull", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			s.Stage, s.StageKnown = GhostSpokeGhost, true
			return s
		}, StepCustom, CustomGhostSkull},
		{"stage 4 with the skull lost: re-take it", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			s.Stage, s.StageKnown = GhostHasSkull, true
			return s
		}, StepCustom, CustomGhostSkull},
		{"a full pack at the skull banks first", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			s.Stage, s.StageKnown = GhostSpokeGhost, true
			s.FreeSlots = 0
			return s
		}, StepDeposit, ""},
		{"skull held: the coffin", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.InvIDs[ObjGhostSkull] = 1
			return s
		}, StepCustom, CustomGhostReturn},
		{"skull banked: withdraw it", func() Snapshot {
			s := ghostSnap(StatusInProgress)
			s.WornIDs[ObjGhostspeak] = 1
			s.BankIDs[ObjGhostSkull] = 1
			return s
		}, StepWithdraw, ""},
	}
	for _, c := range cases {
		step := DecideRestlessGhost(c.snap())
		if why := ValidateStep(step); why != "" {
			t.Errorf("%s: malformed step: %s", c.name, why)
		}
		if step.Kind != c.kind {
			t.Errorf("%s: got %s (%s), want %s", c.name, step.Kind, step.Describe(), c.kind)
			continue
		}
		switch step.Kind {
		case StepTalk:
			if step.Stop.Npc != c.want {
				t.Errorf("%s: talked to %s", c.name, step.Stop.Npc)
			}
		case StepCustom:
			if step.Custom != c.want {
				t.Errorf("%s: custom %s", c.name, step.Custom)
			}
		}
	}
}

func TestParseRestlessGhostJournalAgainstTheContentText(t *testing.T) {
	// priest_journal.rs2's own strings, as the client renders them.
	stage3 := []string{
		"@str@Father Aereck asked me to help him deal with the Ghost in",
		"@str@the graveyard next to the church.",
		"@str@I found Father Urhney in the swamp south of Lumbridge. He",
		"@str@gave me an Amulet of Ghostspeak to talk to the ghost.",
		"@str@I spoke to the Ghost and he told me he could not rest in",
		"@str@peace because an evil wizard had stolen his skull.",
		"@dbl@I should go and search the @dre@Wizard's Tower South West of",
		"@dre@Lumbridge@dbl@ for the @dre@Ghost's Skull@dbl@.",
	}
	if st, ok := ParseRestlessGhostJournal(stage3); !ok || st != GhostSpokeGhost {
		t.Fatalf("stage %d %v", st, ok)
	}
	stage1 := []string{"@str@Father Aereck asked me to help him deal with the Ghost in",
		"@dbl@I should find @dre@Father Urhney@dbl@ who is an expert on @dre@ghosts@dbl@."}
	if st, ok := ParseRestlessGhostJournal(stage1); !ok || st != GhostStarted {
		t.Fatalf("stage %d %v", st, ok)
	}
	if _, ok := ParseRestlessGhostJournal(nil); ok {
		t.Fatal("an empty journal is unknown")
	}
}

// The fleet-wide slot is held until the skull is in hand; an account still at the priests or the ghost
// is not past it, and neither is one whose tab is unread.
func TestGhostPastSkull(t *testing.T) {
	at := func(j Status, stage int, known bool, skull int) Snapshot {
		return Snapshot{Journal: j, Stage: stage, StageKnown: known, InvIDs: map[int]int{ObjGhostSkull: skull}}
	}
	for _, c := range []struct {
		name string
		s    Snapshot
		want bool
	}{
		{"not started", at(StatusNotStarted, 0, false, 0), false},
		{"spoke to Urhney", at(StatusInProgress, GhostSpokeUrhny, true, 0), false},
		{"to the basement", at(StatusInProgress, GhostHasSkull-1, true, 0), false},
		{"unread journal", at(StatusInProgress, 0, false, 0), false},
		{"skull in hand", at(StatusInProgress, GhostHasSkull-1, true, 1), true},
		{"journal at the return", at(StatusInProgress, GhostHasSkull, true, 0), true},
		{"complete", at(StatusComplete, 0, false, 0), true},
	} {
		if got := GhostPastSkull(c.s); got != c.want {
			t.Errorf("%s: GhostPastSkull = %v, want %v", c.name, got, c.want)
		}
	}
}
