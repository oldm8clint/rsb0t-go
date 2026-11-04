package questmods

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// WitchsPotion is Witch's Potion ready for the engine.
func WitchsPotion() questrunner.Quest {
	return questrunner.Quest{
		Module: questplan.WitchsPotion,
		Customs: map[string]questrunner.CustomFunc{
			questplan.CustomHettyHandIn: hettyHandIn,
			questplan.CustomHettyRat:    hettyRat,
			questplan.CustomHettyDrink:  hettyDrink,
		},
	}
}

// hettyHandIn gives Hetty the four ingredients (she has no options at stage 1) and drinks from
// the cauldron straight after, as the TS handInAndDrink does.
func hettyHandIn(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	if !e.Execute(questplan.Step{Kind: questplan.StepTalk, Stop: questplan.HettyStop}) {
		return false
	}
	if questrunner.HeldID(d, questplan.ObjRatsTail) > 0 {
		return false // she did not take them: something was missing after all
	}
	e.InvalidateStage()
	return hettyDrink(e, questplan.Step{})
}

// hettyDrink drinks from the cauldron and continues the mesbox that follows: the script queues
// the completion only once that message's pause returns, and closing the box instead of
// continuing it abandons the script (the second EMPTY lab run parked at stage 2 on exactly that).
func hettyDrink(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	defer e.InvalidateStage()
	if !questrunner.Travel(d, questplan.HettyAnchor, 2) {
		return false
	}
	m, ok := questrunner.FindLoc(d, questplan.LocCauldron, "", "", questplan.HettyCauldron, 4)
	if !ok {
		logf(d, "no cauldron at Hetty's")
		return false
	}
	if err := questrunner.OperateLoc(d, m, "Drink From"); err != nil {
		logf(d, "drink from the cauldron: %v", err)
		return false
	}
	dl := questrunner.NewDialogue(d)
	if !waitUntil(d, 6*time.Second, dl.Ready) {
		logf(d, "the cauldron opened no message")
		return false
	}
	// No menu here: Drive only continues pages.
	dl.Drive(nil)
	d.Delay(1200 * time.Millisecond)
	return true
}

// hettyRat kills a Rimmington rat (max hit 0) and takes the tail it drops for this account.
// The rats live INSIDE the house west of Hetty's (walls x 2953-2960, z 3201-3205, its door on
// the north edge of 2956,3205), and that door swings shut behind a visit: the first Witch's
// Potion EMPTY lab run stood outside it fighting a rat through the shut door ("I can't reach
// that!") until it was stopped. So each attempt first walks INTO the house (the walker's door
// hop opens it) and only picks a rat standing inside.
func hettyRat(e *questrunner.Engine, _ questplan.Step) bool {
	d := e.Deps()
	for try := 0; try < 4; try++ {
		if !questplan.InRatHouse(questrunner.CurrentTile(d)) {
			if !questrunner.Travel(d, questplan.RimmingtonRats, 0) || !questplan.InRatHouse(questrunner.CurrentTile(d)) {
				logf(d, "could not get into the rat house")
				continue
			}
		}
		rat, ok := insideRat(d)
		if !ok {
			d.Delay(3 * time.Second) // they respawn
			continue
		}
		died := tile.Tile{X: rat.X, Z: rat.Z, Level: rat.Level}
		questrunner.FightBack(d, rat, questplan.Sustain{})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if questrunner.TakeGround(d, questplan.ObjRatsTail, died, 2) {
				return true
			}
			d.Delay(400 * time.Millisecond)
		}
		if questrunner.HeldID(d, questplan.ObjRatsTail) > 0 {
			return true
		}
	}
	return questrunner.HeldID(d, questplan.ObjRatsTail) > 0
}

// insideRat is the nearest rat standing inside the rat house.
func insideRat(d *questrunner.Deps) (entityquery.Entity, bool) {
	var best entityquery.Entity
	found := false
	for _, n := range entityquery.Npcs(d.Session.State(), d.Config).Within(10).Results() {
		if n.ID != questplan.NpcRat || !questplan.InRatHouse(tile.Tile{X: n.X, Z: n.Z, Level: n.Level}) {
			continue
		}
		if !found || n.Distance < best.Distance {
			best, found = n, true
		}
	}
	return best, found
}
