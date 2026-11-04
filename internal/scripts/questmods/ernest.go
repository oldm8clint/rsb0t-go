package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ErnestTheChicken is Ernest the Chicken ready for the engine. The talks, the stat-free provisioning of the banked parts
// (questplan.ErnestChicken's KitErnest) and the combination of the poison and the fish food are generic steps; the ten
// executors below are the legs that need a client: the three ground gathers, the compost heap's key, the closet's tube, the
// maze's can (ernestmaze.go), the fountain's three steps and the hand-in. See questplan/ernestchicken.go for what the
// content does on each.
func ErnestTheChicken() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.ErnestChicken,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomErnestFishFood: ernestTake(questplan.ObjFishFood, "fish food", questplan.ErnestFishFoodSpawn),
			questplan.CustomErnestPoison:   ernestTake(questplan.ObjPoison, "poison", questplan.ErnestPoisonSpawn),
			questplan.CustomErnestSpade:    ernestTake(questplan.ObjSpade, "spade", questplan.ErnestSpadeSpawn),
			questplan.CustomErnestCan:      ernestCan,
			questplan.CustomErnestProbe:    ernestProbe,
			questplan.CustomErnestPour:     ernestPour,
			questplan.CustomErnestGauge:    ernestGauge,
			questplan.CustomErnestKey:      ernestKey,
			questplan.CustomErnestTube:     ernestTube,
			questplan.CustomErnestHandIn:   ernestHandIn,
		},
	}
}

// ernestSpawnWait is how long a ground gather waits for a spawn another account has just emptied: 100 ticks and the walk.
const ernestSpawnWait = 80 * time.Second

// ernestTake gathers one item off its ground spawn inside the manor. It walks with the plain Travel the Vampire Slayer's legs
// prove through the garden: the provisioner's ground rung walks with TravelCautious, whose flee squares round an attacker
// would close the corridor between the garden's trees once one had hit the account.
func ernestTake(id int, what string, at tile.Tile) questrunner.CustomFunc {
	return func(e *questrunner.Engine, _ questplan.Step) bool {
		d := e.Deps()
		if held(d, id) {
			return true
		}
		if !questrunner.Travel(d, at, 2) {
			logf(d, "ernest: could not reach the %s's spawn at %d,%d,%d", what, at.X, at.Z, at.Level)
			return false
		}
		deadline := time.Now().Add(ernestSpawnWait)
		for !held(d, id) && time.Now().Before(deadline) {
			if len(questrunner.GroundItemsNear(d, id, at, 3)) > 0 && questrunner.TakeGround(d, id, at, 3) {
				break
			}
			if !d.Delay(1500 * time.Millisecond) {
				return false
			}
		}
		if held(d, id) {
			logf(d, "ernest: took the %s", what)
			return true
		}
		logf(d, "ernest: no %s on the ground at %d,%d,%d within %s (another account took it, or the spawn is not up)", what, at.X, at.Z, at.Level, ernestSpawnWait)
		return false
	}
}

// ernestFountainPark is how long the quest is left alone after the fountain kept biting through a poisoning that said it
// worked.
const ernestFountainPark = 2 * time.Hour

// ernestLowHP is whether the account is too hurt for the next one-hitpoint bite, a skeleton's blow or a tree's.
func ernestLowHP(d *questrunner.Deps, floor int) bool {
	hp, max := questrunner.Hitpoints(d)
	return max > 0 && hp < floor
}

// fountainResult is what a search of the fountain taught.
type fountainResult int

const (
	fountainUnknown fountainResult = iota
	fountainGauge                  // "You get the pressure gauge from the fountain."
	fountainBitten                 // "Something in the water bites you.": the piranhas live
)

