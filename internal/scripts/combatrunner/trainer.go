package combatrunner

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/entityquery"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/webwalk"
)

// The training-ladder half of the runner: travelling to a rung, buying and wearing gear,
// fighting with a leash, and the bank trips in between.
//
// Milestone 1's half of this package (runner.go) is the primitives — prayer, autocast, kill
// detection, loot approach, re-gear, a logout that stays out. This file is milestone 2: the
// live I/O the rung schedule in combatplan needs.
//
// The rule from runner.go's own doc comment still governs every function here: internal/actions
// is fire-and-forget, so nothing below reports success on a click. Each one sends and then
// verifies against observed state — a pack delta, a worn panel, a tile, a stat.

// Stat indices, in gamestate.SkillNames order. Cross-checked against that table rather than
// assumed: index 3 is Hitpoints and index 16 is Agility, the two this repo has already proven
// live elsewhere, and the melee three land where the table says.
const (
	AttackStat    = 0
	DefenceStat   = 1
	StrengthStat  = 2
	HitpointsStat = 3
	RangedStat    = 4
	PrayerStatIdx = 5
	MagicStat     = 6
)

// SkillLevel reads one base level, 0 when the stat has not arrived yet.
func SkillLevel(d *Deps, stat int) int {
	sk, ok := d.Session.State().Skill(stat)
	if !ok {
		return 0
	}
	return sk.Level
}

// TrainerLevels reads the five levels the gear table and the ladder compare against.
func TrainerLevels(d *Deps) combatplan.Levels {
	return combatplan.Levels{
		Attack:   SkillLevel(d, AttackStat),
		Strength: SkillLevel(d, StrengthStat),
		Defence:  SkillLevel(d, DefenceStat),
		Ranged:   SkillLevel(d, RangedStat),
		Magic:    SkillLevel(d, MagicStat),
	}
}

// CombatLevelOf reads the account's own combat level, which the rung's MinCombatLevel gate
// compares against. 0 when the server has not sent one yet, which reads as "too low" and so
// fails safe.
func CombatLevelOf(d *Deps) int {
	lvl, ok := d.Session.State().CombatLevel()
	if !ok {
		return 0
	}
	return lvl
}

// ---- waiting and travel ----

type trainerWaiter struct{ d *Deps }

func (w trainerWaiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w trainerWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	return waitFor(w.d, "a confirmation", timeout, cond)
}
func (w trainerWaiter) DelayTicks(n int) bool {
	return w.d.Delay(time.Duration(n) * 600 * time.Millisecond)
}

// Waiter adapts Deps to actions.Waiter, for the bank and shop helpers that take one.
func Waiter(d *Deps) actions.Waiter { return trainerWaiter{d} }

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is being handled, so the trainer does not fight
// through a Genie. Cooldown-aware via randomevents.Busy rather than bare Detect, which is bug
// class 6 in docs/script_porting_framework.md: Detect still reports an event Guardian has
// already given up on, so a caller polling it blocks forever.
func WaitForGuardian(d *Deps) bool {
	if !randomevents.Busy(d.Session) {
		return true
	}
	last := time.Now()
	for randomevents.Busy(d.Session) {
		if time.Since(last) >= guardianWaitLogEvery {
			d.logf("waiting for the random-event guardian to finish")
			last = time.Now()
		}
		if !d.Delay(500 * time.Millisecond) {
			return false
		}
	}
	return true
}

// TravelWalkTimeout is this project's floor for a "walk somewhere nearby" primitive. 30s was
// tuned against a near-zero-latency local engine and gave up on real, distance-appropriate
// waypoints in production (bug class 5 in docs/script_porting_framework.md).
const TravelWalkTimeout = 120 * time.Second

// TravelMaxTimeout is the ceiling TravelBudget clamps to.
//
// Ten minutes rather than "as long as it takes", because a walk that genuinely cannot be made
// still has to end: the whole point of a timeout here is that the caller gets to retire a rung
// or a shop after three attempts instead of standing in one forever.
const TravelMaxTimeout = 10 * time.Minute

// TravelBudget sizes a walk's timeout from the distance it actually has to cover.
//
// One constant was serving two jobs that are nothing like each other, and the second one was
// invisible until it failed. TravelWalkTimeout's own comment calls it the floor for "a walk
// somewhere nearby" and 120 s is right for that — the walk from the giants stand to the
// Edgeville bank is about 95 tiles with the trapdoor counted. But the SAME call in
// combattrainer's Loop is also the only path home after a random event throws the account across
// the map, and on 2026-09-20 a fail-teleport put one 677 tiles away at Ardougne Monastery. That
// walk crosses two ship fares and five doors, it took the best part of four minutes of the run's
// own recovery, and the fixed two-minute budget duly timed out on it: the run fell back to
// same-level pathfinding, which cannot make that journey at all, and only got home because the
// retry loop gave it another two attempts.
//
// "The only path home" is meant literally and it is checkable: internal/randomevents contains
// no walker at all — no webwalk import, no WalkTo call — so the Guardian can flee a few tiles
// and set a SafeRef, and nothing in it can route an account across the map. The journey is
// always made by whichever of the script's own travel calls comes due first.
//
// Raising the constant instead would be the wrong fix in the other direction: a rung hop that
// has silently stopped working would then burn ten minutes before anyone heard about it. So the
// budget is a function of the distance rather than a constant, a second per tile over the folded
// route, floored at the old value so nothing nearby gets less than it used to and capped so
// nothing gets forever. A 677-tile displacement asks for the cap; a bank trip asks for the floor.
func TravelBudget(from, to tile.Tile) time.Duration {
	budget := time.Duration(combatplan.RouteTiles(from, to)) * time.Second
	if budget < TravelWalkTimeout {
		budget = TravelWalkTimeout
	}
	if budget > TravelMaxTimeout {
		budget = TravelMaxTimeout
	}
	return budget
}

