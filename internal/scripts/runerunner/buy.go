package runerunner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/thieverrunner"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/tile"
)

// ShopWalkTimeout bounds one walk to a shop: the boat chain from Catherby to Port Sarim is the
// longest leg, 17-20 minutes at the real tick.
const ShopWalkTimeout = 20 * time.Minute

// WalkTries walks to dest on fleetrunner.WalkTo (the wolf- and bear-avoiding webwalk over the
// boat chain), up to tries times, logging each failure. It reports whether the account arrived.
//
// After a failed attempt with less than runerunnerplan.FareFloat in the pack it draws the fares at
// the nearest bank first. LIVE 26 Sep 20:27 on c1ff7c2f: a buyer started beside Varrock east bank
// with no coins in the pack (its float was banked), the Walker left the ship fares out of every
// plan to the Catherby post, and three "no path: unreachable" attempts stopped the script.
func WalkTries(d *fleetrunner.Deps, label string, dest tile.Tile, radius int, timeout time.Duration, tries int) bool {
	for attempt := 1; attempt <= tries; attempt++ {
		if fleetrunner.WalkTo(d, dest, radius, timeout) {
			return true
		}
		logf(d, "runerunner: walk-failed station=%s dest=%d,%d attempt=%d at=%s", label, dest.X, dest.Z, attempt, fleetrunner.HereIs(d))
		drawFares(d, label)
		if !d.Delay(2 * time.Second) {
			return false
		}
	}
	return false
}

// drawFares tops the pack's coins up to runerunnerplan.FareFloat at the nearest bank, when short.
func drawFares(d *fleetrunner.Deps, label string) {
	held := fleetrunner.CoinsHeld(d)
	if runerunnerplan.FareShort(held) == 0 {
		return
	}
	here := fleetrunner.Here(d)
	if here == nil {
		return
	}
	bank := questplan.NearestBank(*here)
	got, why := fleetrunner.WithdrawCoinsTo(d, bank.Tile, runerunnerplan.FareFloat)
	logf(d, "runerunner: fares station=%s bank=%q held=%d now=%d want=%d %s", label, bank.Name, held, got, runerunnerplan.FareFloat, why)
}

// Visit is one shop station's outcome, measured from the shop's own panels.
type Visit struct {
	fleetrunner.ShopVisit
	// Plan is what planFor decided from the shelf actually seen (for the price check).
	Plan fleetplan.PurchasePlan
	// Walked is false when the account never reached the keeper.
	Walked bool
	// OpenFailed is true when the account reached the keeper but the shop would not open (or
	// opened with no stock panel): a strike on the pair.
	OpenFailed bool
}

// BuyAt walks to a site's keeper (two tries) and buys what planFor decides from the stock on the
// shelf and the coins in the pack (fleetrunner.BuyFromKeeperPlanned, which also re-walks beside
// the keeper and retries once when the engine's approach stops short).
func BuyAt(d *fleetrunner.Deps, site runerunnerplan.ShopSite, planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan) Visit {
	var v Visit
	if !WalkTries(d, site.Key, site.Approach, 3, ShopWalkTimeout, 2) {
		v.Reason = "walk-failed"
		return v
	}
	v.Walked = true
	capture := func(seen map[string]int, coins int) fleetplan.PurchasePlan {
		v.Plan = planFor(seen, coins)
		return v.Plan
	}
	v.ShopVisit = fleetrunner.BuyFromKeeperPlanned(d, site.Shop.Keeper, capture)
	if v.Reason != "" && len(v.Seen) == 0 {
		v.OpenFailed = true
	}
	return v
}

// StaffVisit is one Zaff station's outcome, measured from the shop's own panels.
type StaffVisit struct {
	// Seen is the item on Zaff's shelf when the shop opened (-1 when it never opened).
	Seen   int
	Bought int
	Spent  int
	// Plan is what runerunnerplan.StaffPlan decided from Seen (for the price check).
	Plan       fleetplan.PurchasePlan
	Walked     bool
	OpenFailed bool
	Reason     string
}

// BuyStaffs walks to Zaff (two tries) and buys up to `want` Staffs of air, none dearer than capGP
// and within `spendable` coins, planned from the stock actually on his shelf
// (runerunnerplan.StaffPlan). It uses the same shop panel as the rune buys: entityquery.OpenShop
// on the Trade op (Zaff's op3 opens his shop directly), a re-walk beside the keeper and one retry
// on ErrKeeperOutOfReach, then actions.Shop.BuyCount. Bought and Spent are read off the pack.
func BuyStaffs(d *fleetrunner.Deps, want, capGP, spendable int) StaffVisit {
	return BuyFromZaff(d, fleetplan.StaffOfAir, want, capGP, spendable)
}

// BuyFromZaff is BuyStaffs for any of Zaff's lines (a Staff of air, a Battlestaff).
func BuyFromZaff(d *fleetrunner.Deps, item string, want, capGP, spendable int) StaffVisit {
	v := StaffVisit{Seen: -1}
	if !WalkTries(d, runerunnerplan.SiteZaff, runerunnerplan.ZaffStand, 3, ShopWalkTimeout, 2) {
		v.Reason = "walk-failed"
		return v
	}
	v.Walked = true
	keeper := runerunnerplan.Zaff.Keeper
	sh := actions.NewShop(d.Session)
	if !sh.IsOpen() {
		w := thieverrunner.Waiter(fleetrunner.Thiev(d))
		err := entityquery.OpenShop(d.Session, d.Config, keeper, w)
		if errors.Is(err, entityquery.ErrKeeperOutOfReach) {
			if npc, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(keeper).Nearest(); ok {
				fleetrunner.WalkTo(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 1, 20*time.Second)
			}
			err = entityquery.OpenShop(d.Session, d.Config, keeper, w)
		}
		if err != nil {
			v.OpenFailed = true
			v.Reason = fmt.Sprintf("opening %s's shop: %v", keeper, err)
			return v
		}
	}
	defer func() {
		_ = sh.Close()
		d.Delay(600 * time.Millisecond)
	}()
	if !waitUntil(d, 5*time.Second, func() bool { return len(sh.Stock()) > 0 }) {
		v.OpenFailed = true
		v.Reason = "the shop opened with no stock panel"
		return v
	}
	var id int32 = -1
	v.Seen = 0
	for _, row := range sh.Stock() {
		if strings.EqualFold(objName(d, row.ID), item) {
			id, v.Seen = row.ID, int(row.Count)
		}
	}
	v.Plan = runerunnerplan.ZaffPlan(item, v.Seen, want, capGP, spendable)
	if id < 0 || len(v.Plan.Lines) == 0 {
		return v
	}
	coinsBefore := shopCount(sh, fleetrunner.CoinsID)
	held := shopCount(sh, id)
	if _, err := sh.BuyCount(id, int32(v.Plan.Lines[0].Count), thieverrunner.Waiter(fleetrunner.Thiev(d))); err != nil {
		logf(d, "runerunner: buying %d %s from %s: %v", v.Plan.Lines[0].Count, item, keeper, err)
	}
	logf(d, "runerunner: %s", sh.LastPass())
	v.Bought = shopCount(sh, id) - held
	v.Spent = coinsBefore - shopCount(sh, fleetrunner.CoinsID)
	return v
}

// shopCount is how many of an object the shop panel's copy of the pack holds (a staff per slot,
// a stack's count).
func shopCount(sh *actions.Shop, id int32) int {
	n := 0
	for _, it := range sh.Pack() {
		if it.ID == id {
			n += int(it.Count)
		}
	}
	return n
}
