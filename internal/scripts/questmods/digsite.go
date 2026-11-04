package questmods

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Digsite is the Digsite Quest ready for the engine. The talks (the Examiner, the Curator, the students, the panning guide's
// gate) and the shop, bank and mule kits are the engine's generic steps; the customs are the legs the content makes special: the
// pocket of a Digsite workman (Steal-from, a Thieving 25 gate), the bush, the sacks, the river and its tray, the level 3 and training
// digs with a trowel, the expert named on a find, the two winches (a rope tied once, Agility 10) and their shafts, the cave workman who gives
// a key after four pleas, the chest and the barrel, the four mixes, the blast, and the tablet in the skeletons' cave.
func Digsite() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.Digsite,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomDigPickpocket:    digCustomPickpocket,
			questplan.CustomDigBush:          digCustomBush,
			questplan.CustomDigSacks:         digCustomSacks,
			questplan.CustomDigTray:          digCustomTray,
			questplan.CustomDigPan:           digCustomPan,
			questplan.CustomDigGiveSample:    digCustomGiveSample,
			questplan.CustomDigStudentGreen:  digStudentTalk(questplan.DigGreenStop),
			questplan.CustomDigStudentOrange: digStudentTalk(questplan.DigOrangeStop),
			questplan.CustomDigStudentPurple: digStudentTalk(questplan.DigPurpleStop),
			// The same talks, under the names the third exam's steps carry (see questplan.CustomDigStudentGreen3).
			questplan.CustomDigStudentGreen3:  digStudentTalk(questplan.DigGreenStop),
			questplan.CustomDigStudentOrange3: digStudentTalk(questplan.DigOrangeStop),
			questplan.CustomDigStudentPurple3: digStudentTalk(questplan.DigPurpleStop),
			questplan.CustomDigCutOpal:        digCustomCutOpal,
			questplan.CustomDigTalisman:       digCustomTalisman,
			questplan.CustomDigCharcoal:       digCustomCharcoal,
			questplan.CustomDigUseOnExpert:    digCustomUseOnExpert,
			questplan.CustomDigLostLetter:     digCustomLostLetter,
			questplan.CustomDigGiveScroll:     digCustomGiveScroll,
			questplan.CustomDigWest:           digCustomWest,
			questplan.CustomDigChest:          digCustomChest,
			questplan.CustomDigBarrel:         digCustomBarrel,
			questplan.CustomDigMix:            digCustomMix,
			questplan.CustomDigBlast:          digCustomBlast,
			questplan.CustomDigTablet:         digCustomTablet,
			questplan.CustomDigLeave:          digCustomLeave,
		},
	}
}

const (
	// digQuiet is how long a conversation is quiet before it counts as over.
	digQuiet = 5 * time.Second
	// digTalkBudget bounds one such conversation.
	digTalkBudget = 60 * time.Second
	// digPickpocketBudget bounds one stretch of pocket-picking: a sample is three successes in eleven, a rope one in eleven, and a pocket
	// at Thieving 25 gives about half of them, with a stun of eight ticks after each miss.
	digPickpocketBudget = 8 * time.Minute
	// digPanBudget bounds one stretch of panning: an orange sample or an uncut opal is three in forty, a pan and a search about twelve ticks.
	digPanBudget = 10 * time.Minute
	// digDigBudget bounds one stretch of digging, and digDigsPerCall the digs in it; digDigsToDoubt is how many digs of one run find no
	// talisman before the quest asks the expert whether it has been found already (the chance of missing for that long is under one in a
	// thousand).
	digDigBudget    = 7 * time.Minute
	digDigsPerCall  = 60
	digDigsToDoubt  = 140
	digWinchWait    = 15 * time.Second
	digSettleWait   = 12 * time.Second
	digClimbWait    = 15 * time.Second
	digStunTicks    = 9
	digTrayWait     = 100 * time.Second
	digTabletWait   = 90 * time.Second
	digRootWait     = 70 * time.Second
	digBlastWait    = 40 * time.Second
	digPleaBudget   = 60 * time.Second
	digTrayPark     = 30 * time.Minute
	digJunkKeepFree = 6
)

// digDriveAll drives every chat page that appears until none has for quiet (or budget runs out), clearing a message box between pages: a
// quest script's dialogues come in bursts with delays between them. A menu none of the preferences fits is closed, never spun on.
func digDriveAll(d *questrunner.Deps, prefer []string, budget, quiet time.Duration) {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	last := time.Now()
	for time.Now().Before(deadline) {
		if dl.Ready() {
			if !dl.Drive(prefer) && len(dl.OptionTexts()) > 0 {
				logf(d, "digsite: an unexpected menu [%s]: closing it", strings.Join(dl.OptionTexts(), " | "))
				questrunner.ClearLeftoverModal(d)
				return
			}
			last = time.Now()
			continue
		}
		if questrunner.ClearLeftoverModal(d) {
			last = time.Now()
			continue
		}
		if time.Since(last) >= quiet {
			return
		}
		if !d.Delay(300 * time.Millisecond) {
			return
		}
	}
}

// digRegion is where the account stands relative to the shafts.
func digRegion(d *questrunner.Deps) questplan.DigRegion {
	return questplan.DigRegionOf(questrunner.CurrentTile(d))
}

// digBudgetEnd is when a stretch of work started now must end: the budget, and never past the run.
func digBudgetEnd(e *questrunner.Engine, budget time.Duration) time.Time {
	end := time.Now().Add(budget)
	if left := e.RunLeft(); left > 0 && time.Now().Add(left).Before(end) {
		end = time.Now().Add(left)
	}
	return end
}

// digDropJunk drops the pocket's and the dig's loot (and a surplus of a tool a pocket's pickings repeat) until keepFree slots are free. It
// drops ids from questplan.DigJunkIDs and nothing else: the chemicals explode on Drop (25 to 65 damage), so there is no "everything that is
// not kept".
func digDropJunk(d *questrunner.Deps, keepFree int) {
	if questrunner.FreeSlots(d) >= keepFree {
		return
	}
	pack := questrunner.PackCounts(d)
	// A surplus of a tool first (a rope past two, a brush past one), then the loot.
	for id, limit := range questplan.DigKeepLimit {
		for n := pack[id]; n > limit && questrunner.FreeSlots(d) < keepFree; n-- {
			if err := questrunner.OpHeldByName(d, id, "Drop"); err != nil {
				logf(d, "digsite: dropping a spare %s: %v", questrunner.ObjName(d, id), err)
				break
			}
			logf(d, "digsite: dropped a spare %s", questrunner.ObjName(d, id))
			if !d.Delay(700 * time.Millisecond) {
				return
			}
		}
	}
	for _, id := range questplan.DigJunkIDs() {
		if questrunner.FreeSlots(d) >= keepFree {
			return
		}
		if pack[id] == 0 {
			continue
		}
		if err := questrunner.OpHeldByName(d, id, "Drop"); err != nil {
			logf(d, "digsite: dropping %s: %v", questrunner.ObjName(d, id), err)
			continue
		}
		logf(d, "digsite: dropped %s", questrunner.ObjName(d, id))
		if !d.Delay(700 * time.Millisecond) {
			return
		}
	}
}

