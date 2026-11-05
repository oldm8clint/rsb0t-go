package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// The Bedabin camp's half of the quest: the plans shown to Al Shabim, the tent and its anvil, the feathers on the tip, the dart and the pineapple.

// ttAl finds Al Shabim, walking to his stop first.
func ttAl(d *questrunner.Deps) bool {
	stop := questplan.TtAlDealStop
	if _, ok := questrunner.FindNpc(d, stop, stop.Leash); ok {
		return true
	}
	return questrunner.GotoNpc(d, stop, nil)
}

// ttShowPlansStep shows the technical plans to Al Shabim ([opnpcu,al_shabim] with the plans: "Yes, I'm very interested.", and "Yes, I'm kind of
// curious." when the pack holds a bronze bar and ten feathers; the stage moves only on the second, so the stage is read afterwards).
func ttShowPlansStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtPlans) == 0 {
		logf(d, "tourist trap: no plans to show Al Shabim")
		return false
	}
	if !ttAl(d) {
		logf(d, "tourist trap: Al Shabim was not reached near %v (at %v)", questplan.TtAlAt, ttHere(d))
		return false
	}
	al, ok := questrunner.FindNpcByID(d, questplan.NpcTtAlShabim, "", 14)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnNpc(d, questplan.ObjTtPlans, al); err != nil {
		logf(d, "tourist trap: the plans on Al Shabim: %v", err)
		return false
	}
	driveScript(d, []string{"Yes, I'm very interested.", "Yes, I'm kind of curious."}, nil, 90*time.Second)
	ttSettle(d)
	if !e.StageIs(questplan.TtShownPlans) {
		logf(d, "tourist trap: Al Shabim's offer was not taken to its end (a bronze bar and ten feathers are what he asks to see)")
		return false
	}
	return true
}

// ttTentStep takes the plans to the Bedabin guard (use them on him: "Okay, you can go in, Al Shabim has told me about you.", which moves the account
// one tile north through the tent door into a sealed piece of ground) and forges the dart tip at the experimental anvil.
func ttTentStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtDartTip) > 0 {
		return true
	}
	if ttAreaNow(d) != questplan.TtAreaTent {
		if questrunner.HeldID(d, questplan.ObjTtPlans) == 0 {
			logf(d, "tourist trap: the tent's guard wants the plans")
			return false
		}
		if !ttTravel(d, questplan.TtTentOutside, 1) {
			logf(d, "tourist trap: the tent's door %v was not reached from %v", questplan.TtTentOutside, ttHere(d))
			return false
		}
		guard, ok := questrunner.FindNpcByID(d, questplan.NpcTtBedabinGrd, "", 8)
		if !ok {
			logf(d, "tourist trap: no Bedabin guard near the tent")
			return false
		}
		if err := questrunner.UseItemOnNpc(d, questplan.ObjTtPlans, guard); err != nil {
			logf(d, "tourist trap: the plans on the guard: %v", err)
			return false
		}
		inside := func() bool { return ttAreaNow(d) == questplan.TtAreaTent }
		driveScript(d, nil, inside, 40*time.Second)
		ttSettle(d)
		if !inside() {
			logf(d, "tourist trap: the guard did not let the account into the tent")
			return false
		}
	}
	return ttForge(d)
}

// ttForge spends bronze bars at the anvil until a dart tip comes ("You waste the bronze bar through an unlucky accident." is a miss: the bar is spent
// either way). The anvil box asks "Follow the technical plans?" and the answer is "Yes, I'd like to try."; Smithing 20 is the content's gate.
func ttForge(d *questrunner.Deps) bool {
	for try := 0; try < 8; try++ {
		if questrunner.HeldID(d, questplan.ObjTtDartTip) > 0 {
			logf(d, "tourist trap: the prototype dart tip is forged")
			return true
		}
		if questrunner.HeldID(d, questplan.ObjBronzeBar) == 0 || questrunner.HeldID(d, questplan.ObjHammer) == 0 || questrunner.HeldID(d, questplan.ObjTtPlans) == 0 {
			logf(d, "tourist trap: the anvil wants a bronze bar, a hammer and the plans (bar %d, hammer %d, plans %d)",
				questrunner.HeldID(d, questplan.ObjBronzeBar), questrunner.HeldID(d, questplan.ObjHammer), questrunner.HeldID(d, questplan.ObjTtPlans))
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocTtAnvil, "", "", questplan.TtAnvil, 4)
		if !ok {
			logf(d, "tourist trap: no experimental anvil near %v", questplan.TtAnvil)
			return false
		}
		mark := time.Now()
		if err := questrunner.UseItemOnLoc(d, questplan.ObjBronzeBar, m); err != nil {
			logf(d, "tourist trap: the bar on the anvil: %v", err)
			return false
		}
		done := func() bool {
			return questrunner.HeldID(d, questplan.ObjTtDartTip) > 0 || ttSaw(d, mark, "waste the bronze bar", "you need level 20")
		}
		driveScript(d, []string{"Yes, I'd like to try."}, done, 75*time.Second)
		ttSettle(d)
		if ttSaw(d, mark, "you need level 20") {
			logf(d, "tourist trap: the anvil wants Smithing %d", questplan.TtSmithing)
			return false
		}
	}
	return questrunner.HeldID(d, questplan.ObjTtDartTip) > 0
}

