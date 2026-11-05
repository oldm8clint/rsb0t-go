package questmods

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// BigChompy is Big Chompy Bird Hunting ready for the engine. The start, the generic kit round and the item-on-item fletching are the engine's own
// steps; the customs are the parts the content makes special: Rantz's conversations (his scripts raise message boxes in the middle of a talk
// and loop their menus), the wolves for the bones, the chest and the bellows and the toads, the bait and the shot, the seasonings and the spit.
func BigChompy() questrunner.Quest {
	talk := func(name string, stop questplan.NpcStop, picks []string, done func(d *questrunner.Deps) bool) questrunner.CustomFunc {
		return func(e *questrunner.Engine, _ questplan.Step) bool { return chTalk(e, name, stop, picks, done) }
	}
	return questrunner.Quest{
		Module: questplan.BigChompy,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomChBugs:       chBugs,
			questplan.CustomChChop:       chChop,
			questplan.CustomChWolf:       chWolf,
			questplan.CustomChGive:       chGive,
			questplan.CustomChToadies:    talk(questplan.CustomChToadies, questplan.ChRantzStop, chToadiesMenu, nil),
			questplan.CustomChShowToad:   talk(questplan.CustomChShowToad, questplan.ChRantzStop, []string{"Ok, thanks"}, nil),
			questplan.CustomChBow:        talk(questplan.CustomChBow, questplan.ChRantzStop, chBowMenu, chHasBow),
			questplan.CustomChShowChompy: talk(questplan.CustomChShowChompy, questplan.ChRantzStop, []string{"Ok, thanks"}, nil),
			questplan.CustomChHandOver: talk(questplan.CustomChHandOver, questplan.ChRantzStop, []string{"Ok, thanks"}, func(d *questrunner.Deps) bool {
				return questrunner.HeldID(d, questplan.ObjChSeasonedChomp) == 0
			}),
			questplan.CustomChChest:  chChest,
			questplan.CustomChToad:   chToadRound,
			questplan.CustomChBait:   chBait,
			questplan.CustomChShoot:  chShoot,
			questplan.CustomChKids:   chKids,
			questplan.CustomChSeason: chSeason,
			questplan.CustomChCook:   chCook,
		},
		// The bow replaced the weapon the account fights with: it goes back on once the quest is done.
		Finish: func(e *questrunner.Engine) { chRestoreWeapon(e) },
	}
}

const (
	// chTalkBudget bounds one conversation with Rantz or a child (the longest is the hand-over, a dozen pages and three boxes) and chTalkQuiet is how
	// long nothing may have been asked for before it counts as over: these scripts p_delay between pages.
	chTalkBudget = 90 * time.Second
	chTalkQuiet  = 4 * time.Second
	// chFightBudget bounds one fight with a wolf (69 hitpoints; the price says it ends well inside this).
	chFightBudget = 4 * time.Minute
	// chHuntBudget bounds one hunt for one wolf's bones.
	chHuntBudget = 12 * time.Minute
	// chBaitWait is how long a placed toad is given to bring a chompy (four rolls at twenty-five ticks, and the walk) and chBaitMisses how many
	// toads one step puts down before it gives up.
	chBaitWait   = 75 * time.Second
	chBaitRounds = 4
	// chShotWindow bounds the shots at one chompy bird (its hundred ticks of life, and the walk to range).
	chShotWindow = 70 * time.Second
	// chChestTries is how many times the rock is tried (a Strength roll, a level of Strength lost on each miss).
	chChestTries = 6
	// chPickWait is how long a seasoning's pick or a ground item's respawn is waited for.
	chPickWait = 12 * time.Second
	chCookWait = 25 * time.Second
	chGasWait  = 12 * time.Second
	// chWeaponHint prefixes the store hint that names the weapon the bow replaced; chRightHand is the worn slot a weapon occupies.
	chWeaponHint = "weapon:"
	chRightHand  = 3
)

// The menus Rantz asks, as the content's lines (rantz.rs2): taken once each, in the order they are offered.
var (
	chToadiesMenu = []string{"How do we make the chompys come?", "Ok, thanks"}
	chBowMenu     = []string{"Come on, let me have a go", "I'm actually quite strong", "Yes, I'll buy the bow"}
	// chExits are the lines that leave a menu none of the picks is left in.
	chExits = []string{"Ok, thanks", "Ok thanks", "No thanks"}
)

func chHeld(d *questrunner.Deps, id int) int { return questrunner.HeldID(d, id) }

func chHasBow(d *questrunner.Deps) bool {
	return chHeld(d, questplan.ObjChBow) > 0 || questrunner.WornCounts(d)[questplan.ObjChBow] > 0
}

