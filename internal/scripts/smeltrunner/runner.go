// Package smeltrunner is the I/O half of the SmelterBot port: banking ore at Al Kharid and
// smelting it into bars at the furnace, driven by the pure decisions in
// internal/scripts/smeltplan. It follows the split internal/scripts/gemrunner and
// internal/scripts/bankfletchrunner already proved — a pure plan package decides, this package
// does the walking/clicking/banking, and internal/bot/smelterbot.go wires the two together.
// See docs/script_porting_framework.md for the recipe.
//
// Smelting uses actions.OpLoc with the furnace's own "Smelt" option (content's
// `[oploc2,_smithing_furnace]`), then drives the make menu it opens through
// actions.ChatDialog.MakeX — the same already-proven pair bankfletchrunner uses for
// knife-on-logs. It deliberately never uses OpLocU, the use-ore-on-furnace path content also
// supports: that is the primitive with the known silent no-op on this engine/content pairing
// (CLAUDE.md's Gotchas), and option 2 reaches the identical interface without it.
//
// Every ore and bar is matched by OBJECT ID, never by display name, because the noted variant of
// each sits at id+1 and carries the identical name — a name match would count a noted stack the
// furnace cannot smelt and the make menu cannot consume.
//
// Progress is measured in ITEMS, never XP. Content's smelting XP is in tenths of a displayed
// point (a bronze bar is `productexp,62`, the 6.2 XP a player sees) and the engine floors the
// fraction, so an XP delta cannot be divided back into a bar count. See smeltplan's own doc
// comment.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep, so
// internal/bot's Context.Delay can cancel a running smelter mid-wait.
//
// Deliberately NOT ported here: the paint/status overlay — no headless equivalent.
package smeltrunner

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
	"rsb0t-go/internal/scripts/smeltplan"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// PlayerPack is the persistent inventory-tab component id — see flaxrunner.PlayerPack's own
// doc comment for why this fixed component is read instead of scanning every known inventory.
const PlayerPack = 3214

// SmithingStat is UPDATE_STAT's index for Smithing, index 13 in the engine's own PlayerStat
// enum (gamestate.SkillNames' own table, where Crafting is the 12 gemrunner already cites).
const SmithingStat = 13

// Delayer sleeps for d, returning false early if the caller should stop.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one bank/smelt cycle needs from the outside world.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	// Components is the parsed interface archive the bank and the make menu need; nil makes
	// OpenBank fail clearly rather than opening a bank it cannot read.
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Walker, when set, is tried whenever the same-level pathfinder cannot plan a route at
	// all — it walks doors, stairs and (with hops installed) ships.
	//
	// Why this exists: pathfind.Pack.FindPath is a SAME-LEVEL planner and answers
	// "unreachable" for anything off the current scene, so without a Walker this script cannot
	// get back to Al Kharid after a death or a fail-teleport — the exact gap gemrunner's first
	// live run found (19 consecutive unreachable answers from Lumbridge). Nil keeps the plain
	// direct-pathfinding behaviour.
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

// actionsWaiter adapts Deps.Delay to actions.Waiter, so a cancelled run stops waiting on an
// interface instead of sleeping out the timeout.
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

// ---- the guardian ----

// emergencyFleeDriftTiles is 0 and the flee reference is deliberately left unset, the same
// reasoning gemrunner/flaxrunner give: this script works between two fixed tiles, but neither is
// worth dragging an account back to in the middle of a random event.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — duplicated per this
// project's established per-package convention, see gemrunner.WaitForGuardian's own doc comment.
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

// ---- walking ----

const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops, until
// within radius tiles. Duplicated per this project's established per-package convention.
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

// PackCounts is a smeltplan.Counts over the pack.
func PackCounts(d *Deps) smeltplan.Counts {
	return func(id int32) int { return CountByID(d, id) }
}

