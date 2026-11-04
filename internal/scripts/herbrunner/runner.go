// Package herbrunner is the I/O half of the HerbCleaner port: banking unidentified herbs and
// identifying them, driven by the pure decisions in internal/scripts/herbplan. It follows the
// exact split internal/scripts/gemrunner/flaxrunner already proved — a pure plan package
// decides, this package does the walking/clicking/banking, and internal/bot/herbcleaner.go wires
// the two together. See docs/script_porting_framework.md for the recipe.
//
// Identifying uses actions.OpHeld (OPHELD1: the herb's own "Identify" inventory option), the
// same primitive actions.Bury/Open already proved live for bones and caskets. Content's
// ~identify_herb replaces exactly ONE herb in place at the clicked slot, so a load is cleaned by
// a burst of single clicks rather than a make menu.
//
// WHY THE SLOT IS PASSED EXPLICITLY rather than using actions.Equipment.InteractHeld: that
// helper resolves "the first backpack slot holding objID" itself, from an inventory snapshot
// that has not necessarily caught up between two rapid sends in one burst. Every unidentified
// herb of one kind shares an object id, so a stale snapshot would aim several clicks of a burst
// at the same slot. This package reads the pack once per burst and sends each click at its own
// concrete slot, exactly as boneburierrunner.BuryOne does.
//
// Every item here is matched by OBJECT ID, never by display name, for a reason stronger than the
// GemCutter port's: every unidentified herb shares ONE display name ("Herb", or "Unidentified
// herb" for the Karamja five), so a name match could not tell Guam from Torstol at all, and
// would also happily pick up a noted stack that cannot be identified. See herbplan's own package
// doc comment for both id traps.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep, so
// internal/bot's Context.Delay can cancel a running cleaner mid-wait.
//
// Deliberately NOT ported here: the paint/status overlay (HerbCleaner.ts's own onPaint and its
// xpPerHour) — no headless equivalent, the same boundary every other ported script draws.
package herbrunner

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
	"rsb0t-go/internal/scripts/herbplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id — see flaxrunner.PlayerPack's own doc
// comment for why this fixed component is read instead of scanning every known inventory.
const PlayerPack = 3214

// HerbloreStat is UPDATE_STAT's index for Herblore, index 15 in the engine's own PlayerStat enum
// — read straight off gamestate.SkillNames, whose own doc comment records that this table was
// cross-checked against two indices independently proven live (Hitpoints at 3, Agility at 16).
const HerbloreStat = 15

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one clean/bank cycle needs from the outside world.
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
	// Walker, when set, is tried whenever the same-level pathfinder cannot plan a route at all
	// — it walks doors, stairs and (with hops installed) ships.
	//
	// Why this exists: pathfind.Pack.FindPath is a SAME-LEVEL planner and answers "unreachable"
	// for anything off-scene, so a script that assumes it is standing at one bank still cannot
	// get home after a death or a fail-teleport without this. The GemCutter port proved that
	// gap live (19 consecutive "unreachable" answers after a mid-run teleport to Lumbridge);
	// this port carries the same escalation from the start rather than rediscovering it. Nil
	// keeps plain same-level pathfinding.
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

// emergencyFleeDriftTiles is 0 and emergencyFleeHome is unset for the same reason
// gemrunner/essencerunner/flaxrunner leave theirs: there is no single "home" worth dragging an
// account back to mid-event.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — duplicated per this project's
// established per-package convention, see essencerunner.WaitForGuardian's own doc comment for
// why these small helpers are copied rather than shared. Uses randomevents.Busy (cooldown-aware)
// rather than bare Detect(), which would block forever on an event Guardian has given up on.
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
// framework's own bug-class list records several accounts giving up at a real, distance-
// appropriate waypoint under a 30s cap tuned against a local engine.
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

// walkWeb runs one d.Walker walk, reporting only whether it arrived. Kept deliberately simpler
// than barbarianrunner.walkWeb's arrived/died/stopped triple: this script has no course position
// to lose, so a death on the way is just another reason it did not arrive and the next Loop call
// re-derives the stage from live state.
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

// CountByID totals the pack's items with object id — the only matcher this package uses, see the
// package doc comment for why names cannot work here at all.
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

// UsedSlots is how many pack slots hold anything.
func UsedSlots(d *Deps) int { return len(packSlots(d)) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	free := herbplan.PackSlots - UsedSlots(d)
	if free < 0 {
		return 0
	}
	return free
}

// EligibleUnidsHeld counts the unidentified herbs in the pack this run is allowed to clean —
// herbplan.Snapshot.EligibleUnidsHeld's own live source.
func EligibleUnidsHeld(d *Deps, herbs []herbplan.Herb) int {
	n := 0
	for _, h := range herbs {
		n += CountByID(d, h.UnidID)
	}
	return n
}

