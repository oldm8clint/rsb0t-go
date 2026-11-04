package questrunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// pipParkedOnEssenceByTheOldBuild is the park reason live 14 Oct 2026 (build d5362bb2) wrote 70 times.
const pipParkedOnEssenceByTheOldBuild = `waited 15 ticks on "no unnoted rune essence from the bank, the mule, the Seers market or mining (Aubury's teleport needs Rune Mysteries)" without the world ever saying more`

// setQuestTab colours a quest's tab entry as the server's IF_SETCOLOUR does.
func setQuestTab(t *testing.T, e *Engine, name string, status quests.QuestStatus) {
	t.Helper()
	comID, ok := quests.ComponentID(name)
	if !ok {
		t.Fatalf("no quest tab entry for %q", name)
	}
	var packed uint16
	switch status {
	case quests.NotStarted:
		packed = 0x1f << 10
	case quests.InProgress:
		packed = 0x1f<<10 | 0x1f<<5
	case quests.Complete:
		packed = 0x1f << 5
	}
	w := packet.NewWriter(4)
	w.P2(uint16(comID))
	w.P2(packed)
	e.D.Session.State().Handle(serverprot.IF_SETCOLOUR, w.Data[:w.Pos])
}

// Priest in Peril's snapshot carries whether Rune Mysteries, its soft prerequisite, reads complete;
// a module with no soft prerequisite carries none.
func TestASnapshotCarriesTheSoftPrerequisitesTab(t *testing.T) {
	e := fakeEngine(&logSink{})
	e.quest = Quest{Module: questplan.PriestInPeril}
	e.runStart = time.Now()
	if snap := e.Snapshot(0); snap.QuestDone(questplan.RuneMysteriesID) {
		t.Fatal("an uncoloured Rune Mysteries tab read as complete")
	}
	setQuestTab(t, e, questplan.RuneMysteriesName, quests.InProgress)
	if snap := e.Snapshot(0); snap.QuestDone(questplan.RuneMysteriesID) {
		t.Fatal("a yellow Rune Mysteries tab read as complete")
	}
	setQuestTab(t, e, questplan.RuneMysteriesName, quests.Complete)
	snap := e.Snapshot(0)
	if !snap.QuestDone(questplan.RuneMysteriesID) {
		t.Fatal("a green Rune Mysteries tab did not read as complete")
	}
	// The decision follows: the mine and its pickaxe, not the stalls.
	snap.Journal, snap.Stage, snap.StageKnown = questplan.StatusInProgress, 10, true
	st := questplan.DecidePriestInPeril(snap)
	if st.Kind != questplan.StepProvision || len(st.Kit) != 2 || st.Kit[0].ID != questplan.ObjBronzePickaxe {
		t.Fatalf("Rune Mysteries done: want the mining kit, got %s %+v", st.Describe(), st.Kit)
	}

	e.quest = Quest{Module: questplan.VampireSlayer}
	if got := e.Snapshot(0).QuestsDone; len(got) != 0 {
		t.Fatalf("a quest with no soft prerequisite carries %v", got)
	}
}

// Rune Mysteries completing ends the Priest in Peril park that was waiting on it, in the same ladder
// pass: the account does not sit out the rest of a 2 hour backoff with the mine open.
func TestRuneMysteriesCompletingEndsPriestInPerilsEssencePark(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	done := map[string]bool{}
	e.Store.NotePark(questplan.PriestInPerilID, pipParkedOnEssenceByTheOldBuild, time.Now())
	res := e.RunLadder([]Quest{instantQuest(questplan.RuneMysteriesID, done), instantQuest(questplan.PriestInPerilID, done)}, 20*time.Minute, time.Hour)
	if res.Outcome != LadderDone || !done[questplan.PriestInPerilID] {
		t.Fatalf("outcome %s, done %v (%v)", res.Outcome, done, sink.lines)
	}
	if !sink.has("runemysteries is complete, so the park waiting on it ends") {
		t.Errorf("the park's end is not logged: %v", sink.lines)
	}
	if parked, _ := e.Store.Parked(questplan.PriestInPerilID, time.Now()); parked {
		t.Error("Priest in Peril is still parked")
	}
}

