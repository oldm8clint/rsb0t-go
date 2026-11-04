package questmods

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// SeaSlug is the Sea Slug Quest ready for the engine. Caroline's and Bailey's talks, the swamp paste (the provisioner's kit and the
// shop), the ground pickups and the glass on the sticks are the engine's generic steps; the customs are the legs the content makes
// special: Holgart's boat between the shore, the platform and the island (a talk and a teleport, which the walker cannot do), the
// platform's ladders, the crates Kennith is called across, the loose panel, the crane, Kent on the island, and the rubbing of the
// dry sticks.
func SeaSlug() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.SeaSlug,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomSlugToPlatform: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return slugSailTo(e.Deps(), questplan.SlugPlatform)
			},
			questplan.CustomSlugToIsland: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return slugSailTo(e.Deps(), questplan.SlugIsland)
			},
			questplan.CustomSlugToMainland: func(e *questrunner.Engine, _ questplan.Step) bool {
				defer e.InvalidateStage()
				return slugSailTo(e.Deps(), questplan.SlugMainland)
			},
			questplan.CustomSlugDown:    func(e *questrunner.Engine, _ questplan.Step) bool { return slugClimb(e.Deps(), false) },
			questplan.CustomSlugKennith: slugKennith,
			questplan.CustomSlugKent:    slugKent,
			questplan.CustomSlugRub:     slugRub,
			questplan.CustomSlugPanel:   slugPanel,
			questplan.CustomSlugCrane:   slugCrane,
		},
	}
}

const (
	// slugBoatWait is how long a boarding gets to move the account to another region: Holgart's last line, a two-tick delay, the teleport.
	slugBoatWait = 25 * time.Second
	// slugHops bounds the boardings of one voyage: shore to platform to island and back is three.
	slugHops = 4
	// slugClimbWait is how long a ladder gets to change the account's floor.
	slugClimbWait = 12 * time.Second
	// slugRubs bounds the rubbing of the dry sticks: a Firemaking 30 account lights them about three times in four.
	slugRubs = 12
	// slugRubWait is how long one rub gets to answer.
	slugRubWait = 8 * time.Second
	// slugShoutWait is how long the shout across the crates gets to open Kennith's lines.
	slugShoutWait = 8 * time.Second
	// slugCraneWait is how long the crane gets to lower Kennith (its script has about ten ticks of delays).
	slugCraneWait = 25 * time.Second
	// slugQuiet is how long a conversation is quiet before it counts as over: Kent's "Traveller wait!" follows his first talk by
	// a few seconds.
	slugQuiet = 7 * time.Second
	// slugTalkBudget bounds one such conversation.
	slugTalkBudget = 60 * time.Second
)

// slugRegion is where the account stands relative to Holgart's boat.
func slugRegion(d *questrunner.Deps) questplan.SlugRegion {
	return questplan.SlugRegionOf(questrunner.CurrentTile(d))
}

