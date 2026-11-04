// Package bowstringrunner is the I/O half of BowStringer: resolve the bow's ids, string the
// bows held one action at a time, bank at the mule's post, and ask the mule for a kit of noted
// bows and strings. The decisions are internal/scripts/bowstringplan's.
//
// Everything here reuses legs already proven elsewhere and adds none of its own: the bank is
// scarabrunner.OpenBank (the walk, then thieverrunner.OpenBankAt's booth), the draw is
// actions.Bank.WithdrawX, the stringing actions.Equipment.UseOnHeld (OPHELDU, as BankFletcher's
// knife on logs), and the kit scarabrunner.RequestKitBundle (the ScarabSwarm's bundled order).
package bowstringrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/bowstringplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// FletchingStat is Fletching's index in the protocol's stat table (gamestate.SkillNames).
const FletchingStat = 9

// IDs is what one bow resolves to in the object config.
type IDs struct {
	Unstrung, Strung, String int32
}

// ResolveIDs finds the unstrung and strung ids of bow (bowstringplan.UnstrungID) and the Bow
// string's unnoted id.
func ResolveIDs(cfg *cachecfg.Config, bow string) (IDs, error) {
	if cfg == nil {
		return IDs{}, fmt.Errorf("no object config")
	}
	var objs []bowstringplan.ObjInfo
	for _, id := range cfg.ObjIDs(bow) {
		o, ok := cfg.Obj(id)
		if !ok {
			continue
		}
		objs = append(objs, bowstringplan.ObjInfo{ID: id, Cost: int(o.Cost), Noted: o.Noted()})
	}
	un, st, ok := bowstringplan.UnstrungID(objs)
	if !ok {
		return IDs{}, fmt.Errorf("cannot tell the unstrung %s from the strung one among %v", bow, objs)
	}
	ids := IDs{Unstrung: int32(un), Strung: int32(st)}
	for _, id := range cfg.ObjIDs(bowstringplan.BowString) {
		if o, ok := cfg.Obj(id); ok && !o.Noted() {
			ids.String = int32(id)
			break
		}
	}
	if ids.String == 0 {
		return IDs{}, fmt.Errorf("no %s in the object config", bowstringplan.BowString)
	}
	return ids, nil
}

