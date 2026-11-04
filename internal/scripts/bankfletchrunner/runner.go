// Package bankfletchrunner is the I/O half of the BankFletcher port: banking a knife and logs,
// then fletching arrow shafts via the make menu, driven by the pure decisions in
// internal/scripts/bankfletchplan. Follows the exact split every other runner package already
// proved — see docs/script_porting_framework.md.
//
// Fletching itself uses actions.Equipment.UseOnHeld (OPHELDU: knife on logs — a real,
// already-proven primitive, confirmed by its own test coverage and doc comment before this port
// reused it, not invented for this port) to open the make menu, then
// actions.ChatDialog.MakeX (also already proven, chatdialog_make_test.go) to batch-produce.
//
// Deliberately NOT ported here: the paint/status overlay — no headless equivalent.
package bankfletchrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/bankfletchplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// PlayerPack is the persistent inventory-tab component id.
const PlayerPack = 3214

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one bank/fletch cycle needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Loot receives what a fletching batch really made, for the Loot panel's Fletching card
	// (see fletchcount.go). Nil records nothing, which is what a bare test Deps and every
	// standalone harness gets; internal/bot passes bot.Context.Loot.
	Loot func(loot.Drop)
}

func (d *Deps) logf(format string, args ...any) {
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

const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all.
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

const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there, stopping once within radius tiles — duplicated per
// this project's established per-package convention (see essencerunner.WalkNear's own doc
// comment). No live-found door gap has been hit on this port's own Seers bank route (unlike
// vialrunner's Falador leg), so this stays a plain copy rather than delegating to
// freshstartrunner.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		here := currentTile(d)
		if tile.Chebyshev(here, dest) <= radius {
			return true
		}
		res := d.Pack.FindPath(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("no path from %d,%d,%d to %d,%d,%d: %s", here.X, here.Z, here.Level, dest.X, dest.Z, dest.Level, res.Reason)
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
			if tile.Chebyshev(cur, dest) <= radius {
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
		Loot: d.Loot,
	}
}

// ---- live reads ----

func packItemID(d *Deps, name string) (int32, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, false
	}
	for _, it := range inv.NonEmpty() {
		// A NOTE IS NOT THE ITEM. An obj and its note share a name, so matching on the name
		// alone hands the caller a note id, and a knife used on a note opens nothing at all.
		//
		// Live 2026-09-22: acct150 and acct155 sat at the Draynor camp logging "timed out
		// waiting for the make menu to offer Arrow shaft" 144 times each, at 0 xp for the whole
		// run. Both packs held noted "Logs" and noted "Oak logs" -- an undelivered mule hand-off
		// load drawn from a bank -- so packItemID returned a note, UseOnHeld did nothing, and
		// the three-second wait timed out for ever. The failure is silent in the sense that
		// matters: "no-menu" reads like an interface problem, not like "you are holding the
		// wrong kind of logs".
		//
		// Skipping notes also makes HasLogs honest, which is the half that ends the loop rather
		// than merely renaming it: an account holding only noted logs now correctly reports no
		// logs, so its caller banks or withdraws instead of trying to cut a note.
		//
		// woodcutrunner.itemInStock already had this check; this is the same rule, in the one
		// place that lacked it.
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			return it.ID, true
		}
	}
	return 0, false
}

// HasKnife reports whether a Knife is held.
func HasKnife(d *Deps) bool { _, ok := packItemID(d, bankfletchplan.Knife); return ok }

// HasLogs reports whether the configured log material is held.
func HasLogs(d *Deps, material string) bool { _, ok := packItemID(d, material); return ok }

// PackCountByName totals the pack's items named name.
func PackCountByName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		// Notes are skipped, the same rule packItemID above carries and for the same reason:
		// this answers "how many can I work with", and a note cannot be cut, cast on or spent.
		// A count that sees notes while the id lookup beside it refuses them is what let
		// acct150 sit at the Draynor camp logging "no-logs" forever while holding 243 noted
		// Logs — see woodcutrunner.packCountByName for that whole transcript.
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && !obj.Noted() && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return bankfletchplan.PackSlots
	}
	free := bankfletchplan.PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// ---- fletching ----

