package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// Biohazard is the quest ready for the engine. The decision (questplan/biohazard.go) names a custom step for every leg because nearly
// every conversation of the quest holds a message box, a pause longer than a page turn or a scripted move: the shared driver
// (plagueDrive, Plague City's) carries a script's pages, menus and boxes to its goal, and each executor below is one leg on top of it:
// Elena and Jerico, the bird feed and the pigeons, Omart's ladder and Kilron's, the rotten apples and the cauldron, the nurse's
// cupboard, the Mourners' headquarters (the door, the stairs, the sick mourner and his key, the gate and the crate), the Chemist and his
// errand boys, Varrock's east gate, Guidor and King Lathas.
func Biohazard() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.Biohazard,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomBioStart:    plagueTalk(nil),
			questplan.CustomBioJerico:   plagueTalk(nil),
			questplan.CustomBioBirdfeed: bioBirdfeed,
			questplan.CustomBioFeed:     bioFeed,
			questplan.CustomBioPigeons:  bioPigeons,
			questplan.CustomBioRelease:  bioRelease,
			questplan.CustomBioCross:    bioCross,
			questplan.CustomBioApples:   bioApples,
			questplan.CustomBioStew:     bioStew,
			questplan.CustomBioGown:     bioGown,
			questplan.CustomBioHQ:       bioHQ,
			questplan.CustomBioUp:       bioUp,
			questplan.CustomBioKey:      bioKey,
			questplan.CustomBioCrate:    bioCrate,
			questplan.CustomBioLeaveHQ:  bioLeaveHQLeg,
			questplan.CustomBioHome:     bioHome,
			questplan.CustomBioHandBack: plagueTalk(func(d *questrunner.Deps) bool { return !held(d, questplan.BioObjDistillator) }),
			questplan.CustomBioReplace:  plagueTalk(bioHoldsTheKit),
			questplan.CustomBioChemist:  plagueTalk(func(d *questrunner.Deps) bool { return held(d, questplan.BioObjTouchPaper) }),
			questplan.CustomBioErrands:  bioErrands,
			questplan.CustomBioGateIn:   bioGateIn,
			questplan.CustomBioCollect:  bioCollect,
			questplan.CustomBioGuidor:   bioGuidor,
			questplan.CustomBioGateOut:  bioGateOut,
			questplan.CustomBioReport:   plagueTalk(nil),
			questplan.CustomBioLathas:   plagueTalk(nil),
		},
	}
}

func init() {
	// The Mourners' headquarters is a pocket inside West Ardougne's (Plague City's): its street door and the doors upstairs are the
	// scripts', no walk crosses them, and the first floor lies outside the boxes Plague City's pocket uses. An account that a run
	// ended in there (its budget, a death on the stairs' far side, a park) leaves by the door and then by Plague City's own way
	// home, whichever quest walks next.
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Mourners' headquarters", In: questplan.InBioHQ, Leave: bioPocketLeave})
}

// The times a leg waits.
const (
	// bioFightBudget bounds the fight with the sick mourner (19 hitpoints, a hit of 2): the priced fight is a minute or two.
	bioFightBudget = 3 * time.Minute
	// bioTakeBudget bounds the wait for a respawned apple or cage that other accounts took.
	bioTakeBudget = 4 * time.Minute
	// bioGateBudget bounds Varrock's gate: the guard's search is two dialogue pages and four three-tick pauses.
	bioGateBudget = 45 * time.Second
)

// bioAreaNow is where the account stands.
func bioAreaNow(d *questrunner.Deps) questplan.BioArea {
	return questplan.BioAreaOf(questrunner.CurrentTile(d))
}

// bioHunterRects are the rectangles a walk in West Ardougne keeps out of: the reach of every zombie and ghost of the city's west end.
func bioHunterRects() []webwalk.Rect {
	zones := questplan.BioHunterZones()
	out := make([]webwalk.Rect, 0, len(zones))
	for _, z := range zones {
		out = append(out, webwalk.Rect{MinX: z.MinX, MaxX: z.MaxX, MinZ: z.MinZ, MaxZ: z.MaxZ, Level: z.Level})
	}
	return out
}