// digWorkman is the nearest Digsite workman in view that has not refused, if any.
func digWorkman(d *questrunner.Deps, radius int, refused map[int]bool) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(radius).
		Where(func(n entityquery.Entity) bool { return n.ID == questplan.DigNpcWorkman && !refused[n.Index] }).Nearest()
}

// digLevel is a base level, 0 when unread.
func digLevel(d *questrunner.Deps, skill string) int { return questrunner.BaseLevels(d)[skill] }

// digPocketTry is what one attempt on one workman came to.
type digPocketTry int

const (
	// digPocketIdle: nothing came of it (no workman in view, one that would not be reached or answered): look again.
	digPocketIdle digPocketTry = iota
	// digPocketPicked: a pocket was picked.
	digPocketPicked
	// digPocketStunned: a miss, one hitpoint lost; the stun has been waited out.
	digPocketStunned
	// digPocketGated: the content answered that the pocket wants Thieving 25.
	digPocketGated
	// digPocketOver: the run ended while the attempt waited.
	digPocketOver
)

// digPocketBites caps the bites the pocket loop takes to get over its floor in one go.
const digPocketBites = 8

// digPocketThieving is what the quest parks on when the content refuses the pocket for the level.
const digPocketThieving = "the workmen's pockets need Thieving 25"

// digPocketOps is what the pocket loop does to the world, so that a test can run the loop on a world of its own (digPickpocket builds the
// live one).
type digPocketOps struct {
	// have reports that the pack holds what is being stolen toward.
	have func() bool
	// thieving is the base level, 0 unread.
	thieving func() int
	// arrive brings the account to the workmen (false when the walk failed), and begin starts the stretch's budget.
	arrive func() bool
	begin  func()
	// over reports that the stretch's budget, or the run, is spent.
	over func() bool
	// vitals reads the hitpoints now, the most, and what the food in the pack heals; bite eats one food at hp and reports that it did.
	vitals func() (hp, maxHP, food int)
	bite   func(hp int) bool
	// park asks for the quest to be parked for a time, with the reason.
	park func(reason string, skip time.Duration)
	// tidy clears a leftover modal and drops the loot; try is one attempt on one workman.
	tidy func()
	try  func() digPocketTry
	// log is the module's log line (nil: none).
	log func(format string, args ...any)
}

// digPocketResult is what a run of the pocket loop did.
type digPocketResult struct {
	ok           bool // the pack holds what was wanted
	started      bool // the account reached the workmen and began
	picks, stuns int
	rested       bool // the loop stopped at its hitpoint floor and asked for a rest
}

// digPocketHold is the pocket loop's hitpoint guard: true when it may steal. Under the floor it eats (a bite at a time, up to
// digPocketBites) and steals on, and with nothing it can eat it parks the quest for the time the hitpoints take to come back and reports
// false. entering is the start of a stretch (questplan.DigPocketJudge).
func digPocketHold(o digPocketOps, entering bool) bool {
	hp, maxHP := 0, 0
	for bites := 0; ; bites++ {
		var food int
		hp, maxHP, food = o.vitals()
		advice := questplan.DigPocketJudge(hp, maxHP, food, entering)
		if advice == questplan.DigPocketGo {
			return true
		}
		if advice == questplan.DigPocketEat && bites < digPocketBites && o.bite(hp) {
			continue
		}
		break
	}
	rest := questplan.DigPocketRestFor(hp, maxHP)
	reason := fmt.Sprintf("the workmen's pockets cost a hitpoint a miss and this account has %d of %d hitpoints (the floor is %d, the resume %d) with no food it can eat: resting %s",
		hp, maxHP, questplan.DigPocketFloor(maxHP), questplan.DigPocketResume(maxHP), rest)
	if o.log != nil {
		o.log("digsite: %s", reason)
	}
	o.park(reason, rest)
	return false
}

// digPocketLoop steals from the workmen until the pack holds what is wanted, the budget ends, the content refuses the pocket for the level, or
// the hitpoints run to the floor with nothing to eat (digPocketHold).
func digPocketLoop(o digPocketOps) digPocketResult {
	var r digPocketResult
	if o.have() {
		r.ok = true
		return r
	}
	if lvl := o.thieving(); lvl > 0 && lvl < questplan.DigThieving {
		o.park(digPocketThieving, questplan.DigSkillParkFor)
		return r
	}
	// Before the walk to the workmen: an account that cannot take a miss does not go to them.
	if !digPocketHold(o, true) {
		r.rested = true
		return r
	}
	if !o.arrive() {
		return r
	}
	o.begin()
	r.started = true
loop:
	for !o.over() && !o.have() {
		o.tidy()
		if !digPocketHold(o, false) {
			r.rested = true
			break
		}
		switch o.try() {
		case digPocketPicked:
			r.picks++
		case digPocketStunned:
			r.stuns++
		case digPocketGated:
			o.park(digPocketThieving, questplan.DigSkillParkFor)
			break loop
		case digPocketOver:
			break loop
		}
	}
	r.ok = o.have()
	return r
}

// digPickpocket steals from the site's workmen until the pack holds qty of item (or the budget ends). It reports whether it does. A
// failed pocket stuns for eight ticks and costs one hitpoint, which can kill, so the loop keeps to a hitpoint floor (digPocketLoop); the
// loot it does not want is dropped as it comes. The content's levels are read off its own answers ("You need to be at level 25 Thieving"),
// which parks the quest.
func digPickpocket(e *questrunner.Engine, item, qty int, budget time.Duration) bool {
	d := e.Deps()
	var deadline time.Time
	refused := map[int]bool{}
	foods := questplan.Digsite.Sustain.FoodIDs
	res := digPocketLoop(digPocketOps{
		have:     func() bool { return questrunner.HeldID(d, item) >= qty },
		thieving: func() int { return digLevel(d, "thieving") },
		arrive: func() bool {
			if _, ok := digWorkman(d, 12, nil); !ok && !questrunner.Travel(d, questplan.DigWorkmenAnchor, 4) {
				logf(d, "digsite: the workmen were not reached from %v", questrunner.CurrentTile(d))
				return false
			}
			return true
		},
		begin: func() { deadline = digBudgetEnd(e, budget) },
		over:  func() bool { return !time.Now().Before(deadline) },
		vitals: func() (int, int, int) {
			hp, maxHP := questrunner.Hitpoints(d)
			return hp, maxHP, questplan.FoodPoolHeal(questrunner.PackCounts(d), foods)
		},
		// An inventory click closes a chat box, so a box left open from the step before is cleared first (the loop's own tidy does so before
		// every attempt, but the stretch's first bite comes before the first tidy).
		bite: func(hp int) bool {
			questrunner.ClearLeftoverModal(d)
			return questrunner.EatIfNeeded(d, foods, hp)
		},
		park: e.RequestPark,
		tidy: func() {
			questrunner.ClearLeftoverModal(d)
			digDropJunk(d, digJunkKeepFree)
		},
		try: func() digPocketTry { return digPocketAttempt(d, refused) },
		log: func(format string, args ...any) { logf(d, format, args...) },
	})
	if res.started || res.rested {
		digDropJunk(d, digJunkKeepFree)
	}
	if res.started {
		logf(d, "digsite: %d pockets picked, %d misses; holding %d of %s", res.picks, res.stuns, questrunner.HeldID(d, item), questrunner.ObjName(d, item))
	}
	return res.ok
}

