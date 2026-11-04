package mastercraftrunner

// The crystal chest round's bank work (mastercraftplan/chest.go has the decisions; the chest itself,
// the Falador bank and the long walk are lootchestfisherrunner's and scarabrunner's). Two things
// are new here and nothing else:
//
//   - ChestKit: one bank visit that banks the whole pack and draws the kit again from what the bank
//     then holds: the trip's keys, the teleports' runes (none that a worn staff pays), the coins for
//     the ships and the Strange fruit for the run. Every deposit goes out in one batch and every
//     withdrawal in another (actions.Bank.Batch: five user events a tick, one confirm each, no wait
//     on any single item): the visit it replaced deposited one kind at a time, each waiting for its
//     slot to empty, and drew each item with a count dialog of its own, 8 to 18 seconds of a trip.
//   - DrawLoot: the hand-over's draw, by QUANTITY. The chest pays coins and runes, which this
//     account also keeps of its own in the same bank, so drawing a product's whole stock
//     (DrawProducts) would hand the mule the main account's own purse; only what the chest gave is drawn.

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// ChestKitSpec is what ChestKit draws.
type ChestKitSpec struct {
	// IDs are the object ids behind the kit's items.
	IDs mastercraftplan.KitIDs
	// Want is the kit to draw, given the Crystal keys the bank holds with the pack banked
	// (mastercraftplan.ChestKitWant).
	Want func(keysBanked int) mastercraftplan.KitWant
	// Keep leaves the items it names in the pack through the deposit (the weapon a Staff of fire replaced);
	// nil banks everything.
	Keep func(id int32) bool
}

// ChestKitResult is what a kit visit did.
type ChestKitResult struct {
	// KeysBank is the keys left in the bank, KeysDrawn the keys that arrived.
	KeysBank, KeysDrawn int
	// Drew is what was drawn, by item name, as the pack showed it.
	Drew map[string]int
	// Banked is the bank as it was read after the deposit.
	Banked []gamestate.SlotItem
	// Took is the whole visit, from the open bank to the closed one.
	Took time.Duration
	Err  string
}

// bankSettle is how long a visit waits for the bank's own rows to show a deposit: the pack's update
// and the bank's go out in the same tick, but a poll can land between them.
const bankSettle = 2 * time.Second

// ChestKit banks the whole pack and draws the kit again, in an open bank, and closes it.
func ChestKit(d *fleetrunner.Deps, bank *actions.Bank, spec ChestKitSpec) (out ChestKitResult) {
	start := time.Now()
	out.Drew = map[string]int{}
	defer func() { out.Took = time.Since(start) }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	st := d.Session.State()
	if err := bank.SetNoteMode(false, w); err != nil {
		out.Err = "item mode: " + err.Error()
		return out
	}
	// 1. The whole pack goes into the bank, every kind at once; a deposit the batch did not confirm
	// is made again one at a time (Bank.BatchDepositAllMatching).
	packBefore := len(bank.Inventory())
	bankGen := invGen(st, actions.BankWithdraw)
	deposit := bank.BatchDepositInventory
	if spec.Keep != nil {
		deposit = func(w actions.Waiter) error {
			return bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !spec.Keep(it.ID) }, w)
		}
	}
	if err := deposit(w); err != nil {
		out.Err = "deposit: " + err.Error()
	}
	if packBefore > 0 {
		waitUntil(d, func() bool { return invGen(st, actions.BankWithdraw) > bankGen }, bankSettle)
	}
	out.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	// 2. The kit is drawn from the bank as it now stands, every item at once.
	keysIn := 0
	for _, id := range spec.IDs.Keys {
		keysIn += int(actions.CountByID(out.Banked, int32(id)))
	}
	want := spec.Want(keysIn)
	draws := mastercraftplan.PlanChestDraws(bankRows(out.Banked), spec.IDs, want)
	noted := mastercraftplan.PlanChestNotedDraws(bankRows(out.Banked), spec.IDs, want)
	ops := make([]actions.BankOp, 0, len(draws))
	for _, dr := range draws {
		ops = append(ops, actions.WithdrawOp(int32(dr.ID), int32(dr.Count)))
	}
	res := bank.Batch(ops, w, actions.BatchOptions{})
	if res.Err != nil {
		out.Err = "withdraw: " + res.Err.Error()
	}
	// What the batch did not confirm is drawn one at a time, as the paced visit always did.
	for _, sh := range res.Short {
		if sh.Op.Kind != actions.BankOpWithdraw || sh.Why == "the bank holds none" || sh.Why == "the pack filled" {
			continue
		}
		if rest := int32(sh.Want - sh.Got); rest > 0 {
			if err := bank.WithdrawX(sh.Op.ID, rest, w); err != nil {
				logf(d, "mastercrafter: chest: withdrawing %d of item %d: %v", rest, sh.Op.ID, err)
			}
		}
	}
	for _, dr := range draws {
		if n := packCount(d, int32(dr.ID)); n > 0 {
			out.Drew[dr.Item] += n
		}
	}
	// 3. What the kit carries as notes (the gems of the walks), in note mode, one batch.
	if len(noted) > 0 {
		if err := bank.SetNoteMode(true, w); err != nil {
			logf(d, "mastercrafter: chest: note mode on for the kit's gems: %v", err)
		} else {
			nops := make([]actions.BankOp, 0, len(noted))
			for _, dr := range noted {
				nops = append(nops, actions.WithdrawAsOp(int32(dr.ID), int32(dr.Count), NotedForm(d.Config, int32(dr.ID))))
			}
			before := map[int32]int{}
			for _, dr := range noted {
				before[NotedForm(d.Config, int32(dr.ID))] = packCount(d, NotedForm(d.Config, int32(dr.ID)))
			}
			if res := bank.Batch(nops, w, actions.BatchOptions{}); res.Err != nil {
				logf(d, "mastercrafter: chest: withdrawing the kit's gems as notes: %v", res.Err)
			}
			for _, dr := range noted {
				nid := NotedForm(d.Config, int32(dr.ID))
				if n := packCount(d, nid) - before[nid]; n > 0 {
					out.Drew[dr.Item+" (noted)"] += n
				}
			}
			if err := bank.SetNoteMode(false, w); err != nil {
				logf(d, "mastercrafter: chest: could not switch the bank back to items: %v", err)
			}
		}
	}
	for _, id := range spec.IDs.Keys {
		out.KeysDrawn += packCount(d, int32(id))
	}
	out.KeysBank = max(0, keysIn-out.KeysDrawn)
	_ = bank.Close()
	// The bank is shut when the close has been read: the walk's first click would close it anyway.
	waitUntil(d, func() bool { return !bank.IsOpen() }, time.Second)
	return out
}