// searchFountain walks to the fountain and searches it, driving the dialogue the script wraps the bite and the gauge in
// ([oploc1,hauntedfountain]: two `chatplayer` boxes before the branch). The gauge in the pack is the one proof of a poisoned
// fountain, and the bite message of a live one; anything else (the op dropped, the box not continued) is unknown.
func searchFountain(d *questrunner.Deps) fountainResult {
	if held(d, questplan.ObjPressureGauge) {
		return fountainGauge
	}
	if !questrunner.Travel(d, questplan.ErnestFountainStand, 1) {
		logf(d, "ernest: could not reach the fountain")
		return fountainUnknown
	}
	m, ok := questrunner.FindLoc(d, questplan.LocManorFountain, "", "Search", questplan.ErnestFountainStand, 6)
	if !ok {
		logf(d, "ernest: no fountain (loc %d) near %d,%d", questplan.LocManorFountain, questplan.ErnestFountainStand.X, questplan.ErnestFountainStand.Z)
		return fountainUnknown
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "ernest: search the fountain: %v", err)
		return fountainUnknown
	}
	bitten := func() bool { return strings.Contains(strings.ToLower(chatSince(d, mark)), "bites you") }
	questrunner.NewDialogue(d).DriveUntil(func() bool { return held(d, questplan.ObjPressureGauge) || bitten() }, nil, 25*time.Second)
	questrunner.ClearLeftoverModal(d)
	switch {
	case held(d, questplan.ObjPressureGauge):
		return fountainGauge
	case bitten():
		return fountainBitten
	}
	logf(d, "ernest: the fountain answered neither the gauge nor a bite: [%s]", strings.TrimSpace(chatSince(d, mark)))
	return fountainUnknown
}

// ernestProbe is the first search: it costs the account one hitpoint if the fountain has not been poisoned (this run or an
// earlier one; the varp is unreadable). It succeeds, and Ran says so, once the fountain has been heard from either way.
func ernestProbe(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if ernestLowHP(d, 4) {
		questrunner.EatIfNeeded(d, questplan.ErnestChicken.Sustain.FoodIDs, 4)
		if ernestLowHP(d, 4) {
			logf(d, "ernest: too few hitpoints for the piranhas' bite")
			e.RequestPark("hitpoints too low for the fountain's bite and no food to eat", 20*time.Minute)
			return false
		}
	}
	res := searchFountain(d)
	switch res {
	case fountainGauge:
		logf(d, "ernest: the fountain was already poisoned: the gauge is in the pack")
	case fountainBitten:
		logf(d, "ernest: the piranhas bit: the fountain needs the poisoned fish food")
	}
	return res != fountainUnknown
}

