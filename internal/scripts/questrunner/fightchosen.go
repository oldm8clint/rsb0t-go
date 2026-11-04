package questrunner

import (
	"time"

	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/tile"
)

// FightResult is how a fight against one chosen npc ended.
type FightResult int

const (
	// FightWon: the target left the scene (it died, or a teleport took it out of view: the caller reads the ground to tell).
	FightWon FightResult = iota
	// FightGaveUp: the caller's giveUp said the target was no longer worth the fight (it had moved out of reach).
	FightGaveUp
	// FightLost: the account died, was losing and walked away, or could not send the attack.
	FightLost
	// FightStopped: the bot was asked to stop.
	FightStopped
	// FightTimedOut: the budget ran out with the target still there.
	FightTimedOut
)

// giveUpEnv is a fightEnv whose target stops counting as there the moment giveUp says so, which ends the loop as a won
// fight; gaveUp tells the caller it was this and not a kill.
type giveUpEnv struct {
	fightEnv
	giveUp func() bool
	gaveUp bool
}

func (e *giveUpEnv) View() fightView {
	v := e.fightEnv.View()
	if v.Alive && e.giveUp != nil && e.giveUp() {
		e.gaveUp = true
		v.Alive = false
	}
	return v
}

// fightChosenWalkAbove is how far the target may be before the account walks towards it ahead of the first blow: the
// attack click itself brings the account to a target that is in view, and chases it when it moves, which a walk to
// where it stood a moment ago does not.
const fightChosenWalkAbove = 12

// FightChosen fights npc itself, where FightNpc fights the nearest of its kind that is not facing another player: the
// choice (and the reasons for it) is the caller's. giveUp is asked once a poll (every 600 ms) while the target is in
// view and ends the fight when it returns true. The attack loop is the shared one (meleeFightVs): it sends the attack
// again when eating or a dropped click has stopped it and gives up a fight it is losing. It logs nothing for a fight that
// needed its attack sent again; the caller keeps its own tally.
//
// FightNpc's pick is the nearest, by Chebyshev distance, in a radius of 16. For an imp that is a pocket of the map as
// often as not: the monastery's rooms, a courtyard, the Fight Arena's yard. The attack on such an imp is accepted and goes
// nowhere ("I can't reach that!"), and the loop sends it again every three seconds for the fight's three minutes.
func FightChosen(d *Deps, npc entityquery.Entity, sus questplan.Sustain, budget time.Duration, giveUp func() bool) FightResult {
	sus, foe, _ := VsFoe(d, sus, npc.ID)
	PrepareForFight(d, sus.FoodIDs, foe.PeakHit())
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if here, ok := currentTileOK(d); ok && (here.Level != at.Level || tile.Chebyshev(here, at) > fightChosenWalkAbove) {
		WalkNear(d, at, 3)
	}
	defer ensureRetaliate(d)()
	f := newLiveFight(d, npc, sus)
	env := &giveUpEnv{fightEnv: f, giveUp: giveUp}
	end := meleeFightVs(env, sus, foe.PeakHit(), budget, nil)
	switch end.Outcome {
	case fightWon:
		if env.gaveUp {
			return FightGaveUp
		}
		return FightWon
	case fightStopped:
		return FightStopped
	case fightTimedOut:
		return FightTimedOut
	}
	finishFight(d, f, end) // a lost fight walks away, as every quest fight does
	return FightLost
}
