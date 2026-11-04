package bot

import (
	"sync"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
)

// THE SHOP PURCHASES CARD: every item any script buys from a shop, with the coins it cost, recorded
// without touching any script (a user request: "we want a loot panel for all things bought in
// shops, 1 combined panel in the loot tracker"). internal/loot/shop.go says what the card is.
//
// While a shop is open (the main modal is actions.ShopRoot) the watch reads, once a tick, the pack the
// shop window shows (actions.ShopPack) and diffs it with the tick before. Coins leaving it and items
// arriving in it are a purchase. The coins and the items of one click can land a tick apart, and a
// buying spell clicks several ticks running, so both halves accumulate while the pack keeps changing
// and the purchase is recorded on the first tick it stands still. A half that never finds the other
// within shopUnpairedTicks is dropped: a sale (items leave, coins arrive) and anything else that is
// not a purchase never reach the card.
//
// THE FIRST READING. The shop window's pack is its own component, which keeps whatever it held at the
// last shop until the server's full update for this one lands. Diffing against that stale copy would
// read every item gained since the last visit as bought, so the watch takes no baseline until the
// shop window's pack reads the same as the real backpack (lootPackComponent), which it does as soon
// as the full update is in.

// shopUnpairedTicks is how many quiet ticks a half purchase (coins with no items, or items with no
// coins) waits for its other half before it is dropped.
const shopUnpairedTicks = 3

// shopWatch pairs coins leaving a shop window's pack with items arriving in it. Pure apart from its
// record callback, so it is tested by feeding it readings.
type shopWatch struct {
	mu     sync.Mutex
	prev   loot.Counts // nil until a baseline is taken in this shop
	coins  int64       // coins paid since the last record
	gained loot.Counts // items arrived since the last record
	quiet  int         // ticks the pack has stood still
	record func(gained loot.Counts, coins int64)
}

func newShopWatch(record func(gained loot.Counts, coins int64)) *shopWatch {
	return &shopWatch{record: record, gained: loot.Counts{}}
}

// observe takes one tick's reading: whether a shop is open, the shop window's pack and the backpack.
func (w *shopWatch) observe(open bool, shopPack, backpack loot.Counts) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !open {
		w.flushLocked()
		w.prev = nil
		w.resetLocked()
		return
	}
	if w.prev == nil {
		if sameCounts(shopPack, backpack) {
			w.prev = shopPack
		}
		return
	}
	paid := w.prev[loot.CoinsID] - shopPack[loot.CoinsID]
	changed := false
	if paid > 0 {
		w.coins += paid
	}
	for id, n := range shopPack {
		if d := n - w.prev[id]; d != 0 {
			changed = true
			if d > 0 && id != loot.CoinsID {
				w.gained[id] += d
			}
		}
	}
	for id := range w.prev {
		if _, ok := shopPack[id]; !ok {
			changed = true
		}
	}
	w.prev = shopPack
	if changed {
		w.quiet = 0
		return
	}
	w.quiet++
	if w.coins > 0 && len(w.gained) > 0 {
		w.flushLocked()
		w.resetLocked()
		return
	}
	if w.quiet >= shopUnpairedTicks {
		w.resetLocked()
	}
}

func (w *shopWatch) flushLocked() {
	if w.coins > 0 && len(w.gained) > 0 && w.record != nil {
		w.record(w.gained, w.coins)
	}
}

func (w *shopWatch) resetLocked() {
	w.coins, w.gained, w.quiet = 0, loot.Counts{}, 0
}

// sameCounts reports whether two readings hold the same items in the same counts.
func sameCounts(a, b loot.Counts) bool {
	if len(a) != len(b) {
		return false
	}
	for id, n := range a {
		if b[id] != n {
			return false
		}
	}
	return true
}

// watchShopPurchases starts the shop watch on this Context's session, for as long as the Runner runs.
// A no-op with no loot sink or no session. The subscription is made through On, so the Runner's
// disposeSubscriptions cancels it with the others.
func (c *Context) watchShopPurchases() {
	if c == nil || c.Loot == nil || c.gameState() == nil {
		return
	}
	st := c.gameState()
	w := newShopWatch(func(gained loot.Counts, coins int64) {
		for _, d := range loot.ShopPurchases(loot.Items(c.Config, gained), coins) {
			c.Loot(d)
			c.logf("loot: bought %d x %s for %d coins", d.Events, d.Source.Name, d.Spent[0].Count)
		}
	})
	c.On(func(ev gamestate.Event) {
		main, _, _ := st.Modals()
		if main != actions.ShopRoot {
			w.observe(false, nil, nil)
			return
		}
		w.observe(true, loot.PackCounts(st.Inventory(actions.ShopPack)), loot.PackCounts(st.Inventory(lootPackComponent)))
	}, gamestate.EventTick)
}