// TravelTo goes to dest, using webwalk when one is configured and falling back to same-level
// pathfinding when it is not.
//
// The fallback matters for the rungs: three of them are underground, and although this world
// puts its dungeons on level 0 at z+6400 (so nothing here is a level change), the route into
// them still runs through a trapdoor or a manhole that only the webwalk graph knows about.
func TravelTo(d *Deps, dest tile.Tile, radius int, timeout time.Duration) bool {
	if here, ok := currentTile(d); ok && here.Level == dest.Level && tile.Chebyshev(here, dest) <= radius {
		return true
	}
	if d.Walker != nil {
		res := d.Walker.WalkTo(webwalk.Point{X: dest.X, Z: dest.Z, Level: dest.Level}, webwalk.WalkOptions{
			Radius:  radius,
			Combat:  webwalk.CombatOptions{RunWhenAttacked: true, RunBelowHP: 40},
			Timeout: timeout,
		})
		switch res.Outcome {
		case webwalk.OutcomeArrived:
			return true
		case webwalk.OutcomeStopped:
			return false
		}
		d.logf("webwalk to %d,%d,%d ended %s (%s) — trying same-level pathfinding", dest.X, dest.Z, dest.Level, res.Outcome, res.Reason)
	}
	return WalkNear(d, dest, radius, timeout)
}

// ---- banking ----

// openNearestBankBooth tries up to four nearest-first booths rather than one.
//
// One candidate is not enough, and this is a live-found bug rather than caution: an account
// once timed out for minutes beside a "Bank booth" the static bake had placed slightly wrong,
// while a second account opened a different instance of the same type two tiles along without
// trouble (fisherrunner.openNearestBankBooth's own note).
func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	if d.Locs == nil {
		d.logf("no loc index configured — cannot find a bank booth")
		return nil, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if len(candidates) == 0 {
		candidates = d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth"}, live)
	}
	if len(candidates) == 0 {
		d.logf("no bank booth near %d,%d", x, z)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			d.logf("gave up after %d bank booth candidates near %d,%d", maxTries, x, z)
			return nil, false
		}
		if !walkToInteract(d, tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, 30*time.Second) {
			continue
		}
		op := booth.Op
		if op == 0 {
			op = 1
		}
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("OpLoc on the booth: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		if open, _ := bank.AwaitOpen(mark, actions.DelayWaiter(d.Delay), timeout); open {
			return bank, true
		}
	}
	return nil, false
}

// maxBoothApproaches caps how many of a loc's own interact tiles are tried before falling back,
// so one booth standing in water or behind a counter cannot spend the whole bank trip walking.
const maxBoothApproaches = 3

// boothApproachTimeout is per stand tile rather than for the whole approach. The candidates are
// neighbours of the same loc, so a walk that has not closed in ten seconds is not going to.
const boothApproachTimeout = 10 * time.Second

// walkToInteract walks to a tile the engine will accept an interaction with target FROM, rather
// than merely one that is near it.
//
// Why this is not WalkNear(target, 1), which is what it replaces: the engine's own
// inOperableDistance/reachRectangle1 require the two tiles to touch ORTHOGONALLY with no wall on
// the facing side, and Chebyshev 1 includes the four diagonals, which touch at a corner only. A
// bank booth is a blocked loc, so a click sent from a diagonal is accepted by the client, queued,
// and then never satisfies the server's check: there is no refusal, no chat line and no error —
// bank.IsOpen simply never becomes true and the caller's 20s wait burns before it tries the next
// booth. This is the same rule ApproachAndTake closes onto a touching tile for, that FightOne
// asks the pathfinder about, and that fisherplan/guild.go already recorded live at the Fishing
// Guild ("from 2586,3420 every booth is diagonal and the engine ignores the click ... two booths
// timed out, 30s each"). pathfind.CanInteractFrom is the shared answer; woodcutrunner reached it
// first, and this is that function for this package, per the per-package duplication convention.
//
// It fails open with no pack and falls back to the old walk when none of the interact tiles can
// be reached, so it is never worse than the behaviour it replaces.
func walkToInteract(d *Deps, target tile.Tile, timeout time.Duration) bool {
	if d.Pack == nil {
		return WalkNear(d, target, 1, timeout)
	}
	here, ok := currentTile(d)
	if !ok {
		return WalkNear(d, target, 1, timeout)
	}
	at := pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level}
	if d.Pack.CanInteractFrom(pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level}, at) {
		return true
	}
	stands := d.Pack.InteractTiles(at)
	if len(stands) == 0 {
		d.logf("nothing can interact with the loc at %d,%d,%d — it is walled in or the bake has it in the wrong place", target.X, target.Z, target.Level)
		return false
	}
	sort.Slice(stands, func(i, j int) bool {
		return tile.Chebyshev(here, tile.Tile{X: stands[i].X, Z: stands[i].Z, Level: stands[i].Level}) <
			tile.Chebyshev(here, tile.Tile{X: stands[j].X, Z: stands[j].Z, Level: stands[j].Level})
	})
	// The tiles no walk reaches are not tried: the keeper's side of a counter used to take two of the three tries and
	// answer "no path from 3007,3348,0 to 3011,3353,0: unreachable" in the log each time (37 lines at the Falador east
	// bank in 40 minutes, 14 Oct 2026), and when the reachable tile was a third in the list it was never tried.
	stands = reachableStands(d, stands)
	// Said plainly in the log, because the diagonal case is the one worth counting: it is the
	// one that used to send a click the server ignored without saying so.
	why := "was too far away to use it"
	if tile.Chebyshev(here, target) <= 1 && here.Level == target.Level {
		why = "touched it corner to corner only, which the engine refuses"
	}
	for i, stand := range stands {
		if i >= maxBoothApproaches {
			break
		}
		if WalkNear(d, tile.Tile{X: stand.X, Z: stand.Z, Level: stand.Level}, 0, boothApproachTimeout) {
			d.logf("walked from %d,%d onto %d,%d to use the loc at %d,%d: the tile we were on %s",
				here.X, here.Z, stand.X, stand.Z, target.X, target.Z, why)
			return true
		}
	}
	d.logf("could not reach any of the %d tile(s) that can interact with the loc at %d,%d — falling back to the old approach", len(stands), target.X, target.Z)
	return WalkNear(d, target, 1, timeout)
}

// reachableStands is the interact tiles a walk can reach from where the account stands (ReachFor: a tile behind a shut
// door counts when the run goes through doors), in the order given, or all of them when none is, so a pack that has the
// loc walled in does not leave the old approach nothing to try.
func reachableStands(d *Deps, stands []pathfind.NavPoint) []pathfind.NavPoint {
	var out []pathfind.NavPoint
	for _, s := range stands {
		if ReachFor(d, tile.Tile{X: s.X, Z: s.Z, Level: s.Level}).OK {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return stands
	}
	return out
}

// OpenBankNear travels to stand and opens the nearest bank booth.
func OpenBankNear(d *Deps, stand tile.Tile, radius int) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive configured — cannot open the bank")
		return nil, false
	}
	// Sized from the distance, for the same reason the walk to the rung is. This is not only the
	// short hop from a rung to its own bank: when a random event has thrown the account across
	// the map, whichever of the two walks comes due first is the one that has to make the
	// journey, and a fixed two-minute budget cannot. See TravelBudget.
	budget := TravelWalkTimeout
	if here, ok := currentTile(d); ok {
		budget = TravelBudget(here, stand)
	}
	if !TravelTo(d, stand, radius, budget) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	return openNearestBankBooth(d, x, z, level, 20*time.Second)
}

