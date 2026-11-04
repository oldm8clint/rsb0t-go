// Package shoprunner is the I/O half of the ShopBuyout port: walking between a bank and one
// shopkeeper, opening the shop, buying stock out and banking the haul, driven by the pure
// decisions in internal/scripts/shopplan. It follows the exact split
// internal/scripts/gemrunner, herbrunner and flaxrunner already proved — a pure plan package
// decides, this package does the walking/clicking/banking, and internal/bot/shopbuyout.go
// wires the two together. See docs/script_porting_framework.md for the recipe.
//
// Buying goes through actions.Shop.BuyCount, which batches a quantity into the Buy-10/5/1
// options a real shop panel actually offers (the engine drops user-event packets past five a
// tick) and reports how many units landed in the pack. Opening the shop goes through
// entityquery.OpenShop, the port of Shop.open(npcName) — which until this script had no live
// caller at all (docs/script_api_coverage.md marks it "unit-tested, needs live check").
//
// Every item here is matched by OBJECT ID, never by display name. That is not a style choice:
// the live config archive resolves "Shortbow" to four object ids, "Bolts" and even "Coins" to
// two each, so a name match can pick a note, a members-only twin, or a different item. See
// shopplan's own package doc comment for both id traps this table sits on.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep, so
// internal/bot's Context.Delay can cancel a running script mid-wait.
//
// Deliberately NOT ported here: the paint/status overlay (ShopBuyout.ts's own onPaint) — no
// headless equivalent, the same boundary every other ported script draws.
package shoprunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/shopplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id — see flaxrunner.PlayerPack's own
// doc comment for why this fixed component is read instead of scanning every known inventory.
// It matters more here than usual: a shopkeeper's stock is itself an inventory, and counting
// across every known one would add the shop's 2000 fire runes to the pack's.
const PlayerPack = 3214

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one buy/bank cycle needs from the outside world.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Components is the parsed interface archive the bank needs to resolve its deposit-side
	// component; nil makes OpenBank fail clearly rather than opening a bank it cannot read.
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Walker, when set, is tried whenever the same-level pathfinder cannot plan a route at
	// all — it walks doors, stairs and (with hops installed) ships.
	//
	// Why this exists: pathfind.Pack.FindPath is a SAME-LEVEL planner and answers
	// "unreachable" for anything off-scene, so a script that assumes it is standing at one
	// bank cannot get home after a death or a fail-teleport without this. The GemCutter port
	// proved that gap live (19 consecutive "unreachable" answers after a mid-run teleport);
	// this port carries the escalation from the start rather than rediscovering it. Nil keeps
	// plain same-level pathfinding.
	Walker *webwalk.Walker
}

func (d *Deps) logf(format string, args ...interface{}) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func currentTile(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

func waitFor(d *Deps, what string, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

// emergencyFleeDriftTiles is 0 and emergencyFleeHome is unset for the same reason
// gemrunner/herbrunner/flaxrunner leave theirs: there is no single "home" worth dragging an
// account back to mid-event.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — duplicated per this
// project's established per-package convention, see essencerunner.WaitForGuardian's own doc
// comment for why these small helpers are copied rather than shared. Uses randomevents.Busy
// (cooldown-aware) rather than bare Detect(), which would block forever on an event Guardian
// has given up on.
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
		if time.Since(lastLog) >= guardianWaitLogEvery {
			d.logf("[guardian] still waiting on a random event")
			lastLog = time.Now()
		}
	}
	return true
}

