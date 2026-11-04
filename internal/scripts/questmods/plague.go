package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlagueCity is Plague City ready for the engine. The decision (questplan/plaguecity.go) names a custom step for every leg because
// nearly every conversation of the quest holds a message box, which sits between the pages of a chat and ends a plain talk step at
// the first one; plagueDrive carries a script's pages, menus and boxes to its end, and each executor below is one leg of the
// quest on top of it: the Edmond and Alrena talks, the water to the soil, the dig, the rope and the pipe, the way home out of the
// pocket the sewer and the city make, the Rehnisons' door, the mourner, the clerk, Bravek, the plague house, and the ingredients of
// the hangover cure (the cow's milk, the hobgoblins' snape grass, the mixing).
func PlagueCity() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.PlagueCity,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomPlagueStart:    plagueTalk(nil),
			questplan.CustomPlagueAlrena:   plagueTalk(func(d *questrunner.Deps) bool { return held(d, questplan.PlagueObjGasMask) }),
			questplan.CustomPlagueEdmond:   plagueTalk(nil),
			questplan.CustomPlagueGrill:    plagueTalk(nil),
			questplan.CustomPlagueJethick:  plagueTalk(nil),
			questplan.CustomPlagueFinish:   plagueTalk(nil),
			questplan.CustomPlagueBerries:  plagueBerries,
			questplan.CustomPlagueCupboard: plagueCupboard,
			questplan.CustomPlagueWater:    plagueWater,
			questplan.CustomPlagueDig:      plagueDig,
			questplan.CustomPlagueRope:     plagueRope,
			questplan.CustomPlagueCross:    plagueCross,
			questplan.CustomPlagueHome:     plagueHome,
			questplan.CustomPlagueBook:     plagueBook,
			questplan.CustomPlagueParents:  plagueParents,
			questplan.CustomPlagueMilli:    plagueMilli,
			questplan.CustomPlagueDown:     plagueDown,
			questplan.CustomPlagueMourner:  plagueMourner,
			questplan.CustomPlagueAudience: plagueAudience,
			questplan.CustomPlagueCure:     plagueCure,
			questplan.CustomPlagueHouse:    plagueHouse,
			questplan.CustomPlagueMilk:     plagueMilk,
			questplan.CustomPlagueGrass:    plagueGrass,
			questplan.CustomPlagueBrew:     plagueBrew,
			questplan.CustomPlagueScroll:   plagueScroll,
		},
	}
}

func init() {
	// West Ardougne, the Rehnisons' upper floor, the plague house's cellar and the sewer under the garden are one pocket: no walk
	// leaves them (the city's gates stay shut until Biohazard, the garden's mud pile lands on a tile the baked graph calls blocked, and
	// the plague house's door is the script's), so a quest that finds an account there walks it out by the quest's own way first.
	questrunner.RegisterPocket(questrunner.Pocket{Name: "West Ardougne and the Ardougne sewer", In: questplan.InPlaguePocket, Leave: plagueGoHome})
}

// The times a leg waits.
const (
	// plagueQuiet is how long the chat and the main modal must both stay shut for a script to count as over: the longest pause in
	// the quest's scripts is Alrena's two p_delay(2) between her messages (1.2 seconds each).
	plagueQuiet = 3 * time.Second
	// plagueTalkBudget bounds one conversation (the longest is the clerk's, fourteen pages and two menus).
	plagueTalkBudget = 120 * time.Second
	// plagueArrive is how long a stairs, a manhole, a pipe or a hole is given to put the account where it goes (the pipe's
	// exact-move is five ticks and the dig's two message boxes wait for a click).
	plagueArrive = 25 * time.Second
	// plagueEarshot is how far a mourner (or Ted, or Bravek) must be from the account for the script that asks for one to do
	// anything: 14 tiles for the plague house door, 7 for the Rehnisons' door and for the clerk's call.
	plagueMournerEarshot = 14
	plagueHostEarshot    = 7
	// plagueNpcWait is how long a leg waits for such an npc to wander into earshot.
	plagueNpcWait = 45 * time.Second
	// plagueMilkBudget bounds the milking and plagueGrassWait the wait for a respawned snape grass at the trail's edge.
	plagueMilkBudget = 4 * time.Minute
	plagueGrassWait  = 3 * time.Minute
	plagueBerryWait  = 4 * time.Minute
)

// plagueSettle closes whatever message box a script left open, so that the next interaction is not dropped.
func plagueSettle(d *questrunner.Deps) {
	for i := 0; i < 3; i++ {
		if !questrunner.ClearLeftoverModal(d) {
			return
		}
	}
}

