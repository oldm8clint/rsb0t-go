package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/barcrawlrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// N0 of docs/quest_selfsufficiency_plan_2026-10-19.md, with the real Barcrawl module: the unproven tail's ladder lists Scorpion
// Catcher and Horror from the Deep but not the proven Barcrawl they wait for, and the miniquest has no quest tab. The engine
// reads such a prerequisite through the registry's own Barcrawl (Engine.QuestFor, which internal/bot fills from QuestFactories),
// whose Done is the side store's record of the guard's verdict.

// tailNeedingTheBarcrawl is a one-quest tail ladder that waits for the Barcrawl as Scorpion Catcher and Horror do, and says
// when it ran.
func tailNeedingTheBarcrawl(ran *bool) questrunner.Quest {
	return questrunner.Quest{Module: questplan.Module{
		Record: questplan.Record{ID: "needs-barcrawl", Name: "needs the barcrawl",
			Requirements: questplan.Requirements{Quests: []string{BarcrawlID}}},
		Decide: func(questplan.Snapshot) questplan.Step {
			*ran = true
			return questplan.Step{Kind: questplan.StepDone}
		},
	}, Done: func(*questrunner.Engine) bool { return *ran }}
}

func barcrawlRegistry(id string) (questrunner.Quest, bool) {
	if id == BarcrawlID {
		return Barcrawl(), true
	}
	return questrunner.Quest{}, false
}

func TestATailLadderReadsTheBarcrawlTheHeadFinished(t *testing.T) {
	e, _, log := bcRig(t, func(*barcrawlrunner.Deps) bool { return true })
	e.QuestFor = barcrawlRegistry
	ran := false
	tail := []questrunner.Quest{tailNeedingTheBarcrawl(&ran)}

	// Before the crawl: held, with the reason the audit counted.
	res := e.RunLadder(tail, time.Minute, time.Minute)
	if ran || res.Outcome != questrunner.LadderStuck || !strings.Contains(strings.Join(res.Blocked["needs-barcrawl"], ";"), "prerequisite quest not complete: barcrawl") {
		t.Fatalf("before the crawl: outcome %s, blocked %v, ran %v", res.Outcome, res.Blocked, ran)
	}

	// The head's pass finishes the crawl (a run of the Barcrawl itself, on the same engine and store).
	if r := e.Run(Barcrawl(), time.Minute); r.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the head's Barcrawl run ended %s (%s): %v", r.Outcome, r.Reason, *log)
	}

	// The tail's next pass: the prerequisite reads complete and the quest behind it runs.
	res = e.RunLadder(tail, time.Minute, time.Minute)
	if !ran || res.Outcome != questrunner.LadderDone {
		t.Fatalf("after the crawl: outcome %s, blocked %v, ran %v: %v", res.Outcome, res.Blocked, ran, *log)
	}
	for _, line := range *log {
		if strings.Contains(line, "barcrawl is not in this ladder and has no quest tab: read complete from its own Done") {
			return
		}
	}
	t.Errorf("the log does not say how the Barcrawl was read: %v", *log)
}

// A lost side store is not a re-run of the whole crawl: with the record gone the Barcrawl reads incomplete, its next run is the
// guard's verdict (the tour the proven runner makes when the guard "waves us through" is one call that answers true at once,
// barcrawlrunner.Ensure), and that run records it again, so the tail reads it complete the pass after.
func TestALostStoreRereadsTheBarcrawlFromTheGuardNotTheTour(t *testing.T) {
	tours := 0
	e, _, log := bcRig(t, func(*barcrawlrunner.Deps) bool { tours++; return true })
	e.QuestFor = barcrawlRegistry
	ran := false
	tail := []questrunner.Quest{tailNeedingTheBarcrawl(&ran)}
	if r := e.Run(Barcrawl(), time.Minute); r.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the first Barcrawl run ended %s (%s): %v", r.Outcome, r.Reason, *log)
	}

	e.Store = questplan.NewStore() // the store is lost
	if res := e.RunLadder(tail, time.Minute, time.Minute); ran || res.Outcome != questrunner.LadderStuck {
		t.Fatalf("with no record the prerequisite must read incomplete: outcome %s, ran %v", res.Outcome, ran)
	}
	if r := e.Run(Barcrawl(), time.Minute); r.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the Barcrawl run after the loss ended %s (%s): %v", r.Outcome, r.Reason, *log)
	}
	if !Barcrawl().Done(e) {
		t.Fatalf("the run did not record the verdict again: %v", *log)
	}
	if res := e.RunLadder(tail, time.Minute, time.Minute); !ran || res.Outcome != questrunner.LadderDone {
		t.Fatalf("after the re-record: outcome %s, blocked %v, ran %v", res.Outcome, res.Blocked, ran)
	}
	if tours != 2 {
		t.Errorf("the tour seam was called %d times, want one per Barcrawl run (2)", tours)
	}
}
