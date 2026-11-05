package mastercraftrunner

// The anvil round's I/O (mastercraftplan/smith.go decides it and has the content facts: the interface, the
// columns and slots, the five ticks a make). Three things are here:
//
//   - SmithKit: one bank visit at Varrock west that banks the products and what is left, keeps the hammer and
//     the run staller's jewellery in the pack, and draws a FULL pack of bars (mastercraftplan.PlanSmithDraws);
//   - FindAnvil, with the furnace round's approach (a tile the engine accepts a use on, internal/locreach):
//     the anvil is found BY NAME through the loc index, never an id at a coordinate;
//   - SmithAtAnvil: a bar used on the anvil (OPLOCU) opens interface 994, and Make 10, Make 5 or Make 1 on
//     the product's column slot works the bars, batch after batch, until the pack holds none of that bar.

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/thieverrunner"
	"rsb0t-go/internal/scripts/tile"
)

// AnvilName is the loc the round uses (content loc 2783; the three Varrock west anvils at 3188,3421/3424/3426).
const AnvilName = "Anvil"

// VarrockWestBank is the stand tile of the Varrock west bank (the booths at 3186,3436-3446), the same tile
// questplan's Varrock west bank uses; the anvils are 10 to 15 tiles south of it.
var VarrockWestBank = tile.Tile{X: 3185, Z: 3436}

// VarrockAnvil is the middle of Varrock west's anvils, where FindAnvil looks from.
var VarrockAnvil = tile.Tile{X: 3188, Z: 3424}

// The pace of a make and the waits around it.
const (
	smithTick = 600 * time.Millisecond
	// smithMenuWait is how long the interface has to open after a bar is used on the anvil.
	smithMenuWait  = 3 * time.Second
	smithMenuTries = 3
	// smithStall is how long a Make may go without a bar being used before it is taken as stopped (a make
	// is five ticks, three seconds).
	smithStall = 9 * time.Second
	// smithAfterLast is the ticks after the last make's bars left the pack until the engine takes the next
	// op: the make's closing p_delay(2) is read on the fourth tick, so one sent when the client has seen the
	// third lands then (mastercraftplan/smith.go THE PACE).
	smithAfterLast = 3
)