// ttFletchStep puts ten feathers on the tip (a feather used on the tip: Fletching 10, 31% a try at that level; a miss spends the ten feathers).
func ttFletchStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for try := 0; try < 8; try++ {
		if questrunner.HeldID(d, questplan.ObjTtDart) > 0 {
			logf(d, "tourist trap: the prototype dart is made")
			return true
		}
		if questrunner.HeldID(d, questplan.ObjTtDartTip) == 0 || questrunner.HeldID(d, questplan.ObjTtFeather) < 10 {
			logf(d, "tourist trap: the dart wants the tip and ten feathers (tip %d, feathers %d)",
				questrunner.HeldID(d, questplan.ObjTtDartTip), questrunner.HeldID(d, questplan.ObjTtFeather))
			return false
		}
		mark := time.Now()
		if err := questrunner.UseItemOnItem(d, questplan.ObjTtFeather, questplan.ObjTtDartTip); err != nil {
			logf(d, "tourist trap: the feathers on the tip: %v", err)
			return false
		}
		done := func() bool {
			return questrunner.HeldID(d, questplan.ObjTtDart) > 0 || ttSaw(d, mark, "unlucky accident", "fletching level of at least 10")
		}
		driveScript(d, nil, done, 60*time.Second)
		ttSettle(d)
		if ttSaw(d, mark, "fletching level of at least 10") {
			logf(d, "tourist trap: the dart wants Fletching %d", questplan.TtFletching)
			return false
		}
	}
	return questrunner.HeldID(d, questplan.ObjTtDart) > 0
}

// ttTentExit leaves the tent by its door ("Walk-through" from inside: the tent is a sealed piece of ground).
func ttTentExit(d *questrunner.Deps) bool {
	if ttAreaNow(d) != questplan.TtAreaTent {
		return true
	}
	if !ttOperate(d, "the tent's door", []int{questplan.LocTtTentDoor}, "Walk-through", questplan.TtTentInside, 3) {
		return false
	}
	out := func() bool { return ttAreaNow(d) != questplan.TtAreaTent }
	driveScript(d, nil, out, 30*time.Second)
	ttSettle(d)
	return out() || waitUntil(d, ttArriveWait, out)
}

// ttDartStep leaves the tent and gives the dart to Al Shabim: six bronze darts, the key taken back and a Tenti pineapple.
func ttDartStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtPineapple) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjTtDart) == 0 {
		logf(d, "tourist trap: no prototype dart for Al Shabim")
		return false
	}
	if !ttTentExit(d) {
		logf(d, "tourist trap: the tent's door did not let the account out")
		return false
	}
	if !ttAl(d) {
		logf(d, "tourist trap: Al Shabim was not reached near %v (at %v)", questplan.TtAlAt, ttHere(d))
		return false
	}
	al, ok := questrunner.FindNpcByID(d, questplan.NpcTtAlShabim, "", 14)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnNpc(d, questplan.ObjTtDart, al); err != nil {
		logf(d, "tourist trap: the dart on Al Shabim: %v", err)
		return false
	}
	have := func() bool { return questrunner.HeldID(d, questplan.ObjTtPineapple) > 0 }
	driveScript(d, nil, have, 2*time.Minute)
	ttSettle(d)
	return have()
}

// ttPineappleStep gives the pineapple to the cave guard (use it on him: "Great! Just what I've been looking for!").
func ttPineappleStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjTtPineapple) == 0 {
		return true
	}
	if ttAreaNow(d) != questplan.TtAreaHall {
		logf(d, "tourist trap: the cave guard stands in the hall, not at %v", ttHere(d))
		return false
	}
	if !ttTravelRound(d, questplan.TtGuardAt, 2) {
		logf(d, "tourist trap: the cave guard %v was not reached from %v", questplan.TtGuardAt, ttHere(d))
		return false
	}
	guard, ok := questrunner.FindNpcByID(d, questplan.NpcTtCaveGuard, "", 10)
	if !ok {
		logf(d, "tourist trap: no cave guard within 10 tiles of %v", ttHere(d))
		return false
	}
	if err := questrunner.UseItemOnNpc(d, questplan.ObjTtPineapple, guard); err != nil {
		logf(d, "tourist trap: the pineapple on the guard: %v", err)
		return false
	}
	gone := func() bool { return questrunner.HeldID(d, questplan.ObjTtPineapple) == 0 }
	driveScript(d, nil, gone, 60*time.Second)
	ttSettle(d)
	return gone()
}
