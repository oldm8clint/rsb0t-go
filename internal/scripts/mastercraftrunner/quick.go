package mastercraftrunner

// THE QUICK BANK. A load-by-load round (27 uncut gems, 27 oyster pearls) spends most of its time at
// the bank if every visit is a full OpenBank: the walk check and a fresh booth search, a wait for the
// bank to open polled at 100 ms, a fixed half-second pause, a deposit confirmed before anything else
// goes out, then Withdraw-X with its count dialog (a round trip for the dialog and another for the
// items). What the server actually needs, from the engine and content sources:
//
//   - The bank must be OPEN before a deposit or withdrawal is accepted: both are INV_BUTTON on the
//     bank's own components (content interface_bank/scripts/bank.rs2 [inv_button4,bank_main:inv],
//     bank_deposit.rs2 [inv_button4,bank_side:inv]), and the engine refuses an INV_BUTTON whose
//     component's interface is not open (engine InvButtonHandler: isComponentVisible). The booth's
//     Use-quickly ([oploc2,bankbooth] @openbank: if_openmain_side, no delay) opens it in the
//     player's turn of the tick the OPLOC is read (engine World.processPlayers ->
//     processInteraction), AFTER that tick's packets were read (processClientsIn): so the bank
//     operations must reach the engine in a LATER tick than the open. They go out the moment the
//     client sees the bank open (IF_OPENMAIN_SIDE and the two inventories, sent at the end of the
//     open's tick), which lands them in the very next tick.
//   - Withdraw-All (option 4: @bank_withdraw(last_slot, ^max_32bit_int)) takes as many as the
//     pack has room for (bank_withdraw_request: inv_itemspace2 overflow), so one packet draws the
//     next 27 with no count dialog. Deposit-All on one slot deposits every one of that item
//     (bank_deposit: inv_total), so 27 cut gems are one packet.
//   - The close (CLOSE_MODAL) only sets requestModalClose; the bank closes in the player's queue
//     processing of the same tick (engine Player.processQueues), so the first use of the next load
//     goes out when the client sees IF_CLOSE and the new pack, landing in the next tick.
//   - The booth op for the NEXT visit goes out right behind the last burst of uses
//     (gemrunner.UseOptions.AfterLast), last in that tick: an OPHELDU read after it would clear it
//     (engine OpHeldUHandler: clearPendingAction).
//
// So a load is: tick 1 the deposit, the withdrawal and the close (three of the five user events a
// tick); ticks 2 to 7 the 27 uses five a tick with the next booth op behind the last two: SEVEN
// ticks, 4.2 seconds, a load. Anything a visit cannot do quickly (not standing where the bank last
// opened, no chisel, the bank not opening, a deposit that did not happen) falls back to the full
// visit, which teaches the quick one again.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/thieverplan"
	"rsb0t-go/internal/scripts/tile"
)

// quickPoll is how often a quick visit looks at the session: well inside a tick.
const quickPoll = 10 * time.Millisecond

// quickOpenWait is how long a quick visit waits for the bank to open after the booth op: the tick
// that reads it, the trip back, and a margin. Beyond it the visit falls back to a full one.
const quickOpenWait = 2500 * time.Millisecond

// quickDoneWait is how long it waits for the deposit, the withdrawal and the close to show.
const quickDoneWait = 2500 * time.Millisecond

// QuickBank is what a quick visit needs to know: the booth beside the account, the tile it stood
// on when that booth opened the bank, and the bank's side inventory component.
type QuickBank struct {
	booth   locindex.Match
	stand   tile.Tile
	sideCom int
	ready   bool
	// primed is set when the booth op has gone out for the next visit; bankGen and sideGen are the
	// bank's and the side panel's inventory generations just before it, so the visit knows a fresh
	// inventory from the last one.
	primed           bool
	bankGen, sideGen int
	primedAt         time.Time
}

// Ready reports whether a quick visit can be tried from where the account stands now.
func (q *QuickBank) Ready(d *fleetrunner.Deps) bool {
	if q == nil || !q.ready {
		return false
	}
	x, z, level, ok := d.Session.State().Tile()
	return ok && x == q.stand.X && z == q.stand.Z && level == q.stand.Level
}

// Stand is the tile the quick bank was taught: where the account must stand for a quick visit (ok false when
// it knows none). A caller that walks away and back walks onto it exactly, so the next visit is quick.
func (q *QuickBank) Stand() (tile.Tile, bool) {
	if q == nil || !q.ready {
		return tile.Tile{}, false
	}
	return q.stand, true
}