// plagueDrive carries a script's messages to their end: pages are continued and menus answered from prefer by questrunner's
// Dialogue (which also presses the continue on a message box, the chat interface a ~mesbox opens), and a box the dialogue does not
// see (a main modal) is closed. With done it runs until done holds or the budget ends and reports done; without it, it runs until
// the chat and the modals have stayed shut for plagueQuiet and reports whether the script raised anything at all (nothing is what
// a door that is gated by the script, with nobody in earshot, answers).
func plagueDrive(d *questrunner.Deps, prefer []string, done func() bool, budget time.Duration) bool {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	var quiet time.Time
	spoke := false
	for time.Now().Before(deadline) {
		if done != nil && done() {
			plagueSettle(d)
			return true
		}
		if dl.Ready() {
			spoke = true
			quiet = time.Time{}
			if !dl.Drive(prefer) {
				break // no option to choose, or the page budget ran out
			}
			continue
		}
		if main, _, _ := d.Session.State().Modals(); main != gamestate.NoModal {
			spoke = true
			quiet = time.Time{}
			questrunner.ClearLeftoverModal(d)
			continue
		}
		if done == nil {
			if quiet.IsZero() {
				quiet = time.Now()
			}
			if time.Since(quiet) >= plagueQuiet {
				break
			}
		}
		if !d.Delay(300 * time.Millisecond) {
			return false
		}
	}
	plagueSettle(d)
	if done != nil {
		return done()
	}
	return spoke
}

// plagueConverse walks to the stop, opens its conversation and drives it (plagueDrive).
func plagueConverse(d *questrunner.Deps, stop questplan.NpcStop, done func() bool, budget time.Duration) bool {
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "plague: could not get to %s", stop.Npc)
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		logf(d, "plague: %s opened no conversation", stop.Npc)
		return false
	}
	return plagueDrive(d, stop.Prefer, done, budget)
}

// plagueTalk is a talk leg: the stop is the step's, and done (nil for none) says when its goal is reached, else the conversation's
// end is.
func plagueTalk(done func(d *questrunner.Deps) bool) questrunner.CustomFunc {
	return func(e *questrunner.Engine, step questplan.Step) bool {
		d := e.Deps()
		defer e.InvalidateStage()
		var f func() bool
		if done != nil {
			f = func() bool { return done(d) }
		}
		ok := plagueConverse(d, step.Stop, f, plagueTalkBudget)
		logf(d, "plague: talked to %s: %v", step.Stop.Npc, ok)
		return ok
	}
}

func plagueAreaNow(d *questrunner.Deps) questplan.PlagueArea {
	return questplan.PlagueAreaOf(questrunner.CurrentTile(d))
}

// plagueArrived waits for the account to stand in want.
func plagueArrived(d *questrunner.Deps, want questplan.PlagueArea, budget time.Duration) bool {
	return waitUntil(d, budget, func() bool { return plagueAreaNow(d) == want })
}

// plagueNpcNear reports an npc of this id within r tiles of the account.
func plagueNpcNear(d *questrunner.Deps, id, r int) bool {
	_, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(r).Where(func(n entityquery.Entity) bool { return n.ID == id }).Nearest()
	return ok
}

// plagueLoc finds a loc by id (or name) near a tile, with the op it must offer, and says so in the log when it is not there.
func plagueLoc(d *questrunner.Deps, id int, name, op string, near tile.Tile, radius int) (locindex.Match, bool) {
	m, ok := questrunner.FindLoc(d, id, name, op, near, radius)
	if !ok {
		logf(d, "plague: no %s%s (loc %d) offering %q near %d,%d,%d", name, "", id, op, near.X, near.Z, near.Level)
	}
	return m, ok
}

// plagueOperate finds a loc and sends op from a tile the engine accepts it from.
func plagueOperate(d *questrunner.Deps, id int, name, op string, near tile.Tile, radius int) bool {
	m, ok := plagueLoc(d, id, name, op, near, radius)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "plague: %s %s: %v", op, name, err)
		return false
	}
	return true
}

// plagueStandOn walks onto exactly this tile (a door whose script reads which side of the wall the account is on).
func plagueStandOn(d *questrunner.Deps, at tile.Tile) bool {
	for try := 0; try < 3; try++ {
		if questrunner.CurrentTile(d) == at {
			return true
		}
		questrunner.Travel(d, at, 0)
	}
	return questrunner.CurrentTile(d) == at
}

// ---------------------------------------------------------------------------------------------------------------
// The way home, and the pocket.
// ---------------------------------------------------------------------------------------------------------------

// plagueHome is the leg that brings an account out of the sewer, the city or the cellar to Edmond's garden.
func plagueHome(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return plagueGoHome(d)
}

// plagueGoHome walks the account out of the pocket the quest's far side makes: the cellar's stairs, the plague house's door, the
// Rehnisons' stairs, the city's manhole, the sewer's mud pile. It is also the pocket's Leave, so it needs nothing of the quest but
// the account's own pack: the manhole opens for anyone and the mud pile has no stage check.
func plagueGoHome(d *questrunner.Deps) bool {
	for step := 0; step < 10; step++ {
		area := plagueAreaNow(d)
		if area == questplan.PlagueEast {
			plagueSettle(d)
			return true
		}
		var ok bool
		switch area {
		case questplan.PlagueCellar:
			ok = plagueLeaveCellar(d)
		case questplan.PlagueUpstairs:
			ok = plagueDownStairs(d)
		case questplan.PlagueWest:
			ok = plagueLeaveCity(d)
		default:
			ok = plagueLeaveSewer(d)
		}
		if !ok {
			logf(d, "plague: no progress getting out of %s", area)
			return plagueAreaNow(d) == questplan.PlagueEast
		}
	}
	return plagueAreaNow(d) == questplan.PlagueEast
}

