package questmods

import (
	"sync"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// JunglePotion is Jungle Potion ready for the engine. Its supplies pass is the provisioner's kit and its cleaning of the herbs a
// generic Identify; the customs are the parts the content and the island make special: the four talks to Trufitus and the pick of
// each herb, which are walks round the jungle's spiders and tribesmen (questplan.JungleAvoid) and, for the rogues purse, a run
// through the pothole cave and its jogres.
func JunglePotion() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.JunglePotion,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomJungleStart: func(e *questrunner.Engine, _ questplan.Step) bool {
				// The tab leaves red as soon as the job is taken.
				return jungleTalk(e, questplan.TrufitusStartStop, func(d *questrunner.Deps) bool {
					return waitUntil(d, jungleTabWait, func() bool {
						return questrunner.ReadStatus(d, questplan.JunglePotionName) != questplan.StatusNotStarted
					})
				})
			},
			questplan.CustomJungleHandIn: func(e *questrunner.Engine, step questplan.Step) bool {
				// A herb Trufitus took is out of the pack; one he refused (not fresh, or not the one he asked for) is still in it.
				if step.Qty < 0 || step.Qty >= len(questplan.JungleHerbs) {
					return false
				}
				herb := questplan.JungleHerbs[step.Qty]
				return jungleTalk(e, questplan.TrufitusHandInStop, func(d *questrunner.Deps) bool {
					return waitUntil(d, jungleTabWait, func() bool { return questrunner.HeldID(d, herb.Clean) == 0 })
				})
			},
			questplan.CustomJungleFinish: func(e *questrunner.Engine, _ questplan.Step) bool {
				// The closing queue turns the tab green a few ticks after the message box.
				return jungleTalk(e, questplan.TrufitusHandInStop, func(d *questrunner.Deps) bool {
					return waitUntil(d, 2*jungleTabWait, func() bool {
						return questrunner.ReadStatus(d, questplan.JunglePotionName) == questplan.StatusComplete
					})
				})
			},
			questplan.CustomJunglePick: junglePick,
		},
	}
}

const (
	// jungleTabWait is how long a conversation's result (the tab's colour, the herb gone from the pack) gets to show.
	jungleTabWait = 6 * time.Second
	// jungleHerbWait is how long a pick waits for a shared loc another account has just picked to grow its herb back: a hundred
	// ticks (a minute), with the quest's slot admitting three accounts.
	jungleHerbWait = 3 * time.Minute
	// jungleSearchWait is how long a search gets to put the herb in the pack.
	jungleSearchWait = 8 * time.Second
	// jungleCaveBudget bounds the run from the cave's foot to the wall; jungleCaveLeaveBudget the walk back to the hand holds.
	jungleCaveBudget = 3 * time.Minute
	// jungleDialogueBudget bounds the message boxes and the menu of the Rocks and the Hand holds.
	jungleDialogueBudget = 30 * time.Second
	// jungleArriveWait is how long the Rocks and the Hand holds get to set the account down.
	jungleArriveWait = 12 * time.Second
)

// jungleAvoidRects are questplan.JungleAvoid as the walker's rectangles, made once.
var jungleAvoidRects = sync.OnceValue(func() []webwalk.Rect {
	var out []webwalk.Rect
	for _, r := range questplan.JungleAvoid() {
		out = append(out, webwalk.Rect{MinX: r.MinX, MaxX: r.MaxX, MinZ: r.MinZ, MaxZ: r.MaxZ, Level: r.Level})
	}
	return out
})

// jungleGo walks to within radius of dest keeping out of the spiders' and tribesmen's ground. The ships have fares: the same
// pre-walk step Travel takes (Deps.BeforeTravel, which ScarabSwarm uses to draw the fares or cast a teleport) runs first, and the
// avoiding walk is the one TravelAvoiding makes. A walk that cannot be made round the hunters is the caller's to park on; none of
// the island's walks falls back to the plain one.
func jungleGo(d *questrunner.Deps, dest tile.Tile, radius int) bool {
	if here, ok := questrunner.CurrentTileOK(d); ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	if d.BeforeTravel != nil {
		d.BeforeTravel(dest)
	}
	return questrunner.TravelAvoiding(d, dest, radius, jungleAvoidRects())
}

