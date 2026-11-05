package questmods

import (
	"slices"
	"sync"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// The nightshade, the shamans' enclave, the ogre potion, the wizard's four crystals and the lever.

// wtNightshadeStep takes a cave nightshade from the floor of cave 2 (a ground spawn that comes back; cave 6's is behind the east gate). Every entry to the
// enclave burns one.
func wtNightshadeStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjNightshade) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "watchtower: no free slot for the nightshade")
		return false
	}
	if !wtEnterCave(d, 2) {
		return false
	}
	spot := questplan.WtNightshade2
	if !questrunner.Travel(d, spot, 1) {
		logf(d, "watchtower: the nightshade's spot %v was not reached from %v", spot, questrunner.CurrentTile(d))
	}
	deadline := time.Now().Add(100 * time.Second)
	for !wtHeld(d, questplan.WtObjNightshade) {
		if questrunner.TakeGround(d, questplan.WtObjNightshade, spot, 6) {
			break
		}
		if time.Now().After(deadline) || wtRunLow(e) {
			logf(d, "watchtower: no nightshade on the floor of cave 2 near %v (it respawns: worth retrying)", spot)
			break
		}
		if !d.Delay(3 * time.Second) {
			break
		}
	}
	got := wtHeld(d, questplan.WtObjNightshade)
	out := wtCaveOut(d)
	return got && out
}

// wtShielded reports an anti-dragon shield worn: the dragons' fire is 5 then and not up to 50.
func wtShielded(d *questrunner.Deps) bool {
	return questrunner.WornCounts(d)[questplan.WtObjAntiDragonShield] > 0
}

// wtEnclaveEatLine is the hitpoints at or under which the enclave's eater takes a bite for this account (questplan.WtEnclaveEatLine).
func wtEnclaveEatLine(d *questrunner.Deps) (line, peak int) {
	_, max := questrunner.Hitpoints(d)
	shielded := wtShielded(d)
	return questplan.WtEnclaveEatLine(max, shielded), questplan.WtDragonPeak(shielded)
}

// wtEnclaveEater eats at the dragons' line (a bite before a breath of up to 50 could finish the account; the module's own sustain eats at 28) for as long as the
// returned stop is not called, never while a dialogue or a modal is up (eating would close it). It runs beside whatever the enclave's executors are doing, as
// the engine's own eat loop does, and the two share the account's eat lock.
func wtEnclaveEater(d *questrunner.Deps) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(600 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
			}
			main, side, chat := d.Session.State().Modals()
			if main != gamestate.NoModal || side != gamestate.NoModal || chat != gamestate.NoModal {
				continue
			}
			line, peak := wtEnclaveEatLine(d)
			questrunner.EatIfNeededVs(d, questplan.WtFoodIDs, line, peak)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-finished
		})
	}
}

// wtTopUp eats until the account is at the hitpoints a fight with a hardest hit of peak starts at (questplan.FightStartHP) or has nothing left to eat. A bite
// that would overshoot is not taken unless the account is well down (the decision's top-up is the same: questplan wtTopUp).
func wtTopUp(d *questrunner.Deps, peak int) {
	for try := 0; try < 8; try++ {
		hp, max := questrunner.Hitpoints(d)
		if max <= 0 || hp <= 0 || hp >= questplan.FightStartHP(max, peak) {
			return
		}
		smallest := 0
		for id, n := range questrunner.PackCounts(d) {
			if heal := questplan.FoodBiteHeal(id); n > 0 && heal > 0 && slices.Contains(questplan.WtFoodIDs, id) && (smallest == 0 || heal < smallest) {
				smallest = heal
			}
		}
		if smallest == 0 || (max-hp < smallest && hp*10 >= max*6) {
			return
		}
		if !questrunner.EatIfNeeded(d, questplan.WtFoodIDs, hp) {
			return
		}
		d.Delay(900 * time.Millisecond)
	}
}

