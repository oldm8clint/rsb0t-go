// Package boneburierrunner is the I/O half of the BoneBurier port: it drives the pure
// stage/disposal decisions in internal/scripts/boneplan against a live session, following the
// exact split internal/scripts/essencerunner and its siblings already proved — a pure plan
// package decides, this package does the walking/clicking/banking, and
// internal/bot/boneburier.go wires the two together. See docs/script_porting_framework.md for
// the recipe this package follows.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// matching every sibling runner package, so internal/bot's Context.Delay can actually cancel a
// running burier mid-wait.
//
// Deliberately NOT ported here:
//   - the HUD/bank feed/tally/update-checker — no headless equivalent, same boundary drawn
//     around every other ported script.
//   - banking at a specific named BANK_LOCATIONS stand — see boneplan's own doc comment; every
//     run banks at whichever booth is nearest to wherever it currently stands.
package boneburierrunner

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
	"rsb0t-go/internal/scripts/boneplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// PrayerStat is UPDATE_STAT's index for Prayer — see cmd/burytest's identical constant and
// doc comment for the citation (engine/src/engine/entity/PlayerStat.ts).
const PrayerStat = 5

// PlayerPack is the persistent backpack inventory-tab component id — confirmed live and
// reused verbatim per every sibling runner's identical constant (see essencerunner.PlayerPack's
// own doc comment for the citation trail).
const PlayerPack = 3214

// PackSlots is the backpack's own slot count.
const PackSlots = 28

// BoothName/BoothOp open a bank booth — the same literal pair essenceplan.BoothName/BoothOp
// already proved live.
const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

// Delayer sleeps for d, returning false early if the caller should stop instead of true once d
// has fully elapsed — the same shape every sibling runner package uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one collect/bury/bank cycle needs from the outside world.
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

// emergencyFleeDriftTiles is 0: BoneBurier works whatever patch of ground it was started on,
// with no single fixed "home" the way a camp-based script has — see essencerunner's identical
// choice and doc comment for the precedent.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all. Returns false if a stop was
// requested while waiting. Mirrors essencerunner.WaitForGuardian exactly.
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

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops, stopping
// once within radius tiles of dest. Duplicated per this project's established per-package
// convention (see docs/script_porting_framework.md's "why duplicated" section) rather than
// shared.
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

func PrayerLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(PrayerStat)
	if !ok {
		return 0
	}
	return skill.Level
}

func PrayerXP(d *Deps) int {
	skill, ok := d.Session.State().Skill(PrayerStat)
	if !ok {
		return 0
	}
	return skill.XP
}

// PackFull reports whether the player's own pack holds PackSlots non-empty slots.
func PackFull(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	return inv != nil && len(inv.NonEmpty()) >= PackSlots
}

func packItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// CountBound totals every pack slot whose item resolves to disposal via boneplan.DisposalFor.
func CountBound(d *Deps, disposal boneplan.Disposal, names, bankNames []string, mode boneplan.DisposalMode) int {
	n := 0
	for _, it := range packItems(d) {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		if got, matched := boneplan.DisposalFor(obj.Name, mode, names, bankNames); matched && got == disposal {
			n++
		}
	}
	return n
}

// HeldBoneToBury returns the first pack slot bound for the ground that offers a burial op, and
// the op to use — ported from boneToBury(): a name match alone is not enough to click.
func HeldBoneToBury(d *Deps, names, bankNames []string, mode boneplan.DisposalMode) (gamestate.SlotItem, string, bool) {
	for _, it := range packItems(d) {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		disp, matched := boneplan.DisposalFor(obj.Name, mode, names, bankNames)
		if !matched || disp != boneplan.ToBury {
			continue
		}
		if op, ok := boneplan.BurialOp(obj.IOps[:]); ok {
			return it, op, true
		}
	}
	return gamestate.SlotItem{}, "", false
}

// buryOptionIndex maps the op label to actions.Bury's/Open's OPHELD1 family option number —
// both Bury and Scatter are option 1, per bonePlan.ts's own BURIAL_OPS (both bury_bone.rs2 and
// its ashes/scatter twin sit on the same op-menu entry).
func buryOptionIndex(string) int { return 1 }

// BuryOne buries or scatters one pack slot via OPHELD1, waiting for it to leave the pack.
func BuryOne(d *Deps, it gamestate.SlotItem, op string) bool {
	_ = buryOptionIndex(op)
	before := len(packItems(d))
	if err := actions.Bury(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
		d.logf("%s: %v", op, err)
		return false
	}
	return waitFor(d, fmt.Sprintf("%s to leave the pack", op), 3*time.Second, func() bool {
		if len(packItems(d)) < before {
			return true
		}
		for _, cur := range packItems(d) {
			if cur.Slot == it.Slot {
				return cur.ID != it.ID
			}
		}
		return true
	})
}