// ernestPour pours the poisoned fish food into the fountain and waits for the script to finish: the varp that makes the
// fountain give the gauge is set after a message chain of about five ticks ("... then die and float to the surface."), and a
// search that races it is bitten.
func ernestPour(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !held(d, questplan.ObjPoisonedFishFood) {
		logf(d, "ernest: no poisoned fish food to pour")
		return false
	}
	if !questrunner.Travel(d, questplan.ErnestFountainStand, 1) {
		logf(d, "ernest: could not reach the fountain")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocManorFountain, "", "", questplan.ErnestFountainStand, 6)
	if !ok {
		logf(d, "ernest: no fountain near %d,%d", questplan.ErnestFountainStand.X, questplan.ErnestFountainStand.Z)
		return false
	}
	mark := time.Now()
	if err := questrunner.UseItemOnLoc(d, questplan.ObjPoisonedFishFood, m); err != nil {
		logf(d, "ernest: pour the poisoned fish food: %v", err)
		return false
	}
	floated := func() bool { return strings.Contains(strings.ToLower(chatSince(d, mark)), "float to the surface") }
	if d.Chat == nil {
		floated = func() bool { return !held(d, questplan.ObjPoisonedFishFood) }
	}
	if !waitUntil(d, 15*time.Second, floated) {
		logf(d, "ernest: the pour never said the piranhas died (poisoned fish food still held: %v)", held(d, questplan.ObjPoisonedFishFood))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	logf(d, "ernest: poured the poisoned fish food into the fountain: the piranhas died")
	return true
}

// ernestGauge searches the poisoned fountain for the gauge, up to three times with the script's own tail between. A bite
// after a pour that said it worked is not a race any longer, and each bite costs a hitpoint: the quest is left alone.
func ernestGauge(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	for attempt := 1; attempt <= 3; attempt++ {
		if ernestLowHP(d, 4) {
			questrunner.EatIfNeeded(d, questplan.ErnestChicken.Sustain.FoodIDs, 4)
			if ernestLowHP(d, 4) {
				e.RequestPark("hitpoints too low to search the fountain again and no food to eat", 20*time.Minute)
				return false
			}
		}
		switch searchFountain(d) {
		case fountainGauge:
			logf(d, "ernest: the gauge is in the pack")
			return true
		case fountainBitten:
			logf(d, "ernest: the poisoned fountain bit on search %d of 3", attempt)
			if !d.Delay(3600 * time.Millisecond) {
				return false
			}
		default:
			if !d.Delay(1200 * time.Millisecond) {
				return false
			}
		}
	}
	if held(d, questplan.ObjPressureGauge) {
		return true
	}
	e.RequestPark("the fountain still bites after the poisoned fish food was poured in", ernestFountainPark)
	return false
}

// ernestKey digs the compost heap with the spade for the closet key. The heap's own Search answers "I'm not looking through
// that with my hands!"; the spade used on it digs, and gives the key only to an account that holds none.
func ernestKey(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjClosetKey) {
		return true
	}
	if !held(d, questplan.ObjSpade) {
		logf(d, "ernest: no spade to dig the compost heap with")
		return false
	}
	if !questrunner.Travel(d, questplan.ErnestCompostStand, 1) {
		logf(d, "ernest: could not reach the compost heap")
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocManorCompost, "", "", questplan.ErnestCompostStand, 6)
	if !ok {
		logf(d, "ernest: no compost heap (loc %d) near %d,%d", questplan.LocManorCompost, questplan.ErnestCompostStand.X, questplan.ErnestCompostStand.Z)
		return false
	}
	for attempt := 0; attempt < 2; attempt++ {
		mark := time.Now()
		if err := questrunner.UseItemOnLoc(d, questplan.ObjSpade, m); err != nil {
			logf(d, "ernest: dig the compost heap: %v", err)
			return false
		}
		if waitUntil(d, 10*time.Second, func() bool { return held(d, questplan.ObjClosetKey) }) {
			logf(d, "ernest: the compost heap gave a key")
			return true
		}
		logf(d, "ernest: the dig gave no key: [%s]", strings.TrimSpace(chatSince(d, mark)))
	}
	return false
}

// ernestClosetFloor is the hitpoints the account walks into the closet with when the skeleton hunts it
// (questplan.ErnestClosetFloor). The fight's own rules (an eat line from the foe, the race judged as it goes, a retreat that
// keeps the last food for the walk) are the shared loop's.
func ernestClosetFloor(d *questrunner.Deps) int {
	_, max := questrunner.Hitpoints(d)
	return questplan.ErnestClosetFloor(max)
}

// skeletonTargetsUs is whether the closet skeleton is facing this account.
func skeletonTargetsUs(d *questrunner.Deps, sk entityquery.Entity) bool {
	self := d.Session.State().Self()
	return self >= 0 && sk.FaceEntity == 32768+self
}

