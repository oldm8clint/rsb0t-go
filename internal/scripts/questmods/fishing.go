package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// FishingContest is the Fishing Contest ready for the engine. Its talks (the Mountain Dwarf, the spare pass) and its provisioning
// (the rod and the spade) are the engine's generic steps; the garlic from Morgan's cupboard is Vampire Slayer's own custom. The
// customs below are what has no generic step: the gate (a conversation inbound, a prompt outbound), the garlic in the wall pipe, the
// worms dug in a wood of guard dogs, the fee and the seating, the casts at the pipes and Bonzo's spare trophy.
func FishingContest() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.FishingContest,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomFishGarlic: vampireGarlic,
			questplan.CustomFishDig:    fishDig,
			questplan.CustomFishPay:    fishPay,
			questplan.CustomFishStash:  fishStash,
			questplan.CustomFishLeave:  fishLeave,
			questplan.CustomFishFish:   fishFish,
			questplan.CustomFishSpare:  fishSpare,
		},
	}
}

const (
	// fishGateWait bounds one crossing of the gate: Morris's three pages and his object box, or Bonzo's prompt.
	fishGateWait = 40 * time.Second
	// fishStashWait is how long the pipe gets to take the garlic: the script waits two ticks first.
	fishStashWait = 10 * time.Second
	// fishCastWait is how long one cast gets to land: four ticks of delay and the catch.
	fishCastWait = 12 * time.Second
	// fishJudgeWait bounds Bonzo's judgement after the third catch: his pages, a message box and the trophy's object box.
	fishJudgeWait = 40 * time.Second
	// fishCasts is the most casts one run makes: three win the contest, the rest are spare for a cast the server refused.
	fishCasts = 6
	// fishDigWait is how long one dig gets to give a worm.
	fishDigWait = 8 * time.Second
	// fishDigSkip is how long a vine that gave nothing is left alone.
	fishDigSkip = 20 * time.Second
	// fishVineRadius is how far from the patch's stand a vine may be and still be dug.
	fishVineRadius = 4
	// fishRetryPark is how long a step that learned it cannot work now is left alone.
	fishRetryPark = 30 * time.Minute
)

// The Hemenster competition ground is a pocket for every quest's walks: a walk from inside to anywhere outside would plan through
// the gate's door edge, and the generic door handler can neither drive Morris's conversation inbound nor answer Bonzo's prompt
// outbound. Leaving is the gate's own crossing, with the prompt's reset chosen when it comes.
func init() {
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the Hemenster competition ground", In: questplan.InFishingCompound,
		Leave: func(d *questrunner.Deps) bool { return fishCross(d, false) }})
}

// fishGate finds the gate's left half (the only one that lets a player out at stage 1 without Bonzo's prompt), else the right half.
func fishGate(d *questrunner.Deps, near tile.Tile) (locindex.Match, bool) {
	if m, ok := questrunner.FindLoc(d, questplan.LocHemensterGateLeft, "", "Open", near, 4); ok {
		return m, true
	}
	return questrunner.FindLoc(d, questplan.LocHemensterGateRight, "", "Open", near, 4)
}