// walkNearTimeout is 120s, this project's floor for a "walk somewhere nearby" primitive: the
// framework's own bug-class list records several accounts giving up at a real,
// distance-appropriate waypoint under a 30s cap tuned against a local engine.
const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops, until
// within radius tiles. Duplicated per this project's established per-package convention — see
// herbrunner.WalkNear's own twin.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		here := currentTile(d)
		if tile.Chebyshev(here, dest) <= radius && here.Level == dest.Level {
			return true
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
			// Escalate to webwalk rather than giving up: an unreachable direct path is exactly
			// the cross-map case webwalk exists for. See Deps.Walker's own doc comment.
			if d.Walker != nil {
				return walkWeb(d, dest, radius, time.Until(deadline))
			}
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		wp := res.Waypoints[len(res.Waypoints)-1]
		d.logf("walk from %d,%d toward %d,%d (%d corners)", here.X, here.Z, wp.X, wp.Z, len(res.Waypoints))
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("walk: %v", err)
			return false
		}
		lastX, lastZ, still := here.X, here.Z, time.Now()
		for time.Now().Before(deadline) {
			randomevents.EmergencyFlee(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur := currentTile(d)
			if tile.Chebyshev(cur, dest) <= radius && cur.Level == dest.Level {
				return true
			}
			if cur.X != lastX || cur.Z != lastZ {
				lastX, lastZ, still = cur.X, cur.Z, time.Now()
			} else if time.Since(still) > 2*time.Second {
				break
			}
		}
	}
	here := currentTile(d)
	d.logf("gave up walking; stopped at %d,%d,%d", here.X, here.Z, here.Level)
	return false
}

// fresh adapts d to freshstartrunner.Deps, the same field-for-field copy as thieverrunner.fresh,
// for freshstartrunner.WalkToUse.
func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
		Walker: d.Walker,
	}
}

// walkWeb runs one d.Walker walk, reporting only whether it arrived. Kept deliberately simpler
// than barbarianrunner.walkWeb's arrived/died/stopped triple: this script has no course
// position to lose, so a death on the way is just another reason it did not arrive and the
// next Loop call re-derives the stage from live state.
func walkWeb(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = walkNearTimeout
	}
	res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{
		Radius:  radius,
		Timeout: timeout,
		Combat:  webwalk.CombatOptions{RunWhenAttacked: true, RunBelowHP: 50},
	})
	if res.Outcome == webwalk.OutcomeArrived {
		d.logf("webwalked to %d,%d,%d", dest.X, dest.Z, dest.Level)
		return true
	}
	d.logf("webwalk to %d,%d,%d ended %s (%s)", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason)
	return false
}

// ---- live reads ----