// Forget drops what the quick bank knew: the next visit is a full one.
func (q *QuickBank) Forget() {
	if q != nil {
		*q = QuickBank{}
	}
}

// Teach sets the booth, the stand tile and the side component directly (a test, or a caller that
// found them itself).
func (q *QuickBank) Teach(booth locindex.Match, stand tile.Tile, sideCom int) {
	*q = QuickBank{booth: booth, stand: stand, sideCom: sideCom, ready: sideCom >= 0}
}

// Learn reads, while a full visit has the bank open, everything a quick visit needs: the tile the
// account stands on, the booth beside it (Use-quickly, orthogonally adjacent first: the one the
// account faces), the side inventory component. It reports whether a quick visit is now possible.
func (q *QuickBank) Learn(d *fleetrunner.Deps, bank *actions.Bank) bool {
	if q == nil {
		return false
	}
	q.Forget()
	if bank == nil || d.Locs == nil || d.Config == nil {
		return false
	}
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return false
	}
	side, _ := bank.InventorySnapshot()
	if side < 0 {
		return false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	found := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 1, Name: thieverplan.BoothName, Op: thieverplan.BoothOp}, live)
	var pick locindex.Match
	have := false
	for _, m := range found {
		if m.Distance != 1 {
			continue
		}
		orth := (m.X == x) != (m.Z == z)
		if !have || orth {
			pick, have = m, true
		}
		if orth {
			break
		}
	}
	if !have {
		return false
	}
	q.Teach(pick, tile.Tile{X: x, Z: z, Level: level}, side)
	return true
}

func invGen(st *gamestate.State, com int) int {
	if inv := st.Inventory(com); inv != nil {
		return inv.Generation
	}
	return 0
}

// Prime sends the booth's Use-quickly for the next visit now. Called right behind the last uses of
// a load (gemrunner.UseOptions.AfterLast) so the bank opens in the tick those uses are read.
func (q *QuickBank) Prime(d *fleetrunner.Deps) bool {
	if q == nil || !q.ready {
		return false
	}
	st := d.Session.State()
	q.bankGen, q.sideGen = invGen(st, actions.BankWithdraw), invGen(st, q.sideCom)
	if err := actions.OpLoc(d.Session, q.booth.Op, q.booth.X, q.booth.Z, q.booth.ID); err != nil {
		q.primed = false
		return false
	}
	q.primed, q.primedAt = true, time.Now()
	return true
}

// QuickVisit is what one quick visit did.
type QuickVisit struct {
	// Opened is the bank opening; Done the deposit, withdrawal and close all showing. A visit that
	// is not Done has fallen short (Err says how) and the caller makes a full one.
	Opened, Done bool
	Err          string
	// AtOpen is the bank as it opened, before this visit's deposit and withdrawal.
	AtOpen []gamestate.SlotItem
	// Drew is the object ids drawn (Withdraw-All), Deposited the pack ids banked.
	Drew, Deposited []int32
	// OpenTook is the wait from the booth op (or the visit's start, when it was primed earlier) to
	// the bank showing, Took the whole visit.
	OpenTook, Took time.Duration
}

// Visit makes one quick visit: the booth op unless Prime already sent it, then, the moment the bank
// shows with fresh inventories, Deposit-All on every pack item keep does not keep, Withdraw-All on
// each id draw picks from the bank, and the close, sent together; then it waits for the pack and the
// close to show. Nothing waits on anything the server does not need (see the file comment).
func (q *QuickBank) Visit(d *fleetrunner.Deps, keep func(id int32) bool, draw func(bank []gamestate.SlotItem) []int32) (v QuickVisit) {
	start := time.Now()
	defer func() { v.Took = time.Since(start) }()
	bank, ok := q.awaitOpen(d, &v, start)
	if !ok {
		return v
	}
	q.finish(d, bank, &v, keep, draw)
	return v
}

// Primed reports whether the booth op for the next visit has gone out.
func (q *QuickBank) Primed() bool { return q != nil && q.primed }

// awaitOpen is a visit's first half: the booth op unless Prime already sent it, then the wait for
// the bank to show with fresh inventories.
func (q *QuickBank) awaitOpen(d *fleetrunner.Deps, v *QuickVisit, start time.Time) (*actions.Bank, bool) {
	if !q.Ready(d) {
		v.Err = "not standing where the bank last opened"
		return nil, false
	}
	if !q.primed && !q.Prime(d) {
		v.Err = "the booth op did not go out"
		return nil, false
	}
	sent := q.primedAt
	if sent.Before(start) {
		sent = start
	}
	q.primed = false
	st := d.Session.State()
	bank := actions.NewBank(d.Session, d.Components)
	open := func() bool {
		return bank.IsOpen() && invGen(st, actions.BankWithdraw) > q.bankGen && invGen(st, q.sideCom) > q.sideGen
	}
	if !waitUntil(d, open, quickOpenWait) {
		v.Err = fmt.Sprintf("the bank did not open within %s of the booth op", quickOpenWait)
		return nil, false
	}
	v.Opened, v.OpenTook = true, time.Since(sent)
	v.AtOpen = append([]gamestate.SlotItem(nil), bank.Items()...)
	return bank, true
}