// fishCross goes through the gate: in (Morris asks for the pass and teleports the account onto the gate tile) or out (Bonzo asks
// whether the account is calling it quits when a contest is running, and "Yes I'll compete again another day." resets it; at every
// other stage the gate just opens). It reports whether the account is on the wanted side.
func fishCross(d *questrunner.Deps, in bool) bool {
	inside := func() bool { return questplan.InFishingCompound(questrunner.CurrentTile(d)) }
	if inside() == in {
		return true
	}
	stand := questplan.FcGateOutside
	var prefer []string
	if !in {
		stand = questplan.FcGateInside
		prefer = []string{questplan.FcQuitContest}
	} else if questrunner.HeldID(d, questplan.ObjFishingPass) == 0 {
		logf(d, "fishing: Morris wants the competition pass and the pack holds none")
		return false
	}
	if !questrunner.Travel(d, stand, 0) {
		logf(d, "fishing: the gate's %s (%v) was not reached from %v", map[bool]string{true: "outside", false: "inside"}[in], stand, questrunner.CurrentTile(d))
		return false
	}
	gate, ok := fishGate(d, stand)
	if !ok {
		logf(d, "fishing: no Hemenster gate near %v", stand)
		return false
	}
	if err := questrunner.OperateLoc(d, gate, "Open"); err != nil {
		logf(d, "fishing: the gate: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	done := dl.DriveUntil(func() bool { return inside() == in }, prefer, fishGateWait)
	questrunner.ClearLeftoverModal(d)
	if !done {
		logf(d, "fishing: the gate did not put the account %s (at %v)", map[bool]string{true: "in", false: "out"}[in], questrunner.CurrentTile(d))
		return false
	}
	d.Delay(600 * time.Millisecond)
	return true
}

// fishLeave quits the contest: out through the gate, taking the prompt's reset when one is running. The stage goes back to 1, the entry
// fee is lost and the pipe stays stashed.
func fishLeave(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	return fishCross(d, false)
}

// fishPipe finds the wall pipe the account stands on: a straight wall decoration is operated from its own tile and from no other.
func fishPipe(d *questrunner.Deps) (locindex.Match, bool) {
	here := questrunner.CurrentTile(d)
	m, ok := questrunner.FindLoc(d, questplan.LocWallPipe, "", "Check", here, 1)
	if !ok || m.X != here.X || m.Z != here.Z || m.Level != here.Level {
		return locindex.Match{}, false
	}
	return m, true
}

// fishStashGarlic puts the garlic in the wall pipe from the pipe's own tile, inside the compound. The script waits two ticks, takes the
// garlic and, for a player already seated at the willow tree (the fee paid), moves the Stranger and seats the player at the pipes in
// the same breath, with pages of dialogue to continue.
func fishStashGarlic(d *questrunner.Deps) bool {
	if questrunner.HeldID(d, questplan.ObjGarlic) == 0 {
		return true
	}
	if !questrunner.Travel(d, questplan.FcPipeStand, 0) || questrunner.CurrentTile(d) != questplan.FcPipeStand {
		logf(d, "fishing: the pipe's own tile %v was not reached (at %v)", questplan.FcPipeStand, questrunner.CurrentTile(d))
		return false
	}
	pipe, ok := fishPipe(d)
	if !ok {
		logf(d, "fishing: no wall pipe (loc %d) underfoot at %v", questplan.LocWallPipe, questplan.FcPipeStand)
		return false
	}
	if err := questrunner.UseItemOnLoc(d, questplan.ObjGarlic, pipe); err != nil {
		logf(d, "fishing: the garlic on the wall pipe: %v", err)
		return false
	}
	if !waitUntil(d, fishStashWait, func() bool { return questrunner.HeldID(d, questplan.ObjGarlic) == 0 }) {
		logf(d, "fishing: the pipe did not take the garlic in %s", fishStashWait)
		return false
	}
	logf(d, "fishing: the garlic is in the wall pipe")
	// A seated player gets the Stranger's complaint, Bonzo's line and a message box.
	dl := questrunner.NewDialogue(d)
	if waitUntil(d, 4*time.Second, dl.Ready) {
		dl.Drive(nil)
	}
	return true
}

// fishStash is the stage 2 step: the garlic, into the pipe, in the compound.
func fishStash(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !fishCross(d, true) {
		return false
	}
	return fishStashGarlic(d)
}

// fishPay takes the account through the gate and talks to Bonzo: the fee, and the seat (the garlic is in the pipe already, a step of its
// own before this one). A seat that is not given (the Stranger is away from his post: another account's garlic moved him) leaves the
// fee paid and the stage at 1, and talking to Bonzo again is free, so the step waits a little for the Stranger to walk home and ends;
// the engine asks again.
func fishPay(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !fishCross(d, true) {
		return false
	}
	stop := questplan.FcBonzoStop
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		logf(d, "fishing: could not open a dialogue with Bonzo from %v", questrunner.CurrentTile(d))
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	questrunner.ClearLeftoverModal(d)
	e.InvalidateStage()
	if e.StageIs(questplan.FcStarted) {
		logf(d, "fishing: Bonzo took the fee and did not seat the account at the pipes: waiting for the Stranger to come back")
		d.Delay(8 * time.Second)
	}
	return true
}

// fishSpot finds the pipes' fishing spot.
func fishSpot(d *questrunner.Deps) (entityquery.Entity, bool) {
	return questrunner.FindNpcByID(d, questplan.NpcPipesSpot, "", 20)
}

// fishFish casts at the pipes' spot until the trophy comes. Each click is one cast (the script ends after the catch), the third
// catch makes Bonzo judge the pack, and a giant carp among the fish wins. The step ends at the trophy, at the bait running out or at
// the run's cast cap.
func fishFish(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	held := func(id int) int { return questrunner.HeldID(d, id) }
	if held(questplan.ObjFishingTrophy) > 0 {
		return true
	}
	if !fishCross(d, true) {
		return false
	}
	if _, ok := fishSpot(d); !ok {
		questrunner.Travel(d, questplan.FcPipesSpot, 3)
	}
	spot, ok := fishSpot(d)
	if !ok {
		logf(d, "fishing: no fishing spot (npc %d) near the pipes", questplan.NpcPipesSpot)
		return false
	}
	if !fluffsApproach(d, tile.Tile{X: spot.X, Z: spot.Z, Level: spot.Level}) {
		logf(d, "fishing: could not stand beside the pipes' fishing spot at %d,%d", spot.X, spot.Z)
		return false
	}
	dl := questrunner.NewDialogue(d)
	fish := func() int { return held(questplan.ObjRawGiantCarp) + held(questplan.ObjRawSardine) }
	for casts := 0; casts < fishCasts && held(questplan.ObjFishingTrophy) == 0; casts++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if held(questplan.ObjRedVineWorm)+held(questplan.ObjFishingBait) == 0 {
			logf(d, "fishing: no bait left after %d cast(s)", casts)
			break
		}
		if spot, ok = fishSpot(d); !ok {
			logf(d, "fishing: the fishing spot is gone")
			break
		}
		mark := time.Now()
		before := held(questplan.ObjRedVineWorm) + held(questplan.ObjFishingBait)
		fishBefore := fish()
		if err := entityquery.Interact(d.Session, spot, "Fish"); err != nil {
			logf(d, "fishing: casting at the pipes: %v", err)
			break
		}
		waitUntil(d, fishCastWait, func() bool {
			return held(questplan.ObjFishingTrophy) > 0 || dl.Ready() || fish() > fishBefore ||
				held(questplan.ObjRedVineWorm)+held(questplan.ObjFishingBait) < before ||
				cookSaw(d, mark, "you need", "you can't carry")
		})
		if cookSaw(d, mark, "you need at least 10 fishing") {
			logf(d, "fishing: the spot wants Fishing 10")
			questrunner.ClearLeftoverModal(d)
			return false
		}
		if dl.Ready() {
			// Bonzo's judgement after the third catch, or his line about the seat.
			dl.Drive(nil)
		}
		d.Delay(600 * time.Millisecond)
	}
	if held(questplan.ObjFishingTrophy) == 0 {
		dl.DriveUntil(func() bool { return held(questplan.ObjFishingTrophy) > 0 }, nil, fishJudgeWait)
	}
	questrunner.ClearLeftoverModal(d)
	if held(questplan.ObjFishingTrophy) > 0 {
		logf(d, "fishing: won the contest, the trophy is in the pack")
		return true
	}
	logf(d, "fishing: no trophy after the casts (%d fish in the pack)", fish())
	return false
}

// fishSpare asks Bonzo for the spare trophy a champion who lost theirs is given: through the gate, and his "Hello champ!".
func fishSpare(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if questrunner.HeldID(d, questplan.ObjFishingTrophy) > 0 {
		return true
	}
	if !fishCross(d, true) {
		return false
	}
	stop := questplan.FcBonzoSpareStop
	if !questrunner.GotoNpc(d, stop, nil) || !questrunner.OpenDialogue(d, stop) {
		logf(d, "fishing: could not open a dialogue with Bonzo from %v", questrunner.CurrentTile(d))
		return false
	}
	dl := questrunner.NewDialogue(d)
	dl.DriveUntil(func() bool { return questrunner.HeldID(d, questplan.ObjFishingTrophy) > 0 }, stop.Prefer, 20*time.Second)
	questrunner.ClearLeftoverModal(d)
	return questrunner.HeldID(d, questplan.ObjFishingTrophy) > 0
}

// fishVineFor finds the nearest vine to the patch's stand that is not set aside.
func fishVineFor(d *questrunner.Deps, skip map[tile.Tile]time.Time) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	near := questplan.FcVineStand
	q := locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: fishVineRadius, IDs: questplan.FcRedVineLocs, Op: "Check"}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	for _, m := range d.Locs.Find(d.Config, q, live) {
		if until, ok := skip[tile.Tile{X: m.X, Z: m.Z, Level: m.Level}]; ok && time.Now().Before(until) {
			continue
		}
		return m, true
	}
	return locindex.Match{}, false
}

