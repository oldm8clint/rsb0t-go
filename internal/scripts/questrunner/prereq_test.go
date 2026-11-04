package questrunner

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// grail-empty-1: a ladder of The Holy Grail alone must still read Merlin's Crystal's tab.
func TestPrereqsOutsideTheLadderAreRead(t *testing.T) {
	grail := Quest{Module: questplan.HolyGrail}
	merlin := Quest{Module: questplan.MerlinsCrystal}
	if len(questplan.HolyGrail.Record.Requirements.Quests) == 0 {
		t.Fatal("The Holy Grail's record names no prerequisite")
	}
	if got := prereqsOutside([]Quest{grail}, map[string]bool{}); !reflect.DeepEqual(got, []string{questplan.MerlinsCrystalID}) {
		t.Errorf("grail alone: %v", got)
	}
	if got := prereqsOutside([]Quest{merlin, grail}, map[string]bool{}); len(got) != 0 {
		t.Errorf("merlin in the ladder is read as a ladder quest, not again: %v", got)
	}
	if got := prereqsOutside([]Quest{grail}, map[string]bool{questplan.MerlinsCrystalID: true}); len(got) != 0 {
		t.Errorf("an already-complete prerequisite is not read again: %v", got)
	}
	if questplan.LadderFacts[questplan.MerlinsCrystalID].Name != questplan.MerlinsCrystalRecord.Name {
		t.Errorf("LadderFacts names merlin %q, the record %q", questplan.LadderFacts[questplan.MerlinsCrystalID].Name, questplan.MerlinsCrystalRecord.Name)
	}
}

// A ladder fact's hard prerequisite holds a quest back though its server record names none: Horror
// from the Deep waits for Barcrawl (the Barbarian Outpost gate), and runs once Barcrawl is done.
func TestLadderFactPrereqsHoldAQuestBack(t *testing.T) {
	if len(questplan.LadderFacts["horror"].Prereqs) == 0 {
		t.Fatal("Horror from the Deep has no ladder prerequisite")
	}
	done := map[string]bool{}
	e := fakeEngine(&logSink{})
	res := e.RunLadder([]Quest{instantQuest("horror", done)}, time.Minute, time.Minute)
	if res.Outcome != LadderStuck || done["horror"] || !strings.Contains(strings.Join(res.Blocked["horror"], ";"), "prerequisite quest not complete: barcrawl") {
		t.Fatalf("horror without barcrawl: outcome %s, blocked %v, ran %v", res.Outcome, res.Blocked, done["horror"])
	}
	res = e.RunLadder([]Quest{instantQuest("barcrawl", done), instantQuest("horror", done)}, time.Minute, time.Hour)
	if res.Outcome != LadderDone || !done["barcrawl"] || !done["horror"] {
		t.Fatalf("barcrawl then horror: outcome %s, blocked %v, done %v", res.Outcome, res.Blocked, done)
	}
}