// DepositAllExcept banks every pack item keep says no to, and reports how many kinds went in.
//
// One deposit per OBJECT ID, not per slot. DepositAll moves the whole holding in a single
// action, so a naive walk over the inventory snapshot asks again for each of the other slots
// that item used to occupy and gets "item not found in inventory" for every one of them. That
// is exactly what the first live run of this script did: five trout, one real deposit, then
// three failures logged as if something had gone wrong. The snapshot is stale the moment the
// first deposit lands, so the ids are collected before any of them is sent.
func DepositAllExcept(d *Deps, bank *actions.Bank, keep func(name string) bool) Deposit {
	dep := Deposit{}

	// Wait for the side panel to populate before reading it.
	//
	// This is the gap that made the count meaningless. The deposits act on the bank's own side
	// component, which the server fills a moment AFTER the bank opens — actions.Bank's own
	// DepositAllMatching already waits for exactly this (bankSideReadyWait, "the side backpack
	// can arrive a moment after the bank opens") and internal/scripts/mulerunner names the same
	// trap in its own comment. This function is the one reimplementation that never had it, so a
	// trip could read an empty or half-filled panel, find one item to deposit, and report
	// "deposited 1 kind(s)" while leaving a full pack untouched.
	waitFor(d, "the bank's side panel to fill", 2*time.Second, func() bool {
		return len(bank.Inventory()) > 0 || !bank.IsOpen()
	})

	dep.PanelBefore = len(bank.Inventory())
	dep.PackBefore = packSlotsUsed(d)

	type row struct {
		id   int32
		name string
	}
	var rows []row
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			// An id the config cannot name is skipped, and skipping it silently is how a pack
			// fills with something nothing will ever bank. Counted so the caller can say so.
			dep.Unnamed++
			continue
		}
		if keep(obj.Name) {
			continue
		}
		seen[it.ID] = true
		rows = append(rows, row{id: it.ID, name: obj.Name})
	}

	// What the bank already held of exactly those ids, read before a single click goes out, so
	// that what it gains can be measured instead of assumed.
	dep.BankBefore = bankHeld(bank, seen)

	// ALL THE DEPOSIT-ALLS IN ONE BURST, five a game tick (twopertick-1012): each used to be followed by a 400 ms sleep.
	// The pack does not close up when a kind leaves it, so every slot read here is still right when the engine
	// reaches it; the readings below still say what actually moved.
	var packets []actions.BurstPacket
	for _, r := range rows {
		r := r
		packets = append(packets, actions.BurstPacket{Events: 1, Send: func() error {
			if err := bank.DepositAll(r.id); err != nil {
				d.logf("deposit %s: %v", r.name, err)
				return nil
			}
			dep.Clicked++
			return nil
		}})
	}
	if burst := actions.SendBurst(d.Session, Waiter(d), actions.BankMaxPerTick, nil, packets); len(packets) > 0 {
		d.logf("combat deposit: %d Deposit-All(s) clicked in %d tick(s)", burst.Sent, burst.Bursts)
		if burst.Err == nil {
			// The clicks are read a tick after they are written, so the readings below would see the pack before the
			// server acted (the old 400 ms sleeps gave each click time to land): wait until every kind's slot is gone
			// from the side panel, or two seconds, which is the stuck case's whole wait.
			target := dep.PanelBefore - len(rows)
			waitFor(d, "the deposits to land", 2*time.Second, func() bool {
				return !bank.IsOpen() || len(bank.Inventory()) <= target
			})
		}
	}

	dep.PanelAfter = len(bank.Inventory())
	dep.PackAfter = packSlotsUsed(d)

	// The bank's own rows can lag the side panel's by a tick, so they get a bounded moment to
	// show the deposit before being read. Taken AFTER the panel and pack readings on purpose:
	// those two feed the stuck-pack latch and the anti-spin floor, and settling them on a
	// different schedule would quietly change numbers this script already decides from. A
	// deposit that landed satisfies this on the first poll; only one that moved nothing pays the
	// whole wait, and that is the case worth being sure about.
	if len(rows) > 0 {
		waitFor(d, "the bank's own rows to show the deposit", 2*time.Second, func() bool {
			return !bank.IsOpen() || bankHeld(bank, seen) > dep.BankBefore
		})
	}
	dep.BankAfter = bankHeld(bank, seen)

	// What the keep-filter did not keep and the deposit did not move. A click that returns no
	// error is not a deposit, so this is the only honest account of what is still being carried.
	for _, it := range bank.Inventory() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" || keep(obj.Name) {
			continue
		}
		dep.Stuck = append(dep.Stuck, obj.Name)
	}
	return dep
}

// Deposit is what one DepositAllExcept actually did, as opposed to what it clicked.
//
// The old return value was "how many object ids were sent a Deposit-All without an error", which
// cannot tell a real deposit from a click the server ignored. The 273-trip spin logged
// "deposited 1 kind(s)" on every trip while freeing nothing at all, and that number is precisely
// why nobody could tell what was happening.
type Deposit struct {
	// Clicked is how many object ids were sent a Deposit-All without an error.
	Clicked int
	// PanelBefore and PanelAfter are used slots in the bank's own side panel, which is the view
	// the deposits act on.
	PanelBefore, PanelAfter int
	// PackBefore and PackAfter are the same reading taken from the persistent backpack
	// component. Recorded alongside the panel's so a divergence between the two views is
	// visible in the log rather than being a silent wrong answer.
	PackBefore, PackAfter int
	// BankBefore and BankAfter are what the BANK itself held, summed over exactly the object ids
	// this deposit set out to move and read from the bank's own rows either side of the clicks.
	//
	// Every other number in this struct is read from the pack, and a pack that emptied is
	// equally true of a run that dropped its load on the floor, so this pair is the only reading
	// that can tell a deposit which worked from one the server ignored. The live Prayer-window
	// runs that found this had to go to the bank's rows for exactly that reason.
	BankBefore, BankAfter int
	// Stuck names what the keep-filter did not keep and the deposit did not move.
	Stuck []string
	// Unnamed counts slots whose object id the config could not resolve. Those are skipped, so
	// they can never be banked and will sit in the pack for the rest of the run.
	Unnamed int
}