// plagueLeaveSewer climbs the mud pile at the sewer's east end to the garden.
func plagueLeaveSewer(d *questrunner.Deps) bool {
	if !questrunner.Travel(d, questplan.PlagueMudPileStand, 1) {
		return false
	}
	if !plagueOperate(d, questplan.PlagueLocMudPile, "", "Climb", questplan.PlagueMudPileStand, 4) {
		return false
	}
	plagueDrive(d, nil, func() bool { return plagueAreaNow(d) == questplan.PlagueEast }, plagueArrive)
	return plagueAreaNow(d) == questplan.PlagueEast
}

// plagueLeaveCity takes the manhole down from West Ardougne's street (an account in the plague house goes out of its door first).
func plagueLeaveCity(d *questrunner.Deps) bool {
	if questplan.InPlagueHouse(questrunner.CurrentTile(d)) {
		// From inside the door lets anyone out (plaguehouse's doors.rs2: not on the door's axis, it just opens).
		if !plagueOperate(d, questplan.PlagueLocHouseDoor, "", "Open", questplan.PlagueHouseInside, 3) {
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool { return !questplan.InPlagueHouse(questrunner.CurrentTile(d)) }) {
			return false
		}
	}
	if !questrunner.Travel(d, questplan.PlagueWestLanding, 1) {
		return false
	}
	if _, open := questrunner.FindLoc(d, questplan.PlagueLocManholeOpen, "", "Climb-down", questplan.PlagueManhole, 3); !open {
		// A manhole shut again (the cover reverts 500 ticks after the pipe opened it) offers Open; it stays open 500 ticks.
		if !plagueOperate(d, questplan.PlagueLocManholeShut, "", "Open", questplan.PlagueManhole, 3) {
			return false
		}
		if !waitUntil(d, 10*time.Second, func() bool {
			_, ok := questrunner.FindLoc(d, questplan.PlagueLocManholeOpen, "", "Climb-down", questplan.PlagueManhole, 3)
			return ok
		}) {
			logf(d, "plague: the manhole did not open")
			return false
		}
	}
	if !plagueOperate(d, questplan.PlagueLocManholeOpen, "", "Climb-down", questplan.PlagueManhole, 3) {
		return false
	}
	return plagueArrived(d, questplan.PlagueSewer, plagueArrive)
}

// plagueDownStairs goes down the Rehnisons' stairs.
func plagueDownStairs(d *questrunner.Deps) bool {
	if plagueAreaNow(d) != questplan.PlagueUpstairs {
		return true
	}
	if !plagueOperate(d, questplan.PlagueLocRehnisonTop, "", "Walk-down", questplan.PlagueRehnisonTop, 8) {
		return false
	}
	return plagueArrived(d, questplan.PlagueWest, plagueArrive)
}

// plagueLeaveCellar goes up the plague house's stairs, through the cell's gate first when the account is shut in the cell (the gate
// opens for the key, or for anyone once Elena is freed).
func plagueLeaveCellar(d *questrunner.Deps) bool {
	if here := questrunner.CurrentTile(d); here.X >= questplan.PlagueElenaGate.X+1 {
		if m, ok := questrunner.FindLoc(d, questplan.PlagueLocElenaGate, "", "Open", questplan.PlagueElenaGate, 3); ok {
			if held(d, questplan.PlagueObjKey) {
				_ = questrunner.UseItemOnLoc(d, questplan.PlagueObjKey, m)
			} else if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
				logf(d, "plague: the cell's gate: %v", err)
			}
			plagueDrive(d, nil, func() bool { return questrunner.CurrentTile(d).X <= questplan.PlagueElenaGate.X }, 15*time.Second)
		}
	}
	if !plagueOperate(d, questplan.PlagueLocCellarStairs, "", "Walk-Up", questplan.PlagueCellarStairs, 4) {
		return false
	}
	return plagueArrived(d, questplan.PlagueWest, plagueArrive)
}

// ---------------------------------------------------------------------------------------------------------------
// The mainland: the mask, the berries, the water, the dig.
// ---------------------------------------------------------------------------------------------------------------

// plagueCupboard searches Alrena's cupboard for the spare gas mask: open it, search it, take the mask (cupboard.rs2: only while
// the pack, the bank and the worn items hold none).
func plagueCupboard(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.PlagueObjGasMask) || questrunner.WornCounts(d)[questplan.PlagueObjGasMask] > 0 {
		return true
	}
	if _, open := questrunner.FindLoc(d, questplan.PlagueLocCupboardOpen, "", "Search", questplan.PlagueCupboard, 3); !open {
		if !plagueOperate(d, questplan.PlagueLocCupboardShut, "", "Open", questplan.PlagueCupboard, 3) {
			return false
		}
		if !waitUntil(d, 8*time.Second, func() bool {
			_, ok := questrunner.FindLoc(d, questplan.PlagueLocCupboardOpen, "", "Search", questplan.PlagueCupboard, 3)
			return ok
		}) {
			logf(d, "plague: Alrena's cupboard did not open")
			return false
		}
	}
	if !plagueOperate(d, questplan.PlagueLocCupboardOpen, "", "Search", questplan.PlagueCupboard, 3) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { return held(d, questplan.PlagueObjGasMask) }, 15*time.Second)
	logf(d, "plague: the cupboard's gas mask: %v", ok)
	return ok
}

