package combatplan

// ExtendedCeiling is the band end ExtendTopBands gives the top rungs: one past 99, because a band is [FromLevel, ToLevel)
// and the highest level a run asks a rung for is the goal minus one.
const ExtendedCeiling = 100

// ExtendTopBands returns a copy of ladder in which every rung whose band ends at the ladder's top (75 exclusive on the
// melee and spread ladders: Ice giants, Ice warriors, Ogres, Fire giants, the Wilderness Ice giants) ends at to instead,
// so a lowest melee level of 75 to 99 still has a rung.
//
// Why that is safe. A rung's band is where the monster is the right size for the account: below it the kills are too
// slow, above it they are not worth the walk. Melee experience is 4 per point of damage dealt whatever the monster, so
// above 75 nothing is gained by a bigger monster, only by a safer and denser one, and the top rungs are both. What the
// band edge protected was the ladder's own honesty (MaxTrainableGoal: a goal above the edge stopped a run with "no rung
// available"); an extended copy reports the extended ceiling, so the same guard still holds. Only the Stage 5 endgame
// stints (bot.scarabswarm_endgamemelee.go) ask for it; MeleeLadder and SpreadLadder are unchanged.
//
// A ladder whose top is already at or past to is returned as a plain copy.
func ExtendTopBands(ladder []Rung, to int) []Rung {
	out := make([]Rung, len(ladder))
	copy(out, ladder)
	top := 0
	for _, r := range out {
		top = max(top, r.ToLevel)
	}
	if top <= 0 || top >= to {
		return out
	}
	for i := range out {
		if out[i].ToLevel == top {
			out[i].ToLevel = to
		}
	}
	return out
}
