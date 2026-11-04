package crafterrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/crafterplan"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/freshstart"
	"rsb0t-go/internal/scripts/tile"
)

// The Seers bank pieces ProgressiveCrafter.ts's flax loop and hand-over need on top of runner.go:
// the bank's bow string and flax counts (noteBankStock), drawing flax from the bank (drawFlax), and
// the hand-over's load and return (loadHandoff, returnHandoff).

// BankCounts is what the Seers bank held at the last look.
type BankCounts struct {
	Strings int
	Flax    int
}

// OpenSeersBank walks down to the Seers bank, opens its booth from an orthogonal tile and waits for
// both panels to load. The caller closes it.
func OpenSeersBank(d *Deps) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !GoToBank(d) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	booth, ok := d.Locs.Nearest(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if !ok {
		d.logf("could not find a bank booth near the Seers bank")
		return nil, false
	}
	if !walkOrthogonal(d, tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}) {
		return nil, false
	}
	// The bank is waited for with AwaitOpen, which returns the moment the open's own inventories have arrived (a
	// tick or two after the booth op), instead of polling at a tenth of a second and then pausing another half
	// second every visit (twopertick-1012; the same open MasterCrafter and the thieving bank use).
	mark := actions.MarkBankOpen(d.Session)
	if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
		d.logf("OpLoc: %v", err)
		return nil, false
	}
	bank := actions.NewBank(d.Session, d.Components)
	if open, _ := bank.AwaitOpen(mark, Waiter(d), 8*time.Second); !open {
		return nil, false
	}
	if !d.Delay(0) {
		_ = bank.Close()
		return nil, false
	}
	return bank, true
}

// depositOps is one Deposit-All per kind of pack item not named in keep, and how many items those kinds hold.
func depositOps(d *Deps, bank *actions.Bank, keep []string) (ops []actions.BankOp, items int) {
	seen := map[int32]bool{}
	pack := bank.Inventory()
	for _, it := range pack {
		if seen[it.ID] {
			continue
		}
		name := nameOf(d, it.ID)
		if donorplan.InKeep(name, keep) || donorplan.InKeep(name, d.KeepExtra) {
			continue
		}
		seen[it.ID] = true
		items += int(sumByID(pack, it.ID))
		ops = append(ops, actions.DepositAllOp(it.ID))
	}
	return ops, items
}

// visit runs a bank visit's operations in one burst, five user events a tick, confirmed once, with the
// one-at-a-time path behind it for whatever fell short, and logs what it did (twopertick-1012: this used to be a
// Deposit-All and a wait for the bank's list per kind, then a Withdraw-All and a wait).
func visit(d *Deps, bank *actions.Bank, label string, ops []actions.BankOp) actions.BankBatch {
	if len(ops) == 0 {
		return actions.BankBatch{}
	}
	r, retried := bank.Visit(ops, Waiter(d), actions.BatchOptions{})
	d.logf("%s", actions.VisitLog("crafter "+label, len(ops), r, retried))
	return r
}

// DepositExcept deposits every pack item not named in keep into the open bank, in one burst confirmed once
// (the kinds go out together, five a tick), and returns how many items went in.
func DepositExcept(d *Deps, bank *actions.Bank, keep []string) int {
	ops, items := depositOps(d, bank, keep)
	r := visit(d, bank, "deposit", ops)
	if r.Err != nil {
		return 0
	}
	return items - shortItems(r)
}

// shortItems is how many items the deposits of a visit still left in the pack.
func shortItems(r actions.BankBatch) int {
	n := 0
	for _, sh := range r.Short {
		if sh.Op.Kind == actions.BankOpDeposit {
			n += int(sh.Want - sh.Got)
		}
	}
	return n
}

// ReadBankCounts counts the open bank's bow strings and flax.
func ReadBankCounts(d *Deps, bank *actions.Bank) BankCounts {
	var c BankCounts
	for _, row := range bankItemRows(d, bank) {
		switch {
		case strings.EqualFold(row.Name, crafterplan.BowString):
			c.Strings += row.Count
		case strings.EqualFold(row.Name, crafterplan.Flax):
			c.Flax += row.Count
		}
	}
	return c
}

// BankAllCounts is BankAll that also reads the bank's counts while it is open.
func BankAllCounts(d *Deps, keep []string) (int, BankCounts, bool) {
	bank, ok := OpenSeersBank(d)
	if !ok {
		return 0, BankCounts{}, false
	}
	defer bank.Close()
	n := DepositExcept(d, bank, keep)
	return n, ReadBankCounts(d, bank), true
}

// DrawFlax ports drawFlax: at the Seers bank, bank whatever else the trip picked up (not the flax in
// hand), and, while the bank still holds flax, withdraw a pack of it. got is how many arrived.
func DrawFlax(d *Deps) (got int, counts BankCounts, ok bool) {
	bank, opened := OpenSeersBank(d)
	if !opened {
		return 0, BankCounts{}, false
	}
	defer bank.Close()
	return drawFlaxAt(d, bank)
}