// digPocketAttempt is one attempt on one workman: the nearest that has not refused, a walk up to it, Steal-from, and what the game answers.
// A miss is waited out here (eight ticks of stun).
func digPocketAttempt(d *questrunner.Deps, refused map[int]bool) digPocketTry {
	npc, ok := digWorkman(d, 14, refused)
	if !ok {
		for k := range refused {
			delete(refused, k)
		}
		questrunner.Travel(d, questplan.DigWorkmenAnchor, 4)
		if !d.Delay(2 * time.Second) {
			return digPocketOver
		}
		return digPocketIdle
	}
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if tile.Chebyshev(questrunner.CurrentTile(d), at) > 1 {
		if !questrunner.WalkNear(d, at, 1) {
			refused[npc.Index] = true
			return digPocketIdle
		}
		// He wanders: look again from where we stand.
		if npc, ok = digWorkman(d, 6, refused); !ok {
			return digPocketIdle
		}
	}
	before := questrunner.PackCounts(d)
	mark := time.Now()
	if err := entityquery.Interact(d.Session, npc, "Steal-from"); err != nil {
		logf(d, "digsite: stealing from the workman at %d,%d: %v", npc.X, npc.Z, err)
		refused[npc.Index] = true
		return digPocketIdle
	}
	dl := questrunner.NewDialogue(d)
	settled := waitUntil(d, 12*time.Second, func() bool {
		return dl.Ready() || cookSaw(d, mark, "you steal", "you find a specimen", "you fail to pick", "stunned", "thieving to pick") ||
			packChanged(before, questrunner.PackCounts(d))
	})
	if cookSaw(d, mark, "thieving to pick") {
		return digPocketGated
	}
	if !settled {
		refused[npc.Index] = true
		return digPocketIdle
	}
	if dl.Ready() {
		// The stolen sample's box.
		digDriveAll(d, nil, 15*time.Second, 2*time.Second)
	}
	if cookSaw(d, mark, "stunned") {
		if !d.Delay(digStunTicks * 600 * time.Millisecond) {
			return digPocketOver
		}
		return digPocketStunned
	}
	return digPocketPicked
}

// packChanged reports a different pack.
func packChanged(a, b map[int]int) bool {
	if len(a) != len(b) {
		return true
	}
	for k, v := range a {
		if b[k] != v {
			return true
		}
	}
	return false
}

func digCustomPickpocket(e *questrunner.Engine, step questplan.Step) bool {
	defer e.InvalidateStage()
	qty := step.Qty
	if qty < 1 {
		qty = 1
	}
	return digPickpocket(e, step.ItemID, qty, digPickpocketBudget)
}

// digStudentTalk is a talk with one student: no menu, no condition; each of the second and third exam's talks sets that student's errand.
func digStudentTalk(stop questplan.NpcStop) questrunner.CustomFunc {
	return func(e *questrunner.Engine, _ questplan.Step) bool {
		d := e.Deps()
		defer e.InvalidateStage()
		if !questrunner.GotoNpc(d, stop, nil) {
			return false
		}
		if !questrunner.OpenDialogue(d, stop) {
			return false
		}
		ok := questrunner.NewDialogue(d).Drive(stop.Prefer)
		questrunner.ClearLeftoverModal(d)
		return ok
	}
}

// digStudentStop is the student whose npc type is id.
func digStudentStop(id int) (questplan.NpcStop, bool) {
	switch id {
	case questplan.DigNpcGreen:
		return questplan.DigGreenStop, true
	case questplan.DigNpcOrange:
		return questplan.DigOrangeStop, true
	case questplan.DigNpcPurple:
		return questplan.DigPurpleStop, true
	}
	return questplan.NpcStop{}, false
}

// digCustomGiveSample uses a rock sample on its student (opnpcu answers the errand whatever its state) and drives what he says.
func digCustomGiveSample(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	stop, ok := digStudentStop(step.TargetID)
	if !ok || questrunner.HeldID(d, step.ItemID) == 0 {
		logf(d, "digsite: no sample %d to give student %d", step.ItemID, step.TargetID)
		return false
	}
	if !questrunner.GotoNpc(d, stop, nil) {
		return false
	}
	for attempt := 0; attempt < 3 && questrunner.HeldID(d, step.ItemID) > 0; attempt++ {
		npc, found := questrunner.FindNpcByID(d, step.TargetID, "", stop.Leash)
		if !found {
			logf(d, "digsite: student %d is not within %d tiles of %v", step.TargetID, stop.Leash, questrunner.CurrentTile(d))
			return false
		}
		if err := questrunner.UseItemOnNpc(d, step.ItemID, npc); err != nil {
			logf(d, "digsite: the sample on the student: %v", err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, step.ItemID) == 0 })
		digDriveAll(d, nil, digTalkBudget, digQuiet)
	}
	return questrunner.HeldID(d, step.ItemID) == 0
}

// digCustomBush searches the one bush that holds the purple student's sample ("Hey, something has been dropped here...").
func digCustomBush(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjSamplePurple) > 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.DigBushAnchor, 5) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.DigLocSampleBush, "", "Search", questplan.DigBushAnchor, 8)
	if !ok {
		logf(d, "digsite: no sample bush (loc %d) near %v", questplan.DigLocSampleBush, questplan.DigBushAnchor)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "digsite: searching the bush: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, questplan.DigObjSamplePurple) > 0 })
	digDriveAll(d, nil, 30*time.Second, 3*time.Second)
	return questrunner.HeldID(d, questplan.DigObjSamplePurple) > 0
}

// digCustomSacks searches the sample sacks for a specimen jar ("Hey there's something under here.").
func digCustomSacks(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjSpecimenJar) > 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.DigSacksAnchor, 5) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.DigLocSacks, "", "Search", questplan.DigSacksAnchor, 8)
	if !ok {
		logf(d, "digsite: no sample sacks (loc %d) near %v", questplan.DigLocSacks, questplan.DigSacksAnchor)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "digsite: searching the sacks: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, questplan.DigObjSpecimenJar) > 0 })
	digDriveAll(d, nil, 30*time.Second, 3*time.Second)
	return questrunner.HeldID(d, questplan.DigObjSpecimenJar) > 0
}