// BankCounts is a smeltplan.Counts over an open bank.
func BankCounts(bank *actions.Bank) smeltplan.Counts {
	return func(id int32) int {
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
}

// UsedSlots is how many pack slots hold anything.
func UsedSlots(d *Deps) int { return len(packSlots(d)) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	free := smeltplan.PackSlots - UsedSlots(d)
	if free < 0 {
		return 0
	}
	return free
}

// CanSmelt reports whether the pack holds enough of every ingredient for at least one bar.
func CanSmelt(d *Deps, r smeltplan.Recipe) bool { return smeltplan.CanSmelt(PackCounts(d), r) }

// SmithingLevel is the account's live Smithing level, or 0 if the server has not reported it.
func SmithingLevel(d *Deps) int {
	if sk, ok := d.Session.State().Skill(SmithingStat); ok {
		return sk.Level
	}
	return 0
}

// SmithingXP is the account's live Smithing XP in DISPLAYED points (gamestate already divides
// the engine's internal tenths). Reported for the log only — never used to infer a bar count,
// see the package doc comment.
func SmithingXP(d *Deps) int {
	if sk, ok := d.Session.State().Skill(SmithingStat); ok {
		return sk.XP
	}
	return 0
}

// ---- smelting ----

// SmeltResult is one smelting batch's outcome.
type SmeltResult struct {
	// Bars is the real pack delta of the bar object, and OreUsed the delta of the primary ore.
	// For iron these legitimately disagree: content gives iron a 50% "too impure" coin flip
	// that consumes the ore and produces nothing (see smeltplan.Recipe.FailsRandomly).
	Bars    int
	OreUsed int
	// Reason is "smelted", "no-ore", "no-furnace", "no-menu", "stalled" or "stopped".
	Reason string
}

const (
	// smeltMenuTimeout bounds the wait for the make menu after clicking the furnace.
	smeltMenuTimeout = 6 * time.Second
	// smeltBatchTimeout bounds one whole Smelt-X drain. Generous on purpose: a full pack of
	// bronze is 14 bars at four ticks each, and this must not mistake a slow tick for a stall.
	smeltBatchTimeout = 120 * time.Second
)

// FindFurnace locates the nearest furnace carrying content's own "Smelt" option. The Match's
// own Op is the resolved option NUMBER for that name, which is what actions.OpLoc wants — this
// build has several locs named "Furnace" and only some carry Smelt (2781 does, 2785 beside it
// does not), so the query filters on the option rather than the name alone.
func FindFurnace(d *Deps) (locindex.Match, bool) {
	here := currentTile(d)
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	return d.Locs.Nearest(d.Config, locindex.Query{
		X: here.X, Z: here.Z, Level: here.Level,
		Radius: smeltplan.FurnaceLeash,
		Name:   smeltplan.FurnaceName,
		Op:     smeltplan.FurnaceOp,
	}, live)
}

// Smelt walks to the furnace, opens its smelting menu and drains the pack through one Smelt-X,
// reporting what actually changed — ported from SmelterBot.ts's own SmeltTrip task.
//
// The wait deliberately polls the ore count rather than parking on one long delay: a single
// 120s wait would stop the caller from noticing a level-up dialogue or a random event, which is
// the TS source's own "#711" bug.
func Smelt(d *Deps, r smeltplan.Recipe) SmeltResult {
	if !CanSmelt(d, r) {
		return SmeltResult{Reason: "no-ore"}
	}
	if !WalkNear(d, smeltplan.FurnaceStand, 2) {
		return SmeltResult{Reason: "no-furnace"}
	}
	dlg := actions.NewChatDialog(d.Session, d.Components)

	// offersBar is the real question, and "is a make menu open" is not it.
	//
	// A live run caught this: the bot span for four minutes calling MakeX against a menu whose
	// only product was "Bronze mace" — the anvil's SMITHING interface, not the furnace's
	// smelting one. Because IsMakeMenu() was true, the furnace was never re-clicked, so every
	// retry hit the identical wrong menu and the run made four bars and then nothing. Gating on
	// the menu actually offering the bar we came to make turns that into one recovery instead
	// of an infinite loop. This is the same "prefer the check that is false until the thing has
	// happened" rule docs/trustworthy_harnesses.md states for harnesses; it applies to runners.
	offersBar := func() bool { return dlg.Offers(d.Config, r.Bar) }

	if !offersBar() {
		if dlg.IsMakeMenu() {
			d.logf("a make menu was open but offered %s, not %s — closing it and clicking the furnace", dlg.DescribeProducts(d.Config), r.Bar)
			_ = actions.CloseModal(d.Session)
			if !d.Delay(600 * time.Millisecond) {
				return SmeltResult{Reason: "stopped"}
			}
		}
		furnace, ok := FindFurnace(d)
		if !ok {
			d.logf("no %q with a %q option within %d tiles of %d,%d",
				smeltplan.FurnaceName, smeltplan.FurnaceOp, smeltplan.FurnaceLeash, currentTile(d).X, currentTile(d).Z)
			return SmeltResult{Reason: "no-furnace"}
		}
		if err := actions.OpLoc(d.Session, furnace.Op, furnace.X, furnace.Z, furnace.ID); err != nil {
			d.logf("OpLoc(furnace %d, op %d): %v", furnace.ID, furnace.Op, err)
			return SmeltResult{Reason: "no-furnace"}
		}
		if !waitFor(d, "the smelting menu to offer "+r.Bar, smeltMenuTimeout, offersBar) {
			// Leave nothing open for the next attempt to mistake for a usable menu.
			_ = actions.CloseModal(d.Session)
			return SmeltResult{Reason: "no-menu"}
		}
	}

	barsBefore := CountByID(d, r.BarID)
	oreBefore := CountByID(d, r.Primary.ID)
	want := smeltplan.BarsFundedBy(PackCounts(d), r)
	if want <= 0 {
		return SmeltResult{Reason: "no-ore"}
	}
	if err := dlg.MakeX(d.Config, r.Bar, int32(want), actionsWaiter{d}); err != nil {
		d.logf("MakeX(%s x%d): %v — menu offered %s", r.Bar, want, err, dlg.DescribeProducts(d.Config))
		// Close it: a half-driven make interface left open is exactly what the next attempt
		// would mistake for a usable menu.
		_ = actions.CloseModal(d.Session)
		return SmeltResult{Reason: "no-menu"}
	}

	// Drain: the batch is done when the ore runs out, a dialogue interrupts, or the count stops
	// falling for smeltplan.SmeltIdle.
	last := CountByID(d, r.Primary.ID)
	lastChange := time.Now()
	deadline := time.Now().Add(smeltBatchTimeout)
	stalled := false
	for time.Now().Before(deadline) {
		now := CountByID(d, r.Primary.ID)
		if now == 0 || !CanSmelt(d, r) {
			break
		}
		if now < last {
			last, lastChange = now, time.Now()
		} else if smeltplan.SmeltStalled(now, last, int(time.Since(lastChange)/time.Millisecond)) {
			stalled = true
			break
		}
		if !d.Delay(400 * time.Millisecond) {
			return SmeltResult{Reason: "stopped"}
		}
	}

	res := SmeltResult{
		Bars:    CountByID(d, r.BarID) - barsBefore,
		OreUsed: oreBefore - CountByID(d, r.Primary.ID),
		Reason:  "smelted",
	}
	if res.Bars < 0 {
		res.Bars = 0
	}
	if res.OreUsed < 0 {
		res.OreUsed = 0
	}
	if stalled && res.Bars == 0 {
		res.Reason = "stalled"
	}
	return res
}

// ---- the bank ----

// openNearestBankBooth tries every matching booth near (x,z,level) nearest-first — duplicated
// per this project's convention, and the nearest-first retry specifically recovers from the
// live-found bad-bake case gemrunner.openNearestBankBooth's own doc comment records.
func openNearestBankBooth(d *Deps, boothName string, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: boothName, Op: smeltplan.BoothOp}, live)
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

