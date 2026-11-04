package actions

import (
	"time"

	"rsb0t-go/internal/gamestate"
)

// Shop interface component ids, hardcoded literals matching rsb0t-main's own
// src/bot/api/shop/Shop.ts against this exact content build — not dynamically
// resolved, the same reasoning that made Trade's ids implementable without the
// interface archive: these are already proven-correct production values, not
// something this session derived. Option layout confirmed against the content
// source (content/scripts/shop/interfaces/shop_template[.side].if):
// stock option1=Value option2="Buy 1" option3="Buy 5" option4="Buy 10";
// pack option1=Value option2="Sell 1" option3="Sell 5" option4="Sell 10".
const (
	ShopRoot  = 3824 // main modal while a shop is open
	ShopStock = 3900 // the shopkeeper's stock inv
	ShopPack  = 3823 // the player's own pack, shown alongside the stock

	shopBuy1   = 2
	shopBuy5   = 3
	shopBuy10  = 4
	shopSell1  = 2
	shopSell5  = 3
	shopSell10 = 4
)

// shopSteps mirrors Shop.ts's SHOP_STEPS: the engine reads USER_OPS_PER_TICK user events a tick
// and queues the rest, so a large quantity is split into individual Buy-10/Buy-5/Buy-1 clicks
// (there is no bigger Buy-N option) sent five to a tick.
var shopSteps = []struct {
	qty    int32
	option int
}{
	{10, shopBuy10},
	{5, shopBuy5},
	{1, shopBuy1},
}

var shopSellSteps = []struct {
	qty    int32
	option int
}{
	{10, shopSell10},
	{5, shopSell5},
	{1, shopSell1},
}

// Shop is the Shop.* surface from rsb0t-main's api, bound to a session.
type Shop struct {
	s Session
	// last is the most recent BuyCount/SellCount (shop_quantity.go), for LastPass.
	last shopPass
}

func NewShop(s Session) *Shop { return &Shop{s: s} }

func (sh *Shop) IsOpen() bool {
	main, _, _ := sh.s.State().Modals()
	return main == ShopRoot
}

// Stock returns the shopkeeper's current stock.
func (sh *Shop) Stock() []gamestate.SlotItem { return items(sh.s.State(), ShopStock) }

// Pack returns the player's own inventory as shown in the shop's side panel.
func (sh *Shop) Pack() []gamestate.SlotItem { return items(sh.s.State(), ShopPack) }

// Buy purchases up to n of objID from stock, batched into Buy-10/5/1 clicks the way a
// real client's shop panel is limited to, stopping early if the stock runs out.
func (sh *Shop) Buy(objID int32, n int32) error {
	return sh.trade(objID, n, ShopStock, shopSteps, sh.Stock)
}

// Sell offers up to n of objID from the player's own pack, same batching as Buy.
func (sh *Shop) Sell(objID int32, n int32) error {
	return sh.trade(objID, n, ShopPack, shopSellSteps, sh.Pack)
}

func (sh *Shop) trade(objID, n int32, comID int, steps []struct {
	qty    int32
	option int
}, side func() []gamestate.SlotItem) error {
	if !sh.IsOpen() {
		return ErrNotOnScreen
	}
	remaining := n
	for remaining > 0 {
		var slot int
		found := false
		for _, it := range side() {
			if it.ID == objID {
				slot = it.Slot
				found = true
				break
			}
		}
		if !found {
			return ErrItemNotFound
		}
		clicked := false
		for _, step := range steps {
			if step.qty > remaining {
				continue
			}
			if err := InvButton(sh.s, step.option, objID, slot, comID); err != nil {
				return err
			}
			remaining -= step.qty
			clicked = true
			break
		}
		if !clicked {
			// remaining < the smallest step (1) never happens since qty=1 always
			// matches; this guards against a future steps table that omits it.
			break
		}
		time.Sleep(150 * time.Millisecond) // let the tick settle before rechecking slots
	}
	return nil
}

func (sh *Shop) Close() error { return CloseModal(sh.s) }
