package questmods

// The Tourist Trap through the real engine (Engine.Run) on a simulated session, in the manner of the other fillers' (hazeel_engine_test.go): the
// module's own Decide runs against snapshots built from a fake pack, a worn panel, the account's tile and the quest tab's colour, and every step it
// asks for is performed by a stub that changes that world the way the content would. The stub is STRICT: each executor checks the preconditions the
// content's scripts check (the gate's key and the clothes, the mine doors' slave clothes, the cave's, the cart's, the lift's) and fails the test when
// the plan sends the account into a crossing it could not make, so a run that reaches Complete is a run whose every crossing was legal. What is
// proven is the state machine under the engine's bookkeeping; what is not proven is each executor's own live execution (see the real-data tests).

import (
	"slices"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/quests"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

type trapRig struct {
	*csRig
	// jails is how many times the slave's lock puts the account in the cell before it opens.
	jails int
	// cellVisits counts the surface cell's escapes, mineJails the punishment mine's.
	cellVisits, mineJails int
	talks                 map[int]int
	// loseKeyOnJail drops the Metal key on the first jailing, as the guards' search does one time in five.
	loseKeyOnJail bool
	keyLost       bool
	// ana is where the content has Ana: "" nowhere yet, "cart" in the lower mine's barrels (put in the deep cart), "lift" on the winch bucket.
	ana string
	// winchFails is how many winch pulls come up empty although Ana is on the lift (the first failure the plan answers with the replay);
	// recaptureFails how many replays stop short; escapeFails how many rides out take Ana's barrel and fail; recaptures counts the replays run.
	winchFails, recaptureFails, escapeFails, recaptures int
	// banked is what the bank visits of the kit steps put in the bank: the provisioner deposits first everything that is neither kit nor kept.
	banked map[int]int
}

// wornSlots: the worn panel's slots for the pieces the rig knows.
var trapSlotOf = map[int]int{
	questplan.ObjTtShirt: 4, questplan.ObjTtSlaveShirt: 4, questplan.ObjTtRobe: 7, questplan.ObjTtSlaveRobe: 7, questplan.ObjTtBoots: 10, questplan.ObjTtSlaveBoots: 10,
	1333: 3, // a scimitar
	1127: 4, // rune platebody, which a shirt replaces
	// The rest of a full set of gear, for the strip's room: a helm, a cape, an amulet, a shield, legs (which a robe replaces), gloves, boots (which
	// boots replace) and a ring.
	1163: 0, 1052: 1, 1712: 2, 1201: 5, 1079: 7, 7462: 9, 4131: 10, 2550: 12,
}

func newTrapRig(t *testing.T, stage int, at tile.Tile) *trapRig {
	sim := actionsim.New(actionsim.Objs{Stackable: func(id int32) bool {
		switch id {
		case questplan.CoinsID, questplan.ObjTtPass, questplan.ObjTtFeather, questplan.ObjTtDartTip, questplan.ObjTtDart, questplan.ObjTtBronzeDart:
			return true
		}
		return false
	}}, nil)
	sim.StandAt(at.X, at.Z)
	r := &trapRig{csRig: &csRig{t: t, sim: sim}, talks: map[int]int{}}
	d := &questrunner.Deps{
		Session: sim, Components: actionsim.TabComponents(),
		Log:   func(s string) { r.log = append(r.log, s) },
		Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true },
	}
	sim.ShowTabs()
	for id, slot := range trapSlotOf {
		sim.SetWearPos(int32(id), slot)
	}
	prov := &questrunner.Provisioner{Bank: &questrunner.BankView{Counts: map[int]int{}, Known: true, At: time.Now().Add(time.Hour)}}
	r.e = &questrunner.Engine{D: d, Prov: prov, Store: questplan.NewStore()}
	r.stageKnown = true
	r.setStage(stage)
	for skill, lvl := range map[string]int{"attack": 60, "strength": 60, "defence": 55, "hitpoints": 70, "magic": 50, "smithing": 25, "fletching": 15, "thieving": 20, "agility": 30} {
		tfSeed(r.csRig, skill, lvl)
	}
	return r
}

func (r *trapRig) setStage(n int) {
	r.stage = n
	status := quests.InProgress
	switch {
	case n == questplan.TtNotStarted:
		status = quests.NotStarted
	case n >= questplan.TtComplete:
		status = quests.Complete
	}
	r.setTab(questplan.TouristTrapName, status)
}