func packSlots(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// countIn totals object id across one inventory snapshot. A count below 1 is treated as 1
// because an unstackable slot reports its quantity as 0 or 1 depending on the update path.
func countIn(items []gamestate.SlotItem, id int32) int32 {
	n := int32(0)
	for _, it := range items {
		if it.ID != id {
			continue
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// CountByID totals the pack's items with object id — the only matcher this package uses. See
// the package doc comment for why names cannot work here.
func CountByID(d *Deps, id int32) int32 { return countIn(packSlots(d), id) }

// CoinsHeld is real coins in the pack. Matched by shopplan.CoinsID because the display name
// "Coins" resolves to two objects on this build.
func CoinsHeld(d *Deps) int32 { return CountByID(d, shopplan.CoinsID) }

// UsedSlots is how many pack slots hold anything.
func UsedSlots(d *Deps) int { return len(packSlots(d)) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	free := shopplan.PackSlots - UsedSlots(d)
	if free < 0 {
		return 0
	}
	return free
}

// PackFull reports a pack with no free slot.
func PackFull(d *Deps) bool { return FreeSlots(d) <= 0 }

// HaulSlots counts the pack slots holding something other than coins — what a bank trip has
// to deposit, and what tells a caller the run is actually buying things.
func HaulSlots(d *Deps) int {
	n := 0
	for _, it := range packSlots(d) {
		if it.ID != shopplan.CoinsID {
			n++
		}
	}
	return n
}

func waiterFor(d *Deps) actions.Waiter { return actionsWaiter{d} }

type actionsWaiter struct{ d *Deps }

func (w actionsWaiter) Delay(t time.Duration) bool { return w.d.Delay(t) }

func (w actionsWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
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

func (w actionsWaiter) DelayTicks(n int) bool {
	return w.d.Delay(time.Duration(n) * 600 * time.Millisecond)
}

// ---- the shop ----

// shopOpenTimeout bounds one attempt to get the keeper's shop on screen.
const shopOpenTimeout = 10 * time.Second

// OpenKeeperShop walks to the shop's stand tile and opens the keeper's shop, leaving it open
// for the caller to drive. It returns the live actions.Shop handle.
//
// The walk happens here rather than inside entityquery.OpenShop on purpose: upstream's
// Shop.open does not walk either, and this engine does not path to an out-of-reach NPC, so an
// open sent from across the room simply does nothing.
func OpenKeeperShop(d *Deps, shop shopplan.Shop) (*actions.Shop, bool) {
	sh := actions.NewShop(d.Session)
	if sh.IsOpen() {
		return sh, true
	}
	if !WalkNear(d, shop.ShopStand, 3) {
		d.logf("could not reach %s's stand at %d,%d", shop.Keeper, shop.ShopStand.X, shop.ShopStand.Z)
		return nil, false
	}
	if err := entityquery.OpenShop(d.Session, d.Config, shop.Keeper, waiterFor(d)); err != nil {
		d.logf("opening %s's shop: %v", shop.Keeper, err)
		return nil, false
	}
	if !waitFor(d, fmt.Sprintf("%s's shop to open", shop.Keeper), shopOpenTimeout, sh.IsOpen) {
		return nil, false
	}
	return sh, true
}

// StockByID reads the open shop's stock into the map BuyoutPlan takes, keeping only ids this
// preset knows. An id the table does not list is skipped rather than guessed at: without a
// cost and a baseline there is no price model for it.
func StockByID(sh *actions.Shop, shop shopplan.Shop) map[int32]int32 {
	stock := map[int32]int32{}
	for _, row := range sh.Stock() {
		if _, ok := shop.ItemByID(row.ID); !ok {
			continue
		}
		if row.Count > 0 {
			stock[row.ID] = row.Count
		}
	}
	return stock
}

// BuyResult is one planned line's real outcome, measured rather than assumed.
type BuyResult struct {
	Item shopplan.Item
	// Bought is how many units actually landed in the pack.
	Bought int32
	// Spent is the real coin delta over the purchase. It is the authority on cost: the plan's
	// EstCost is an estimate, and the shelf can move between planning and clicking.
	Spent int32
	// Reason is "bought", "nothing", "no-coins" or an error string.
	Reason string
}

// BuyWant buys one planned line and reports what really changed hands. Counting both sides
// from the shop's own pack component keeps the measurement consistent with what BuyCount
// itself watches.
func BuyWant(d *Deps, sh *actions.Shop, want shopplan.Want) BuyResult {
	res := BuyResult{Item: want.Item}
	if want.Units <= 0 {
		res.Reason = "nothing"
		return res
	}
	coinsBefore := countIn(sh.Pack(), shopplan.CoinsID)
	if coinsBefore <= 0 {
		res.Reason = "no-coins"
		return res
	}
	held := countIn(sh.Pack(), want.Item.ID)

	bought, err := sh.BuyCount(want.Item.ID, want.Units, waiterFor(d))
	if err != nil {
		res.Reason = err.Error()
	}
	// Let the last click of the batch settle before measuring, so a purchase is not reported
	// short just because the final tick had not landed.
	d.Delay(700 * time.Millisecond)

	landed := countIn(sh.Pack(), want.Item.ID) - held
	if landed < 0 {
		landed = 0
	}
	// BuyCount's own return and the pack delta should agree; when they do not, the pack is
	// the authority — it is the thing the account actually owns.
	if landed != bought {
		d.logf("%s: BuyCount reported %d but the pack gained %d — trusting the pack", want.Item.Name, bought, landed)
	}
	res.Bought = landed
	res.Spent = coinsBefore - countIn(sh.Pack(), shopplan.CoinsID)
	if res.Spent < 0 {
		res.Spent = 0
	}
	if res.Reason == "" {
		if res.Bought > 0 {
			res.Reason = "bought"
		} else {
			res.Reason = "nothing"
		}
	}
	return res
}

// ---- the bank ----

// openNearestBankBooth tries every matching booth near (x,z,level) nearest-first — duplicated
// per this project's convention, and the nearest-first retry specifically recovers from the
// live-found bad-bake case essencerunner.openNearestBankBooth's own doc comment records (one
// account timed out beside a booth the static bake placed slightly wrong while another opened
// a different instance two tiles along).
func openNearestBankBooth(d *Deps, shop shopplan.Shop, boothName string, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: boothName, Op: shop.BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("could not find a %q near %d,%d", boothName, x, z)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			d.logf("gave up after %d bank booth candidates near %d,%d", maxTries, x, z)
			return nil, false
		}
		// Onto a tile the booth answers from: the server ignores a click from a diagonal one in
		// silence (freshstartrunner.WalkToUse).
		if !freshstartrunner.WalkToUse(fresh(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, walkNearTimeout) {
			continue
		}
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, booth.Op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		if open, _ := bank.AwaitOpen(mark, actions.DelayWaiter(d.Delay), timeout); open {
			return bank, true
		}
	}
	return nil, false
}

// OpenBank walks to the preset's bank stand and opens its booth, leaving it open for the
// caller to drive its own deposit/withdraw sequence.
func OpenBank(d *Deps, shop shopplan.Shop, boothName string) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, shop.BankStand, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, shop, boothName, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAllExceptIDs deposits every pack item whose object id is not in keep, waiting for
// each deposit to clear. It returns how many distinct item ids were deposited, and which ids
// REFUSED to leave the pack.
//
// Callers keep coins (shopplan.CoinsID), so the next trip does not have to withdraw them all
// over again — which is ShopBuyout.ts's own depositAllMatching(name => name !== 'Coins').
//
// Why stuck is reported rather than swallowed: the HerbCleaner port found a live run
// collecting "Strange box" random-event items (object 3062) that the bank simply will not
// accept; every trip re-tried the same id, burned the full deposit timeout, and shrank the
// next load because those slots never freed. That is this project's own bug class #4
// (docs/script_porting_framework.md) and the established fix is the same one
// donorrunner.Options.KnownUntradable uses: the CALLER remembers the id — so it survives
// across many calls — and passes it back in keep, which is why this function only reports.
func DepositAllExceptIDs(d *Deps, bank *actions.Bank, keep map[int32]bool) (deposited int, stuck []int32) {
	var ids []int32
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] || keep[it.ID] {
			continue
		}
		seen[it.ID] = true
		ids = append(ids, it.ID)
	}
	// One burst, five Deposit-Alls a tick, confirmed once (twopertick-1012).
	done, stuck, r, retried := bank.DepositKinds(ids, actions.DelayWaiter(d.Delay))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("shop deposit", len(ids), r, retried))
	}
	return len(done), stuck
}