// FirstIdentifiable is the first herb of herbs present in the pack, in the order given (herbplan
// orders them lowest level first, so cheap XP is cleaned before expensive).
func FirstIdentifiable(d *Deps, herbs []herbplan.Herb) (herbplan.Herb, bool) {
	for _, h := range herbs {
		if CountByID(d, h.UnidID) > 0 {
			return h, true
		}
	}
	return herbplan.Herb{}, false
}

// HerbloreLevel is the account's live Herblore level, or 0 if the server has not reported it.
func HerbloreLevel(d *Deps) int {
	if sk, ok := d.Session.State().Skill(HerbloreStat); ok {
		return sk.Level
	}
	return 0
}

// HerbloreXP is the account's live Herblore XP as the server reports it: a whole-XP total that
// the engine has already FLOORED (UpdateStatEncoder writes `(exp / 10) | 0` over an internal
// tenths value). Several herbs award a fractional amount, so a single identify can move this by
// 2 or by 3 for the same herb — never treat a delta here as proof that an identify landed. See
// herbplan's package doc comment.
func HerbloreXP(d *Deps) int {
	if sk, ok := d.Session.State().Skill(HerbloreStat); ok {
		return sk.XP
	}
	return 0
}

// identifyOption resolves the 1-based held-option index for the herb's Identify entry off the
// object's own config, rather than hardcoding 1. Every one of the 19 unidentified ids was
// observed carrying iop1="Identify" in the running engine's config archive, so 1 is the expected
// answer and is used as the fallback when no config is available.
func identifyOption(d *Deps, unidID int32) int {
	if d.Config == nil {
		return 1
	}
	obj, ok := d.Config.Obj(int(unidID))
	if !ok {
		return 1
	}
	if n := actions.OptionIndex(actions.HeldOps(obj.IOps), herbplan.IdentifyOp); n != 0 {
		return n
	}
	return 1
}

// ---- identifying ----

// CleanResult is one identifying burst's outcome.
type CleanResult struct {
	Herb herbplan.Herb
	// Cleaned is the real pack delta: how many of this herb's unidentified form left the pack.
	Cleaned int
	// XPGained is the live Herblore XP delta, reported only. It is a floored whole-XP figure
	// over a possibly fractional award, so it is not evidence and nothing branches on it.
	XPGained int
	// Reason is "cleaned", "no-herbs", "refused", "members", "stalled" or "stopped".
	Reason string
}

const (
	// cleanBurstTimeout bounds the wait for the FIRST result of a burst. Generous on purpose: a
	// production 600ms tick makes a five-click burst take several seconds, and this must not
	// mistake a slow tick for a stall.
	cleanBurstTimeout = 8 * time.Second
	// cleanSettleQuiet is how long the counts must stop moving before a burst is called done, so
	// the remaining clicks of the burst are counted rather than cut off mid-drain.
	cleanSettleQuiet = 1200 * time.Millisecond
	// cleanSettleMax bounds the whole settle phase.
	cleanSettleMax = 10 * time.Second
)

