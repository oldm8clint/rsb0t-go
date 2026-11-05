package questmods

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// TouristTrap is the Tourist Trap ready for the engine. The bank, the mule and Shantay's shelf (the kit), the talks (Irena, the cave guard, Al
// Shabim) and the shop's waterskins are the engine's generic steps; the customs are the legs the content makes special: the Shantay pass, the
// clothes each crossing wants, the captain's taunt and fight, the camp's gate, the slave's lock, the mine doors, the guarded cave, Siad's bookcase
// and chest, the anvil, the carts, the lift, the winch and the cart out, and the two cells. questplan/touristtrap.go and touristtrapdecide.go
// say what each is for and why.
func TouristTrap() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.TouristTrap,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomTtSouth:     ttSouthStep,
			questplan.CustomTtNorth:     ttNorthStep,
			questplan.CustomTtDress:     ttDressStep,
			questplan.CustomTtCaptain:   ttCaptainStep,
			questplan.CustomTtGateIn:    ttGateInStep,
			questplan.CustomTtGateOut:   ttGateOutStep,
			questplan.CustomTtKeys:      ttKeysStep,
			questplan.CustomTtSlave:     ttSlaveStep,
			questplan.CustomTtMineIn:    ttMineInStep,
			questplan.CustomTtMineOut:   ttMineOutStep,
			questplan.CustomTtPlans:     ttPlansStep,
			questplan.CustomTtShowPlans: ttShowPlansStep,
			questplan.CustomTtTent:      ttTentStep,
			questplan.CustomTtFletch:    ttFletchStep,
			questplan.CustomTtDart:      ttDartStep,
			questplan.CustomTtPineapple: ttPineappleStep,
			questplan.CustomTtCave:      ttCaveStep,
			questplan.CustomTtBarrel:    ttBarrelStep,
			questplan.CustomTtCart:      ttCartStep,
			questplan.CustomTtCatch:     ttCatchStep,
			questplan.CustomTtDeepCheck: ttDeepStep,
			questplan.CustomTtLift:      ttLiftStep,
			questplan.CustomTtWinch:     ttWinchStep,
			questplan.CustomTtEscape:    ttEscapeStep,
			questplan.CustomTtRecapture: ttRecaptureStep,
			questplan.CustomTtJailCell:  ttJailCellStep,
			questplan.CustomTtJailMine:  ttJailMineStep,
			questplan.CustomTtTentOut:   ttTentOutStep,
		},
		// The gear that came off at the camp's gate goes back on when the quest ends.
		Finish: ttFinish,
	}
}

// The camp, the mines, their cells and the tent are one pocket: the gate, the doors, the cave, the carts and the tent's guard are crossings the walk
// graph holds only behind items and dialogue, so a walk that starts inside and ends outside leaves by the module's own way first.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Tourist Trap's camp and mines", In: ttInPocket, Leave: ttLeaveAll})
}

func ttInPocket(t tile.Tile) bool {
	a := questplan.TtAreaOf(t)
	switch a.ZoneOf() {
	case questplan.TtZoneCamp, questplan.TtZoneHall, questplan.TtZoneLower, questplan.TtZoneDeep:
		return true
	}
	return a == questplan.TtAreaSurfaceCell || a == questplan.TtAreaMineCell || a == questplan.TtAreaTent
}

const (
	// ttArriveWait is how long a gate, a door, a cave or a cart gets to move the account to the other side.
	ttArriveWait = 15 * time.Second
	// ttBoxWait is how long a loc op gets to put its message box on the screen.
	ttBoxWait = 8 * time.Second
	// ttTalkBudget bounds one conversation (the slave's whole lock-picking one is the longest).
	ttTalkBudget = 4 * time.Minute
	// ttFightBudget bounds the captain's fight: 80 hitpoints at the slowest swing the model accepts is under five minutes.
	ttFightBudget = 7 * time.Minute
)