// wtOutOfFood reports the account in the enclave with no food and at or under its eat line: nothing more can be asked of the visit, and the way out is the
// only thing left.
func wtOutOfFood(d *questrunner.Deps) bool {
	hp, max := questrunner.Hitpoints(d)
	if max <= 0 || questplan.FoodPoolHeal(questrunner.PackCounts(d), questplan.WtFoodIDs) > 0 {
		return false
	}
	line, _ := wtEnclaveEatLine(d)
	return hp <= line
}

// wtEnclaveIn feeds the enclave guard a nightshade, which runs the account past him into the enclave. The conversation the use opens is driven, never a fresh one
// started: both of the guard's own options make him attack. The account is topped up for the guard's stand and eats at the dragons' line from the moment it is in.
func wtEnclaveIn(d *questrunner.Deps) bool {
	if wtArea(d) == questplan.WtAreaEnclave {
		return true
	}
	if !wtHeld(d, questplan.WtObjNightshade) {
		logf(d, "watchtower: no nightshade for the enclave guard")
		return false
	}
	defer wtEnclaveEater(d)()
	wtTopUp(d, questplan.WtDragonPeak(wtShielded(d)))
	if !wtWalk(d, questplan.WtEnclaveGuard, 2) {
		logf(d, "watchtower: the enclave guard's stand %v was not reached from %v", questplan.WtEnclaveGuard, questrunner.CurrentTile(d))
		return false
	}
	guard, ok := wtNpcIn(d, questplan.WtNpcEnclave, 10)
	if !ok {
		logf(d, "watchtower: no enclave guard within 10 tiles of %v", questrunner.CurrentTile(d))
		return false
	}
	if err := questrunner.UseItemOnNpc(d, questplan.WtObjNightshade, guard); err != nil {
		logf(d, "watchtower: the nightshade on the enclave guard: %v", err)
		return false
	}
	wtAwait(d, nil, func() bool { return questrunner.NewDialogue(d).Ready() }, wtItemWait)
	wtDriveAll(d, nil, 4*time.Second, 30*time.Second)
	if !wtAwait(d, nil, func() bool { return wtArea(d) == questplan.WtAreaEnclave }, wtTeleWait) {
		logf(d, "watchtower: the guard did not let the account into the enclave (at %v)", questrunner.CurrentTile(d))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	return true
}

// wtEnclaveOut leaves the enclave by its cave at 2598,9468 (the pocket's way out). The walk is the nest's unavoidable half (58 tiles, 55 dragon-ticks,
// questplan.WtVisitExit): it is made eating at the dragons' line, and the cave's op is retried while the dragons breathe.
func wtEnclaveOut(d *questrunner.Deps) bool {
	if wtArea(d) != questplan.WtAreaEnclave {
		return true
	}
	defer wtEnclaveEater(d)()
	if !questrunner.Travel(d, questplan.WtEnclaveExit, 1) {
		logf(d, "watchtower: the enclave's exit %v was not reached from %v", questplan.WtEnclaveExit, questrunner.CurrentTile(d))
		return false
	}
	for try := 0; try < 3; try++ {
		if wtOperate(d, questplan.LocWtEnclaveExit, "Enter", questplan.WtEnclaveCave, 4, nil, func() bool { return wtArea(d) != questplan.WtAreaEnclave }, wtTeleWait) {
			d.Delay(900 * time.Millisecond)
			return true
		}
		questrunner.ClearLeftoverModal(d)
	}
	logf(d, "watchtower: the enclave's cave did not take the account out (at %v)", questrunner.CurrentTile(d))
	return false
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The ogre potion
// ---------------------------------------------------------------------------------------------------------------------------------

// wtMix uses item on target and waits for product. Any other pairing explodes the vial for five hitpoints, so each mix is made only from the state the
// order of the content allows (questplan.wtPotionNeeds).
func wtMix(d *questrunner.Deps, item, target, product int, what string) bool {
	if err := questrunner.UseItemOnItem(d, item, target); err != nil {
		logf(d, "watchtower: %s: %v", what, err)
		return false
	}
	if !waitUntil(d, wtItemWait, func() bool { return wtHeld(d, product) }) {
		logf(d, "watchtower: %s made nothing", what)
		return false
	}
	d.Delay(600 * time.Millisecond)
	questrunner.ClearLeftoverModal(d)
	return true
}

// wtBrewStep makes the ogre potion from whatever of its chain the pack already holds: bat bones ground with the pestle, a guam leaf in a vial of water, the
// jangerberries into that, the ground bones into that. The order is the content's: guam, then jangerberries, then ground bones.
func wtBrewStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjOgrePotion) || wtHeld(d, questplan.WtObjMagicPotion) {
		return true
	}
	if !wtHeld(d, questplan.WtObjGroundBones) && wtHeld(d, questplan.WtObjBatBones) && wtHeld(d, questplan.WtObjPestle) {
		if !wtMix(d, questplan.WtObjPestle, questplan.WtObjBatBones, questplan.WtObjGroundBones, "the pestle on the bat bones") {
			return false
		}
	}
	made1 := wtHeld(d, questplan.WtObjGuamVial) || wtHeld(d, questplan.WtObjGuamJangerVial)
	if !made1 && wtHeld(d, questplan.WtObjGuamLeaf) && wtHeld(d, questplan.WtObjVialWater) {
		if !wtMix(d, questplan.WtObjGuamLeaf, questplan.WtObjVialWater, questplan.WtObjGuamVial, "the guam leaf in the vial of water") {
			return false
		}
	}
	if !wtHeld(d, questplan.WtObjGuamJangerVial) && wtHeld(d, questplan.WtObjGuamVial) && wtHeld(d, questplan.WtObjJangerberries) {
		if !wtMix(d, questplan.WtObjJangerberries, questplan.WtObjGuamVial, questplan.WtObjGuamJangerVial, "the jangerberries in the guam vial") {
			return false
		}
	}
	if wtHeld(d, questplan.WtObjGuamJangerVial) && wtHeld(d, questplan.WtObjGroundBones) {
		if !wtMix(d, questplan.WtObjGroundBones, questplan.WtObjGuamJangerVial, questplan.WtObjOgrePotion, "the ground bat bones in the potion") {
			return false
		}
	}
	return wtHeld(d, questplan.WtObjOgrePotion)
}