// digCustomTray takes the panning tray from its one spawn (it comes back after a pickup, so another account's tray is waited for).
func digCustomTray(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	has := func() bool {
		return questrunner.HeldID(d, questplan.DigObjTrayEmpty)+questrunner.HeldID(d, questplan.DigObjTrayMud) > 0
	}
	if has() {
		return true
	}
	// The spawn's own tile is not walkable: the pickup is sent from the tile beside it, which the walk is to end on.
	if !questrunner.Travel(d, questplan.DigTrayStand, 0) && questrunner.CurrentTile(d) != questplan.DigTrayStand {
		logf(d, "digsite: the tray's stand %v was not reached from %v", questplan.DigTrayStand, questrunner.CurrentTile(d))
		return false
	}
	deadline := digBudgetEnd(e, digTrayWait)
	for time.Now().Before(deadline) {
		if questrunner.TakeGround(d, questplan.DigObjTrayEmpty, questplan.DigTraySpawn, 3) || has() {
			return true
		}
		if !d.Delay(3 * time.Second) {
			return has()
		}
	}
	e.RequestPark("the panning tray's one spawn stayed empty for "+digTrayWait.String()+": another account holds it", digTrayPark)
	return false
}

// digCustomPan pans the river until the pack holds the find (an orange sample or an uncut opal): a pan fills the tray, a search of the tray
// empties it and finds one of ten things in forty. The panning guide stops the first pan of an account that has not given him tea (the tray
// is not filled, a chat opens): with tea in the pack the same conversation gives it to him and the next pan goes through; without, it
// ends in his request and the step reports failure, which is how the plan knows to buy a cup.
func digCustomPan(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	goal := step.ItemID
	if questrunner.HeldID(d, goal) > 0 {
		return true
	}
	pans := 0
	if !questrunner.Travel(d, questplan.DigPanAnchor, 3) {
		return false
	}
	deadline := digBudgetEnd(e, digPanBudget)
	for time.Now().Before(deadline) && questrunner.HeldID(d, goal) == 0 {
		// Whatever box the last pan or search left up.
		digDriveAll(d, questplan.DigGuideStop.Prefer, 10*time.Second, 1500*time.Millisecond)
		digDropJunk(d, digJunkKeepFree)
		if questrunner.HeldID(d, questplan.DigObjTrayMud) > 0 {
			if err := questrunner.OpHeldByName(d, questplan.DigObjTrayMud, "Search"); err != nil {
				logf(d, "digsite: searching the tray: %v", err)
				return pans > 0
			}
			waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.DigObjTrayMud) == 0 })
			continue
		}
		if questrunner.HeldID(d, questplan.DigObjTrayEmpty) == 0 {
			logf(d, "digsite: no panning tray in the pack")
			return pans > 0
		}
		m, ok := questrunner.FindLoc(d, questplan.DigLocPanning, "", "Look", questplan.DigPanAnchor, 10)
		if !ok {
			logf(d, "digsite: no panning point near %v", questplan.DigPanAnchor)
			questrunner.Travel(d, questplan.DigPanAnchor, 3)
			continue
		}
		teaBefore := questrunner.HeldID(d, questplan.DigObjTea)
		if err := questrunner.OperateLoc(d, m, "Look"); err != nil {
			logf(d, "digsite: panning: %v", err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, digSettleWait, func() bool { return questrunner.HeldID(d, questplan.DigObjTrayMud) > 0 || dl.Ready() })
		if questrunner.HeldID(d, questplan.DigObjTrayMud) > 0 {
			pans++
			continue
		}
		if dl.Ready() {
			// The guide's gate: "So how do I become invited then?", and the tea if the pack holds one.
			digDriveAll(d, questplan.DigGuideStop.Prefer, digTalkBudget, 3*time.Second)
			if questrunner.HeldID(d, questplan.DigObjTea) < teaBefore {
				logf(d, "digsite: the panning guide took the tea at the water")
				continue
			}
			logf(d, "digsite: the panning guide refused the pan: he wants a cup of tea")
			return false
		}
	}
	digDropJunk(d, digJunkKeepFree)
	logf(d, "digsite: %d trays panned; holding %d of %s", pans, questrunner.HeldID(d, goal), questrunner.ObjName(d, goal))
	return questrunner.HeldID(d, goal) > 0 || pans > 0
}

// digCustomCutOpal cuts the uncut opal with the chisel (Crafting 1; at that level half the cuts smash the stone, which the plan answers
// with another pan).
func digCustomCutOpal(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjUncutOpal) == 0 || questrunner.HeldID(d, questplan.DigObjChisel) == 0 {
		return false
	}
	if err := questrunner.UseItemOnItem(d, questplan.DigObjChisel, questplan.DigObjUncutOpal); err != nil {
		logf(d, "digsite: the chisel on the uncut opal: %v", err)
		return false
	}
	return waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.DigObjUncutOpal) == 0 })
}

// digDigCounts counts the digs of a run that found no talisman, by engine (the engine is the account's).
var digDigCounts sync.Map // *questrunner.Engine -> *digCount

type digCount struct {
	mu   sync.Mutex
	n    int
	last time.Time
}

// digCountFor is the dig counter of the engine, reset when it has been idle for a quarter of an hour (a new run).
func digCountFor(e *questrunner.Engine) *digCount {
	v, _ := digDigCounts.LoadOrStore(e, &digCount{})
	c := v.(*digCount)
	c.mu.Lock()
	if time.Since(c.last) > 15*time.Minute {
		c.n = 0
	}
	c.last = time.Now()
	c.mu.Unlock()
	return c
}