// bioTravel is Travel, and for a walk inside West Ardougne's streets one planned round the hunters (the armed zombie's reach comes
// down to the lane the nurse's hut lets out on).
func bioTravel(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if bioAreaNow(d) == questplan.BioWest && questplan.BioAreaOf(dest) == questplan.BioWest {
		return questrunner.TravelAvoiding(d, dest, radius, bioHunterRects())
	}
	return questrunner.Travel(d, dest, radius)
}

// bioStage is the journal's stage as the engine last read it (a re-read when a step made it stale), false when it is not in progress.
func bioStage(e *questrunner.Engine) (int, bool) {
	for n := questplan.BioNotStarted; n < questplan.BioComplete; n++ {
		if e.StageIs(n) {
			return n, true
		}
	}
	return 0, false
}

// bioHoldsTheKit reports the sample and the three vials in the pack: what Elena's replacement is for.
func bioHoldsTheKit(d *questrunner.Deps) bool {
	if !held(d, questplan.BioObjSample) {
		return false
	}
	for _, v := range questplan.BioVials() {
		if !held(d, v) {
			return false
		}
	}
	return true
}

// bioWearGown puts the doctors' gown on (by exact id: the priest's two halves share a display name) and reports it worn.
func bioWearGown(d *questrunner.Deps) bool {
	if questrunner.WornCounts(d)[questplan.BioObjDoctorGown] > 0 {
		return true
	}
	if !held(d, questplan.BioObjDoctorGown) {
		logf(d, "biohazard: no doctors' gown to put on")
		return false
	}
	if !questrunner.Equip(d, questplan.BioObjDoctorGown) {
		logf(d, "biohazard: the doctors' gown would not go on")
		return false
	}
	return true
}

// bioCupboard opens a cupboard and searches it for an item (Jerico's and the nurse's: the shut form offers Open, the open form Search;
// the item comes with the search, in a message box or in a plain line).
func bioCupboard(d *questrunner.Deps, shut, open int, near tile.Tile, item int, what string) bool {
	if held(d, item) {
		return true
	}
	isOpen := func() bool {
		_, ok := questrunner.FindLoc(d, open, "", "Search", near, 3)
		return ok
	}
	if !isOpen() {
		if !plagueOperate(d, shut, "", "Open", near, 3) {
			return false
		}
		if !waitUntil(d, 8*time.Second, isOpen) {
			logf(d, "biohazard: %s did not open", what)
			return false
		}
	}
	if !plagueOperate(d, open, "", "Search", near, 3) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { return held(d, item) }, 25*time.Second)
	logf(d, "biohazard: searched %s: %v", what, ok)
	return ok
}

// bioTake picks an item up from one of its spawns, waiting for a respawn another account's pickup left empty.
func bioTake(d *questrunner.Deps, id int, spots []tile.Tile, budget time.Duration, what string) bool {
	if held(d, id) {
		return true
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "biohazard: no room for %s", what)
		return false
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		for _, spot := range spots {
			if !bioTravel(d, spot, 1) {
				logf(d, "biohazard: the walk to %s at %d,%d did not arrive", what, spot.X, spot.Z)
				continue
			}
			if questrunner.TakeGround(d, id, spot, 2) || held(d, id) {
				logf(d, "biohazard: took %s at %d,%d", what, spot.X, spot.Z)
				return true
			}
		}
		// Another account took it: the spawn comes back by itself.
		if !d.Delay(6 * time.Second) {
			return false
		}
	}
	logf(d, "biohazard: no %s in %s", what, budget)
	return false
}

// ---------------------------------------------------------------------------------------------------------------
// The east side: Jerico's cupboard, the tower, the pigeons.
// ---------------------------------------------------------------------------------------------------------------