// chSaw reports a game message (lower-cased substring) that arrived since mark.
func chSaw(d *questrunner.Deps, mark time.Time, needle string) bool {
	return d.Chat != nil && strings.Contains(strings.ToLower(d.Chat.Since(mark)), needle)
}

// chPackSnap is the pack, the hitpoints and the levels as the plan reads them, for a price made in the middle of a step.
func chPackSnap(d *questrunner.Deps) questplan.Snapshot {
	snap := questrunner.BuildSnapshot(d, questplan.ChompyName, 0)
	snap.HP, snap.MaxHP = questrunner.Hitpoints(d)
	snap.Levels = questrunner.BaseLevels(d)
	return snap
}

// chTopUp eats to the hitpoints a fight with what stands on the hills' hostile ground starts at: nothing can be eaten inside a conversation, and a wolf or an ogre that hunts the
// account on a walk does not wait for a bite.
func chTopUp(d *questrunner.Deps) {
	questrunner.PrepareForFight(d, questplan.BigChompy.Sustain.FoodIDs, questplan.ChGroundPeakHit())
}

// chGroundGate is the check every stay on the hills' hostile ground begins with (the pool's lip, the west patches): the account is topped up, and the pack in hand NOW is priced
// against a wolf and an ogre, each on its own (the food may be half spent by the seasonings, or by an earlier round of toads). When the model loses the quest is parked for
// six hours with the reason and the step ends.
func chGroundGate(e *questrunner.Engine, what string) bool {
	d := e.Deps()
	chTopUp(d)
	v := questplan.ChGroundReady(chPackSnap(d))
	if v.OK {
		return true
	}
	reason := "not going to " + what + " on the Feldip hills' hostile ground with this pack: " + v.Why
	logf(d, "chompy: %s", reason)
	e.RequestPark(reason, questplan.ChFightParkFor)
	return false
}

// chTravel walks a leg of the quest: inside the enclave in the east (questplan.ChInEnclave) it is the plain Travel; any other leg (to or from the pool, the west patches, the road
// to Yanille) starts with a top-up and is walked with TravelCautious, which holds auto-retaliate off (a hit in the back does not turn the account round and cancel the walk) and
// plans round an attacker.
func chTravel(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if here, ok := questrunner.CurrentTileOK(d); ok && !questplan.ChLegExposed(here, dest) {
		return chWalk(d, dest, radius)
	}
	chTopUp(d)
	return chWalkCautious(d, dest, radius)
}

// chWalk and chWalkCautious are the two walks chTravel chooses between (seams for the tests: the live ones are questrunner's).
var (
	chWalk         = questrunner.Travel
	chWalkCautious = questrunner.TravelCautious
)

// chAttackerNow is who is fighting the account (a seam for the tests: the live one is questrunner.Attacker).
var chAttackerNow = questrunner.Attacker

// chAttacked reports a monster fighting the account.
func chAttacked(d *questrunner.Deps) bool {
	_, hit := chAttackerNow(d)
	return hit
}

// chWaitCalm waits for cond as waitUntil does, and gives the wait up the moment something starts fighting the account (a stationary wait beside an ogre is where it does): the step
// then fails at once, and the engine's own answer to a step that fails under attack is the fight back.
func chWaitCalm(d *questrunner.Deps, what string, timeout time.Duration, cond func() bool) bool {
	waitUntil(d, timeout, func() bool { return cond() || chAttacked(d) })
	if cond() {
		return true
	}
	if chAttacked(d) {
		logf(d, "chompy: something is fighting the account at %v while it waits for %s", questrunner.CurrentTile(d), what)
	}
	return false
}

// chDrive drives a conversation through its pages, its menus and its message boxes: a main-modal box (an objbox, a mesbox) is continued between pages, a menu
// is answered by the first of picks not yet taken that it offers (each pick is taken once: Rantz's topic menus repeat after every answer), then by a line
// from chExits, and a menu with none of them is closed and ends it. It ends when nothing has been asked for quiet, or at budget; it reports whether
// anything was said at all.
func chDrive(d *questrunner.Deps, picks []string, quiet, budget time.Duration) bool {
	return chDriveUntil(d, picks, quiet, budget, nil)
}