// digDigZone digs a zone's soil with the trowel until want holds, maxDigs digs have been made or the budget ends. A dig is "You dig through
// the earth..." and, six ticks on, one line of the zone's loot table (every branch of every table starts "You find"). Three digs in a row
// that never begin are a refusal (a missing jar or brush, a full pack), and end it, as does a stand the walk could not reach. It returns the
// digs made.
func digDigZone(e *questrunner.Engine, zone questplan.DigZone, want func() bool, maxDigs int, budget time.Duration) (digs int, refused bool) {
	d := e.Deps()
	if want() {
		return 0, false
	}
	if !questrunner.Travel(d, zone.Stand, 1) {
		logf(d, "digsite: the dig site's stand %v was not reached from %v", zone.Stand, questrunner.CurrentTile(d))
		return 0, true
	}
	deadline := digBudgetEnd(e, budget)
	bad := 0
	for time.Now().Before(deadline) && digs < maxDigs && !want() {
		digDriveAll(d, nil, 8*time.Second, 1500*time.Millisecond)
		digDropJunk(d, digJunkKeepFree)
		if questrunner.HeldID(d, questplan.DigObjTrowel) == 0 {
			logf(d, "digsite: no trowel in the pack to dig with")
			return digs, true
		}
		m, ok := questrunner.FindLoc(d, 0, "Soil", "", zone.Stand, 6)
		if !ok || !zone.Contains(tile.Tile{X: m.X, Z: m.Z}) {
			logf(d, "digsite: no soil of the zone x %d..%d, z %d..%d near %v", zone.MinX, zone.MaxX, zone.MinZ, zone.MaxZ, zone.Stand)
			questrunner.Travel(d, zone.Stand, 1)
			if !d.Delay(2 * time.Second) {
				return digs, false
			}
			bad++
			if bad >= 3 {
				return digs, true
			}
			continue
		}
		mark := time.Now()
		if err := questrunner.UseItemOnLoc(d, questplan.DigObjTrowel, m); err != nil {
			logf(d, "digsite: the trowel on the soil at %d,%d: %v", m.X, m.Z, err)
			bad++
			if bad >= 3 {
				return digs, true
			}
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 15*time.Second, func() bool {
			return want() || dl.Ready() || cookSaw(d, mark, "you dig through the earth", "enough space", "you need a sample jar", "specimen brush", "can't dig here")
		})
		if want() {
			digs++
			break
		}
		if cookSaw(d, mark, "you dig through the earth") {
			bad = 0
			digs++
			// The find lands six ticks after that line.
			waitUntil(d, digSettleWait, func() bool { return want() || dl.Ready() || cookSaw(d, mark, "you find", "you carefully clean") })
			continue
		}
		// No dig began: a workman's chat, or a message that says why.
		if text := strings.TrimSpace(d.Chat.Since(mark)); text != "" {
			logf(d, "digsite: the dig was refused: %q", strings.ReplaceAll(text, "\n", " | "))
		}
		bad++
		if bad >= 3 {
			digDriveAll(d, nil, 10*time.Second, 2*time.Second)
			return digs, true
		}
	}
	digDriveAll(d, nil, 10*time.Second, 2*time.Second)
	digDropJunk(d, digJunkKeepFree)
	return digs, false
}

// digCustomTalisman digs the level 3 site for the strange talisman (one dig in twenty, once ever). It reports false after a refusal, or
// after digDigsToDoubt digs of a run have found nothing: then the plan asks the expert about a talisman he has already seen.
func digCustomTalisman(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjTalisman) > 0 {
		return true
	}
	for _, need := range []struct {
		id   int
		what string
	}{{questplan.DigObjTrowel, "a trowel"}, {questplan.DigObjSpecimenJar, "a specimen jar"}, {questplan.DigObjSpecimenBrush, "a specimen brush"}} {
		if questrunner.HeldID(d, need.id) == 0 {
			logf(d, "digsite: the level 3 site wants %s and the pack has none", need.what)
			return false
		}
	}
	c := digCountFor(e)
	want := func() bool { return questrunner.HeldID(d, questplan.DigObjTalisman) > 0 }
	digs, refused := digDigZone(e, questplan.DigLevel3Zone, want, digDigsPerCall, digDigBudget)
	c.mu.Lock()
	c.n += digs
	n := c.n
	c.mu.Unlock()
	if want() {
		c.mu.Lock()
		c.n = 0
		c.mu.Unlock()
		return true
	}
	if refused {
		return false
	}
	if n >= digDigsToDoubt {
		logf(d, "digsite: %d digs of the level 3 site found no talisman", n)
		return false
	}
	return true
}

// digCustomCharcoal digs the training site for a charcoal (one dig in thirteen), before any explosive item is made.
func digCustomCharcoal(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjCharcoal) > 0 {
		return true
	}
	want := func() bool { return questrunner.HeldID(d, questplan.DigObjCharcoal) > 0 }
	_, refused := digDigZone(e, questplan.DigTrainingZone, want, digDigsPerCall, digDigBudget)
	if want() {
		return true
	}
	if refused {
		return false
	}
	return true
}

// digExpertStop is the expert's talk stop with a single menu preference (the executors that use an item on him have no menu).
var digExpertStop = questplan.NpcStop{Npc: "Archaeological expert", ID: questplan.DigNpcExpert, Anchor: questplan.DigExpertAnchor, Leash: 8,
	Prefer: []string{"No thanks."}}

// digCustomUseOnExpert shows an item to the expert (the talisman, the powder, the liquid, the tablet) and drives his answer.
func digCustomUseOnExpert(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	item := step.ItemID
	if questrunner.HeldID(d, item) == 0 {
		return false
	}
	if !questrunner.GotoNpc(d, digExpertStop, nil) {
		return false
	}
	for attempt := 0; attempt < 3 && questrunner.HeldID(d, item) > 0; attempt++ {
		npc, found := questrunner.FindNpcByID(d, questplan.DigNpcExpert, "", digExpertStop.Leash+4)
		if !found {
			logf(d, "digsite: the expert is not within reach of %v", questrunner.CurrentTile(d))
			return false
		}
		if err := questrunner.UseItemOnNpc(d, item, npc); err != nil {
			logf(d, "digsite: the %s on the expert: %v", questrunner.ObjName(d, item), err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, item) == 0 })
		digDriveAll(d, nil, 2*digTalkBudget, 6*time.Second)
	}
	return questrunner.HeldID(d, item) == 0
}

// digCustomLostLetter asks the expert for the invitation letter again ("I lost the letter you gave me." exists only for an account he has
// already given one to); any other menu is closed with "No thanks.".
func digCustomLostLetter(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	stop := questplan.DigExpertLetterStop
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	digDriveAll(d, stop.Prefer, digTalkBudget, 4*time.Second)
	c := digCountFor(e)
	c.mu.Lock()
	c.n = 0
	c.mu.Unlock()
	return true
}

// digCustomGiveScroll shows the invitation letter to a workman, who takes it ("I give permission... blah de blah") and moves the quest to
// the shafts.
func digCustomGiveScroll(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjExpertScroll) == 0 {
		return false
	}
	if _, ok := digWorkman(d, 12, nil); !ok && !questrunner.Travel(d, questplan.DigWorkmenAnchor, 4) {
		return false
	}
	for attempt := 0; attempt < 4 && questrunner.HeldID(d, questplan.DigObjExpertScroll) > 0; attempt++ {
		npc, ok := digWorkman(d, 14, nil)
		if !ok {
			questrunner.Travel(d, questplan.DigWorkmenAnchor, 4)
			continue
		}
		if err := questrunner.UseItemOnNpc(d, questplan.DigObjExpertScroll, npc); err != nil {
			logf(d, "digsite: the letter on the workman: %v", err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, questplan.DigObjExpertScroll) == 0 })
		digDriveAll(d, nil, digTalkBudget, digQuiet)
	}
	return questrunner.HeldID(d, questplan.DigObjExpertScroll) == 0
}

