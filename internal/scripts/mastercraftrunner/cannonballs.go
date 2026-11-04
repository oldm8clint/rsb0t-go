package mastercraftrunner

// The cannonball round's I/O (mastercraftplan/cannonballs.go decides it and has the content facts: the smelt, the mould, four balls a
// bar, the pace). Three things are here:
//
//   - CannonKit: one bank visit that banks the pack but the Ammo mould, draws the mould when the pack has none, and draws a full pack
//     of Steel bars (mastercraftplan.CannonDraw);
//   - the furnace: FindFurnace, ApproachFurnace and FurnaceReached (jewellery.go) find it BY NAME and option through the loc index and
//     put the account on a tile the engine accepts a use from;
//   - SmeltCannonballs: a Steel bar used on the furnace (OPLOCU), one bar a use (cannonballs.rs2 has no menu and no count), the next
//     use sent as soon as the four balls show.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
)

// CannonOwn reads the cannonball round's items out of a bank reading: the Steel bars, the Cannonballs and the Ammo mould.
func CannonOwn(banked []gamestate.SlotItem) (steel, balls int, mould bool) {
	return int(actions.CountByID(banked, mastercraftplan.SteelBarID)),
		int(actions.CountByID(banked, mastercraftplan.CannonballID)),
		actions.CountByID(banked, mastercraftplan.AmmoMouldID) > 0
}

// CannonProducts is what a cannonball round hands the mule.
func CannonProducts() []Product {
	return []Product{{ID: mastercraftplan.CannonballID, Name: mastercraftplan.Cannonball}}
}

// ---- the kit ----

// CannonKitResult is what a bank visit did.
type CannonKitResult struct {
	// Banked is the bank as it was read after the deposit; Drew the Steel bars drawn; Mould says the pack holds an Ammo mould when the
	// visit ends; Took the visit from the open bank to the closed one.
	Banked []gamestate.SlotItem
	Drew   int
	Mould  bool
	Took   time.Duration
	// NoMould says the pack held none and the bank has none to draw.
	NoMould bool
	// Free is the pack slots free after the mould, before the bars.
	Free int
	Err  string
}

// CannonKit banks the pack but the Ammo mould, in an open bank, reads the bank, draws the mould when the pack has none and then up to
// remaining Steel bars (as many as the free slots hold), and closes it. Every deposit goes out in one batch and every withdrawal in
// another (actions.Bank.Batch); a withdrawal the batch did not confirm is made again one at a time.
func CannonKit(d *fleetrunner.Deps, bank *actions.Bank, remaining int) (out CannonKitResult) {
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	st := d.Session.State()
	if err := bank.SetNoteMode(false, w); err != nil {
		out.Err = "item mode: " + err.Error()
		return out
	}
	keep := func(it gamestate.SlotItem) bool { return it.ID != mastercraftplan.AmmoMouldID }
	packBefore := len(bank.Inventory())
	bankGen := invGen(st, actions.BankWithdraw)
	if err := bank.BatchDepositAllMatching(keep, w); err != nil {
		out.Err = "deposit: " + err.Error()
	}
	if packBefore > 0 {
		waitUntil(d, func() bool { return invGen(st, actions.BankWithdraw) > bankGen }, bankSettle)
	}
	out.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	steel, _, mouldBanked := CannonOwn(out.Banked)
	if packCount(d, mastercraftplan.AmmoMouldID) == 0 {
		if !mouldBanked {
			out.NoMould = true
			_ = bank.Close()
			return out
		}
		if err := bank.WithdrawOne(mastercraftplan.AmmoMouldID); err != nil {
			out.Err = "withdraw mould: " + err.Error()
		}
		waitUntil(d, func() bool { return packCount(d, mastercraftplan.AmmoMouldID) > 0 }, 3*time.Second)
	}
	out.Mould = packCount(d, mastercraftplan.AmmoMouldID) > 0
	out.Free = fleetrunner.FreeSlots(d)
	if n := mastercraftplan.CannonDraw(out.Free, remaining, steel); n > 0 && out.Mould {
		res := bank.Batch([]actions.BankOp{actions.WithdrawOp(mastercraftplan.SteelBarID, int32(n))}, w, actions.BatchOptions{})
		if res.Err != nil {
			out.Err = "withdraw: " + res.Err.Error()
		}
		for _, sh := range res.Short {
			if sh.Op.Kind != actions.BankOpWithdraw || sh.Why == "the bank holds none" || sh.Why == "the pack filled" {
				continue
			}
			if rest := int32(sh.Want - sh.Got); rest > 0 {
				if err := bank.WithdrawX(sh.Op.ID, rest, w); err != nil {
					logf(d, "mastercrafter: cannonballs: withdrawing %d steel bars: %v", rest, err)
				}
			}
		}
		out.Drew = packCount(d, mastercraftplan.SteelBarID)
	}
	_ = bank.Close()
	waitUntil(d, func() bool { return !bank.IsOpen() }, time.Second)
	return out
}

