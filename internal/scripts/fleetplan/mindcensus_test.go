package fleetplan

// windStrikeTenths is Wind Strike's base experience a cast in tenths, from the spell table.
func windStrikeTenths() int {
	sp, _ := SpellByName("Wind Strike")
	return sp.XP
}
