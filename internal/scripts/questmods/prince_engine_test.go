package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// Prince Ali Rescue through the real engine (Engine.Run: the watchdog, the Ran/Failed bookkeeping that tells the module its kits and
// custom steps ran, the completion record), with every step's EXECUTOR stubbed on the simulated pack of fillerRig
// (fillers_engine_test.go). The module's Decide is the real one; its custom steps keep their own names, so the module's reading of
// Failed and Ran is the live one. The executors the module relies on live (the dialogue driver, the walker, the fire, the cell) are
// the engine's own or are checked on the real pack and loc index (prince_pack_test.go).

// wrapPrince is wrap for a module with custom steps: they keep their own names (the module reads Failed and Ran by them) and every
// other step becomes a stub named for its kind.
func (r *fillerRig) wrapPrince(q questrunner.Quest, stubs map[string]questrunner.CustomFunc) questrunner.Quest {
	real := q.Module.Decide
	q.Module.ReadStage = nil
	q.Module.Decide = func(s questplan.Snapshot) questplan.Step {
		switch {
		case r.stage == 0:
			s.Journal, s.StageKnown = questplan.StatusNotStarted, false
		case r.stage >= questplan.PrinceComplete:
			s.Journal = questplan.StatusComplete
		default:
			s.Journal, s.Stage, s.StageKnown = questplan.StatusInProgress, r.stage, true
		}
		st := real(s)
		switch st.Kind {
		case questplan.StepDone, questplan.StepWait:
			return st
		}
		st.Reason = st.Describe() // the engine logs a custom step as its name and reason: keep what the step really is
		switch st.Kind {
		case questplan.StepProvision:
			st.Kind, st.Custom = questplan.StepCustom, "provision:"+st.KitName
		case questplan.StepCustom:
		default:
			st.Kind, st.Custom = questplan.StepCustom, "stub:"+string(st.Kind)
		}
		return st
	}
	q.Customs = stubs
	return q
}