// chDriveUntil is chDrive that stops as soon as until holds (a conversation that ends in a shop's interface: the modal clearing would close the shop).
func chDriveUntil(d *questrunner.Deps, picks []string, quiet, budget time.Duration, until func() bool) bool {
	dl := questrunner.NewDialogue(d)
	taken := map[string]bool{}
	deadline := time.Now().Add(budget)
	last := time.Now()
	spoke := false
	for time.Now().Before(deadline) {
		if until != nil && until() {
			return true
		}
		if dl.Ready() {
			spoke, last = true, time.Now()
			opts := dl.OptionTexts()
			if len(opts) == 0 {
				dl.Drive(nil) // the pages up to the next menu or the end
				continue
			}
			pick, key, ok := "", "", false
			for _, p := range picks {
				if taken[p] {
					continue
				}
				if o, found := questplan.PickPreferred(opts, []string{p}); found {
					pick, key, ok = o, p, true
					break
				}
			}
			if !ok {
				pick, ok = questplan.PickPreferred(opts, chExits)
			}
			if !ok {
				logf(d, "chompy: no line to take in [%s]: closing it", strings.Join(opts, " | "))
				questrunner.ClearLeftoverModal(d)
				return spoke
			}
			if key != "" {
				taken[key] = true
			}
			dl.DriveSequence([]string{pick}, 20*time.Second)
			continue
		}
		if questrunner.ClearLeftoverModal(d) {
			spoke, last = true, time.Now()
			continue
		}
		if time.Since(last) >= quiet {
			return spoke
		}
		if !d.Delay(300 * time.Millisecond) {
			return spoke
		}
	}
	return spoke
}

// chOpen walks to a stop and opens its conversation.
func chOpen(d *questrunner.Deps, stop questplan.NpcStop) bool {
	// From outside the enclave (the pool, the west patches, the bank's road) the way to the stop is a hostile leg: topped up, and walked with TravelCautious.
	if here, ok := questrunner.CurrentTileOK(d); !ok || questplan.ChLegExposed(here, stop.Anchor) {
		if !chTravel(d, stop.Anchor, 4) {
			logf(d, "chompy: %s (%v) was not reached from %v", stop.Npc, stop.Anchor, questrunner.CurrentTile(d))
			return false
		}
	}
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "chompy: %s (%v) was not reached from %v", stop.Npc, stop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		logf(d, "chompy: no conversation with %s from %v", stop.Npc, questrunner.CurrentTile(d))
		return false
	}
	return true
}

// chTalk is a conversation: the walk, the opening, the driving, and the proof that it did what it was for (done, when there is one).
func chTalk(e *questrunner.Engine, name string, stop questplan.NpcStop, picks []string, done func(d *questrunner.Deps) bool) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !chOpen(d, stop) {
		return false
	}
	spoke := chDrive(d, picks, chTalkQuiet, chTalkBudget)
	questrunner.ClearLeftoverModal(d)
	if done != nil {
		if !done(d) {
			logf(d, "chompy: %s: the conversation with %s did not do it", name, stop.Npc)
			return false
		}
		return true
	}
	if !spoke {
		logf(d, "chompy: %s: nothing was said", name)
	}
	return spoke
}

// chGive hands Rantz the six arrows he asks for and takes the toadies menu that follows (it moves the quest to 15): the arrows the pack lost are the proof.
func chGive(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	before := chHeld(d, questplan.ObjChArrow)
	ok := chTalk(e, questplan.CustomChGive, questplan.ChRantzStop, chToadiesMenu, func(d *questrunner.Deps) bool {
		return chHeld(d, questplan.ObjChArrow) <= before-questplan.ChGiveArrows
	})
	if !ok {
		logf(d, "chompy: Rantz did not take the six ogre arrows (the pack held %d, holds %d)", before, chHeld(d, questplan.ObjChArrow))
	}
	return ok
}

// chBugs buys the knife and the chisel from Bugs: ten coins, at stage 5, to an account that does not hold both.
func chBugs(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	have := func() bool { return chHeld(d, questplan.ObjKnife) > 0 && chHeld(d, questplan.ObjChChisel) > 0 }
	if have() {
		return true
	}
	if chHeld(d, questplan.CoinsID) < 10 {
		logf(d, "chompy: no ten coins for Bugs's knife and chisel")
		return false
	}
	if !chOpen(d, questplan.ChBugsStop) {
		return false
	}
	chDrive(d, []string{"Ok, I'll give you 10 bright pretties"}, chTalkQuiet, chTalkBudget)
	questrunner.ClearLeftoverModal(d)
	return have()
}

// chChop cuts one achey log.
func chChop(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "chompy: no room in the pack for an achey log")
		return false
	}
	if !chTravel(d, questplan.ChGrove, 6) {
		logf(d, "chompy: the achey grove %v was not reached from %v", questplan.ChGrove, questrunner.CurrentTile(d))
		return false
	}
	before := chHeld(d, questplan.ObjChLogs)
	for try := 0; try < 3; try++ {
		m, ok := questrunner.FindLoc(d, questplan.LocChAchey, "", "Chop", questrunner.CurrentTile(d), 14)
		if !ok {
			logf(d, "chompy: no achey tree offering Chop near %v", questrunner.CurrentTile(d))
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Chop"); err != nil {
			logf(d, "chompy: chop: %v", err)
			continue
		}
		if waitUntil(d, 40*time.Second, func() bool { return chHeld(d, questplan.ObjChLogs) > before }) {
			return true
		}
	}
	return chHeld(d, questplan.ObjChLogs) > before
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The wolves
// ---------------------------------------------------------------------------------------------------------------------------------

