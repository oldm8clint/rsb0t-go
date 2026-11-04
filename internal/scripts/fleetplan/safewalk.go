package fleetplan

// SafeWalkMinHPPercent is the share of the maximum hitpoints under which an account with no food does not begin a walk that is not an
// escape. It is the run staller's line (StallMinHPPercent): the point under which the account must be free to eat is the point under which
// it has no business walking anywhere without anything to eat.
const SafeWalkMinHPPercent = StallMinHPPercent

// WalkHurt reports hitpoints known and under SafeWalkMinHPPercent of the maximum. An unread maximum, or a dead account (0 hitpoints, a stat
// packet not yet read), is not hurt: the gate only ever holds a walk back on a reading it trusts.
func WalkHurt(hp, maxHP int) bool {
	return maxHP > 0 && hp > 0 && hp*100 < maxHP*SafeWalkMinHPPercent
}

// StallStandAside is the run staller's hitpoints gate: the walk's casts and rubs wait while hitpoints are under StallMinHPPercent so a bite
// can land (a cast's p_delay(3) and a rub's p_delay(1) refuse an eat). noFood is the pack holding nothing to eat, and then there is no bite
// for the gate to make room for: it says nothing and holds nothing back (live 18 Oct 15:34:56 and 15:37:05, 18 and 20 of 55 with nothing to
// eat). The zero value of noFood keeps the old gate, for every caller that does not read the pack.
func StallStandAside(hp, maxHP int, noFood bool) bool { return !noFood && StallHPLow(hp, maxHP) }
