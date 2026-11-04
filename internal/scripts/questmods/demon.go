package questmods

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// DemonSlayer is Demon Slayer ready for the engine. Its talks (Gypsy Aris, Sir Prysin, Captain Rovin), its kits (the coins, the banked
// keys, the bones, the bucket, the food) and its equip are generic steps and the provisioner's; the executors below are the parts the
// content makes special: Traiborn's 25 bones and his ritual of message boxes (demonTraiborn), the bucket filled at the kitchen sink
// (demonWater), the drain key, which is a three minute clock from the pour to the sewer floor (demonDrain, demonSewer), Silverlight
// worn with the old weapon remembered (demonWield, demonFinish) and Delrith at the stone circle with the incantation (demonDelrith).
// The bones' skill source, the Lumbridge cows, is demonBones (SkillMethods).
func DemonSlayer() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.DemonSlayer,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomDemonTraiborn: demonTraiborn,
			questplan.CustomDemonWater:    demonWater,
			questplan.CustomDemonDrain:    demonDrain,
			questplan.CustomDemonSewer:    demonSewer,
			questplan.CustomDemonWield:    demonWield,
			questplan.CustomDemonDelrith:  demonDelrith,
		},
		Finish: demonFinish,
	}
}

const (
	// demonTraibornRounds is how many conversations a visit to Traiborn holds: the first promises the bones (stage 3) and the second
	// hands them over; a third covers a lost key, which sets the stage back to 3 and is answered with the bones still in the pack.
	demonTraibornRounds = 3
	// demonTraibornBudget bounds one conversation: 25 bones go a tick each (about fifteen seconds with no dialogue open), then the
	// ritual's three message boxes and verse.
	demonTraibornBudget = 150 * time.Second
	// demonQuietSteps is how many looks (each 600 ms) at an empty screen, with the bones not changing, end a conversation: longer than
	// the script's longest gap but one hand-in tick.
	demonQuietSteps = 14

	// demonKeyLife is how long the pour's key lies in the sewer (questplan.DemonKeyTicks) and demonKeyMargin what is kept back of it for
	// the take.
	demonKeyLife   = questplan.DemonKeyTicks * 600 * time.Millisecond
	demonKeyMargin = 25 * time.Second
	// demonSewerLeaveBudget bounds the walk out of the sewer.
	demonSewerLeaveBudget = 2 * time.Minute
	// demonKeySeen is how long the key gets to appear on the floor after the arrival (the ground item reaches the client with the zone).
	demonKeySeen = 8 * time.Second

	// demonDelrithBudget bounds the visit: the walk in, the blows, the chant.
	demonDelrithBudget = 4 * time.Minute
	// demonDelrithSearch is how far from the circle's middle Delrith is looked for.
	demonDelrithSearch = 16
	// demonDelrithIdle is how many looks (2 s apart) without a Delrith, whole or weakened, end the visit: he respawns within a minute
	// of another account's kill.
	demonDelrithIdle = 30
	// demonRetreatPercent is the hitpoints (percent of the maximum) at or under which an account with no food stops and leaves the circle.
	demonRetreatPercent = 30
	// demonChantBudget bounds the dialogue that follows the weakening, and demonChantTries is how many such conversations a visit holds.
	demonChantBudget = 45 * time.Second
	demonChantTries  = 3
	// demonClickGap is the least time between two attacks sent at Delrith. The server answers an attack at the next tick (a hit, or a
	// refusal in the chat), and OpNpc has no pacing of its own, so a second click inside the gap only floods the connection.
	demonClickGap = 1200 * time.Millisecond
)

// demonWeaponHint prefixes the store hint that names the weapon Silverlight replaced.
const demonWeaponHint = "weapon:"

// ---------------------------------------------------------------------------------------------------------
// Traiborn.
// ---------------------------------------------------------------------------------------------------------