// bioBirdfeed takes the bag of bird feed from Jerico's cupboard (it gives one only while neither the pack nor the bank holds one).
func bioBirdfeed(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.BioObjBirdfeed) {
		return true
	}
	return bioCupboard(d, questplan.BioLocJericoCupboardShut, questplan.BioLocJericoCupboardOpen, questplan.BioJericoCupboard,
		questplan.BioObjBirdfeed, "Jerico's cupboard")
}

// bioFeed throws the bag onto the watch tower (biowatchtower's oplocu: the bag deleted, two ticks, the stage moved). The journal moves
// after the bag has gone, so the leg waits the ticks out before it hands the next decision a stale stage.
func bioFeed(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !held(d, questplan.BioObjBirdfeed) {
		logf(d, "biohazard: no bird feed for the watch tower")
		return false
	}
	if !questrunner.Travel(d, questplan.BioWatchtower, 3) {
		logf(d, "biohazard: the walk to the watch tower did not arrive")
		return false
	}
	tower, ok := plagueLoc(d, questplan.BioLocWatchtower, "", "", questplan.BioWatchtower, 6)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.BioObjBirdfeed, tower); err != nil {
		logf(d, "biohazard: the bird feed on the watch tower: %v", err)
		return false
	}
	if !plagueDrive(d, nil, func() bool { return !held(d, questplan.BioObjBirdfeed) }, 20*time.Second) {
		logf(d, "biohazard: the watch tower took no bird feed")
		return false
	}
	d.Delay(3 * time.Second)
	return true
}

// bioPigeons takes a full cage from behind Jerico's house (three spawns, each back some time after a pickup).
func bioPigeons(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return bioTake(d, questplan.BioObjPigeons, questplan.BioPigeonSpawns, bioTakeBudget, "a pigeon cage")
}

// bioRelease opens the cage inside the box beside the tower (opheld1,pigeons: anywhere else the pigeons do not want to leave and the
// cage stays full).
func bioRelease(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !held(d, questplan.BioObjPigeons) {
		logf(d, "biohazard: no full pigeon cage to open")
		return false
	}
	if !questplan.BioPigeonZone.Contains(questrunner.CurrentTile(d)) {
		if !questrunner.Travel(d, bioPigeonStand, 1) {
			logf(d, "biohazard: the walk into the pigeon box did not arrive")
			return false
		}
	}
	if here := questrunner.CurrentTile(d); !questplan.BioPigeonZone.Contains(here) {
		logf(d, "biohazard: %d,%d is outside the pigeon box: the cage would not open", here.X, here.Z)
		return false
	}
	if err := questrunner.OpHeldByName(d, questplan.BioObjPigeons, "Open"); err != nil {
		logf(d, "biohazard: opening the pigeon cage: %v", err)
		return false
	}
	if !plagueDrive(d, nil, func() bool { return !held(d, questplan.BioObjPigeons) }, 25*time.Second) {
		logf(d, "biohazard: the pigeons stayed in the cage")
		return false
	}
	d.Delay(4 * time.Second)
	return true
}

// bioPigeonStand is a tile inside the box, east of the tower's wall.
var bioPigeonStand = tile.Tile{X: 2564, Z: 3303}

// ---------------------------------------------------------------------------------------------------------------
// The wall.
// ---------------------------------------------------------------------------------------------------------------

// bioCross is Omart's rope ladder, east to west: five beats of chat and pauses before the choice, and the climb itself.
func bioCross(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	west := func() bool { return bioAreaNow(d) == questplan.BioWest }
	if west() {
		return true
	}
	if bioAreaNow(d) != questplan.BioEast {
		logf(d, "biohazard: Omart's ladder is crossed from the mainland, and the account is in %s", bioAreaNow(d))
		return false
	}
	ok := plagueConverse(d, questplan.BioOmartStop, west, 90*time.Second)
	logf(d, "biohazard: over the wall with Omart: %v", ok)
	return west()
}