// ttJails counts the cells an account has been put in during this process, by Deps: the slave's lock (a Thieving roll a try, a guard within ten tiles
// of the slave from the third failure on) is what puts accounts there, and a quest that keeps losing to it is parked rather than left to repeat the
// cell until the slot ends (questplan.TtJailsToPark).
var ttJails sync.Map // *questrunner.Deps -> *int

// ttNoteJail records one cell for the account and parks the quest when it has had too many.
func ttNoteJail(e *questrunner.Engine, why string) {
	d := e.Deps()
	v, _ := ttJails.LoadOrStore(d, new(int))
	n := v.(*int)
	*n++
	logf(d, "tourist trap: put in a cell (%s): %d of %d", why, *n, questplan.TtJailsToPark)
	if *n >= questplan.TtJailsToPark {
		*n = 0
		e.RequestPark(fmt.Sprintf("put in a cell %d times in one run (the slave's lock keeps failing)", questplan.TtJailsToPark), questplan.TtJailPark)
	}
}

// ---------------------------------------------------------------------------------------------------------
// Small helpers.
// ---------------------------------------------------------------------------------------------------------

func ttHere(d *questrunner.Deps) tile.Tile { return questrunner.CurrentTile(d) }

func ttAreaNow(d *questrunner.Deps) questplan.TtArea { return questplan.TtAreaOf(ttHere(d)) }

func ttSettle(d *questrunner.Deps) {
	for i := 0; i < 3; i++ {
		if !questrunner.ClearLeftoverModal(d) {
			return
		}
	}
}

func ttWorn(d *questrunner.Deps, id int) bool { return questrunner.WornCounts(d)[id] > 0 }

// ttFind finds the first loc of ids offering op near a tile.
func ttFind(d *questrunner.Deps, ids []int, op string, near tile.Tile, radius int) (locindex.Match, bool) {
	for _, id := range ids {
		if m, ok := questrunner.FindLoc(d, id, "", op, near, radius); ok {
			return m, true
		}
	}
	return locindex.Match{}, false
}

// ttOperate finds a loc and operates it from an accepted stand.
func ttOperate(d *questrunner.Deps, what string, ids []int, op string, near tile.Tile, radius int) bool {
	m, ok := ttFind(d, ids, op, near, radius)
	if !ok {
		logf(d, "tourist trap: no %s (loc %v) offering %q near %v", what, ids, op, near)
		return false
	}
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "tourist trap: %s: %v", what, err)
		return false
	}
	return true
}

// ttTravel walks to a tile inside the account's piece of ground, and reports whether it stands within radius of it afterwards.
func ttTravel(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if questrunner.Travel(d, dest, radius) {
		return true
	}
	at := ttHere(d)
	return at.Level == dest.Level && tile.Chebyshev(at, dest) <= radius
}

// ttAvoidRowdy is the rectangles round the rowdy guards' posts on the account's level of the mines or the camp, for the walks that can go round
// them (questplan.TtRowdyPosts). The posts within 6 tiles of dest are left out: a walk that ends beside one cannot keep out of it.
func ttAvoidRowdy(dest tile.Tile) []webwalk.Rect {
	var out []webwalk.Rect
	for _, p := range questplan.TtRowdyPosts {
		if tile.Chebyshev(p, dest) <= 6 {
			continue
		}
		r := questplan.TtRowdyReach
		out = append(out, webwalk.Rect{MinX: p.X - r, MaxX: p.X + r, MinZ: p.Z - r, MaxZ: p.Z + r, Level: p.Level})
	}
	return out
}

// ttTravelRound is ttTravel with the rowdy guards' squares kept out of, and the plain walk when no way round exists.
func ttTravelRound(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if avoid := ttAvoidRowdy(dest); len(avoid) > 0 && d.Walker != nil {
		if questrunner.TravelAvoiding(d, dest, radius, avoid) {
			return true
		}
	}
	return ttTravel(d, dest, radius)
}

// ttEnsureSurface climbs the camp's ladder down from Siad's room.
func ttEnsureSurface(d *questrunner.Deps) bool {
	if ttAreaNow(d) != questplan.TtAreaCampUpper {
		return true
	}
	down := questplan.TtLadder
	down.Level = 1
	if !ttOperate(d, "the ladder down", []int{questplan.LocTtLadderDown}, "Climb-down", down, 4) {
		return false
	}
	return waitUntil(d, ttArriveWait, func() bool { return ttAreaNow(d) != questplan.TtAreaCampUpper })
}