func (r *trapRig) at(t tile.Tile) { r.sim.StandAtLevel(t.X, t.Z, t.Level) }

func (r *trapRig) here() tile.Tile {
	x, z, level, _ := r.sim.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func (r *trapRig) wornID(id int) bool {
	for _, s := range r.sim.Worn() {
		if int(s.ID) == id {
			return true
		}
	}
	return false
}

// wear puts id on, from the pack when it is there; the piece it replaces goes to the pack.
func (r *trapRig) wear(id int) {
	slot := trapSlotOf[id]
	for _, s := range r.sim.Worn() {
		if trapSlotOf[int(s.ID)] == slot && int(s.ID) != id {
			r.sim.Give(s.ID, 1)
		}
	}
	if r.held(id) > 0 {
		r.take(id, 1)
	}
	r.sim.Wear(slot, int32(id))
}

// strip takes off every worn piece the keep function refuses, into the pack. It is STRICT about room, as the game is: a piece with no free slot
// stays on, and the plan that asked for the strip is told so.
func (r *trapRig) strip(keep func(id int) bool) {
	pieces := 0
	for _, s := range r.sim.Worn() {
		if !keep(int(s.ID)) {
			pieces++
		}
	}
	if free := 28 - len(r.sim.Pack()); free < pieces {
		r.t.Errorf("the strip needs %d free slots and the pack has %d (the plan sent the account to a strip it had no room for)", pieces, free)
		return
	}
	for _, s := range r.sim.Worn() {
		if !keep(int(s.ID)) {
			r.sim.Wear(trapSlotOf[int(s.ID)], 0)
			r.sim.Give(s.ID, 1)
		}
	}
}

// clothesOnly reports no weapon or armour worn.
func (r *trapRig) clothesOnly() bool {
	for _, s := range r.sim.Worn() {
		if !questplan.TtClothesID(int(s.ID)) {
			return false
		}
	}
	return true
}

func (r *trapRig) slaveWorn() bool {
	return r.wornID(questplan.ObjTtSlaveShirt) && r.wornID(questplan.ObjTtSlaveRobe) && r.wornID(questplan.ObjTtSlaveBoots)
}

func (r *trapRig) anySlaveWorn() bool {
	return r.wornID(questplan.ObjTtSlaveShirt) || r.wornID(questplan.ObjTtSlaveRobe) || r.wornID(questplan.ObjTtSlaveBoots)
}

func (r *trapRig) need(cond bool, what string) bool {
	r.t.Helper()
	if !cond {
		r.t.Errorf("the plan sent the account into a crossing it could not make: %s (at %v, stage %d)", what, r.here(), r.stage)
	}
	return cond
}

func (r *trapRig) zone() questplan.TtZone { return questplan.TtAreaOf(r.here()).ZoneOf() }

func (r *trapRig) removeAll(id int) {
	if n := r.held(id); n > 0 {
		r.take(id, n)
	}
}

// apply plays the content's side of the quest.
func (r *trapRig) apply(step questplan.Step) bool {
	switch step.Kind {
	case questplan.StepProvision:
		r.provision(step)
		return true
	case questplan.StepTalk:
		return r.talk(step)
	case questplan.StepScanBank, questplan.StepDeposit:
		return true
	}
	if step.Kind != questplan.StepCustom {
		r.t.Errorf("no answer for the step %s", step.Describe())
		return false
	}
	z := r.zone()
	switch step.Custom {
	case questplan.CustomTtDress:
		r.dress(step.Qty)
	case questplan.CustomTtSouth:
		if !r.need(z == questplan.TtZoneNorth && r.held(questplan.ObjTtPass) > 0, "the pass needs the shop side and a pass") {
			return false
		}
		r.take(questplan.ObjTtPass, 1)
		if r.held(questplan.ObjTtDisclaimer) == 0 {
			r.give(questplan.ObjTtDisclaimer, 1)
		}
		r.at(questplan.TtPassSouth)
	case questplan.CustomTtNorth:
		if !r.need(z == questplan.TtZoneOpen, "the pass north is crossed from the open desert") {
			return false
		}
		r.at(questplan.TtPassNorth)
	case questplan.CustomTtCaptain:
		if !r.need(z == questplan.TtZoneOpen && r.stage >= questplan.TtStarted && (r.stage <= questplan.TtKilledCapt || r.held(questplan.ObjTtMetalKey) == 0), "the captain is taunted in the desert, and again only for a key") {
			return false
		}
		if r.held(questplan.ObjTtMetalKey) == 0 {
			r.give(questplan.ObjTtMetalKey, 1)
		}
		if r.stage < questplan.TtKilledCapt {
			r.setStage(questplan.TtKilledCapt)
		}
		r.at(questplan.TtCaptainAt)
	case questplan.CustomTtGateIn:
		if !r.need(z == questplan.TtZoneOpen && r.held(questplan.ObjTtMetalKey) > 0 && r.clothesOnly(), "the gate wants the key and nothing but clothes") {
			return false
		}
		if r.stage == questplan.TtKilledCapt {
			r.setStage(questplan.TtEnteredCmp)
		}
		r.at(questplan.TtGateIn)
	case questplan.CustomTtGateOut:
		if !r.need(z == questplan.TtZoneCamp && r.held(questplan.ObjTtMetalKey) > 0 && !r.anySlaveWorn(), "the gate wants the key and no slave clothes") {
			return false
		}
		r.at(questplan.TtGateOut)
	case questplan.CustomTtKeys:
		if !r.need(z == questplan.TtZoneCamp, "the desk is in the camp") {
			return false
		}
		r.give(questplan.ObjTtMetalKey, 1)
		r.at(tile.Tile{X: 3289, Z: 3033, Level: 1})
	case questplan.CustomTtSlave:
		return r.slave()
	case questplan.CustomTtMineIn:
		if !r.need(z == questplan.TtZoneCamp && r.slaveWorn() && r.clothesOnly(), "the mine doors want slave clothes and nothing else") {
			return false
		}
		if r.stage == questplan.TtTraded {
			r.setStage(questplan.TtEnteredMin)
		}
		r.at(tile.Tile{X: 3278, Z: 9426})
	case questplan.CustomTtMineOut:
		if !r.need(z == questplan.TtZoneHall && r.slaveWorn(), "the exit doors want slave clothes") {
			return false
		}
		r.at(questplan.TtMineDoor)
	case questplan.CustomTtPlans:
		if !r.need(z == questplan.TtZoneCamp && r.held(questplan.ObjTtBedobinKey) > 0, "the chest wants the camp and Al's key") {
			return false
		}
		if r.held(questplan.ObjTtPlans) == 0 {
			r.give(questplan.ObjTtPlans, 1)
		}
		if r.stage == questplan.TtBedobinKey {
			r.setStage(questplan.TtGotPlans)
		}
		r.at(tile.Tile{X: 3293, Z: 3033, Level: 1})
	case questplan.CustomTtShowPlans:
		if !r.need(z == questplan.TtZoneOpen && r.held(questplan.ObjTtPlans) > 0 && r.held(questplan.ObjBronzeBar) > 0 && r.held(questplan.ObjTtFeather) >= 10, "Al wants the plans, a bar and ten feathers") {
			return false
		}
		r.setStage(questplan.TtShownPlans)
		r.at(questplan.TtAlAt)
	case questplan.CustomTtTent:
		if !r.need(z == questplan.TtZoneOpen && r.held(questplan.ObjTtPlans) > 0 && r.held(questplan.ObjBronzeBar) > 0 && r.held(questplan.ObjHammer) > 0, "the anvil wants plans, a bar and a hammer") {
			return false
		}
		r.take(questplan.ObjBronzeBar, min(2, r.held(questplan.ObjBronzeBar))) // a miss spends a bar
		r.give(questplan.ObjTtDartTip, 1)
		if r.stage == questplan.TtShownPlans {
			r.setStage(questplan.TtMadeTip)
		}
		r.at(questplan.TtTentInside)
	case questplan.CustomTtFletch:
		if !r.need(r.held(questplan.ObjTtDartTip) > 0 && r.held(questplan.ObjTtFeather) >= 10, "the dart wants the tip and ten feathers") {
			return false
		}
		r.take(questplan.ObjTtDartTip, 1)
		r.take(questplan.ObjTtFeather, 20)
		r.give(questplan.ObjTtDart, 1)
		r.setStage(questplan.TtMadeDart)
	case questplan.CustomTtDart:
		if !r.need(r.held(questplan.ObjTtDart) > 0, "Al wants the dart") {
			return false
		}
		r.take(questplan.ObjTtDart, 1)
		r.removeAll(questplan.ObjTtPlans)
		r.removeAll(questplan.ObjTtBedobinKey)
		r.give(questplan.ObjTtBronzeDart, 6)
		r.give(questplan.ObjTtPineapple, 1)
		r.setStage(questplan.TtLearned)
		r.at(questplan.TtAlAt)
	case questplan.CustomTtPineapple:
		if !r.need(z == questplan.TtZoneHall && r.held(questplan.ObjTtPineapple) > 0, "the guard is in the hall") {
			return false
		}
		r.take(questplan.ObjTtPineapple, 1)
		r.setStage(questplan.TtGivenPine)
	case questplan.CustomTtCave:
		if step.Qty == 1 {
			if !r.need(z == questplan.TtZoneHall && r.slaveWorn() && r.stage >= questplan.TtGivenPine, "the cave lets a slave through after the pineapple") {
				return false
			}
			r.at(questplan.TtCaveLower)
		} else {
			if !r.need(z == questplan.TtZoneLower && r.slaveWorn() && r.held(questplan.ObjTtAnaBarrel) == 0, "the cave out wants slave clothes and no Ana") {
				return false
			}
			r.at(questplan.TtCaveHall)
		}
	case questplan.CustomTtBarrel:
		if !r.need(z == questplan.TtZoneLower, "the barrels are in the lower mine") {
			return false
		}
		if step.Qty == 1 {
			r.give(questplan.ObjTtBarrel, 1)
			return true
		}
		if r.ana != "cart" {
			return false // no Ana among them: she is not there, or she is on the lift
		}
		r.give(questplan.ObjTtAnaBarrel, 1)
		r.ana = ""
		r.setStage(questplan.TtRescue)
		r.at(questplan.TtLowerBarrel)
	case questplan.CustomTtCart:
		if step.Qty == 1 {
			if !r.need(z == questplan.TtZoneLower && r.held(questplan.ObjTtAnaBarrel) == 0, "the lower cart is ridden from the lower mine without Ana") {
				return false
			}
			if r.stage == questplan.TtGivenPine {
				r.setStage(questplan.TtUsedCart)
			}
			r.at(tile.Tile{X: 3319, Z: 9431})
		} else {
			if !r.need(z == questplan.TtZoneDeep && r.held(questplan.ObjTtAnaBarrel) == 0, "the deep cart is ridden from the deep mine without Ana") {
				return false
			}
			r.at(tile.Tile{X: 3302, Z: 9417})
		}
	case questplan.CustomTtCatch:
		if !r.need(z == questplan.TtZoneDeep && r.held(questplan.ObjTtBarrel) > 0, "Ana is caught in the deep mine with an empty barrel") {
			return false
		}
		r.take(questplan.ObjTtBarrel, 1)
		r.give(questplan.ObjTtAnaBarrel, 1)
		r.ana = ""
		r.setStage(questplan.TtRescue)
		r.at(questplan.TtAnaAt)
	case questplan.CustomTtDeepCheck:
		if !r.need(z == questplan.TtZoneDeep, "the deep cart is in the deep mine") {
			return false
		}
		r.removeAll(questplan.ObjTtAnaBarrel) // her barrel goes in the cart
		r.ana = "cart"
		r.at(tile.Tile{X: 3302, Z: 9417}) // the account rides it
	case questplan.CustomTtLift:
		if !r.need(z == questplan.TtZoneLower && r.held(questplan.ObjTtAnaBarrel) > 0, "the lift takes Ana's barrel in the lower mine") {
			return false
		}
		r.removeAll(questplan.ObjTtAnaBarrel)
		r.ana = "lift"
	case questplan.CustomTtWinch:
		if !r.need(z == questplan.TtZoneCamp, "the winch is in the camp") {
			return false
		}
		r.at(tile.Tile{X: 3280, Z: 3018})
		if r.winchFails > 0 {
			r.winchFails--
			return false // the barrel that comes up is full of stone, though Ana is on the lift
		}
		if r.ana != "lift" {
			return false // the barrel that comes up is full of stone
		}
		r.give(questplan.ObjTtAnaBarrel, 1)
		r.ana = ""
	case questplan.CustomTtRecapture:
		// The replay: back down with a new barrel, Ana caught again, the lift, the cave and the doors out, and the winch itself.
		if !r.need(z == questplan.TtZoneCamp && r.held(questplan.ObjTtAnaBarrel) == 0, "the replay starts in the camp's yard without Ana") {
			return false
		}
		r.recaptures++
		if r.recaptureFails > 0 {
			r.recaptureFails--
			return false
		}
		r.removeAll(questplan.ObjTtBarrel)
		r.give(questplan.ObjTtAnaBarrel, 1)
		r.ana = ""
		r.at(tile.Tile{X: 3280, Z: 3018})
		r.e.ForgetFailure(questplan.CustomTtWinch, questplan.CustomTtEscape) // what the real replay does when it succeeds
	case questplan.CustomTtEscape:
		if !r.need(z == questplan.TtZoneCamp && r.held(questplan.ObjTtAnaBarrel) > 0, "the cart takes Ana's barrel in the camp") {
			return false
		}
		if r.escapeFails > 0 {
			r.escapeFails--
			r.removeAll(questplan.ObjTtAnaBarrel) // the cart took the barrel and the ride did not happen
			return false
		}
		r.take(questplan.CoinsID, 100)
		r.at(questplan.TtCartLanding)
	case questplan.CustomTtJailCell:
		r.cellVisits++
		r.at(tile.Tile{X: 3270, Z: 3039})
	case questplan.CustomTtJailMine:
		r.mineJails++
		r.at(tile.Tile{X: 3293, Z: 9434})
	case questplan.CustomTtTentOut:
		r.at(questplan.TtTentOutside)
	default:
		r.t.Errorf("no answer for the step %q", step.Custom)
		return false
	}
	return true
}

// dress is the clothes modes of the content's crossings.
func (r *trapRig) dress(mode int) {
	desert := []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots}
	slave := []int{questplan.ObjTtSlaveShirt, questplan.ObjTtSlaveRobe, questplan.ObjTtSlaveBoots}
	clothes := func(id int) bool { return questplan.TtClothesID(id) }
	switch mode {
	case questplan.TtDressDesert:
		for _, id := range desert {
			if r.held(id) > 0 && !r.wornID(id) {
				r.wear(id)
			}
		}
	case questplan.TtDressCamp:
		r.strip(clothes)
		if !r.slaveWorn() {
			for _, id := range desert {
				if r.held(id) > 0 && !r.wornID(id) {
					r.wear(id)
				}
			}
		}
	case questplan.TtDressSlave:
		r.strip(func(id int) bool { return id == slave[0] || id == slave[1] || id == slave[2] })
		for _, id := range slave {
			if !r.wornID(id) {
				r.wear(id)
			}
		}
	case questplan.TtDressPlain:
		r.strip(func(id int) bool { return id == desert[0] || id == desert[1] || id == desert[2] })
		for _, id := range desert {
			if r.held(id) > 0 && !r.wornID(id) {
				r.wear(id)
			}
		}
	}
}