// slugDriveAll drives every chat page that appears until none has for slugQuiet (or budget runs out), clearing a message box between
// pages: a quest script's dialogues come in bursts with delays between them, and a driver that stops at the first lull misses the next.
func slugDriveAll(d *questrunner.Deps, budget time.Duration) {
	dl := questrunner.NewDialogue(d)
	deadline := time.Now().Add(budget)
	last := time.Now()
	for time.Now().Before(deadline) {
		if dl.Ready() {
			if !dl.Drive(nil) && len(dl.OptionTexts()) > 0 {
				// A menu none of these scripts has: close it and stop, never spin on it (Drive refuses to guess and returns at once).
				logf(d, "seaslug: an unexpected menu [%s]: closing it", strings.Join(dl.OptionTexts(), " | "))
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
		if time.Since(last) >= slugQuiet {
			return
		}
		if !d.Delay(300 * time.Millisecond) {
			return
		}
	}
}

// slugBoard talks to the Holgart of the region the account stands in and waits for the boat to set it down somewhere else. The talk is
// the stop's: the shore's menu is answered "Okay, lets do it." / "Will you take me there?" (the way out), the platform's "Okay, let's go
// back." when the voyage is home and "No, I'm going to stay a while." when it is not (the platform's Holgart has no menu at stage 4, when his
// boat goes to the island, and at 11, when it goes home).
func slugBoard(d *questrunner.Deps, from, target questplan.SlugRegion) bool {
	var stop questplan.NpcStop
	switch from {
	case questplan.SlugMainland:
		stop = questplan.SlugHolgartLandStop
	case questplan.SlugPlatform:
		if questrunner.CurrentTile(d).Level != 0 && !slugClimb(d, false) {
			return false
		}
		stop = questplan.SlugHolgartPlatformStop
		if target != questplan.SlugMainland {
			stop.Prefer = []string{"No, I'm going to stay a while."}
		}
	default:
		stop = questplan.SlugHolgartIslandStop
	}
	logf(d, "seaslug: boarding at the %s bound for the %s", from, target)
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "seaslug: Holgart was not reached at the %s (at %v)", from, questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	moved := waitUntil(d, slugBoatWait, func() bool { return slugRegion(d) != from })
	questrunner.ClearLeftoverModal(d) // "You arrive at ..."
	return moved
}

// slugSailTo takes the account to a region by as many boardings as it takes: which shore the boat reaches from where depends on the
// quest stage (the content sends the shore to the platform before Kent is found and to the island after), so the voyage is made a
// hop at a time and judged by where the account lands.
func slugSailTo(d *questrunner.Deps, target questplan.SlugRegion) bool {
	for hop := 0; hop < slugHops; hop++ {
		here := slugRegion(d)
		if here == target {
			return true
		}
		if !slugBoard(d, here, target) {
			return slugRegion(d) == target
		}
	}
	return slugRegion(d) == target
}

// slugClimb takes the platform's ladder up or down. Climbing up from stage 6 on without a lit torch costs 4 hitpoints each time
// (the fishermen's rod), so a refused climb is reported as that and not retried.
func slugClimb(d *questrunner.Deps, up bool) bool {
	want, stand, op, id := 0, questplan.SlugLadderTop, "Climb-down", questplan.LocSlugLadderDown
	if up {
		want, stand, op, id = 1, questplan.SlugLadderFoot, "Climb-up", questplan.LocSlugLadderUp
	}
	if questrunner.CurrentTile(d).Level == want {
		return true
	}
	if !questrunner.Travel(d, stand, 0) && !atTile(d, stand) {
		logf(d, "seaslug: the ladder's stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, id, "", op, stand, 4)
	if !ok {
		logf(d, "seaslug: no ladder offering %q (loc %d) near %v", op, id, stand)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, op); err != nil {
		logf(d, "seaslug: the ladder: %v", err)
		return false
	}
	if waitUntil(d, slugClimbWait, func() bool { return questrunner.CurrentTile(d).Level == want }) {
		d.Delay(time.Second)
		return true
	}
	if cookSaw(d, mark, "fishing rod", "smack you") {
		logf(d, "seaslug: the fishermen turned the account back from the ladder (a lit torch is needed from stage 6 on)")
	} else {
		logf(d, "seaslug: the ladder did not change the account's floor (at %v)", questrunner.CurrentTile(d))
	}
	return false
}

// slugShout shouts across the crates to Kennith and drives what he says (his lines have no menu). It reports whether he answered.
func slugShout(d *questrunner.Deps) bool {
	if !slugClimb(d, true) {
		return false
	}
	stand := questplan.SlugCratesStand
	if !questrunner.Travel(d, stand, 0) && !atTile(d, stand) {
		logf(d, "seaslug: the crates' stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocSlugCrates, "", "Shout-across", stand, 4)
	if !ok {
		logf(d, "seaslug: no crates (loc %d) offering Shout-across near %v", questplan.LocSlugCrates, stand)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Shout-across"); err != nil {
		logf(d, "seaslug: shouting across the crates: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, slugShoutWait, func() bool { return dl.Ready() || cookSaw(d, mark, "doesn't seem interested") }) {
		logf(d, "seaslug: Kennith answered nothing in %s (at %v)", slugShoutWait, questrunner.CurrentTile(d))
		return false
	}
	if cookSaw(d, mark, "doesn't seem interested") {
		logf(d, "seaslug: Kennith has nothing to say at this stage")
		return false
	}
	slugDriveAll(d, slugTalkBudget)
	return true
}

// slugKennith takes the account to the platform and shouts across the crates (stage 3, and stage 7 with the torch lit).
func slugKennith(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !slugSailTo(d, questplan.SlugPlatform) {
		return false
	}
	return slugShout(d)
}

// slugPanel kicks the loose panel out (stage 8; at stage 9 it only says nothing happens) and calls Kennith through it.
func slugPanel(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !slugSailTo(d, questplan.SlugPlatform) || !slugClimb(d, true) {
		return false
	}
	stand := questplan.SlugPanelStand
	if !questrunner.Travel(d, stand, 0) && !atTile(d, stand) {
		logf(d, "seaslug: the loose panel's stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocSlugPanel, "", "Kick", stand, 3)
	if !ok {
		logf(d, "seaslug: no loose panel (loc %d) near %v", questplan.LocSlugPanel, stand)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Kick"); err != nil {
		logf(d, "seaslug: kicking the panel: %v", err)
		return false
	}
	if !waitUntil(d, slugShoutWait+4*time.Second, func() bool { return cookSaw(d, mark, "crumbles away", "nothing interesting happens") }) {
		logf(d, "seaslug: the kick on the panel answered nothing")
		return false
	}
	return slugShout(d)
}

// slugCrane rotates the crane from its north side (stage 10): the content refuses it from the south edge ("I need to get closer to use
// that."), and the lowering is a ten-tick script.
func slugCrane(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !slugSailTo(d, questplan.SlugPlatform) || !slugClimb(d, true) {
		return false
	}
	stand := questplan.SlugCraneStand
	if !questrunner.Travel(d, stand, 0) && !atTile(d, stand) {
		logf(d, "seaslug: the crane's stand %v was not reached from %v", stand, questrunner.CurrentTile(d))
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocSlugCrane, "", "Rotate", stand, 6)
	if !ok {
		logf(d, "seaslug: no crane (loc %d) near %v", questplan.LocSlugCrane, stand)
		return false
	}
	mark := time.Now()
	if err := questrunner.OperateLoc(d, m, "Rotate"); err != nil {
		logf(d, "seaslug: rotating the crane: %v", err)
		return false
	}
	if waitUntil(d, slugCraneWait, func() bool { return cookSaw(d, mark, "lower kennith to the row boat") }) {
		logf(d, "seaslug: Kennith is in the row boat")
		return true
	}
	if cookSaw(d, mark, "get closer to use that") {
		logf(d, "seaslug: the crane refused from %v: its stand has to be north of its 4x4 footprint", questrunner.CurrentTile(d))
	} else {
		logf(d, "seaslug: the crane turned and Kennith did not come down")
	}
	return false
}

// slugKent takes the account to the island and talks to Kent, then drives the rest of his scene (he pulls a sea slug off the account's
// neck and drops it on the ground; nothing in the quest touches it again: its Take costs 3 hitpoints).
func slugKent(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !slugSailTo(d, questplan.SlugIsland) {
		return false
	}
	stop := questplan.SlugKentStop
	if !questrunner.GotoNpc(d, stop, nil) {
		logf(d, "seaslug: Kent was not reached on the island (at %v)", questrunner.CurrentTile(d))
		return false
	}
	if !questrunner.OpenDialogue(d, stop) {
		return false
	}
	questrunner.NewDialogue(d).Drive(stop.Prefer)
	slugDriveAll(d, slugTalkBudget)
	return true
}

// slugRub rubs the dry sticks together until the torch is lit: a Firemaking 30 account lights them three times in four, the sticks are
// not used up by a rub, and the content lights the unlit torch held with them (a rub with no unlit torch moves the quest on and lights
// nothing, which the decision's torch rule keeps from happening). Below the level it parks the quest for good measure.
func slugRub(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	for attempt := 1; attempt <= slugRubs; attempt++ {
		if questrunner.HeldID(d, questplan.ObjTorchLit) > 0 {
			return true
		}
		if questrunner.HeldID(d, questplan.ObjDrySticks) == 0 {
			logf(d, "seaslug: no dry sticks in the pack to rub")
			return false
		}
		if lvl := questrunner.BaseLevels(d)["firemaking"]; lvl > 0 && lvl < questplan.SlugFiremaking {
			e.RequestPark("Firemaking is under 30: the dry sticks never catch", questplan.SlugParkFor)
			return false
		}
		mark := time.Now()
		if err := questrunner.OpHeldByName(d, questplan.ObjDrySticks, "Rub-together"); err != nil {
			logf(d, "seaslug: rubbing the sticks: %v", err)
			return false
		}
		if !waitUntil(d, slugRubWait, func() bool { return cookSaw(d, mark, "catch alight", "smoke momentarily", "firemaking level of 30") }) {
			logf(d, "seaslug: the rub produced no message at all")
			return false
		}
		if cookSaw(d, mark, "firemaking level of 30") {
			e.RequestPark("Firemaking is under 30: the dry sticks never catch", questplan.SlugParkFor)
			return false
		}
		if cookSaw(d, mark, "catch alight") {
			return waitUntil(d, 4*time.Second, func() bool { return questrunner.HeldID(d, questplan.ObjTorchLit) > 0 }) ||
				questrunner.HeldID(d, questplan.ObjTorchUnlit) == 0
		}
	}
	logf(d, "seaslug: %d rubs and the sticks never caught", slugRubs)
	return questrunner.HeldID(d, questplan.ObjTorchLit) > 0
}

// slugLeave takes the account back to the shore: the pocket's way out for every other quest's walks.
func slugLeave(d *questrunner.Deps) bool { return slugSailTo(d, questplan.SlugMainland) }

// The platform and the island are pockets: Holgart's boat is their only way off, and the walk graph has neither.
func init() {
	in := func(t tile.Tile) bool { return questplan.OnSlugPlatform(t) || questplan.OnSlugIsland(t) }
	questrunner.RegisterPocket(questrunner.Pocket{Name: "the fishing platform and Kent's island", In: in, Leave: slugLeave})
}
