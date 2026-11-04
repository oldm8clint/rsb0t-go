package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Scorpion Catcher on the owner's unproven tail: the Prayer 31 rung ahead of it, the Barcrawl listed BEHIND it (the
// owner's order is "... scorpion, deathplateau, agility35, barcrawl ..."), and the quest itself needing both. Live
// 16 Oct 2026 the ladder did exactly this for the accounts that had both in their list: barcrawl "ended complete" and
// "running scorpion" followed in the same second. These tests pin the three behaviours that made it work, on the
// real record and the real LadderFacts.
func scorpionAccount(prayer int) map[string]int {
	return map[string]int{"attack": 45, "strength": 45, "defence": 40, "hitpoints": 49, "prayer": prayer, "magic": 60, "agility": 35}
}

func lineIndex(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// Below Prayer 31 the quest is held, the rung trains the level, the Barcrawl (listed after the quest) is run while
// the quest waits for it, and the quest runs last, all in one call.
func TestRunLadderTrainsPrayerThenBarcrawlThenScorpionInOneCall(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, scorpionAccount(29))
	st := e.D.Session.State()
	done := map[string]bool{}
	qs := []Quest{
		trainingRung("prayer31", "prayer", 31, st, skillStat(t, "prayer"), done),
		recordQuest(questplan.ScorpionCatcher, done),
		instantQuest("barcrawl", done),
	}
	res := e.RunLadder(qs, time.Hour, time.Hour)
	if res.Outcome != LadderDone || !done["scorpion"] {
		t.Fatalf("outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
	p, b, s := lineIndex(sink.lines, "running prayer31"), lineIndex(sink.lines, "running barcrawl"), lineIndex(sink.lines, "running scorpion")
	if p < 0 || b < p || s < b {
		t.Errorf("order: prayer31 %d, barcrawl %d, scorpion %d, want the rung, the Barcrawl, the quest: %v", p, b, s, sink.lines)
	}
}

// With the rung NOT in the list (an operator's explicit list) a Prayer 29 account never runs the quest, and the log
// says which rung would train it; the Barcrawl behind it still runs.
func TestRunLadderHoldsScorpionBelowPrayer31AndSaysTheRungIsMissing(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, scorpionAccount(29))
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.ScorpionCatcher, done), instantQuest("barcrawl", done)}, time.Hour, time.Hour)
	if done["scorpion"] || res.Outcome != LadderStuck || !done["barcrawl"] {
		t.Fatalf("outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
	if !sink.has("[ladder] scorpion is held back, not run: needs Prayer 31 (have 29; server gate: the Edgeville Monastery upstairs") ||
		!sink.has("trained by prayer31 (not in this ladder)") {
		t.Errorf("the Prayer 31 hold is not in the log: %v", sink.lines)
	}
}

// At Prayer 31 with the Barcrawl still to do, the quest waits for the Barcrawl by name (it has no quest tab, so a
// ladder without it can never mark it complete), and runs the pass the Barcrawl finishes in.
func TestRunLadderScorpionWaitsForTheBarcrawlByName(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, scorpionAccount(38))
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.ScorpionCatcher, done)}, time.Hour, time.Hour)
	if res.Outcome != LadderStuck || done["scorpion"] || !strings.Contains(strings.Join(res.Blocked["scorpion"], ";"), "prerequisite quest not complete: barcrawl") {
		t.Fatalf("scorpion without the Barcrawl: outcome %s, blocked %v, done %v", res.Outcome, res.Blocked, done)
	}
	done = map[string]bool{}
	res = e.RunLadder([]Quest{recordQuest(questplan.ScorpionCatcher, done), instantQuest("barcrawl", done)}, time.Hour, time.Hour)
	if res.Outcome != LadderDone || !done["barcrawl"] || !done["scorpion"] {
		t.Fatalf("scorpion then barcrawl at Prayer 38: outcome %s, done %v: %v", res.Outcome, done, sink.lines)
	}
}