// digRopeOut is the rope loc of the region the account is in.
func digRopeOut(d *questrunner.Deps) (int, bool) {
	switch digRegion(d) {
	case questplan.DigShaftEast, questplan.DigAltarEast:
		return questplan.DigLocRopeEast, true
	case questplan.DigShaftWest, questplan.DigAltarWest:
		return questplan.DigLocRopeWest, true
	}
	return 0, false
}

// digClimb climbs the shaft's rope back to the surface. It reports whether the account is out.
func digClimb(d *questrunner.Deps) bool {
	for attempt := 0; attempt < 3; attempt++ {
		id, in := digRopeOut(d)
		if !in {
			return true
		}
		here := questrunner.CurrentTile(d)
		m, ok := questrunner.FindLoc(d, id, "", "Climb-up", here, 40)
		if !ok {
			logf(d, "digsite: no rope (loc %d) near %v in the %s", id, here, digRegion(d))
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Climb-up"); err != nil {
			logf(d, "digsite: climbing the rope: %v", err)
			continue
		}
		if waitUntil(d, digClimbWait, func() bool { return digRegion(d) == questplan.DigSurface }) {
			d.Delay(time.Second)
			return true
		}
	}
	return digRegion(d) == questplan.DigSurface
}

func digCustomLeave(e *questrunner.Engine, _ questplan.Step) bool {
	defer e.InvalidateStage()
	return digClimb(e.Deps())
}

// digDescend operates a winch and waits to arrive in the shaft it leads to. A winch whose bucket has no rope tied answers "The bucket
// descends, but does not reach the bottom." and a chat: the rope is then taken from the pack (pickpocketed when the pack has none) and used
// on the winch, which ties it for good. Agility 10 is the content's gate.
func digDescend(e *questrunner.Engine, winch int, anchor tile.Tile, arrived func() bool) bool {
	d := e.Deps()
	if arrived() {
		return true
	}
	if lvl := digLevel(d, "agility"); lvl > 0 && lvl < questplan.DigAgility {
		e.RequestPark("the winch needs Agility 10", questplan.DigSkillParkFor)
		return false
	}
	if !questrunner.Travel(d, anchor, 4) {
		logf(d, "digsite: the winch at %v was not reached from %v", anchor, questrunner.CurrentTile(d))
		return false
	}
	for attempt := 0; attempt < 4 && !arrived(); attempt++ {
		m, ok := questrunner.FindLoc(d, winch, "", "Operate", anchor, 8)
		if !ok {
			logf(d, "digsite: no winch (loc %d) near %v", winch, anchor)
			return false
		}
		mark := time.Now()
		if err := questrunner.OperateLoc(d, m, "Operate"); err != nil {
			logf(d, "digsite: operating the winch: %v", err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, digWinchWait, func() bool {
			return arrived() || dl.Ready() || cookSaw(d, mark, "does not reach the bottom", "agility level of 10")
		})
		if arrived() {
			d.Delay(1500 * time.Millisecond)
			return true
		}
		if cookSaw(d, mark, "agility level of 10") {
			e.RequestPark("the winch needs Agility 10", questplan.DigSkillParkFor)
			return false
		}
		// The bucket stopped short: no rope on it yet.
		digDriveAll(d, nil, 15*time.Second, 2*time.Second)
		if questrunner.HeldID(d, questplan.DigObjRope) == 0 {
			logf(d, "digsite: the winch needs a rope tied to its bucket and the pack has none: picking pockets for one")
			if !digPickpocket(e, questplan.DigObjRope, 1, digPickpocketBudget) {
				return false
			}
			if !questrunner.Travel(d, anchor, 4) {
				return false
			}
			continue
		}
		m, ok = questrunner.FindLoc(d, winch, "", "Operate", anchor, 8)
		if !ok {
			return false
		}
		before := questrunner.HeldID(d, questplan.DigObjRope)
		tieMark := time.Now()
		if err := questrunner.UseItemOnLoc(d, questplan.DigObjRope, m); err != nil {
			logf(d, "digsite: the rope on the winch: %v", err)
			continue
		}
		waitUntil(d, digSettleWait, func() bool {
			return arrived() || questrunner.HeldID(d, questplan.DigObjRope) < before || cookSaw(d, tieMark, "tie the rope")
		})
		digDriveAll(d, nil, 6*time.Second, 1500*time.Millisecond)
	}
	if !arrived() {
		logf(d, "digsite: the winch at %v never set the account down in its shaft (at %v)", anchor, questrunner.CurrentTile(d))
	}
	return arrived()
}

// digWestNeeds reports what the west shaft still has to give: the key and the root.
func digWestNeeds(d *questrunner.Deps) (key, root bool) {
	held := func(id int) int { return questrunner.HeldID(d, id) }
	mixed := held(questplan.DigObjPreMix)+held(questplan.DigObjPostMix)+held(questplan.DigObjCompound) > 0
	key = held(questplan.DigObjChestKey) == 0 && held(questplan.DigObjPowder) == 0 && held(questplan.DigObjNitrate) == 0 && !mixed
	root = held(questplan.DigObjArceniaRoot) == 0 && held(questplan.DigObjCompound) == 0
	return key, root
}

// digCustomWest is the private winch's trip: down the west shaft, the cave workman's key (four pleas), an arcenia root off the floor, and up
// the rope again. The west shaft is a closed pocket with no monster in it.
func digCustomWest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if digRegion(d) != questplan.DigShaftWest {
		if digRegion(d) != questplan.DigSurface && !digClimb(d) {
			return false
		}
		if !digDescend(e, questplan.DigLocPrivateWinch, questplan.DigPrivateWinch, func() bool { return digRegion(d) == questplan.DigShaftWest }) {
			return false
		}
	}
	needKey, needRoot := digWestNeeds(d)
	if needKey {
		stop := questplan.NpcStop{Npc: "Digsite workman", ID: questplan.DigNpcCaveWorkman, Anchor: questplan.DigCaveWorkmanAnchor, Leash: 10, Prefer: questplan.DigKeyPleas}
		// The scene of a shaft the winch has just set the account down in takes a moment to fill with its npcs.
		waitUntil(d, 8*time.Second, func() bool {
			_, ok := questrunner.FindNpcByID(d, questplan.DigNpcCaveWorkman, "", 16)
			return ok
		})
		if questrunner.GotoNpc(d, stop, nil) && questrunner.OpenDialogue(d, stop) {
			dl := questrunner.NewDialogue(d)
			dl.DriveUntil(func() bool { return questrunner.HeldID(d, questplan.DigObjChestKey) > 0 }, questplan.DigKeyPleas, digPleaBudget)
			digDriveAll(d, questplan.DigKeyPleas, 15*time.Second, 2*time.Second)
		}
		if questrunner.HeldID(d, questplan.DigObjChestKey) == 0 {
			logf(d, "digsite: the cave workman kept his key this time")
		}
	}
	if needRoot && questrunner.HeldID(d, questplan.DigObjArceniaRoot) == 0 {
		deadline := digBudgetEnd(e, digRootWait)
		for time.Now().Before(deadline) && questrunner.HeldID(d, questplan.DigObjArceniaRoot) == 0 {
			for _, at := range questplan.DigRootSpawns {
				if questrunner.TakeGround(d, questplan.DigObjArceniaRoot, at, 4) {
					break
				}
				// Not in view from here: walk to it.
				if len(questrunner.GroundItemsNear(d, questplan.DigObjArceniaRoot, at, 4)) == 0 {
					questrunner.Travel(d, at, 2)
					if questrunner.TakeGround(d, questplan.DigObjArceniaRoot, at, 4) {
						break
					}
				}
			}
			if questrunner.HeldID(d, questplan.DigObjArceniaRoot) == 0 && !d.Delay(5*time.Second) {
				break
			}
		}
	}
	nowKey, nowRoot := digWestNeeds(d)
	progress := (needKey && !nowKey) || (needRoot && !nowRoot)
	if !nowKey && !nowRoot {
		digClimb(d)
		return true
	}
	if !progress {
		// Nothing came of the trip: out of the shaft, and the step is reported failed.
		digClimb(d)
		return false
	}
	// Half the trip is done (the key but not the root, say): the next pass finishes it from where the account stands.
	return true
}