func (r *fillerRig) princeStubs() map[string]questrunner.CustomFunc {
	stubs := r.provisionStub(questplan.KitPrinceCoins, questplan.KitPrincePantry, questplan.KitPrinceWater, questplan.KitPrinceBar,
		questplan.KitPrinceFlour, questplan.KitPrinceBerries, questplan.KitPrinceSkirt, questplan.KitPrinceShears, questplan.KitPrincePickaxe,
		questplan.KitPrinceFire, questplan.KitPrinceFood)
	keystatus := 0
	leela := func() {
		if r.held(questplan.ObjPrinceKey) == 0 && keystatus == 1 {
			r.give(questplan.ObjPrinceKey, 1)
			keystatus = 2
		}
		if r.stage == questplan.PrinceSpokeOsman && r.held(questplan.ObjPrinceKey) > 0 && r.held(questplan.ObjBlondWig) > 0 &&
			r.held(questplan.ObjPinkSkirt) > 0 && r.held(questplan.ObjSkinPaste) > 0 {
			r.stage = questplan.PrinceHasEquipment
		}
	}
	stubs["stub:talk"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch st.Stop.ID {
		case questplan.NpcHassan:
			switch r.stage {
			case 0:
				r.stage = questplan.PrinceStarted
			case questplan.PrinceSaved:
				r.stage = questplan.PrinceComplete
			}
		case questplan.NpcOsman:
			r.stage = questplan.PrinceSpokeOsman
		case questplan.NpcLeela:
			leela()
		case questplan.NpcAggie:
			if strings.Contains(st.Stop.Prefer[len(st.Stop.Prefer)-1], "skin paste") {
				for _, id := range []int{questplan.ObjRedberries, questplan.ObjPotOfFlour, questplan.ObjAshes} {
					if !r.take(id, 1) {
						return false
					}
				}
				if !r.take(questplan.ObjJugOfWater, 1) && !r.take(questplan.ObjBucketOfWater, 1) {
					return false
				}
				r.give(questplan.ObjSkinPaste, 1)
			} else {
				if !r.take(questplan.ObjOnion, questplan.PrinceOnions) || !r.take(questplan.CoinsID, questplan.PrinceDyeFee) {
					return false
				}
				r.give(questplan.ObjYellowDye, 1)
			}
		case questplan.NpcNed:
			if !r.take(questplan.ObjBallOfWool, questplan.PrinceBalls) {
				return false
			}
			r.give(questplan.ObjPlainWig, 1)
		default:
			r.t.Fatalf("talk to an unexpected npc %d", st.Stop.ID)
		}
		r.stubLog("talk %d -> stage %d", st.Stop.ID, r.stage)
		return true
	}
	stubs["stub:pickLoc"] = func(e *questrunner.Engine, st questplan.Step) bool { r.give(questplan.ObjOnion, 1); return true }
	stubs["stub:useOn"] = func(e *questrunner.Engine, st questplan.Step) bool {
		switch st.TargetID {
		case questplan.ObjPlainWig:
			if !r.take(questplan.ObjYellowDye, 1) || !r.take(questplan.ObjPlainWig, 1) {
				return false
			}
			r.give(questplan.ObjBlondWig, 1)
		case questplan.ObjClay:
			if !r.take(st.ItemID, 1) || !r.take(questplan.ObjClay, 1) {
				return false
			}
			r.give(questplan.ObjSoftClay, 1)
		default:
			r.t.Fatalf("unexpected use-on %s", st.Describe())
		}
		return true
	}
	stubs["stub:deposit"] = func(e *questrunner.Engine, st questplan.Step) bool { return true }
	stubs[questplan.CustomSheepWool] = func(e *questrunner.Engine, st questplan.Step) bool {
		r.give(questplan.ObjBallOfWool, st.Qty)
		return true
	}
	stubs[questplan.CustomPrinceKey] = func(e *questrunner.Engine, st questplan.Step) bool {
		if st.Qty > 0 && keystatus == 0 && r.take(questplan.ObjKeyPrint, 1) && r.take(questplan.ObjBronzeBar, 1) {
			keystatus = 1
		}
		leela()
		return r.held(questplan.ObjPrinceKey) > 0
	}
	stubs[questplan.CustomPrincePrint] = func(e *questrunner.Engine, st questplan.Step) bool {
		if r.stage != questplan.PrinceSpokeOsman || !r.take(questplan.ObjSoftClay, 1) {
			return false
		}
		r.give(questplan.ObjKeyPrint, 1)
		return true
	}
	stubs[questplan.CustomPrinceAshes] = func(e *questrunner.Engine, st questplan.Step) bool {
		if !r.take(questplan.ObjLogs, 1) {
			return false
		}
		r.give(questplan.ObjAshes, 1)
		return true
	}
	stubs[questplan.CustomPrinceClay] = func(e *questrunner.Engine, st questplan.Step) bool { r.give(questplan.ObjClay, 1); return true }
	buy := func(price int) questrunner.CustomFunc {
		return func(e *questrunner.Engine, st questplan.Step) bool {
			for r.held(st.ItemID) < st.Qty && r.take(questplan.CoinsID, price) {
				r.give(st.ItemID, 1)
			}
			return true
		}
	}
	stubs[questplan.CustomPrinceRope] = buy(questplan.PrinceRopeFee)
	stubs[questplan.CustomPrinceBeerBuy] = buy(2)
	stubs[questplan.CustomPrinceBeer] = func(e *questrunner.Engine, st questplan.Step) bool {
		if r.stage != questplan.PrinceHasEquipment || !r.take(questplan.ObjBeer, questplan.PrinceBeers) {
			return false
		}
		r.stage = questplan.PrinceGuardDrunk
		return true
	}
	stubs[questplan.CustomPrinceBreakout] = func(e *questrunner.Engine, st questplan.Step) bool {
		if r.stage == questplan.PrinceGuardDrunk {
			if !r.take(questplan.ObjRope, 1) {
				return false
			}
			r.stage = questplan.PrinceTiedKeli
		}
		for _, id := range []int{questplan.ObjPrinceKey, questplan.ObjBlondWig, questplan.ObjPinkSkirt, questplan.ObjSkinPaste} {
			if !r.take(id, 1) {
				return false
			}
		}
		r.stage = questplan.PrinceSaved
		return true
	}
	stubs[questplan.CustomPrinceLeave] = func(e *questrunner.Engine, st questplan.Step) bool { return true }
	return stubs
}