// jungleTalk walks to Trufitus round the hunters and has the conversation the stop names: the start (three menus), a hand-in
// ("Of course!") or the finish (a message box and the closing queue). It reports whether the conversation did what it was for
// (settled: the tab's colour, the herb gone from the pack): a talk that ended with Trufitus refusing, or a walk that could not be
// made, fails the step, and the Decide parks the quest on it instead of asking again at once.
func jungleTalk(e *questrunner.Engine, stop questplan.NpcStop, settled func(d *questrunner.Deps) bool) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !jungleGo(d, stop.Anchor, 3) {
		logf(d, "jungle: Trufitus (%v) was not reached from %v", stop.Anchor, questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.GotoNpc(d, stop, nil) {
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	ok := questrunner.NewDialogue(d).Drive(stop.Prefer)
	// A closing message box or a quest-complete scroll left up blocks every later click.
	questrunner.ClearLeftoverModal(d)
	if !settled(d) {
		logf(d, "jungle: the talk with Trufitus (driven to its end: %v) did not do what it was for", ok)
		return false
	}
	return true
}

// junglePick brings the unidentified herb the step names (Qty is its index) into the pack. A herb of the surface is searched at
// its loc, waiting for the loc to grow it back when another account has picked it; the rogues purse is in the cave. The pick is
// proven by the herb in the pack, the only evidence there is (the stage is not transmitted).
func junglePick(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if step.Qty < 0 || step.Qty >= len(questplan.JungleHerbs) {
		logf(d, "jungle: no herb numbered %d", step.Qty)
		return false
	}
	herb := questplan.JungleHerbs[step.Qty]
	if questrunner.HeldID(d, herb.Unid) > 0 {
		return true
	}
	if herb.Underground {
		return jungleCave(e, d, herb)
	}
	if questplan.InJungleCave(questrunner.CurrentTile(d)) && !jungleLeaveCave(d) {
		return false
	}
	if !jungleGo(d, herb.Stand, 0) && !jungleGo(d, herb.Stand, 1) {
		logf(d, "jungle: the %s (%v) was not reached from %v round the spiders and tribesmen", herb.Name, herb.Stand, questrunner.CurrentTile(d))
		return false
	}
	if jungleSearch(d, herb) {
		return true
	}
	e.RequestPark("no "+herb.Name+" to search for in "+jungleHerbWait.String()+" (another account picks the loc every hundred ticks, or the loc is not where the plan says)", questplan.JungleLegPark)
	return false
}

// jungleSearch searches the herb's loc until the unidentified herb is in the pack: the full loc is operated, and when it is the
// empty form (an account picked it, back in a hundred ticks) the search waits for it.
func jungleSearch(d *questrunner.Deps, herb questplan.JungleHerb) bool {
	deadline := time.Now().Add(jungleHerbWait)
	for time.Now().Before(deadline) {
		if questrunner.HeldID(d, herb.Unid) > 0 {
			return true
		}
		m, ok := questrunner.FindLoc(d, herb.Loc, "", "Search", herb.At, 3)
		if !ok {
			if !d.Delay(3 * time.Second) {
				return false
			}
			continue
		}
		if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
			logf(d, "jungle: searching for the %s: %v", herb.Name, err)
			if !d.Delay(2 * time.Second) {
				return false
			}
			continue
		}
		if waitUntil(d, jungleSearchWait, func() bool { return questrunner.HeldID(d, herb.Unid) > 0 }) {
			// The find raises an object box that waits for its button; the herb and the stage are already in.
			questrunner.ClearLeftoverModal(d)
			logf(d, "jungle: picked the %s at %d,%d", herb.Name, m.X, m.Z)
			return true
		}
	}
	return questrunner.HeldID(d, herb.Unid) > 0
}

// jungleCave brings the rogues purse out of the cave under the Rocks: the walk to the Rocks round the hunters, a top-up to full
// hitpoints, the Rocks, the run to the wall, the search and the Hand holds out again. The cave's jogres are run past, not fought
// (Module.NoRetaliate); the Decide priced the run before the account left the mainland. Whatever happens in the cave, the account
// climbs out again before the step returns, unless the climb itself fails.
func jungleCave(e *questrunner.Engine, d *questrunner.Deps, herb questplan.JungleHerb) bool {
	sus := questplan.JunglePotion.Sustain
	if !questplan.InJungleCave(questrunner.CurrentTile(d)) {
		if !jungleGo(d, questplan.JunglePotholeStand, 1) {
			logf(d, "jungle: the Rocks (%v) were not reached from %v round the spiders and tribesmen", questplan.JunglePotholeStand, questrunner.CurrentTile(d))
			return false
		}
		if hp, max := questrunner.Hitpoints(d); max > 0 {
			if hp < max {
				questrunner.TopUp(d, sus.FoodIDs, max)
			}
			if hp, max = questrunner.Hitpoints(d); hp*10 < max*7 {
				e.RequestPark("under seven tenths of the hitpoints at the cave of the jogres and no food to mend them with", questplan.JungleOddsPark)
				return false
			}
		}
		if !jungleEnterCave(d) {
			e.RequestPark("the Rocks north of the village did not take the account into the cave", questplan.JungleLegPark)
			return false
		}
	}
	got := questrunner.HeldID(d, herb.Unid) > 0
	if !got {
		if questrunner.TravelFor(d, herb.Stand, 0, jungleCaveBudget) || questrunner.TravelFor(d, herb.Stand, 1, jungleCaveBudget/2) {
			got = jungleSearch(d, herb)
		} else {
			logf(d, "jungle: the wall (%v) was not reached from %v", herb.Stand, questrunner.CurrentTile(d))
		}
	}
	out := jungleLeaveCave(d)
	if !got {
		e.RequestPark("no rogues purse from the wall of the cave (the run did not reach it or the search found nothing)", questplan.JungleLegPark)
	}
	return got && (out || !questplan.InJungleCave(questrunner.CurrentTile(d)))
}

// jungleEnterCave searches the Rocks: a message box, the menu ("Yes, I'll enter the cave.") and another message box, then the
// account is set down in the cave. It reports whether the account is in it.
func jungleEnterCave(d *questrunner.Deps) bool {
	if questplan.InJungleCave(questrunner.CurrentTile(d)) {
		return true
	}
	m, ok := questrunner.FindLoc(d, questplan.LocJunglePothole, "", "Search", questplan.JunglePotholeStand, 6)
	if !ok {
		logf(d, "jungle: no Rocks (loc %d) near %v", questplan.LocJunglePothole, questplan.JunglePotholeStand)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Search"); err != nil {
		logf(d, "jungle: searching the Rocks: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	inCave := func() bool { return questplan.InJungleCave(questrunner.CurrentTile(d)) }
	dl.DriveUntil(inCave, []string{"Yes, I'll enter the cave."}, jungleDialogueBudget)
	if waitUntil(d, jungleArriveWait, inCave) {
		logf(d, "jungle: down into the cave under the Rocks")
		return true
	}
	questrunner.ClearLeftoverModal(d)
	logf(d, "jungle: the Rocks did not take the account into the cave (at %v)", questrunner.CurrentTile(d))
	return false
}

// jungleLeaveCave climbs the Hand holds from the only tile they are climbed from and reports whether the account is out. It is
// also the cave's pocket: the walk graph has no edge for the Hand holds, so no walk leaves the cave.
func jungleLeaveCave(d *questrunner.Deps) bool {
	if !questplan.InJungleCave(questrunner.CurrentTile(d)) {
		return true
	}
	if !questrunner.TravelFor(d, questplan.JungleHandHoldsStand, 0, jungleCaveBudget) {
		logf(d, "jungle: the Hand holds (%v) were not reached from %v", questplan.JungleHandHoldsStand, questrunner.CurrentTile(d))
	}
	m, ok := questrunner.FindLoc(d, questplan.LocJungleHandHolds, "", "Climb", questplan.JungleHandHoldsStand, 3)
	if !ok {
		logf(d, "jungle: no Hand holds (loc %d) near %v", questplan.LocJungleHandHolds, questplan.JungleHandHoldsStand)
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Climb"); err != nil {
		logf(d, "jungle: climbing the Hand holds: %v", err)
		return false
	}
	out := func() bool { return !questplan.InJungleCave(questrunner.CurrentTile(d)) }
	questrunner.NewDialogue(d).DriveUntil(out, nil, jungleDialogueBudget)
	if waitUntil(d, jungleArriveWait, out) {
		logf(d, "jungle: up the Hand holds, out of the cave")
		return true
	}
	questrunner.ClearLeftoverModal(d)
	logf(d, "jungle: the Hand holds did not take the account out of the cave (at %v)", questrunner.CurrentTile(d))
	return false
}

// The cave under the Rocks is a pocket for every quest's walks.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Karamja pothole cave", In: questplan.InJungleCave, Leave: jungleLeaveCave})
}
