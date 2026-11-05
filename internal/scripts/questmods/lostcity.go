package questmods

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// LostCity is Lost City ready for the engine: questplan/lostcity.go says why each part is what it is. The talks (the warrior, the monks) and
// the carving of the branch are the generic steps; the customs are the parts the content makes special: Shamus and the leprechaun's tree, the
// strip and the ship, the cave monk's one-way ladder, an axe taken from an Entrana zombie, the corridor of the greater demons and the Tree
// spirit, the branch, the teleport out of the dungeon (the Magic door into the Wilderness only as the last resort) and the shed's door into
// Zanaris.
func LostCity() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.LostCity,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomLcShamus:  lcShamus,
			questplan.CustomLcSail:    lcSail,
			questplan.CustomLcDescend: lcDescend,
			questplan.CustomLcAxe:     lcAxe,
			questplan.CustomLcSpirit:  lcSpirit,
			questplan.CustomLcBranch:  lcBranch,
			questplan.CustomLcPortal: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return lcLeaveDungeon(e.Deps())
			},
			questplan.CustomLcZanaris: lcZanaris,
		},
		// The door's teleport leaves the account in Zanaris, which the walker leaves by its entrance ladder: the account is walked out
		// to the shed's door once the quest is complete, so the stage loop's next walk does not start underground. Best effort: the
		// server already says the quest is done.
		Finish: lcLeaveZanaris,
	}
}

const (
	// lcTabWait is how long a conversation's result (the tab, the stage) gets to show.
	lcTabWait = 6 * time.Second
	// lcShamusTries is how many times the leprechaun's tree is chopped before the step is given up: a Shamus that is out already makes
	// the tree answer "Hey! Yer big elephant!" and nothing else, and he is gone in a hundred ticks (a minute).
	lcShamusTries = 3
	// lcShamusAppear is how long a chop gets to put Shamus on the ground, lcShamusRetry how long a refused chop waits before the next.
	lcShamusAppear = 10 * time.Second
	lcShamusRetry  = 30 * time.Second
	// lcSailBudget is how long the conversation with the Port Sarim monk and the ship take to put the account on Entrana's deck.
	lcSailBudget = 40 * time.Second
	// lcArriveWait is how long the ladder and the doors get to set the account down.
	lcArriveWait = 14 * time.Second
	// lcDialogueBudget bounds a conversation that ends in a teleport.
	lcDialogueBudget = 45 * time.Second
	// lcAxeBudget bounds the zombie hunt for an axe: a zombie drops a bronze axe 50 times in 128 (about one kill in 2.5), an iron axe once
	// more, and comes back 70 ticks after a death.
	lcAxeBudget = 12 * time.Minute
	// lcZombieFight bounds one zombie's fight (30 hitpoints).
	lcZombieFight = 60 * time.Second
	// lcSpiritBudget bounds the corridor, the chop and the fight with the Tree spirit.
	lcSpiritBudget = 9 * time.Minute
	// lcDropWait is how long a kill's drop is waited for (the death animation, then the drop).
	lcDropWait = 4 * time.Second
	// lcTreeWait is how long a chop of the Dramen tree gets to put the branch in the pack or the spirit on the ground, lcSpiritComeWait how long
	// the spirit gets to walk round the tree to the account.
	lcTreeWait       = 10 * time.Second
	lcSpiritComeWait = 6 * time.Second
	// lcWalkBudget bounds one walk of the dungeon.
	lcWalkBudget = 3 * time.Minute
	// lcShedWait is how long the shed's door gets to take the account into Zanaris.
	lcShedWait = 15 * time.Second
	// lcCastTries is how many times an exit teleport is cast before the account is given up on for the run: a click can be dropped (nothing is
	// spent then), a landed cast spends its runes and the next try finds the account out.
	lcCastTries = 3
)

// lcCastWait is how long one exit cast gets to set the account down: the click is handled on the next tick, the cast animation and its two
// delays follow (about four ticks in all). A variable only so a test can shorten it.
var lcCastWait = 12 * time.Second

