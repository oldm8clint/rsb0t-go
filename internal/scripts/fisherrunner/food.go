package fisherrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/freshstart"
)

// EdibleFood is the cooked food in the pack as name/count lines, one count per item. Notes are left
// out: a noted food carries its item's name but cannot be eaten.
func EdibleFood(d *Deps) []freshstart.PackItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return nil
	}
	index := map[string]int{}
	var out []freshstart.PackItem
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" || obj.Noted() {
			continue
		}
		if _, food := fighterfood.BaseFood(obj.Name); !food {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		if i, seen := index[obj.Name]; seen {
			out[i].Count += c
			continue
		}
		index[obj.Name] = len(out)
		out = append(out, freshstart.PackItem{Name: obj.Name, Count: c})
	}
	return out
}

// eatConfirm bounds the first wait for a bite to show. It was 2.4 s on the reading that the content's eat delay is 2 ticks
// (consume.rs2 player_consume_item) so "a bite the server accepted shows well inside it"; the eat delay says when the server
// REFUSES a second bite, not when the answer reaches the client, and the fleet measured bites landing 3 to 9 s after the click
// (15 Oct 2026, thieverrunner.EatConfirmWait). It is the shared wait now, and a bite that has not shown by then stays in
// actions' bite ledger and is credited by the next EatFood rather than eaten twice.
const eatConfirm = actions.BiteConfirmWait

// biteClock is the clock the bites are timed on; nil is the wall clock. Tests set it.
var biteClock func() time.Time

// EatFood eats one pack item named name through its Eat op, the same click-then-confirm shape as
// barbarianrunner.EatOnce. A bite at full HP never raises HP, so the caller only eats when hurt.
//
// Confirmation is "HP rose OR the food left that slot", not HP alone. Live-found 2026-09-19 in
// the equivalent thieverrunner/scarabrunner code and repaired here for the same reason: an
// account eats precisely when something is hitting it, and incoming damage can cancel the heal
// between samples, so HP never exceeds its pre-eat value even though the server took the bite.
// The old check then timed out and reported failure while the food HAD been consumed, which
// presents as an account starving with a full pack rather than as an error. This is not
// hypothetical for the fisher specifically: its fund phase pickpockets at Lumbridge, which
// rouses guards, so eating under sustained fire is exactly the case it hits.
//
// The slot no longer holding the same object is the honest signal. It stays true whether or not
// the heal out-ran the damage, and it also covers food that TRANSFORMS rather than leaving the
// pack (a whole Cake becomes a 2/3 cake in the same slot, so a plain "slot is now empty" test
// would miss it). HP rising is kept as an early accept so the ordinary undamaged case returns
// exactly as fast as it did before.
func EatFood(d *Deps, name string) bool {
	if d.Session.State().Inventory(PlayerPack) == nil || d.Config == nil {
		return false
	}
	return actions.EatBite(actions.BiteEnv{Session: d.Session, Now: biteClock, Delay: d.Delay, Logf: d.logf}, actions.BiteRequest{
		Food: name,
		Wait: eatConfirm,
		Pick: func() (actions.BitePick, bool) {
			inv := d.Session.State().Inventory(PlayerPack)
			if inv == nil {
				return actions.BitePick{}, false
			}
			slot, objID := -1, int32(0)
			for _, it := range inv.NonEmpty() {
				obj, ok := d.Config.Obj(int(it.ID))
				if ok && !obj.Noted() && strings.EqualFold(obj.Name, name) {
					slot, objID = it.Slot, it.ID
					break
				}
			}
			if slot < 0 {
				d.logf("no %s in the pack to eat", name)
				return actions.BitePick{}, false
			}
			return actions.BitePick{Obj: objID, Slot: slot, Changed: func() bool { return slotChanged(d, slot, objID) }}, true
		},
	})
}

// slotChanged reports whether pack slot no longer holds objID -- either emptied (the food was
// consumed outright) or transformed in place (a whole Cake becoming a 2/3 cake keeps the slot but
// changes the object). Either way the server took the bite. A missing inventory is reported as
// "not changed" rather than as success, so a read failure can never fake a confirmation.
func slotChanged(d *Deps, slot int, objID int32) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if it.Slot == slot {
			return it.ID != objID
		}
	}
	// The slot is no longer occupied at all.
	return true
}
