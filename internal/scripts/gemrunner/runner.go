// Package gemrunner is the I/O half of the GemCutter port: banking uncut gems and chiselling
// them, driven by the pure decisions in internal/scripts/gemplan. It follows the exact split
// internal/scripts/flaxrunner/bankfletchrunner already proved — a pure plan package decides,
// this package does the walking/clicking/banking, and internal/bot/gemcutter.go wires the two
// together. See docs/script_porting_framework.md for the recipe.
//
// Cutting uses actions.Equipment.UseOnHeld (OPHELDU: chisel on an uncut gem), the same
// already-proven primitive bankfletchrunner's knife-on-logs uses. Unlike that one, there is NO
// make menu afterwards: content's [opheldu,chisel] handler cuts exactly one gem per use, so a
// load is cut by sending a small burst of uses and watching the counts move.
//
// Every item here is matched by OBJECT ID, never by display name, because each gem's noted
// variant sits at id+1 and carries the identical name — a name match would pick up a noted
// stack the chisel cannot cut. This is the one place this port deliberately departs from its
// sibling runners, which match by name.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep, so
// internal/bot's Context.Delay can cancel a running cutter mid-wait.
//
// Deliberately NOT ported here: the paint/status overlay — no headless equivalent.
package gemrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id — see flaxrunner.PlayerPack's own
// doc comment for why this fixed component is read instead of scanning every known inventory.
const PlayerPack = 3214

// CraftingStat is UPDATE_STAT's index for Crafting, index 12 in the engine's own PlayerStat
// enum — the same constant crafterrunner.CraftingStat already established and cites.
const CraftingStat = 12

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one cut/bank cycle needs from the outside world.
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
	// Why this exists: the first live run of this port proved the gap. The account was
	// teleported from the Seers bank to Lumbridge mid-run, and every FindPath answered
	// "unreachable" (19 times in a row) because the collision pack's A* is a same-level
	// planner, not a cross-map one. Without a Walker the script simply cannot get home after
	// a death or a fail-teleport, which is the exact recovery every sibling script leans on
	// webwalk for. Nil keeps the old direct-pathfinding-only behaviour.
	Walker *webwalk.Walker
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

// emergencyFleeDriftTiles is 0 — this script stands at one bank the whole run, but the flee
// reference below is deliberately left unset for the same reason essencerunner/flaxrunner
// leave theirs: there is no single "home" worth dragging an account back to mid-event.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — duplicated per this
// project's established per-package convention, see essencerunner.WaitForGuardian's own doc
// comment for why these small helpers are copied rather than shared.
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

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops, until
// within radius tiles. Duplicated per this project's established per-package convention — see
// essencerunner.WalkNear's own doc comment.
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
			// Escalate to webwalk rather than giving up: an unreachable direct path is
			// exactly the cross-map case webwalk exists for. See Deps.Walker's own doc
			// comment for the live run that made this necessary.
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
		Walker: d.Walker,
	}
}

// walkWeb runs one d.Walker walk, reporting only whether it arrived. Kept deliberately
// simpler than barbarianrunner.walkWeb's own arrived/died/stopped triple: this script has no
// course position to lose, so a death on the way is just another reason it did not arrive and
// the next Loop call re-derives the stage from live state.
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
		d.logf("webwalked back to %d,%d,%d", dest.X, dest.Z, dest.Level)
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