// wtInfuseStep has the wizard infuse the ogre potion: it is used on him, and he closes the chat to mutter over it and speaks again.
func wtInfuseStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjMagicPotion) {
		return true
	}
	if !wtHeld(d, questplan.WtObjOgrePotion) {
		logf(d, "watchtower: no ogre potion to infuse")
		return false
	}
	if !wtUseOnWizard(d, questplan.WtObjOgrePotion, wtLongChat) {
		return false
	}
	return wtAwait(d, nil, func() bool { return wtHeld(d, questplan.WtObjMagicPotion) }, 15*time.Second)
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The shamans, the rock and the robe
// ---------------------------------------------------------------------------------------------------------------------------------

// wtNpcsOf lists the visible npcs of id within radius.
func wtNpcsOf(d *questrunner.Deps, id, radius int) []entityquery.Entity {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(radius).Where(func(e entityquery.Entity) bool { return e.ID == id }).Results()
}

// wtNearestShaman picks the nearest visible shaman, wherever it stands: the whole enclave is the dragons' ground, so the shamans beside a chieftain are no worse
// than the others, and the account does not wait about for the clear ones (a dissolved shaman is back in 102 ticks, at its own spawn).
func wtNearestShaman(d *questrunner.Deps) (entityquery.Entity, bool) {
	best, found := entityquery.Entity{}, false
	for _, s := range wtNpcsOf(d, questplan.WtNpcShaman, 40) {
		if !found || s.Distance < best.Distance {
			best, found = s, true
		}
	}
	return best, found
}