// provision fills a kit as the bank, the mule and Shantay's shelf would. A kit that has something to fetch is a bank visit, and a bank visit deposits
// first everything that is neither in the kit nor on the step's keep list (the weapon and armour a strip left in the pack, a rock, a kebab).
func (r *trapRig) provision(step questplan.Step) {
	counts := func() (pack, worn map[int]int) {
		pack, worn = map[int]int{}, map[int]int{}
		for _, s := range r.sim.Pack() {
			pack[int(s.ID)] += s.Count
		}
		for _, s := range r.sim.Worn() {
			worn[int(s.ID)]++
		}
		return pack, worn
	}
	// wanted reports a line the plan still wants: short of its need, and (OnlyIfShort) only while the line it serves is short.
	wanted := func(l questplan.KitLine) bool {
		pack, worn := counts()
		if l.OnlyIfShort != 0 {
			served := false
			for _, o := range step.Kit {
				served = served || (o.ID == l.OnlyIfShort && o.Needs(pack, worn) > 0)
			}
			if !served {
				return false
			}
		}
		return l.Needs(pack, worn) > 0
	}
	short := false
	for _, l := range step.Kit {
		short = short || wanted(l)
	}
	if short {
		for _, s := range r.sim.Pack() {
			id := int(s.ID)
			if slices.Contains(step.KeepIDs, id) {
				continue
			}
			if r.banked == nil {
				r.banked = map[int]int{}
			}
			r.banked[id] += s.Count
			r.removeAll(id)
		}
	}
	for _, l := range step.Kit {
		if !wanted(l) {
			continue
		}
		pack, worn := counts()
		need := l.Qty - l.Held(pack, worn)
		if free := 28 - len(r.sim.Pack()); !l.Stackable && need > free {
			need = free // the pack's room: a flexible line takes what fits
		}
		if need > 0 {
			r.sim.Give(int32(l.ID), need)
		}
	}
}