// plagueBerries takes a dwellberry from McGrubor's Wood. Ten level-44 guard dogs stand in the wood, two of them in the only ways in:
// no route keeps four tiles from all of them, but one to the east side's berry (the others lie behind the dogs at the gaps) keeps
// four from every spawn, and that is the route taken, with a rectangle of three tiles round every dog that the walker will not step
// into. The account tops itself up before it goes in (the gate's price is in the decision), is not fought for (the quest does not
// retaliate) and goes out the way it came.
func plagueBerries(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.PlagueObjDwellberries) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "plague: no room for a dwellberry")
		return false
	}
	zones := plagueZones(questplan.PlagueGuardDogs, plagueDogBerth)
	spot := questplan.PlagueBerrySpots[0]
	questrunner.PrepareForFight(d, questplan.PlagueFoodIDs(), questplan.FoePlagueGuardDog.PeakHit())
	if !questrunner.TravelAvoiding(d, spot, 0, zones) {
		logf(d, "plague: the walk to the dwellberries at %d,%d keeping clear of the guard dogs did not arrive", spot.X, spot.Z)
		return false
	}
	got := false
	deadline := time.Now().Add(plagueBerryWait)
	for time.Now().Before(deadline) && !got {
		if questrunner.TakeGround(d, questplan.PlagueObjDwellberries, spot, 1) || held(d, questplan.PlagueObjDwellberries) {
			got = true
			break
		}
		// Another account took it: the spawn comes back by itself.
		if !d.Delay(5 * time.Second) {
			return false
		}
	}
	// Out the way it came, round the dogs, to the bank.
	if !questrunner.TravelAvoiding(d, tile.Tile{X: 2616, Z: 3332}, 6, zones) {
		logf(d, "plague: the way out of McGrubor's Wood did not finish")
	}
	logf(d, "plague: dwellberry: %v", got)
	return got
}

// plagueZones are the walker's rectangles for a list of guard dog spawns: berth tiles round each (a dog roams three from its spawn
// and notices a player one tile from where it stands, so the wood's berth is three beside the four the dogs reach, and the walk
// is planned round them).
func plagueZones(spawns []tile.Tile, berth int) []webwalk.Rect {
	rs := questplan.PlagueDogZones(spawns, berth)
	out := make([]webwalk.Rect, 0, len(rs))
	for _, r := range rs {
		out = append(out, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	return out
}

// plagueDogBerth is the rectangle half-width the dog walks use.
const plagueDogBerth = 3

// plagueWater fills the buckets at the market fountain and pours them on the garden soil until the journal says the soil is soft.
// The garden hands every bucket back empty, so the account with one bucket makes four trips and the account with four makes one;
// the journal is asked after every pour (it is the only reading of how many are left) and the step ends as soon as it reads soft.
func plagueWater(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for round := 0; round < 6; round++ {
		if e.InvalidateStage(); e.StageIs(questplan.PlagueSoft) {
			return true
		}
		if questrunner.HeldID(d, questplan.ObjBucketOfWater) == 0 && !plagueFill(d) {
			return false
		}
		if !questrunner.TravelAvoiding(d, questplan.PlagueGarden, 1, plagueZones(questplan.PlagueMarketDogs, plagueDogBerth)) {
			logf(d, "plague: could not walk back to Edmond's garden")
			return false
		}
		for questrunner.HeldID(d, questplan.ObjBucketOfWater) > 0 {
			patch, ok := plagueLoc(d, 0, "Mud patch", "", questplan.PlagueGarden, 6)
			if !ok {
				return false
			}
			before := questrunner.HeldID(d, questplan.ObjBucketOfWater)
			if err := questrunner.UseItemOnLoc(d, questplan.ObjBucketOfWater, patch); err != nil {
				logf(d, "plague: the water on the soil: %v", err)
				return false
			}
			if !plagueDrive(d, nil, func() bool { return questrunner.HeldID(d, questplan.ObjBucketOfWater) < before }, 20*time.Second) {
				logf(d, "plague: the soil took no water")
				return false
			}
			logf(d, "plague: poured a bucket onto the garden soil")
			if e.InvalidateStage(); e.StageIs(questplan.PlagueSoft) {
				return true
			}
		}
	}
	return false
}

// plagueFill fills every empty bucket in the pack (up to four) at the market fountain.
func plagueFill(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjBucket) == 0 {
		logf(d, "plague: no bucket to fill")
		return false
	}
	// Four guard dogs stand in the market between the bank and the fountain: the walk is planned round them.
	if !questrunner.TravelAvoiding(d, questplan.PlagueFountain, 2, plagueZones(questplan.PlagueMarketDogs, plagueDogBerth)) {
		logf(d, "plague: could not walk to the fountain")
		return false
	}
	fountain, ok := plagueLoc(d, questplan.PlagueLocFountain, "", "", questplan.PlagueFountain, 8)
	if !ok {
		return false
	}
	for filled := 0; filled < 4 && questrunner.HeldID(d, questplan.ObjBucket) > 0; filled++ {
		before := questrunner.HeldID(d, questplan.ObjBucketOfWater)
		if err := questrunner.UseItemOnLoc(d, questplan.ObjBucket, fountain); err != nil {
			logf(d, "plague: the bucket on the fountain: %v", err)
			break
		}
		if !waitUntil(d, 10*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjBucketOfWater) > before }) {
			break
		}
	}
	logf(d, "plague: %d bucket(s) of water from the fountain", questrunner.HeldID(d, questplan.ObjBucketOfWater))
	return questrunner.HeldID(d, questplan.ObjBucketOfWater) > 0
}