// Freed is how many slots the deposit actually released.
func (dep Deposit) Freed() int { return dep.PanelBefore - dep.PanelAfter }

// BankGained is how much the bank actually rose, which is what the word "deposited" has to mean
// in a log a fleet operator reads: the bank gained this much, not that a click was sent.
func (dep Deposit) BankGained() int { return dep.BankAfter - dep.BankBefore }

// Summary is the line a caller should log after a deposit, written so that a deposit which moved
// nothing cannot read like one that worked.
//
// It exists because the finish of a run discarded this struct entirely and the helper logs only
// when an individual deposit errors, so the whole banking step of a run that ends for good was
// silent: nothing at all between the booth approach and the logout click. Every clause below is
// a reading taken either side of the clicks rather than a report of what was attempted.
func (dep Deposit) Summary() string {
	bank := "the bank did not rise"
	switch g := dep.BankGained(); {
	case g > 0:
		bank = fmt.Sprintf("the bank rose by %d", g)
	case g < 0:
		bank = fmt.Sprintf("the bank FELL by %d", -g)
	}
	head := fmt.Sprintf("deposited %d kind(s): %s and %d slot(s) came free", dep.Clicked, bank, dep.Freed())
	if dep.BankGained() <= 0 && dep.Freed() <= 0 {
		head = fmt.Sprintf("deposited NOTHING: %d kind(s) clicked, %s and no slot came free", dep.Clicked, bank)
	}

	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "; pack %d/%d -> %d/%d", dep.PackBefore, PackSize, dep.PackAfter, PackSize)
	// What the account walks away still carrying, which nothing in the product recorded: real
	// runs logged out holding ten slots in one case and five in another and neither said so.
	if len(dep.Stuck) > 0 {
		fmt.Fprintf(&b, "; still carrying [%s]", strings.Join(dep.Stuck, ", "))
	}
	if dep.Unnamed > 0 {
		fmt.Fprintf(&b, "; %d slot(s) hold an object id the config cannot name, so nothing will ever bank them", dep.Unnamed)
	}
	return b.String()
}

// bankHeld totals what the bank's own rows hold of the given object ids. Quantity rather than
// slots, because a stack that grows by 27 occupies the same one row it did before.
func bankHeld(bank *actions.Bank, ids map[int32]bool) int {
	n := 0
	for _, it := range bank.Items() {
		if ids[it.ID] {
			n += int(it.Count)
		}
	}
	return n
}

// packSlotsUsed is the persistent backpack component's occupied slot count.
func packSlotsUsed(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0
	}
	return len(inv.NonEmpty())
}

// HeldCount totals the quantity of a named item in the pack. Quantity, not slots: a stack of
// 100 arrows is 100, which is what a restock decision needs.
func HeldCount(d *Deps, name string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, name) {
			n += int(it.Count)
		}
	}
	return n
}

// CoinsHeld is how much money the account is carrying, for the gear-buying loop's own log.
func CoinsHeld(d *Deps) int { return HeldCount(d, "Coins") }

// Withdrawal is what one WithdrawTo actually achieved, and why it stopped.
//
// It used to return a bare total, which is the reading that hid the 2026-09-19 spin: every one
// of 273 trips reported "carrying 3 Trout" and 3 was simply what the account had walked in with.
// A total cannot say that nothing arrived, so the run could not tell a full pack from a working
// restock.
type Withdrawal struct {
	// Before and After are how many were held either side of the attempt.
	Before, After int
	// Target is what was asked for.
	Target int
	// PackFull means the attempt was refused for want of space, or never made because the pack
	// was already full. Content's bank_withdraw_request answers a full pack with
	// "You don't have enough inventory space." and RETURNS, moving nothing, so a script that
	// only waits for the item to appear sees an indistinguishable timeout.
	PackFull bool
	// NotInBank means the bank held none of it.
	NotInBank bool
	// TimedOut means a click went out and nothing arrived within the wait.
	TimedOut bool
}

// Gained is how many actually arrived.
func (w Withdrawal) Gained() int { return w.After - w.Before }

// WithdrawTo tops a pack item up to target, reporting what it achieved and why it stopped.
func WithdrawTo(d *Deps, bank *actions.Bank, name string, target int) Withdrawal {
	have := HeldCount(d, name)
	w := Withdrawal{Before: have, After: have, Target: target}
	if have >= target {
		return w
	}

	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		for have < target {
			// A full pack is not a timeout, and asking anyway is what produced 114 refusals in
			// one measured run. Content refuses and returns without moving anything, so the only
			// way to tell the two apart is to look before clicking.
			if actions.IsFull(bank.Inventory()) {
				w.PackFull = true
				w.After = HeldCount(d, name)
				return w
			}
			before := have
			// In tens and fives rather than one at a time, and it is the food-load change that
			// makes this matter. A trip used to carry four to six bites, so one click each was
			// nothing; a rung that banks through a trapdoor now carries a pack full, and twenty
			// single withdrawals is twenty server round trips added to the very trip this is all
			// meant to make rarer. A chunk is only ever used when it cannot overshoot the target,
			// so the pack never fills with food the run did not ask for.
			need := target - have
			chunk := 1
			withdraw := bank.WithdrawOne
			switch {
			case need >= 10:
				chunk, withdraw = 10, bank.WithdrawTen
			case need >= 5:
				chunk, withdraw = 5, bank.WithdrawFive
			}
			if err := withdraw(it.ID); err != nil {
				d.logf("withdraw %s: %v", name, err)
				w.After = HeldCount(d, name)
				return w
			}
			// Wait for the whole chunk, but treat a partial arrival as progress rather than as a
			// failure: the loop simply asks again for what is still short. Only a chunk that
			// moved NOTHING is the refusal case, which is the one reading that distinguishes a
			// full pack from a bank that has run dry.
			waitFor(d, "the withdrawal to land", 3*time.Second, func() bool { return HeldCount(d, name) >= before+chunk })
			have = HeldCount(d, name)
			w.After = have
			if have == before {
				w.TimedOut = true
				w.PackFull = actions.IsFull(bank.Inventory())
				return w
			}
		}
		return w
	}
	w.NotInBank = true
	return w
}

// ---- gear ----