// BankCount is how many of object id the open bank holds.
func BankCount(bank *actions.Bank, id int32) int32 { return countIn(bank.Items(), id) }

// WithdrawCoins takes up to n coins out of an open bank and reports how many actually landed
// — 0 means the bank has none, which is how a caller confirms it dry.
func WithdrawCoins(d *Deps, bank *actions.Bank, n int32) int32 {
	if n <= 0 || BankCount(bank, shopplan.CoinsID) <= 0 {
		return 0
	}
	before := CoinsHeld(d)
	if err := bank.WithdrawX(shopplan.CoinsID, n, waiterFor(d)); err != nil {
		d.logf("withdraw %d coins: %v", n, err)
	}
	waitFor(d, "coins to land in the pack", 4*time.Second, func() bool {
		return CoinsHeld(d) > before
	})
	got := CoinsHeld(d) - before
	if got < 0 {
		return 0
	}
	return got
}

// DescribeItem names an object for a log line, falling back to its id. Used only for logging
// an id the preset table does not know — never for matching.
func DescribeItem(d *Deps, id int32) string {
	if d.Config != nil {
		if obj, ok := d.Config.Obj(int(id)); ok && strings.TrimSpace(obj.Name) != "" {
			return fmt.Sprintf("%q (object %d)", obj.Name, id)
		}
	}
	return fmt.Sprintf("object %d", id)
}