// CountByID totals the pack's items with object id — the only matcher this package uses, see
// the package doc comment for why names are not enough here.
func CountByID(d *Deps, id int32) int {
	n := 0
	for _, it := range packSlots(d) {
		if it.ID != id {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// HasChisel reports whether an unnoted chisel is held.
func HasChisel(d *Deps) bool { return CountByID(d, gemplan.ChiselID) > 0 }

// UsedSlots is how many pack slots hold anything.
func UsedSlots(d *Deps) int { return len(packSlots(d)) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	free := gemplan.PackSlots - UsedSlots(d)
	if free < 0 {
		return 0
	}
	return free
}

// EligibleUncutHeld counts the uncut gems in the pack this run is allowed to cut —
// gemplan.Snapshot.EligibleUncutHeld's own live source.
func EligibleUncutHeld(d *Deps, gems []gemplan.Gem) int {
	n := 0
	for _, g := range gems {
		n += CountByID(d, g.UncutID)
	}
	return n
}

// FirstCuttable is the first gem of gems present in the pack, in the order given (gemplan
// orders them lowest level first, so cheap XP is cut before expensive).
func FirstCuttable(d *Deps, gems []gemplan.Gem) (gemplan.Gem, bool) {
	for _, g := range gems {
		if CountByID(d, g.UncutID) > 0 {
			return g, true
		}
	}
	return gemplan.Gem{}, false
}

// CraftingLevel is the account's live Crafting level, or 0 if the server has not reported it.
func CraftingLevel(d *Deps) int {
	if sk, ok := d.Session.State().Skill(CraftingStat); ok {
		return sk.Level
	}
	return 0
}

// CraftingXP is the account's live Crafting XP in DISPLAYED points (gamestate already divides
// the engine's internal tenths) — one tenth of gemplan's own content XP column.
func CraftingXP(d *Deps) int {
	if sk, ok := d.Session.State().Skill(CraftingStat); ok {
		return sk.XP
	}
	return 0
}

// ---- cutting ----

// CutResult is one cutting burst's outcome.
type CutResult struct {
	Gem gemplan.Gem
	// Cuts and Crushed are real pack deltas: a crushable gem sometimes smashes instead of
	// cutting, which content treats as a normal outcome (quarter XP, a Crushed gemstone).
	Cuts    int
	Crushed int
	// XPGained is the live Crafting XP delta in displayed points.
	XPGained int
	// RefusedLevel is the Crafting level a refusal line demanded, set only for "refused".
	RefusedLevel int
	// Reason is "cut", "no-chisel", "no-gems", "refused", "members", "stalled" or "stopped".
	Reason string
}

const (
	// cutBurstTimeout bounds the wait for the FIRST result of a burst. Generous on purpose:
	// a production 600ms tick makes a five-use burst take several seconds, and this must not
	// mistake a slow tick for a stall.
	cutBurstTimeout = 8 * time.Second
	// cutSettleQuiet is how long the counts must stop moving before a burst is called done,
	// so the remaining uses of the burst are counted rather than cut off mid-drain.
	cutSettleQuiet = 1200 * time.Millisecond
	// cutSettleMax bounds the whole settle phase.
	cutSettleMax = 10 * time.Second
)

// Cut sends one burst of chisel uses at the first cuttable gem in the pack and reports what
// actually changed — ported from GemCutter.ts's own Cut task, but with a bounded burst and a
// real settle instead of its 60-deep queue (see gemplan.BurstSize for why).
//
// A level refusal is reported rather than retried: the caller denies that gem, which is what
// stops a run from spinning forever on a gem its Crafting level cannot touch.
func Cut(d *Deps, gems []gemplan.Gem) CutResult {
	if !HasChisel(d) {
		return CutResult{Reason: "no-chisel"}
	}
	gem, ok := FirstCuttable(d, gems)
	if !ok {
		return CutResult{Reason: "no-gems"}
	}

	uncutBefore := CountByID(d, gem.UncutID)
	cutBefore := CountByID(d, gem.CutID)
	crushedBefore := CountByID(d, gemplan.CrushedGemstoneID)
	xpBefore := CraftingXP(d)

	eq := actions.NewEquipment(d.Session, d.Components)
	clickAt := time.Now()
	// FIVE DIFFERENT GEMS IN ONE TICK (twopertick-1012). The burst used to call UseOnHeld five times, which names "the
	// first other slot holding the gem" each time from a pack the client has not yet seen change: all five named the
	// SAME slot and the engine took the first and dropped the other four. UseOnHeldMany names five different slots
	// (a cut has no delay, so the engine reads five a tick).
	sent, err := eq.UseOnHeldMany(gemplan.ChiselID, gem.UncutID, gemplan.BurstSize)
	if err != nil {
		d.logf("UseOnHeldMany(chisel on %s): %v", gem.UncutName, err)
	} else {
		d.logf("gem cutter: chisel on %d %s in one burst", sent, gem.UncutName)
	}
	if sent == 0 {
		return CutResult{Gem: gem, Reason: "no-gems"}
	}

	progressed := waitFor(d, fmt.Sprintf("a %s to be cut", gem.Name), cutBurstTimeout, func() bool {
		return CountByID(d, gem.UncutID) < uncutBefore || CraftingXP(d) > xpBefore
	})

	// Let the rest of the burst drain before measuring, so a five-use burst is not reported as
	// one cut just because the first result arrived early.
	if progressed {
		lastSeen := CountByID(d, gem.UncutID)
		quietSince := time.Now()
		settleDeadline := time.Now().Add(cutSettleMax)
		for time.Now().Before(settleDeadline) {
			if CountByID(d, gem.UncutID) == 0 {
				break
			}
			if !d.Delay(200 * time.Millisecond) {
				return CutResult{Gem: gem, Reason: "stopped"}
			}
			if now := CountByID(d, gem.UncutID); now != lastSeen {
				lastSeen = now
				quietSince = time.Now()
				continue
			}
			if time.Since(quietSince) >= cutSettleQuiet {
				break
			}
		}
	}

	// A refusal is authoritative even when something was cut: it means this gem is out of
	// reach here, and the caller should stop choosing it. The two refusals are kept apart
	// because only one of them is fixed by training Crafting.
	said := d.Chat.Since(clickAt)
	if level, refused := gemplan.LevelRefusal(said); refused {
		return CutResult{Gem: gem, RefusedLevel: level, Reason: "refused"}
	}
	if gemplan.IsMembersRefusal(said) {
		return CutResult{Gem: gem, Reason: "members"}
	}
	if !progressed {
		return CutResult{Gem: gem, Reason: "stalled"}
	}

	cuts := CountByID(d, gem.CutID) - cutBefore
	crushed := CountByID(d, gemplan.CrushedGemstoneID) - crushedBefore
	if cuts < 0 {
		cuts = 0
	}
	if crushed < 0 {
		crushed = 0
	}
	return CutResult{
		Gem:      gem,
		Cuts:     cuts,
		Crushed:  crushed,
		XPGained: CraftingXP(d) - xpBefore,
		Reason:   "cut",
	}
}

// ---- the bank ----

// openNearestBankBooth tries every matching booth near (x,z,level) nearest-first — duplicated
// per this project's convention, and the nearest-first retry specifically recovers from the
// live-found bad-bake case essencerunner.openNearestBankBooth's own doc comment records.
func openNearestBankBooth(d *Deps, boothName string, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: boothName, Op: gemplan.BoothOp}, live)
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

// OpenBank walks to gemplan.BankStand and opens its booth, leaving it open for the caller to
// drive its own deposit/withdraw sequence.
func OpenBank(d *Deps, boothName string) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, gemplan.BankStand, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, boothName, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAllExceptIDs deposits every pack item whose object id is not in keep, waiting for each
// deposit to clear. Returns how many distinct item ids were deposited. Keeping by ID rather
// than by name is deliberate: the chisel's noted variant shares its name.
func DepositAllExceptIDs(d *Deps, bank *actions.Bank, keep map[int32]bool) int {
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
	deposited, _, r, retried := bank.DepositKinds(ids, actions.DelayWaiter(d.Delay))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("gem deposit", len(ids), r, retried))
	}
	return len(deposited)
}

