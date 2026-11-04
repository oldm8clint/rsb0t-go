package questplan

import (
	"strings"
	"testing"
)

// rmJournalLines are the rendered journal of each stage, line for line as
// runemysteries_journal.rs2 builds them (the '|' is the client's own line break).
func rmJournalLines(stage int) []string {
	const (
		duke     = "@str@I spoke to Duke Horacio and he showed me a strange|@str@talisman that had been found by one of his subjects.|"
		tower    = "@str@I agreed to take it to the Wizards' Tower, South West of|@str@Lumbridge for further examination by the wizards.|"
		gave     = "@str@I gave the Talisman to the Head Wizard of the Tower and|@str@agreed to help him with his research into rune stones.|"
		pkg      = "@str@I took the research package to Varrock and delivered it.|"
		notes    = "@str@Aubury was interested in the research package and gave|@str@me his own research notes to deliver to Sedridor.|"
		finished = "@str@I delivered the notes to Sedridor. As a thank you|@str@for my help, he granted me permission to use the Rune|@str@Essence Mine whenever I please.||"
	)
	var text string
	switch stage {
	case 0:
		text = "@dbl@I can start this quest by speaking to @dre@Duke Horacio of Lumbridge@dbl@, upstairs in @dre@Lumbridge Castle@dbl@"
	case RMStarted:
		text = duke + tower + "@dbl@I need to find the @dre@Head Wizard@dbl@ and give him the @dre@Talisman"
	case RMGivenTalisman:
		text = duke + tower + gave + "@dbl@I should talk to the @dre@Head Wizard@dbl@ again"
	case RMReceivedPackage:
		text = duke + tower + gave + "@dbl@I should take this @dre@Research Package@dbl@ to @dre@Aubury@dbl@ in @dre@Varrock"
	case RMGivenPackage:
		text = duke + tower + gave + pkg + "@dbl@I should speak to @dre@Aubury@dbl@ again when he has finished examining the @dre@research package@dbl@ I have delivered him."
	case RMReceivedNotes:
		text = duke + tower + gave + pkg + notes + "@dbl@I should take the @dre@notes@dbl@ to @dre@Sedridor@dbl@ and see what he says"
	default:
		text = duke + tower + gave + pkg + notes + finished + "@red@QUEST COMPLETE!"
	}
	return strings.Split(text, "|")
}

// Every stage's journal reads as that stage, wrapped or not: the earlier stages' struck-through
// lines stay in the journal and must not make a later stage read as an earlier one.
func TestParseRuneMysteriesJournalReadsEveryStage(t *testing.T) {
	for stage := 0; stage <= RMComplete; stage++ {
		lines := rmJournalLines(stage)
		got, ok := ParseRuneMysteriesJournal(lines)
		if !ok || got != stage {
			t.Errorf("stage %d: read %d (%v)", stage, got, ok)
		}
		// The client wraps a long line at the interface's width: the same text split mid-sentence.
		wrapped := strings.Fields(strings.Join(lines, " "))
		var rewrapped []string
		for i := 0; i < len(wrapped); i += 6 {
			end := i + 6
			if end > len(wrapped) {
				end = len(wrapped)
			}
			rewrapped = append(rewrapped, strings.Join(wrapped[i:end], " "))
		}
		if got, ok := ParseRuneMysteriesJournal(rewrapped); !ok || got != stage {
			t.Errorf("stage %d wrapped: read %d (%v)", stage, got, ok)
		}
	}
	if _, ok := ParseRuneMysteriesJournal(nil); ok {
		t.Error("an empty journal read as a stage")
	}
	if _, ok := ParseRuneMysteriesJournal([]string{"Some other quest entirely"}); ok {
		t.Error("an unrecognised journal read as a stage")
	}
}

func TestRuneMysteriesModuleReadsItsStageAndKeepsItsItemsByID(t *testing.T) {
	if RuneMysteries.ReadStage == nil {
		t.Fatal("the module does not read its stage")
	}
	if len(RuneMysteries.ToolIDs) != 3 {
		t.Fatalf("tool ids %v", RuneMysteries.ToolIDs)
	}
}

// rmAt is a Rune Mysteries snapshot at a read stage with nothing in the pack.
func rmAt(stage int) Snapshot {
	return Snapshot{Journal: StatusInProgress, Stage: stage, StageKnown: true,
		Inv: map[string]int{}, InvIDs: map[int]int{}, Worn: map[string]int{}, WornIDs: map[int]int{},
		BankIDs: map[int]int{}, Bank: map[string]int{}}
}

