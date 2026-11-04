package questmods

import (
	"time"

	"rsb0t-go/internal/scripts/crafterrunner"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// SheepShearer is Sheep Shearer ready for the engine.
func SheepShearer() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.SheepShearer,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomSheepWool:   sheepWool,
			questplan.CustomSheepHandIn: sheepHandIn,
		},
	}
}

// sheepWoolBudget bounds the shearing of one run: about 27 attempts for 20 wool (a quarter of them fail), and the
// sheep that gave wool grow it back after a minute.
const sheepWoolBudget = 8 * time.Minute

// sheepWool shears the Lumbridge pen until the pack holds step.Qty wool (the balls it still lacks) and spins all of it at
// the castle wheel, on the legs the Crafting rung proved live (crafterrunner: the pen's gate, Shear, the staircase, SpinWool).
// Wool already in the pack counts, and a pack that already holds enough is only spun. It reports whether the pack gained
// balls of wool; a partial gain is progress, and the next decision asks for what is left.
func sheepWool(e *questrunner.Engine, step questplan.Step) bool {
	d := e.Deps()
	cd := crafterDeps(d)
	want := step.Qty
	ballsBefore := crafterrunner.Count(cd, freshstart.Ball)
	if crafterrunner.Count(cd, freshstart.Wool) < want {
		if crafterrunner.Count(cd, freshstart.Shears) == 0 {
			logf(d, "sheep: no shears in the pack")
			return false
		}
		if !crafterrunner.GoToPen(cd) {
			return false
		}
		skip := map[int]time.Time{}
		deadline := time.Now().Add(sheepWoolBudget)
		for crafterrunner.Count(cd, freshstart.Wool) < want && time.Now().Before(deadline) {
			switch crafterrunner.Shear(cd, skip) {
			case crafterrunner.ShearNoShears:
				logf(d, "sheep: the shears are gone")
				return false
			case crafterrunner.ShearNoSheep:
				if !d.Delay(2 * time.Second) {
					return false
				}
			}
		}
		crafterrunner.LeavePen(cd)
	}
	wool := crafterrunner.Count(cd, freshstart.Wool)
	spun := crafterrunner.SpinWool(cd, freshstart.LumbridgeWheel)
	balls := crafterrunner.Count(cd, freshstart.Ball)
	logf(d, "sheep: sheared %d wool, spun %d; the pack holds %d balls of wool (wanted %d more)", wool, spun, balls, want)
	return balls > ballsBefore
}

// sheepHandInBudget bounds the hand-in: 19 balls a tick or two apart, then the closing dialogue.
const sheepHandInBudget = 90 * time.Second

// sheepHandIn gives Fred the balls. He takes them one by one with his dialogue shut between (fred_give_wool: if_close, delete
// a ball, p_delay), and speaks again only for the last ("I have your last ball of wool."), so a plain talk step would
// return after the first page and the next decision would find the conversation half done; this keeps driving whatever
// dialogue he reopens until the pack holds no ball or the tab reads complete.
func sheepHandIn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !questrunner.GotoNpc(d, questplan.FredStop, nil) {
		return false
	}
	if !questrunner.OpenDialogue(d, questplan.FredStop) {
		return false
	}
	start := questrunner.HeldID(d, questplan.ObjBallOfWool)
	complete := func() bool { return questrunner.ReadStatus(d, questplan.SheepShearerName) == questplan.StatusComplete }
	finished := func() bool { return questrunner.HeldID(d, questplan.ObjBallOfWool) == 0 || complete() }
	questrunner.NewDialogue(d).DriveUntil(finished, questplan.FredStop.Prefer, sheepHandInBudget)
	questrunner.ClearLeftoverModal(d)
	given := start - questrunner.HeldID(d, questplan.ObjBallOfWool)
	logf(d, "sheep: Fred took %d of %d balls of wool (quest tab complete: %v)", given, start, complete())
	return given > 0 || complete()
}
