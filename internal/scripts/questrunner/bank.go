package questrunner

import (
	"errors"
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/tile"
)

// Banking for the quest engine: open a booth near a tile, read the bank, deposit everything a
// step does not keep, withdraw exact counts by object id.

// bankOpenTimeout is how long a booth gets to open.
const bankOpenTimeout = 15 * time.Second

// OpenBankAt travels to bankTile and opens the nearest bank booth. It reports the open bank,
// or false with the reason logged.
func OpenBankAt(d *Deps, bankTile tile.Tile) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive — cannot open a bank")
		return nil, false
	}
	bank := actions.NewBank(d.Session, d.Components)
	if bank.IsOpen() {
		return bank, true
	}
	before := currentTile(d)
	if !TravelCautious(d, bankTile, 4) {
		// A leg of several hundred tiles can time out (TravelTimeout) a short way from the bank: live
		// 13 Oct 2026 every Holy Grail kit walked from Draynor Manor to Catherby for six minutes and
		// stopped 90 tiles short, and the whole round failed on it. Ask again from where the walk got
		// to when it came well closer; a walk that got nowhere (unreachable, stranded) is not repeated.
		if !walkWorthRepeating(before, currentTile(d), bankTile) || !TravelCautious(d, bankTile, 4) {
			d.logf("could not reach the bank at %d,%d,%d", bankTile.X, bankTile.Z, bankTile.Level)
			return nil, false
		}
	}
	here := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	var cands []locindex.Match
	for _, op := range []string{"Use-quickly", "Bank", "Use"} {
		cands = d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: 15, Name: "Bank booth", Op: op}, live)
		if len(cands) > 0 {
			break
		}
	}
	for i, booth := range cands {
		if i >= 4 {
			break
		}
		if !ApproachLoc(d, booth) {
			continue
		}
		ClearLeftoverModal(d)
		// The mark tells this open's inventories from the last visit's, which the client keeps;
		// AwaitOpen hands the bank over the moment both have arrived (actions/fastbank.go).
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("opening the booth at %d,%d: %v", booth.X, booth.Z, err)
			continue
		}
		if open, fresh := bank.AwaitOpen(mark, waiterFor(d), bankOpenTimeout); open {
			if !fresh {
				// The bank's side panel fills a moment after the modal opens.
				waitQuietly(d, func() bool { return len(bank.Inventory()) > 0 || FreeSlots(d) == PackSlots }, 2*time.Second)
			}
			return bank, true
		}
		d.logf("the booth at %d,%d did not open the bank", booth.X, booth.Z)
	}
	d.logf("no bank booth near %d,%d,%d opened", here.X, here.Z, here.Level)
	return nil, false
}

// walkRepeatProgress is how many tiles closer to its goal a failed walk must have got for the same
// walk to be asked for again at once.
const walkRepeatProgress = 60

// walkWorthRepeating reports whether a walk from before that ended on after, short of dest, made
// enough headway (walkRepeatProgress tiles of Chebyshev distance) on the same level to be repeated.
func walkWorthRepeating(before, after, dest tile.Tile) bool {
	if before.Level != dest.Level || after.Level != dest.Level {
		return false
	}
	return tile.Chebyshev(before, dest)-tile.Chebyshev(after, dest) >= walkRepeatProgress
}

// BankCounts reads an open bank by exact object id.
func BankCounts(bank *actions.Bank) map[int]int {
	out := map[int]int{}
	for _, it := range bank.Items() {
		if it.ID > 0 {
			out[int(it.ID)] += int(it.Count)
		}
	}
	return out
}

// DepositExcept banks every pack item keep does not claim (nor Deps.KeepInPack, which every deposit of this account
// honours) and waits for the pack to show it. Deps.AtBank runs after it. Inside a bare-pack stretch (Deps.BarePack) it is
// DepositBare instead: the account's own kit is neither kept nor drawn back.
func DepositExcept(d *Deps, bank *actions.Bank, keep func(id int) bool) bool {
	if d.InBarePack() {
		return DepositBare(d, bank, keep)
	}
	var own func(id int) bool
	if kp := d.KeepInPack; kp != nil {
		own = keep
		keep = func(id int) bool { return own(id) || kp(ObjName(d, id)) }
	}
	if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !keep(int(it.ID)) }, waiterFor(d)); err != nil {
		d.logf("depositing: %v", err)
	}
	ok := waitQuietly(d, func() bool {
		for id := range PackCounts(d) {
			if !keep(id) {
				return false
			}
		}
		return true
	}, 4*time.Second)
	if !ok {
		d.logf("the pack still holds items the step does not keep after depositing")
	}
	// Deps.KeepInPack (the run staller's rub item and alch kit) stays only while it leaves the quest room:
	// live 7 Oct 2026, spare103's pack held the kit and every Nature Spirit withdrawal answered "the pack
	// filled" for 16 passes, round after round.
	if d.KeepInPack != nil && own != nil && FreeSlots(d) < keepInPackMinFree {
		d.logf("depositing the run staller's kit too: %d free slots is under the %d a quest step needs", FreeSlots(d), keepInPackMinFree)
		if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !own(int(it.ID)) }, waiterFor(d)); err != nil {
			d.logf("depositing: %v", err)
		}
		waitQuietly(d, func() bool { return FreeSlots(d) >= keepInPackMinFree }, 4*time.Second)
	}
	if d.AtBank != nil {
		d.AtBank(bank, waiterFor(d))
	}
	return ok
}