// demonTraiborn takes the bones to Traiborn and returns with his key. It walks to him (the graph climbs the tower's stair), talks and
// drives the conversation until the screen has been quiet while the bones stop going; a talk that ends without the key (the first one
// ends with the bones promised) is followed by another, up to demonTraibornRounds. The message boxes of his ritual are closed as they
// come: they are on the main modal, which the chat driver cannot see, and while one is up the script waits.
func demonTraiborn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	key := func() bool { return held(d, questplan.ObjKeyTraiborn) }
	if key() {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjBones) < 1 {
		logf(d, "demon: no bones to take to Traiborn")
		return false
	}
	stop := questplan.TraibornStop
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "demon: Traiborn was not reached from %v", questrunner.CurrentTile(d))
		return false
	}
	for round := 1; round <= demonTraibornRounds && !key(); round++ {
		if !questrunner.OpenDialogue(d, stop) {
			logf(d, "demon: Traiborn would not talk (round %d of %d)", round, demonTraibornRounds)
			return false
		}
		before := questrunner.HeldID(d, questplan.ObjBones)
		demonDriveUntilQuiet(d, stop.Prefer, key, demonTraibornBudget)
		questrunner.ClearLeftoverModal(d)
		logf(d, "demon: Traiborn, conversation %d: %d bones left of %d, key %v", round, questrunner.HeldID(d, questplan.ObjBones), before, key())
		if questrunner.HeldID(d, questplan.ObjBones) == 0 && !key() {
			// Every bone was handed over and no key came: the count is off by what an earlier visit did, and 25 more is a new trip.
			logf(d, "demon: all the bones are given and Traiborn has not made the key")
			break
		}
	}
	return key()
}

// demonDriveUntilQuiet drives conversations and message boxes until done holds, or until the screen has been empty for demonQuietSteps
// looks with the bones in the pack not changing (the hand-in is a tick a bone with no dialogue open), or the budget runs out. It
// reports whether done held.
func demonDriveUntilQuiet(d *questrunner.Deps, prefer []string, done func() bool, budget time.Duration) bool {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	quiet, bones := 0, questrunner.HeldID(d, questplan.ObjBones)
	for time.Now().Before(deadline) {
		if done() {
			return true
		}
		switch {
		case dl.Ready():
			quiet = 0
			if !dl.Drive(prefer) {
				return done()
			}
			continue
		case demonModalUp(d):
			quiet = 0
			questrunner.ClearLeftoverModal(d)
			continue
		}
		if n := questrunner.HeldID(d, questplan.ObjBones); n != bones {
			bones, quiet = n, 0
		} else if quiet++; quiet >= demonQuietSteps {
			return done()
		}
		if !d.Delay(600 * time.Millisecond) {
			return false
		}
	}
	return done()
}

// demonModalUp reports a message box or any other interface up on the main, side or chat modal.
func demonModalUp(d *questrunner.Deps) bool {
	main, side, chat := d.Session.State().Modals()
	return main != gamestate.NoModal || side != gamestate.NoModal || chat != gamestate.NoModal
}

// ---------------------------------------------------------------------------------------------------------
// The bones.
// ---------------------------------------------------------------------------------------------------------

// demonBonesMaxFailures ends the farm after this many fights in a row fail to start.
const demonBonesMaxFailures = 20

// demonBonesBudget bounds one farming run; the provisioner re-plans after it. It is the time the bones take among the accounts the
// pasture is planned for (questplan.DemonBonesWindow), where it was a flat 14 minutes that no run ever used: the cows were never
// reached.
var demonBonesBudget = questplan.DemonBonesWindow()

// demonBones kills the Lumbridge pasture's cows and takes the bones they drop until the task's quantity more is held (or the budget runs
// out). The pasture is a closed pen whose gate the walker opens: the account walks in to a stand inside it (questplan.CowPastureStand,
// the farm's runCowFarm walks in again if a fight or a random event takes it out) and chooses only cows a walk over open ground reaches
// (Pack.InteractTiles, the imp and goblin farms' rule) so it does not repeat an attack on a cow behind a fence. When no cow in view can
// be fought it walks the pasture's beat instead of waiting where it looked. Cows are level 2 with eight hitpoints and hit for 1 or 2;
// the module's sustain eats if anything goes wrong, and a lost fight walks away and counts toward the quest's park. It reports whether
// it gained a bone.
func demonBones(d *questrunner.Deps, task questplan.SourceTask) bool {
	start := questrunner.HeldID(d, questplan.ObjBones)
	want := start + task.Qty
	// In by the gate before the clock starts: the walk from the bank is minutes, the window is for the cows. (The farm walks in again
	// when it finds the account outside.)
	if !questrunner.Travel(d, questplan.CowPastureStand, questplan.CowPastureEnterRadius) {
		return false
	}
	w := newLiveCows(d, "demon bones", questplan.DemonSlayer.Sustain)
	w.done = func() bool { return questrunner.HeldID(d, questplan.ObjBones) >= want }
	w.room = func() bool { return questrunner.FreeSlots(d) > 0 }
	w.progress = func() string {
		return fmt.Sprintf("%d of %d bones taken", questrunner.HeldID(d, questplan.ObjBones)-start, task.Qty)
	}
	logf(d, "demon bones: the cows for %d (holding %d), up to %s; about %d kills expected in that, at %.1f a minute among %d accounts",
		task.Qty, start, demonBonesBudget, questplan.CowExpectedKills(demonBonesBudget, questplan.DemonCowFarmers),
		questplan.CowRatePerMinute(questplan.DemonCowFarmers), questplan.DemonCowFarmers)
	t := runCowFarm(w, "demon bones", cowSpec{Budget: demonBonesBudget, MaxFailures: demonBonesMaxFailures})
	got := questrunner.HeldID(d, questplan.ObjBones) - start
	logf(d, "demon bones: %d of %d taken from the cows in %s (%d killed, %d given up on, %d timed out; %.1f a minute; %d walks in by the gate, %d stops of the pasture's beat; ended on %s)",
		got, task.Qty, t.Elapsed.Round(time.Second), t.Won, t.GaveUp, t.TimedOut, t.perMinute(), t.Entries, t.Sweeps, t.End)
	return got > 0
}

