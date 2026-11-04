package crafterrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
)

// waiter adapts a Deps' Delayer to actions.Waiter, so the confirmation waits in actions (note mode,
// Make-X, shop quantities) stop with the bot.
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

// Fresh is d as the shared first-day runner's Deps. The Walker goes along so a walk that finds
// itself fenced in hands over to the door-aware webwalk route (freshstartrunner's STRANDED rescue;
// live 6 Oct 2026, the account "acct" at the Rogues' Castle).
func Fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: d.Walker,
	}
}

func packSlots(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

func nameOf(d *Deps, id int32) string {
	if obj, ok := d.Config.Obj(int(id)); ok {
		return strings.TrimSpace(obj.Name)
	}
	return ""
}

// slotsNamed is every pack slot whose item is named one of names, lowest slot first.
func slotsNamed(d *Deps, names []string) []gamestate.SlotItem {
	var out []gamestate.SlotItem
	for _, it := range packSlots(d) {
		n := nameOf(d, it.ID)
		for _, want := range names {
			if strings.EqualFold(n, want) {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// firstSlotNamed is the first pack slot whose item is named one of names.
func firstSlotNamed(d *Deps, names []string) (gamestate.SlotItem, bool) {
	for _, it := range packSlots(d) {
		n := nameOf(d, it.ID)
		for _, want := range names {
			if strings.EqualFold(n, want) {
				return it, true
			}
		}
	}
	return gamestate.SlotItem{}, false
}

// Count totals the pack's items named name, notes included.
func Count(d *Deps, name string) int { return countOfName(d, name) }

// UsedSlots is how many pack slots hold something.
func UsedSlots(d *Deps) int { return len(packSlots(d)) }

// PackSlotRows reads the pack one row per slot, as the TS hands Inventory.items() to
// muleHaul/muleLoad.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	var out []donorplan.ItemRow
	for _, it := range packSlots(d) {
		name := nameOf(d, it.ID)
		if name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: name, ID: int(it.ID), Count: c})
	}
	return out
}

// bankItemRows reads the open bank as one row per (name, id).
func bankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	var out []donorplan.ItemRow
	index := map[int32]int{}
	for _, it := range bank.Items() {
		name := nameOf(d, it.ID)
		if name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		if i, ok := index[it.ID]; ok {
			out[i].Count += c
			continue
		}
		index[it.ID] = len(out)
		out = append(out, donorplan.ItemRow{Name: name, ID: int(it.ID), Count: c})
	}
	return out
}
