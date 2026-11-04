package runerunner

import (
	"errors"
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/runerunnerplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// AmmoVisit is one ammo station's outcome, measured from the shop's own panels.
type AmmoVisit struct {
	fleetrunner.ShopVisit
	// Plan is what planFor decided from the shelf actually seen (for the price check).
	Plan fleetplan.PurchasePlan
	// Walked is false when the account never reached the keeper's stand.
	Walked bool
	// OpenFailed is true when the account reached the stand but the shop would not open (or opened with no
	// stock panel): a strike on the pair.
	OpenFailed bool
}

// ammoOpenTries is how many times a station asks the keeper to open the shop, walking beside him between
// the tries. LIVE (MasterCrafter, 5 October): Hickton would not open on about two visits in six ("Hickton is 2
// tiles away at 2823,3442 ... keeper is out of reach"): these keepers wander indoors (moverestrict=indoors),
// and OpenShop only trades from a tile touching the keeper.
const ammoOpenTries = 3

// BuyAmmoAt walks to an ammo shop's stand (two tries), opens the keeper's shop and buys what planFor decides
// from the shelf actually on it and the coins actually in the pack, in the plan's priority order. It is the
// rune buy (scarabrunner.BuyFromKeeperPlanned) for items that are not runes: the same OpenShop on the Trade op,
// the same re-walk beside the keeper, the same actions.Shop.BuyCount (five clicks a tick), the same
// measurement off the pack. Seen holds every line the shop sells that its panel showed, by name; Bought and
// Spent are pack deltas.
func BuyAmmoAt(d *fleetrunner.Deps, site runerunnerplan.AmmoSite, planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan) AmmoVisit {
	var v AmmoVisit
	v.ShopVisit = fleetrunner.ShopVisit{Seen: map[string]int{}, Bought: map[string]int{}}
	if !WalkTries(d, site.Key, site.Stand, 3, ShopWalkTimeout, 2) {
		v.Reason = "walk-failed"
		return v
	}
	v.Walked = true
	keeper := site.Shop.Keeper
	altKeeper := site.AltKeeper
	sh := actions.NewShop(d.Session)
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if !sh.IsOpen() {
		var err error
		for try := 1; try <= ammoOpenTries; try++ {
			err = entityquery.OpenShop(d.Session, d.Config, keeper, w)
			if err == nil || !errors.Is(err, entityquery.ErrKeeperOutOfReach) || try == ammoOpenTries {
				break
			}
			if npc, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(keeper).Nearest(); ok {
				logf(d, "runerunner: %s is out of reach at %d,%d (try %d of %d): walking beside him", keeper, npc.X, npc.Z, try, ammoOpenTries)
				fleetrunner.WalkTo(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 1, 20*time.Second)
			}
		}
		if err != nil && altKeeper != "" {
			// Aemad and Kortan run one shelf: the second keeper answers when the first wandered out of reach.
			logf(d, "runerunner: %s's shop would not open (%v): trying %s", keeper, err, altKeeper)
			if altErr := entityquery.OpenShop(d.Session, d.Config, altKeeper, w); altErr == nil {
				err, keeper = nil, altKeeper
			}
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
	idOf := map[string]int32{}
	for _, row := range sh.Stock() {
		name := objName(d, row.ID)
		if l, ok := site.Shop.Line(name); ok {
			v.Seen[l.Rune] = int(row.Count)
			idOf[l.Rune] = row.ID
		}
	}
	coinsBefore := shopCount(sh, fleetrunner.CoinsID)
	v.Plan = planFor(v.Seen, int(coinsBefore))
	for _, line := range v.Plan.Lines {
		id, ok := idOf[line.Rune]
		if !ok || v.Seen[line.Rune] <= 0 || line.Count <= 0 {
			continue
		}
		held := shopCount(sh, id)
		if _, err := sh.BuyCount(id, int32(line.Count), w); err != nil {
			logf(d, "runerunner: buying %d %s from %s: %v", line.Count, line.Rune, keeper, err)
		}
		logf(d, "runerunner: %s (%s)", sh.LastPass(), line.Rune)
		if got := shopCount(sh, id) - held; got > 0 {
			v.Bought[line.Rune] += got
		}
	}
	v.Spent = int(coinsBefore - shopCount(sh, fleetrunner.CoinsID))
	return v
}
