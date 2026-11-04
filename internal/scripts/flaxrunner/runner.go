// Package flaxrunner is the I/O half of the FlaxPicker port: it drives the pure stage
// decisions in internal/scripts/flaxplan against a live session, following the exact split
// internal/scripts/essencerunner/fisherrunner/woodcutrunner already proved — a pure plan
// package decides, this package does the walking/clicking/banking, and
// internal/bot/flaxpicker.go wires the two together. See docs/script_porting_framework.md for
// the recipe this package follows.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly, so
// internal/bot's Context.Delay can actually cancel a running picker mid-wait.
//
// Deliberately NOT ported here: the paint/status overlay (no headless equivalent) and
// EscapeFlaxTrap's carving recovery — see flaxplan's own doc comment for why.
package flaxrunner

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/flaxplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// PlayerPack is the persistent inventory-tab component id — see essencerunner.PlayerPack's own
// doc comment for why this fixed, always-current component is read instead of scanning every
// known inventory.
const PlayerPack = 3214

// Delayer sleeps for d, returning false early if the caller should stop instead of true once d
// has fully elapsed — the same shape every sibling runner package uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one pick/bank cycle needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
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

// emergencyFleeDriftTiles is 0 — the picker splits its time between the field and the bank, not
// one single fixed "home", matching essencerunner's own identical choice and doc comment.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — duplicated per this
// project's established per-package convention, see essencerunner.WaitForGuardian's own doc
// comment.
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

// walkNearTimeout bounds WalkNear's entire operation — see essencerunner.walkNearTimeout's own
// doc comment for the live evidence behind 120s rather than a shorter cap.
const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops, stopping
// once within radius tiles of dest. Duplicated per this project's established per-package
// convention rather than shared — see essencerunner.WalkNear's own doc comment.
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
	}
}

// ---- live reads ----

// PackCountByName totals the pack's items named name (case-insensitive).
func PackCountByName(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// PackFull reports whether the player's own pack holds flaxplan.PackSlots non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= flaxplan.PackSlots
}

// Haul is every non-empty pack slot, matching flaxplan.Snapshot.Haul's own meaning (anything
// held at all, not just Flax — the startup bank trip sweeps whatever a resumed account already
// carries, exactly like StartupReset in the TS source).
func Haul(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	return len(inv.NonEmpty())
}

// FlaxHeld counts unnoted Flax currently in the pack.
func FlaxHeld(d *Deps, name string) int {
	return PackCountByName(d, name)
}

// ---- picking ----

// FlaxNearby reports whether a Flax bush is within flaxplan.FieldScope of the current tile —
// flaxplan.Snapshot.FlaxNearby's own live source.
func FlaxNearby(d *Deps, name, op string) bool {
	here := currentTile(d)
	_, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: flaxplan.FieldScope, Name: name, Op: op}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	return ok
}

// PickResult is one Pick attempt's outcome.
type PickResult struct {
	// Gained is Flax caught during this call.
	Gained int
	// Reason is why the picking session ended: "no-flax", "full", "stalled", "dialog", or
	// "stopped".
	Reason string
}

// pickStallTimeout is how long picking can go without a gain before giving up on this bush and
// re-searching — there is no TS-documented stall constant for FlaxPicker (its own loop just
// retries the click up to 30 times per Pick task), so this mirrors essenceplan.MineStallTimeout,
// the closest proven sibling for "click a gatherable loc and wait for the count to rise."
const pickStallTimeout = 20 * time.Second

const pickTickInterval = 600 * time.Millisecond

// Pick finds the nearest Flax bush, walks to it, and holds the click until the pack fills, no
// Flax lands for pickStallTimeout, or a chat it cannot answer interrupts — ported from
// FlaxPicker.ts's own Pick task. A level-up that opens meanwhile is answered with its Continue
// here and the bush clicked once more (holdPick), not handed back for the Loop to answer.
func Pick(d *Deps, name, op string) PickResult {
	here := currentTile(d)
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: flaxplan.FieldScope, Name: name, Op: op}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	if !ok {
		d.logf("no %s in sight — waiting", name)
		return PickResult{Reason: "no-flax"}
	}
	bushTile := tile.Tile{X: m.X, Z: m.Z, Level: m.Level}
	if tile.Chebyshev(here, bushTile) > flaxplan.PickRadius {
		if !WalkNear(d, bushTile, flaxplan.PickRadius) {
			return PickResult{Reason: "stopped"}
		}
	}
	return holdPick(d, name, op, m)
}