// ttMakeRoom drops what the quest can do without until the pack has n free slots, junk first (an empty skin, the reward darts, a kebab), then the
// spare papers (a hammer or bars once the tip is made): questplan.TtJunkIDs, the list the plan counts its strip room by. It never drops the Metal
// key, a barrel, the plans, the outfits, the food or the coins.
func ttMakeRoom(d *questrunner.Deps, n int) bool {
	junk := questplan.TtJunkIDs(questrunner.HeldID(d, questplan.ObjTtDartTip)+questrunner.HeldID(d, questplan.ObjTtDart) > 0)
	for _, id := range junk {
		for questrunner.FreeSlots(d) < n && questrunner.HeldID(d, id) > 0 {
			before := questrunner.FreeSlots(d)
			if err := questrunner.OpHeldByName(d, id, "Drop"); err != nil {
				logf(d, "tourist trap: dropping %s: %v", questrunner.ObjName(d, id), err)
				break
			}
			if !waitUntil(d, 3*time.Second, func() bool { return questrunner.FreeSlots(d) > before }) {
				break
			}
		}
		if questrunner.FreeSlots(d) >= n {
			return true
		}
	}
	return questrunner.FreeSlots(d) >= n
}

// ttSaw reports a game message (lower case needle) since mark.
func ttSaw(d *questrunner.Deps, mark time.Time, needles ...string) bool {
	return cookSaw(d, mark, needles...)
}

// The falls. A rock the account fails to climb out of the surface cell deals 7 (desertrescue's damage_self(7): a real hit, which can kill), a mine
// cart it fails to squeeze into 2, and the cell is a sealed piece of ground whose floor keeps the stripped weapon and armour a death would drop. Each
// attempt waits until the hitpoints are above the fall and a margin.
const (
	ttFallDamage = 7
	ttCartDamage = 2
	ttHPMargin   = 3
)

// ttRestLimit bounds how long an account rests for hitpoints before an attempt (hitpoints come back at about one a minute; a variable so a test can
// shorten it).
var ttRestLimit = 8 * time.Minute

// ttRestAbove waits, eating from the pack when it has food and resting when it has none, until the account's hitpoints are above floor, for at most
// limit. It reports whether they are (true too when the hitpoints have not been read: nothing to judge).
func ttRestAbove(d *questrunner.Deps, floor int, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	logged := false
	for {
		hp, max := questrunner.Hitpoints(d)
		if max == 0 || hp > floor {
			return true
		}
		if !logged {
			logf(d, "tourist trap: %d of %d hitpoints is not above %d: resting before the attempt", hp, max, floor)
			logged = true
		}
		questrunner.EatIfNeeded(d, questplan.TouristTrap.Sustain.FoodIDs, floor)
		if !time.Now().Before(deadline) {
			return false
		}
		if !d.Delay(5 * time.Second) {
			return false
		}
	}
}

// ttWeakPark asks the engine to park the quest when a step that risks a fall failed with the hitpoints at or under floor: the account is better
// elsewhere than resting in a cell for the rest of its slot, and the next run starts with its hitpoints back.
func ttWeakPark(e *questrunner.Engine, floor int, what string) {
	hp, max := questrunner.Hitpoints(e.Deps())
	if max == 0 || hp > floor {
		return
	}
	e.RequestPark(fmt.Sprintf("%d of %d hitpoints are not enough to risk %s (a fall deals up to %d)", hp, max, what, floor-ttHPMargin), questplan.TtItemPark)
}

// ---------------------------------------------------------------------------------------------------------
// Clothes and gear.
// ---------------------------------------------------------------------------------------------------------

const ttGearHint = "tt-gear:"

var (
	ttDesertIDs = []int{questplan.ObjTtShirt, questplan.ObjTtRobe, questplan.ObjTtBoots}
	ttSlaveIDs  = []int{questplan.ObjTtSlaveShirt, questplan.ObjTtSlaveRobe, questplan.ObjTtSlaveBoots}
)

