package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The Elemental Workshop states Mining, Smithing and Crafting 20 in its record, not as hard gates in the ladder's table (a hard gate
// would insert a training rung, and none is run for one filler). The ladder still holds an account short of any of the three: the quest
// is blocked with the reason, the quests after it go on, and at the levels it runs. Hazeel Cult asks for nothing and runs at once.
func TestRunLadderBlocksTheElementalWorkshopBelowItsLevels(t *testing.T) {
	m := questplan.ElementalWorkshop
	id := m.Record.ID
	all := map[string]int{"mining": 20, "smithing": 20, "crafting": 20}
	for _, skill := range []string{"mining", "smithing", "crafting"} {
		below := map[string]int{"mining": 20, "smithing": 20, "crafting": 20}
		below[skill] = 19
		sink := &logSink{}
		e := fakeEngineAt(sink, below)
		done := map[string]bool{}
		res := e.RunLadder([]Quest{recordQuest(m, done), instantQuest("after", done)}, time.Hour, time.Hour)
		if done[id] || sink.has("running "+id) {
			t.Fatalf("%s ran under its %s level: %v", id, skill, sink.lines)
		}
		if !done["after"] || res.Outcome != LadderStuck {
			t.Errorf("%s: the quest behind the held one did not run (outcome %s, done %v)", skill, res.Outcome, done)
		}
		want := "needs " + strings.ToUpper(skill[:1]) + skill[1:] + " 20 (have 19)"
		if got := strings.Join(res.Blocked[id], " | "); !strings.Contains(got, want) {
			t.Errorf("%s: blocked reasons %q lack %q", skill, got, want)
		}
	}
	// At the levels it runs.
	sink := &logSink{}
	e := fakeEngineAt(sink, all)
	done := map[string]bool{}
	if res := e.RunLadder([]Quest{recordQuest(m, done)}, time.Hour, time.Hour); res.Outcome != LadderDone || !done[id] {
		t.Errorf("%s did not run at 20 in all three: outcome %s: %v", id, res.Outcome, sink.lines)
	}
	// Hazeel Cult runs for an account with no levels at all.
	sink = &logSink{}
	e = fakeEngineAt(sink, map[string]int{})
	done = map[string]bool{}
	if res := e.RunLadder([]Quest{recordQuest(questplan.HazeelCult, done)}, time.Hour, time.Hour); res.Outcome != LadderDone || !done[questplan.HazeelCultID] {
		t.Errorf("Hazeel Cult did not run for a level-1 account: outcome %s: %v", res.Outcome, sink.lines)
	}
}

// Neither runs under a fleet-wide slot: nothing in either is shared that one account's visit takes from another (every item is the
// account's own: a vial, a key, a scroll, a bowl, a needle; the machinery's bits are per account; the earth elemental is set on the
// account that mines the rock).
func TestHazeelCultAndTheElementalWorkshopRunUnderNoSlot(t *testing.T) {
	for _, id := range []string{questplan.HazeelCultID, questplan.ElementalWorkshopID} {
		if exclusiveQuests[id] || capacityOf(id) != 1 || slotRepeats[id] {
			t.Errorf("%s: exclusive %v capacity %d repeats %v", id, exclusiveQuests[id], capacityOf(id), slotRepeats[id])
		}
		if !tryHoldQuest(id, &Deps{}, time.Now()) || !tryHoldQuest(id, &Deps{}, time.Now()) {
			t.Errorf("%s refuses a second account", id)
		}
	}
}
