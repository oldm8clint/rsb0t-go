package mastercraftplan

import (
	"strings"

	"rsb0t-go/internal/scripts/fleetplan"
)

// HIGH ALCHEMY ON THE PLATEBODY ROUND'S WALKS (a user request: "master should high alch between runs to and from the bank
// ensuring no coins are lost and smithing is still done optimally").
//
// WHAT ALREADY EXISTS: scarabrunner.Travel's walk alchemy (fleetplan.ShouldWalkAlch) casts only on a walk, never inside the
// anvil's five-tick makes, with a clear-move in front of each cast, and fleetrunner records each cast through loot.Alchemy
// (the Magic card). So an alch can never delay a smithing click: the round's anvil work is a click and a wait with no walk in
// it. The bank visit draws the alch kit in the same one-batch withdrawal as the bars.
//
// WHAT THIS ADDS, "no coins are lost": a cast is made only when what it pays beats what the cast burns. A cast burns a Nature
// rune (cost 20, runes.obj) and, with no fire staff worn, five Fire runes (cost 4 each, 20 more). High Alchemy pays 60% of an
// item's cost, so "more than the item's value plus the rune" cannot hold for any item; the rule that can, and that this
// implements, is payout > the runes the cast burns. With a fire staff worn the floor is 20, without one it is 40, and an item
// that does not clear it is never on the allow-list. Without a fire staff the walk alchemy's own rule still lets Fire runes
// above its reserve be spent (fleetplan.ShouldWalkAlch), which is the existing rule the owner accepted; only the payout floor is
// raised.
//
// THE ROUND'S OWN PLATEBODIES ARE NOT ALCHED: they are for the fleet, so the round's trips never draw its own Rune, Adamant,
// Mithril or Iron platebodies out of the bank into a walk's allow-list.

// FireRuneCost is runes.obj's cost of a Fire rune.
const FireRuneCost = 4

// AlchRuneCost is the coins' worth of runes one High Level Alchemy cast burns: a Nature rune and, without a fire staff, five
// Fire runes.
func AlchRuneCost(staff bool) int {
	c := fleetplan.NatureRuneCost
	if !staff {
		c += fleetplan.WalkAlchFirePerCast * FireRuneCost
	}
	return c
}

// AlchPays is whether a cast on an item paying payout coins is worth its runes: strictly more.
func AlchPays(payout int, staff bool) bool { return payout > AlchRuneCost(staff) }

// PlateWalkAlch is the walk alchemy the platebody round's walks carry: the owner's list less every item that does not pay
// its runes (and less the round's own platebodies), with the least payout raised to just over the runes' cost.
func PlateWalkAlch(staff bool) fleetplan.WalkAlchSettings {
	var items []string
	for _, name := range fleetplan.DefaultAlchItems {
		if isPlateName(name) {
			continue
		}
		if v := fleetplan.KnownAlchValue(name); v > 0 && AlchPays(v, staff) {
			items = append(items, name)
		}
	}
	s := fleetplan.WalkAlchSettings{Enabled: true, MinMagic: 55, Items: items, MinValue: AlchRuneCost(staff) + 1}
	return s.Normalised()
}

// isPlateName is whether name is one of the round's platebodies.
func isPlateName(name string) bool {
	for _, p := range PlateKinds {
		if strings.EqualFold(strings.TrimSpace(name), p.Name) {
			return true
		}
	}
	return false
}