func ttAllWorn(worn map[int]int, ids []int) bool {
	for _, id := range ids {
		if worn[id] == 0 {
			return false
		}
	}
	return true
}

func ttAnyWorn(worn map[int]int, ids []int) bool {
	for _, id := range ids {
		if worn[id] > 0 {
			return true
		}
	}
	return false
}

// ttDressApply puts on the clothes a crossing wants and takes off what it refuses (questplan.TtDress* modes), remembering the gear it took off so
// that ttFinish can put it back. The camp's guards search a weapon or armour, the gate refuses the slave's clothes going out, the doors let nobody
// else in.
func ttDressApply(d *questrunner.Deps, e *questrunner.Engine, mode int) bool {
	pack, worn := questrunner.PackCounts(d), questrunner.WornCounts(d)
	var wear []int
	permit := map[int]bool{}
	strip := mode != questplan.TtDressDesert
	switch mode {
	case questplan.TtDressDesert:
		for _, id := range ttDesertIDs {
			if pack[id] > 0 && worn[id] == 0 {
				wear = append(wear, id)
			}
		}
	case questplan.TtDressCamp:
		for _, id := range append(append([]int(nil), ttDesertIDs...), ttSlaveIDs...) {
			permit[id] = true
		}
		if !ttAllWorn(worn, ttSlaveIDs) {
			for _, id := range ttDesertIDs {
				if pack[id] > 0 && worn[id] == 0 {
					wear = append(wear, id)
				}
			}
		}
	case questplan.TtDressSlave:
		for _, id := range ttSlaveIDs {
			permit[id] = true
			if pack[id] > 0 && worn[id] == 0 {
				wear = append(wear, id)
			}
		}
	case questplan.TtDressPlain:
		for _, id := range ttDesertIDs {
			permit[id] = true
			if pack[id] > 0 && worn[id] == 0 {
				wear = append(wear, id)
			}
		}
	default:
		logf(d, "tourist trap: no clothes mode %d", mode)
		return false
	}
	var off []int32
	if strip {
		var ids []int
		for id, n := range worn {
			if n > 0 && !permit[id] {
				ids = append(ids, id)
			}
		}
		sort.Ints(ids)
		for _, id := range ids {
			off = append(off, int32(id))
		}
	}
	if len(off) > 0 {
		if free := questrunner.FreeSlots(d); free < len(off) {
			logf(d, "tourist trap: %d free slots for %d pieces to take off", free, len(off))
			if !ttMakeRoom(d, len(off)) {
				logf(d, "tourist trap: the clothes mode %d needs %d free pack slots for the weapons and armour that come off and the pack has %d even with its junk dropped",
					mode, len(off), questrunner.FreeSlots(d))
				return false
			}
		}
		ttRememberGear(e, off)
		eq := actions.NewEquipment(d.Session, d.Components)
		r := eq.UnequipMany(off, actions.DelayWaiter(d.Delay))
		logf(d, "tourist trap: %s", r.Log("took off"))
	}
	if len(wear) > 0 {
		questrunner.EquipMany(d, wear)
	}
	pack, worn = questrunner.PackCounts(d), questrunner.WornCounts(d)
	switch mode {
	case questplan.TtDressDesert:
		return ttAllWorn(worn, ttDesertIDs) || !ttHasSet(pack, worn, ttDesertIDs)
	case questplan.TtDressSlave:
		return ttAllWorn(worn, ttSlaveIDs) && ttClothesOnly(worn)
	case questplan.TtDressPlain:
		return !ttAnyWorn(worn, ttSlaveIDs) && ttClothesOnly(worn)
	}
	return ttClothesOnly(worn)
}

func ttHasSet(pack, worn map[int]int, ids []int) bool {
	for _, id := range ids {
		if pack[id]+worn[id] == 0 {
			return false
		}
	}
	return true
}