// lcAvoidRects are questplan.LcBlackDemonAvoid as the walker's rectangles, made once.
var lcAvoidRects = sync.OnceValue(func() []webwalk.Rect {
	var out []webwalk.Rect
	for _, r := range questplan.LcBlackDemonAvoid() {
		out = append(out, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	return out
})

// lcGo walks to within radius of dest keeping out of the black demons' ground (the corridor of the greater demons has no way round and is
// walked at full hitpoints). The ships and teleports of the stage loop's own hook (Deps.BeforeTravel) run first, as Travel's do.
func lcGo(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if here, ok := questrunner.CurrentTileOK(d); ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	if d.BeforeTravel != nil {
		d.BeforeTravel(dest)
	}
	return questrunner.TravelAvoiding(d, dest, radius, lcAvoidRects())
}

// lcHasAxe reports any axe in the pack or worn.
func lcHasAxe(d *questrunner.Deps) bool {
	pack, worn := questrunner.PackCounts(d), questrunner.WornCounts(d)
	for _, a := range questplan.LcAxes {
		if pack[a] > 0 || worn[a] > 0 {
			return true
		}
	}
	return false
}

// lcCorridorReady tops the account up to its full hitpoints before the corridor of the greater demons and says whether it may go in: an
// account that cannot mend itself to seven tenths of its hitpoints (no food left) is not walked past two greater demons.
func lcCorridorReady(d *questrunner.Deps) bool {
	sus := questplan.LostCity.Sustain
	hp, max := questrunner.Hitpoints(d)
	if max == 0 {
		return true
	}
	if hp < max {
		questrunner.TopUp(d, sus.FoodIDs, max)
	}
	if hp, max = questrunner.Hitpoints(d); hp*10 < max*7 {
		logf(d, "lost city: %d of %d hitpoints and nothing to mend them with: not walking the corridor of the greater demons", hp, max)
		return false
	}
	return true
}

// ---------------------------------------------------------------------------------------------------------
// Shamus.
// ---------------------------------------------------------------------------------------------------------

// lcShamus chops the leprechaun's tree (an axe in the pack: the module's kit) and talks to Shamus, who walks ten tiles east and is gone in a
// hundred ticks. A Shamus another account's chop has set down is talked to as well: the conversation is the account's own stage.
func lcShamus(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	stop := questplan.LcShamusStop
	if _, ok := questrunner.FindNpc(d, stop, stop.Leash); !ok {
		if !lcChopLeprechaunTree(d, stop) {
			return false
		}
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	ok := questrunner.NewDialogue(d).Drive(stop.Prefer)
	// "The leprechaun magically disappears." is a message box on the main modal: it is continued, never left up.
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	if !e.StageIs(questplan.LcSpokenShamus) {
		logf(d, "lost city: the talk with Shamus (driven to its end: %v) did not take the journal to stage 2", ok)
		return false
	}
	return true
}

// lcChopLeprechaunTree walks to the leprechaun's tree and chops it until Shamus is on the ground.
func lcChopLeprechaunTree(d *questrunner.Deps, stop questplan.NpcStop) bool {
	if !questrunner.Travel(d, questplan.LcShamusTreeAt, 3) {
		logf(d, "lost city: the leprechaun's tree (%v) was not reached from %v", questplan.LcShamusTreeAt, questrunner.CurrentTile(d))
		return false
	}
	for try := 0; try < lcShamusTries; try++ {
		m, ok := questrunner.FindLoc(d, questplan.LocLcLeprechaunTree, "", "Chop", questplan.LcShamusTreeAt, 5)
		if !ok {
			logf(d, "lost city: no leprechaun's tree (loc %d) near %v", questplan.LocLcLeprechaunTree, questplan.LcShamusTreeAt)
			return false
		}
		if err := questrunner.OperateLoc(d, m, "Chop"); err != nil {
			logf(d, "lost city: chopping the leprechaun's tree: %v", err)
			return false
		}
		if waitUntil(d, lcShamusAppear, func() bool { _, ok := questrunner.FindNpc(d, stop, stop.Leash); return ok }) {
			return true
		}
		logf(d, "lost city: no Shamus after the chop (another account's is out, or the axe is not the tree's): waiting %s", lcShamusRetry)
		if !d.Delay(lcShamusRetry) {
			return false
		}
	}
	return false
}

// ---------------------------------------------------------------------------------------------------------
// The ship and the ladder.
// ---------------------------------------------------------------------------------------------------------

// lcKeepSet is the ids a strip leaves in the pack.
func lcKeepSet() map[int]bool {
	keep := map[int]bool{}
	for _, id := range questplan.LcEntranaKeep() {
		keep[id] = true
	}
	return keep
}

// lcPackNames lists the pack's items for a log line.
func lcPackNames(d *questrunner.Deps) string {
	pack := questrunner.PackCounts(d)
	var out []string
	for id, n := range pack {
		out = append(out, fmt.Sprintf("%s x%d", questrunner.ObjName(d, id), n))
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// lcSail strips at Draynor's bank (every worn piece off, every pack item the monks forbid or the quest does not carry banked, the deposit
// bare so the run staller's jewellery is not kept), walks to the Port Sarim monks and sails. The monk searches the pack and the worn items
// and refuses (monk_of_entrana.rs2) any weapon, armour, axe, pickaxe or staff; an account he refuses is left with what it carries in the
// log. The ship sets the account down on Entrana's deck.
func lcSail(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questplan.OnEntrana(questrunner.CurrentTile(d)) {
		return true
	}
	keep := lcKeepSet()
	if !StripAtBank(d, questplan.DraynorBank, func(id int) bool { return keep[id] }) {
		logf(d, "lost city: the strip at Draynor's bank left something on or in the pack: %s", lcPackNames(d))
		return false
	}
	if !questrunner.Travel(d, questplan.PortSarimMonks, 4) {
		logf(d, "lost city: the Port Sarim monks (%v) were not reached from %v", questplan.PortSarimMonks, questrunner.CurrentTile(d))
		return false
	}
	stop := questplan.LcPortMonkStop
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	onIsland := func() bool { return questplan.OnEntrana(questrunner.CurrentTile(d)) }
	questrunner.NewDialogue(d).DriveUntil(onIsland, stop.Prefer, lcSailBudget)
	questrunner.ClearLeftoverModal(d)
	if !onIsland() {
		logf(d, "lost city: the monks did not sail; the pack holds %s", lcPackNames(d))
		return false
	}
	return true
}

// lcDescend walks from Entrana's deck to the cave monk (the graph has the gangplank), tops up the account's hitpoints (the ladder is one
// way and the corridor below is the first thing met), and talks the monk into the ladder: "Well that is a risk I will have to take." sets the
// account down at 2822,9774.
func lcDescend(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questplan.InLcDungeon(questrunner.CurrentTile(d)) {
		return true
	}
	stop := questplan.LcCaveMonkStop
	if !questrunner.Travel(d, stop.Anchor, 2) {
		logf(d, "lost city: the cave monk (%v) was not reached from %v", stop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	if !lcCorridorReady(d) {
		e.RequestPark("not enough food on Entrana to go down the one-way ladder: the corridor of the greater demons is the first thing below", questplan.LcItemParkFor)
		return false
	}
	// The ladder's drop is a teleport (p_telejump) and the account's own: announced, not a random event.
	land := questplan.LcDungeonArrival
	withdraw := randomevents.ExpectLandings(d.Session, "lost city: the cave monk's ladder into the Entrana dungeon", lcDialogueBudget, randomevents.LandingNear(land.X, land.Z, 3))
	if !questrunner.OpenDialogue(d, stop) {
		withdraw()
		return false
	}
	inDungeon := func() bool { return questplan.InLcDungeon(questrunner.CurrentTile(d)) }
	questrunner.NewDialogue(d).DriveUntil(inDungeon, stop.Prefer, lcDialogueBudget)
	questrunner.ClearLeftoverModal(d)
	if !waitUntil(d, lcArriveWait, inDungeon) {
		withdraw()
		logf(d, "lost city: the cave monk's ladder did not take the account down (at %v)", questrunner.CurrentTile(d))
		return false
	}
	logf(d, "lost city: down the ladder, in the Entrana dungeon at %v", questrunner.CurrentTile(d))
	return true
}

// ---------------------------------------------------------------------------------------------------------
// An axe from the zombies.
// ---------------------------------------------------------------------------------------------------------

const (
	// lcZombieSearch is how far from the account a zombie may be and still be chosen, lcZombieWindow the square (half its side) the flood
	// over open ground covers round the account.
	lcZombieSearch = 16
	lcZombieWindow = 30
	// lcZombieSkipFor is how long a zombie given up on is left alone.
	lcZombieSkipFor = 90 * time.Second
	// lcZombieNoHitAfter is how long a fight may go without a blow landing before the zombie is given up.
	lcZombieNoHitAfter = 25 * time.Second
	// lcZombieNorthBound is the northmost row a zombie is fought on: the black demons' ground begins at 9767, and a zombie wanders nine tiles.
	lcZombieNorthBound = 9764
)

// lcZombieRun is one hunt's state.
type lcZombieRun struct {
	d     *questrunner.Deps
	skip  map[int]time.Time
	reach *pathfind.Reach
	from  tile.Tile
	at    time.Time
	won   int
}

func (r *lcZombieRun) flood(here tile.Tile) *pathfind.Reach {
	if r.d.Pack == nil {
		return nil
	}
	if r.reach != nil && r.from == here && time.Since(r.at) < 2*time.Second {
		return r.reach
	}
	reach := r.d.Pack.ReachFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, lcZombieWindow, nil)
	if reach.Size() == 0 {
		reach = nil
	}
	r.reach, r.from, r.at = reach, here, time.Now()
	return reach
}

func (r *lcZombieRun) steps(here, t tile.Tile) int {
	return interactSteps(r.d.Pack, r.flood(here), here, t)
}

// lcZombieOK reports a zombie worth fighting: south of the black demons' ground, on the west side of it, not in another player's fight.
func lcZombieOK(e entityquery.Entity, self int) bool {
	t := tile.Tile{X: e.X, Z: e.Z, Level: e.Level}
	return e.ID == questplan.NpcLcZombie && e.Z <= lcZombieNorthBound && !questplan.InLcBlackDemonGround(t) &&
		!(e.InCombat && self >= 0 && e.FaceEntity >= 32768 && e.FaceEntity-32768 != self)
}

// lcAxe kills Entrana zombies on the west side of their ground, unarmed (the monks took everything), until one drops an axe, and takes it. The
// zombie is level 25 with 30 hitpoints and hits up to 3; the black demons beyond its north side are never walked into (lcZombieNorthBound,
// LcBlackDemonAvoid). No axe in lcAxeBudget parks the quest, with the reason, and the account stays on the zombies' ground for the next run.
func lcAxe(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if lcHasAxe(d) {
		return true
	}
	if !lcGo(d, questplan.LcZombieStand, 3) {
		logf(d, "lost city: the zombies' ground (%v) was not reached from %v", questplan.LcZombieStand, questrunner.CurrentTile(d))
		return false
	}
	r := &lcZombieRun{d: d, skip: map[int]time.Time{}}
	deadline := time.Now().Add(lcAxeBudget)
	sus := questplan.LostCity.Sustain
	idle := 0
	for time.Now().Before(deadline) {
		if lcHasAxe(d) {
			logf(d, "lost city: an axe from the zombies after %d kills", r.won)
			return true
		}
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if lcTakeAxeFromGround(d) {
			continue
		}
		if questrunner.FreeSlots(d) == 0 {
			logf(d, "lost city: no room in the pack for an axe")
			return false
		}
		here := questrunner.CurrentTile(d)
		self := d.Session.State().Self()
		npcs := entityquery.Npcs(d.Session.State(), d.Config).Within(lcZombieSearch).Results()
		var best entityquery.Entity
		bestSteps, found := 0, false
		for _, n := range npcs {
			if !lcZombieOK(n, self) || !time.Now().After(r.skip[n.Index]) {
				continue
			}
			s := r.steps(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
			if s < 0 {
				continue
			}
			if !found || s < bestSteps || (s == bestSteps && n.Index < best.Index) {
				best, bestSteps, found = n, s, true
			}
		}
		if !found {
			idle++
			if idle == 1 || idle%20 == 0 {
				logf(d, "lost city: no zombie in reach of %v (%d npcs in view); waiting for one", here, len(npcs))
			}
			if tile.Chebyshev(here, questplan.LcZombieStand) > 4 {
				lcGo(d, questplan.LcZombieStand, 3)
			}
			if !d.Delay(3 * time.Second) {
				return false
			}
			continue
		}
		idle = 0
		began := time.Now()
		why := ""
		giveUp := func() bool {
			cur, ok := entityquery.Npcs(d.Session.State(), d.Config).Within(lcZombieSearch + 4).
				Where(func(n entityquery.Entity) bool { return n.Index == best.Index && n.ID == best.ID }).Nearest()
			if !ok {
				return false
			}
			at := tile.Tile{X: cur.X, Z: cur.Z, Level: cur.Level}
			switch {
			case questplan.InLcBlackDemonGround(questrunner.CurrentTile(d)):
				why = "the fight has drawn the account into a black demon's ground"
			case cur.Z > lcZombieNorthBound+1:
				why = "it has wandered north"
			case r.steps(questrunner.CurrentTile(d), at) < 0:
				why = "no walk over open ground reaches it"
			case time.Since(began) >= lcZombieNoHitAfter && cur.TotalHealth == 0:
				why = "no blow has landed on it"
			}
			return why != ""
		}
		switch questrunner.FightChosen(d, best, sus, lcZombieFight, giveUp) {
		case questrunner.FightWon:
			r.won++
			waitUntil(d, lcDropWait, func() bool { return lcAxeOnGround(d) })
			lcTakeAxeFromGround(d)
		case questrunner.FightGaveUp:
			r.skip[best.Index] = time.Now().Add(lcZombieSkipFor)
			logf(d, "lost city: gave up on the zombie at %d,%d: %s", best.X, best.Z, why)
		case questrunner.FightTimedOut:
			r.skip[best.Index] = time.Now().Add(lcZombieSkipFor)
		case questrunner.FightStopped:
			return false
		case questrunner.FightLost:
			if !d.Delay(2 * time.Second) {
				return false
			}
		}
		if questplan.InLcBlackDemonGround(questrunner.CurrentTile(d)) {
			lcGo(d, questplan.LcZombieStand, 2)
		}
	}
	if lcHasAxe(d) {
		return true
	}
	e.RequestPark(fmt.Sprintf("no axe from the Entrana zombies in %s (%d zombies killed)", lcAxeBudget, r.won), questplan.LcLegPark)
	return false
}

// lcAxeOnGround reports a bronze or iron axe (what a zombie drops) lying within a few tiles.
func lcAxeOnGround(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	for _, id := range []int{questplan.ObjBronzeAxe, questplan.ObjIronAxe} {
		if len(questrunner.GroundItemsNear(d, id, here, 6)) > 0 {
			return true
		}
	}
	return false
}

// lcTakeAxeFromGround takes a bronze or iron axe lying within a few tiles and reports whether one came into the pack.
func lcTakeAxeFromGround(d *questrunner.Deps) bool {
	here := questrunner.CurrentTile(d)
	for _, id := range []int{questplan.ObjBronzeAxe, questplan.ObjIronAxe} {
		if len(questrunner.GroundItemsNear(d, id, here, 6)) > 0 && questrunner.TakeGround(d, id, here, 6) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------------------------------------
// The Dramen tree.
// ---------------------------------------------------------------------------------------------------------

// lcSpirit runs the corridor to the Dramen tree at full hitpoints, stands south of it, tops up for the fight and chops: at stage 2 that sets the
// Tree spirit on the account (no branch comes), which is cast at from the tree's stand with the best spell the pack's runes pay for and, when
// they run out with the spirit still standing, meleed only when the model says the account's swing finishes it with the margin
// (questplan.MeleeOdds). The step is the stage's: it succeeds when the journal says the spirit is defeated.
func lcSpirit(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	began := time.Now()
	deadline := began.Add(lcSpiritBudget)
	sus := questplan.LostCity.Sustain
	if !lcCorridorReady(d) {
		e.RequestPark("under seven tenths of the hitpoints at the corridor of the greater demons and nothing to mend them with", questplan.LcItemParkFor)
		return false
	}
	if !lcGo(d, questplan.LcTreeStand, 0) && !lcGo(d, questplan.LcTreeStand, 1) {
		logf(d, "lost city: the Dramen tree's stand (%v) was not reached from %v", questplan.LcTreeStand, questrunner.CurrentTile(d))
		return false
	}
	// Out of the corridor: the demons that followed have turned back at their own ground, which the tree is beyond. Mended to the level a
	// fight starts at, before the first chop (the spirit attacks the moment it is set down).
	questrunner.PrepareForFight(d, sus.FoodIDs, questplan.FoeTreeSpirit.PeakHit())
	for time.Now().Before(deadline) {
		if questrunner.DiedSince(d, began) {
			// The respawn is in Lumbridge with nothing in the pack: the engine counts the death and parks the quest.
			return false
		}
		if _, ok := lcSpiritNpc(d); !ok {
			m, found := questrunner.FindLoc(d, questplan.LocLcDramenTree, "", "Chop down", questplan.LcTreeAt, 4)
			if !found {
				logf(d, "lost city: no Dramen tree (loc %d) near %v", questplan.LocLcDramenTree, questplan.LcTreeAt)
				return false
			}
			if err := questrunner.OperateLoc(d, m, "Chop down"); err != nil {
				logf(d, "lost city: chopping the Dramen tree: %v", err)
				return false
			}
			if !waitUntil(d, lcTreeWait, func() bool { _, ok := lcSpiritNpc(d); return ok }) {
				// The stage says whether a branch came (stage 3 already) or the spirit is another account's and busy.
				if e.InvalidateStage(); e.StageIs(questplan.LcSpiritDefeated) {
					return true
				}
				logf(d, "lost city: no Tree spirit after the chop (the tree is another account's fight): waiting")
				if !d.Delay(10 * time.Second) {
					return false
				}
				continue
			}
		}
		// Let the spirit come: it attacks the moment it is set down, and a cast at it round the tree's corner (the tree is a 2x2 loc between them
		// and blocks the line) would walk the account towards the greater demons' ground.
		waitUntil(d, lcSpiritComeWait, func() bool {
			sp, ok := lcSpiritNpc(d)
			return ok && tile.Chebyshev(tile.Tile{X: sp.X, Z: sp.Z, Level: sp.Level}, questrunner.CurrentTile(d)) <= 2
		})
		if !lcFightSpirit(e, d, sus) {
			// The spirit still stands and the fight was lost or given up (a lost one has walked the account away already): not this account's
			// today. The engine counts the lost fight (a day's park), and the step's Failed parks the quest for six hours at the least.
			return false
		}
		break
	}
	// The spirit's death raises a message box (With the Tree Spirit defeated you can now chop the tree.) that is continued, then the journal.
	waitUntil(d, lcTabWait, func() bool { m, _, _ := d.Session.State().Modals(); return m != gamestate.NoModal })
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	if e.StageIs(questplan.LcSpiritDefeated) {
		logf(d, "lost city: the Tree spirit is defeated")
		return true
	}
	logf(d, "lost city: the spirit is gone but the journal does not say it is defeated (another account's credit, or the fight not ours)")
	return false
}

// lcSpiritNpc is the Tree spirit in view.
func lcSpiritNpc(d *questrunner.Deps) (entityquery.Entity, bool) {
	return entityquery.Npcs(d.Session.State(), d.Config).Within(18).Where(func(n entityquery.Entity) bool { return n.ID == questplan.NpcLcTreeSpirit }).Nearest()
}

// lcFightSpirit casts at the spirit from the tree's stand and, when the casts end with it standing, finishes it by melee if the model says the swing
// can. It reports whether the spirit is gone.
func lcFightSpirit(e *questrunner.Engine, d *questrunner.Deps, sus questplan.Sustain) bool {
	cast := questplan.Step{Kind: questplan.StepFight, Target: "Tree spirit", TargetID: questplan.NpcLcTreeSpirit, Spell: "best", Anchor: questplan.LcTreeStand}
	if e.Execute(cast) {
		return true
	}
	sp, ok := lcSpiritNpc(d)
	if !ok {
		return true
	}
	hp, maxHP := questrunner.Hitpoints(d)
	if hp == 0 {
		return false
	}
	// The casts are over (the runes ran out, or the loop gave the fight up). The spirit's health bar says how much is left of its 85 hitpoints.
	left := questplan.FoeTreeSpirit.Hitpoints
	if sp.TotalHealth > 0 {
		left = max(1, questplan.FoeTreeSpirit.Hitpoints*sp.Health/sp.TotalHealth)
	}
	snap := questplan.Snapshot{Levels: questrunner.BaseLevels(d), HP: hp, MaxHP: maxHP}
	fighter := questplan.MeleeFighter{Attack: snap.Level("attack"), Strength: snap.Level("strength"), Defence: max(snap.Level("defence"), 1), Magic: snap.Level("magic")}
	bites := questplan.FoodBites(questrunner.PackCounts(d), sus.FoodIDs)
	line := questplan.FoeEatLine(sus.EatAt, questplan.FoeTreeSpirit.PeakHit(), maxHP)
	if ok, why := questplan.MeleeOdds(questplan.FoeTreeSpirit, fighter, hp, maxHP, left, bites, line); !ok {
		logf(d, "lost city: the casts are over with the spirit at about %d hitpoints and the swing cannot finish it: %s", left, why)
		questrunner.RetreatFromFight(d, "the Tree spirit", why)
		return false
	}
	melee := questplan.Step{Kind: questplan.StepFight, Target: "Tree spirit", TargetID: questplan.NpcLcTreeSpirit}
	return e.Execute(melee)
}

// lcBranch cuts a branch from the Dramen tree: it walks the corridor to the tree's stand at full hitpoints when the account is not there, and
// one "Chop down" puts a branch in the pack (a free slot) and, at stage 3, moves the stage to 4.
func lcBranch(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "lost city: no room in the pack for a branch")
		return false
	}
	if tile.Chebyshev(questrunner.CurrentTile(d), questplan.LcTreeStand) > 2 {
		if !lcCorridorReady(d) {
			e.RequestPark("under seven tenths of the hitpoints at the corridor of the greater demons and nothing to mend them with", questplan.LcItemParkFor)
			return false
		}
		if !lcGo(d, questplan.LcTreeStand, 0) && !lcGo(d, questplan.LcTreeStand, 1) {
			logf(d, "lost city: the Dramen tree's stand (%v) was not reached from %v", questplan.LcTreeStand, questrunner.CurrentTile(d))
			return false
		}
	}
	before := questrunner.HeldID(d, questplan.ObjLcBranch)
	m, ok := questrunner.FindLoc(d, questplan.LocLcDramenTree, "", "Chop down", questplan.LcTreeAt, 4)
	if !ok {
		logf(d, "lost city: no Dramen tree (loc %d) near %v", questplan.LocLcDramenTree, questplan.LcTreeAt)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Chop down"); err != nil {
		logf(d, "lost city: chopping the Dramen tree: %v", err)
		return false
	}
	if !waitUntil(d, lcTreeWait, func() bool { return questrunner.HeldID(d, questplan.ObjLcBranch) > before }) {
		logf(d, "lost city: no branch from the Dramen tree (the spirit is not defeated for this account, or the tree is not chopped from here)")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------------------------------------
// The way out, and Zanaris.
// ---------------------------------------------------------------------------------------------------------

// lcLeaveDungeon takes the account out of the dungeon and reports whether it is out. A teleport is the way (lcTeleportOut): the Varrock
// Teleport the kit carries the runes of, or the Lumbridge Teleport when the pack holds an earth rune, cast where the account stands, which
// spares it the corridor's second pass past the greater demons and puts it down in a city. Only an account that cannot cast one (no runes, too
// little Magic: nothing is sent) takes the Magic door and the walk out of the Wilderness it lands in (lcLeaveByTheDoor), which is the last
// resort and says so; an account whose cast was sent and did not land is not sent into the Wilderness, it is left for the next run (the
// engine parks the quest and the logs hold the server's reason). It is also the dungeon pocket's Leave.
func lcLeaveDungeon(d *questrunner.Deps) bool {
	if !questplan.InLcDungeon(questrunner.CurrentTile(d)) {
		return true
	}
	landed, castable := lcTeleportOut(d)
	if landed {
		return true
	}
	if castable {
		return false
	}
	logf(d, "lost city: no teleport can be cast out of the dungeon: the Magic door and the walk out of the Wilderness are the last resort")
	return lcLeaveByTheDoor(d)
}

// lcTeleportOut casts the nearest exit teleport the pack and the Magic level allow, from where the account stands, announced to the Guardian
// (the displacement is the account's own, not a random event). landed says the account is out of the dungeon. castable is false only when no
// teleport could be cast, so that nothing was sent: the one case the Magic door is the way left.
func lcTeleportOut(d *questrunner.Deps) (landed, castable bool) {
	out := func() bool { return !questplan.InLcDungeon(questrunner.CurrentTile(d)) }
	sent := false
	for try := 1; try <= lcCastTries; try++ {
		if out() {
			return true, true
		}
		pack, magic := questrunner.PackCounts(d), questrunner.BaseLevels(d)["magic"]
		exit, ok := questplan.LcExitFor(pack, magic)
		if !ok {
			if !sent {
				logf(d, "lost city: %s", questplan.LcExitRefusal(pack, magic))
				return false, false
			}
			// A cast that was sent has spent its runes and the landing has not been seen yet: it gets one more wait.
			logf(d, "lost city: the runes of the exit cast are spent and the account is still at %v: waiting for the landing", questrunner.CurrentTile(d))
			return waitUntil(d, lcCastWait, out), true
		}
		if !questrunner.WaitForGuardian(d) {
			return false, true
		}
		questrunner.ClearLeftoverModal(d)
		land := exit.Landing
		withdraw := randomevents.ExpectLandings(d.Session, "lost city: the "+exit.Name+" out of the Entrana dungeon", lcDialogueBudget, randomevents.LandingNear(land.X, land.Z, 3))
		at := time.Now()
		if err := actions.Teleport(d.Session, exit.Name); err != nil {
			withdraw()
			logf(d, "lost city: casting the %s: %v", exit.Name, err)
			return false, true
		}
		sent = true
		if waitUntil(d, lcCastWait, out) {
			logf(d, "lost city: out of the dungeon by the %s, at %v", exit.Name, questrunner.CurrentTile(d))
			return true, true
		}
		withdraw()
		said := ""
		if d.Chat != nil {
			said = strings.TrimSpace(d.Chat.Since(at))
		}
		logf(d, "lost city: the %s did not take the account out of the dungeon (try %d of %d, at %v): %q", exit.Name, try, lcCastTries, questrunner.CurrentTile(d), said)
	}
	return false, true
}

// lcLeaveByTheDoor is the dungeon's other way out, the last resort: back along the corridor to the Magic door (full hitpoints first), the door
// opened from the tile west of it, which sets the account down in the Wilderness at 3250,3772 (level 32: no teleport there, the walker's own walk
// from an endpoint inside the Wilderness takes it out). The jump is announced to the Guardian. It reports whether the account is out.
func lcLeaveByTheDoor(d *questrunner.Deps) bool {
	if !lcCorridorReady(d) {
		logf(d, "lost city: no way to walk the corridor to the Magic door at full strength")
	}
	if !lcGo(d, questplan.LcDoorStand, 0) && !lcGo(d, questplan.LcDoorStand, 1) {
		logf(d, "lost city: the Magic door's stand (%v) was not reached from %v", questplan.LcDoorStand, questrunner.CurrentTile(d))
		return false
	}
	// The demons that followed the run are some tiles behind: mend before the door and the Wilderness.
	if hp, max := questrunner.Hitpoints(d); max > 0 && hp < max {
		questrunner.TopUp(d, questplan.LostCity.Sustain.FoodIDs, max)
	}
	m, ok := questrunner.FindLoc(d, questplan.LocLcMagicDoor, "", "Open", questplan.LcDoorAt, 3)
	if !ok {
		logf(d, "lost city: no Magic door (loc %d) near %v", questplan.LocLcMagicDoor, questplan.LcDoorAt)
		return false
	}
	land := questplan.LcWildernessLanding
	withdraw := randomevents.ExpectLandings(d.Session, "lost city: the Entrana dungeon's Magic door into the Wilderness", lcDialogueBudget, randomevents.LandingNear(land.X, land.Z, 3))
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		withdraw()
		logf(d, "lost city: opening the Magic door: %v", err)
		return false
	}
	out := func() bool { return !questplan.InLcDungeon(questrunner.CurrentTile(d)) }
	if !waitUntil(d, lcArriveWait, out) {
		withdraw()
		questrunner.ClearLeftoverModal(d)
		logf(d, "lost city: the Magic door did not take the account out (at %v)", questrunner.CurrentTile(d))
		return false
	}
	logf(d, "lost city: out by the Magic door, at %v in the Wilderness", questrunner.CurrentTile(d))
	return true
}

// lcZanaris wears the Dramen staff, stands on the tile west of the shed's door and opens it: the door teleports an account that is NOT on
// the door's own column (check_axis) and wears the staff, and at stage 5 completes the quest in a queue. The walk starts wherever the account
// stands: Varrock or Lumbridge after the exit teleport, and the Wilderness only after the last resort (the walker's rule for an endpoint inside).
func lcZanaris(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	staff := questplan.ObjLcStaff
	if questrunner.HeldID(d, staff) == 0 && questrunner.WornCounts(d)[staff] == 0 {
		logf(d, "lost city: no Dramen staff in the pack or worn")
		return false
	}
	if !questrunner.Equip(d, staff) {
		logf(d, "lost city: the Dramen staff would not be worn")
		return false
	}
	out := questplan.LcShedOutside
	if !questrunner.Travel(d, out, 0) || questrunner.CurrentTile(d) != out {
		questrunner.WalkNear(d, out, 0)
	}
	if questrunner.CurrentTile(d) != out {
		logf(d, "lost city: the shed's door is opened from %v and the account stands at %v", out, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocLcShedDoor, "", "Open", questplan.LcShedDoorAt, 3)
	if !ok {
		logf(d, "lost city: no shed door (loc %d) near %v", questplan.LocLcShedDoor, questplan.LcShedDoorAt)
		return false
	}
	land := questplan.LcZanarisArrival
	withdraw := randomevents.ExpectLandings(d.Session, "lost city: the shed's door into Zanaris", lcDialogueBudget, randomevents.LandingNear(land.X, land.Z, 3))
	if err := questrunner.OperateLoc(d, m, "Open"); err != nil {
		withdraw()
		logf(d, "lost city: opening the shed's door: %v", err)
		return false
	}
	if !waitUntil(d, lcShedWait, func() bool { return questplan.InLcZanaris(questrunner.CurrentTile(d)) }) {
		withdraw()
		logf(d, "lost city: the shed's door did not take the account to Zanaris (at %v)", questrunner.CurrentTile(d))
		return false
	}
	// The quest-complete scroll comes a few ticks after the teleport (the queue): continued, and the tab is the proof.
	waitUntil(d, lcShedWait, func() bool { m, _, _ := d.Session.State().Modals(); return m != gamestate.NoModal || lcTabComplete(d) })
	questrunner.ClearLeftoverModal(d)
	return waitUntil(d, lcShedWait, func() bool { return lcTabComplete(d) })
}

func lcTabComplete(d *questrunner.Deps) bool {
	return questrunner.ReadStatus(d, questplan.LostCityName) == questplan.StatusComplete
}

// lcLeaveZanaris walks the account out of Zanaris to the shed's door (the entrance ladder is in the walk graph) once the quest is complete.
func lcLeaveZanaris(e *questrunner.Engine) {
	d := e.Deps()
	if !questplan.InLcZanaris(questrunner.CurrentTile(d)) {
		return
	}
	if !questrunner.Travel(d, questplan.LcShedOutside, 3) {
		logf(d, "lost city: the walk out of Zanaris to the shed (%v) did not end at it (at %v)", questplan.LcShedOutside, questrunner.CurrentTile(d))
		return
	}
	logf(d, "lost city: out of Zanaris by the entrance ladder, at %v", questrunner.CurrentTile(d))
}

// lcLeaveEntrana sails back to Port Sarim with an Entrana-side monk (monk_of_entrana.rs2 shipmonk2: "Do you wish to leave holy Entrana?", then
// "Yes, I'm ready to go."): the walk graph has no way off the island (the Holy Grail's lab run took the same way home), so an account the cave monk's
// ladder would not take down is not left on it for every later quest's walks to end unreachable. It reports whether the account is off the island.
func lcLeaveEntrana(d *questrunner.Deps) bool {
	off := func() bool { return !questplan.OnEntrana(questrunner.CurrentTile(d)) }
	if off() {
		return true
	}
	if !questrunner.Travel(d, questplan.EntranaDock, 4) {
		logf(d, "lost city: the Entrana dock (%v) was not reached from %v", questplan.EntranaDock, questrunner.CurrentTile(d))
		return false
	}
	monk := questplan.NpcStop{Npc: "Monk of Entrana", ID: questplan.NpcShipMonkEntrana, Anchor: questplan.EntranaDock, Leash: 8,
		Prefer: []string{"Yes, I'm ready to go."}}
	if !questrunner.OpenDialogue(d, monk) {
		return false
	}
	questrunner.NewDialogue(d).DriveUntil(off, monk.Prefer, lcSailBudget)
	questrunner.ClearLeftoverModal(d)
	return off()
}

// The dungeon under Entrana is a pocket for every quest's walks: no walk leaves it, and its way out is a teleport (the Magic door as the last
// resort). So is the island: the walk graph has no way off it either, and the way out is the monks' ship.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Entrana dungeon", In: questplan.InLcDungeon, Leave: lcLeaveDungeon})
	questrunner.RegisterPocket(questrunner.Pocket{Name: "holy Entrana", In: questplan.OnEntrana, Leave: lcLeaveEntrana})
}