// finish is a visit's second half, with the bank open: the deposits, the withdrawals and the close
// sent together, then the wait for the pack and the close to show.
func (q *QuickBank) finish(d *fleetrunner.Deps, bank *actions.Bank, v *QuickVisit, keep func(id int32) bool, draw func(bank []gamestate.SlotItem) []int32) {
	st := d.Session.State()
	packGen := invGen(st, actions.PlayerPack)
	// Deposit-All once per item kind: it banks every one of that item, not only the slot named.
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] || (keep != nil && keep(it.ID)) {
			continue
		}
		seen[it.ID] = true
		if err := actions.InvButton(d.Session, 4, it.ID, it.Slot, q.sideCom); err != nil {
			v.Err = "deposit: " + err.Error()
			return
		}
		v.Deposited = append(v.Deposited, it.ID)
	}
	if draw != nil {
		for _, id := range draw(v.AtOpen) {
			if err := bank.WithdrawAll(id); err != nil {
				v.Err = fmt.Sprintf("withdraw %d: %v", id, err)
				return
			}
			v.Drew = append(v.Drew, id)
		}
	}
	if err := bank.Close(); err != nil {
		v.Err = "close: " + err.Error()
		return
	}
	changes := len(v.Deposited)+len(v.Drew) > 0
	// Done is the close, and a pack update since the operations went out that shows every
	// deposited item gone and every drawn one there (draw only names items the bank holds).
	drawn := map[int32]bool{}
	for _, id := range v.Drew {
		drawn[id] = true
	}
	shown := func() bool {
		for _, id := range v.Deposited {
			if !drawn[id] && packCount(d, id) > 0 { // a leftover of the drawn item goes in and comes out again
				return false
			}
		}
		for _, id := range v.Drew {
			if packCount(d, id) == 0 {
				return false
			}
		}
		return true
	}
	done := func() bool {
		if bank.IsOpen() {
			return false
		}
		return !changes || (invGen(st, actions.PlayerPack) > packGen && shown())
	}
	// The engine reads five user events of a player a tick: a visit with more operations than a
	// load's few (a storage kit's 28 kinds) needs a tick more for each further five.
	wait := quickDoneWait + time.Duration((len(v.Deposited)+len(v.Drew))/5)*600*time.Millisecond
	if !waitUntil(d, done, wait) {
		v.Err = "the deposit, withdrawal and close did not show"
		if !shown() {
			v.Err = "the pack did not show the deposit and the withdrawal"
		}
		return
	}
	v.Done = true
}

// GemQuickVisit is GemBankVisit made as a quick visit: the cut gems (and anything else but the
// chisel) banked, Withdraw-All of the best uncut gem pick names from the bank as it opened, and the
// close, together. A visit that is not Done (qv.Err) leaves the caller to make a full one.
func GemQuickVisit(d *fleetrunner.Deps, q *QuickBank, pick func(uncut map[string]int) (gemplan.Gem, bool)) (v GemVisit, qv QuickVisit) {
	var gem gemplan.Gem
	picked := false
	qv = q.Visit(d, KeepChisel, func(bank []gamestate.SlotItem) []int32 {
		v.Banked = bank
		if pick == nil {
			return nil
		}
		if gem, picked = pick(UncutCounts(bank)); !picked {
			return nil
		}
		return []int32{gem.UncutID}
	})
	v.Opened, v.Err = qv.Opened, qv.Err
	v.ChiselHeld = packCount(d, gemplan.ChiselID) > 0
	if picked {
		v.Gem, v.Drawn = gem, packCount(d, gem.UncutID)
	}
	return v, qv
}

func packCount(d *fleetrunner.Deps, id int32) int {
	n := 0
	for _, it := range packItems(d) {
		if it.ID == id {
			n += max(1, int(it.Count))
		}
	}
	return n
}

// waitUntil polls cond every quickPoll until it holds or timeout passes; false on a stop too.
func waitUntil(d *fleetrunner.Deps, cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		if !d.Delay(quickPoll) {
			return false
		}
	}
	return true
}