func TestPrinceAliRunsToCompleteThroughTheEngine(t *testing.T) {
	r := newFillerRig(t, 0, map[int]int{questplan.CoinsID: 700}, nil)
	q := r.wrapPrince(PrinceAli(), r.princeStubs())
	if q.ID() != questplan.PrinceAliID {
		t.Fatalf("the factory built %q", q.ID())
	}
	e := r.engine()
	res := e.Run(q, 40*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	if r.stage != questplan.PrinceComplete {
		t.Errorf("stage %d at the end", r.stage)
	}
	cs := e.Store.Record(questplan.PrinceAliID).Completions
	if len(cs) != 1 || !cs[0].Earned() || cs[0].Account != "Test Player" {
		t.Errorf("completions %+v, want one earned completion", cs)
	}
	joined := strings.Join(r.log, "\n")
	for _, want := range []string{
		"talk to Hassan", "talk to Osman", "provision prince-pantry", "provision prince-water", "Pick Onion", "prince-ashes", "talk to Aggie",
		"talk to Ned", "use Yellow dye on Wig", "prince-print", "prince-rope", "prince-key", "prince-beer ", "prince-breakout", "[prince] COMPLETED",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("the log has no %q:\n%s", want, joined)
		}
	}
	// The order: Osman before anything is gathered; Keli's print before the key; the key before Joe; Joe before the break-out.
	idx := func(s string) int { return strings.Index(joined, s) }
	if !(idx("talk to Osman") < idx("provision prince-pantry") && idx("prince-print") < idx("prince-key") &&
		idx("prince-key") < idx("prince-beer ") && idx("prince-beer ") < idx("prince-breakout") && idx("prince-breakout") < idx("[prince] COMPLETED")) {
		t.Errorf("the quest's order is wrong:\n%s", joined)
	}
	// Nothing is left in the pack that the quest consumed.
	for _, id := range []int{questplan.ObjPrinceKey, questplan.ObjBlondWig, questplan.ObjPinkSkirt, questplan.ObjSkinPaste, questplan.ObjBeer} {
		if n := r.held(id); n != 0 {
			t.Errorf("obj %d left in the pack: %d", id, n)
		}
	}
}

// An account that stands at the beer stage with the whole disguise goes straight to Joe and on: nothing is gathered twice.
func TestPrinceAliResumesAtTheBeersThroughTheEngine(t *testing.T) {
	pack := map[int]int{questplan.CoinsID: 300, questplan.ObjPrinceKey: 1, questplan.ObjBlondWig: 1, questplan.ObjPinkSkirt: 1, questplan.ObjSkinPaste: 1,
		questplan.ObjBeer: 3, questplan.ObjRope: 2}
	r := newFillerRig(t, questplan.PrinceHasEquipment, pack, nil)
	q := r.wrapPrince(PrinceAli(), r.princeStubs())
	res := r.engine().Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	for _, absent := range []string{"prince-pantry", "prince-water", "Pick Onion", "prince-ashes", "talk to Aggie", "talk to Ned", "prince-print", "prince-rope", "prince-key"} {
		if strings.Contains(joined, absent) {
			t.Errorf("the log has %q, a step the pack made unnecessary:\n%s", absent, joined)
		}
	}
}

// A banked key blocks Leela's replacement: the engine reads the bank (the pantry) before the key step asks her.
func TestPrinceAliDrawsABankedKeyBeforeAskingLeelaThroughTheEngine(t *testing.T) {
	pack := map[int]int{questplan.CoinsID: 300, questplan.ObjBlondWig: 1, questplan.ObjPinkSkirt: 1, questplan.ObjSkinPaste: 1,
		questplan.ObjBeer: 3, questplan.ObjRope: 2}
	r := newFillerRig(t, questplan.PrinceHasEquipment, pack, map[int]int{questplan.ObjPrinceKey: 1})
	q := r.wrapPrince(PrinceAli(), r.princeStubs())
	res := r.engine().Run(q, 30*time.Second)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s): %v", res.Outcome, res.Reason, r.log)
	}
	joined := strings.Join(r.log, "\n")
	if strings.Contains(joined, "prince-key") || !strings.Contains(joined, "stub provision prince-pantry") {
		t.Errorf("the key was not drawn from the bank by the pantry:\n%s", joined)
	}
}

// A quest with no coins and nothing in the bank waits on the toll and the shops, and the engine parks it rather than loop.
func TestPrinceAliWithoutCoinsParksInsteadOfLooping(t *testing.T) {
	r := newFillerRig(t, questplan.PrinceStarted, nil, nil)
	q := r.wrapPrince(PrinceAli(), r.princeStubs())
	res := r.engine().Run(q, 40*time.Second)
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "coins") {
		t.Fatalf("the run ended %s (%s), want a park on the purse: %v", res.Outcome, res.Reason, r.log)
	}
}
