package mastercraftrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
)

// FireStaffCoins is the coins a Staff of fire purchase carries: Zaff's price (staff_of_fire cost
// 1500 at his baseline of 2, area_varrock/configs/varrock.inv [staffshop] stock6=staff_of_fire,2,1000)
// with room for a drained shelf.
const FireStaffCoins = 3000

// seersMarket is the market account the owner named for a Staff of fire, and where it stands.
const seersMarket = "Seers Market"

// BuyFireStaff buys a Staff of fire with the coins in the pack: from the Seers market first, for at
// most maxPrice (scarabrunner.BuyFromMarket: it never pays without a stated price and a trade
// screen that matches it), else from Zaff in Varrock (combatrunner.BuyFrom, the path ScarabSwarm's
// own BuyFireStaffFromZaff uses). It returns the staff's name when one is in the pack, or why not.
func BuyFireStaff(d *fleetrunner.Deps, maxPrice int) (string, string) {
	var why []string
	if place, ok := fleetplan.DefaultMarketPlaces[seersMarket]; ok && fleetrunner.CoinsHeld(d) > 0 {
		if fleetrunner.WalkTo(d, place, 3, ShopWalk) {
			r := fleetrunner.BuyFromMarket(d, seersMarket, mastercraftplan.StaffOfFire, 1, min(maxPrice, fleetrunner.CoinsHeld(d)), 90*time.Second)
			if r.Got > 0 {
				return mastercraftplan.StaffOfFire, ""
			}
			why = append(why, "Seers market: "+r.Reason)
		} else {
			why = append(why, "could not reach the Seers market")
		}
	}
	if !fleetrunner.WalkTo(d, fleetplan.ZaffStand, 3, 20*time.Minute) {
		return "", strings.Join(append(why, "could not reach Zaff"), "; ")
	}
	res, err := combatrunner.BuyFrom(fleetrunner.Combat(d), fleetrunner.ZaffSite(), mastercraftplan.StaffOfFire, 1)
	if err == nil && res.Bought > 0 {
		return mastercraftplan.StaffOfFire, ""
	}
	if err != nil {
		why = append(why, "Zaff: "+err.Error())
	}
	if res.Said != "" {
		why = append(why, "Zaff: "+res.Said)
	}
	return "", strings.Join(why, "; ")
}