// plagueDig digs the soft soil with the spade: two message boxes and a fall into the sewer (mud_patch.rs2 elena_dig_mud_patch). The
// first box waits for a click before the account is moved, so a poll of the tile alone would wait for ever.
func plagueDig(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if plagueAreaNow(d) == questplan.PlagueSewer {
		return true
	}
	if !held(d, questplan.ObjSpade) {
		logf(d, "plague: no spade for the dig")
		return false
	}
	if !questrunner.Travel(d, questplan.PlagueGarden, 1) {
		return false
	}
	patch, ok := plagueLoc(d, 0, "Mud patch", "", questplan.PlagueGarden, 6)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjSpade, patch); err != nil {
		logf(d, "plague: the spade on the soil: %v", err)
		return false
	}
	ok = plagueDrive(d, nil, func() bool { return plagueAreaNow(d) == questplan.PlagueSewer }, plagueArrive)
	logf(d, "plague: dug into the sewer: %v", ok)
	return ok
}

// plagueScroll reads the Ardougne teleport scroll Edmond paid (elena_teleport_scroll.rs2): a box, then a second.
func plagueScroll(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !held(d, questplan.PlagueObjScroll) {
		return true
	}
	if err := questrunner.OpHeldByName(d, questplan.PlagueObjScroll, "Read"); err != nil {
		logf(d, "plague: reading the scroll: %v", err)
		return false
	}
	return plagueDrive(d, nil, func() bool { return !held(d, questplan.PlagueObjScroll) }, 20*time.Second)
}

// ---------------------------------------------------------------------------------------------------------------
// The sewer and the pipe.
// ---------------------------------------------------------------------------------------------------------------

// plagueRope ties the rope to the sewer pipe's grill (sewerpipe.rs2 [oplocu,plaguesewerpipe]).
func plagueRope(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !held(d, questplan.ObjRope) {
		logf(d, "plague: no rope for the grill")
		return false
	}
	if !questrunner.Travel(d, questplan.PlagueSewerPipeEnd, 1) {
		return false
	}
	pipe, ok := plagueLoc(d, questplan.PlagueLocSewerPipe, "", "", questplan.PlagueSewerPipeEnd, 6)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjRope, pipe); err != nil {
		logf(d, "plague: the rope on the pipe: %v", err)
		return false
	}
	ok = plagueDrive(d, nil, func() bool { return !held(d, questplan.ObjRope) }, 20*time.Second)
	logf(d, "plague: rope tied to the grill: %v", ok)
	return ok
}

// plagueCross puts the gas mask on and goes through the pipe to West Ardougne (sewerpipe.rs2 elena_climb_through_sewerpipe: the
// pipe checks the hat slot for the mask).
func plagueCross(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if plagueAreaNow(d) == questplan.PlagueWest {
		return true
	}
	if questrunner.WornCounts(d)[questplan.PlagueObjGasMask] == 0 {
		if !held(d, questplan.PlagueObjGasMask) || !questrunner.Equip(d, questplan.PlagueObjGasMask) {
			logf(d, "plague: the gas mask cannot be put on")
			return false
		}
		if !waitUntil(d, 6*time.Second, func() bool { return questrunner.WornCounts(d)[questplan.PlagueObjGasMask] > 0 }) {
			logf(d, "plague: the gas mask is not worn")
			return false
		}
	}
	if !questrunner.Travel(d, questplan.PlagueSewerPipeEnd, 1) {
		return false
	}
	if !plagueOperate(d, questplan.PlagueLocSewerPipe, "", "Search", questplan.PlagueSewerPipeEnd, 6) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { return plagueAreaNow(d) == questplan.PlagueWest }, plagueArrive)
	logf(d, "plague: through the pipe to West Ardougne: %v", ok)
	return ok
}

// ---------------------------------------------------------------------------------------------------------------
// West Ardougne.
// ---------------------------------------------------------------------------------------------------------------

// plagueInRehnisons reports the Rehnisons' ground floor.
func plagueInRehnisons(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2527 && t.X <= 2533 && t.Z >= 3329 && t.Z <= 3333
}

// plagueInBravek reports Bravek's room.
func plagueInBravek(t tile.Tile) bool {
	return t.Level == 0 && t.X >= 2530 && t.X <= 2539 && t.Z >= 3312 && t.Z <= 3316
}

