package runerunnerplan

import "fmt"

// The Vial of water shuttle.
//
// a user request: "more vials of water should be bought, acct should run between the bank banking the vials of water
// until the shop is depleted on each world so we can get a decent supply of vials of water to make our antipoisons".
//
// MEASURED (acct, 5 Nov 2026): Aemad's shelf held 498 Vials of water on average at the one world it was visited on, and NONE were
// bought in 11 visits ("capped ... no free pack slot for a new stack"): a Vial of water does not stack, so a visit buys what the pack has
// free slots for, and the pack reached Aemad already holding rune stacks and the Rope.
//
// READ FROM CONTENT 289 (area_ardougne_east/configs/ardougne_east.inv [adventurershop]): vial_water 500, one back every 30 ticks
// (120 an hour a world, so a depleted shelf is worth about 100 a world per hour afterwards), and the bank is 42 tiles from the shop. A
// trip of 24 vials is therefore about a minute, and the first depletion of a world about 20 trips.
//
// The rule below is only whether another trip is worth making; the clicks are internal/bot/runerunner_ammo.go's (vialShuttle).

const (
	// DefaultVialTrips is the vialTrips setting's default: the most bank trips one Aemad visit makes. 0 switches the shuttle off.
	DefaultVialTrips = 24
	// MaxVialTrips is the vialTrips ceiling.
	MaxVialTrips = 60
	// MinVialTrip is the fewest Vials worth a bank trip: under it the walk costs more than the shelf is worth.
	MinVialTrip = 8
)

// ShuttleSite reports whether the site's shelf is shuttled (Aemad's, the one general shelf that sells Vials of water on the circuit).
func (s AmmoSite) ShuttleSite() bool { return s.Key == SiteAemad }

// VialShuttleFirst says whether the pack must be banked BEFORE the first visit: Vials are wanted, the shelf is expected to hold a
// trip's worth and the pack has not the room for one. free is the pack's free slots beyond the reserve.
func VialShuttleFirst(maxTrips int, vialsWanted bool, expected, free int) (bool, string) {
	switch {
	case maxTrips <= 0:
		return false, "vialTrips is 0"
	case !vialsWanted:
		return false, "Vial of water is not wanted here"
	case expected < MinVialTrip:
		return false, fmt.Sprintf("the shelf is expected to hold %d Vials, under %d", expected, MinVialTrip)
	case free >= MinVialTrip:
		return false, fmt.Sprintf("%d free slots already take a trip", free)
	}
	return true, fmt.Sprintf("%d free slots for an expected %d Vials", free, expected)
}

// NextVialTrip says whether to bank the pack and come back for more: left Vials are still on the shelf after trip number `trips`
// (the first visit is trip 1), the budget allows maxTrips and a trip is worth at least MinVialTrip Vials.
func NextVialTrip(left, trips, maxTrips int) (bool, string) {
	switch {
	case maxTrips <= 0:
		return false, "vialTrips is 0"
	case trips >= maxTrips:
		return false, fmt.Sprintf("%d trips made (vialTrips %d), %d Vials left", trips, maxTrips, left)
	case left < MinVialTrip:
		return false, fmt.Sprintf("the shelf is depleted: %d Vials left, under %d", left, MinVialTrip)
	}
	return true, fmt.Sprintf("%d Vials left on the shelf after trip %d of at most %d", left, trips, maxTrips)
}
