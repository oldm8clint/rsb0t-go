package questrunner

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// A bank view read before the run began is not trusted by the snapshot: between two runs the Stage
// 5 trainer banks what it does not keep (a Murder Mystery thread included), so a reading from the
// last run says the thread is in the pack that is in the bank. A reading from this run is.
func TestASnapshotIgnoresABankViewFromAnEarlierRun(t *testing.T) {
	e := fakeEngine(&logSink{})
	e.quest = instantQuest("murder", map[string]bool{})
	e.Prov = &Provisioner{Bank: &BankView{}}
	e.Prov.Bank.Note(map[int]int{questplan.ObjThreadRed: 1, questplan.CoinsID: 500})

	e.runStart = time.Now().Add(time.Hour) // a run that began after the reading
	if snap := e.Snapshot(0); snap.BankKnown || len(snap.BankIDs) != 0 || snap.BankCoins != 0 {
		t.Errorf("a stale bank reading reached the snapshot: known=%v ids=%v coins=%d", snap.BankKnown, snap.BankIDs, snap.BankCoins)
	}

	e.runStart = time.Now().Add(-time.Hour) // a run that began before the reading
	snap := e.Snapshot(0)
	if !snap.BankKnown || snap.BankIDs[questplan.ObjThreadRed] != 1 || snap.BankCoins != 500 {
		t.Errorf("this run's own bank reading was dropped: known=%v ids=%v coins=%d", snap.BankKnown, snap.BankIDs, snap.BankCoins)
	}

	// Outside a run (runStart never set) the reading counts, as it always did.
	e.runStart = time.Time{}
	if snap := e.Snapshot(0); !snap.BankKnown {
		t.Error("with no run in progress the bank view must still count")
	}

	// A bank never read stays unread.
	fresh := fakeEngine(&logSink{})
	fresh.quest = instantQuest("murder", map[string]bool{})
	fresh.runStart = time.Now()
	if fresh.Snapshot(0).BankKnown {
		t.Error("an unread bank read as known")
	}
}

// A provision that fails on the way (here the bank cannot be used at all: a fake session has no
// interface archive) is a failure but not a BLOCKED kit: decide() keeps trying it, where a blocked
// kit would be waited on. The blocked half is the plan's (questplan tests); the flag between the
// two is Provisioner.LastBlocked, which only a round that ran out of sources sets.
func TestAProvisionThatFailedOnTheWayIsNotBlocked(t *testing.T) {
	e := fakeEngine(&logSink{})
	var failed, blocked, seen bool
	n := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "murder", Name: "murder"},
		Decide: func(s questplan.Snapshot) questplan.Step {
			n++
			if n == 1 {
				return questplan.Step{Kind: questplan.StepProvision, KitName: "k", Kit: []questplan.KitLine{{ID: 1931, Name: "Pot", Qty: 1}}}
			}
			failed, blocked, seen = s.Failed["provision:k"], s.Blocked["provision:k"], true
			return questplan.Step{Kind: questplan.StepDone}
		},
	}}
	if res := e.Run(q, time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if !seen || !failed || blocked {
		t.Errorf("failed=%v blocked=%v: a kit that failed on the way is Failed and not Blocked", failed, blocked)
	}
	if e.Prov.LastBlocked {
		t.Error("LastBlocked is set by a round that did not run out of sources")
	}
}

// Run takes its own start, so a view read during the run (a provisioning bank visit) counts for
// the rest of it.
func TestARunStampsItsStartBeforeTheFirstSnapshot(t *testing.T) {
	e := fakeEngine(&logSink{})
	e.Prov = &Provisioner{Bank: &BankView{}}
	e.Prov.Bank.Note(map[int]int{questplan.ObjThreadRed: 1}) // an earlier run's reading
	time.Sleep(5 * time.Millisecond)                         // a clock tick between that reading and this run
	var knownAtFirstDecide, knownAfterRead bool
	n := 0
	q := Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "murder", Name: "murder"},
		Decide: func(s questplan.Snapshot) questplan.Step {
			n++
			if n == 1 {
				knownAtFirstDecide = s.BankKnown
				e.Prov.Bank.Note(map[int]int{questplan.ObjThreadGreen: 1}) // this run reads the bank
				return questplan.Step{Kind: questplan.StepWait, Reason: "w"}
			}
			knownAfterRead = s.BankKnown && s.BankIDs[questplan.ObjThreadGreen] == 1
			return questplan.Step{Kind: questplan.StepDone}
		},
	}}
	if res := e.Run(q, time.Minute); res.Outcome != OutcomeComplete {
		t.Fatalf("outcome %s", res.Outcome)
	}
	if knownAtFirstDecide {
		t.Error("the previous run's bank reading was used by this run's first decision")
	}
	if !knownAfterRead {
		t.Error("the bank reading this run took was not used by its next decision")
	}
}
