package fisherrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/tile"
)

// BuyResult is what one visit to Harry's counter came to.
type BuyResult struct {
	// Bought names the tool that landed in the pack; empty when nothing did.
	Bought string
	// Opened is true once the shop interface showed.
	Opened bool
	// OutOfStock is true when the shop listed the wanted tool with none left (or not at all).
	// Harry's Big net stock is 5 restocking one a minute (content catherby.inv), so after
	// 2026-09-20's 72 lost nets this was the state for over an hour.
	OutOfStock bool
	// Yielded is true when a random event appeared before the shop was reached, and the visit
	// stood down for the Guardian instead of clicking over it.
	Yielded bool
}

// approachShopkeeper walks the account beside npc for its Trade op. The engine does not walk
// to an NPC clicked from further away (see OpenShopNear), so this has to arrive first. The White
// wolf by Harry's shop no longer wedges this walk: internal/hostileevade now ignores a hostile
// whose own walking route to the account is far longer than its hunt range (walledOff).
func approachShopkeeper(d *Deps, npc gamestate.NPC) bool {
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if tile.Chebyshev(currentTile(d), at) <= 1 {
		return true
	}
	return WalkNear(d, at, 1)
}

// openShopGuarded is the shared shop opening for buying and selling: find the keeper, walk
// beside him, then — only once the Guardian has nothing pending — hold the action lock while
// the Trade op goes out and the shop opens, the way BankFish holds it for a bank.
//
// Live 2026-09-20 (acct84): a Genie arrived while the account looped at Harry's out-of-stock
// counter; the loop kept clicking Harry between the Guardian's Genie attempts, the Guardian gave
// up after four, and the server fail-teleported the account to Brimhaven, where it died. A
// random event now always wins: yielded=true and nothing is clicked.
func openShopGuarded(d *Deps, npcName string, stand tile.Tile) (shop *actions.Shop, unlock func(), yielded bool) {
	if randomevents.Busy(d.Session) {
		return nil, nil, true
	}
	npc, ok := findNpcByName(d, npcName, stand, 15, nil)
	if !ok {
		d.logf("could not find %s near %d,%d", npcName, stand.X, stand.Z)
		return nil, nil, false
	}
	d.logf("found %s (index=%d type=%d) at %d,%d", npcName, npc.Index, npc.Type, npc.X, npc.Z)
	if !approachShopkeeper(d, npc) {
		d.logf("could not walk to %s", npcName)
		return nil, nil, randomevents.Busy(d.Session)
	}
	if randomevents.Busy(d.Session) {
		return nil, nil, true
	}
	unlock = randomevents.Lock(d.Session)
	npcType, _ := d.Config.Npc(npc.Type)
	tradeOp := cachecfg.OpIndex(npcType.Ops, "Trade")
	if tradeOp == 0 {
		tradeOp = 3 // matches cmd/shoptest's own fallback for this content build
	}
	shop = actions.NewShop(d.Session)
	if err := actions.OpNpc(d.Session, tradeOp, npc.Index); err != nil {
		unlock()
		d.logf("OpNpc(Trade) on %s: %v", npcName, err)
		return nil, nil, false
	}
	if !waitFor(d, npcName+"'s shop to open", 5*time.Second, shop.IsOpen) {
		unlock()
		return nil, nil, false
	}
	return shop, unlock, false
}

// shopStockOf is how many of the tool the open shop has; ok is false when it is not listed.
func shopStockOf(d *Deps, shop *actions.Shop, name string) (id int32, count int, ok bool) {
	for _, it := range shop.Stock() {
		obj, found := d.Config.Obj(int(it.ID))
		if found && strings.EqualFold(obj.Name, name) && !obj.Noted() {
			return it.ID, int(it.Count), true
		}
	}
	return -1, 0, false
}

// BuyToolOr buys one tool from Harry: the first of tools he has in stock that coins cover, in
// the order given. The caller puts the wanted rung's tool first and a cheaper fallback after
// it (live 2026-09-20: with Harry's Big nets gone, a small net keeps an account fishing while
// his stock refills, instead of it standing at the counter).
func BuyToolOr(d *Deps, near tile.Tile, tools ...string) BuyResult {
	return BuyFrom(d, fisherplan.Harry, near, tools...)
}

// BuyFrom buys one of the first of items the named shopkeeper (found within 15 tiles of near) has in
// stock and the coins cover, in the order given: BuyToolOr's whole visit for any shop. BassOysterCaskets
// buys its chisel from Arhein's general store with it.
func BuyFrom(d *Deps, shopkeeper string, near tile.Tile, tools ...string) BuyResult {
	shop, unlock, yielded := openShopGuarded(d, shopkeeper, near)
	if shop == nil {
		return BuyResult{Yielded: yielded}
	}
	defer unlock()
	defer shop.Close()
	res := BuyResult{Opened: true}
	for i, tool := range tools {
		id, stock, listed := shopStockOf(d, shop, tool)
		if !listed || stock <= 0 {
			if i == 0 {
				res.OutOfStock = true
				d.logf("%s has no %s in stock", shopkeeper, tool)
			}
			continue
		}
		before := packCountByID(shop.Pack(), id)
		if err := shop.Buy(id, 1); err != nil {
			d.logf("Buy %s: %v", tool, err)
			continue
		}
		if waitFor(d, strings.ToLower(tool)+" in pack after buying", 5*time.Second, func() bool {
			return packCountByID(shop.Pack(), id) > before
		}) {
			res.Bought = tool
			return res
		}
		if i == 0 {
			// Listed and in stock but nothing arrived: dearer than the coins held, or the last
			// one went to someone else first. Either way, not this visit.
			res.OutOfStock = true
		}
	}
	return res
}