// bioHome brings the account west to east: out of the headquarters by its door, then Kilron's ladder (from stage 5 on, when he offers
// it), then, if he will not cross, Plague City's own way (the manhole, the sewer, the mud pile).
func bioHome(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	stage, known := bioStage(e)
	return bioGoEast(d, known && stage >= questplan.BioClimbedLadder)
}

// bioPocketLeave is the headquarters pocket's way out for a walk that is not this quest's: Kilron works from stage 5 on and the walk does
// not know the stage, so it goes by the manhole.
func bioPocketLeave(d *questrunner.Deps) bool { return bioGoEast(d, false) }

// bioGoEast walks the account out of the headquarters, the city, the sewer or a plague house to the mainland.
func bioGoEast(d *questrunner.Deps, kilron bool) bool {
	kilronFailed := !kilron
	for round := 0; round < 4; round++ {
		switch bioAreaNow(d) {
		case questplan.BioEast, questplan.BioQuarter:
			plagueSettle(d)
			return true
		case questplan.BioHQ, questplan.BioHQUp:
			if !bioLeaveHQ(d) {
				return false
			}
		case questplan.BioWest:
			if !kilronFailed {
				east := func() bool { return bioAreaNow(d) == questplan.BioEast }
				bioTravel(d, questplan.BioKilronAnchor, 2)
				plagueConverse(d, questplan.BioKilronStop, east, 90*time.Second)
				if east() {
					plagueSettle(d)
					return true
				}
				kilronFailed = true
				logf(d, "biohazard: Kilron would not cross: leaving by the manhole instead")
				continue
			}
			return plagueGoHome(d)
		default:
			return plagueGoHome(d)
		}
	}
	return bioAreaNow(d) == questplan.BioEast
}

// ---------------------------------------------------------------------------------------------------------------
// West Ardougne.
// ---------------------------------------------------------------------------------------------------------------

// bioApples takes the rotten apples from the ground at 2535,3333 (one spawn, back after a pickup).
func bioApples(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return bioTake(d, questplan.BioObjRottenApples, []tile.Tile{questplan.BioApples}, bioTakeBudget, "rotten apples")
}

// bioStew puts the apples into the Mourners' cauldron in the headquarters' yard, which the street reaches by the fence at 2541,3331
// (the planner has the hop). The apples are deleted whatever the stage; the stage moves three ticks later and the journal after it.
func bioStew(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !held(d, questplan.BioObjRottenApples) {
		logf(d, "biohazard: no rotten apples for the cauldron")
		return false
	}
	if !bioTravel(d, questplan.BioCauldronAt, 1) {
		logf(d, "biohazard: the walk to the cauldron (over the yard's fence) did not arrive")
		return false
	}
	cauldron, ok := plagueLoc(d, questplan.BioLocCauldron, "", "", questplan.BioCauldron, 6)
	if !ok {
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.BioObjRottenApples, cauldron); err != nil {
		logf(d, "biohazard: the apples on the cauldron: %v", err)
		return false
	}
	if !plagueDrive(d, nil, func() bool { return !held(d, questplan.BioObjRottenApples) }, 25*time.Second) {
		logf(d, "biohazard: the cauldron took no apples")
		return false
	}
	d.Delay(5 * time.Second)
	return true
}

// bioGown searches the nurse's cupboard in the hut at 2517,3276 for a doctors' gown (from stage 6, and only while no gown is banked).
func bioGown(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.BioObjDoctorGown) || questrunner.WornCounts(d)[questplan.BioObjDoctorGown] > 0 {
		return true
	}
	// The hut is reached by the lane south of the armed zombie's reach: the cupboard's own approach is a short step from there.
	bioTravel(d, bioNurseStand, 1)
	return bioCupboard(d, questplan.BioLocNurseCupboardShut, questplan.BioLocNurseCupboardOpen, questplan.BioNurseCupboard,
		questplan.BioObjDoctorGown, "the nurse's cupboard")
}

