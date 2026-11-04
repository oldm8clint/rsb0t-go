package fleetplan

// TrainFloorHP is the HP at or below which a training fight is broken off (the account walks out of
// the herd and the decider eats): the larger of the eat setting less one, half of MaxHP and 3,
// never at or above MaxHP. Half of MaxHP, because the fight's own damage is not the only damage
// (a random-event monster, a second attacker) and a cake heals 4: a ten-hitpoint account breaks
// off at 5 and eats back to 9 before the next fight. eatBelow 0 (never eat) still gets the floor.
func TrainFloorHP(maxHP, eatBelow int) int {
	if maxHP <= 1 {
		return 0
	}
	floor := max(eatBelow-1, (maxHP+1)/2, 3)
	if floor >= maxHP {
		floor = maxHP - 1
	}
	return floor
}

// TrainEatBelowHP is the training branch's eat threshold: at or below the fight floor the account
// eats before it fights again, so a fight broken off at the floor is always followed by a bite,
// never by the next fight.
func (s Stage3Snapshot) TrainEatBelowHP() int {
	floor := TrainFloorHP(s.MaxHP, s.EatBelowHP)
	if floor <= 0 {
		return s.EatBelowHP
	}
	// After a view lag, what already faces the account for as long as an episode can hide it (lageat.go); nothing facing it, no change.
	return s.lagEatBelow(max(floor+1, s.EatBelowHP), max(s.LagFacing, s.LagReach))
}

// EatEmergencyHP is the HP under which Phase3Eat eats whatever heals most rather than waiting for a
// bite that fits: the larger of the thieving and training thresholds, so a decider that chose to
// eat on either one is never answered by a ChooseFood that declines to.
func (s Stage3Snapshot) EatEmergencyHP() int {
	return max(s.ThieveEatBelowHP(), s.TrainEatBelowHP())
}