// wtNextSpawn is the nearest of the six shaman spawn tiles that has not been looked at, from where the account stands: the shamans are seen from some fifteen
// tiles and the enclave is sixty wide, so from the arrival none is in view and the account goes where they stand.
func wtNextSpawn(d *questrunner.Deps, seen map[tile.Tile]bool) (tile.Tile, bool) {
	here := questrunner.CurrentTile(d)
	best, found := tile.Tile{}, false
	for _, spawns := range [][3]tile.Tile{questplan.WtShamanClear, questplan.WtShamanNear} {
		for _, sp := range spawns {
			if seen[sp] {
				continue
			}
			if !found || tile.Chebyshev(here, sp) < tile.Chebyshev(here, best) {
				best, found = sp, true
			}
		}
	}
	return best, found
}

// wtDissolveStep uses the magic ogre potion on the shamans, one after the other, until the sixth spends it: the sixth hands over the third crystal. A shaman is
// never talked to or attacked (20 and 30 damage). The account goes from spawn to spawn, nearest first, and uses the potion on whatever shaman is in view; it does
// not wait about in the dragons' ground for a better one (the decision priced the whole tour, questplan.WtVisitWork), it eats at the dragons' line all the while,
// and it leaves for the way out when it is out of food and under that line.
func wtDissolveStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !wtHeld(d, questplan.WtObjMagicPotion) {
		logf(d, "watchtower: no magic ogre potion to dissolve the shamans with")
		return false
	}
	defer wtEnclaveEater(d)()
	if !wtEnclaveIn(d) {
		return false
	}
	deadline := time.Now().Add(wtShamanBudget)
	dissolved := 0
	seen := map[tile.Tile]bool{}
	for wtHeld(d, questplan.WtObjMagicPotion) && time.Now().Before(deadline) && !wtRunLow(e) {
		if step.Qty > 0 && dissolved >= step.Qty {
			break
		}
		if wtOutOfFood(d) {
			logf(d, "watchtower: out of food under the dragons' line: leaving the enclave (%d dissolved this visit)", dissolved)
			break
		}
		sh, ok := wtNearestShaman(d)
		if !ok {
			next, more := wtNextSpawn(d, seen)
			if !more {
				if len(seen) == 0 {
					break
				}
				// Every spawn has been looked at: one more round, the shamans having wandered or come back.
				seen = map[tile.Tile]bool{}
				if !d.Delay(3 * time.Second) {
					break
				}
				continue
			}
			seen[next] = true
			if !questrunner.Travel(d, next, 5) {
				logf(d, "watchtower: the shaman spawn %v was not reached from %v", next, questrunner.CurrentTile(d))
			}
			continue
		}
		at := tile.Tile{X: sh.X, Z: sh.Z, Level: sh.Level}
		if !questrunner.WalkNear(d, at, 2) {
			d.Delay(1200 * time.Millisecond)
			continue
		}
		// It wanders: the nearest one now.
		if again, ok := wtNearestShaman(d); ok {
			sh = again
		}
		if err := questrunner.UseItemOnNpc(d, questplan.WtObjMagicPotion, sh); err != nil {
			logf(d, "watchtower: the potion on a shaman: %v", err)
			d.Delay(1200 * time.Millisecond)
			continue
		}
		idx := sh.Index
		waitUntil(d, 6*time.Second, func() bool {
			for _, s := range wtNpcsOf(d, questplan.WtNpcShaman, 40) {
				if s.Index == idx {
					return false
				}
			}
			return true
		})
		dissolved++
		d.Delay(1800 * time.Millisecond)
		wtDriveAll(d, nil, 2*time.Second, 10*time.Second)
	}
	logf(d, "watchtower: %d shaman(s) dissolved this visit", dissolved)
	return dissolved > 0
}