// keepInPackMinFree is the room a quest deposit leaves at least before it keeps Deps.KeepInPack's items.
const keepInPackMinFree = 10

// DepositBare banks every pack item keep claims not, and consults nothing else: not Deps.KeepInPack (the run staller's
// jewellery, runes and laws), and Deps.AtBank does not run, so the jewellery is not drawn back out of the bank behind it.
// It is the deposit for a place that refuses the account's own kit: Glarial's tomb takes no rune or jewellery, Entrana no
// armour, Thorvald's ladder nothing but food, the Wilderness altar trip risks nothing. A strip through DepositExcept never
// got there: the kit the first pass kept was banked by the "too few free slots" pass at best, and AtBank put the ring
// straight back in the pack, so waterfallplan.TombSafe and questplan.WildernessSafe failed for ever (live 14-15 Oct 2026:
// 238 wf-strip parks, ten of ten Merlin runs). The kit comes back at the next ordinary deposit, which is after the quest has
// left the room. It reports whether the pack holds nothing but what keep claims.
func DepositBare(d *Deps, bank *actions.Bank, keep func(id int) bool) bool {
	if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool { return !keep(int(it.ID)) }, waiterFor(d)); err != nil {
		d.logf("depositing: %v", err)
	}
	ok := waitQuietly(d, func() bool {
		for id := range PackCounts(d) {
			if !keep(id) {
				return false
			}
		}
		return true
	}, 4*time.Second)
	if !ok {
		d.logf("the pack still holds items the bare-pack deposit does not keep")
	}
	return ok
}

// WithdrawExact draws n of id (or all the bank has, if less) and reports how many arrived.
func WithdrawExact(d *Deps, bank *actions.Bank, id, n int) int {
	if n <= 0 {
		return 0
	}
	if bank.NoteMode() {
		if err := bank.SetNoteMode(false, waiterFor(d)); err != nil {
			d.logf("turning note mode off: %v", err)
		}
	}
	before := HeldID(d, id)
	if err := bank.QuickWithdrawX(int32(id), int32(n), waiterFor(d)); err != nil {
		d.logf("withdraw %d %s: %v", n, ObjName(d, id), err)
	}
	waitQuietly(d, func() bool { return HeldID(d, id)-before >= n }, 4*time.Second)
	got := HeldID(d, id) - before
	if got < n {
		d.logf("withdrew %d of %d %s", got, n, ObjName(d, id))
	}
	return got
}

// Draw is one WithdrawExactMany line: n of the object id.
type Draw struct{ ID, N int }

// WithdrawExactMany is WithdrawExact for several lines at once: the switch to items and every
// withdrawal go out in one burst (actions.Bank.Batch, five user events a tick, Withdraw-X's count
// right behind its button) and are confirmed once against the bank's side panel. A line the
// burst fell short of, while the bank still holds some, is drawn the old way (WithdrawExact). It
// reports how many of each line arrived, in order.
func WithdrawExactMany(d *Deps, bank *actions.Bank, draws []Draw) []int {
	got := make([]int, len(draws))
	var ops []actions.BankOp
	if bank.NoteMode() {
		ops = append(ops, actions.NoteModeOp(false))
	}
	before := map[int]int{}
	for _, dr := range draws {
		if dr.N <= 0 {
			continue
		}
		if _, seen := before[dr.ID]; !seen {
			before[dr.ID] = int(actions.CountByID(bank.Inventory(), int32(dr.ID)))
		}
		ops = append(ops, actions.WithdrawOp(int32(dr.ID), int32(dr.N)))
	}
	if len(ops) == 0 {
		return got
	}
	fast := bank.Batch(ops, waiterFor(d), actions.BatchOptions{})
	if !fast.OK() {
		d.logf("withdrawing: %s", fast)
	}
	if errors.Is(fast.Err, actions.ErrBankStopped) {
		return got
	}
	// What arrived is shared out over the lines of each id in order.
	arrived := map[int]int{}
	for id, b := range before {
		arrived[id] = max(int(actions.CountByID(bank.Inventory(), int32(id)))-b, 0)
	}
	for i, dr := range draws {
		if dr.N <= 0 {
			continue
		}
		got[i] = min(dr.N, arrived[dr.ID])
		arrived[dr.ID] -= got[i]
		if got[i] < dr.N && (fast.Err != nil || BankCounts(bank)[dr.ID] > 0) {
			got[i] += WithdrawExact(d, bank, dr.ID, dr.N-got[i])
		} else if got[i] < dr.N {
			d.logf("withdrew %d of %d %s", got[i], dr.N, ObjName(d, dr.ID))
		}
	}
	return got
}

// BankView is the engine's cached view of the account's own bank: the last complete read, kept
// across steps so decide() can see it without a booth open. It is a hint between reads; every
// withdrawal re-reads the live panel.
type BankView struct {
	Counts map[int]int
	Known  bool
	At     time.Time
}

// Note records a fresh read.
func (b *BankView) Note(counts map[int]int) {
	b.Counts = counts
	b.Known = true
	b.At = time.Now()
}

// Describe is the log form.
func (b *BankView) Describe() string {
	if !b.Known {
		return "bank unread"
	}
	return fmt.Sprintf("bank read %s ago (%d kinds)", time.Since(b.At).Round(time.Second), len(b.Counts))
}