// ---- the furnace ----

// The pace of a smelt and the waits around it.
const (
	cannonTick = 600 * time.Millisecond
	// cannonRetries is how many uses in a row may show nothing before the smelt is taken as stalled.
	cannonRetries = 3
)

// cannonWait is how long a bar has to show its four balls: ten ticks, and four of margin (a lagging account, a congratulations
// message box that swallowed the use). A var: a test shortens it.
var cannonWait = (mastercraftplan.CannonBarTicks + 4) * cannonTick

// CannonResult is one SmeltCannonballs.
type CannonResult struct {
	// Bars is the Steel bars smelted, Balls the cannonballs the pack gained, Uses the uses of the furnace, XP the Smithing XP gained in
	// displayed points, Ticks the server ticks it spanned.
	Bars, Balls, Uses, XP, Ticks int
	// Reason is "done" (no Steel bar left in the pack), "no-mould", "level", "members", "reach" (the engine would not take the use from
	// here), "stalled", "held" or "stopped".
	Reason string
}

// Describe is a one-line account of a SmeltCannonballs for a log.
func (r CannonResult) Describe() string {
	return fmt.Sprintf("%d Steel bar(s) -> %d cannonballs from %d use(s) of the furnace over %d tick(s), +%d Smithing XP (%s)", r.Bars, r.Balls, r.Uses, r.Ticks, r.XP, r.Reason)
}

// SmeltCannonballs uses the pack's Steel bars on the furnace one at a time until the pack holds none, each use measured by the four
// cannonballs that arrive. The account must stand where the engine accepts a use (ApproachFurnace). hold runs before every use.
// A use read while the player is still delayed is dropped without a word, so a use that shows nothing is sent again, at most
// cannonRetries times in a row.
func SmeltCannonballs(d *fleetrunner.Deps, furn locindex.Match, hold func() bool) (res CannonResult) {
	st := d.Session.State()
	balls0 := packCount(d, mastercraftplan.CannonballID)
	_, xp0 := statOf(d, SmithingStat)
	tick0 := st.Tick()
	defer func() {
		res.Balls = max(0, packCount(d, mastercraftplan.CannonballID)-balls0)
		_, xp := statOf(d, SmithingStat)
		res.XP, res.Ticks = xp-xp0, st.Tick()-tick0
	}()
	idle := 0
	for {
		if hold != nil && !hold() {
			res.Reason = "held"
			return res
		}
		if packCount(d, mastercraftplan.AmmoMouldID) == 0 {
			res.Reason = "no-mould"
			return res
		}
		slot := -1
		for _, it := range packItems(d) {
			if it.ID == mastercraftplan.SteelBarID {
				slot = it.Slot
				break
			}
		}
		if slot < 0 {
			res.Reason = "done"
			return res
		}
		dismissChat(d)
		before := packCount(d, mastercraftplan.CannonballID)
		at := time.Now()
		if err := actions.OpLocU(d.Session, furn.X, furn.Z, furn.ID, mastercraftplan.SteelBarID, slot, fleetrunner.PlayerPack); err != nil {
			logf(d, "mastercrafter: cannonballs: OpLocU(a steel bar on the furnace): %v", err)
			res.Reason = "stalled"
			return res
		}
		res.Uses++
		waitUntil(d, func() bool {
			return packCount(d, mastercraftplan.CannonballID) >= before+mastercraftplan.CannonballsPerBar || mastercraftplan.CannonballRefusal(d.Chat.Since(at)) != ""
		}, cannonWait)
		if packCount(d, mastercraftplan.CannonballID) >= before+mastercraftplan.CannonballsPerBar {
			res.Bars++
			idle = 0
			continue
		}
		switch mastercraftplan.CannonballRefusal(d.Chat.Since(at)) {
		case "members":
			res.Reason = "members"
			return res
		case "level":
			res.Reason = "level"
			return res
		case "mould":
			res.Reason = "no-mould"
			return res
		case "bar":
			res.Reason = "done"
			return res
		}
		if idle++; idle >= cannonRetries {
			res.Reason = "stalled"
			if d.Config != nil && !FurnaceReached(d, furn) {
				res.Reason = "reach"
			}
			return res
		}
		if !d.Delay(600 * time.Millisecond) {
			res.Reason = "stopped"
			return res
		}
	}
}