// ---------------------------------------------------------------------------------------------------------
// The bucket, the drain and the sewer.
// ---------------------------------------------------------------------------------------------------------

// demonWater fills the bucket in the pack at the kitchen's sink. The use is retried from a standstill: sent the moment the walk ends,
// it lands inside the arrival window and is dropped (monkJug's note).
func demonWater(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if held(d, questplan.ObjBucketOfWater) {
		return true
	}
	if !held(d, questplan.ObjBucket) {
		logf(d, "demon: no bucket in the pack to fill")
		return false
	}
	if !questrunner.Travel(d, questplan.DemonSinkStand, 1) {
		logf(d, "demon: the kitchen's sink (%v) was not reached from %v", questplan.DemonSinkStand, questrunner.CurrentTile(d))
		return false
	}
	for try := 1; try <= 3; try++ {
		m, ok := questrunner.FindLoc(d, 0, "Sink", "", questplan.DemonSinkStand, 6)
		if !ok {
			logf(d, "demon: no sink near %v", questplan.DemonSinkStand)
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.ObjBucket, m); err != nil {
			logf(d, "demon: the bucket on the sink at %d,%d: %v", m.X, m.Z, err)
			return false
		}
		if waitUntil(d, 8*time.Second, func() bool { return held(d, questplan.ObjBucketOfWater) }) {
			logf(d, "demon: filled the bucket at the sink at %d,%d", m.X, m.Z)
			return true
		}
		if !d.Delay(1200 * time.Millisecond) {
			return false
		}
	}
	return false
}

// demonDrain pours the bucket of water down the kitchen drain, which puts the drain key on the sewer floor for three minutes, and
// goes for it at once: the walk to the key crosses the Varrock manhole (the graph opens it and climbs down), the key is taken and the
// ladder up is climbed. The clock starts at the pour, so nothing but the walk lies between the two. It reports whether the pack
// holds the key; a key that has gone ends in the account up the ladder again, the empty bucket in its pack, and the next pass fills it.
func demonDrain(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.ObjKeyDrain) {
		return demonSewerLeave(d)
	}
	if !held(d, questplan.ObjBucketOfWater) {
		logf(d, "demon: no bucket of water for the drain")
		return false
	}
	if !questrunner.Travel(d, questplan.DemonDrain, 0) {
		logf(d, "demon: the drain (%v) was not reached from %v", questplan.DemonDrain, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocQuestDrain, "", "", questplan.DemonDrain, 4)
	if !ok {
		logf(d, "demon: no drain near %v", questplan.DemonDrain)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjBucketOfWater, m); err != nil {
		logf(d, "demon: the bucket of water on the drain: %v", err)
		return false
	}
	if !waitUntil(d, 8*time.Second, func() bool { return !held(d, questplan.ObjBucketOfWater) }) {
		logf(d, "demon: the bucket of water did not go down the drain")
		return false
	}
	poured := time.Now()
	logf(d, "demon: poured the bucket down the drain; the key lies in the sewer for %s", demonKeyLife)
	left := demonKeyLife - time.Since(poured) - demonKeyMargin
	arrived := questrunner.TravelFor(d, questplan.DemonSewerKey, 1, left)
	if !arrived && !questplan.InVarrockSewer(questrunner.CurrentTile(d)) {
		logf(d, "demon: the sewer's key tile was not reached from %v within %s", questrunner.CurrentTile(d), left.Round(time.Second))
		return false
	}
	got := demonTakeKey(d)
	if !got {
		logf(d, "demon: no drain key to take at %v (it had gone, or another account took it)", questplan.DemonSewerKey)
	}
	demonSewerLeave(d)
	return got
}