// slave is the lock and the trade: the first r.jails attempts end in the cell, then the lock opens and the clothes change hands.
func (r *trapRig) slave() bool {
	if !r.need(r.zone() == questplan.TtZoneCamp && r.clothesOnly(), "the slave is in the camp and the account is searched for weapons") {
		return false
	}
	if r.jails > 0 {
		r.jails--
		if r.loseKeyOnJail && !r.keyLost {
			r.keyLost = true
			r.removeAll(questplan.ObjTtMetalKey)
		}
		r.at(questplan.TtJailRocks[0])
		r.sim.StandAt(3285, 3034)
		return true
	}
	// A desert piece in the pack is converted first, a worn one in place.
	for i, d := range []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots} {
		sl := []int{questplan.ObjTtSlaveShirt, questplan.ObjTtSlaveRobe, questplan.ObjTtSlaveBoots}[i]
		switch {
		case r.held(d) > 0:
			r.take(d, 1)
			r.give(sl, 1)
		case r.wornID(d):
			r.sim.Wear(trapSlotOf[d], int32(sl))
		}
	}
	r.setStage(questplan.TtTraded)
	return true
}

// talk answers the talks the plan makes.
func (r *trapRig) talk(step questplan.Step) bool {
	r.talks[step.Stop.ID]++
	switch step.Stop.ID {
	case questplan.NpcTtIrena:
		switch {
		case r.stage == questplan.TtNotStarted:
			if !r.need(r.zone() == questplan.TtZoneOpen || true, "Irena") {
				return false
			}
			r.setStage(questplan.TtStarted)
			r.at(questplan.TtIrena)
		case r.held(questplan.ObjTtAnaBarrel) > 0 || r.stage == questplan.TtReward:
			if r.held(questplan.ObjTtAnaBarrel) > 0 {
				r.take(questplan.ObjTtAnaBarrel, 1)
			}
			r.setStage(questplan.TtComplete)
			r.at(questplan.TtIrena)
		default:
			r.t.Errorf("a talk with Irena at stage %d with no Ana", r.stage)
			return false
		}
	case questplan.NpcTtCaveGuard:
		if !r.need(r.zone() == questplan.TtZoneHall && r.slaveWorn(), "the cave guard talks to a slave in the hall") {
			return false
		}
		r.setStage(questplan.TtFindingPin)
	case questplan.NpcTtAlShabim:
		if !r.need(r.zone() == questplan.TtZoneOpen, "Al Shabim is in the open desert") {
			return false
		}
		switch r.stage {
		case questplan.TtFindingPin:
			r.give(questplan.ObjTtBedobinKey, 1)
			r.setStage(questplan.TtBedobinKey)
		default:
			if r.held(questplan.ObjTtBedobinKey) == 0 && step.UntilItemID == questplan.ObjTtBedobinKey {
				r.give(questplan.ObjTtBedobinKey, 1)
			} else if step.UntilItemID == questplan.ObjTtPineapple {
				r.give(questplan.ObjTtPineapple, 1)
			}
		}
		r.at(questplan.TtAlAt)
	default:
		r.t.Errorf("a talk with npc %d", step.Stop.ID)
		return false
	}
	return true
}

