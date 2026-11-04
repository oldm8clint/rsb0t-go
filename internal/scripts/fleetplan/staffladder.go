package fleetplan

import (
	"sort"
	"strings"
)

// MagicStaff is staves.obj's [magic_staff]: cost 200, magic attack +10, supplies no rune.
const MagicStaff = "Magic staff"

// StaffAskLadder is the order the mule is asked for a staff, best first. Each rung is one order,
// so a rung the mule's published bank shows empty is skipped rather than asked for (see
// StaffAsks).
var StaffAskLadder = []string{StaffOfAir, MagicStaff, PlainStaff}

// staffInfo is the content's own numbers for the free-to-play staves (staves.obj).
type staffInfo struct {
	Cost        int
	MagicAttack int
}

var Staves = map[string]staffInfo{
	"staff":          {Cost: 15, MagicAttack: 4},
	"magic staff":    {Cost: 200, MagicAttack: 10},
	"staff of air":   {Cost: 1500, MagicAttack: 10},
	"staff of water": {Cost: 1500, MagicAttack: 10},
	"staff of earth": {Cost: 1500, MagicAttack: 10},
	"staff of fire":  {Cost: 1500, MagicAttack: 10},
}

func StaffKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// StaffScore ranks a staff for casting sp: every rune per cast it pays for outranks any bonus,
// then magic attack, then what death is more likely to keep. Zero for a name that is not an
// autocast staff, so a caller can use "> 0" as "is a staff".
//
// sp may be the zero Spell when the caller does not know what will be cast (a bank read that
// only records "the best staff in there"); the wind spells are then assumed, because every
// ScarabSwarm spell setting the rollout uses is one (Wind Strike to train, Wind Bolt to fight).
func StaffScore(name string, sp Spell) int {
	if !IsAutocastStaff(name) {
		return 0
	}
	if sp.Name == "" {
		sp, _ = SpellByName(DefaultTrainSpell)
	}
	saved := 0
	free := StaffProvides(name)
	for _, r := range sp.Runes {
		if free[r.Name] {
			saved += r.Count
		}
	}
	info := Staves[StaffKey(name)]
	// Any other autocast staff (a members' battlestaff in a lab bank, say) still casts: rank it
	// above nothing and below every staff whose numbers are known.
	score := 1 + saved*100000 + info.MagicAttack*1000 + info.Cost/10
	return score
}

// BetterStaff reports whether candidate should replace current for casting sp. An empty current
// is always replaced by any staff; a non-staff candidate never replaces anything.
func BetterStaff(candidate, current string, sp Spell) bool {
	c := StaffScore(candidate, sp)
	if c == 0 {
		return false
	}
	return c > StaffScore(current, sp)
}

// StaffPreference is every known free-to-play staff name, best first for casting sp: the order a
// bank should be searched in when the kit may take only one.
func StaffPreference(sp Spell) []string {
	names := []string{StaffOfAir, "Staff of water", "Staff of earth", "Staff of fire", MagicStaff, PlainStaff}
	sort.SliceStable(names, func(i, j int) bool { return StaffScore(names[i], sp) > StaffScore(names[j], sp) })
	return names
}