// fishSafeStand is the stand of a vine, nearest the account first, that is out of every guard dog's reach.
func fishSafeStand(d *questrunner.Deps, vine locindex.Match) (tile.Tile, bool) {
	for _, s := range questrunner.LocStands(d, vine, questrunner.CurrentTile(d)) {
		if questplan.FcSafeFromDogs(s) {
			return s, true
		}
	}
	return tile.Tile{}, false
}

// fishLeaveWood walks out of McGrubor's Wood to the tile outside the loose railing, by the hostile-breaking walk.
func fishLeaveWood(d *questrunner.Deps) bool {
	return questrunner.TravelCautious(d, questplan.FcWoodOutside, 1)
}

// fishDig digs red vine worms in McGrubor's Wood until the pack holds the step's quantity. The wood is fenced, its one door the loose
// railing, and ten level 44 guard dogs hunt there (accounts of combat 88 or less, within a tile of themselves): the trip is topped
// up to the starting level of a fight with a dog before the railing (the module priced it, questplan.FishingWoodFit), walked with the
// hostile-breaking walk and auto-retaliate off, abandoned the moment a dog is at the account or its hitpoints are under half, and
// capped at one death a day. The vines are dug from the patch's middle, and a vine that gave nothing is set aside, because the engine
// can only path to some of them from a given tile.
func fishDig(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	want := step.Qty
	if want <= 0 {
		want = questplan.FcWormTarget
	}
	worms := func() int { return questrunner.HeldID(d, questplan.ObjRedVineWorm) }
	if worms() >= want {
		return true
	}
	if questrunner.HeldID(d, questplan.ObjSpade) == 0 {
		logf(d, "fishing: no spade in the pack for the vines")
		return false
	}
	if ate := questrunner.PrepareForFight(d, questplan.FishFoodIDs(), questplan.FoeGuardDog.PeakHit()); ate > 0 {
		logf(d, "fishing: ate %d bite(s) before McGrubor's Wood", ate)
	}
	if hp, max := questrunner.Hitpoints(d); max > 0 {
		logf(d, "fishing: into McGrubor's Wood for the red vines with %d of %d hitpoints", hp, max)
	}
	if !questrunner.TravelCautious(d, questplan.FcVineStand, 1) {
		logf(d, "fishing: the vine patch %v was not reached from %v", questplan.FcVineStand, questrunner.CurrentTile(d))
		fishLeaveWood(d)
		return false
	}
	skip := map[tile.Tile]time.Time{}
	dug, failures := 0, 0
	abandoned := ""
	for worms() < want && failures < 6 && abandoned == "" {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		if _, hit := questrunner.Attacker(d); hit {
			abandoned = "a guard dog is at the account"
			break
		}
		if hp, max := questrunner.Hitpoints(d); max > 0 && hp*2 < max {
			abandoned = "under half the hitpoints"
			break
		}
		vine, ok := fishVineFor(d, skip)
		if !ok {
			logf(d, "fishing: no digable vine within %d of the patch's stand (%d set aside)", fishVineRadius, len(skip))
			failures++
			d.Delay(2 * time.Second)
			continue
		}
		// Dig from a tile out of every dog's reach: the engine takes the nearest stand of a vine, and a vine at the patch's edge has
		// stands within a dog's hunt range of its wander box. The account is walked onto a safe stand first, and a vine with none is
		// set aside for good.
		stand, ok := fishSafeStand(d, vine)
		if !ok {
			logf(d, "fishing: the vine at %d,%d has no stand out of the dogs' reach", vine.X, vine.Z)
			skip[tile.Tile{X: vine.X, Z: vine.Z, Level: vine.Level}] = time.Now().Add(time.Hour)
			failures++
			continue
		}
		if questrunner.CurrentTile(d) != stand && !questrunner.WalkNear(d, stand, 0) {
			logf(d, "fishing: the safe stand %v of the vine at %d,%d was not reached from %v", stand, vine.X, vine.Z, questrunner.CurrentTile(d))
			skip[tile.Tile{X: vine.X, Z: vine.Z, Level: vine.Level}] = time.Now().Add(fishDigSkip)
			failures++
			continue
		}
		before := worms()
		mark := time.Now()
		if err := questrunner.OperateLoc(d, vine, "Check"); err != nil {
			logf(d, "fishing: the vine at %d,%d: %v", vine.X, vine.Z, err)
			skip[tile.Tile{X: vine.X, Z: vine.Z, Level: vine.Level}] = time.Now().Add(fishDigSkip)
			failures++
			continue
		}
		if waitUntil(d, fishDigWait, func() bool { return worms() > before }) {
			dug++
			failures = 0
			continue
		}
		logf(d, "fishing: the vine at %d,%d gave no worm (%s)", vine.X, vine.Z, map[bool]string{true: "the spade's refusal", false: "no answer"}[cookSaw(d, mark, "spade")])
		skip[tile.Tile{X: vine.X, Z: vine.Z, Level: vine.Level}] = time.Now().Add(fishDigSkip)
		failures++
	}
	if abandoned != "" {
		logf(d, "fishing: leaving McGrubor's Wood with %d worm(s): %s", worms(), abandoned)
	}
	left := fishLeaveWood(d)
	logf(d, "fishing: dug %d worm(s), the pack holds %d (out of the wood: %v)", dug, worms(), left)
	// Fewer worms than the step asked for is progress the pack keeps, and not a reason to walk back into the wood at once: the quest
	// parks for half an hour, and the next run digs the rest (or goes on with plain bait for the other casts, FcBaitReady).
	if worms() < want {
		if abandoned != "" {
			e.RequestPark("the wood's guard dogs drove the account off the red vines", fishRetryPark)
		} else {
			e.RequestPark("the red vines gave fewer worms than the contest wants", fishRetryPark)
		}
	}
	return worms() > 0
}