// FindAnvil is the nearest loc named AnvilName within radius of near.
func FindAnvil(d *fleetrunner.Deps, near tile.Tile, radius int) (locindex.Match, bool) {
	if d.Locs == nil || d.Config == nil {
		return locindex.Match{}, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	return d.Locs.Nearest(d.Config, locindex.Query{X: near.X, Z: near.Z, Level: near.Level, Radius: radius, Name: AnvilName}, live)
}

// SmithOwn reads the anvil round's bars and the hammer out of a bank reading, by item name.
func SmithOwn(banked []gamestate.SlotItem) mastercraftplan.SmithStock {
	byName := map[string]int{mastercraftplan.Hammer: int(actions.CountByID(banked, mastercraftplan.HammerID))}
	for _, p := range mastercraftplan.SmithAllProducts() {
		byName[p.Bar] = int(actions.CountByID(banked, p.BarID))
	}
	return func(item string) int { return byName[item] }
}

// SmithPackBars reads the bars (and the hammer) in the pack, by item name.
func SmithPackBars(d *fleetrunner.Deps) mastercraftplan.SmithStock {
	byName := map[string]int{mastercraftplan.Hammer: packCount(d, mastercraftplan.HammerID)}
	for _, p := range mastercraftplan.SmithAllProducts() {
		byName[p.Bar] = packCount(d, p.BarID)
	}
	return func(item string) int { return byName[item] }
}

// SmithProducts are what an anvil round hands the mule: the arrowtips of every metal and the Iron platebody.
func SmithProducts() []Product {
	var out []Product
	for _, p := range mastercraftplan.SmithAllProducts() {
		out = append(out, Product{ID: p.ID, Name: p.Name})
	}
	return out
}

// PlateProducts are the platebodies the platebody round hands the mule: every metal's (mastercraftplan.PlateKinds).
func PlateProducts() []Product {
	var out []Product
	for _, p := range mastercraftplan.PlateKinds {
		out = append(out, Product{ID: p.ID, Name: p.Name})
	}
	return out
}

// SmithProductCounts reads the made products out of a bank reading, by product name.
func SmithProductCounts(banked []gamestate.SlotItem) map[string]int {
	return CountProducts(banked, SmithProducts())
}

// ---- the kit ----

// SmithKitResult is what a bank visit did.
type SmithKitResult struct {
	// Banked is the bank as it was read after the deposit; Draws what was drawn (bars per product, as
	// planned), Took the visit from the open bank to the closed one.
	Banked []gamestate.SlotItem
	Draws  []mastercraftplan.SmithDraw
	Took   time.Duration
	// NoHammer says the pack holds no hammer and the bank none to draw.
	NoHammer bool
	// Free is the pack slots left free after the hammer and the jewellery, before the bars.
	Free int
	Err  string
}

// smithCoinsID is the Coins obj (content 289 obj.pack).
const smithCoinsID = 995

// SmithKit banks the pack but the hammer and the run staller's jewellery, in an open bank, reads the bank,
// draws the hammer when the pack has none, one rub item when the Travel carries a staller, and a full pack of
// bars for products, then closes it. Every deposit goes out in one batch and every withdrawal in another
// (actions.Bank.Batch); a withdrawal the batch did not confirm is made again one at a time.
func SmithKit(d *fleetrunner.Deps, bank *actions.Bank, products []mastercraftplan.SmithProduct, travel *fleetrunner.Travel) (out SmithKitResult) {
	start := time.Now()
	defer func() { out.Took = time.Since(start) }()
	w := thieverrunner.Waiter(fleetrunner.Thiev(d))
	st := d.Session.State()
	if err := bank.SetNoteMode(false, w); err != nil {
		out.Err = "item mode: " + err.Error()
		return out
	}
	keep := func(it gamestate.SlotItem) bool {
		if it.ID == mastercraftplan.HammerID {
			return false
		}
		if it.ID == fleetplan.StrangeFruitID {
			return false // the run keeper's fruit (Travel.KeepRunning)
		}
		if it.ID == smithCoinsID && travel != nil && travel.Alch.Enabled {
			return false // the walk alchemy's coins land on this stack, so a full pack of bars still has room for them
		}
		if travel != nil && d.Config != nil {
			if o, ok := d.Config.Obj(int(it.ID)); ok {
				if travel.Rub && !o.Noted() && fleetplan.IsRubItem(o.Name) {
					return false
				}
				// The walk alchemy's kit (natures, fire runes, the noted alch stock) rides every trip.
				if travel.AlchKeeps(d, o.Name) {
					return false
				}
			}
		}
		return true // deposit it
	}
	packBefore := len(bank.Inventory())
	bankGen := invGen(st, actions.BankWithdraw)
	if err := bank.BatchDepositAllMatching(keep, w); err != nil {
		out.Err = "deposit: " + err.Error()
	}
	if packBefore > 0 {
		waitUntil(d, func() bool { return invGen(st, actions.BankWithdraw) > bankGen }, bankSettle)
	}
	out.Banked = append([]gamestate.SlotItem(nil), bank.Items()...)
	inBank := SmithOwn(out.Banked)
	if packCount(d, mastercraftplan.HammerID) == 0 {
		if inBank(mastercraftplan.Hammer) == 0 {
			out.NoHammer = true
			_ = bank.Close()
			return out
		}
		if err := bank.WithdrawOne(mastercraftplan.HammerID); err != nil {
			out.Err = "withdraw hammer: " + err.Error()
		}
		waitUntil(d, func() bool { return packCount(d, mastercraftplan.HammerID) > 0 }, 3*time.Second)
	}
	travel.DrawRubItem(d, bank, w) // one Games necklace or Ring of dueling, when the bank holds one and the pack none
	if travel != nil && travel.Alch.Enabled {
		travel.DrawAlchKit(d, bank, w) // the walk alchemy's natures, fire runes and noted alch stock
	}
	out.Free = fleetrunner.FreeSlots(d)
	draws := mastercraftplan.PlanSmithDraws(products, inBank, out.Free)
	ops := make([]actions.BankOp, 0, len(draws))
	for _, dr := range draws {
		ops = append(ops, actions.WithdrawOp(dr.Product.BarID, int32(dr.N)))
	}
	if len(ops) > 0 {
		res := bank.Batch(ops, w, actions.BatchOptions{})
		if res.Err != nil {
			out.Err = "withdraw: " + res.Err.Error()
		}
		for _, sh := range res.Short {
			if sh.Op.Kind != actions.BankOpWithdraw || sh.Why == "the bank holds none" || sh.Why == "the pack filled" {
				continue
			}
			if rest := int32(sh.Want - sh.Got); rest > 0 {
				if err := bank.WithdrawX(sh.Op.ID, rest, w); err != nil {
					logf(d, "mastercrafter: smith: withdrawing %d of item %d: %v", rest, sh.Op.ID, err)
				}
			}
		}
	}
	out.Draws = draws
	_ = bank.Close()
	waitUntil(d, func() bool { return !bank.IsOpen() }, time.Second)
	return out
}

// ---- the anvil ----

// AnvilStands is the tiles the engine accepts a use on the anvil from, nearest to from first (the furnace's
// locreach model, which any loc has).
func AnvilStands(d *fleetrunner.Deps, anvil locindex.Match, from tile.Tile) []tile.Tile {
	return FurnaceStands(d, anvil, from)
}

// AnvilReached is whether the account stands where the engine accepts a use on the anvil.
func AnvilReached(d *fleetrunner.Deps, anvil locindex.Match) bool { return FurnaceReached(d, anvil) }

// ApproachAnvil walks the account onto a tile the engine accepts a use on the anvil from (see
// ApproachFurnace, whose walk and locreach it shares); the walk is the round's run-stalled WalkTo.
func ApproachAnvil(d *fleetrunner.Deps, anvil locindex.Match, walk WalkFunc, first time.Duration) bool {
	return ApproachFurnace(d, anvil, walk, first)
}

// smithMenu reports whether the smithing interface is the open main modal and shows the product in its
// column slot (the bar's metal, the level and a hammer let it).
func smithMenu(d *fleetrunner.Deps, p mastercraftplan.SmithProduct) bool {
	st := d.Session.State()
	if main, _, _ := st.Modals(); main != mastercraftplan.SmithInterface {
		return false
	}
	inv := st.Inventory(p.Com)
	if inv == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if it.Slot == p.Slot && it.ID == p.ID {
			return true
		}
	}
	return false
}