// holdPick clicks the bush m and holds the click. The click goes through the shared gate (actions.HoldChat: one click on the same
// bush per actions.ClickGapTicks game ticks however often this is called), and a chat is the hold's to answer, not an exit:
// this loop used to return "dialog" the moment a Continue-able chat showed, which is what let essencerunner.Mine spin on a
// level-up (docs/essence_mine_exit_2026-10-15.md). The Loop's own top-of-pass Continue would have caught it here, but nothing in
// this package should depend on its caller to be the only thing standing between a chat and a tight loop of clicks.
func holdPick(d *Deps, name, op string, m locindex.Match) PickResult {
	gained := 0
	hold := actions.NewHoldChat(d.Session, actions.DelayWaiter(d.Delay), d.Components, actions.LocKey(m.X, m.Z, int(m.ID)), func() error {
		return actions.OpLoc(d.Session, m.Op, m.X, m.Z, m.ID)
	})
	hold.Busy = func() bool { return randomevents.Busy(d.Session) }
	hold.Held = func(ticks int) {
		d.logf("flax: holding the next click on the bush for %d tick(s): the server reads five clicks a tick", ticks)
	}
	hold.OnAnswer = func(chat int) {
		d.logf("flax: answered chat %d (a level-up or a message box) with its Continue after %d flax", chat, gained)
	}
	click := func() string {
		if err := hold.Click(); err != nil {
			if errors.Is(err, actions.ErrClickStopped) {
				return "stopped"
			}
			d.logf("OpLoc(%s) on %s: %v", op, name, err)
			return "no-flax"
		}
		return ""
	}
	if why := click(); why != "" {
		return PickResult{Reason: why}
	}
	count := FlaxHeld(d, name)
	lastGain := time.Now()
	for {
		now := FlaxHeld(d, name)
		if now > count {
			gained += now - count
			count = now
			lastGain = time.Now()
		}
		if PackFull(d) {
			return PickResult{Gained: gained, Reason: "full"}
		}
		switch hold.Step() {
		case actions.HoldClick:
			if why := click(); why != "" {
				return PickResult{Gained: gained, Reason: why}
			}
		case actions.HoldHandBack:
			return PickResult{Gained: gained, Reason: "dialog"}
		}
		if time.Since(lastGain) > pickStallTimeout {
			return PickResult{Gained: gained, Reason: "stalled"}
		}
		if !d.Delay(pickTickInterval) {
			return PickResult{Gained: gained, Reason: "stopped"}
		}
	}
}

// ---- the bank ----

// openNearestBankBooth finds every candidate matching boothName near (x,z,level) and tries them
// nearest-first — duplicated per this project's established per-package convention, see
// essencerunner.openNearestBankBooth's own doc comment for the live bake-artifact bug this
// nearest-first retry recovers from.
func openNearestBankBooth(d *Deps, boothName string, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: boothName, Op: flaxplan.BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("could not find a %s near the bank", boothName)
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

// OpenBank walks to flaxplan.BankStand and opens its booth, leaving it open for the caller to
// drive its own deposit sequence.
func OpenBank(d *Deps, boothName string) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, flaxplan.BankStand, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, boothName, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAll deposits the entire pack (FlaxPicker.ts banks everything, no keep list) into an
// already-open bank, waiting for the deposit to clear. Returns how many distinct item ids were
// deposited.
func DepositAll(d *Deps, bank *actions.Bank) int {
	var ids []int32
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		if _, ok := d.Config.Obj(int(it.ID)); !ok {
			continue
		}
		seen[it.ID] = true
		ids = append(ids, it.ID)
	}
	// One burst, five Deposit-Alls a tick, confirmed once (twopertick-1012).
	deposited, _, r, retried := bank.DepositKinds(ids, actions.DelayWaiter(d.Delay))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("flax deposit", len(ids), r, retried))
	}
	return len(deposited)
}