// BankCount is how many of object id the open bank holds.
func BankCount(bank *actions.Bank, id int32) int {
	n := 0
	for _, it := range bank.Items() {
		if it.ID != id {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// WithdrawChisel takes exactly one chisel out of an open bank.
func WithdrawChisel(d *Deps, bank *actions.Bank) bool {
	if BankCount(bank, gemplan.ChiselID) == 0 {
		return false
	}
	before := CountByID(d, gemplan.ChiselID)
	if err := bank.WithdrawOne(gemplan.ChiselID); err != nil {
		d.logf("withdraw chisel: %v", err)
		return false
	}
	return waitFor(d, "the chisel to land in the pack", 3*time.Second, func() bool {
		return CountByID(d, gemplan.ChiselID) > before
	})
}

// WithdrawUncut takes up to n uncut gems of gem out of an open bank and reports how many
// actually landed — 0 means the bank has none, which is how a caller confirms a gem empty.
func WithdrawUncut(d *Deps, bank *actions.Bank, gem gemplan.Gem, n int) int {
	if n <= 0 || BankCount(bank, gem.UncutID) == 0 {
		return 0
	}
	before := CountByID(d, gem.UncutID)
	if err := bank.WithdrawX(gem.UncutID, int32(n), waiterFor(d)); err != nil {
		d.logf("withdraw %d %s: %v", n, gem.UncutName, err)
	}
	waitFor(d, fmt.Sprintf("%s to land in the pack", gem.UncutName), 3*time.Second, func() bool {
		return CountByID(d, gem.UncutID) > before
	})
	got := CountByID(d, gem.UncutID) - before
	if got < 0 {
		return 0
	}
	return got
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