// chWolf kills Feldip wolves until one has left its bones in the pack (or the budget runs out): the shared field hunt (fieldhunt.go) over the wolves' field. The Decide has priced the
// wolf and refused the hunt when the model loses.
func chWolf(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	bones := func() bool { return chHeld(d, questplan.ObjChWolfBones) > 0 }
	if bones() {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "chompy: no room in the pack for a wolf's bones")
		return false
	}
	// A bow in the right hand would shoot at the wolf: the weapon it replaced is put back for the fight.
	chRestoreWeapon(e)
	h := &fieldHunt{d: d, label: "chompy wolf", ids: map[int]bool{questplan.NpcFeldipWolf: true}, field: questplan.ChWolfField, radius: questplan.ChWolfRadius,
		drop: questplan.ObjChWolfBones, done: bones, sus: questplan.BigChompy.Sustain, fightBudget: chFightBudget}
	return h.run(chHuntBudget)
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The weapon the bow replaces
// ---------------------------------------------------------------------------------------------------------------------------------

// chRememberWeapon records the weapon in the right hand as the quest store's hint "weapon:<id>" (one at a time), before the bow takes its place.
func chRememberWeapon(e *questrunner.Engine) {
	if e.Store == nil {
		return
	}
	d := e.Deps()
	id := 0
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if it.Slot == chRightHand && it.ID > 0 && int(it.ID) != questplan.ObjChBow {
			id = int(it.ID)
		}
	}
	if id == 0 {
		return
	}
	rec := e.Store.Record(questplan.ChompyID)
	if rec.Hints == nil {
		rec.Hints = map[string]bool{}
	}
	for k := range rec.Hints {
		if strings.HasPrefix(k, chWeaponHint) {
			delete(rec.Hints, k)
		}
	}
	rec.Hints[fmt.Sprintf("%s%d", chWeaponHint, id)] = true
	if e.Save != nil {
		e.Save()
	}
}

// chRestoreWeapon wields the weapon the bow replaced again, when it is in the pack: for a wolf, and when the quest is done.
func chRestoreWeapon(e *questrunner.Engine) {
	if e.Store == nil {
		return
	}
	d := e.Deps()
	rec := e.Store.Record(questplan.ChompyID)
	for k, on := range rec.Hints {
		if !on || !strings.HasPrefix(k, chWeaponHint) {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(strings.TrimPrefix(k, chWeaponHint), "%d", &id); err != nil || id <= 0 {
			delete(rec.Hints, k)
			continue
		}
		switch {
		case questrunner.WornCounts(d)[id] > 0:
			delete(rec.Hints, k)
		case chHeld(d, id) > 0:
			if questrunner.Equip(d, id) {
				logf(d, "chompy: wielding the %s again", questrunner.ObjName(d, id))
				delete(rec.Hints, k)
			}
		default:
			logf(d, "chompy: the %s the bow replaced is not in the pack; it stays where it is", questrunner.ObjName(d, id))
			delete(rec.Hints, k)
		}
	}
	if e.Save != nil {
		e.Save()
	}
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The chest, the bellows and the toads
// ---------------------------------------------------------------------------------------------------------------------------------

// chBellowsHeld is the id of the bellows in the pack, the fullest first, 0 for none.
func chBellowsHeld(d *questrunner.Deps) int {
	for _, id := range []int{questplan.ObjChBellows3, questplan.ObjChBellows2, questplan.ObjChBellows1, questplan.ObjChBellowsEmpty} {
		if chHeld(d, id) > 0 {
			return id
		}
	}
	return 0
}

// chChest lifts the rock off the ogre chest (a Strength roll that costs a level of Strength when it misses) and searches it for the bellows. The chest holds a
// pair for an account that holds or banks none: "You search but find nothing" means the account owns one already, which the plan then looks for in the bank.
func chChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if chBellowsHeld(d) != 0 {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "chompy: no room in the pack for the bellows")
		return false
	}
	if !chTravel(d, questplan.ChChestNear, 2) {
		logf(d, "chompy: the chest %v was not reached from %v", questplan.ChChestNear, questrunner.CurrentTile(d))
		return false
	}
	mark := time.Now()
	lifted := 0
	for try := 0; try < chChestTries+4 && chBellowsHeld(d) == 0; try++ {
		questrunner.ClearLeftoverModal(d)
		open, openOK := questrunner.FindLoc(d, questplan.LocChChestOpen, "", "Search", questplan.ChChestAt, 4)
		if openOK {
			if err := questrunner.OperateLoc(d, open, "Search"); err != nil {
				logf(d, "chompy: the open chest: %v", err)
				return false
			}
			waitUntil(d, chBoxWait, func() bool {
				return chBellowsHeld(d) != 0 || chSaw(d, mark, "find nothing in the ogre chest") || chSaw(d, mark, "do not have the space")
			})
			questrunner.ClearLeftoverModal(d)
			if chSaw(d, mark, "find nothing in the ogre chest") {
				logf(d, "chompy: the chest is empty: the account owns a pair of bellows already")
				return false
			}
			continue
		}
		if lifted >= chChestTries {
			break
		}
		shut, shutOK := questrunner.FindLoc(d, questplan.LocChChestShut, "", "Unlock", questplan.ChChestAt, 4)
		if !shutOK {
			logf(d, "chompy: no ogre chest at %v", questplan.ChChestAt)
			return false
		}
		lifted++
		if err := questrunner.OperateLoc(d, shut, "Unlock"); err != nil {
			logf(d, "chompy: the rock: %v", err)
			return false
		}
		if !waitUntil(d, chBoxWait, func() bool {
			_, ok := questrunner.FindLoc(d, questplan.LocChChestOpen, "", "Search", questplan.ChChestAt, 4)
			return ok
		}) {
			logf(d, "chompy: the rock was too heavy (try %d of %d)", lifted, chChestTries)
		}
	}
	questrunner.ClearLeftoverModal(d)
	if chBellowsHeld(d) == 0 {
		logf(d, "chompy: the ogre chest gave up no bellows")
		return false
	}
	return true
}