// bioNurseStand is the tile in front of the nurse's cupboard (the cupboard is searched from the south of it).
var bioNurseStand = tile.Tile{X: 2517, Z: 3275}

// bioHQ puts the gown on and goes in by the street door ("In you go doc.", the stages 6 and 7 only). The door's script moves the
// account through when the chat box has been clicked on: it is operated from the street side, which the walk to its own tile is.
func bioHQ(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	switch bioAreaNow(d) {
	case questplan.BioHQ, questplan.BioHQUp:
		return true
	case questplan.BioWest:
	default:
		logf(d, "biohazard: the headquarters' door is reached from West Ardougne, and the account is in %s", bioAreaNow(d))
		return false
	}
	if !bioWearGown(d) {
		return false
	}
	if !bioTravel(d, questplan.BioHQDoorOutside, 0) {
		logf(d, "biohazard: the walk to the headquarters' door did not arrive")
		return false
	}
	if !plagueOperate(d, questplan.BioLocHQDoor, "", "Open", questplan.BioHQDoorOutside, 3) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { return bioAreaNow(d) == questplan.BioHQ }, 30*time.Second)
	logf(d, "biohazard: in by the headquarters' door: %v", ok)
	return ok
}

// bioUp crosses the dining hall (the mourners at dinner are never talked to) and climbs to the sick mourners' room: the planner has the
// hall's inner door, the stairs and the door upstairs, which opens for a doctor.
func bioUp(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	switch bioAreaNow(d) {
	case questplan.BioHQUp:
		return true
	case questplan.BioHQ:
	default:
		logf(d, "biohazard: the stairs are in the headquarters, and the account is in %s", bioAreaNow(d))
		return false
	}
	if !bioWearGown(d) {
		return false
	}
	if !questrunner.Travel(d, questplan.BioSickRoom, 1) {
		logf(d, "biohazard: the walk up to the sick mourners' room did not arrive")
	}
	return bioAreaNow(d) == questplan.BioHQUp
}

// bioSickMourners are the sick mourners in view, nearest first, that no other player is fighting.
func bioSickMourners(d *questrunner.Deps) []entityquery.Entity {
	self := d.Session.State().Self()
	var out []entityquery.Entity
	for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Within(12).Results() {
		if n.ID == questplan.NpcSickMourner && !goblinBusy(n, self) {
			out = append(out, n)
		}
	}
	return out
}