// EquipByName wears one pack item and confirms it reached the worn panel.
//
// A refusal is not an error here: the server answers a level it does not like with a chat
// line and nothing else, so "clicked and it never appeared" is the observation, and the
// caller decides what it means.
func EquipByName(d *Deps, name string, timeout time.Duration) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	id, ok := packItemID(d, eq, name)
	if !ok {
		return false
	}
	if err := eq.EquipItem(d.Config, id); err != nil {
		d.logf("wear %s: %v", name, err)
		return false
	}
	if !waitFor(d, "to be wearing "+name, timeout, func() bool { return eq.IsWorn(id) }) {
		return false
	}
	d.logf("wearing %s", name)
	return true
}

// bladeWords are the melee weapon kinds this trainer treats as bladed.
//
// Honest limit: this is derived from the gear table's own weapon suffixes, NOT from the web's
// own content script, which was not read. It matters because the Varrock sewer web is cut from
// the RIGHT-HAND slot only — a knife in the pack does nothing — so the trainer has to know
// whether what it is WEARING will cut. Every weapon the ladder ever buys is on this list, so
// the derivation is safe for this script and should be re-checked before anything else relies
// on it.
var bladeWords = []string{"scimitar", "longsword", "sword", "dagger"}

// ContainersReadable reports whether the backpack and worn panels can be read at all this pass.
//
// Every "what is this account holding" question in this package resolves through the sidebar
// tab that owns the panel, so when the server nulls those tabs the honest answer to all of them
// is "unknown" and the answer they actually give is "nothing". A macro event does exactly that:
// the mime's entry proc calls ~macro_events_set_tabs, which if_settab(null, ...)s the backpack
// and worn tabs for the whole event. Ask this before treating an empty WornNames or PackNames
// as evidence about the account rather than about the connection.
func ContainersReadable(d *Deps) bool {
	return actions.NewEquipment(d.Session, d.Components).Readable()
}

// WieldingBlade reports whether the worn weapon can cut the sewer web.
func WieldingBlade(d *Deps) bool {
	for _, name := range WornNames(d) {
		lower := strings.ToLower(name)
		for _, w := range bladeWords {
			if strings.Contains(lower, w) {
				return true
			}
		}
	}
	return false
}

// ShopTravelTimeout is how long a shop trip gets to reach its keeper.
//
// Longer than TravelWalkTimeout because a shop trip is not a walk to the rung next door: the
// gear ladder sends an account from Al Kharid to Barbarian Village to Falador, three or four
// hundred tiles a leg through gates and doors. Measured on this engine, three cross-map shop
// trips in a row died at exactly the two-minute mark with the account still walking, and the
// Al Kharid toll gate alone can eat that whole budget re-crossing itself. A trip that genuinely
// cannot be made still ends: the walker gives up on its own hop budget, and the caller retires
// the slot after three attempts.
const ShopTravelTimeout = 5 * time.Minute

// ShopStandRadius is how close to the keeper's own spawn tile a trip has to get. The keeper
// wanders, so arriving at the spawn is not arriving at the keeper — entityquery.OpenShop closes
// the last few tiles itself and answers ErrKeeperOutOfReach when it cannot.
const ShopStandRadius = 3

// ErrNotOnShelf means the keeper's shop does not carry the item at all.
//
// It is worth a sentinel rather than a string, because the caller's answer to it is specific
// and different from every other shop failure: the gear table records which shop LISTS an item,
// read from that shop's own .inv, but an .inv line carries a stock COUNT as well, and several
// are zero. Cassie's shield shop is the case that found this — it lists a mithril square shield
// and stocks none, so the best shield the table can offer is one the shelf never has. The right
// answer is to drop to the next tier the shop actually has, not to retry the same shelf.
var ErrNotOnShelf = errors.New("combatrunner: the shop does not have that item on the shelf")

// ErrShopUnreachable means the trip to the keeper never got there. Distinct from ErrNotOnShelf
// because it says nothing about the item: the same purchase may work once the route does.
var ErrShopUnreachable = errors.New("combatrunner: could not reach the keeper")

// BuyResult is what one shop trip actually did.
type BuyResult struct {
	Bought int
	// Said is what the server told us, when it refused.
	Said string
	// Shelf is what the keeper actually had, filled in when the wanted item was not there. It
	// turns one wasted trip into the answer to "then what CAN this shop sell us", which is the
	// difference between learning a shelf is empty in one trip and learning it a tier at a time.
	Shelf []string
}

// BuyFrom travels to a keeper, opens the shop and buys qty of an item, confirming against the
// pack rather than the click.
//
// Affordability is discovered here, not predicted: this client cannot read an item's price off
// the shop screen, and content's own buy price is a haggle formula over live stock, so a pure
// "can we afford it" answer would be a guess. The trip either gains the item or it does not,
// and the chat line says why.
func BuyFrom(d *Deps, site combatplan.ShopSite, itemName string, qty int) (BuyResult, error) {
	if !site.Reachable() {
		return BuyResult{}, fmt.Errorf("combatrunner: %s is behind %s, which this trainer never opens", site.Shop, site.Note)
	}
	if !TravelTo(d, site.Stand, ShopStandRadius, ShopTravelTimeout) {
		return BuyResult{}, fmt.Errorf("combatrunner: could not reach %s at %d,%d: %w", site.Keeper, site.Stand.X, site.Stand.Z, ErrShopUnreachable)
	}
	if err := openKeeperShop(d, site); err != nil {
		return BuyResult{}, err
	}
	shop := actions.NewShop(d.Session)
	defer func() { _ = shop.Close() }()

	var objID int32
	found := false
	var shelf []string
	for _, it := range shop.Stock() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		// A line the keeper lists but keeps none of is an advertisement, not a shelf entry,
		// and it has to read as absent here or the caller cannot strike it in one trip.
		// gamestate.Inventory.NonEmpty filters on the object id alone, so a stock-0 line really
		// does arrive with Count 0 rather than being dropped — which is why asking for one got
		// "The shop has run out of stock." instead of ErrNotOnShelf. Cassie is the case that
		// matters: her mithril square, steel square, steel kite and iron kite shields are four
		// consecutive zeros, so treating them one at a time cost four cross-map trips, each
		// paid for with a kill, before the plan reached a shield she actually keeps.
		if it.Count <= 0 {
			continue
		}
		shelf = append(shelf, obj.Name)
		if strings.EqualFold(obj.Name, itemName) {
			objID, found = it.ID, true
		}
	}
	if !found {
		return BuyResult{Shelf: shelf}, fmt.Errorf("combatrunner: %s does not have %s on the shelf (it has %s): %w",
			site.Keeper, itemName, strings.Join(shelf, ", "), ErrNotOnShelf)
	}

	mark := time.Now()
	before := HeldCount(d, itemName)
	if err := shop.Buy(objID, int32(qty)); err != nil {
		return BuyResult{}, fmt.Errorf("combatrunner: buying %s: %w", itemName, err)
	}
	waitFor(d, "the purchase to reach the pack", 6*time.Second, func() bool { return HeldCount(d, itemName) > before })
	got := HeldCount(d, itemName) - before
	res := BuyResult{Bought: got}
	if got <= 0 && d.Chat != nil {
		res.Said = d.Chat.Since(mark)
	}
	return res, nil
}