// chBoxWait is how long a loc op gets to put its message on the screen or its result on the pack.
const chBoxWait = 8 * time.Second

// chFillBellows sucks swamp gas into the bellows at the one bubble the pool's lip reaches: three charges.
func chFillBellows(e *questrunner.Engine) bool {
	d := e.Deps()
	if chHeld(d, questplan.ObjChBellows3) > 0 {
		return true
	}
	id := chBellowsHeld(d)
	if id == 0 {
		logf(d, "chompy: no ogre bellows to fill")
		return false
	}
	if !chGroundGate(e, "the swamp pool's lip") {
		return false
	}
	if !chTravel(d, questplan.ChBubbleStand, 2) {
		logf(d, "chompy: the swamp pool's lip %v was not reached from %v", questplan.ChBubbleStand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocChBubbles, "", "", questplan.ChBubbleAt, 1)
	if !ok {
		logf(d, "chompy: no swamp bubbles at %v", questplan.ChBubbleAt)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, id, m); err != nil {
		logf(d, "chompy: the bellows on the swamp bubbles: %v", err)
		return false
	}
	return chWaitCalm(d, "the bellows to fill", chGasWait, func() bool { return chHeld(d, questplan.ObjChBellows3) > 0 })
}

// chInflate uses filled bellows on a swamp toad: a bloated toad.
func chInflate(e *questrunner.Engine, pick int) bool {
	d := e.Deps()
	if chHeld(d, questplan.ObjChBellows3)+chHeld(d, questplan.ObjChBellows2)+chHeld(d, questplan.ObjChBellows1) == 0 {
		if !chFillBellows(e) {
			return false
		}
	}
	id := chBellowsHeld(d)
	if id == 0 || id == questplan.ObjChBellowsEmpty {
		return false
	}
	if !chGroundGate(e, "the swamp pool's lip") {
		return false
	}
	if !chTravel(d, questplan.ChBubbleStand, 2) {
		return false
	}
	toads := entityquery.Npcs(d.Session.State(), d.Config).Within(12).Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcChToad }).Results()
	if len(toads) == 0 {
		logf(d, "chompy: no swamp toad in reach of the pool")
		return false
	}
	toad := toads[pick%len(toads)]
	before := chHeld(d, questplan.ObjChToad)
	if err := questrunner.UseItemOnNpc(d, id, toad); err != nil {
		logf(d, "chompy: the bellows on a swamp toad: %v", err)
		return false
	}
	return chWaitCalm(d, "a toad to bloat", 12*time.Second, func() bool { return chHeld(d, questplan.ObjChToad) > before })
}

// chToads fills the pack with bait, up to the three the quest lets it carry (a fourth lets one go).
func chToads(e *questrunner.Engine) bool {
	d := e.Deps()
	for attempt := 0; attempt < 5 && chHeld(d, questplan.ObjChToad) < questplan.ChToadsMax; attempt++ {
		if !questrunner.WaitForGuardian(d) {
			break
		}
		// Each round is a visit to the pool's lip and is priced on its own: the pack that passed the last one may have lost its food to a wolf.
		if !chGroundGate(e, "the swamp pool's lip") {
			break
		}
		chInflate(e, attempt)
	}
	return chHeld(d, questplan.ObjChToad) > 0
}