// plagueEnterRehnisons goes in by the Rehnisons' door, from the door's own tile (rehnisons.rs2 rehnissons_enter_house: from stage
// 21 the door opens for anyone; at stage 20 it opens for an account with Jethick's book and nothing at all happens unless Ted is
// within seven tiles of it, which is waited for). When the account holds the book it is handed over in the chat that follows.
func plagueEnterRehnisons(d *questrunner.Deps, book bool) bool {
	if plagueInRehnisons(questrunner.CurrentTile(d)) {
		return true
	}
	if !plagueStandOn(d, questplan.PlagueRehnisonDoor) {
		logf(d, "plague: could not stand at the Rehnisons' door")
		return false
	}
	if !waitUntil(d, plagueNpcWait, func() bool { return plagueNpcNear(d, questplan.PlagueNpcTed, plagueHostEarshot) }) {
		logf(d, "plague: Ted Rehnison is not within %d tiles of the door: the door does nothing without him", plagueHostEarshot)
		return false
	}
	if !plagueOperate(d, questplan.PlagueLocRehnisonDoor, "", "Open", questplan.PlagueRehnisonDoor, 3) {
		return false
	}
	done := func() bool {
		return plagueInRehnisons(questrunner.CurrentTile(d)) && (!book || !held(d, questplan.PlagueObjBook))
	}
	ok := plagueDrive(d, nil, done, 45*time.Second)
	logf(d, "plague: through the Rehnisons' door (book %v): %v", book, ok)
	return ok
}

// plagueBook returns Jethick's book through the Rehnisons' door (stage 20 to 21).
func plagueBook(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return plagueEnterRehnisons(d, true) && !held(d, questplan.PlagueObjBook)
}

// plagueParents asks Ted (or Martha) about Elena (stage 21 to 22). At stage 20 the door stays shut to an account without the book
// and the leg fails, which sends the decision to Jethick for another book.
func plagueParents(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !plagueEnterRehnisons(d, false) {
		return false
	}
	for _, stop := range []questplan.NpcStop{questplan.PlagueTedStop, questplan.PlagueMarthaStop} {
		plagueConverse(d, stop, nil, plagueTalkBudget)
		if e.InvalidateStage(); e.StageIs(questplan.PlagueSpokenParents) {
			return true
		}
	}
	logf(d, "plague: the Rehnisons said nothing of Elena (the journal did not move)")
	return false
}

// plagueMilli goes up the Rehnisons' stairs, asks Milli, and comes down again (stage 22 to 23).
func plagueMilli(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if plagueAreaNow(d) == questplan.PlagueWest {
		if !plagueEnterRehnisons(d, false) {
			return false
		}
		if !plagueOperate(d, questplan.PlagueLocRehnisonStairs, "", "Walk-up", questplan.PlagueRehnisonStairs, 5) {
			return false
		}
		if !plagueArrived(d, questplan.PlagueUpstairs, plagueArrive) {
			logf(d, "plague: the stairs did not take the account up")
			return false
		}
	}
	plagueConverse(d, questplan.PlagueMilliStop, nil, plagueTalkBudget)
	plagueDownStairs(d)
	e.InvalidateStage()
	return e.StageIs(questplan.PlagueSpokenMilli)
}

// plagueDown comes down the Rehnisons' stairs.
func plagueDown(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return plagueDownStairs(d)
}

// plagueMourner asks the mourner at the plague house's door about clearance (stage 23 to 24): from the door's own tile, with a
// mourner within fourteen tiles (doors.rs2: with none in earshot the door says nothing at all).
func plagueMourner(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !plagueStandOn(d, questplan.PlagueHouseDoor) {
		logf(d, "plague: could not stand at the plague house's door")
		return false
	}
	if !waitUntil(d, plagueNpcWait, func() bool { return plagueNpcNear(d, questplan.PlagueNpcMourner, plagueMournerEarshot) }) {
		logf(d, "plague: no mourner within %d tiles of the plague house's door: the door does nothing without one", plagueMournerEarshot)
		return false
	}
	if !plagueOperate(d, questplan.PlagueLocHouseDoor, "", "Open", questplan.PlagueHouseDoor, 3) {
		return false
	}
	plagueDrive(d, questplan.PlagueMournerStop.Prefer, nil, plagueTalkBudget)
	e.InvalidateStage()
	return e.StageIs(questplan.PlagueNeedClearance)
}

// plagueEnterBravek goes in by Bravek's door (bravekdoorshut: from stage 25 it opens for anyone, before that Bravek is "in a
// meeting"). It reports whether the account is in the room.
func plagueEnterBravek(d *questrunner.Deps) bool {
	if plagueInBravek(questrunner.CurrentTile(d)) {
		return true
	}
	if !plagueOperate(d, questplan.PlagueLocBravekDoor, "", "Open", questplan.PlagueBravekDoor, 4) {
		return false
	}
	plagueDrive(d, nil, func() bool { return plagueInBravek(questrunner.CurrentTile(d)) }, 20*time.Second)
	if !plagueInBravek(questrunner.CurrentTile(d)) {
		logf(d, "plague: Bravek's door stayed shut (the clerk has not called him in)")
		return false
	}
	return true
}

