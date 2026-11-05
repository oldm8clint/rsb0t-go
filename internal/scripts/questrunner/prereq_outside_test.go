package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// N0 of docs/quest_selfsufficiency_plan_2026-10-19.md: a prerequisite the ladder does not list and that has no quest tab.
//
// The tail tier lists the unproven quests, and Scorpion Catcher and Horror from the Deep (both wait for the Barcrawl) are in it
// while the Barcrawl is proven and so in the head. The tail's ladder then met "barcrawl" as a prerequisite outside its list, read
// the tab of "Alfred Grimhand's Barcrawl" (there is none: the miniquest's varp is not transmitted) and held both quests at
// "prerequisite quest not complete: barcrawl" on 24 to 51 accounts a day, 1,911 and 1,655 events in 15 days, whether or not the
// head had finished the crawl. These tests pin the ways it is read now, on the real LadderFacts.

// barcrawlLike is a registered quest with no tab whose Done reads the side store, as questmods.Barcrawl's does.
func barcrawlLike() Quest {
	return Quest{
		Module: questplan.Module{Record: questplan.Record{ID: "barcrawl", Name: "Alfred Grimhand's Barcrawl"}},
		Done: func(e *Engine) bool {
			r := e.Store.Quests["barcrawl"]
			return r != nil && r.SeenComplete
		},
	}
}

func registryOf(counts map[string]int, quests ...Quest) func(string) (Quest, bool) {
	byID := map[string]Quest{}
	for _, q := range quests {
		byID[q.ID()] = q
	}
	return func(id string) (Quest, bool) {
		counts[id]++
		q, ok := byID[id]
		return q, ok
	}
}

func countLines(sink *logSink, sub string) int {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	n := 0
	for _, l := range sink.lines {
		if strings.Contains(l, sub) {
			n++
		}
	}
	return n
}

// The tail's ladder: Horror alone, the Barcrawl done and recorded by the head. Through the registry the prerequisite reads
// complete from the Barcrawl's own Done, the quest runs, and the line that says how is logged once however many times the ladder
// looks.
func TestATablessPrerequisiteOutsideTheLadderIsReadThroughItsOwnDone(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	calls := map[string]int{}
	e.QuestFor = registryOf(calls, barcrawlLike())
	done := map[string]bool{}

	res := e.RunLadder([]Quest{instantQuest("horror", done)}, time.Minute, time.Minute)
	if res.Outcome != LadderStuck || done["horror"] || !strings.Contains(strings.Join(res.Blocked["horror"], ";"), "prerequisite quest not complete: barcrawl") {
		t.Fatalf("the Barcrawl is not done yet: outcome %s, blocked %v, ran %v", res.Outcome, res.Blocked, done["horror"])
	}
	if sink.has("has no quest tab: read complete") {
		t.Errorf("an incomplete Barcrawl was logged as read complete: %v", sink.lines)
	}

	e.Store.Record("barcrawl").SeenComplete = true // the head's Barcrawl run, which the tail's ladder cannot see
	res = e.RunLadder([]Quest{instantQuest("horror", done)}, time.Minute, time.Minute)
	if res.Outcome != LadderDone || !done["horror"] {
		t.Fatalf("the Barcrawl is done: outcome %s, blocked %v, ran %v: %v", res.Outcome, res.Blocked, done["horror"], sink.lines)
	}
	if strings.Contains(strings.Join(res.Blocked["horror"], ";"), "barcrawl") {
		t.Errorf("Horror is still blocked on the Barcrawl: %v", res.Blocked)
	}
	if n := countLines(sink, "[ladder] barcrawl is not in this ladder and has no quest tab: read complete from its own Done"); n != 1 {
		t.Errorf("the read-complete line was logged %d times, want once: %v", n, sink.lines)
	}
	if calls["barcrawl"] != 1 {
		t.Errorf("the registry was asked for the Barcrawl %d times, want it built once and kept", calls["barcrawl"])
	}
}

// The same on the real Scorpion Catcher record and a Prayer 38 account (scorpionladder_test.go's third test, whose first half is
// the ladder with no way to read the Barcrawl).
func TestScorpionCatcherRunsOnATailLadderOnceTheBarcrawlIsRecorded(t *testing.T) {
	sink := &logSink{}
	e := fakeEngineAt(sink, scorpionAccount(38))
	e.QuestFor = registryOf(map[string]int{}, barcrawlLike())
	e.Store.Record("barcrawl").SeenComplete = true
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.ScorpionCatcher, done)}, time.Hour, time.Hour)
	if res.Outcome != LadderDone || !done["scorpion"] {
		t.Fatalf("outcome %s, blocked %v, done %v: %v", res.Outcome, res.Blocked, done, sink.lines)
	}
}