// demonTakeKey takes the drain key from the sewer floor near its tile, waiting a few seconds for it to appear.
func demonTakeKey(d *questrunner.Deps) bool {
	if held(d, questplan.ObjKeyDrain) {
		return true
	}
	waitUntil(d, demonKeySeen, func() bool {
		return len(questrunner.GroundItemsNear(d, questplan.ObjKeyDrain, questplan.DemonSewerKey, 6)) > 0
	})
	return questrunner.TakeGround(d, questplan.ObjKeyDrain, questplan.DemonSewerKey, 6) || held(d, questplan.ObjKeyDrain)
}

// demonSewer takes an account found in the sewer out of it: the key first if one lies near its tile and the pack lacks it (a pour whose
// step was interrupted), then the ladder. It reports whether the account is out.
func demonSewer(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !held(d, questplan.ObjKeyDrain) && len(questrunner.GroundItemsNear(d, questplan.ObjKeyDrain, questplan.DemonSewerKey, 8)) > 0 {
		if questrunner.TravelFor(d, questplan.DemonSewerKey, 1, 60*time.Second) {
			demonTakeKey(d)
		}
	}
	return demonSewerLeave(d)
}

// demonSewerLeave climbs out of the Varrock sewer and reports whether the account is out: the walker takes the ladder up (it is an edge
// of its graph), and a ladder it did not take is climbed by hand from its foot.
func demonSewerLeave(d *questrunner.Deps) bool {
	if !questplan.InVarrockSewer(questrunner.CurrentTile(d)) {
		return true
	}
	if questrunner.TravelFor(d, questplan.DemonManholeStand, 1, demonSewerLeaveBudget) && !questplan.InVarrockSewer(questrunner.CurrentTile(d)) {
		return true
	}
	if !questplan.InVarrockSewer(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.WalkNear(d, questplan.DemonSewerLanding, 0) {
		logf(d, "demon: the sewer's ladder foot (%v) was not reached from %v", questplan.DemonSewerLanding, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCaveLadderUp, "", "Climb-up", questplan.DemonSewerLadder, 2)
	if !ok {
		logf(d, "demon: no ladder up near %v", questplan.DemonSewerLadder)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
		logf(d, "demon: the sewer's ladder: %v", err)
		return false
	}
	out := waitUntil(d, 12*time.Second, func() bool { return !questplan.InVarrockSewer(questrunner.CurrentTile(d)) })
	if out {
		logf(d, "demon: up the ladder, out of the sewer")
	}
	return out
}

// ---------------------------------------------------------------------------------------------------------
// Silverlight and Delrith.
// ---------------------------------------------------------------------------------------------------------

// demonWield wears Silverlight, remembering the weapon it replaces in the quest's store so demonFinish can put it back: the old weapon
// goes to the pack, and the next bank visit of the fleet's trainer would bank it.
func demonWield(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.WornCounts(d)[questplan.ObjSilverlight] > 0 {
		return true
	}
	if !held(d, questplan.ObjSilverlight) {
		logf(d, "demon: no Silverlight in the pack to wield")
		return false
	}
	demonRememberWeapon(e)
	if !questrunner.Equip(d, questplan.ObjSilverlight) {
		logf(d, "demon: Silverlight would not go on")
		return false
	}
	logf(d, "demon: wielding Silverlight")
	return true
}

// demonRightHand is the worn slot a weapon occupies.
const demonRightHand = 3

// demonRememberWeapon records the weapon in the right hand as the quest store's hint "weapon:<id>" (one at a time).
func demonRememberWeapon(e *questrunner.Engine) {
	if e.Store == nil {
		return
	}
	d := e.Deps()
	id := 0
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if it.Slot == demonRightHand && it.ID > 0 && int(it.ID) != questplan.ObjSilverlight {
			id = int(it.ID)
		}
	}
	if id == 0 {
		return
	}
	rec := e.Store.Record(questplan.DemonSlayerID)
	if rec.Hints == nil {
		rec.Hints = map[string]bool{}
	}
	for k := range rec.Hints {
		if strings.HasPrefix(k, demonWeaponHint) {
			delete(rec.Hints, k)
		}
	}
	rec.Hints[fmt.Sprintf("%s%d", demonWeaponHint, id)] = true
	if e.Save != nil {
		e.Save()
	}
}

// demonFinish puts the weapon Silverlight replaced back on, once the quest is complete, when it is still in the pack.
func demonFinish(e *questrunner.Engine) {
	if e.Store == nil {
		return
	}
	d := e.Deps()
	rec := e.Store.Record(questplan.DemonSlayerID)
	for k, on := range rec.Hints {
		if !on || !strings.HasPrefix(k, demonWeaponHint) {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(strings.TrimPrefix(k, demonWeaponHint), "%d", &id); err != nil || id <= 0 {
			delete(rec.Hints, k)
			continue
		}
		switch {
		case questrunner.WornCounts(d)[id] > 0:
			delete(rec.Hints, k)
		case held(d, id):
			if questrunner.Equip(d, id) {
				logf(d, "demon: wielding the %s again", questrunner.ObjName(d, id))
				delete(rec.Hints, k)
			}
		default:
			logf(d, "demon: the %s Silverlight replaced is not in the pack (the trainer banked it); it stays where it is", questrunner.ObjName(d, id))
			delete(rec.Hints, k)
		}
	}
	if e.Save != nil {
		e.Save()
	}
}

// demonDelrith is the stone circle: walk in, attack Delrith with Silverlight worn, eat as the engine's loop does, and drive the
// incantation when his hitpoints are gone and the chat opens (he turns into Weakened Delrith and the account is asked which words are
// right). It never fights anything else (Module.NoRetaliate). The gate (questplan.DemonHuntFit) keeps out every account the dark
// wizards hunt: the circle is single-way combat, and while a wizard's spell has landed in the last eight ticks the server refuses the
// attack on Delrith ("I'm already under attack!"). This is the net under that gate, for an account it read wrongly: the second
// refusal ends the visit. Attacks go out at most one in demonClickGap, and an attack has taken only when Delrith is hit (the account's
// own combat window proves nothing: a wizard's spell opens it too). An account at the retreat line with no food, or a death, ends the
// visit as well. It reports whether the quest is complete.
func demonDelrith(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	complete := func() bool {
		return questrunner.ReadStatus(d, questplan.DemonSlayerName) == questplan.StatusComplete
	}
	if complete() {
		return true
	}
	if questrunner.WornCounts(d)[questplan.ObjSilverlight] == 0 {
		logf(d, "demon: Silverlight is not worn; Delrith cannot be hurt without it")
		return false
	}
	sus := e.Current().Module.Sustain
	mark := time.Now()
	// The top-up first, at the foot of the walk: food is not eaten in a chat.
	questrunner.PrepareForFight(d, sus.FoodIDs, questplan.FoeDelrith.PeakHit())
	if !questrunner.Travel(d, questplan.DelrithSpawn, 4) {
		logf(d, "demon: the stone circle (%v) was not reached from %v", questplan.DelrithSpawn, questrunner.CurrentTile(d))
		return false
	}
	dl := questrunner.NewDialogue(d)
	watch := &demonCircleWatch{d: d}
	deadline := time.Now().Add(demonDelrithBudget)
	idle, clicks, chants := 0, 0, 0
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if complete() {
			questrunner.ClearLeftoverModal(d)
			logf(d, "demon: Delrith is banished; the quest is complete")
			return true
		}
		if questrunner.DiedSince(d, mark) {
			logf(d, "demon: the account died at the stone circle")
			return false
		}
		// The chat: Delrith's hitpoints are gone, he is the Weakened one and the account is being asked for the words. Only the exact
		// line is chosen (a wrong one is no worse than a lost minute, but nothing here guesses), and the message box and the
		// quest-complete scroll that follow are closed by the driver. A conversation that ends without the quest done, three times,
		// is a menu this module does not know: the visit ends.
		if dl.Ready() {
			chants++
			dl.DriveUntil(complete, []string{questplan.DemonIncantation}, demonChantBudget)
			questrunner.ClearLeftoverModal(d)
			if !complete() {
				if chants >= demonChantTries {
					logf(d, "demon: %d conversations at the stone circle and the quest is not complete", chants)
					return false
				}
				d.Delay(600 * time.Millisecond)
			}
			continue
		}
		// A message box or a level-up scroll the blows raised: closed, or every later click is dropped.
		if demonModalUp(d) {
			questrunner.ClearLeftoverModal(d)
			d.Delay(600 * time.Millisecond)
			continue
		}
		if hp, max := questrunner.Hitpoints(d); max > 0 && hp*100 <= max*demonRetreatPercent && sus.FoodHeld(questrunner.PackCounts(d)) == 0 {
			logf(d, "demon: %d of %d hitpoints and no food at the stone circle: leaving", hp, max)
			questrunner.NoteLostFight(d, "Delrith", "the account was down to its last hitpoints with no food")
			questrunner.Travel(d, questplan.PrysinAnchor, 6)
			return false
		}
		npc, ok := demonDelrithNpc(d, questplan.NpcDelrith)
		if !ok {
			if _, weak := demonDelrithNpc(d, questplan.NpcDelrithWeakened); weak {
				idle = 0
				d.Delay(600 * time.Millisecond) // the chat opens a tick after the change
				continue
			}
			if idle++; idle >= demonDelrithIdle {
				logf(d, "demon: no Delrith at the stone circle in %d looks (he respawns within a minute of a kill)", idle)
				return false
			}
			d.Delay(2 * time.Second)
			continue
		}
		idle = 0
		self := d.Session.State().Self()
		if demonCowBusy(npc, self) {
			d.Delay(2 * time.Second) // another account is fighting him: single combat refuses a second
			continue
		}
		if d.Session.State().InCombat() && npc.InCombat {
			d.Delay(600 * time.Millisecond)
			continue
		}
		click := time.Now()
		if err := entityquery.Interact(d.Session, npc, "Attack"); err != nil {
			logf(d, "demon: attacking Delrith: %v", err)
			if !d.Delay(demonClickGap) {
				return false
			}
			continue
		}
		clicks++
		if clicks == 1 || clicks%10 == 0 {
			logf(d, "demon: attacking Delrith at %d,%d (click %d)", npc.X, npc.Z, clicks)
		}
		// The attack has taken when Delrith is hit, a chat opens or a box comes up; a refusal in the chat ends the wait at once.
		waitUntil(d, 3*time.Second, func() bool {
			return dl.Ready() || demonModalUp(d) || watch.refusedSince(click) || demonDelrithHit(d)
		})
		if watch.note(click) {
			logf(d, "demon: %d attacks on Delrith refused: leaving the stone circle", watch.refused)
			questrunner.NoteLostFight(d, "Delrith", "the dark wizards' spells keep the server from starting the fight (\"I'm already under attack!\")")
			questrunner.Travel(d, questplan.PrysinAnchor, 6)
			return false
		}
		if dl.Ready() || demonModalUp(d) {
			continue
		}
		if !d.Delay(demonClickGap) {
			return false
		}
	}
	return complete()
}

// demonCircleWatch counts the server's single-combat refusals of the attacks sent at Delrith: the circle is single-way combat, so
// while a dark wizard's spell has landed in the last eight ticks the server answers an attack with "I'm already under attack!" and
// starts nothing.
type demonCircleWatch struct {
	d       *questrunner.Deps
	refused int
}

// refusedSince reports whether the game chat holds the refusal, said since an attack went out.
func (w *demonCircleWatch) refusedSince(click time.Time) bool {
	return w.d.Chat != nil && questplan.DemonRefusedUnderAttack(w.d.Chat.Since(click))
}

// note folds the answer to the attack sent at click into the count (one refusal at most per attack) and reports whether the visit has
// reached questplan.DemonUnderAttackLimit and must leave.
func (w *demonCircleWatch) note(click time.Time) bool {
	if !w.refusedSince(click) {
		return false
	}
	w.refused++
	logf(w.d, "demon: the server refused the attack on Delrith (\"I'm already under attack!\", %d of %d): a dark wizard's spell landed in the last eight ticks", w.refused, questplan.DemonUnderAttackLimit)
	return w.refused >= questplan.DemonUnderAttackLimit
}

// demonDelrithHit is whether Delrith has been hit lately (the client's combat window on him): the sign an attack took.
func demonDelrithHit(d *questrunner.Deps) bool {
	n, ok := demonDelrithNpc(d, questplan.NpcDelrith)
	return ok && n.InCombat
}

// demonDelrithNpc is the nearest npc of the type near the stone circle's middle.
func demonDelrithNpc(d *questrunner.Deps, id int) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(demonDelrithSearch).
		Where(func(n entityquery.Entity) bool { return n.ID == id }).Nearest()
}