// bioKey is the one fight of the quest. At stage 6 the account in the gown talks to a sick mourner and answers "There's nothing I can
// do, it's fatal.": every answer ends with "You're no doctor!" and the mourner turns on the account, which is what makes his death
// drop the key to it. The account is topped up first (a chat cannot be eaten in), fights the mourner that is hitting it by name through
// the shared fight step (never the nearest of a kind, never by distance) and takes the key from his death (the script adds it to the pack
// after the death animation). Nothing but this mourner is talked to or struck; the others at dinner downstairs and at the doors are left
// alone. At any other stage the mourner does not talk and is attacked outright, which may or may not turn the key over.
func bioKey(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.BioObjKey) {
		return true
	}
	if bioAreaNow(d) != questplan.BioHQUp {
		logf(d, "biohazard: the sick mourners are upstairs, and the account is in %s", bioAreaNow(d))
		return false
	}
	if questrunner.FreeSlots(d) == 0 {
		logf(d, "biohazard: no room in the pack for the key")
		return false
	}
	if !bioWearGown(d) {
		return false
	}
	sus := questplan.Biohazard.Sustain
	questrunner.PrepareForFight(d, sus.FoodIDs, questplan.FoeSickMourner.PeakHit())
	stage, _ := bioStage(e)
	if stage == questplan.BioPoisonedStew {
		stop := questplan.BioMournerStop
		if len(bioSickMourners(d)) == 0 {
			logf(d, "biohazard: no sick mourner free to talk to (the other account's, or not yet back)")
			return false
		}
		if !questrunner.OpenDialogue(d, stop) {
			logf(d, "biohazard: the sick mourner opened no conversation")
			return false
		}
		plagueDrive(d, stop.Prefer, nil, plagueTalkBudget)
	}
	foe, ok := questrunner.Attacker(d)
	if !ok || foe.ID != questplan.NpcSickMourner {
		// The talk did not turn him (or the stage does not talk): the nearest free sick mourner, struck outright.
		ms := bioSickMourners(d)
		if len(ms) == 0 {
			logf(d, "biohazard: no sick mourner to fight")
			return false
		}
		foe = ms[0]
	}
	began := time.Now()
	giveUp := func() bool {
		cur, found := entityquery.Npcs(d.Session.State(), d.Config).Within(14).
			Where(func(n entityquery.Entity) bool { return n.Index == foe.Index && n.ID == foe.ID }).Nearest()
		return found && time.Since(began) >= 8*time.Second && goblinBusy(cur, d.Session.State().Self())
	}
	res := questrunner.FightChosen(d, foe, sus, bioFightBudget, giveUp)
	logf(d, "biohazard: the fight with the sick mourner at %d,%d ended %v", foe.X, foe.Z, res)
	if res == questrunner.FightLost {
		return false
	}
	got := waitUntil(d, 12*time.Second, func() bool { return held(d, questplan.BioObjKey) })
	logf(d, "biohazard: the key from the sick mourner: %v", got)
	return got
}

// bioCrate takes the key to the double gate beside the sick mourners (the key used on a leaf: "The key fits the gate.", and the script
// moves the account through) and searches the crate behind it for Elena's distillator.
func bioCrate(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if held(d, questplan.BioObjDistillator) {
		return true
	}
	if bioAreaNow(d) != questplan.BioHQUp {
		logf(d, "biohazard: the crate is upstairs in the headquarters, and the account is in %s", bioAreaNow(d))
		return false
	}
	if !held(d, questplan.BioObjKey) {
		logf(d, "biohazard: no key in the pack: the gate refuses an empty-handed Open")
		return false
	}
	if questrunner.CurrentTile(d).X < questplan.BioGate.X+1 {
		if !questrunner.Travel(d, questplan.BioGate, 0) {
			logf(d, "biohazard: the walk to the gate did not arrive")
			return false
		}
		gate, ok := plagueLoc(d, questplan.BioLocHQGateLeft, "", "Open", questplan.BioGate, 3)
		if !ok {
			return false
		}
		if err := questrunner.UseItemOnLoc(d, questplan.BioObjKey, gate); err != nil {
			logf(d, "biohazard: the key on the gate: %v", err)
			return false
		}
		if !plagueDrive(d, nil, func() bool { return questrunner.CurrentTile(d).X > questplan.BioGate.X }, 25*time.Second) {
			logf(d, "biohazard: the gate did not let the account through")
			return false
		}
	}
	if !questrunner.Travel(d, questplan.BioCrateStand, 0) {
		logf(d, "biohazard: the walk to the crate did not arrive")
		return false
	}
	if !plagueOperate(d, questplan.BioLocHQCrate, "", "Search", questplan.BioCrate, 3) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { return held(d, questplan.BioObjDistillator) }, 25*time.Second)
	logf(d, "biohazard: Elena's distillator from the crate: %v", ok)
	return ok
}

// bioLeaveHQLeg is the leg that brings the account out of the headquarters to the street.
func bioLeaveHQLeg(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return bioLeaveHQ(d)
}

