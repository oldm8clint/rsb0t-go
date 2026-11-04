// Package questmods is the live half of the ported quests that are small enough not to need a
// package pair of their own: each quest's pure module lives in questplan (runemysteries.go,
// restlessghost.go ...) and its custom steps live here, registered on a questrunner.Quest. The
// Waterfall Quest keeps its own waterfallplan/waterfallrunner pair because of its size.
package questmods

import (
	"fmt"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// RestlessGhost is The Restless Ghost ready for the engine.
func RestlessGhost() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.RestlessGhost,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomGhostTalk:   ghostTalk,
			questplan.CustomGhostSkull:  ghostSkull,
			questplan.CustomGhostReturn: ghostReturn,
		},
	}
}

func waitUntil(d *questrunner.Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(200 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

func logf(d *questrunner.Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log("[quest] " + fmt.Sprintf(format, args...))
	}
}

// operateCoffin opens the shut coffin, or searches the open one; either spawns the ghost when
// none is near (quest_priest.rs2 check_restlessghost_spawn). It never Closes: the loc change is
// global and would shut the coffin under another account's use-on.
func operateCoffin(d *questrunner.Deps) bool {
	if m, ok := questrunner.FindLoc(d, questplan.LocCoffinShut, "", "Open", questplan.GhostCoffinTile, 3); ok {
		if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
			logf(d, "open the coffin: %v", err)
			return false
		}
		d.Delay(1200 * time.Millisecond)
		return true
	}
	if m, ok := questrunner.FindLoc(d, questplan.LocCoffinOpen, "", "Search", questplan.GhostCoffinTile, 3); ok {
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			logf(d, "search the coffin: %v", err)
			return false
		}
		d.Delay(1200 * time.Millisecond)
		return true
	}
	logf(d, "no coffin loaded at %d,%d", questplan.GhostCoffinTile.X, questplan.GhostCoffinTile.Z)
	return false
}

// ghostTalk spawns the ghost if needed and drives its dialogue (stage 2 to 3).
func ghostTalk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.GhostCoffinStand, 1) {
		return false
	}
	ghostNear := func() bool {
		_, ok := questrunner.FindNpcByID(d, questplan.NpcRestlessGhost, "", 6)
		return ok
	}
	if !ghostNear() {
		if !operateCoffin(d) {
			return false
		}
		if !waitUntil(d, 5*time.Second, ghostNear) {
			logf(d, "no ghost after operating the coffin")
			return false
		}
	}
	if !questrunner.OpenDialogue(d, questplan.GhostStop) {
		return false
	}
	ok := questrunner.NewDialogue(d).Drive(questplan.GhostStop.Prefer)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	return ok
}

// skullPatience bounds the wait for the shared skull spawn to come back (100 ticks: 60s in
// production, 20s on the lab), several respawns over.
const skullPatience = 4 * time.Minute

// ghostSkull takes the skull in the Wizards' Tower basement and deals with the skeleton it
// raises: fought where it stands by any account with 10 or more Hitpoints (max hit 2), walked
// away from otherwise (it is leashed 8 tiles from its spawn and cannot follow through the door).
func ghostSkull(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.GhostSkullTile, 2) {
		return false
	}
	deadline := time.Now().Add(skullPatience)
	for questrunner.HeldID(d, questplan.ObjGhostSkull) == 0 && time.Now().Before(deadline) {
		if len(questrunner.GroundItemsNear(d, questplan.ObjGhostSkull, questplan.GhostSkullTile, 1)) == 0 {
			logf(d, "the skull is not on the altar (taken by another account?): waiting for its respawn")
			waitUntil(d, 30*time.Second, func() bool {
				return len(questrunner.GroundItemsNear(d, questplan.ObjGhostSkull, questplan.GhostSkullTile, 1)) > 0
			})
			continue
		}
		questrunner.TakeGround(d, questplan.ObjGhostSkull, questplan.GhostSkullTile, 1)
	}
	if questrunner.HeldID(d, questplan.ObjGhostSkull) == 0 {
		return false
	}
	e.InvalidateStage()
	// The skeleton is not fought: the skull is in the pack and the coffin is a walk away (it is leashed 8 tiles from
	// its spawn and cannot follow through the door). Live 6 Oct 2026, Spare 25 (22:44) and Spare33 (23:59) stood and
	// fought it at 27-28 hitpoints with no food on this quest and died to a level-13 skeleton that hits for 2.
	hp, maxHP := questrunner.Hitpoints(d)
	if sk, found := questrunner.FindNpcByID(d, questplan.NpcSkullSkeleton, "", 8); found {
		logf(d, "took the skull; leaving the skeleton at %d,%d alone (hitpoints %d/%d): walking out of its leash", sk.X, sk.Z, hp, maxHP)
	}
	return true
}

// ghostReturn opens the coffin if it is shut and uses the skull on the open one, then clears the
// three message boxes (the completion queue only runs once no modal is open).
func ghostReturn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questrunner.Travel(d, questplan.GhostCoffinStand, 1) {
		return false
	}
	if _, open := questrunner.FindLoc(d, questplan.LocCoffinOpen, "", "", questplan.GhostCoffinTile, 3); !open {
		if !operateCoffin(d) {
			return false
		}
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCoffinOpen, "", "", questplan.GhostCoffinTile, 3)
	if !ok {
		logf(d, "the coffin did not open")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjGhostSkull, m); err != nil {
		logf(d, "the skull on the coffin: %v", err)
		return false
	}
	waitUntil(d, 6*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjGhostSkull) == 0 })
	for i := 0; i < 6; i++ {
		dl := questrunner.NewDialogue(d)
		if dl.Ready() {
			dl.Drive([]string{"x"})
		}
		questrunner.ClearLeftoverModal(d)
		if questrunner.ReadStatus(d, questplan.RestlessGhostName) == questplan.StatusComplete {
			return true
		}
		d.Delay(800 * time.Millisecond)
	}
	return questrunner.HeldID(d, questplan.ObjGhostSkull) == 0
}
