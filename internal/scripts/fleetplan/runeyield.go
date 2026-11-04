package fleetplan

import (
	"strings"
)

// RunePayRunes are the runes a yield counts: the four the fleet's Magic training is short of. Law
// and cosmic runes drop too, but the mule holds them in the tens of thousands.
var RunePayRunes = []string{"Chaos rune", "Nature rune", "Death rune", "Blood rune"}

// IsRunePayName reports whether an item name is one of RunePayRunes (case-insensitive).
func IsRunePayName(name string) bool {
	for _, r := range RunePayRunes {
		if strings.EqualFold(strings.TrimSpace(name), r) {
			return true
		}
	}
	return false
}