func logf(d *fleetrunner.Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func pack(d *fleetrunner.Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(fleetrunner.PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// PackCount is how many of id the pack holds (an id is never its own note).
func PackCount(d *fleetrunner.Deps, id int32) int { return int(actions.CountByID(pack(d), id)) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *fleetrunner.Deps) int {
	return max(0, bowstringplan.PackSlots-len(pack(d)))
}

// Fletching is the account's current Fletching level; ok false before the server sent it.
func Fletching(d *fleetrunner.Deps) (int, bool) {
	s, ok := d.Session.State().Skill(FletchingStat)
	if !ok || s.Generation == 0 {
		return 0, false
	}
	return s.Level, true
}

// stringConfirm is how long one stringing action has to show in the pack: the content strings
// on the tick the OPHELDU is processed, so two ticks and a margin.
const stringConfirm = 2500 * time.Millisecond

// StringResult is one StringAll.
type StringResult struct {
	Strung int
	// Reason is "done" (bows or strings ran out), "refused" (an action changed nothing: the
	// level, a free world, a note), "use-failed" or "stopped".
	Reason string
}

// StringsPerTick is how many stringing actions go out together: the engine reads five user events
// a player a tick (ClientGameProtCategory.USER_EVENT) and the content strings with no delay, so
// five pairs sent at once are five bows strung on the next tick (owner, 29 Sep: "i think we can
// string 5 bows per tick").
const StringsPerTick = 5

// StringAll strings bows in bursts of StringsPerTick, each burst confirmed by the unstrung count
// dropping and the strung count rising, until bows or strings run out.
func StringAll(d *fleetrunner.Deps, ids IDs) StringResult {
	var r StringResult
	eq := actions.NewEquipment(d.Session, d.Components)
	for {
		bows, strs := PackCount(d, ids.Unstrung), PackCount(d, ids.String)
		if bows == 0 || strs == 0 {
			r.Reason = "done"
			return r
		}
		strung := PackCount(d, ids.Strung)
		sent, err := eq.UseOnHeldPairs(ids.String, ids.Unstrung, StringsPerTick)
		if err != nil && sent == 0 {
			logf(d, "bowstring: use %d on %d: %v", ids.String, ids.Unstrung, err)
			r.Reason = "use-failed"
			return r
		}
		deadline := time.Now().Add(stringConfirm)
		got := 0
		for time.Now().Before(deadline) {
			if !d.Delay(100 * time.Millisecond) {
				r.Reason = "stopped"
				return r
			}
			got = PackCount(d, ids.Strung) - strung
			if got >= sent && PackCount(d, ids.Unstrung) <= bows-sent {
				break
			}
		}
		if got <= 0 {
			r.Reason = "refused"
			return r
		}
		r.Strung += got
	}
}

// waiter adapts Deps.Delay to actions.Waiter.
type waiter struct{ d *fleetrunner.Deps }

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

// BankVisit is what one bank visit found and did.
type BankVisit struct {
	// Opened is false when the bank could not be reached or opened.
	Opened bool
	// BankBows and BankStrings are the bank's unstrung bows and strings after the deposit and
	// before the draw; BankStrung its strung bows.
	BankBows, BankStrings, BankStrung int
	// Drew is how many of each were drawn (0: nothing to draw).
	Drew int
	// Cycle is true when the owner's three-click cycle (ownerCycle) drew the load, in place of a deposit
	// of the whole pack and two Withdraw-X.
	Cycle bool
	Err   string
}

// loadHeld is whether the pack is a finished load of the owner's cycle: Batch strung bows and nothing
// else (what stringing a batch leaves).
func loadHeld(pack []gamestate.SlotItem, ids IDs) bool {
	if len(pack) != bowstringplan.Batch {
		return false
	}
	for _, it := range pack {
		if it.ID != ids.Strung {
			return false
		}
	}
	return true
}

// cycleConfirm is how long the owner's cycle waits for the pack to show a load drawn (a var so a test
// of a cycle that falls short does not wait it out).
var cycleConfirm = 4 * time.Second

// ownerCycle is the bank visit that follows a finished load, in the owner's order (6 Oct 2026:
// "withdraw all bow string, bank the strung bows, withdraw all unstrung bows ... saving an input"): the
// pack holds Batch strung bows, so Withdraw-All of the strings fills exactly the Batch free slots,
// Deposit-All of the strung bows empties the other Batch, and Withdraw-All of the unstrung bows fills
// them. Three clicks, no count dialog and no note-mode switch, against the deposit, Withdraw-X and
// Withdraw-X it replaces. The clicks go out one behind the other (the engine reads them in order) and the
// pack is read once. It reports the bows and strings the pack then holds; fewer than Batch of either
// means the cycle fell short, and the caller banks the pack and draws the old way.
func ownerCycle(d *fleetrunner.Deps, bank *actions.Bank, w waiter, ids IDs) (bows, strs int) {
	if bank.WithdrawAll(ids.String) != nil || bank.DepositAll(ids.Strung) != nil || bank.WithdrawAll(ids.Unstrung) != nil {
		return 0, 0
	}
	inv := func() ([]gamestate.SlotItem, int, int) {
		p := bank.Inventory()
		return p, int(actions.CountByID(p, ids.Unstrung)), int(actions.CountByID(p, ids.String))
	}
	w.DelayUntil(func() bool { _, b, s := inv(); return b >= bowstringplan.Batch && s >= bowstringplan.Batch }, cycleConfirm)
	_, bows, strs = inv()
	return bows, strs
}

// openBank is scarabrunner.OpenBank (a var so a test can hand Bank an open bank without a scene).
var openBank = fleetrunner.OpenBank

// Bank walks to the bank at bankTile, deposits the whole pack, reads the bank and draws one
// batch (bowstringplan.Withdraw) of unstrung bows and strings as items. A pack that is a finished load
// (loadHeld) is not deposited whole: the bank holding a batch of each, the owner's cycle (ownerCycle)
// exchanges it for the next.
func Bank(d *fleetrunner.Deps, bankTile tile.Tile, ids IDs) BankVisit {
	var v BankVisit
	bank, ok := openBank(d, bankTile)
	if !ok {
		v.Err = fmt.Sprintf("could not open the bank at %d,%d", bankTile.X, bankTile.Z)
		return v
	}
	v.Opened = true
	defer func() { _ = bank.Close() }()
	w := waiter{d}
	if at := bank.Items(); loadHeld(bank.Inventory(), ids) &&
		int(actions.CountByID(at, ids.Unstrung)) >= bowstringplan.Batch && int(actions.CountByID(at, ids.String)) >= bowstringplan.Batch {
		bankBows, bankStrings, bankStrung := int(actions.CountByID(at, ids.Unstrung)), int(actions.CountByID(at, ids.String)), int(actions.CountByID(at, ids.Strung))
		if bows, strs := ownerCycle(d, bank, w, ids); bows >= bowstringplan.Batch && strs >= bowstringplan.Batch {
			v.BankBows, v.BankStrings, v.BankStrung = bankBows, bankStrings, bankStrung+bowstringplan.Batch
			v.Drew = min(bows, strs)
			v.Cycle = true
			return v
		}
		logf(d, "bowstring: the bank cycle did not draw a load; banking the pack and drawing it the old way")
	}
	if len(bank.Inventory()) > 0 {
		if err := bank.BatchDepositInventory(w); err != nil {
			v.Err = "deposit: " + err.Error()
			return v
		}
	}
	items := bank.Items()
	v.BankBows = int(actions.CountByID(items, ids.Unstrung))
	v.BankStrings = int(actions.CountByID(items, ids.String))
	v.BankStrung = int(actions.CountByID(items, ids.Strung))
	n := bowstringplan.Withdraw(v.BankBows, v.BankStrings, FreeSlots(d))
	if n == 0 {
		return v
	}
	if err := bank.SetNoteMode(false, w); err != nil {
		v.Err = "note mode off: " + err.Error()
		return v
	}
	if err := bank.WithdrawX(ids.Unstrung, int32(n), w); err != nil {
		v.Err = "withdraw bows: " + err.Error()
		return v
	}
	if err := bank.WithdrawX(ids.String, int32(n), w); err != nil {
		v.Err = "withdraw strings: " + err.Error()
		return v
	}
	v.Drew = min(PackCount(d, ids.Unstrung), PackCount(d, ids.String))
	return v
}

// KitOrder is one kit order's outcome.
type KitOrder struct {
	// Got is what arrived per line (notes and items), Declined the mule's reason per line.
	Got      map[string]int
	Declined map[string]string
	// Bundled is false when the mule never answered the kit order and the per-item fallback ran.
	Bundled bool
	Stopped bool
	Reason  string
}

// OrderKit asks mule for the lines in one bundled trade, standing at its post (the caller
// walked there), and leaves any notes in the pack for the next bank visit to deposit: a bank
// takes a note back as the item, and drawing the kit back out by NAME could draw a strung bow.
func OrderKit(d *fleetrunner.Deps, bus *mulebus.Bus, me, mule string, lines []bowstringplan.KitLine, bankTile tile.Tile, world int, timeout time.Duration) KitOrder {
	items := make([]fleetplan.ItemCount, 0, len(lines))
	for _, l := range lines {
		items = append(items, fleetplan.ItemCount{Item: l.Item, Count: l.Qty})
	}
	res, info := fleetrunner.RequestKitBundle(d, bus, me, mule, items, fleetrunner.KitBundleOptions{
		Timeout: timeout, Bank: bankTile, KeepNotes: true, PerOrder: bowstringplan.KitMax,
		FallbackTimeout: 90 * time.Second, World: world,
	})
	got := map[string]int{}
	for k, n := range res.Got {
		got[k] += n
	}
	for k, n := range info.NotedHeld {
		got[k] += n
	}
	return KitOrder{Got: got, Declined: res.Declined, Bundled: info.Bundled, Stopped: info.Stopped, Reason: info.Reason}
}