// FletchResult is one fletching attempt's outcome.
type FletchResult struct {
	Reason string // "made", "no-knife", "no-logs", "use-failed", "no-menu", "stopped"
}

// Fletch uses the held Knife on the held log material (OPHELDU), waits for the make menu, and
// batches the largest count the logs held can fund — ported from BankFletcher.ts's own Fletch
// task (knife-on-log branch only; see bankfletchplan's own doc comment for what's not ported).
func Fletch(d *Deps, material string) FletchResult {
	knifeID, ok := packItemID(d, bankfletchplan.Knife)
	if !ok {
		return FletchResult{Reason: "no-knife"}
	}
	logsID, ok := packItemID(d, material)
	if !ok {
		return FletchResult{Reason: "no-logs"}
	}
	// A bank closed a moment ago would close the make menu this opens, if the engine read both in one tick
	// (gamestate/modalclose.go): wait the two ticks the close needs.
	if !actions.SettleAfterClose(d.Session, actionsWaiter{d}) {
		return FletchResult{Reason: "stopped"}
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.UseOnHeld(knifeID, logsID); err != nil {
		// Deliberately not "no-logs": the logs ARE in the pack and the use itself was refused,
		// so the stage the caller re-derives next loop is identical and the attempt repeats
		// immediately. A live run span exactly this branch 26.9 million times in one session
		// (actions.ErrNotOnScreen, because the engine had not assigned the account's sidebar
		// tabs yet, so Equipment could not resolve the backpack component). A distinct reason
		// lets the caller back off instead.
		d.logf("UseOnHeld(knife on %s): %v", material, err)
		return FletchResult{Reason: "use-failed"}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)
	// Wait for a menu that offers arrow shafts, not merely for "a make menu is open".
	//
	// The weaker check is what made a smelting bot spin for four minutes against the wrong
	// interface, and this caller had the identical shape. Here the trap is worse than picking
	// the wrong product: a make menu left open from an earlier attempt satisfies IsMakeMenu
	// instantly, so the wait returns true before the knife-on-logs menu has even arrived, MakeX
	// then fails to find a shaft, and the bot retries into the same stale menu forever.
	offersShafts := func() bool { return dlg.Offers(d.Config, bankfletchplan.MakeMatch) }
	if !waitFor(d, "the make menu to offer "+bankfletchplan.ArrowShaft, 3*time.Second, offersShafts) {
		if dlg.IsMakeMenu() {
			d.logf("a make menu was open but offered %s, not %s — closing it", dlg.DescribeProducts(d.Config), bankfletchplan.ArrowShaft)
		}
		// Leave nothing open for the next attempt to mistake for a usable menu.
		_ = actions.CloseModal(d.Session)
		return FletchResult{Reason: "no-menu"}
	}
	count := PackCountByName(d, material)
	before := PackCountByName(d, bankfletchplan.ArrowShaft)
	if err := dlg.MakeX(d.Config, bankfletchplan.MakeMatch, int32(count), actionsWaiter{d}); err != nil {
		d.logf("MakeX(%s x%d): %v — menu offered %s", bankfletchplan.MakeMatch, count, err, dlg.DescribeProducts(d.Config))
		// A half-driven make interface left open is exactly what the next attempt would mistake
		// for a usable menu.
		_ = actions.CloseModal(d.Session)
		return FletchResult{Reason: "no-menu"}
	}
	// What this batch actually makes, for the Loot panel's Fletching card (see fletchcount.go).
	// Opened AFTER MakeX so the baseline is the pack as the batch starts; reporting only.
	window := beginFletch(d, logsID, material)
	if !waitFor(d, "arrow shafts to land after fletching", 30*time.Second, func() bool {
		return PackCountByName(d, bankfletchplan.ArrowShaft) > before || PackCountByName(d, material) == 0
	}) {
		window.Flush()
		return FletchResult{Reason: "stopped"}
	}
	// THE BATCH IS NOT OVER WHEN THE FIRST PRODUCT LANDS. One MakeX cuts the whole pack of logs
	// one product per tick or so, and the wait above is satisfied by the first of them. Returning
	// there is what put arrow shafts on the Woodcutting card: ProgressiveWoodcutter's start camp
	// went back to chopping, beginChop snapshotted a pack still missing most of the batch, and
	// every later shaft landed inside a window whose source was the tree (see the header of
	// internal/loot/production.go). Settling here is also the only way the count this reports is
	// the batch rather than one log's worth of it.
	settle(d, material)
	// Events are MATERIALS CONSUMED, not products made: the Loot panel prices a fletching card's
	// cost as events x the material's price (internal/loot/cost.go), so counting the shafts here
	// would charge fifteen logs for every one actually cut. The shafts still arrive as the
	// window's measured pack gain.
	if cut := count - PackCountByName(d, material); cut > 0 {
		window.Got(cut)
	}
	window.Flush()
	return FletchResult{Reason: "made"}
}

// settleQuiet is how long the pack must stop changing before a batch counts as finished, and
// settleMax bounds the whole wait. Two ticks: the engine makes at most one product per 600ms
// tick, so one quiet tick could be ordinary jitter and two is a stopped batch.
const (
	settleQuiet = 1200 * time.Millisecond
	settleMax   = 60 * time.Second
)

// settle waits for the make batch to finish, which is read off the PACK and never off a timer:
// the batch is over when neither the material nor the product has moved for settleQuiet, or when
// the material is gone, or when settleMax passes.
//
// It cannot hang a script that has been asked to stop — d.Delay returning false ends it — and a
// caller that reaches it has already sent MakeX, so the time spent here is time the account could
// not have used for anything else anyway: any other action would cancel the batch.
func settle(d *Deps, material string) {
	deadline := time.Now().Add(settleMax)
	lastMaterial, lastProduct := PackCountByName(d, material), PackCountByName(d, bankfletchplan.ArrowShaft)
	quietSince := time.Now()
	for time.Now().Before(deadline) {
		if lastMaterial == 0 {
			return
		}
		if !d.Delay(200 * time.Millisecond) {
			return
		}
		m, p := PackCountByName(d, material), PackCountByName(d, bankfletchplan.ArrowShaft)
		if m != lastMaterial || p != lastProduct {
			lastMaterial, lastProduct = m, p
			quietSince = time.Now()
			continue
		}
		if time.Since(quietSince) >= settleQuiet {
			return
		}
	}
}

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

// ---- the bank ----

func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: bankfletchplan.BoothName, Op: bankfletchplan.BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("could not find a bank booth near the bank")
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

// OpenBank walks to bankfletchplan.BankStand and opens its booth.
func OpenBank(d *Deps) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, bankfletchplan.BankStand, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAllExcept deposits every pack item not named in keep.
func DepositAllExcept(d *Deps, bank *actions.Bank, keep []string) int {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var ids []int32
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || kept[strings.ToLower(obj.Name)] {
			continue
		}
		seen[it.ID] = true
		ids = append(ids, it.ID)
	}
	// One burst, five Deposit-Alls a tick, confirmed once (twopertick-1012).
	deposited, _, r, retried := bank.DepositKinds(ids, actions.DelayWaiter(d.Delay))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("bankfletch deposit", len(ids), r, retried))
	}
	return len(deposited)
}

// WithdrawOneOf withdraws exactly one of name into the pack.
func WithdrawOneOf(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := PackCountByName(d, name)
		if err := bank.WithdrawOne(it.ID); err != nil {
			d.logf("withdraw one %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return PackCountByName(d, name) > before
		})
	}
	return false
}

// WithdrawLoad withdraws a full load (Withdraw-All) of name into the pack.
func WithdrawLoad(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := PackCountByName(d, name)
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, fmt.Sprintf("%s in pack after withdrawing", name), 3*time.Second, func() bool {
			return PackCountByName(d, name) > before
		})
	}
	return false
}