// ... but only a park that was waiting on it, and only once it is complete.
func TestAnEssenceParkIsKeptWhileRuneMysteriesIsNotDoneAndOtherParksAreKept(t *testing.T) {
	// Rune Mysteries busy (its slots taken) and not complete: the pip park stands.
	sink := &logSink{}
	e := fakeEngine(sink)
	done := map[string]bool{}
	e.Store.NotePark(questplan.PriestInPerilID, pipParkedOnEssenceByTheOldBuild, time.Now())
	var holders []*Deps
	for i := 0; i < capacityOf(questplan.RuneMysteriesID); i++ {
		h := &Deps{}
		holders = append(holders, h)
		if !tryHoldQuest(questplan.RuneMysteriesID, h, time.Now()) {
			t.Fatalf("holder %d refused", i)
		}
	}
	res := e.RunLadder([]Quest{instantQuest(questplan.RuneMysteriesID, done), instantQuest(questplan.PriestInPerilID, done)}, 20*time.Minute, time.Hour)
	if res.Outcome != LadderStuck || done[questplan.PriestInPerilID] || done[questplan.RuneMysteriesID] {
		t.Fatalf("outcome %s, done %v (%v)", res.Outcome, done, sink.lines)
	}
	if got := res.Blocked[questplan.RuneMysteriesID]; len(got) != 1 || got[0] != busyReason {
		t.Errorf("Rune Mysteries blocked by %v, want the busy slot", got)
	}
	if got := res.Blocked[questplan.PriestInPerilID]; len(got) != 1 || !strings.HasPrefix(got[0], "parked: ") {
		t.Errorf("Priest in Peril blocked by %v, want its park", got)
	}
	if !WaitingOnSlot(res) {
		t.Error("a ladder waiting only on Rune Mysteries' slot is a wait, not a failure")
	}
	for _, h := range holders {
		releaseQuest(questplan.RuneMysteriesID, h)
	}

	// A different park (a death cap) survives Rune Mysteries completing.
	sink2 := &logSink{}
	e2 := fakeEngine(sink2)
	done2 := map[string]bool{}
	e2.Store.NoteParkFor(questplan.PriestInPerilID, "2 death(s) on \"pip-monk\" inside 24h0m0s (its cap is 2): skipping the quest for 24h0m0s", time.Now(), 24*time.Hour)
	res2 := e2.RunLadder([]Quest{instantQuest(questplan.RuneMysteriesID, done2), instantQuest(questplan.PriestInPerilID, done2)}, 20*time.Minute, time.Hour)
	if res2.Outcome != LadderStuck || done2[questplan.PriestInPerilID] || !done2[questplan.RuneMysteriesID] {
		t.Fatalf("outcome %s, done %v (%v)", res2.Outcome, done2, sink2.lines)
	}
}

// The ladder's order puts Rune Mysteries ahead of Priest in Peril whenever its slot is free, and
// lets Priest in Peril go on (its stages up to the essence need no Rune Mysteries) only while the
// slot is full.
func TestRuneMysteriesRunsBeforePriestInPerilWhenItsSlotIsFree(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	done := map[string]bool{}
	var order []string
	mark := func(id string) Quest {
		q := instantQuest(id, done)
		decide := q.Module.Decide
		q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
			order = append(order, id)
			return decide(s)
		}
		return q
	}
	res := e.RunLadder([]Quest{mark(questplan.RuneMysteriesID), mark(questplan.PriestInPerilID)}, 20*time.Minute, time.Hour)
	if res.Outcome != LadderDone || strings.Join(order, ",") != questplan.RuneMysteriesID+","+questplan.PriestInPerilID {
		t.Fatalf("outcome %s, order %v", res.Outcome, order)
	}
}