// openKeeperShop opens the keeper's shop, walking to the keeper itself when the engine's own
// click-walk cannot close the last few tiles.
//
// A ShopSite's Stand is the keeper's spawn tile, and keepers wander. Louie Legs is the case that
// found it: the table sends the account to 3321,3188, and across two runs the keeper was
// measured at 3314,3173 through 3316,3176. The trip arrived at the spawn, the Trade click landed
// 3 to 14 tiles short, and the engine's own walk did not close the gap — "the keeper is out of
// reach and the account is not getting closer" — three trips in a row, and the buying loop
// retired the legs slot. Walking our own pathfinder to where the keeper is NOW is the answer a
// fixed arrival tile cannot give.
//
// ErrKeeperOutOfReach and a shop that would not open with the keeper "adjacent" both get the
// second attempt. Adjacent is counted in tiles and ignores walls: live 7 Oct 2026, Ranael's Stand
// (3315,3167) is outside her shop's wall with the keeper inside at 3317,3165, one tile away by
// the count, and every Trade click for the whole night failed with "did not open after 4 Trade
// clicks (keeper 1 tiles away)". The walk goes onto the keeper's own tile (NPCs do not block a
// player), which takes the door; a walk that falls short still leaves the account closer.
func openKeeperShop(d *Deps, site combatplan.ShopSite) error {
	err := entityquery.OpenShop(d.Session, d.Config, site.Keeper, Waiter(d))
	if err == nil {
		return nil
	}
	if !errors.Is(err, entityquery.ErrKeeperOutOfReach) && !errors.Is(err, actions.ErrNotConfirmed) {
		return fmt.Errorf("combatrunner: opening %s's shop: %w", site.Keeper, err)
	}
	keeper, ok := entityquery.Npcs(d.Session.State(), d.Config).Name(site.Keeper).Action("Trade").Nearest()
	if !ok {
		return fmt.Errorf("combatrunner: opening %s's shop: %w", site.Keeper, err)
	}
	d.logf("%s is out of reach at %d,%d (%v) — walking to the keeper rather than to its spawn tile at %d,%d",
		site.Keeper, keeper.X, keeper.Z, err, site.Stand.X, site.Stand.Z)
	// TravelTo, not WalkNear, and the difference is a whole gear slot. WalkNear is the raw
	// same-level pathfinder over the baked pack, which treats a shut door as solid, so the one
	// fallback that exists to rescue a wandering keeper could not reach a keeper standing behind
	// one. Measured at Peksa, whose shop is through the door at 3076,3427: the account stopped
	// 2 tiles short with "the engine's own walk is not closing that gap", retried, and found the
	// keeper 5 tiles away by then — twice, and the helm slot was on course to retire unbought
	// for a reason that had nothing to do with the shop. TravelTo asks webwalk first, which
	// knows that door, and falls back to WalkNear when there is no walker.
	if !TravelTo(d, tile.Tile{X: keeper.X, Z: keeper.Z}, 0, 60*time.Second) {
		d.logf("%s: the walk onto the keeper's tile %d,%d fell short; trying the shop from where it stopped", site.Keeper, keeper.X, keeper.Z)
	}
	if err := entityquery.OpenShop(d.Session, d.Config, site.Keeper, Waiter(d)); err != nil {
		return fmt.Errorf("combatrunner: opening %s's shop after walking to the keeper: %w", site.Keeper, err)
	}
	return nil
}

// ---- food and bones ----

// TrainerBones are the bone items this ladder ever sees. Only big bones actually drop here —
// every giant and the Ogre carry param=death_drop,big_bones, unconditionally — but the plain
// ones are listed so a bone picked up from somebody else's kill still gets buried.
var TrainerBones = []string{"Bones", "Big bones", "Bat bones", "Burnt bones"}

// BuryOneBone buries the first bone in the pack, confirmed by the pack shrinking.
func BuryOneBone(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !isBoneName(obj.Name) {
			continue
		}
		before := len(inv.NonEmpty())
		if err := actions.Bury(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
			d.logf("bury: %v", err)
			return false
		}
		return waitFor(d, "the bones to be buried", 3*time.Second, func() bool {
			cur := d.Session.State().Inventory(PlayerPack)
			return cur == nil || len(cur.NonEmpty()) < before
		})
	}
	return false
}

func isBoneName(name string) bool {
	for _, b := range TrainerBones {
		if strings.EqualFold(b, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// HasBone reports whether the pack holds anything worth burying.
func HasBone(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && isBoneName(obj.Name) {
			return true
		}
	}
	return false
}

// FoodCount counts pack slots holding any form of foodName.
func FoodCount(d *Deps, foodName string) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && fighterfood.IsFoodItem(obj.Name, foodName) {
			n++
		}
	}
	return n
}

// EatOnce eats one bite and confirms a real HP rise.
func EatOnce(d *Deps, foodName string) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return false
	}
	slot, objID := -1, int32(0)
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && fighterfood.IsFoodItem(obj.Name, foodName) {
			slot, objID = it.Slot, it.ID
			break
		}
	}
	if slot < 0 {
		return false
	}
	before, ok := d.Session.State().HP()
	if !ok {
		return false
	}
	if err := actions.Eat(d.Session, objID, slot, PlayerPack); err != nil {
		d.logf("eat: %v", err)
		return false
	}
	waitFor(d, "hp to rise after eating", 3*time.Second, func() bool {
		hp, ok := d.Session.State().HP()
		return ok && hp > before
	})
	hp, _ := d.Session.State().HP()
	if hp > before {
		d.logf("ate a %s (%d -> %d hp)", foodName, before, hp)
		return true
	}
	return false
}