func rmStep(t *testing.T, s Snapshot) Step {
	t.Helper()
	st := DecideRuneMysteries(s)
	if why := ValidateStep(st); why != "" {
		t.Fatalf("malformed %s: %s", st.Describe(), why)
	}
	return st
}

// Live 14 Oct 2026: an in-progress account with the package or the notes in its BANK walked
// Aubury, Sedridor and the Duke in turn and never moved the quest, holding a fleet-wide slot.
// With the stage read, the item is drawn from the bank and the right NPC is asked first.
func TestRuneMysteriesDrawsABankedItemBeforeTalking(t *testing.T) {
	cases := []struct {
		stage   int
		item    int
		name    string
		withNpc string
	}{
		{RMStarted, ObjAirTalisman, AirTalisman, "Sedridor"},
		{RMReceivedPackage, ObjResearchPackage, ResearchPackage, "Aubury"},
		{RMReceivedNotes, ObjResearchNotes, ResearchNotes, "Sedridor"},
	}
	for _, c := range cases {
		s := rmAt(c.stage)
		// The bank has not been read this run: read it.
		if st := rmStep(t, s); st.Kind != StepScanBank || st.Bank != RuneMysteriesBank {
			t.Fatalf("stage %d, unread bank: want a bank scan, got %s", c.stage, st.Describe())
		}
		// Read, and the item is in it: take it out.
		s.BankKnown = true
		s.BankIDs[c.item] = 1
		st := rmStep(t, s)
		if st.Kind != StepWithdraw || len(st.Items) != 1 || st.Items[0].ID != c.item || st.Bank != RuneMysteriesBank {
			t.Fatalf("stage %d, item banked: want its withdrawal, got %s", c.stage, st.Describe())
		}
		// In the pack: on to the NPC the stage names.
		delete(s.BankIDs, c.item)
		s.InvIDs[c.item] = 1
		if st := rmStep(t, s); st.Kind != StepTalk || st.Stop.Npc != c.withNpc {
			t.Fatalf("stage %d, item held: want %s, got %s", c.stage, c.withNpc, st.Describe())
		}
	}
}

// An item that is nowhere (pack, bank, worn) is re-issued by the NPC the content names: the Duke
// for the talisman, Sedridor for the package, Aubury for the notes.
func TestRuneMysteriesAsksTheNpcWhoReissuesAnItemThatIsNowhere(t *testing.T) {
	cases := []struct {
		stage int
		npc   string
	}{
		{RMStarted, "Duke Horacio"},
		{RMReceivedPackage, "Sedridor"},
		{RMReceivedNotes, "Aubury"},
	}
	for _, c := range cases {
		s := rmAt(c.stage)
		s.BankKnown = true
		if st := rmStep(t, s); st.Kind != StepTalk || st.Stop.Npc != c.npc {
			t.Errorf("stage %d, item nowhere: want %s, got %s", c.stage, c.npc, st.Describe())
		}
	}
}

// The stages that need nothing in the pack never touch the bank, and never probe.
func TestRuneMysteriesStagesThatNeedNoItemGoStraightToTheirNpc(t *testing.T) {
	for stage, npc := range map[int]string{RMGivenTalisman: "Sedridor", RMGivenPackage: "Aubury"} {
		s := rmAt(stage)
		for noProgress := 0; noProgress < 4; noProgress++ {
			s.NoProgress = noProgress
			if st := rmStep(t, s); st.Kind != StepTalk || st.Stop.Npc != npc {
				t.Errorf("stage %d, no-progress %d: want %s, got %s", stage, noProgress, npc, st.Describe())
			}
		}
	}
}

// A journal that could not be read keeps the old behaviour: the held items, then the probes.
func TestRuneMysteriesWithoutAStageKeepsTheProbes(t *testing.T) {
	s := rmAt(RMReceivedNotes)
	s.StageKnown = false
	if st := rmStep(t, s); st.Kind != StepTalk || st.Stop.Npc != "Aubury" {
		t.Fatalf("stage unread, empty pack: the first probe is Aubury, got %s", st.Describe())
	}
	s.Inv[ResearchNotes] = 1
	if st := rmStep(t, s); st.Kind != StepTalk || st.Stop.Npc != "Sedridor" {
		t.Fatalf("stage unread, notes held: Sedridor, got %s", st.Describe())
	}
}
