package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// A Priest in Peril stand-in for the one-at-a-time slot: the journal reads stage 5 (the guardian
// long dead) and the module says so through ExclusiveDone.
func pastGuardianQuest(done map[string]bool, probe func()) Quest {
	q := instantQuest(questplan.PriestInPerilID, done)
	q.Module.ReadStage = nil
	q.Module.ExclusiveDone = func(questplan.Snapshot) bool { return true }
	q.Module.Decide = func(questplan.Snapshot) questplan.Step {
		if probe != nil {
			probe()
		}
		return questplan.Step{Kind: questplan.StepDone}
	}
	return q
}

// With narrow slots the account that has passed the guardian gives the slot up on its first
// decide pass, so another account can take it while the first is still mid-quest; without them
// it keeps the slot to the end of its run, as before. The hint is stored, and the next ladder
// pass does not queue for it.
func TestAnAccountPastTheSharedStepFreesTheSlotForTheNextOne(t *testing.T) {
	for _, narrow := range []bool{true, false} {
		a := fakeEngine(&logSink{})
		a.NarrowSlots = narrow
		other := &Deps{}
		var otherGot bool
		probe := func() { otherGot = tryHoldQuest(questplan.PriestInPerilID, other, time.Now()) }
		done := map[string]bool{}
		res := a.RunLadder([]Quest{pastGuardianQuest(done, probe)}, time.Hour, time.Hour)
		releaseQuest(questplan.PriestInPerilID, other)
		if res.Outcome != LadderDone {
			t.Fatalf("narrow=%v: outcome %s", narrow, res.Outcome)
		}
		if otherGot != narrow {
			t.Errorf("narrow=%v: a second account took the slot mid-run: %v", narrow, otherGot)
		}
		hinted := a.Store.Quests[questplan.PriestInPerilID] != nil && a.Store.Quests[questplan.PriestInPerilID].Hints[HintSlotDone]
		if hinted != narrow {
			t.Errorf("narrow=%v: the slot-done hint is %v", narrow, hinted)
		}
	}
}

// An account that is already past the step does not queue for the slot at all, even while another
// account holds it; one that is not still does.
func TestAnAccountPastTheSharedStepDoesNotQueue(t *testing.T) {
	holder := &Deps{}
	if !tryHoldQuest(questplan.PriestInPerilID, holder, time.Now()) {
		t.Fatal("the test could not take the slot")
	}
	defer releaseQuest(questplan.PriestInPerilID, holder)

	// Not past it: busy, as before.
	wait := fakeEngine(&logSink{})
	wait.NarrowSlots = true
	done := map[string]bool{}
	res := wait.RunLadder([]Quest{pastGuardianQuest(done, nil)}, time.Hour, time.Hour)
	if res.Outcome != LadderDone {
		// ExclusiveDone is true for the stand-in, but the slot was needed to start: it must have
		// been refused, not run.
		t.Logf("outcome %s", res.Outcome)
	}
	if done[questplan.PriestInPerilID] {
		t.Fatal("an account that had not passed the guardian ran its quest while another held the slot")
	}
	if !WaitingOnSlot(res) {
		t.Errorf("it should read as waiting on the slot: %v %v", res.Outcome, res.Blocked)
	}

	// Past it (the hint is in its store): no queue.
	past := fakeEngine(&logSink{})
	past.NarrowSlots = true
	rec := past.Store.Record(questplan.PriestInPerilID)
	rec.Hints = map[string]bool{HintSlotDone: true}
	done2 := map[string]bool{}
	res2 := past.RunLadder([]Quest{pastGuardianQuest(done2, nil)}, time.Hour, time.Hour)
	if res2.Outcome != LadderDone || !done2[questplan.PriestInPerilID] {
		t.Fatalf("an account past the guardian waited on a slot it did not need: %s %v", res2.Outcome, res2.Blocked)
	}
	// With narrow slots off the hint is ignored.
	off := fakeEngine(&logSink{})
	off.Store.Record(questplan.PriestInPerilID).Hints = map[string]bool{HintSlotDone: true}
	done3 := map[string]bool{}
	res3 := off.RunLadder([]Quest{pastGuardianQuest(done3, nil)}, time.Hour, time.Hour)
	if done3[questplan.PriestInPerilID] || !WaitingOnSlot(res3) {
		t.Errorf("with narrow slots off the hint must be ignored: ran=%v %v %v", done3[questplan.PriestInPerilID], res3.Outcome, res3.Blocked)
	}
}

// The module's predicate: stage 3 (the report to King Roald after the kill) and beyond, or the
// quest complete; never earlier, never on an unread journal.
func TestPipPastGuardianPredicate(t *testing.T) {
	cases := []struct {
		name string
		s    questplan.Snapshot
		want bool
	}{
		{"not started", questplan.Snapshot{Journal: questplan.StatusNotStarted}, false},
		{"stage 1", questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: 1, StageKnown: true}, false},
		{"stage 2: the guardian", questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: 2, StageKnown: true}, false},
		{"stage 3: the guardian is dead", questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: 3, StageKnown: true}, true},
		{"stage 10", questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: 10, StageKnown: true}, true},
		{"an unread journal", questplan.Snapshot{Journal: questplan.StatusInProgress, Stage: 0, StageKnown: false}, false},
		{"complete", questplan.Snapshot{Journal: questplan.StatusComplete}, true},
		{"an uncoloured tab", questplan.Snapshot{Journal: questplan.StatusUnknown, Stage: 5, StageKnown: true}, false},
	}
	for _, c := range cases {
		if got := questplan.PipPastGuardian(c.s); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if questplan.PriestInPeril.ExclusiveDone == nil {
		t.Error("Priest in Peril's module must name its one-at-a-time step's end")
	}
}