// startKit is the account at the shop side with enough coins and nothing else, gear on.
func (r *trapRig) startKit(coins int) {
	r.give(questplan.CoinsID, coins)
	r.sim.Wear(3, 1333)
	r.sim.Wear(4, 1127)
}

// The whole road, every executor a stub, in the content's order.
func TestTouristTrapReachesCompleteThroughTheEngine(t *testing.T) {
	r := newTrapRig(t, questplan.TtNotStarted, tile.Tile{X: 3269, Z: 3167})
	r.startKit(900)
	res := r.run(TouristTrap(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.tail(12))
	}
	got := r.described()
	t.Logf("%d steps: %s", len(got), strings.Join(got, " | "))
	// The order of the content's stages: every crossing in the order the plan intends.
	r.wantInOrder(
		"provision tt-trip", questplan.CustomTtDress, questplan.CustomTtSouth, "talk to Irena", questplan.CustomTtCaptain, questplan.CustomTtDress, questplan.CustomTtGateIn,
		questplan.CustomTtSlave, questplan.CustomTtDress, questplan.CustomTtMineIn, "talk to Mercenary", questplan.CustomTtMineOut, questplan.CustomTtDress,
		questplan.CustomTtGateOut, "talk to Al Shabim", questplan.CustomTtGateIn, questplan.CustomTtPlans, questplan.CustomTtGateOut, questplan.CustomTtShowPlans,
		questplan.CustomTtTent, questplan.CustomTtFletch, questplan.CustomTtDart, questplan.CustomTtGateIn, questplan.CustomTtDress, questplan.CustomTtMineIn,
		questplan.CustomTtPineapple, questplan.CustomTtCave, questplan.CustomTtBarrel, questplan.CustomTtCart, questplan.CustomTtCatch, questplan.CustomTtDeepCheck,
		questplan.CustomTtBarrel, questplan.CustomTtLift, questplan.CustomTtCave, questplan.CustomTtMineOut, questplan.CustomTtWinch, questplan.CustomTtEscape,
		"talk to Irena")
	if got := r.e.Store.Record(questplan.TouristTrapID).Completions; len(got) != 1 || !got[0].Earned() {
		t.Errorf("completions %+v, want one earned", got)
	}
	// The gear that came off at the gate is in the pack for ttFinish; the quest has put the camp's clothes where they belong.
	if r.held(1333) == 0 || r.held(1127) == 0 {
		t.Errorf("the scimitar (%d) and the platebody (%d) that came off at the camp are not in the pack", r.held(1333), r.held(1127))
	}
	if r.held(questplan.ObjTtAnaBarrel) != 0 {
		t.Error("Ana's barrel is still in the pack")
	}
}