// HPFraction is current over maximum hitpoints, 1 when either is unknown so a script never
// panics on a missing reading.
func HPFraction(d *Deps) float64 {
	hp, ok := d.Session.State().HP()
	max, ok2 := d.Session.State().MaxHP()
	if !ok || !ok2 || max <= 0 {
		return 1
	}
	return float64(hp) / float64(max)
}

// ---- combat style ----

// TrainingStyle maps the stat the ladder chose to the weapon's own combat-style label.
//
// This is the step that makes the stat choice mean anything. The server trains whatever style
// the weapon is set to, not the stat a script would like to train, so a trainer that picks
// "strength" and never touches the style simply trains attack forever.
func TrainingStyle(s combatplan.Skill) (string, bool) {
	switch s {
	case combatplan.SkillAttack:
		return "attack", true
	case combatplan.SkillStrength:
		return "strength", true
	case combatplan.SkillDefence:
		return "defence", true
	}
	return "", false
}

// EnsureTrainingStyle points the weapon at the stat the ladder chose, confirmed against the
// style the client reports rather than the click.
func EnsureTrainingStyle(d *Deps, skill combatplan.Skill) bool {
	label, ok := TrainingStyle(skill)
	if !ok {
		return false
	}
	style, ok := actions.ParseCombatStyle(label)
	if !ok {
		return false
	}
	if actions.HasCombatStyle(d.Session, style) {
		return true
	}
	if _, err := actions.SetCombatStyle(d.Session, style); err != nil {
		d.logf("set combat style: %v", err)
		return false
	}
	if waitFor(d, "the combat style to change", 3*time.Second, func() bool { return actions.HasCombatStyle(d.Session, style) }) {
		d.logf("combat style is now %s", label)
		return true
	}
	return false
}

// ReselectTrainingStyle clicks the style button for skill even when the client already reads
// that style as selected, and reports whether the client reads it afterwards. It answers a
// fight that paid the wrong stat (scarabplan.MeleeStyleMisfire): EnsureTrainingStyle trusts
// com_mode and never clicks again once it matches, and a click re-runs the server's own style
// setup (set_attackstyle, then player_combat_stat).
func ReselectTrainingStyle(d *Deps, skill combatplan.Skill) bool {
	label, ok := TrainingStyle(skill)
	if !ok {
		return false
	}
	style, ok := actions.ParseCombatStyle(label)
	if !ok {
		return false
	}
	if _, err := actions.SetCombatStyle(d.Session, style); err != nil {
		d.logf("set combat style: %v", err)
		return false
	}
	return waitFor(d, "the combat style to hold", 3*time.Second, func() bool { return actions.HasCombatStyle(d.Session, style) })
}

// EnsureRun switches run on when it is off: one click, no wait (the server turns it off itself at 0 energy and
// the varp says so, so this is a read and, only when off, a packet). Run is the walk between two fights and to a
// drop, at two tiles a tick.
func EnsureRun(d *Deps) {
	if !d.Session.State().RunEnabled() {
		_ = actions.SetRun(d.Session, true)
	}
}

// EnsureRetaliate turns auto-retaliate on, so a monster that opens the fight is answered
// without the script having to notice first.
func EnsureRetaliate(d *Deps, want bool) bool {
	if d.Session.State().AutoRetaliateOn() == want {
		return true
	}
	if err := actions.SetAutoRetaliate(d.Session, want); err != nil {
		d.logf("set auto retaliate: %v", err)
		return false
	}
	return waitFor(d, "auto retaliate to change", 3*time.Second, func() bool {
		return d.Session.State().AutoRetaliateOn() == want
	})
}

// ---- fighting ----