// digFindChest reports whether the chest is open and whether it is shut, near its anchor.
func digFindChest(d *questrunner.Deps) (open, shut bool) {
	_, open = questrunner.FindLoc(d, questplan.DigLocChestOpen, "", "Search", questplan.DigChestAnchor, 5)
	_, shut = questrunner.FindLoc(d, questplan.DigLocChestShut, "", "Open", questplan.DigChestAnchor, 5)
	return open, shut
}

// digCustomChest opens the chest with the key (or finds it open, another account having used it) and searches it for the powder.
func digCustomChest(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjPowder) > 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.DigChestAnchor, 3) {
		return false
	}
	open, _ := digFindChest(d)
	if !open {
		shut, ok := questrunner.FindLoc(d, questplan.DigLocChestShut, "", "Open", questplan.DigChestAnchor, 5)
		if !ok || questrunner.HeldID(d, questplan.DigObjChestKey) == 0 {
			logf(d, "digsite: the chest is shut (found %v) and the pack has no chest key", ok)
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.DigObjChestKey, shut); err != nil {
			logf(d, "digsite: the key on the chest: %v", err)
			return false
		}
		waitUntil(d, 12*time.Second, func() bool {
			o, _ := digFindChest(d)
			return o
		})
	}
	m, ok := questrunner.FindLoc(d, questplan.DigLocChestOpen, "", "Search", questplan.DigChestAnchor, 5)
	if !ok {
		logf(d, "digsite: the chest never opened")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "digsite: searching the chest: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, questplan.DigObjPowder) > 0 })
	digDriveAll(d, nil, 20*time.Second, 3*time.Second)
	return questrunner.HeldID(d, questplan.DigObjPowder) > 0
}

// digCustomBarrel levers the barrel open with the trowel (every pass: a second lever changes nothing, and a missing one leaves "It's not
// open!") and dips a vial in it for the unidentified liquid, which explodes on Drop.
func digCustomBarrel(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjLiquid) > 0 {
		return true
	}
	if questrunner.HeldID(d, questplan.DigObjVial) == 0 || questrunner.HeldID(d, questplan.DigObjTrowel) == 0 {
		logf(d, "digsite: the barrel wants a vial and a trowel and the pack has %d and %d", questrunner.HeldID(d, questplan.DigObjVial), questrunner.HeldID(d, questplan.DigObjTrowel))
		return false
	}
	if !questrunner.Travel(d, questplan.DigBarrelAnchor, 3) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.DigLocBarrel, "", "Search", questplan.DigBarrelAnchor, 6)
	if !ok {
		logf(d, "digsite: no barrel (loc %d) near %v", questplan.DigLocBarrel, questplan.DigBarrelAnchor)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.DigObjTrowel, m); err != nil {
		logf(d, "digsite: the trowel on the barrel: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	waitUntil(d, 8*time.Second, func() bool { return dl.Ready() })
	digDriveAll(d, nil, 15*time.Second, 2*time.Second)
	m, ok = questrunner.FindLoc(d, questplan.DigLocBarrel, "", "Search", questplan.DigBarrelAnchor, 6)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.DigObjVial, m); err != nil {
		logf(d, "digsite: the vial on the barrel: %v", err)
		return false
	}
	waitUntil(d, 10*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, questplan.DigObjLiquid) > 0 })
	digDriveAll(d, nil, 15*time.Second, 2*time.Second)
	return questrunner.HeldID(d, questplan.DigObjLiquid) > 0
}

// digMixProduct is what a pair of ingredients makes, whichever is used on which (opheldu is declared on one of the pair), and the Herblore
// level the content asks for the mix (0 for none).
var digMixProduct = []struct {
	a, b, product, herblore int
}{
	{questplan.DigObjCharcoal, questplan.DigObjPestle, questplan.DigObjGroundCharcoal, 0},
	{questplan.DigObjNitrate, questplan.DigObjNitro, questplan.DigObjPreMix, 0},
	{questplan.DigObjPreMix, questplan.DigObjGroundCharcoal, questplan.DigObjPostMix, questplan.DigHerblore},
	{questplan.DigObjPostMix, questplan.DigObjArceniaRoot, questplan.DigObjCompound, questplan.DigHerblore},
}

// digCustomMix mixes two ingredients (the last mix ends in a chat line and adds its product only after it, so the dialogue is driven).
func digCustomMix(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	var product, herblore int
	for _, m := range digMixProduct {
		if (m.a == step.ItemID && m.b == step.TargetID) || (m.b == step.ItemID && m.a == step.TargetID) {
			product, herblore = m.product, m.herblore
		}
	}
	if product == 0 {
		logf(d, "digsite: no recipe for %d and %d", step.ItemID, step.TargetID)
		return false
	}
	if questrunner.HeldID(d, product) > 0 {
		return true
	}
	if lvl := digLevel(d, "herblore"); herblore > 0 && lvl > 0 && lvl < herblore {
		e.RequestPark("the mix needs Herblore 10", questplan.DigSkillParkFor)
		return false
	}
	for _, order := range [][2]int{{step.ItemID, step.TargetID}, {step.TargetID, step.ItemID}} {
		if questrunner.HeldID(d, order[0]) == 0 || questrunner.HeldID(d, order[1]) == 0 {
			logf(d, "digsite: the pack lacks %s or %s for the mix", questrunner.ObjName(d, order[0]), questrunner.ObjName(d, order[1]))
			return false
		}
		if err := questrunner.UseItemOnItem(d, order[0], order[1]); err != nil {
			logf(d, "digsite: mixing: %v", err)
			continue
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 8*time.Second, func() bool { return dl.Ready() || questrunner.HeldID(d, product) > 0 })
		if dl.Ready() {
			digDriveAll(d, nil, 20*time.Second, 3*time.Second)
		}
		if waitUntil(d, 4*time.Second, func() bool { return questrunner.HeldID(d, product) > 0 }) {
			return true
		}
	}
	logf(d, "digsite: mixing %d with %d produced no %d", step.ItemID, step.TargetID, product)
	return false
}