// Clean sends one burst of Identify clicks at the first cleanable herb in the pack and reports
// what actually changed — ported from HerbCleaner.ts's own Clean task, but with a bounded burst
// and a real settle instead of its unbounded "click every eligible unid in one pass" (see
// herbplan.BurstSize for why).
//
// A refusal is reported rather than retried: the caller denies that herb, which is what stops a
// run spinning forever on a herb its Herblore level cannot touch.
func Clean(d *Deps, herbs []herbplan.Herb) CleanResult {
	herb, ok := FirstIdentifiable(d, herbs)
	if !ok {
		return CleanResult{Reason: "no-herbs"}
	}

	unidBefore := CountByID(d, herb.UnidID)
	xpBefore := HerbloreXP(d)
	option := identifyOption(d, herb.UnidID)

	// Read the pack ONCE and aim each click at its own concrete slot. See the package doc
	// comment: resolving the slot per click from a snapshot that has not caught up would point
	// several clicks of one burst at the same slot.
	clickAt := time.Now()
	sent := 0
	for _, it := range packSlots(d) {
		if sent >= herbplan.BurstSize {
			break
		}
		if it.ID != herb.UnidID {
			continue
		}
		if err := actions.OpHeld(d.Session, option, it.ID, it.Slot, PlayerPack); err != nil {
			d.logf("Identify %s (slot %d): %v", herb.Name, it.Slot, err)
			break
		}
		sent++
	}
	if sent == 0 {
		return CleanResult{Herb: herb, Reason: "no-herbs"}
	}

	progressed := waitFor(d, fmt.Sprintf("a %s to be identified", herb.Name), cleanBurstTimeout, func() bool {
		return CountByID(d, herb.UnidID) < unidBefore
	})

	// Let the rest of the burst drain before measuring, so a five-click burst is not reported as
	// one clean just because the first result arrived early.
	if progressed {
		lastSeen := CountByID(d, herb.UnidID)
		quietSince := time.Now()
		settleDeadline := time.Now().Add(cleanSettleMax)
		for time.Now().Before(settleDeadline) {
			if CountByID(d, herb.UnidID) == 0 {
				break
			}
			if !d.Delay(200 * time.Millisecond) {
				return CleanResult{Herb: herb, Reason: "stopped"}
			}
			if now := CountByID(d, herb.UnidID); now != lastSeen {
				lastSeen = now
				quietSince = time.Now()
				continue
			}
			if time.Since(quietSince) >= cleanSettleQuiet {
				break
			}
		}
	}

	// A refusal is authoritative even when something was cleaned: it means this herb is out of
	// reach here, and the caller should stop choosing it. The two refusals are kept apart because
	// only one of them is ever fixed by training Herblore, and content checks members FIRST.
	// Chat.Since returns the window's lines already joined into one string, so both classifiers
	// are substring tests over the whole window rather than a per-line scan.
	said := d.Chat.Since(clickAt)
	if herbplan.IsMembersRefusal(said) {
		return CleanResult{Herb: herb, Reason: "members"}
	}
	if herbplan.IsLevelRefusal(said) {
		return CleanResult{Herb: herb, Reason: "refused"}
	}
	if !progressed {
		return CleanResult{Herb: herb, Reason: "stalled"}
	}

	cleaned := unidBefore - CountByID(d, herb.UnidID)
	if cleaned < 0 {
		cleaned = 0
	}
	return CleanResult{
		Herb:     herb,
		Cleaned:  cleaned,
		XPGained: HerbloreXP(d) - xpBefore,
		Reason:   "cleaned",
	}
}

// ---- the bank ----

// openNearestBankBooth tries every matching booth near (x,z,level) nearest-first — duplicated per
// this project's convention, and the nearest-first retry specifically recovers from the
// live-found bad-bake case essencerunner.openNearestBankBooth's own doc comment records (one
// account timed out beside a booth the static bake placed slightly wrong while another opened a
// different instance two tiles along).
func openNearestBankBooth(d *Deps, boothName string, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: boothName, Op: herbplan.BoothOp}, live)
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

// OpenBank walks to herbplan.BankStand and opens its booth, leaving it open for the caller to
// drive its own deposit/withdraw sequence.
func OpenBank(d *Deps, boothName string) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, herbplan.BankStand, 3) {
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
// deposit to clear. It returns how many distinct item ids were deposited, and which ids REFUSED
// to leave the pack.
//
// HerbCleaner needs no tool at all, so callers ordinarily pass a nil keep set — which matches
// HerbCleaner.ts's own depositAllMatching(() => true) and its settings help ("the bot deposits
// everything from your pack into the bank each cycle, including non-herbs").
//
// Why stuck is reported rather than swallowed: a live run of this port picked up eight "Strange
// box" random-event items (object 3062, content debugname `macro_cube`), which the bank simply
// will not accept. Every
// bank trip then re-tried the same id, burned the full 5s timeout below, and withdrew only 20
// herbs instead of 28 because those eight slots never freed. That is this project's own bug
// class #4 (an untradeable item stalling every future cycle on rediscovery,
// docs/script_porting_framework.md) in a new place, and the established fix is the same one
// donorrunner.Options.KnownUntradable uses: the CALLER remembers the id — so it survives across
// many calls — and passes it back in keep, which is why this function only reports and does not
// keep its own state.
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
		d.logf("%s", actions.VisitLog("herb deposit", len(ids), r, retried))
	}
	return len(done), stuck
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

// WithdrawUnid takes up to n unidentified herbs of herb out of an open bank and reports how many
// actually landed — 0 means the bank has none, which is how a caller confirms a herb empty.
//
// Withdrawing by id is load-bearing here beyond the usual note problem: the bank shows every
// unidentified herb under the same name, so a name-based withdraw could not name one kind at all.
func WithdrawUnid(d *Deps, bank *actions.Bank, herb herbplan.Herb, n int) int {
	if n <= 0 || BankCount(bank, herb.UnidID) == 0 {
		return 0
	}
	before := CountByID(d, herb.UnidID)
	if err := bank.WithdrawX(herb.UnidID, int32(n), waiterFor(d)); err != nil {
		d.logf("withdraw %d grimy %s: %v", n, herb.Name, err)
	}
	waitFor(d, fmt.Sprintf("grimy %s to land in the pack", herb.Name), 3*time.Second, func() bool {
		return CountByID(d, herb.UnidID) > before
	})
	got := CountByID(d, herb.UnidID) - before
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