// ---- ground pickup ----

// dropKey addresses one ground stack the way SkipList/dropKey does in the TS source.
func dropKey(g gamestate.GroundItem, name string) string {
	return fmt.Sprintf("%s@%d,%d,%d", name, g.X, g.Z, g.Level)
}

// DropKey exposes dropKey for the bot adapter's own skip-list bookkeeping.
func DropKey(g gamestate.GroundItem, name string) string { return dropKey(g, name) }

// FindBone returns the nearest ground item matching names within radius of anchor whose key is
// not in skip, ported from findBone().
func FindBone(d *Deps, anchor tile.Tile, radius int, names []string, skip map[string]bool) (gamestate.GroundItem, bool) {
	best := gamestate.GroundItem{}
	bestDist := -1
	found := false
	for _, g := range d.Session.State().GroundItems() {
		obj, ok := d.Config.Obj(g.ID)
		if !ok || !boneplan.IsBoneName(obj.Name, names) {
			continue
		}
		gt := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if !boneplan.InRange(gt, &anchor, radius) {
			continue
		}
		if skip[dropKey(g, obj.Name)] {
			continue
		}
		dist := tile.Chebyshev(anchor, gt)
		if !found || dist < bestDist {
			best, bestDist, found = g, dist, true
		}
	}
	return best, found
}

// CollectResult is one Collect attempt's outcome.
type CollectResult struct {
	Picked bool
	// Gone is true when the drop was no longer there once we arrived — not a skip-worthy miss.
	Gone bool
	Name string
}

// Collect walks to drop if it is not underfoot, takes it, and reports whether it landed —
// ported from collect(). The caller (the bot adapter) owns the skip-list retry bookkeeping,
// matching how BoneBurier.ts's own skips field lives on the class, not in a helper.
func Collect(d *Deps, drop gamestate.GroundItem) CollectResult {
	obj, _ := d.Config.Obj(drop.ID)
	name := obj.Name
	dt := tile.Tile{X: drop.X, Z: drop.Z, Level: drop.Level}
	walkable := d.Pack == nil || d.Pack.Walkable(dt.X, dt.Z, dt.Level)
	if radius, walk := pickupApproach(currentTile(d), dt, walkable); walk {
		if !WalkNear(d, dt, radius) {
			return CollectResult{Name: name}
		}
	}
	// The wait below is sized from where the account stands NOW. It used to be sized from where
	// it stood before the walk above, so a drop four tiles off was given 4.3s to be picked up from
	// a tile beside it.
	here := currentTile(d)
	if !onGround(d, drop) {
		return CollectResult{Gone: true, Name: name}
	}
	beforeUsed := len(packItems(d))
	beforeCount := 0
	for _, it := range packItems(d) {
		if it.ID == int32(drop.ID) {
			beforeCount++
		}
	}
	if err := actions.TakeGroundItem(d.Session, d.Config, drop); err != nil {
		d.logf("take %s: %v", name, err)
		return CollectResult{Name: name}
	}
	dist := tile.Chebyshev(here, dt)
	waitMs := time.Duration(boneplan.PickupWaitMs(dist)) * time.Millisecond
	var goneAt time.Time
	vanished := false
	picked := waitFor(d, fmt.Sprintf("%s picked up", name), waitMs, func() bool {
		if len(packItems(d)) > beforeUsed {
			return true
		}
		n := 0
		for _, it := range packItems(d) {
			if it.ID == int32(drop.ID) {
				n++
			}
		}
		if n > beforeCount {
			return true
		}
		// Somebody else took it (at a cow pen full of the fleet's own accounts, the usual
		// answer once a drop turns public). Our own take removes it from the ground in the same
		// tick it lands in the pack, so give the pack one tick to catch up before believing it.
		if onGround(d, drop) {
			goneAt = time.Time{}
			return false
		}
		if goneAt.IsZero() {
			goneAt = time.Now()
			return false
		}
		if time.Since(goneAt) >= pickupGoneGrace {
			vanished = true
			return true
		}
		return false
	})
	if vanished {
		return CollectResult{Gone: true, Name: name}
	}
	return CollectResult{Picked: picked, Name: name}
}

// pickupGoneGrace is how long a drop must be off the ground, with nothing new in the pack, before
// Collect stops waiting for it: a tick and a poll.
const pickupGoneGrace = 700 * time.Millisecond

func onGround(d *Deps, drop gamestate.GroundItem) bool {
	for _, g := range d.Session.State().GroundItems() {
		if g.ID == drop.ID && g.X == drop.X && g.Z == drop.Z && g.Level == drop.Level {
			return true
		}
	}
	return false
}

