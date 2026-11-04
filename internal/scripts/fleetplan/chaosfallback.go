package fleetplan

// UsesChaos reports whether a cast of s spends a chaos rune.
func (s Spell) UsesChaos() bool {
	for _, r := range s.Runes {
		if r.Name == ChaosRune && r.Count > 0 {
			return true
		}
	}
	return false
}

// IsStrike reports a Strike-tier spell: one mind rune a cast, no chaos.
func (s Spell) IsStrike() bool {
	for _, r := range s.Runes {
		if r.Name == MindRune {
			return !s.UsesChaos()
		}
	}
	return false
}