// digOnBrickStand reports that the account stands on the one tile the blast accepts.
func digOnBrickStand(d *questrunner.Deps) bool {
	return questrunner.CurrentTile(d) == questplan.DigBrickStand
}

// digCustomBlast is the pour and the light: down the main winch to the east shaft, onto the brick stand (the blast's run sequence checks the
// coordinate and answers "Eep! Eep! Unexpected player coord!" anywhere else), the compound poured over the bricks (stage 7) and the
// tinderbox struck on them, which runs the account across the rock to the altar cave. Either half is skipped when it is done.
func digCustomBlast(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if digRegion(d) == questplan.DigAltarEast {
		return true
	}
	if digRegion(d) == questplan.DigSurface {
		if !digDescend(e, questplan.DigLocMainWinch, questplan.DigMainWinch, func() bool {
			return digRegion(d) == questplan.DigShaftEast || digRegion(d) == questplan.DigAltarEast
		}) {
			return false
		}
	}
	if digRegion(d) == questplan.DigAltarEast {
		return true
	}
	if digRegion(d) != questplan.DigShaftEast {
		logf(d, "digsite: the main winch left the account in the %s, not the east shaft", digRegion(d))
		return false
	}
	// Exactly onto the stand: a radius-0 walk that cannot get there reports the nearest tile it reached.
	for attempt := 0; attempt < 3 && !digOnBrickStand(d); attempt++ {
		questrunner.Travel(d, questplan.DigBrickStand, 0)
	}
	if !digOnBrickStand(d) {
		logf(d, "digsite: the blast wants %v and the account is at %v", questplan.DigBrickStand, questrunner.CurrentTile(d))
		return false
	}
	brick, ok := questrunner.FindLoc(d, questplan.DigLocBrick, "", "Search", questplan.DigBrickStand, 4)
	if !ok {
		logf(d, "digsite: no blocked brick (loc %d) near %v", questplan.DigLocBrick, questplan.DigBrickStand)
		return false
	}
	if questrunner.HeldID(d, questplan.DigObjCompound) > 0 {
		if err := questrunner.UseItemOnLoc(d, questplan.DigObjCompound, brick); err != nil {
			logf(d, "digsite: pouring the compound: %v", err)
			return false
		}
		dl := questrunner.NewDialogue(d)
		waitUntil(d, 12*time.Second, func() bool { return questrunner.HeldID(d, questplan.DigObjCompound) == 0 || dl.Ready() })
		digDriveAll(d, nil, 20*time.Second, 3*time.Second)
		if questrunner.HeldID(d, questplan.DigObjCompound) > 0 {
			logf(d, "digsite: the compound was not poured")
			return false
		}
	}
	if questrunner.HeldID(d, questplan.DigObjTinderbox) == 0 {
		logf(d, "digsite: the compound is poured and the pack has no tinderbox")
		return false
	}
	if !digOnBrickStand(d) {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.DigObjTinderbox, brick); err != nil {
		logf(d, "digsite: striking the tinderbox: %v", err)
		return false
	}
	// "Whoa! This is going to blow! I'd better run!" is a chat line, and the run (eight teleports across the rock) follows it.
	deadline := time.Now().Add(digBlastWait)
	for time.Now().Before(deadline) && digRegion(d) != questplan.DigAltarEast {
		digDriveAll(d, nil, 6*time.Second, 1500*time.Millisecond)
		if !d.Delay(500 * time.Millisecond) {
			break
		}
	}
	if digRegion(d) != questplan.DigAltarEast {
		logf(d, "digsite: the tinderbox did not set the compound off (at %v)", questrunner.CurrentTile(d))
		return false
	}
	digDriveAll(d, nil, 15*time.Second, 2*time.Second)
	return true
}

// digCustomTablet is the trip to the altar cave: down the main winch (which, past the blast, sets the account down in the altar cave), the
// tablet off the floor, up the rope. The cave holds ten skeletons the plan has priced; the account never fights, and goes up at once if its
// hitpoints are low.
func digCustomTablet(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.DigObjStoneTablet) > 0 {
		return true
	}
	if digRegion(d) == questplan.DigSurface {
		if !digDescend(e, questplan.DigLocMainWinch, questplan.DigMainWinch, func() bool { return digRegion(d) == questplan.DigAltarEast }) {
			return false
		}
	}
	if digRegion(d) != questplan.DigAltarEast {
		logf(d, "digsite: the account is in the %s, not the altar cave", digRegion(d))
		return false
	}
	got, hurt := false, false
	deadline := digBudgetEnd(e, digTabletWait)
	for time.Now().Before(deadline) && !got {
		if hp, maxHP := questrunner.Hitpoints(d); maxHP > 0 && hp*10 < maxHP*4 && questplan.FoodPoolHeal(questrunner.PackCounts(d), nil) == 0 {
			logf(d, "digsite: %d of %d hitpoints and no food in the altar cave: going up", hp, maxHP)
			hurt = true
			break
		}
		if len(questrunner.GroundItemsNear(d, questplan.DigObjStoneTablet, questplan.DigTabletSpawn, 6)) == 0 {
			questrunner.Travel(d, questplan.DigTabletSpawn, 3)
		}
		got = questrunner.TakeGround(d, questplan.DigObjStoneTablet, questplan.DigTabletSpawn, 8)
		if !got && !d.Delay(3*time.Second) {
			break
		}
	}
	digClimb(d)
	if !got && questrunner.HeldID(d, questplan.DigObjStoneTablet) == 0 && !hurt {
		e.RequestPark("the stone tablet's one spawn stayed empty for "+digTabletWait.String()+": another account holds it", digTrayPark)
	}
	return got || questrunner.HeldID(d, questplan.DigObjStoneTablet) > 0
}

// digLeave takes the account up whichever rope it stands beside: the pockets' way out for every other quest's walks.
func digLeave(d *questrunner.Deps) bool { return digClimb(d) }

// The four shafts and caves are pockets: the winches' ropes are their only way out, and the walk graph has none of them.
func init() {
	for _, p := range []struct {
		name string
		in   func(tile.Tile) bool
	}{
		{"the Digsite's east shaft", questplan.InDigShaftEast},
		{"the Digsite's west shaft", questplan.InDigShaftWest},
		{"the Digsite's altar cave", questplan.InDigAltarEast},
		{"the Digsite's altar cave west shaft", questplan.InDigAltarWest},
	} {
		questrunner.RegisterPocket(questrunner.Pocket{Name: p.name, In: p.in, Leave: digLeave})
	}
}