// drawFlaxAt is DrawFlax at an open bank.
func drawFlaxAt(d *Deps, bank *actions.Bank) (got int, counts BankCounts, ok bool) {
	// The deposits and the Withdraw-All of flax go out together: the bank's list was fresh when it opened, so
	// the flax row is known before anything is deposited (flax is kept, so no deposit touches it).
	ops, _ := depositOps(d, bank, append(crafterplan.SeersKeep(), crafterplan.Flax))
	flaxID, haveFlax := bankFlaxID(d, bank)
	before := FlaxCount(d)
	if haveFlax {
		ops = append(ops, actions.WithdrawAllOp(flaxID))
	}
	visit(d, bank, "deposit and draw flax", ops)
	if d.AtBank != nil {
		d.AtBank(bank)
	}
	counts = ReadBankCounts(d, bank)
	return FlaxCount(d) - before, counts, true
}

// bankFlaxID is the open bank's flax row.
func bankFlaxID(d *Deps, bank *actions.Bank) (int32, bool) {
	for _, it := range bank.Items() {
		if it.Count > 0 && strings.EqualFold(nameOf(d, it.ID), crafterplan.Flax) {
			return it.ID, true
		}
	}
	return 0, false
}

// BankAllDrawFlax is BankAllCounts that also draws a pack of flax in the SAME visit when want says so: the
// bank strings round used to bank the strings, close the bank, and come straight back for the flax (a second
// booth click, open wait and deposit pass, about three seconds of a two minute cycle). want sees the bank's
// counts as the bank opened, before the deposit; got is how many flax arrived.
func BankAllDrawFlax(d *Deps, keep []string, want func(opened BankCounts) bool) (deposited, got int, counts BankCounts, ok bool) {
	bank, opened := OpenSeersBank(d)
	if !opened {
		return 0, 0, BankCounts{}, false
	}
	defer bank.Close()
	return bankAllDrawFlaxAt(d, bank, keep, want)
}

// bankAllDrawFlaxAt is BankAllDrawFlax at an open bank.
func bankAllDrawFlaxAt(d *Deps, bank *actions.Bank, keep []string, want func(opened BankCounts) bool) (deposited, got int, counts BankCounts, ok bool) {
	ops, items := depositOps(d, bank, keep)
	flaxID, haveFlax := bankFlaxID(d, bank)
	before := FlaxCount(d)
	draw := haveFlax && want != nil && want(ReadBankCounts(d, bank))
	if draw {
		ops = append(ops, actions.WithdrawAllOp(flaxID))
	}
	label := "deposit"
	if draw {
		label = "deposit and draw flax"
	}
	r := visit(d, bank, label, ops)
	if r.Err != nil {
		items = shortItems(r) // nothing was banked: report 0 deposited
	}
	return items - shortItems(r), FlaxCount(d) - before, ReadBankCounts(d, bank), true
}

// Load is what one hand-over load came to.
type Load struct {
	// RowsInBank is the bank rows for the mule before the load, RowsLeft after it.
	RowsInBank, RowsLeft int
	// Drawn is the pack slots the withdrawal filled.
	Drawn  int
	Counts BankCounts
}

// LoadHandoff ports loadHandoff: at the Seers bank, draw the bow strings and everything else not in
// keep (crafterplan.MuleRows), as notes, until the pack is full.
func LoadHandoff(d *Deps, keep []string) (Load, bool) {
	var res Load
	bank, ok := OpenSeersBank(d)
	if !ok {
		return res, false
	}
	defer bank.Close()
	rows := crafterplan.MuleRows(bankItemRows(d, bank), keep)
	res.RowsInBank = len(rows)
	if len(rows) == 0 {
		res.Counts = ReadBankCounts(d, bank)
		return res, true
	}
	// The whole draw is one burst (twopertick-1012): note mode on, a Withdraw-All per row (a note takes one pack slot,
	// so as many rows as the pack has free) and note mode off, five user events a tick, confirmed once.
	before := UsedSlots(d)
	var ids []int32
	for _, row := range rows {
		for _, it := range bank.Items() {
			if it.ID == int32(row.ID) && it.Count > 0 {
				ids = append(ids, it.ID)
				break
			}
		}
	}
	noteDraw, asked := bank.DrawAllAsNotes(ids, actions.NoteResolver(d.Config), 28-before, Waiter(d))
	if asked > 0 {
		d.logf("crafter hand-over: %s", actions.VisitLog("note draw", asked+2, noteDraw, 0))
	}
	res.Drawn = UsedSlots(d) - before
	res.RowsLeft = len(crafterplan.MuleRows(bankItemRows(d, bank), keep))
	res.Counts = ReadBankCounts(d, bank)
	return res, true
}

// ReturnHandoff ports returnHandoff: an unfinished load goes back into the bank.
func ReturnHandoff(d *Deps) (BankCounts, bool) {
	bank, ok := OpenSeersBank(d)
	if !ok {
		return BankCounts{}, false
	}
	defer bank.Close()
	DepositExcept(d, bank, crafterplan.SeersKeep())
	return ReadBankCounts(d, bank), true
}

// CoinsHeld is the pack's coins.
func CoinsHeld(d *Deps) int { return countOfName(d, freshstart.Coins) }