// bioLeaveHQ walks the account to the inside of the street door (the planner has the gate, the door upstairs, the stairs and the hall's
// inner door) and opens it from inside, where the script lets anyone out and puts them on the street.
func bioLeaveHQ(d *questrunner.Deps) bool {
	if a := bioAreaNow(d); a != questplan.BioHQ && a != questplan.BioHQUp {
		return true
	}
	if !questrunner.Travel(d, questplan.BioHQDoorInside, 0) {
		logf(d, "biohazard: the walk to the inside of the headquarters' door did not arrive")
	}
	if a := bioAreaNow(d); a != questplan.BioHQ {
		return a != questplan.BioHQUp
	}
	if !plagueOperate(d, questplan.BioLocHQDoor, "", "Open", questplan.BioHQDoorOutside, 3) {
		return false
	}
	ok := plagueDrive(d, nil, func() bool { a := bioAreaNow(d); return a != questplan.BioHQ && a != questplan.BioHQUp }, 25*time.Second)
	logf(d, "biohazard: out by the headquarters' door: %v", ok)
	return ok
}

// ---------------------------------------------------------------------------------------------------------------
// Rimmington and Varrock.
// ---------------------------------------------------------------------------------------------------------------

// bioErrands hands each vial still in the pack to the boy it is safe with (Hops the broline, DeVinci the ethenea, Chancy the honey). A boy
// who already holds a vial ("I'm not taking two") leaves the extra one in the pack, which the gate takes, and the leg succeeds if any
// boy took one.
func bioErrands(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	given, wanted := 0, 0
	for _, er := range questplan.BioErrands {
		if !held(d, er.Vial) {
			continue
		}
		wanted++
		plagueConverse(d, er.Rim, func() bool { return !held(d, er.Vial) }, plagueTalkBudget)
		if held(d, er.Vial) {
			logf(d, "biohazard: %s would not take the vial of %s", er.Name, er.VialName)
			continue
		}
		given++
		logf(d, "biohazard: %s is carrying the vial of %s to Varrock", er.Name, er.VialName)
	}
	return wanted == 0 || given > 0
}

// bioGateIn is Varrock's east gate, inward: from the tile outside the northern leaf, so that the guard beside it searches (stages 10 to
// 13; the account carries no vial) and the script puts the account on the gate's own tile, from which it steps inside.
func bioGateIn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if bioAreaNow(d) == questplan.BioQuarter {
		return true
	}
	if bioAreaNow(d) != questplan.BioEast {
		logf(d, "biohazard: Varrock's east gate is entered from the mainland, and the account is in %s", bioAreaNow(d))
		return false
	}
	for _, v := range questplan.BioVials() {
		if held(d, v) {
			logf(d, "biohazard: the gate guard will take the vial of %s the account still carries", questrunner.ObjName(d, v))
		}
	}
	if !questrunner.Travel(d, questplan.BioGateOutside, 0) || !plagueStandOn(d, questplan.BioGateOutside) {
		logf(d, "biohazard: could not stand outside the gate")
		return false
	}
	gate, ok := plagueLoc(d, questplan.BioLocVarrockGateB, "", "Open", questplan.BioGateOutside, 3)
	if !ok {
		return false
	}
	if err := questrunner.OperateLoc(d, gate, "Open"); err != nil {
		logf(d, "biohazard: the gate: %v", err)
		return false
	}
	plagueDrive(d, nil, func() bool { return questrunner.CurrentTile(d).X >= questplan.BioGateInside.X-1 }, bioGateBudget)
	if questrunner.CurrentTile(d).X < questplan.BioGateInside.X-1 {
		logf(d, "biohazard: the gate did not let the account in")
		return false
	}
	if !questrunner.Travel(d, questplan.BioGateInside, 0) {
		logf(d, "biohazard: the step inside the gate did not arrive")
	}
	in := bioAreaNow(d) == questplan.BioQuarter
	logf(d, "biohazard: through Varrock's east gate: %v", in)
	return in
}