// tail is the last n log lines.
func (r *trapRig) tail(n int) []string {
	if len(r.log) <= n {
		return r.log
	}
	return r.log[len(r.log)-n:]
}

// wantInOrder checks that the described steps contain want as a subsequence.
func (r *trapRig) wantInOrder(want ...string) {
	r.t.Helper()
	got := r.described()
	i := 0
	for _, g := range got {
		if i < len(want) && strings.HasPrefix(g, want[i]) {
			i++
		}
	}
	if i != len(want) {
		r.t.Errorf("the steps %v do not contain %v in order (matched %d of %d, next wanted %q)", got, want, i, len(want), want[min(i, len(want)-1)])
	}
}

// The slave's lock puts the account in the cell twice, and a search takes the Metal key once: the run still ends complete, the cell's way out and
// the desk's key being the plan's.
func TestTouristTrapSurvivesTheCellAndALostKey(t *testing.T) {
	r := newTrapRig(t, questplan.TtNotStarted, tile.Tile{X: 3269, Z: 3167})
	r.startKit(900)
	r.jails, r.loseKeyOnJail = 2, true
	res := r.run(TouristTrap(), r.apply)
	if res.Outcome != questrunner.OutcomeComplete {
		t.Fatalf("the run ended %s (%s) after %v: %v", res.Outcome, res.Reason, r.described(), r.tail(12))
	}
	if r.cellVisits != 2 {
		t.Errorf("%d cell escapes, want 2", r.cellVisits)
	}
	// Out of the cell the account is in the desert without the key (the first jailing took it): the captain's respawn gives another.
	n := 0
	for _, s := range r.steps {
		if s.Kind == questplan.StepCustom && s.Custom == questplan.CustomTtCaptain {
			n++
		}
	}
	if n < 2 {
		t.Errorf("the captain was asked for %d time(s), want the key replaced by a second duel", n)
	}
}

