package fleetrunner

import (
	"errors"
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Shops is the process-wide board. Every Scarab account in a fleet process shares it, exactly as
// the woodcutters share one turn lock per market stall: the accounts it coordinates all live in
// this process, and the shops it describes are shared by all of them.
var Shops = fleetplan.NewShopBoard()

// BuyFromKeeperPlanned opens a rune shop by its keeper's name and buys what planFor decides from
// the stock actually on the shelf and the coins actually in the pack. It is
// BuyFromLundailPlanned's body with the keeper as a parameter (that function is Lundail-only by
// name), so the Aubury purchase reads the shop's own stock panel the same way -- which is what the
// board needs and what combatrunner.BuyFrom, the old Aubury buyer, does not report.
//
// The caller must already be standing near the keeper.
func BuyFromKeeperPlanned(d *Deps, keeper string, planFor func(seen map[string]int, coins int) fleetplan.PurchasePlan) ShopVisit {
	v := ShopVisit{Seen: map[string]int{}, Bought: map[string]int{}}
	sh := actions.NewShop(d.Session)
	if !sh.IsOpen() {
		err := entityquery.OpenShop(d.Session, d.Config, keeper, thieverrunner.Waiter(Thiev(d)))
		if errors.Is(err, entityquery.ErrKeeperOutOfReach) {
			if npc, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(keeper).Nearest(); ok {
				WalkTo(d, tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}, 1, 20*time.Second)
			}
			err = entityquery.OpenShop(d.Session, d.Config, keeper, thieverrunner.Waiter(Thiev(d)))
		}
		if err != nil {
			v.Reason = fmt.Sprintf("opening %s's shop: %v", keeper, err)
			return v
		}
	}
	if !WaitFor(d, keeper+"'s stock", 5*time.Second, func() bool { return len(sh.Stock()) > 0 }) {
		v.Reason = "the shop opened with no stock panel"
		return v
	}
	idOf := map[string]int32{}
	for _, row := range sh.Stock() {
		n := ObjName(d, row.ID)
		if fleetplan.IsRuneName(n) {
			v.Seen[n] = int(row.Count)
			idOf[n] = row.ID
		}
	}
	coinsBefore := countItem(sh.Pack(), CoinsID)
	plan := planFor(v.Seen, int(coinsBefore))
	for _, line := range plan.Lines {
		id, ok := idOf[line.Rune]
		if !ok || v.Seen[line.Rune] <= 0 {
			continue
		}
		held := countItem(sh.Pack(), id)
		if _, err := sh.BuyCount(id, int32(line.Count), thieverrunner.Waiter(Thiev(d))); err != nil {
			d.Logf("shop: buying %d %s from %s: %v", line.Count, line.Rune, keeper, err)
		}
		// BuyCount streams five clicks a tick and settles on the pack before it returns (twopertick-1012), so the
		// 700 ms that used to follow every line is not needed to read what landed.
		d.Logf("%s (%s)", sh.LastPass(), line.Rune)
		if got := int(countItem(sh.Pack(), id) - held); got > 0 {
			v.Bought[line.Rune] += got
		}
	}
	v.Spent = int(coinsBefore - countItem(sh.Pack(), CoinsID))
	_ = sh.Close()
	d.Delay(600 * time.Millisecond)
	return v
}

// ObserveVisit records what a visit saw into the board: the stock LEFT on each line once this
// account had bought, which is what the next account's estimate should start from.
func ObserveVisit(board *fleetplan.ShopBoard, shop fleetplan.RuneShop, v ShopVisit, at time.Time, me string) {
	if board == nil {
		return
	}
	for r, n := range v.Seen {
		board.Observe(shop, r, n-v.Bought[r], at, me)
	}
}
