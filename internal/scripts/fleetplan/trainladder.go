package fleetplan

// UsesFire reports whether a cast of s spends a Fire rune. TRAINING NEVER CASTS ONE (a user request: "the mule
// holds ~395k Water and lots of Earth runes ... never Fire spells, so Fire runes stay for alching"): High Level
// Alchemy pays five Fire runes a cast (or none from a Staff of fire), and the fleet's alch loops, the walk alch
// and the endgame's stun alch, are what the Fire runes are for. Water and Earth spells pay more experience a cast
// than the Wind ones, from the runes the mule has in the hundreds of thousands.
func (s Spell) UsesFire() bool {
	for _, r := range s.Runes {
		if r.Name == RuneFire && r.Count > 0 {
			return true
		}
	}
	return false
}