// pickupApproach is where the account must stand before a Take on a drop at dt is worth sending:
// walk reports whether a walk is needed at all, and radius how close it must get.
//
// ON THE TILE, NOT "WITHIN ONE". The engine takes a ground item only from its own tile or from an
// orthogonally adjacent one with no wall between (Player.inOperableDistance: reachedEntity ||
// reachedObj), and with clientRoutefinder on it will not walk the account there itself — Take is
// OPOBJ with no route (actions.TakeGroundItem "does not walk"). This used to skip the walk for
// anything within Chebyshev 1 and stop a walk at Chebyshev 1, which includes the four diagonals
// and the far side of a fence: the click is refused ("I can't reach that!") and the wait runs out.
// MEASURED live 2026-09-25: 421 "timed out waiting for Bones picked up" from ScarabSwarm accounts
// at the training cows between 20:16 and 22:00. 142 were the second of a back-to-back pair under
// 3s apart (a pass whose two drops were both "close enough" and both refused), and only 109 came
// straight after a walk: most were clicked from wherever the account already stood, one tile
// away. CLAUDE.md's diagonal gotcha, again.
//
// A drop on a tile nobody can stand on (a table, a counter) keeps the old approach: beside it.
func pickupApproach(here, dt tile.Tile, walkable bool) (radius int, walk bool) {
	if here == dt {
		return 0, false
	}
	if walkable {
		return 0, true
	}
	if here.Level == dt.Level && abs(here.X-dt.X)+abs(here.Z-dt.Z) == 1 {
		return 1, false
	}
	return 1, true
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// ---- the bank ----

// OpenNearestBank finds every "Bank booth" candidate within radius of the current position and
// tries them nearest-first, matching every sibling runner's openNearestBankBooth (see
// docs/script_porting_framework.md's bug class #1 on why a 4-candidate retry, not just the
// single nearest, matters against a real bake artifact).
func OpenNearestBank(d *Deps, radius int) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	here := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: radius, Name: BoothName, Op: BoothOp}, live)
	if len(candidates) == 0 {
		d.logf("no bank booth within %d tiles", radius)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			d.logf("gave up after %d bank booth candidates", maxTries)
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
		if open, _ := bank.AwaitOpen(mark, actions.DelayWaiter(d.Delay), 20*time.Second); open {
			return bank, true
		}
	}
	return nil, false
}

// waiter adapts a Deps' Delayer to actions.Waiter.
type waiter struct{ d *Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
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
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

func Waiter(d *Deps) actions.Waiter { return waiter{d} }

func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// DepositBoneNames deposits every already-open bank's pack item bound for the bank (per mode/
// names/bankNames), returning the count deposited (distinct ids) — ported from bankRun()'s
// Bank.depositAllMatching call.
func DepositBoneNames(d *Deps, bank *actions.Bank, names, bankNames []string, mode boneplan.DisposalMode) int {
	before := len(packItems(d))
	match := func(it gamestate.SlotItem) bool {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			return false
		}
		disp, matched := boneplan.DisposalFor(obj.Name, mode, names, bankNames)
		return matched && disp == boneplan.ToBank
	}
	if err := bank.BatchDepositAllMatching(match, Waiter(d)); err != nil {
		d.logf("deposit: %v", err)
	}
	after := len(packItems(d))
	if after >= before {
		return 0
	}
	return before - after
}

// BankRowToWithdraw returns the first bank row named one of names (a bone the run picks up out
// of the bank instead of the ground), ignoring rows already noted — ported from
// bankBoneRow(). withdrawOp of "Withdraw-All" is used by the caller.
func BankRowToWithdraw(d *Deps, bank *actions.Bank, names []string) (gamestate.SlotItem, string, bool) {
	for _, it := range bank.Items() {
		if it.Count <= 0 {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !boneplan.IsBoneName(obj.Name, names) {
			continue
		}
		return it, obj.Name, true
	}
	return gamestate.SlotItem{}, "", false
}

// WithdrawAllOf withdraws every one of the given bank row (as items, not notes — bones cannot
// be buried noted) and waits for the pack to reflect it.
func WithdrawAllOf(d *Deps, bank *actions.Bank, row gamestate.SlotItem) bool {
	before := 0
	for _, it := range packItems(d) {
		if it.ID == row.ID {
			before++
		}
	}
	beforeUsed := len(packItems(d))
	if err := bank.WithdrawAll(row.ID); err != nil {
		d.logf("withdraw all: %v", err)
		return false
	}
	return waitFor(d, "bones in pack after withdrawing", 3*time.Second, func() bool {
		if len(packItems(d)) > beforeUsed {
			return true
		}
		n := 0
		for _, it := range packItems(d) {
			if it.ID == row.ID {
				n++
			}
		}
		return n > before
	})
}