// ernestTube unlocks the closet with the key, takes the tube off the floor (killing the skeleton beside it first if it is
// after the account) and unlocks the door from inside again. An account that stands in the closet already (a restart, a
// park) takes up from there; it ends outside, or says it could not get out.
func ernestTube(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !questplan.InErnestCloset(questrunner.CurrentTile(d)) {
		if held(d, questplan.ObjRubberTube) {
			return true
		}
		if !held(d, questplan.ObjClosetKey) {
			logf(d, "ernest: no key for the closet's door")
			return false
		}
		combat, _ := d.Session.State().CombatLevel()
		if questplan.ErnestSkeletonHunts(combat) {
			floor := ernestClosetFloor(d)
			questrunner.TopUp(d, questplan.ErnestChicken.Sustain.FoodIDs, floor)
			if hp, _ := questrunner.Hitpoints(d); hp < floor {
				logf(d, "ernest: %d hitpoints is under the %d the closet's skeleton wants (combat %d, it hunts up to %d)", hp, floor, combat, 2*questplan.ErnestSkeletonLevel)
				e.RequestPark("hitpoints too low for the closet's skeleton and no food to eat", 20*time.Minute)
				return false
			}
		}
		if !questrunner.Travel(d, questplan.ErnestClosetOutside, 0) || questrunner.CurrentTile(d) != questplan.ErnestClosetOutside {
			logf(d, "ernest: could not stand outside the closet's door")
			return false
		}
		m, ok := questrunner.FindLoc(d, questplan.LocClosetDoor, "", "Open", questplan.ErnestClosetOutside, 3)
		if !ok {
			logf(d, "ernest: no closet door (loc %d) near %d,%d", questplan.LocClosetDoor, questplan.ErnestClosetOutside.X, questplan.ErnestClosetOutside.Z)
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjClosetKey, m); err != nil {
			logf(d, "ernest: the key on the closet door: %v", err)
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questplan.InErnestCloset(questrunner.CurrentTile(d)) }) {
			logf(d, "ernest: the key did not open the closet")
			return false
		}
		logf(d, "ernest: in the closet")
	}
	taken := closetTube(d)
	out := leaveCloset(d)
	if !out {
		logf(d, "ernest: could not get out of the closet (tube held: %v)", held(d, questplan.ObjRubberTube))
	}
	return out && taken
}

// ernestClosetBudget bounds the time in the closet.
const ernestClosetBudget = 3 * time.Minute

// closetTube takes the tube off the closet's floor, fighting the skeleton when it hunts the account.
func closetTube(d *questrunner.Deps) bool {
	deadline := time.Now().Add(ernestClosetBudget)
	for !held(d, questplan.ObjRubberTube) && time.Now().Before(deadline) {
		combat, _ := d.Session.State().CombatLevel()
		if sk, ok := questrunner.FindNpcByID(d, questplan.NpcManorSkeleton, "", 10); ok && (questplan.ErnestSkeletonHunts(combat) || skeletonTargetsUs(d, sk)) {
			at := tile.Tile{X: sk.X, Z: sk.Z, Level: sk.Level}
			logf(d, "ernest: fighting the closet's skeleton at %d,%d (combat %d)", at.X, at.Z, combat)
			if !questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcManorSkeleton, Target: "Skeleton", Anchor: at},
				questplan.ErnestChicken.Sustain) {
				logf(d, "ernest: the skeleton fight did not end in a kill")
				return false
			}
			continue
		}
		if len(questrunner.GroundItemsNear(d, questplan.ObjRubberTube, questplan.ErnestTubeSpawn, 3)) > 0 {
			if questrunner.TakeGround(d, questplan.ObjRubberTube, questplan.ErnestTubeSpawn, 3) {
				break
			}
		} else if !d.Delay(1500 * time.Millisecond) {
			return false
		}
	}
	if held(d, questplan.ObjRubberTube) {
		logf(d, "ernest: the rubber tube is in the pack")
		return true
	}
	logf(d, "ernest: no rubber tube on the closet floor within %s", ernestClosetBudget)
	return false
}

