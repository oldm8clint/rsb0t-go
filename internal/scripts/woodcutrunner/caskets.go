package woodcutrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/fastopen"
	"rsb0t-go/internal/scripts/fisherrunner"
)

// Opening banked caskets for coins, so an account that is broke but rich in unopened loot never
// walks to the mule for a 60-coin ship fare it is already carrying.
//
// Users asked for this after the sub-oak cohort stalled (2026-09-21): "caskets can give
// coins aswel, so if the account needs coins, doesnt have any they should loot the caskets
// first to see if they get any coins from it before moving to the mule to request 60 coins".
// The numbers made the case on their own -- the converted fishers waiting on a fare held 86,
// 74, 41 and 12 banked caskets and up to two hundred oysters each, with zero coins.
//
// What counts as openable comes from fisherrunner.IsOpenable rather than a second list here.
// Content 289's [opheld1,casket] pays out coins, uncut gems, a cosmic talisman or a key half;
// [opheld1,oystershell] pays out a pearl or an EMPTY OYSTER -- which is why the names are
// matched exactly and not as substrings. "Empty oyster" contains "oyster", and a substring
// match would re-open the spent shells forever.

// casketRounds bounds how many withdraw-and-open passes are made. Three is plenty for a
// 60-coin fare and stops a broke account spending its afternoon on two hundred oysters.
const casketRounds = 3

// casketsPerRound is how many to draw at a time: enough to be worth the bank trip, small
// enough to leave pack room for what comes out.
const casketsPerRound = 12

// OpenBankedForCoins opens banked caskets and oysters until the pack holds at least need
// coins, or until there is nothing left worth opening. It returns the coins in hand at the end,
// so the caller can decide whether it still needs the mule.
//
// Safe to call with an empty bank: it opens nothing and reports what is already held. It opens
// on the spot and leaves the rest banked -- it is a fare, not a cleanup pass, and the fishers'
// own hand-off (fisherrunner.OpenBankedOpenables) is what empties the pile properly.
func OpenBankedForCoins(d *Deps, need int) int {
	if d == nil || d.Config == nil || d.Session == nil {
		return 0
	}
	for round := 0; round < casketRounds; round++ {
		if CoinsHeld(d) >= need {
			return CoinsHeld(d)
		}
		bank, ok := OpenSeersBank(d)
		if !ok {
			return CoinsHeld(d)
		}
		if err := SetNoteMode(d, bank, false); err != nil {
			d.logf("caskets: could not switch the bank to items: %v", err)
		}
		drawn := drawOpenables(d, bank)
		_ = bank.Close()
		if drawn == 0 {
			return CoinsHeld(d)
		}
		if opened := openHeldForCoins(d); opened == 0 {
			// Held some but opened none: stop rather than spin. They stay in the pack and the
			// next ordinary bank trip puts them back.
			d.logf("caskets: drew %d but opened none — giving up on paying the fare this way", drawn)
			return CoinsHeld(d)
		}
	}
	return CoinsHeld(d)
}

// drawOpenables withdraws up to casketsPerRound openables, ten at a time, leaving two free
// slots for the rewards. It reports how many withdraw clicks landed.
func drawOpenables(d *Deps, bank *actions.Bank) int {
	drawn := 0
	for _, it := range bank.Items() {
		if drawn >= casketsPerRound || FreeSlots(d) <= 2 {
			break
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !fisherrunner.IsOpenable(obj.Name) {
			continue
		}
		before := len(heldOpenables(d))
		if err := bank.WithdrawTen(it.ID); err != nil {
			d.logf("caskets: withdrawing %s: %v", obj.Name, err)
			continue
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && len(heldOpenables(d)) <= before {
			if !d.Delay(150 * time.Millisecond) {
				return drawn
			}
		}
		if got := len(heldOpenables(d)) - before; got > 0 {
			drawn += got
		}
	}
	return drawn
}

// openHeldForCoins opens every casket and oyster in the pack five a tick (fastopen: the next click
// closes the last reward box itself), and whatever that leaves the old way, paging through the
// reward boxes with Continue. Oysters show no box, so a missing one is not an error. It reports how
// many left the pack, and tells the Loot panel what they paid out.
func openHeldForCoins(d *Deps) int {
	held := heldOpenables(d)
	if len(held) == 0 {
		return 0
	}
	before := loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	// Five opens a tick, one message a tick, confirmed once (fastopen); what it could not open goes
	// the old way below.
	var rest []gamestate.SlotItem
	if r := fastopen.Open(d.Session, fastopen.Delayer(d.Delay), PlayerPack, held); r.Reason != "done" && r.Reason != "stopped" {
		rest = heldOpenables(d)
	}
	for _, it := range rest {
		if err := actions.Open(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
			d.logf("caskets: open %d in slot %d: %v", it.ID, it.Slot, err)
		}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) && len(heldOpenables(d)) > 0 {
		if dlg.CanContinue() {
			_ = dlg.Continue()
		}
		if !d.Delay(150 * time.Millisecond) {
			break
		}
	}
	for i := 0; i < 5 && dlg.CanContinue(); i++ {
		_ = dlg.Continue()
		d.Delay(150 * time.Millisecond)
	}
	if main, _, chat := d.Session.State().Modals(); main != gamestate.NoModal || chat != gamestate.NoModal {
		_ = actions.CloseModal(d.Session)
	}
	opened := len(held) - len(heldOpenables(d))
	if opened > 0 {
		recordCasketLoot(d, before, opened)
	}
	return opened
}

// recordCasketLoot attributes what the burst paid out. The woodcutter draws caskets and oysters
// in the same round, and nothing in the pack change says which reward came from which, so the
// split is only attempted when one kind alone was opened; a mixed round is reported as caskets,
// which is where every coin in it came from (an oyster only ever yields a pearl or a shell).
func recordCasketLoot(d *Deps, before loot.Counts, opened int) {
	if d.Loot == nil {
		return
	}
	after := loot.PackCounts(d.Session.State().Inventory(PlayerPack))
	name, id := casketKindOpened(d, before, after)
	gained := loot.Gained(before, after, func(id int) bool {
		obj, ok := d.Config.Obj(id)
		return ok && !obj.Noted() && fisherrunner.IsOpenable(obj.Name)
	})
	d.Loot(loot.Drop{Source: loot.Openable(name, id), Events: opened, Items: loot.Items(d.Config, gained)})
}

// casketKindOpened names the openable the burst consumed, preferring whichever lost the most
// from the pack, and reports its obj id for the panel's icon.
func casketKindOpened(d *Deps, before, after loot.Counts) (string, int) {
	name, id, best := "Casket", 405, int64(0)
	for objID, n := range before {
		obj, ok := d.Config.Obj(objID)
		if !ok || obj.Noted() || !fisherrunner.IsOpenable(obj.Name) {
			continue
		}
		if left := n - after[objID]; left > best {
			name, id, best = obj.Name, objID, left
		}
	}
	return name, id
}

func heldOpenables(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []gamestate.SlotItem
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && !obj.Noted() && fisherrunner.IsOpenable(obj.Name) {
			out = append(out, it)
		}
	}
	return out
}