// bioGateOut is the gate, outward: from the leaf's own tile, which is where the planner's hop and the script both stand a player (from
// there the script puts the account on the street without the guard's search).
func bioGateOut(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if bioAreaNow(d) != questplan.BioQuarter {
		return true
	}
	for try := 0; try < 3; try++ {
		gate, ok := plagueLoc(d, questplan.BioLocVarrockGateB, "", "Open", questplan.BioGateInside, 3)
		if !ok {
			return false
		}
		if err := questrunner.OperateLoc(d, gate, "Open"); err != nil {
			logf(d, "biohazard: the gate: %v", err)
			return false
		}
		plagueDrive(d, nil, func() bool { return bioAreaNow(d) != questplan.BioQuarter }, bioGateBudget)
		if bioAreaNow(d) != questplan.BioQuarter {
			break
		}
	}
	out := bioAreaNow(d) != questplan.BioQuarter
	logf(d, "biohazard: out of Varrock's east quarter: %v", out)
	return out
}

// bioCollect takes the vials back from the three boys at the Dancing Donkey (a boy who spoilt his vial opens a menu whose two lines
// are harmless, and gives nothing back). A boy who is out of reach or in the middle of a wander is asked again, up to three sweeps of
// the inn, before the leg gives the vial up for lost (which sends the account to Elena for a new one).
func bioCollect(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for sweep := 0; sweep < bioCollectSweeps && len(missingVials(d)) > 0; sweep++ {
		if sweep > 0 && !d.Delay(5*time.Second) {
			break
		}
		for _, er := range questplan.BioErrands {
			if held(d, er.Vial) {
				continue
			}
			er := er
			plagueConverse(d, er.Var, func() bool { return held(d, er.Vial) }, plagueTalkBudget)
			if held(d, er.Vial) {
				logf(d, "biohazard: %s gave the vial of %s back", er.Name, er.VialName)
			} else {
				logf(d, "biohazard: %s did not hand back the vial of %s (sweep %d of %d)", er.Name, er.VialName, sweep+1, bioCollectSweeps)
			}
		}
	}
	return len(missingVials(d)) == 0
}

// bioCollectSweeps is how many times the leg walks the inn asking for the vials still missing.
const bioCollectSweeps = 3

// missingVials lists the vials not in the pack.
func missingVials(d *questrunner.Deps) []int {
	var out []int
	for _, v := range questplan.BioVials() {
		if !held(d, v) {
			out = append(out, v)
		}
	}
	return out
}

// bioGuidor puts on the priest's gown and robe (by id), goes in at Guidor's door from its own tile (his wife lets a priest in; the
// door's script moves the account through) and gives Guidor the vials, the sample and the touch paper.
func bioGuidor(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for _, half := range []int{questplan.BioObjPriestGown, questplan.BioObjPriestRobe} {
		if !held(d, half) && questrunner.WornCounts(d)[half] == 0 {
			logf(d, "biohazard: no priest's garment %d to put on", half)
			return false
		}
		if !questrunner.Equip(d, half) {
			logf(d, "biohazard: the priest's garment %d would not go on", half)
			return false
		}
	}
	room := func() bool { return questplan.InBioGuidorsRoom(questrunner.CurrentTile(d)) }
	if !room() {
		if !questrunner.Travel(d, questplan.BioGuidorDoor, 0) || !plagueStandOn(d, questplan.BioGuidorDoor) {
			logf(d, "biohazard: could not stand at Guidor's door")
			return false
		}
		if !plagueOperate(d, questplan.BioLocGuidorDoor, "", "Open", questplan.BioGuidorDoor, 3) {
			return false
		}
		plagueDrive(d, nil, room, 30*time.Second)
		if !room() {
			logf(d, "biohazard: Guidor's wife did not let the account in")
			return false
		}
	}
	plagueConverse(d, questplan.BioGuidorStop, func() bool { return !held(d, questplan.BioObjSample) }, plagueTalkBudget)
	e.InvalidateStage()
	return e.StageIs(questplan.BioFoundSecret)
}