// SmithResult is one SmithAtAnvil.
type SmithResult struct {
	// Bars is the bars of the product used, Makes the makes they paid for, Uses the interface uses (a bar
	// used on the anvil and a Make button), XP the Smithing XP in displayed points, Ticks the server ticks it
	// spanned.
	Bars, Makes, Uses, XP, Ticks int
	// Reason is "done" (the pack holds no more of the bar), "no-hammer", "level", "members", "no-menu" (the
	// interface would not open or showed no such product), "stalled", "held" or "stopped".
	Reason string
}

// Describe is a one-line account of a SmithAtAnvil for a log.
func (r SmithResult) Describe(p mastercraftplan.SmithProduct) string {
	return fmt.Sprintf("%d x %s -> %d make(s) of %s from %d use(s) of the anvil over %d tick(s), +%d Smithing XP (%s)", r.Bars, p.Bar, r.Makes, p.Name, r.Uses, r.Ticks, r.XP, r.Reason)
}

// SmithAtAnvil works the pack's bars of p on the anvil, Make 10 at a time (Make 5, then Make 1 for the rest),
// until the pack holds fewer than a make's bars. The account must stand where the engine accepts a use
// (ApproachAnvil). hold runs before every use. Each batch's menu is opened afresh (the make ends with the
// interface closed), the next use being sent as soon as the closing delay of the last make is over.
func SmithAtAnvil(d *fleetrunner.Deps, anvil locindex.Match, p mastercraftplan.SmithProduct, hold func() bool) (res SmithResult) {
	st := d.Session.State()
	bars0 := packCount(d, p.BarID)
	_, xp0 := statOf(d, SmithingStat)
	tick0 := st.Tick()
	defer func() {
		res.Bars = max(0, bars0-packCount(d, p.BarID))
		res.Makes = res.Bars / max(1, p.BarsPer)
		_, xp := statOf(d, SmithingStat)
		res.XP, res.Ticks = xp-xp0, st.Tick()-tick0
	}()
	lastDone := -1 // the tick the previous batch's last bars left the pack
	for {
		if hold != nil && !hold() {
			res.Reason = "held"
			return res
		}
		bars := packCount(d, p.BarID)
		if bars < p.BarsPer {
			res.Reason = "done"
			return res
		}
		if packCount(d, mastercraftplan.HammerID) == 0 {
			res.Reason = "no-hammer"
			return res
		}
		dismissChat(d)
		if lastDone >= 0 {
			wait := time.Now().Add(smithAfterLast * smithTick * 2)
			for st.Tick() < lastDone+smithAfterLast && time.Now().Before(wait) {
				if !d.Delay(10 * time.Millisecond) {
					res.Reason = "stopped"
					return res
				}
			}
		}
		opened := false
		for try := 0; try < smithMenuTries && !opened; try++ {
			if try > 0 && !d.Delay(900*time.Millisecond) {
				res.Reason = "stopped"
				return res
			}
			slot := -1
			for _, it := range packItems(d) {
				if it.ID == p.BarID {
					slot = it.Slot
					break
				}
			}
			if slot < 0 {
				res.Reason = "done"
				return res
			}
			at := time.Now()
			if err := actions.OpLocU(d.Session, anvil.X, anvil.Z, anvil.ID, p.BarID, slot, fleetrunner.PlayerPack); err != nil {
				logf(d, "mastercrafter: smith: OpLocU(%s on the anvil): %v", p.Bar, err)
				res.Reason = "stalled"
				return res
			}
			res.Uses++
			waitUntil(d, func() bool {
				return smithMenu(d, p) || mastercraftplan.SmithRefusal(d.Chat.Since(at)) != ""
			}, smithMenuWait)
			if smithMenu(d, p) {
				opened = true
				break
			}
			switch why := mastercraftplan.SmithRefusal(d.Chat.Since(at)); why {
			case "level", "members", "hammer":
				res.Reason = map[string]string{"level": "level", "members": "members", "hammer": "no-hammer"}[why]
				return res
			}
		}
		if !opened {
			res.Reason = "no-menu"
			return res
		}
		option, count := mastercraftplan.SmithButton(bars / p.BarsPer)
		used := count * p.BarsPer
		if err := actions.InvButton(d.Session, option, p.ID, p.Slot, p.Com); err != nil {
			logf(d, "mastercrafter: smith: Make %d %s: %v", count, p.Name, err)
			res.Reason = "stalled"
			return res
		}
		// The bars leave the pack one make at a time, five ticks apart; the batch is over when all of them
		// have, or when none has left for a while.
		deadline := time.Now().Add(time.Duration(count*mastercraftplan.SmithTicks+4) * smithTick)
		last, lastAt := bars, time.Now()
		pressed := lastAt
		for time.Now().Before(deadline) {
			// A refusal says so within a tick: a free world's "members' server", a level the menu let by.
			if why := mastercraftplan.SmithRefusal(d.Chat.Since(pressed)); why != "" && last == bars {
				res.Reason = map[string]string{"level": "level", "members": "members", "hammer": "no-hammer", "bars": "done"}[why]
				return res
			}
			if now := packCount(d, p.BarID); now < last {
				last, lastAt = now, time.Now()
			}
			if bars-last >= used || time.Since(lastAt) > smithStall {
				break
			}
			if !d.Delay(quickPoll) {
				res.Reason = "stopped"
				return res
			}
		}
		if last == bars {
			res.Reason = "stalled"
			return res
		}
		lastDone = st.Tick()
	}
}
