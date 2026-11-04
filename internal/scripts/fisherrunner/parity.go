package fisherrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// The pieces ProgressiveFisher.ts's sell phase and noted hand-over need on top of runner.go.

// waiter adapts a Deps' Delayer to actions.Waiter, so the confirmation waits in actions (note
// mode, Withdraw X, SellCount) stop with the bot.
type waiter struct{ d *Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }

func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !w.d.Delay(100 * time.Millisecond) {
			return false
		}
	}
}

func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

// Waiter is d as an actions.Waiter.
func Waiter(d *Deps) actions.Waiter { return waiter{d} }

// PackSlotRows reads the player's own pack one row per slot, as the TS scripts hand
// Inventory.items() to muleHaul/muleLoad. PackItemRows merges a stack of unstackable fish into one
// row, so a haul counted from it reads 27 raw fish as one slot and a trade plan from it thinks they
// fit one of the mule's free slots.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []donorplan.ItemRow
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: obj.Name, ID: int(it.ID), Count: c})
	}
	return out
}

// PackCountByName totals the pack's items named name, notes included (a note carries its item's
// name).
func PackCountByName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 28
	}
	free := 28 - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// SetNoteMode sets the open bank's withdraw mode, confirmed by the bankcert varp.
func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// WithdrawAllByName withdraws every one of name with Withdraw-All and waits until the pack holds
// more of that name. Unlike WithdrawAllOf it counts by name, so it confirms a withdrawal that lands
// as a note (a different id) in note mode.
func WithdrawAllByName(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := PackCountByName(d, name)
		used := len(bank.Inventory())
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return PackCountByName(d, name) > before || len(bank.Inventory()) > used
		})
	}
	return false
}

// WithdrawXByName withdraws n of name (as items, whatever the note mode was left at is the
// caller's to set) and waits for them.
func WithdrawXByName(d *Deps, bank *actions.Bank, name string, n int) error {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !strings.EqualFold(obj.Name, name) {
			continue
		}
		return bank.WithdrawX(it.ID, int32(n), Waiter(d))
	}
	return fmt.Errorf("%q not in the bank", name)
}

// BankStock reads an open bank as name/count lines for fisherplan.FishToSell.
func BankStock(d *Deps, bank *actions.Bank) []freshstart.PackItem {
	var out []freshstart.PackItem
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, freshstart.PackItem{Name: obj.Name, Count: c})
	}
	return out
}

// OpenShopNear finds npcName within 15 tiles of stand, walks beside it (the engine's
// clientRoutefinder does not walk to an NPC clicked from further away), opens its shop through
// its Trade op and waits for the shop to show. BuyTool's opening, shared with the sell phase.
func OpenShopNear(d *Deps, npcName string, stand tile.Tile) (*actions.Shop, bool) {
	shop, unlock, _ := openShopGuarded(d, npcName, stand)
	if shop == nil {
		return nil, false
	}
	unlock()
	return shop, true
}

// SellToShop sells up to n of the unnoted item named name from the pack to an open shop, and
// returns how many left the pack and the coins gained.
func SellToShop(d *Deps, shop *actions.Shop, name string, n int) (sold int, gained int, err error) {
	var id int32 = -1
	for _, it := range shop.Pack() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && strings.EqualFold(obj.Name, name) {
			id = it.ID
			break
		}
	}
	if id < 0 {
		return 0, 0, fmt.Errorf("no %q in the pack", name)
	}
	before := Coins(d)
	got, err := shop.SellCount(id, int32(n), Waiter(d))
	// Coins land a tick after the stock moves.
	d.Delay(600 * time.Millisecond)
	return int(got), Coins(d) - before, err
}

// spotUsable reports whether a visible fishing spot is fishable, not a whirlpool.
func spotUsable(npcType int) bool { return !fisherplan.IsWhirlpool(npcType) }
