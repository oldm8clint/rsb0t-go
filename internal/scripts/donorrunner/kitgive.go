package donorrunner

// THE HAND-OVER THAT RIDES THE KIT'S OWN TRADE (a user request: "the best time accounts should be handing over
// to the mule [is when they ask for a kit]"). Live on 2 Nov 2026 the kit ask's hand-over was a SEPARATE trade in
// the mule's line before the kit order, and it gave up when the line stayed over two for 90 seconds: 184 of 324
// hand-overs in nine hours were drawn from the bank, never traded, and banked again. Now the donor draws its
// hand-over before it orders, says so on the order (mulebus.WithGive), and puts it on its own side of the kit
// trade: one trade both ways, in the kit's own place in the line.
//
// The trade screen allows it: offering moves an item out of the pack into the trade inventory, and each side's
// "enough inventory space" check (content interface_trade/trade.rs2) counts the free slots of the pack as it then
// stands, so a hand-over that empties a stack makes room for the kit coming the other way.

import (
	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
)

// GiveLine is one stack the donor hands the mule in the kit's trade: its obj id as it sits in the pack (a bank
// note for a noted line) and how many.
type GiveLine struct {
	ID  int32
	Qty int
}

// GiveFreed is how many pack slots offering give empties: every slot of an id whose whole pack count is given.
// An id given only in part (coins drawn onto the purse the account keeps) frees nothing.
func GiveFreed(pack []gamestate.SlotItem, give []GiveLine) int {
	want := map[int32]int{}
	for _, g := range give {
		if g.Qty > 0 {
			want[g.ID] += g.Qty
		}
	}
	held, slots := map[int32]int{}, map[int32]int{}
	for _, it := range pack {
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		held[it.ID] += c
		slots[it.ID]++
	}
	n := 0
	for id, q := range want {
		if held[id] > 0 && held[id] <= q {
			n += slots[id]
		}
	}
	return n
}

// GiveHeld is give cut to what the pack holds (a line the pack lacks is dropped).
func GiveHeld(pack []gamestate.SlotItem, give []GiveLine) []GiveLine {
	held := map[int32]int{}
	for _, it := range pack {
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		held[it.ID] += c
	}
	var out []GiveLine
	for _, g := range give {
		n := min(g.Qty, held[g.ID])
		if n > 0 {
			out = append(out, GiveLine{ID: g.ID, Qty: n})
			held[g.ID] -= n
		}
	}
	return out
}

// giveBus is give as the kit order carries it (names), and the mule pack slots it takes: a stackable or noted
// stack one slot, anything else a slot a unit. A line the config cannot name is dropped from kept as well, since
// the mule judges the screen by name.
func giveBus(cfg *cachecfg.Config, give []GiveLine) (kept []GiveLine, lines []mulebus.KitLine, slots int) {
	for _, g := range give {
		name, stacks, noted := "", false, false
		if cfg != nil {
			if obj, ok := cfg.Obj(int(g.ID)); ok {
				name, stacks, noted = obj.Name, obj.Stackable, obj.Noted()
			}
		}
		if name == "" || g.Qty <= 0 {
			continue
		}
		kept = append(kept, g)
		lines = append(lines, mulebus.KitLine{Item: name, Qty: g.Qty, Noted: noted})
		if stacks || noted {
			slots++
		} else {
			slots += g.Qty
		}
	}
	return kept, lines, slots
}

// giveTradeLines is give as trade-screen offers: Offer-All when the pack holds no more of it than is given.
func giveTradeLines(pack []gamestate.SlotItem, give []GiveLine) []actions.TradeLine {
	held := map[int32]int{}
	for _, it := range pack {
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		held[it.ID] += c
	}
	out := make([]actions.TradeLine, 0, len(give))
	for _, g := range give {
		out = append(out, actions.TradeLine{ID: g.ID, Qty: int32(g.Qty), All: held[g.ID] <= g.Qty})
	}
	return out
}

// giveSlotItems is give as slot items, for naming through handoutRows.
func giveSlotItems(give []GiveLine) []gamestate.SlotItem {
	out := make([]gamestate.SlotItem, 0, len(give))
	for _, g := range give {
		out = append(out, gamestate.SlotItem{Item: gamestate.Item{ID: g.ID, Count: int32(g.Qty)}})
	}
	return out
}