// plagueAudience has the clerk call Bravek in, goes through Bravek's door and asks for the recipe (stage 24 or 25 to 26). The
// clerk's call works only while Bravek is within seven tiles of the account, so the leg waits for him to wander near.
func plagueAudience(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !plagueInBravek(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, questplan.PlagueClerkAnchor, 1) {
			return false
		}
		if !waitUntil(d, plagueNpcWait, func() bool { return plagueNpcNear(d, questplan.PlagueNpcBravek, plagueHostEarshot) }) {
			logf(d, "plague: Bravek is not within %d tiles of the clerk: the clerk cannot call him in", plagueHostEarshot)
			return false
		}
		plagueConverse(d, questplan.PlagueClerkStop, nil, plagueTalkBudget)
		if !plagueEnterBravek(d) {
			return false
		}
	}
	plagueConverse(d, questplan.PlagueBravekRecipe, func() bool { return held(d, questplan.PlagueObjNote) }, plagueTalkBudget)
	e.InvalidateStage()
	return held(d, questplan.PlagueObjNote) || e.StageIs(questplan.PlagueSpokenBravek)
}

// plagueCure gives Bravek the hangover cure and asks for the warrant (stage 26 to 27); at stage 27 without the warrant it asks
// again (bravek.rs2 gives another to an account whose pack holds none).
func plagueCure(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !plagueEnterBravek(d) {
		return false
	}
	plagueConverse(d, questplan.PlagueBravekWarrant, func() bool { return held(d, questplan.PlagueObjWarrant) }, plagueTalkBudget)
	ok := held(d, questplan.PlagueObjWarrant)
	logf(d, "plague: the warrant from Bravek: %v", ok)
	return ok
}

// plagueHouse is the plague house (stage 27 to 28): in by the door on the warrant, the key from the barrel, the stairs down, the
// cell's gate with the key, and Elena. An account already inside, or already in the cellar, takes up from there.
func plagueHouse(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	area := plagueAreaNow(d)
	if area == questplan.PlagueWest && !questplan.InPlagueHouse(questrunner.CurrentTile(d)) {
		if !plagueEnterHouse(d) {
			return false
		}
	}
	if plagueAreaNow(d) == questplan.PlagueWest {
		if !held(d, questplan.PlagueObjKey) && !plagueTakeKey(d) {
			return false
		}
		if !plagueOperate(d, questplan.PlagueLocHouseStairs, "", "Walk-Down", questplan.PlagueHouseStairs, 5) {
			return false
		}
		if !plagueArrived(d, questplan.PlagueCellar, plagueArrive) {
			logf(d, "plague: the stairs did not take the account down")
			return false
		}
	}
	if plagueAreaNow(d) != questplan.PlagueCellar {
		return false
	}
	if questrunner.CurrentTile(d).X <= questplan.PlagueElenaGate.X {
		gate, ok := plagueLoc(d, questplan.PlagueLocElenaGate, "", "Open", questplan.PlagueElenaGate, 3)
		if !ok {
			return false
		}
		if !held(d, questplan.PlagueObjKey) {
			logf(d, "plague: no key for the cell")
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.PlagueObjKey, gate); err != nil {
			logf(d, "plague: the key on the cell's gate: %v", err)
			return false
		}
		if !plagueDrive(d, nil, func() bool { return questrunner.CurrentTile(d).X > questplan.PlagueElenaGate.X }, 20*time.Second) {
			logf(d, "plague: the cell's gate did not let the account through")
			return false
		}
	}
	plagueConverse(d, questplan.PlagueElenaStop, nil, plagueTalkBudget)
	e.InvalidateStage()
	return e.StageIs(questplan.PlagueFreed)
}

// plagueEnterHouse goes in by the plague house's door on the warrant: from the door's own tile (z is what the script compares), with
// a mourner in earshot, and the script sneaks the account in while its mesbox is up. It reports whether the account is inside.
func plagueEnterHouse(d *questrunner.Deps) bool {
	if !held(d, questplan.PlagueObjWarrant) {
		logf(d, "plague: no warrant for the plague house")
		return false
	}
	if !plagueStandOn(d, questplan.PlagueHouseDoor) {
		logf(d, "plague: could not stand at the plague house's door")
		return false
	}
	if !waitUntil(d, plagueNpcWait, func() bool { return plagueNpcNear(d, questplan.PlagueNpcMourner, plagueMournerEarshot) }) {
		logf(d, "plague: no mourner within %d tiles of the plague house's door: the warrant does nothing without one", plagueMournerEarshot)
		return false
	}
	if !plagueOperate(d, questplan.PlagueLocHouseDoor, "", "Open", questplan.PlagueHouseDoor, 3) {
		return false
	}
	plagueDrive(d, nil, func() bool { return questplan.InPlagueHouse(questrunner.CurrentTile(d)) }, plagueTalkBudget)
	in := questplan.InPlagueHouse(questrunner.CurrentTile(d))
	logf(d, "plague: into the plague house: %v", in)
	return in
}

// plagueTakeKey searches the barrel for the small key (plaguehouse.rs2: only at stage 27, and only while the pack holds none).
func plagueTakeKey(d *questrunner.Deps) bool {
	if !plagueOperate(d, questplan.PlagueLocBarrel, "", "Search", questplan.PlagueBarrel, 5) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { return held(d, questplan.PlagueObjKey) }, 20*time.Second)
	logf(d, "plague: the key from the barrel: %v", ok)
	return ok
}

