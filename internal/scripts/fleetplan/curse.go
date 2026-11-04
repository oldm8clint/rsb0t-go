package fleetplan

// FlatXP is the Magic experience a cast pays before any damage, in whole experience.
func (s Spell) FlatXP() float64 { return float64(s.XP) / 10 }