// Resume from every stage the journal can show, with what the stage's work has put in the pack: a healthy run is never parked.
func TestTouristTrapResumesFromEveryStage(t *testing.T) {
	slave := []int{questplan.ObjTtSlaveShirt, questplan.ObjTtSlaveRobe, questplan.ObjTtSlaveBoots}
	for _, c := range []struct {
		name  string
		stage int
		at    tile.Tile
		give  []int
		worn  []int
	}{
		{"started", questplan.TtStarted, tile.Tile{X: 3303, Z: 3113}, []int{questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4, 385, 385, 385, 385, 385, 385, questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots, questplan.ObjBronzeBar, questplan.ObjHammer, questplan.ObjTtFeather}, nil},
		{"killed the captain", questplan.TtKilledCapt, tile.Tile{X: 3272, Z: 3029}, []int{questplan.ObjTtMetalKey, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots, questplan.ObjBronzeBar, questplan.ObjHammer, questplan.ObjTtFeather}, []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots}},
		{"in the mine", questplan.TtEnteredMin, tile.Tile{X: 3278, Z: 9420}, []int{questplan.ObjTtMetalKey, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjBronzeBar, questplan.ObjHammer, questplan.ObjTtFeather}, slave},
		{"after the pineapple, in the hall", questplan.TtGivenPine, tile.Tile{X: 3278, Z: 9420}, []int{questplan.ObjTtMetalKey, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4}, slave},
		{"Ana caught", questplan.TtRescue, tile.Tile{X: 3320, Z: 9440}, []int{questplan.ObjTtMetalKey, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtAnaBarrel, questplan.CoinsID}, slave},
		{"Ana in the camp", questplan.TtRescue, tile.Tile{X: 3290, Z: 3025}, []int{questplan.ObjTtMetalKey, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtAnaBarrel}, []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots}},
		{"reward", questplan.TtReward, tile.Tile{X: 3303, Z: 3113}, []int{questplan.ObjTtSkin4, questplan.ObjTtSkin4, questplan.ObjTtSkin4}, nil},
	} {
		r := newTrapRig(t, c.stage, c.at)
		t.Logf("case %s", c.name)
		r.give(questplan.CoinsID, 700)
		for _, id := range c.give {
			if id == questplan.CoinsID {
				continue
			}
			r.give(id, 1)
		}
		for _, id := range c.worn {
			r.wear(id)
			if r.held(id) > 0 {
				r.take(id, 1)
			}
		}
		if c.stage == questplan.TtStarted {
			r.give(questplan.ObjTtFeather, 59)
			r.give(questplan.ObjBronzeBar, 3)
		}
		res := r.run(TouristTrap(), r.apply)
		if res.Outcome != questrunner.OutcomeComplete {
			t.Errorf("%s: the run ended %s (%s) after %v", c.name, res.Outcome, res.Reason, r.described())
		}
	}
}