// ---------------------------------------------------------------------------------------------------------------
// The hangover cure.
// ---------------------------------------------------------------------------------------------------------------

// plagueMilk milks a cow in the field east of the city with the empty bucket (npc/cow_milking.rs2: "You milk the cow.").
func plagueMilk(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.ObjBucketOfMilk) {
		return true
	}
	if !held(d, questplan.ObjBucket) {
		logf(d, "plague: no empty bucket to milk with")
		return false
	}
	if !questrunner.Travel(d, questplan.PlagueCowField, 6) {
		return false
	}
	refused := map[int]bool{}
	deadline := time.Now().Add(plagueMilkBudget)
	for time.Now().Before(deadline) && !held(d, questplan.ObjBucketOfMilk) {
		cow, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(20).
			Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcDairyCow && !refused[n.Index] }).Nearest()
		if !ok {
			questrunner.Travel(d, questplan.PlagueCowField, 4)
			refused = map[int]bool{}
			if !d.Delay(3 * time.Second) {
				return false
			}
			continue
		}
		if err := questrunner.UseItemOnNpc(d, questplan.ObjBucket, cow); err != nil {
			logf(d, "plague: the bucket on the cow at %d,%d: %v", cow.X, cow.Z, err)
			refused[cow.Index] = true
			continue
		}
		if waitUntil(d, 6*time.Second, func() bool { return held(d, questplan.ObjBucketOfMilk) }) {
			logf(d, "plague: milked the cow at %d,%d", cow.X, cow.Z)
			return true
		}
		refused[cow.Index] = true
	}
	return held(d, questplan.ObjBucketOfMilk)
}

// plagueGrassExit is where the account goes once it has the grass (or has not): Falador's west bank, far enough north of the trail
// that nothing that followed it does.
var plagueGrassExit = tile.Tile{X: 2946, Z: 3368}

// plagueGrass takes a snape grass from the hobgoblins' trail. The three spawns lie among nine hobgoblins that roam fifteen tiles, so
// the decision has priced the leg (PlagueGrassOdds) and the account tops itself up first; it is not fought for (the quest does not
// retaliate), takes the spawn furthest from the armed hobgoblin first, takes the grass in a click, and leaves north. A spawn with
// nothing on it is left for the next (30 ticks to come back), and when none has any the account waits OUTSIDE the trail for the
// respawn rather than standing among the hobgoblins.
func plagueGrass(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.PlagueObjSnapeGrass) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "plague: no room for a snape grass")
		return false
	}
	if combat, _ := d.Session.State().CombatLevel(); questplan.PlagueGrassHunted(combat) {
		questrunner.PrepareForFight(d, questplan.PlagueFoodIDs(), questplan.FoePlagueHobgoblin.PeakHit())
	}
	deadline := time.Now().Add(plagueGrassWait)
	for time.Now().Before(deadline) {
		for _, spawn := range questplan.PlagueGrassSpawns {
			if !questrunner.TravelCautious(d, spawn, 1) {
				logf(d, "plague: the walk to the snape grass at %d,%d did not arrive", spawn.X, spawn.Z)
				continue
			}
			if questrunner.TakeGround(d, questplan.PlagueObjSnapeGrass, spawn, 1) || held(d, questplan.PlagueObjSnapeGrass) {
				questrunner.TravelCautious(d, plagueGrassExit, 8)
				logf(d, "plague: a snape grass from the trail at %d,%d", spawn.X, spawn.Z)
				return true
			}
		}
		// None had grass: out of the trail, and wait for a respawn there.
		questrunner.TravelCautious(d, plagueGrassExit, 8)
		if !d.Delay(30 * time.Second) {
			return false
		}
	}
	logf(d, "plague: no snape grass on the hobgoblins' trail in %s", plagueGrassWait)
	return false
}

// plagueBrew makes the next mix the pack allows, until none is left (pestle on the chocolate bar, dust into the milk, grass into
// that): cakes.rs2 make_chocolate_milk and make_hangover_cure end in a message box each.
func plagueBrew(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	product := map[int]int{questplan.PlagueObjPestle: questplan.PlagueObjChocolateDust, questplan.PlagueObjChocolateDust: questplan.PlagueObjChocolateMilk,
		questplan.PlagueObjSnapeGrass: questplan.PlagueObjHangoverCure}
	made := 0
	for i := 0; i < 4; i++ {
		use, on, ok := questplan.PlagueNextMix(func(id int) bool { return held(d, id) })
		if !ok {
			break
		}
		plagueSettle(d)
		want := product[use]
		before := questrunner.HeldID(d, want)
		if err := questrunner.UseItemOnItem(d, use, on); err != nil {
			logf(d, "plague: %s on %s: %v", questrunner.ObjName(d, use), questrunner.ObjName(d, on), err)
			break
		}
		if !waitUntil(d, 8*time.Second, func() bool { return questrunner.HeldID(d, want) > before }) {
			logf(d, "plague: %s on %s made no %s", questrunner.ObjName(d, use), questrunner.ObjName(d, on), questrunner.ObjName(d, want))
			break
		}
		plagueSettle(d)
		made++
		logf(d, "plague: mixed %s", strings.ToLower(questrunner.ObjName(d, want)))
	}
	return made > 0
}
