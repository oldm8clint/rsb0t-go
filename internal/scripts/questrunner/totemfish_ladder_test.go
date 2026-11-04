package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// Tribal Totem (Thieving 21) and the Fishing Contest (Fishing 10) state their skill gate in their records, not in the ladder's table (a
// gate in the table needs a training rung and none trains Thieving or Fishing). The ladder still holds an account short of the level:
// the quest is blocked with the reason, the quests after it go on, and at the level it runs.
func TestRunLadderBlocksTheTotemAndTheFishingContestBelowTheirLevels(t *testing.T) {
	for _, c := range []struct {
		m      questplan.Module
		skill  string
		level  int
		reason string
	}{
		{questplan.TribalTotem, "thieving", 21, "needs Thieving 21 (have 20)"},
		{questplan.FishingContest, "fishing", 10, "needs Fishing 10 (have 9)"},
	} {
		id := c.m.Record.ID
		below := map[string]int{c.skill: c.level - 1}
		sink := &logSink{}
		e := fakeEngineAt(sink, below)
		done := map[string]bool{}
		res := e.RunLadder([]Quest{recordQuest(c.m, done), instantQuest("after", done)}, time.Hour, time.Hour)
		if done[id] || sink.has("running "+id) {
			t.Fatalf("%s ran under its level: %v", id, sink.lines)
		}
		if !done["after"] || res.Outcome != LadderStuck {
			t.Errorf("%s: the quest behind the held one did not run (outcome %s, done %v)", id, res.Outcome, done)
		}
		if got := strings.Join(res.Blocked[id], " | "); !strings.Contains(got, c.reason) {
			t.Errorf("%s: blocked reasons %q lack %q", id, got, c.reason)
		}
		// At the level it runs.
		sink = &logSink{}
		e = fakeEngineAt(sink, map[string]int{c.skill: c.level})
		done = map[string]bool{}
		if res = e.RunLadder([]Quest{recordQuest(c.m, done)}, time.Hour, time.Hour); res.Outcome != LadderDone || !done[id] {
			t.Errorf("%s did not run at %s %d: outcome %s: %v", id, c.skill, c.level, res.Outcome, sink.lines)
		}
	}
}

// The Fishing Contest runs under a fleet-wide slot of four, held until the account is seated at the pipes (the Sinister Stranger is
// the one shared step), and the step comes round again on a later run (a quit resets the seat), so the release is never remembered for
// good. Tribal Totem runs under none: nothing in it is shared.
func TestTheFishingContestRunsUnderASlotOfFourAndTheTotemUnderNone(t *testing.T) {
	id := questplan.FishingContestID
	if !exclusiveQuests[id] || capacityOf(id) != 4 || !slotRepeats[id] {
		t.Errorf("%s: exclusive %v capacity %d repeats %v, want true, 4 and true", id, exclusiveQuests[id], capacityOf(id), slotRepeats[id])
	}
	if exclusiveQuests[questplan.TribalTotemID] || capacityOf(questplan.TribalTotemID) != 1 {
		t.Errorf("Tribal Totem is exclusive (%v): nothing in it is shared", exclusiveQuests[questplan.TribalTotemID])
	}
	now := time.Now()
	ds := make([]*Deps, 6)
	for i := range ds {
		ds[i] = &Deps{}
	}
	for i := 0; i < 4; i++ {
		if !tryHoldQuest(id, ds[i], now) {
			t.Fatalf("account %d of 4 was refused", i+1)
		}
	}
	if tryHoldQuest(id, ds[4], now) {
		t.Errorf("a fifth account was admitted")
	}
	releaseQuest(id, ds[0])
	if !tryHoldQuest(id, ds[4], now) {
		t.Errorf("a released slot was not given to the fifth account")
	}
	for _, d := range ds {
		releaseQuest(id, d)
	}
	if _, held := slots[id]; held {
		t.Errorf("slots left behind: %v", slots[id])
	}
	if !tryHoldQuest(questplan.TribalTotemID, ds[0], now) || !tryHoldQuest(questplan.TribalTotemID, ds[1], now) {
		t.Errorf("Tribal Totem refused an account")
	}
}