// With every Rune Mysteries slot taken, Priest in Peril goes on without it (its stages up to the
// essence need no Rune Mysteries); the ladder reports Rune Mysteries as busy, which is a wait.
func TestPriestInPerilGoesOnWhileRuneMysteriesIsFull(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	done := map[string]bool{}
	var holders []*Deps
	for i := 0; i < capacityOf(questplan.RuneMysteriesID); i++ {
		h := &Deps{}
		holders = append(holders, h)
		if !tryHoldQuest(questplan.RuneMysteriesID, h, time.Now()) {
			t.Fatalf("holder %d refused", i)
		}
	}
	defer func() {
		for _, h := range holders {
			releaseQuest(questplan.RuneMysteriesID, h)
		}
	}()
	res := e.RunLadder([]Quest{instantQuest(questplan.RuneMysteriesID, done), instantQuest(questplan.PriestInPerilID, done)}, 20*time.Minute, time.Hour)
	if !done[questplan.PriestInPerilID] || done[questplan.RuneMysteriesID] {
		t.Fatalf("done %v (%v)", done, sink.lines)
	}
	if res.Outcome != LadderStuck || !WaitingOnSlot(res) {
		t.Fatalf("outcome %s, blocked %v: want stuck on the busy slot", res.Outcome, res.Blocked)
	}
}

// A source the caller rules out (Aubury's mine without Rune Mysteries, the stalls on another world) is
// skipped up front: its executor is never called, the log says why once a round, and the BLOCKED reason
// carries the reason, not "failed this round".
func TestAnUnavailableSourceIsSkippedWithItsReason(t *testing.T) {
	sink := &logSink{}
	e := fakeEngine(sink)
	calls := 0
	const method = questplan.MethodMineEssence
	newProv := func(unavailable func(string) string) *Provisioner {
		p := &Provisioner{
			Methods:           map[string]SkillMethod{method: func(*Deps, questplan.SourceTask) bool { calls++; return false }},
			SourceUnavailable: unavailable,
		}
		p.bankVisitFn = func(d *Deps, bankTile tile.Tile, withdraw []questplan.ItemCount, keep func(id int) bool) bool {
			p.Bank.Note(map[int]int{})
			return true
		}
		return p
	}
	p := newProv(func(m string) string {
		if m == method {
			return "Rune Mysteries is not complete"
		}
		return ""
	})
	kit := []questplan.KitLine{{ID: questplan.ObjRuneEssence, Name: "Rune essence", Qty: 24, Sources: []questplan.Source{
		{Kind: questplan.SourceSkill, Skill: "mining", Method: method, Tile: questplan.AuburyShop},
	}}}
	for round := 1; round <= 2; round++ {
		ok, why := p.Provision(e.D, "pip-essence", kit, nil, tile.Tile{X: 3253, Z: 3420})
		if ok || calls != 0 {
			t.Fatalf("round %d: ok=%v, executor called %d times", round, ok, calls)
		}
		if !p.LastBlocked || p.LastTravelBlocked {
			t.Fatalf("round %d: blocked=%v travelBlocked=%v (an unavailable source is not a failed walk)", round, p.LastBlocked, p.LastTravelBlocked)
		}
		if !strings.Contains(why, "Rune Mysteries is not complete") || strings.Contains(why, "failed this round") {
			t.Fatalf("round %d: reason %q", round, why)
		}
	}
	n := 0
	for _, l := range sink.lines {
		if strings.Contains(l, "is not available to this account: Rune Mysteries is not complete") {
			n++
		}
	}
	if n != 2 { // once a round
		t.Errorf("the unavailable source was announced %d times over two rounds, want 2: %v", n, sink.lines)
	}

	// Without the ruling the same source is worked, as before.
	q := newProv(nil)
	q.Provision(e.D, "pip-essence", kit, nil, tile.Tile{X: 3253, Z: 3420})
	if calls != 1 {
		t.Errorf("a source with no ruling against it was not worked: %d calls", calls)
	}
}