func chToadRound(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	return chToads(e)
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The bait and the shot
// ---------------------------------------------------------------------------------------------------------------------------------

// chBaitDown reports a bloated toad on the ground in the clearing.
func chBaitDown(d *questrunner.Deps) bool {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(18).Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcChBloatedToad }).Exists()
}

// chPlaceBait puts a bloated toad down in the clearing Rantz pointed at (the toad's Drop is the quest's placement op, refused outside it).
func chPlaceBait(d *questrunner.Deps) bool {
	if chHeld(d, questplan.ObjChToad) == 0 {
		logf(d, "chompy: no bloated toad to place")
		return false
	}
	if !questplan.ChInBaitZone(questrunner.CurrentTile(d)) && !chTravel(d, questplan.ChBaitSpot, 1) {
		logf(d, "chompy: the bait clearing %v was not reached from %v", questplan.ChBaitSpot, questrunner.CurrentTile(d))
		return false
	}
	if !questplan.ChInBaitZone(questrunner.CurrentTile(d)) {
		logf(d, "chompy: not standing in the bait clearing (at %v): the toad would be refused", questrunner.CurrentTile(d))
		return false
	}
	questrunner.ClearLeftoverModal(d)
	before := chHeld(d, questplan.ObjChToad)
	if err := questrunner.OpHeldByName(d, questplan.ObjChToad, "Drop"); err != nil {
		logf(d, "chompy: placing the toad: %v", err)
		return false
	}
	placed := waitUntil(d, chBoxWait, func() bool { return chHeld(d, questplan.ObjChToad) < before })
	questrunner.ClearLeftoverModal(d)
	return placed
}

// chKeepBaitDown makes sure a toad of the account's is on the ground in the clearing, catching and placing one when there is none.
func chKeepBaitDown(e *questrunner.Engine) bool {
	d := e.Deps()
	if chBaitDown(d) && questplan.ChInBaitZone(questrunner.CurrentTile(d)) {
		return true
	}
	if chHeld(d, questplan.ObjChToad) == 0 && !chToads(e) {
		return false
	}
	if !questplan.ChInBaitZone(questrunner.CurrentTile(d)) && !chTravel(d, questplan.ChBaitSpot, 1) {
		return false
	}
	if chBaitDown(d) {
		return true
	}
	return chPlaceBait(d)
}

// chBait baits the clearing and stands by until Rantz has fired and missed (stages 25 to 35 become 40): he fires only while the account is within twenty
// tiles of him, so the wait is made in the clearing.
func chBait(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	mark := time.Now()
	for round := 0; round < chBaitRounds; round++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if !chKeepBaitDown(e) {
			return false
		}
		waitUntil(d, chBaitWait, func() bool { return chSaw(d, mark, "keeps missing") || chSaw(d, mark, "rubbish") || !chBaitDown(d) })
		if chSaw(d, mark, "keeps missing") || chSaw(d, mark, "rubbish") {
			logf(d, "chompy: Rantz shot at the chompy and missed")
			return true
		}
		logf(d, "chompy: nothing came to the bait in %s: baiting again (round %d of %d)", chBaitWait, round+1, chBaitRounds)
	}
	return chSaw(d, mark, "keeps missing")
}

func chLiveChompy(d *questrunner.Deps) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(18).Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcChChompy }).Nearest()
}

func chDeadChompy(d *questrunner.Deps) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(18).Where(func(e entityquery.Entity) bool { return e.ID == questplan.NpcChChompyDead }).Nearest()
}

// chRecoverArrows picks the spent arrows up off the floor.
func chRecoverArrows(d *questrunner.Deps) bool {
	taken := false
	for stack := 0; stack < 4; stack++ {
		if !questrunner.TakeGround(d, questplan.ObjChArrow, questrunner.CurrentTile(d), 16) {
			break
		}
		taken = true
	}
	return taken
}

// chArm puts the bow in the right hand and the arrows in the quiver, remembering the weapon the bow replaces.
func chArm(e *questrunner.Engine) bool {
	d := e.Deps()
	if questrunner.WornCounts(d)[questplan.ObjChBow] == 0 {
		if chHeld(d, questplan.ObjChBow) == 0 {
			logf(d, "chompy: no ogre bow to wield")
			return false
		}
		chRememberWeapon(e)
		if !questrunner.Equip(d, questplan.ObjChBow) {
			logf(d, "chompy: the ogre bow would not go on")
			return false
		}
	}
	if questrunner.WornCounts(d)[questplan.ObjChArrow] == 0 {
		if chHeld(d, questplan.ObjChArrow) == 0 && !chRecoverArrows(d) {
			logf(d, "chompy: no ogre arrow to quiver")
			return false
		}
		if !questrunner.Equip(d, questplan.ObjChArrow) {
			logf(d, "chompy: the ogre arrows would not go in the quiver")
			return false
		}
	}
	return true
}