func ttClothesOnly(worn map[int]int) bool {
	for id, n := range worn {
		if n > 0 && !questplan.TtClothesID(id) {
			return false
		}
	}
	return true
}

// ttRememberGear writes the ids taken off as hints in the quest's store.
func ttRememberGear(e *questrunner.Engine, ids []int32) {
	if e == nil || e.Store == nil || len(ids) == 0 {
		return
	}
	rec := e.Store.Record(questplan.TouristTrapID)
	if rec.Hints == nil {
		rec.Hints = map[string]bool{}
	}
	for _, id := range ids {
		rec.Hints[fmt.Sprintf("%s%d", ttGearHint, id)] = true
	}
	if e.Save != nil {
		e.Save()
	}
}

// ttFinish puts the gear that came off back on, once the quest is complete, for whatever of it is still in the pack.
func ttFinish(e *questrunner.Engine) {
	if e.Store == nil {
		return
	}
	d := e.Deps()
	rec := e.Store.Record(questplan.TouristTrapID)
	var ids []int
	for k, on := range rec.Hints {
		if !on || !strings.HasPrefix(k, ttGearHint) {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(strings.TrimPrefix(k, ttGearHint), "%d", &id); err != nil || id <= 0 {
			delete(rec.Hints, k)
			continue
		}
		delete(rec.Hints, k)
		if questrunner.HeldID(d, id) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	if len(ids) > 0 {
		logf(d, "tourist trap: wearing the gear that came off for the camp again (%d pieces)", len(ids))
		questrunner.EquipMany(d, ids)
	}
	if e.Save != nil {
		e.Save()
	}
}

func ttDressStep(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	ok := ttDressApply(d, e, step.Qty)
	if !ok {
		logf(d, "tourist trap: the clothes for mode %d did not all go on", step.Qty)
	}
	return ok
}

// ---------------------------------------------------------------------------------------------------------
// The Shantay pass.
// ---------------------------------------------------------------------------------------------------------

// ttSouthStep goes through the pass with a Shantay pass in the pack. The first crossing has the poster's three boxes and a menu
// ("Yeah, that poster doesn't scare me!") and the guard's disclaimer; the queue that carries the account south runs when the chat is over.
func ttSouthStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if ttAreaNow(d).South() {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjTtPass) == 0 {
		logf(d, "tourist trap: no Shantay pass to hand over")
		return false
	}
	if !ttTravel(d, questplan.TtPassNorth, 0) {
		logf(d, "tourist trap: the pass's north side %v was not reached from %v", questplan.TtPassNorth, ttHere(d))
		return false
	}
	if !ttOperate(d, "the Shantay pass", []int{questplan.LocTtPass}, "Go-through", tile.Tile{X: 3302, Z: 3116}, 6) {
		return false
	}
	south := func() bool { return ttAreaNow(d).South() }
	driveScript(d, []string{"Yeah, that poster doesn't scare me!"}, south, 60*time.Second)
	ttSettle(d)
	if !waitUntil(d, ttArriveWait, south) {
		logf(d, "tourist trap: the pass did not take the account south (at %v)", ttHere(d))
		return false
	}
	d.Delay(1200 * time.Millisecond)
	ttSettle(d)
	return true
}

// ttNorthStep goes back through the pass, which is free.
func ttNorthStep(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !ttAreaNow(d).South() {
		return true
	}
	if ttAreaNow(d).ZoneOf() != questplan.TtZoneOpen {
		logf(d, "tourist trap: the pass is reached from the open desert, not from %v", ttHere(d))
		return false
	}
	if !ttTravel(d, questplan.TtPassSouth, 1) {
		logf(d, "tourist trap: the pass's south side was not reached from %v", ttHere(d))
		return false
	}
	if !ttOperate(d, "the Shantay pass", []int{questplan.LocTtPass}, "Go-through", tile.Tile{X: 3302, Z: 3116}, 6) {
		return false
	}
	north := func() bool { return !ttAreaNow(d).South() }
	if !waitUntil(d, ttArriveWait, north) {
		driveScript(d, nil, north, 20*time.Second)
	}
	ttSettle(d)
	return north()
}