func bankRows(items []gamestate.SlotItem) []mastercraftplan.BankRow {
	rows := make([]mastercraftplan.BankRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, mastercraftplan.BankRow{ID: int(it.ID), Count: int(it.Count)})
	}
	return rows
}

// PackRows is the pack as the plan's rows (an unstackable's slot counts 1).
func PackRows(d *fleetrunner.Deps) []mastercraftplan.BankRow {
	items := packItems(d)
	rows := make([]mastercraftplan.BankRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, mastercraftplan.BankRow{ID: int(it.ID), Count: max(1, int(it.Count))})
	}
	return rows
}

// WornNames is the names of the items the account wears.
func WornNames(d *fleetrunner.Deps) []string {
	if d.Config == nil {
		return nil
	}
	var out []string
	for _, it := range actions.NewEquipment(d.Session, d.Components).Worn() {
		if o, ok := d.Config.Obj(int(it.ID)); ok {
			out = append(out, o.Name)
		}
	}
	return out
}

// PackCountNamed is the pack's units of the unnoted items named name.
func PackCountNamed(d *fleetrunner.Deps, name string) int {
	if d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range packItems(d) {
		if o, ok := d.Config.Obj(int(it.ID)); ok && !o.Noted() && strings.EqualFold(o.Name, name) {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// BankForm is the id an item is banked under: a note's item, anything else itself.
func BankForm(cfg *cachecfg.Config, id int) int {
	if cfg == nil {
		return id
	}
	if o, ok := cfg.Obj(id); ok && o.Noted() && o.CertLink > 0 {
		return o.CertLink
	}
	return id
}

// NotedForm is the note of an item, or the item itself when it has none (a stackable: coins,
// runes), which is what a note-mode withdrawal puts in the pack.
func NotedForm(cfg *cachecfg.Config, id int32) int32 {
	if cfg == nil {
		return id
	}
	o, ok := cfg.Obj(int(id))
	if !ok || o.Noted() {
		return id
	}
	for _, nid := range cfg.ObjIDs(o.Name) {
		if n, ok := cfg.Obj(nid); ok && n.Noted() && n.CertLink == int(id) {
			return int32(nid)
		}
	}
	return id
}

// LootWant is one owed item a hand-over draws.
type LootWant struct {
	ID   int32
	Name string
	Qty  int
}

// DrawLoot opens the bank at post, banks the pack, and draws each want as bank notes (one slot an
// item), at most its Qty and what the bank holds. Got is by object id (the bank form).
func DrawLoot(d *fleetrunner.Deps, post tile.Tile, wants []LootWant) (Haul, map[int32]int) {
	out := Haul{Got: map[string]int{}}
	byID := map[int32]int{}
	bank, ok := OpenBank(d, post)
	if !ok || bank == nil {
		out.Err = "could not open the bank at the mule's post"
		return out, byID
	}
	out.Opened = true
	defer func() { _ = bank.Close() }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	if len(bank.Inventory()) > 0 {
		if err := bank.BatchDepositInventory(w); err != nil {
			out.Err = "deposit: " + err.Error()
			return out, byID
		}
	}
	out.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	if err := bank.SetNoteMode(true, w); err != nil {
		out.Err = "note mode on: " + err.Error()
		return out, byID
	}
	defer func() {
		if err := bank.SetNoteMode(false, w); err != nil {
			logf(d, "mastercrafter: could not switch the bank back to items: %v", err)
		}
	}()
	for _, want := range wants {
		n := min(want.Qty, int(actions.CountByID(out.Banked, want.ID)))
		if n <= 0 {
			continue
		}
		if packRows(d) >= 28 {
			break // the rest waits for the next pass
		}
		before, slotsBefore := packUnits(d), packRows(d)
		if err := bank.WithdrawXAs(want.ID, int32(n), NotedForm(d.Config, want.ID), w); err != nil {
			logf(d, "mastercrafter: chest: withdrawing %d %s as notes: %v", n, want.Name, err)
		}
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) && packUnits(d) <= before {
			if !d.Delay(150 * time.Millisecond) {
				return out, byID
			}
		}
		if got := packUnits(d) - before; got > 0 {
			out.Got[want.Name] += got
			byID[want.ID] += got
			out.Rows++
			out.Slots += packRows(d) - slotsBefore
		}
	}
	return out, byID
}