// chPluck plucks a dead chompy bird and takes the carcass.
func chPluck(d *questrunner.Deps, dead entityquery.Entity) bool {
	at := tile.Tile{X: dead.X, Z: dead.Z, Level: dead.Level}
	if err := entityquery.Interact(d.Session, dead, "Pluck"); err != nil {
		logf(d, "chompy: plucking the chompy: %v", err)
		return false
	}
	if !waitUntil(d, chBoxWait+4*time.Second, func() bool {
		return len(questrunner.GroundItemsNear(d, questplan.ObjChRawChompy, at, 4)) > 0 || chHeld(d, questplan.ObjChRawChompy) > 0
	}) {
		logf(d, "chompy: the pluck left no raw chompy on the floor")
		return false
	}
	if chHeld(d, questplan.ObjChRawChompy) > 0 {
		return true
	}
	return questrunner.TakeGround(d, questplan.ObjChRawChompy, at, 4) || chHeld(d, questplan.ObjChRawChompy) > 0
}

// chShootBird shoots one chompy bird until it falls or its window ends, and reports whether it fell.
func chShootBird(e *questrunner.Engine, bird entityquery.Entity) bool {
	d := e.Deps()
	deadline := time.Now().Add(chShotWindow)
	clicked := time.Time{}
	for time.Now().Before(deadline) {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if _, dead := chDeadChompy(d); dead {
			return true
		}
		cur, alive := entityquery.Npcs(d.Session.State(), d.Config).Within(20).Where(func(n entityquery.Entity) bool { return n.Index == bird.Index && n.ID == bird.ID }).Nearest()
		if !alive {
			// It flew off, or fell and its body is a new npc this poll has not seen yet.
			d.Delay(1200 * time.Millisecond)
			_, dead := chDeadChompy(d)
			return dead
		}
		if questrunner.WornCounts(d)[questplan.ObjChArrow] == 0 {
			chRecoverArrows(d)
			if !chArm(e) {
				return false
			}
			clicked = time.Time{}
		}
		if time.Since(clicked) >= 6*time.Second {
			if err := entityquery.Interact(d.Session, cur, "Attack"); err != nil {
				logf(d, "chompy: attacking the chompy: %v", err)
				return false
			}
			clicked = time.Now()
		}
		if !d.Delay(1200 * time.Millisecond) {
			return false
		}
	}
	_, dead := chDeadChompy(d)
	return dead
}

// chShoot baits a chompy bird, shoots it with the ogre bow and the ogre arrows, plucks it and takes the raw chompy: the kill that Rantz wants (stage 45
// becomes 50) or another for the spit. It stops with the carcass in the pack.
func chShoot(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if chHeld(d, questplan.ObjChRawChompy) > 0 {
		return true
	}
	for round := 0; round < chBaitRounds; round++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if dead, ok := chDeadChompy(d); ok {
			return chPluck(d, dead)
		}
		if !chArm(e) {
			return false
		}
		bird, alive := chLiveChompy(d)
		if !alive {
			if !chKeepBaitDown(e) {
				return false
			}
			waitUntil(d, chBaitWait, func() bool {
				_, a := chLiveChompy(d)
				_, x := chDeadChompy(d)
				return a || x || !chBaitDown(d)
			})
			if dead, ok := chDeadChompy(d); ok {
				return chPluck(d, dead)
			}
			if bird, alive = chLiveChompy(d); !alive {
				logf(d, "chompy: nothing came to the bait in %s: baiting again (round %d of %d)", chBaitWait, round+1, chBaitRounds)
				continue
			}
		}
		if chShootBird(e, bird) {
			if dead, ok := chDeadChompy(d); ok {
				return chPluck(d, dead)
			}
		}
		logf(d, "chompy: the chompy outlasted its shot window (round %d of %d)", round+1, chBaitRounds)
	}
	return chHeld(d, questplan.ObjChRawChompy) > 0
}

// ---------------------------------------------------------------------------------------------------------------------------------
// The roast
// ---------------------------------------------------------------------------------------------------------------------------------