// A captain the model says wins parks the quest before the account crosses, with the reason, for three hours; a weak account is never sent.
func TestTouristTrapParksAWeakAccountBeforeTheDesert(t *testing.T) {
	r := newTrapRig(t, questplan.TtNotStarted, tile.Tile{X: 3269, Z: 3167})
	r.startKit(900)
	for skill, lvl := range map[string]int{"attack": 8, "strength": 8, "defence": 8, "hitpoints": 30} {
		tfSeed(r.csRig, skill, lvl)
	}
	res := r.run(TouristTrap(), func(step questplan.Step) bool {
		switch {
		case step.Kind == questplan.StepProvision:
			r.provision(step)
		case step.Kind == questplan.StepCustom && step.Custom == questplan.CustomTtDress:
			r.dress(step.Qty)
		default:
			r.t.Errorf("the weak account was sent on: %s", step.Describe())
			return false
		}
		return true
	})
	if res.Outcome != questrunner.OutcomeParked || !strings.Contains(res.Reason, "Mercenary Captain") {
		t.Fatalf("the run ended %s (%s) after %v", res.Outcome, res.Reason, r.described())
	}
	if parked, _ := r.e.Store.Parked(questplan.TouristTrapID, time.Now().Add(2*time.Hour)); !parked {
		t.Error("not parked for hours")
	}
	if parked, _ := r.e.Store.Parked(questplan.TouristTrapID, time.Now().Add(4*time.Hour)); parked {
		t.Error("parked past the captain's three hours")
	}
	if r.here().Z < 3117 {
		t.Errorf("the account crossed the pass: at %v", r.here())
	}
}
