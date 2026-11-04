package mastercraftrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/shopplan"
	"rsb0t-go/internal/scripts/shoprunner"
)

// The shop trip's I/O (mastercraftplan/supply.go decides it). The buying is ShopBuyout's own code:
// shoprunner.OpenKeeperShop opens the keeper, shoprunner.StockByID reads the real shelf,
// shopplan.UnitsUnderCap plans each line under the price cap and the coins held, and
// shoprunner.BuyWant buys it and measures what really changed hands.

// ShopDeps adapts d to shoprunner's Deps.
func ShopDeps(d *fleetrunner.Deps) *shoprunner.Deps {
	return &shoprunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config, Components: d.Components,
		Chat: d.Chat, Delay: shoprunner.Delayer(d.Delay), Log: d.Log, Walker: d.Walker,
	}
}

// ShopWalk is how long the walk to one shop may take: Lowe in Varrock is ~600 tiles from the mule's
// Catherby post.
const ShopWalk = 12 * time.Minute

// ShopVisit is what one shop visit came to.
type ShopVisit struct {
	// Bought and Spent are by obj id: what landed in the pack and the coins it cost (measured).
	Bought map[int32]int
	Spent  int
	// Left is the shelf after the visit, by obj id, for every item of the shop the trip wanted.
	Left map[int32]int32
	// Opened says the shop opened; Why is the reason it did not, or what cut it short.
	Opened bool
	Why    string
}

// BuyAtShop walks to shop's stand, opens the keeper's shop and buys, for every item of the shop
// want names, as many as the shelf holds while the next unit costs at most maxUnit and the coins in
// the pack last, then closes the shop.
func BuyAtShop(d *fleetrunner.Deps, shop shopplan.Shop, want func(id int32) bool, maxUnit int32) ShopVisit {
	v := ShopVisit{Bought: map[int32]int{}, Left: map[int32]int32{}}
	if !fleetrunner.WalkTo(d, shop.ShopStand, 3, ShopWalk) {
		v.Why = fmt.Sprintf("could not reach %s at %d,%d from %s", shop.Keeper, shop.ShopStand.X, shop.ShopStand.Z, fleetrunner.HereIs(d))
		return v
	}
	sd := ShopDeps(d)
	sh, ok := shoprunner.OpenKeeperShop(sd, shop)
	if !ok || sh == nil {
		v.Why = "could not open " + shop.Keeper + "'s shop"
		return v
	}
	v.Opened = true
	defer func() {
		_ = actions.CloseModal(d.Session)
		d.Delay(600 * time.Millisecond)
	}()
	stock := shoprunner.StockByID(sh, shop)
	for _, it := range shop.Items {
		if !want(it.ID) {
			continue
		}
		coins := int32(fleetrunner.CoinsHeld(d))
		units, est := shopplan.UnitsUnderCap(it, shop, stock[it.ID], maxUnit, coins)
		if units > 0 {
			r := shoprunner.BuyWant(sd, sh, shopplan.Want{Item: it, Units: units, EstCost: est})
			if r.Bought > 0 {
				v.Bought[it.ID] += int(r.Bought)
				v.Spent += int(r.Spent)
			}
			logf(d, "mastercrafter: shop: %s: bought %d of %d %s planned for %d gp (shelf %d, cap %d gp a unit): %s",
				shop.Keeper, r.Bought, units, it.Name, r.Spent, stock[it.ID], maxUnit, r.Reason)
		}
		v.Left[it.ID] = stock[it.ID]
	}
	after := shoprunner.StockByID(sh, shop)
	for id := range v.Left {
		v.Left[id] = after[id]
	}
	return v
}