// MonstersNear returns every visible NPC of this rung's monster that is inside the leash and
// still worth attacking. monsterName is one NPC name or a herd key (combatplan.Monster.HerdKey: the
// Barbarians and the Barbarian women are one herd), matched case-insensitively.
func MonstersNear(d *Deps, monsterName string, anchor tile.Tile, leash int) []gamestate.NPC {
	var out []gamestate.NPC
	for _, n := range d.Session.State().NPCs() {
		t, ok := d.Config.Npc(n.Type)
		if !ok || !combatplan.HerdMatch(monsterName, t.Name) {
			continue
		}
		if cachecfgOp(t.Ops, "Attack") == 0 {
			continue
		}
		at := tile.Tile{X: n.X, Z: n.Z, Level: n.Level}
		if at.Level != anchor.Level || tile.Chebyshev(at, anchor) > leash {
			continue
		}
		// A monster already at zero health is somebody's finished kill, not a target.
		if n.TotalHealth > 0 && n.Health == 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

// NearestMonster picks the closest of MonstersNear that there is a route to, and only falls back
// to the closest of any when nothing in the leash can be walked to.
//
// The preference is the general lesson from the Al Kharid rung, applied here so that every caller
// gets it without changing what this function promises: it still answers with a monster whenever
// one is in the leash. Closest alone put three long runs on a warrior standing inside the palace,
// one Chebyshev step away through a wall, over and over. FightOne refuses to click an unreachable
// target now, so the fallback costs one refusal rather than a ninety-second fight with a wall.
func NearestMonster(d *Deps, monsterName string, anchor tile.Tile, leash int) (gamestate.NPC, bool) {
	here, ok := currentTile(d)
	if !ok {
		here = anchor
	}
	var best, bestAny gamestate.NPC
	bestDist, bestAnyDist := 0, 0
	found, foundAny := false, false
	for _, n := range MonstersNear(d, monsterName, anchor, leash) {
		at := tile.Tile{X: n.X, Z: n.Z, Level: n.Level}
		dist := tile.Chebyshev(here, at)
		if !foundAny || dist < bestAnyDist {
			bestAny, bestAnyDist, foundAny = n, dist, true
		}
		if found && dist >= bestDist {
			continue
		}
		if reachable, _ := Reachable(d, at); !reachable {
			continue
		}
		best, bestDist, found = n, dist, true
	}
	if found {
		return best, true
	}
	return bestAny, foundAny
}

func cachecfgOp(ops [5]string, want string) int {
	for i, op := range ops {
		if strings.EqualFold(op, want) {
			return i + 1
		}
	}
	return 0
}

// FightResult is one engagement's outcome.
type FightResult struct {
	Verdict combatplan.Verdict
	Site    combatplan.KillSite
	// Attacked is whether the attack click actually went out.
	Attacked bool
	// Unreachable is whether the fight never started because there is no route from where we
	// are standing to the target, as opposed to a click that went out and produced no kill.
	// The two need telling apart: one is a target to stop picking, the other is a fight that
	// went badly.
	Unreachable bool
	// Refused is the server's refusal of the attack (RefusedContested, RefusedUnderAttack), ""
	// when it was not refused. A refused fight ends at once instead of waiting out the timeout.
	Refused Refusal
	// NotOurs is a target that died with no damage from this account (killIsOurs): somebody
	// else's kill, not reported to OnKill and not to be counted.
	NotOurs bool
	// Doors is how many shut doors FightOne crossed to reach the target (Deps.Doors), 0 for a plain walk.
	// DoorFailed says a door route was found and the crossing did not land, so no attack went out: a
	// different fact from Unreachable's, which is that no route was found at all.
	Doors      int
	DoorFailed bool
}

// ApproachRadius bounds how far out of its way a fight is willing to walk to reach a target, and
// so how far the reachability check below looks for a route.
//
// It is generous against the ladder's own leashes (8 to 14) and mean against the map: a monster
// only chases as far as its own maxrange from its spawn tile, so a route that has to go twenty
// tiles around a building to reach something four tiles away loses the fight it walked to.
const ApproachRadius = 24

// Reachable answers the question a distance comparison cannot: whether there is a route from
// where the account is standing to target, rather than only a short straight line to it.
//
// It fails OPEN — no pack, or no known tile yet, answers "reachable" with the reason — because
// the cost of the two mistakes is not symmetric. A false "unreachable" makes a run refuse to
// fight something it could have killed; the check exists to stop a run grinding on something it
// can never reach, and a run with no collision data has no business overruling the server.
func Reachable(d *Deps, target tile.Tile) (bool, string) {
	if d.Pack == nil {
		return true, "no collision pack is loaded, so reachability cannot be asked"
	}
	here, ok := currentTile(d)
	if !ok {
		return true, "our own tile is not known yet"
	}
	if here.Level != target.Level {
		return false, "a different level"
	}
	res := d.Pack.ReachableWithin(
		pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level},
		pathfind.NavPoint{X: target.X, Z: target.Z, Level: target.Level},
		ApproachRadius)
	if res.OK {
		return true, ""
	}
	return false, res.Reason
}

// reachTarget is the first thing FightOne does: ask whether the target can be reached at all, and with Deps.Doors
// cross the shut doors on the way to it. A target behind a shut door is a route like any other once the door is open,
// so it crosses and asks again, because the walk after it is a plain one and the monster may have moved while the
// door was being opened. It returns how many doors it crossed, or the result FightOne ends with when the attack must
// not go out.
func reachTarget(d *Deps, target tile.Tile) (crossed int, refused *FightResult) {
	if !d.Doors {
		if ok, why := Reachable(d, target); !ok {
			d.logf("no route from here to the target at %d,%d,%d (%s) — not clicking it", target.X, target.Z, target.Level, why)
			return 0, &FightResult{Verdict: combatplan.Unknown, Unreachable: true}
		}
		return 0, nil
	}
	reach := ReachFor(d, target)
	if !reach.OK {
		d.logf("no route from here to the target at %d,%d,%d (%s) — not clicking it", target.X, target.Z, target.Level, reach.Why)
		return 0, &FightResult{Verdict: combatplan.Unknown, Unreachable: true}
	}
	if reach.Doors == 0 {
		return 0, nil
	}
	if !CrossDoors(d, reach) {
		return 0, &FightResult{Verdict: combatplan.Unknown, Unreachable: true, DoorFailed: true}
	}
	if again := ReachFor(d, target); !again.OK || again.Doors > 0 {
		d.logf("door route: crossed %d door(s) but the target at %d,%d,%d is still not a plain walk away — not clicking it", reach.Doors, target.X, target.Z, target.Level)
		return reach.Doors, &FightResult{Verdict: combatplan.Unknown, Unreachable: true, DoorFailed: true, Doors: reach.Doors}
	}
	return reach.Doors, nil
}

// FightOne walks into reach, attacks, and waits for the kill.
//
// The approach is capped by the monster's own disengage range rather than a fixed number: a
// cowardly giant stops chasing past maxrange tiles from ITS OWN spawn tile (the engine
// measures against startX/startZ, and maxrange defaults to wanderrange + 2, which is 5 for
// every monster on this ladder). Walking further than that to start a fight simply loses it.
//
// Reachability is asked FIRST, at every distance, and that ordering is the whole fix for a bug
// that wedged three long runs. The walk below is skipped whenever the target is already
// Chebyshev 1 away, so before this check the one case that most needs the pathfinder was the one
// case that never consulted it — and a tile one step away through a palace wall is Chebyshev 1.
// The attack click is accepted by the server whatever is in between, so nothing further down
// reports the failure either: the fight simply times out with the target still standing, and a
// caller that picks by distance hands back the same monster forever.
func FightOne(d *Deps, npc gamestate.NPC, timeout time.Duration) FightResult {
	target := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	crossed, refused := reachTarget(d, target)
	if refused != nil {
		return *refused
	}
	if here, ok := currentTile(d); ok && tile.Chebyshev(here, target) > 1 && !ClickAtOnce(d, target) {
		if !WalkNear(d, target, 1, 30*time.Second) {
			return FightResult{Verdict: combatplan.Unknown, Doors: crossed}
		}
	}
	if d.OnKillSpent != nil {
		d.runesAtAttack = packRunes(d)
		defer func() { d.runesAtAttack = nil }()
	}
	w := killWatch{attackAt: time.Now()}
	if sk, ok := d.Session.State().Skill(HitpointsStat); ok {
		w.hpXP, w.hpKnown = sk.XP, true
	}
	if err := AttackNPC(d, npc); err != nil {
		d.logf("attack: %v", err)
		return FightResult{Verdict: combatplan.Unknown, Doors: crossed}
	}
	res := watchKill(d, npc.Index, timeout, w)
	// A fight that ended with no kill reported still cost its runes: say so, so the Magic card counts
	// every offensive spell and not only the ones that killed.
	killed := !res.notOurs && (res.verdict == combatplan.Killed || res.verdict == combatplan.LikelyKilled || res.verdict == combatplan.Vanished)
	if d.OnFightSpent != nil && d.runesAtAttack != nil && !killed {
		if spent := runesSpent(d.runesAtAttack, packRunes(d)); len(spent) > 0 {
			d.OnFightSpent(npc.Type, spent)
		}
	}
	return FightResult{Verdict: res.verdict, Site: res.site, Attacked: true, Refused: res.refused, NotOurs: res.notOurs, Doors: crossed}
}