// OpenBank walks to smeltplan.BankStand and opens its booth, leaving it open for the caller to
// drive its own deposit/withdraw sequence.
func OpenBank(d *Deps, boothName string) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, smeltplan.BankStand, 3) {
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
// deposit to clear. Returns how many distinct item ids were deposited, and every id the bank
// refused to take.
//
// The refused list is not decoration: HerbCleaner's port found a random-event drop ("Strange
// box") the bank simply will not accept, which silently cost every later trip both the slot and
// the full deposit timeout. A caller that remembers these across trips stops re-trying them.
func DepositAllExceptIDs(d *Deps, bank *actions.Bank, keep map[int32]bool) (deposited int, refused []int32) {
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
	done, refused, r, retried := bank.DepositKinds(ids, actions.DelayWaiter(d.Delay))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("smelt deposit", len(ids), r, retried))
	}
	return len(done), refused
}

// WithdrawCount withdraws exactly n of ore into the pack and reports how many actually landed.
func WithdrawCount(d *Deps, bank *actions.Bank, ore smeltplan.Ore, n int) int {
	if n <= 0 {
		return 0
	}
	before := CountByID(d, ore.ID)
	if err := bank.WithdrawX(ore.ID, int32(n), actionsWaiter{d}); err != nil {
		d.logf("withdraw %d %s: %v", n, ore.Name, err)
		return 0
	}
	waitFor(d, fmt.Sprintf("%d %s to land in the pack", n, ore.Name), 5*time.Second, func() bool {
		return CountByID(d, ore.ID) >= before+n
	})
	got := CountByID(d, ore.ID) - before
	if got < 0 {
		return 0
	}
	return got
}

// Restock is one whole bank trip: deposit everything, then withdraw the trip's ore plan.
// ok is false only when the bank could not be reached or opened; an empty plan is reported as
// exhausted, which is the run's own stop condition rather than a failure.
type RestockResult struct {
	Deposited int
	Refused   []int32
	Withdrew  int
	Exhausted bool
}

// Restock walks to the bank, banks the bars and refills the pack with ore for the next batch.
func Restock(d *Deps, boothName string, r smeltplan.Recipe, keep map[int32]bool) (RestockResult, bool) {
	bank, ok := OpenBank(d, boothName)
	if !ok {
		return RestockResult{}, false
	}
	defer func() { _ = bank.Close() }()

	out := RestockResult{}
	out.Deposited, out.Refused = DepositAllExceptIDs(d, bank, keep)

	// Read the bank AFTER depositing: the bars just banked do not change the ore counts, but a
	// deposit is also what frees the slots the withdrawal needs.
	plan := smeltplan.WithdrawFor(BankCounts(bank), r)
	if len(plan) == 0 {
		out.Exhausted = true
		return out, true
	}
	for _, step := range plan {
		got := WithdrawCount(d, bank, step.Ore, step.Count)
		out.Withdrew += got
		if got < step.Count {
			d.logf("wanted %d %s but got %d", step.Count, step.Ore.Name, got)
		}
	}
	// A partial withdrawal that cannot fund a single bar is the same dead end as an empty bank.
	if !CanSmelt(d, r) {
		out.Exhausted = true
	}
	return out, true
}