// chKids asks Bugs and Fycie what they want on the chompy, after Rantz has been shown it: each fixes his choice when asked at that stage.
func chKids(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for _, stop := range []questplan.NpcStop{questplan.ChBugsStop, questplan.ChFycieStop} {
		if !chOpen(d, stop) {
			return false
		}
		if !chDrive(d, nil, chTalkQuiet, chTalkBudget) {
			logf(d, "chompy: %s did not answer", stop.Npc)
			questrunner.ClearLeftoverModal(d)
			return false
		}
		questrunner.ClearLeftoverModal(d)
	}
	return true
}

// chSeason picks the seasoning the step names (Step.ItemID): a patch's vegetable or a ground spawn's leaves.
func chSeason(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	var c questplan.ChSeasoningRef
	found := false
	for _, s := range questplan.ChSeasoningList() {
		if s.ID == step.ItemID {
			c, found = s, true
		}
	}
	if !found {
		logf(d, "chompy: no seasoning with the id %d", step.ItemID)
		return false
	}
	if chHeld(d, c.ID) > 0 {
		return true
	}
	if questrunner.FreeSlots(d) < 1 {
		logf(d, "chompy: no room in the pack for the %s", strings.ToLower(c.Name))
		return false
	}
	// The west patches are among the ogres and the wolves (the onion patch and the tomato spawn one tile from an ogre): priced with the pack in hand now.
	if !questplan.ChInEnclave(c.At) && !chGroundGate(e, "the "+strings.ToLower(c.Name)) {
		return false
	}
	if !chTravel(d, c.At, 3) {
		logf(d, "chompy: the %s at %v was not reached from %v", strings.ToLower(c.Name), c.At, questrunner.CurrentTile(d))
		return false
	}
	before := chHeld(d, c.ID)
	if c.Loc != 0 {
		m, ok := questrunner.FindLoc(d, c.Loc, "", "Pick", c.At, 10)
		if !ok {
			logf(d, "chompy: no %s patch offering Pick near %v", strings.ToLower(c.Name), c.At)
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Pick"); err != nil {
			logf(d, "chompy: picking the %s: %v", strings.ToLower(c.Name), err)
			return false
		}
		return chWaitCalm(d, "the "+strings.ToLower(c.Name)+" to be picked", chPickWait, func() bool { return chHeld(d, c.ID) > before })
	}
	if questrunner.TakeGround(d, c.ID, c.At, 8) {
		return true
	}
	// The spawn is another account's to take now: it comes back on its own timer.
	if !chWaitCalm(d, "the "+strings.ToLower(c.Name)+" to respawn", chPickWait, func() bool { return len(questrunner.GroundItemsNear(d, c.ID, c.At, 8)) > 0 }) {
		return false
	}
	return questrunner.TakeGround(d, c.ID, c.At, 8)
}

// chCook roasts the raw chompy on the empty spit with the seasonings in the pack (the spit refuses below Cooking 30, and until the children have been asked).
func chCook(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if chHeld(d, questplan.ObjChSeasonedChomp) > 0 {
		return true
	}
	if chHeld(d, questplan.ObjChRawChompy) == 0 {
		logf(d, "chompy: no raw chompy to roast")
		return false
	}
	if !chTravel(d, questplan.ChSpitNear, 2) {
		logf(d, "chompy: the spit %v was not reached from %v", questplan.ChSpitAt, questrunner.CurrentTile(d))
		return false
	}
	questrunner.ClearLeftoverModal(d)
	var spit locindex.Match
	ok := waitUntil(d, chBoxWait, func() bool {
		m, found := questrunner.FindLoc(d, questplan.LocChSpitEmpty, "", "", questplan.ChSpitAt, 4)
		spit = m
		return found
	})
	if !ok {
		logf(d, "chompy: the ogre spit is still busy")
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjChRawChompy, spit); err != nil {
		logf(d, "chompy: the raw chompy on the spit: %v", err)
		return false
	}
	waitUntil(d, chCookWait, func() bool {
		main, _, chat := d.Session.State().Modals()
		return chHeld(d, questplan.ObjChSeasonedChomp) > 0 || chHeld(d, questplan.ObjChRuinedChompy) > 0 || main != gamestate.NoModal || chat != gamestate.NoModal
	})
	d.Delay(1500 * time.Millisecond)
	questrunner.ClearLeftoverModal(d)
	if chHeld(d, questplan.ObjChRuinedChompy) > 0 && chHeld(d, questplan.ObjChSeasonedChomp) == 0 {
		// A burn is the spit's one in six, not a stuck step: the roast was made and the seasonings went with it, so the step has done its job and the next
		// decision fetches them and another bird (the quest stays at 55).
		logf(d, "chompy: the chompy burnt: another bird and the seasonings are wanted")
		_ = questrunner.OpHeldByName(d, questplan.ObjChRuinedChompy, "Drop")
		return true
	}
	return chHeld(d, questplan.ObjChSeasonedChomp) > 0
}