// leaveCloset uses the key on the door from inside, the closet's only way out. A skeleton standing on the tile the door is
// used from is killed or waited out.
func leaveCloset(d *questrunner.Deps) bool {
	if !questplan.InErnestCloset(questrunner.CurrentTile(d)) {
		return true
	}
	if !held(d, questplan.ObjClosetKey) {
		logf(d, "ernest: in the closet with no key: the door opens only to it")
		return false
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if questrunner.CurrentTile(d) != questplan.ErnestClosetInside {
			if !questrunner.WalkNear(d, questplan.ErnestClosetInside, 0) {
				if sk, ok := questrunner.FindNpcByID(d, questplan.NpcManorSkeleton, "", 6); ok && sk.X == questplan.ErnestClosetInside.X && sk.Z == questplan.ErnestClosetInside.Z {
					logf(d, "ernest: the skeleton stands on the door's tile: fighting it")
					questrunner.FightNpc(d, questplan.Step{Kind: questplan.StepFight, TargetID: questplan.NpcManorSkeleton, Target: "Skeleton",
						Anchor: tile.Tile{X: sk.X, Z: sk.Z, Level: sk.Level}}, questplan.ErnestChicken.Sustain)
					continue
				}
				if !d.Delay(2 * time.Second) {
					return false
				}
				continue
			}
		}
		m, ok := questrunner.FindLoc(d, questplan.LocClosetDoor, "", "Open", questplan.ErnestClosetInside, 3)
		if !ok {
			logf(d, "ernest: no closet door near %d,%d", questplan.ErnestClosetInside.X, questplan.ErnestClosetInside.Z)
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjClosetKey, m); err != nil {
			logf(d, "ernest: the key on the closet door from inside: %v", err)
			if !d.Delay(2 * time.Second) {
				return false
			}
			continue
		}
		if waitUntil(d, 8*time.Second, func() bool { return !questplan.InErnestCloset(questrunner.CurrentTile(d)) }) {
			logf(d, "ernest: out of the closet")
			return true
		}
	}
	return false
}

// ernestHandInBudget bounds the hand-in: the conversation, the scene with the dialogue shut (about four seconds), Ernest's
// thanks and the box that hands the coins over.
const ernestHandInBudget = 100 * time.Second

// ernestHandIn gives the Professor the three parts and keeps driving whatever dialogue he opens until the quest tab says it
// is complete. A plain talk step ends at the first lull, and the script closes its dialogue for the scene (if_close, then four
// delays of 1, 2, 2 and 1 ticks before Ernest speaks): it would return mid-scene and the next decision would find the parts
// still in the pack and the Professor busy.
func ernestHandIn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	complete := func() bool { return questrunner.ReadStatus(d, questplan.ErnestChickenName) == questplan.StatusComplete }
	parts := func() bool {
		return held(d, questplan.ObjRubberTube) || held(d, questplan.ObjOilCan) || held(d, questplan.ObjPressureGauge)
	}
	if !questrunner.GotoNpc(d, questplan.OddensteinStop, nil) {
		return false
	}
	if !questrunner.OpenDialogue(d, questplan.OddensteinStop) {
		return false
	}
	deadline := time.Now().Add(ernestHandInBudget)
	for time.Now().Before(deadline) && !complete() {
		questrunner.NewDialogue(d).DriveUntil(complete, questplan.OddensteinStop.Prefer, 25*time.Second)
		if !parts() {
			// The parts are gone: the tab follows within a few ticks.
			waitUntil(d, 10*time.Second, complete)
			break
		}
		d.Delay(600 * time.Millisecond)
	}
	questrunner.ClearLeftoverModal(d)
	logf(d, "ernest: the Professor took the parts (parts still held: %v, quest tab complete: %v)", parts(), complete())
	return complete() || !parts()
}

// The manor's three sealed places are pockets for every quest's walk too: nothing the walker plans leaves them, each is
// left by its own scripted way out, and an account left in one by a relog or a park must not strand the quest after it.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Draynor Manor closet", In: questplan.InErnestCloset, Leave: leaveCloset})
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Draynor Manor ladder alcove", In: questplan.InErnestAlcove, Leave: leaveAlcove})
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Draynor Manor basement", In: questplan.InErnestBasement, Leave: leaveBasement})
}