// No registry (a harness engine): a prerequisite with no tab is read from the side store's record of the verdict, and the read
// creates no record of its own in a store that is saved to disk.
func TestATablessPrerequisiteWithoutARegistryIsReadFromTheStore(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	done := map[string]bool{}

	res := e.RunLadder([]Quest{instantQuest("horror", done)}, time.Minute, time.Minute)
	if res.Outcome != LadderStuck || done["horror"] {
		t.Fatalf("no record of the Barcrawl: outcome %s, ran %v", res.Outcome, done["horror"])
	}
	if r := e.Store.Quests["barcrawl"]; r != nil {
		t.Errorf("reading the prerequisite created a store record: %+v", *r)
	}

	e.Store.Record("barcrawl").SeenComplete = true
	res = e.RunLadder([]Quest{instantQuest("horror", done)}, time.Minute, time.Minute)
	if res.Outcome != LadderDone || !done["horror"] {
		t.Fatalf("the store says the crawl is done: outcome %s, blocked %v", res.Outcome, res.Blocked)
	}
	if !sink.has("[ladder] barcrawl is not in this ladder and has no quest tab: read complete from the side store's record") {
		t.Errorf("the line that says how is missing: %v", sink.lines)
	}
}

// A registry that does not know the quest, or whose quest has no Done, falls back to the store the same way.
func TestTheRegistryFallsBackToTheStoreForAQuestWithNoDone(t *testing.T) {
	for name, reg := range map[string]func(string) (Quest, bool){
		"unknown to the registry": func(string) (Quest, bool) { return Quest{}, false },
		"registered, no Done": func(id string) (Quest, bool) {
			return Quest{Module: questplan.Module{Record: questplan.Record{ID: id}}}, true
		},
	} {
		e := fakeEngine(&logSink{})
		e.QuestFor = reg
		e.Store.Record("barcrawl").SeenComplete = true
		if !e.priorDone("barcrawl") {
			t.Errorf("%s: the store's record of the verdict is not read", name)
		}
		e = fakeEngine(&logSink{})
		e.QuestFor = reg
		if e.priorDone("barcrawl") {
			t.Errorf("%s: a quest nothing recorded reads complete", name)
		}
	}
}

// A quest with a tab is read from the tab, the server's verdict: neither the store's hint nor a registry Done makes The Holy
// Grail's prerequisite complete (the store's SeenComplete is "never a gate", questplan/store.go).
func TestAQuestWithATabIsStillReadOnlyFromItsTab(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	merlin := Quest{
		Module: questplan.MerlinsCrystal,
		Done:   func(*Engine) bool { return true },
	}
	e.QuestFor = registryOf(map[string]int{}, merlin)
	e.Store.Record(questplan.MerlinsCrystalID).SeenComplete = true
	if !HasQuestTab(questplan.MerlinsCrystalRecord.Name) {
		t.Fatalf("Merlin's Crystal has no quest tab in internal/quests")
	}
	done := map[string]bool{}
	res := e.RunLadder([]Quest{recordQuest(questplan.HolyGrail, done)}, time.Minute, time.Minute)
	if res.Outcome != LadderStuck || done["grail"] || !strings.Contains(strings.Join(res.Blocked["grail"], ";"), "prerequisite quest not complete: "+questplan.MerlinsCrystalID) {
		t.Fatalf("Grail past a Merlin whose tab does not read complete: outcome %s, blocked %v, ran %v", res.Outcome, res.Blocked, done["grail"])
	}
}

// The Barcrawl is the one quest the ladder facts make a prerequisite that has no tab; every other has one, so a new tab-less
// prerequisite fails here until it has a Done and a registry entry (internal/bot's TestEveryTablessPrerequisiteIsInTheRegistry).
func TestOnlyTheBarcrawlIsAPrerequisiteWithNoQuestTab(t *testing.T) {
	if HasQuestTab(questplan.LadderFacts["barcrawl"].Name) {
		t.Fatalf("the Barcrawl has a quest tab now: the tab-less read in engine.priorDone is no longer needed")
	}
	for _, f := range questplan.LadderFacts {
		for _, p := range f.Prereqs {
			if p != "barcrawl" && !HasQuestTab(questplan.QuestTabName(p)) {
				t.Errorf("%s waits for %s, which has no quest tab (%q): give it a Done and a registry entry", f.ID, p, questplan.QuestTabName(p))
			}
		}
	}
}
