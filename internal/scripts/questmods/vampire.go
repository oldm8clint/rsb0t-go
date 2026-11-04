package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// VampireSlayer is Vampire Slayer ready for the engine.
func VampireSlayer() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.VampireSlayer,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomVampireGarlic: vampireGarlic,
			questplan.CustomVampireCount:  vampireCount,
		},
	}
}

// vampireGarlic opens Morgan's upstairs cupboard and searches it (no quest gate, unlimited).
func vampireGarlic(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.GarlicCupboard, 2) {
		return false
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocGarlicCupShut, "", "Open", questplan.GarlicCupboard, 3); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "garlic cupboard: %v", err)
			return false
		}
		d.Delay(1200 * time.Millisecond)
	}
	m, ok := questrunner.FindLoc(d, 0, "Cupboard", "Search", questplan.GarlicCupboard, 3)
	if !ok {
		logf(d, "the garlic cupboard did not open")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		return false
	}
	return waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjGarlic) > 0 })
}

// vampireCount goes into Draynor Manor (its front doors open from the south only), down to the
// crypt, opens the coffin (which spawns this account's own Count for 500 ticks) and fights him
// with the stake and the hammer in the pack, so the killing blow completes the quest.
func vampireCount(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	here := questrunner.CurrentTile(d)
	if !questplan.IsUnderground(here) {
		if !questrunner.Travel(d, questplan.CryptStairs, 2) {
			// The long walk does not know the one-way manor doors: open them from the south.
			if !questrunner.Travel(d, questplan.ManorDoorSouth, 1) {
				return false
			}
			if m, ok := questrunner.FindLoc(d, questplan.LocManorDoorL, "", "Open", questplan.ManorDoorSouth, 3); ok {
				_ = questrunner.OperateLoc(d, m, "Open")
				waitUntil(d, 6*time.Second, func() bool { return questrunner.CurrentTile(d).Z > questplan.ManorDoorSouth.Z+1 })
			}
			if !questrunner.WalkNear(d, questplan.CryptStairs, 2) {
				return false
			}
		}
		m, ok := questrunner.FindLoc(d, questplan.LocCryptStairs, "", "Walk-Down", questplan.CryptStairs, 3)
		if !ok {
			logf(d, "no crypt stairs")
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Walk-Down"); err != nil {
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool { return questplan.IsUnderground(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	countNear := func() bool {
		_, ok := questrunner.FindNpcByID(d, questplan.NpcCountDraynor, "", 10)
		return ok
	}
	if !countNear() {
		m, ok := questrunner.FindLoc(d, questplan.LocVampCoffin, "", "Open", questplan.VampCoffinTile, 4)
		if !ok {
			logf(d, "the coffin is not loaded (or is open for another account)")
			d.Delay(3 * time.Second)
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			return false
		}
		if !waitUntil(d, 10*time.Second, countNear) {
			logf(d, "no Count after opening the coffin")
			return false
		}
	}
	questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcCountDraynor,
		Target: "Count Draynor", Anchor: questplan.VampCoffinTile}, questplan.VampireSlayer.Sustain)
	return waitUntil(d, 8*time.Second, func() bool {
		return questrunner.ReadStatus(d, questplan.VampireSlayerName) == questplan.StatusComplete
	})
}