// wtRockStep mines the Rock of Dalgroth: one swing, once the six shamans are gone (Mining 40 and a pickaxe), gives the fourth crystal.
func wtRockStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjCrystal4) {
		return true
	}
	if !wtHeld(d, questplan.WtObjPickaxe) {
		logf(d, "watchtower: no pickaxe for the Rock of Dalgroth")
		return false
	}
	defer wtEnclaveEater(d)()
	if !wtEnclaveIn(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.WtRockStand, 1) {
		logf(d, "watchtower: the rock's stand %v was not reached from %v", questplan.WtRockStand, questrunner.CurrentTile(d))
		return false
	}
	if !wtOperate(d, questplan.LocWtRock, "Mine", tile.Tile{X: 2590, Z: 9449}, 4, nil, func() bool { return wtHeld(d, questplan.WtObjCrystal4) }, 25*time.Second) {
		logf(d, "watchtower: the rock gave no crystal (Mining 40, a pickaxe and six dissolved shamans are wanted)")
		return false
	}
	return true
}

// wtRobeStep searches a shaman robe for the third crystal: the one an account lost, since the sixth shaman gives it. The robe lies on the enclave floor.
func wtRobeStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if wtHeld(d, questplan.WtObjCrystal3) {
		return true
	}
	defer wtEnclaveEater(d)()
	if !wtEnclaveIn(d) {
		return false
	}
	if !wtHeld(d, questplan.WtObjShamanRobe) {
		if !questrunner.Travel(d, questplan.WtRobe, 2) {
			logf(d, "watchtower: the robe's spot %v was not reached from %v", questplan.WtRobe, questrunner.CurrentTile(d))
			return false
		}
		if !questrunner.TakeGround(d, questplan.WtObjShamanRobe, questplan.WtRobe, 5) {
			logf(d, "watchtower: no shaman robe on the enclave floor near %v", questplan.WtRobe)
			return false
		}
	}
	if err := questrunner.OpHeldByName(d, questplan.WtObjShamanRobe, "Search"); err != nil {
		logf(d, "watchtower: searching the robe: %v", err)
		return false
	}
	return wtAwait(d, nil, func() bool { return wtHeld(d, questplan.WtObjCrystal3) }, wtItemWait)
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The wizard and the lever
// ---------------------------------------------------------------------------------------------------------------------------------

// wtCrystalsStep shows the wizard the four crystals: the first used on him, with all four in the pack, gives "This is the last one." and the lever's stage.
func wtCrystalsStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for _, c := range []int{questplan.WtObjCrystal1, questplan.WtObjCrystal2, questplan.WtObjCrystal3, questplan.WtObjCrystal4} {
		if !wtHeld(d, c) {
			logf(d, "watchtower: crystal %d is not in the pack", c)
			return false
		}
	}
	if !wtUseOnWizard(d, questplan.WtObjCrystal1, wtChat) {
		return false
	}
	st, ok := wtReadState(d)
	return ok && st.Stage == questplan.WtFoundAll
}

// wtLeverStep pulls the lever on the wizard's floor: the shield is activated, the reward paid and the account put on the tower's mirror copy.
func wtLeverStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !wtToWizardFloor(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.WtLeverStand, 1) {
		logf(d, "watchtower: the lever's stand %v was not reached from %v", questplan.WtLeverStand, questrunner.CurrentTile(d))
		return false
	}
	done := func() bool {
		return questrunner.ReadStatus(d, questplan.WatchtowerName) == questplan.StatusComplete || wtArea(d) == questplan.WtAreaMirror
	}
	if !wtOperate(d, questplan.LocWtLever, "Pull", tile.Tile{X: 2543, Z: 3115, Level: 2}, 4, nil, done, 40*time.Second) {
		logf(d, "watchtower: the lever did not complete the quest (at %v)", questrunner.CurrentTile(d))
		return false
	}
	wtDriveAll(d, nil, 6*time.Second, 60*time.Second)
	return done()
}
