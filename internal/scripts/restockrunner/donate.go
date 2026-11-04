package restockrunner

// The donor's actual trip: draw the surplus coins out of its own bank, walk to the mule's post,
// hand them over, and put whatever did not cross back where it was safe.
//
// WHAT IS REUSED RATHER THAN REBUILT, because a second trade implementation is the last thing
// this repo needs. The hand-over itself is donorrunner.Run — the same proven code path every
// skilling script's ordinary mule hand-off already uses, including its meet, its offer
// settling, its untradeable-item learning and its own coin split (Options.CoinsName/CoinsID/
// CoinsHeld/Reserve, which keeps Reserve back in the donor's pocket at the trade screen). A
// restock donation IS an ordinary hand-over; the only new things are what triggers it, which
// rows it draws, and that it is measured in coins rather than pack slots.
//
// WHAT IS NEW AND WHY. donorrunner.Run does not open banks — every caller does its own
// withdrawal first, with its own layout, before calling it (see internal/bot/fisher.go's
// hand-off). So this file adds exactly that: a bank open at a stand the caller names, a
// withdraw-all of the coin row, and — the part a caller could easily forget — putting the
// remainder BACK afterwards. A donor that walked to the mule with its entire bank in its pocket
// and failed to trade must not be left carrying it: a death drops the lot, which would turn a
// feature meant to fund the fleet into a way of losing its money.
//
// THE RECEIPT. Given is the coins that actually left this account's own pack across the trade,
// measured as a before/after count of the real inventory. Not Result.Traded, not Result.Slots,
// not "the trade closed". The mule proves the other half from its own bank; the two halves
// together are what a harness should assert on.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/donorrunner"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/restockplan"
	"rsb0t-go/internal/scripts/tile"
)

// Delayer sleeps for d, returning false early if the caller should stop instead of true once d
// has fully elapsed — the same shape every runner package in this repo uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one donation needs from the outside world, the same sibling shape as
// donorrunner.Deps/mulerunner.Deps/fisherrunner.Deps.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	Bus        *mulebus.Bus
	Me         string
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// PlayerPack is the persistent inventory-tab component id — see essencerunner.PlayerPack's own
// doc comment for why this fixed, always-current component is read instead of scanning every
// known inventory.
const PlayerPack = 3214

// DonateOptions is one trip.
type DonateOptions struct {
	// Mule is who called and Post where it said it is standing — both taken from the Call, not
	// from settings, so a mule that has moved is walked to where it actually is.
	Mule string
	Post tile.Tile
	// BankStand is where THIS account banks. The caller supplies it because only the calling
	// script knows where its own bank is; this package will not guess.
	BankStand tile.Tile
	// Reserve is what stays in the donor's pocket at the trade screen. Zero uses
	// restockplan.DonorReserveCoins, which is the number this repo argued for; a caller that
	// passes its own is trusted, but a caller that passes nothing gets the safe one rather
	// than zero.
	Reserve int
	// CoinsName is the canonical coin item name; blank uses restockplan.CoinsItem.
	CoinsName string
}

// DonateResult is what the trip actually achieved.
type DonateResult struct {
	// Given is coins CONFIRMED to have left this account's own pack across the trade. It is
	// the only number here that is evidence of anything.
	Given int
	// Drawn is what came out of the bank, Returned what went back in afterwards.
	Drawn, Returned int
	// Reason says what happened, set on every outcome including a successful one.
	Reason string
	// Stopped is true when the caller's own Delay asked to stop mid-trip.
	Stopped bool
}

// walkNearTimeout bounds one leg. Matches the 75 seconds donorrunner.walkNear already budgets,
// for the same reason: it re-checks real FindPath progress every iteration rather than sleeping
// blindly, so this is a stall guard and not a route budget.
const walkNearTimeout = 75 * time.Second

var emergencyFleeHome pathfind.NavPoint // unused while emergencyFleeDriftTiles <= 0

// This package opts OUT of EmergencyFlee's drift check, exactly as donorrunner does and for the
// identical reason recorded there: a donation is script-agnostic, the origin varies with
// whichever script called it, and there is no single fixed "home" to measure drift against. The
// InCombat branch, which is the one that matters mid-walk, is unaffected by driftTiles <= 0.
const emergencyFleeDriftTiles = 0

func currentTile(d *Deps) (tile.Tile, bool) {
	x, z, level, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
}

// walkNear paths to dest and walks there, re-clicking from wherever the account stops, until
// within radius tiles — this package's own copy of the same small helper every runner package
// keeps (see mulerunner.walkNear's own doc comment on why this is duplicated per-package rather
// than shared).
func walkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		randomevents.EmergencyFleeCombatAware(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
		here, ok := currentTile(d)
		if ok && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		if !ok {
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			continue
		}
		res := d.Pack.FindPath(
			pathfind.NavPoint{X: here.X, Z: here.Z, Level: here.Level},
			pathfind.NavPoint{X: dest.X, Z: dest.Z, Level: dest.Level})
		if !res.OK {
			d.logf("restock: no path to %d,%d,%d: %s", dest.X, dest.Z, dest.Level, res.Reason)
			return false
		}
		if len(res.Waypoints) < 2 {
			return true
		}
		if err := actions.Walk(d.Session, res.Waypoints, false); err != nil {
			d.logf("restock: walk: %v", err)
			return false
		}
		if !d.Delay(2 * time.Second) {
			return false
		}
	}
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

type waiter struct{ d *Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !w.d.Delay(200 * time.Millisecond) {
			return false
		}
	}
	return false
}
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

// openNearestBankBooth finds every "Bank booth" candidate near (x,z,level) and tries them
// nearest-first — duplicated per this project's established per-package convention (see
// essencerunner.openNearestBankBooth's own doc comment for the live bake-artifact bug this
// nearest-first retry recovers from).
func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	if d.Locs == nil || d.Config == nil || d.Components == nil {
		d.logf("restock: no loc index, config or interface archive — cannot open a bank")
		return nil, false
	}
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if len(candidates) == 0 {
		candidates = d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth"}, live)
	}
	if len(candidates) == 0 {
		d.logf("restock: could not find a bank booth near %d,%d", x, z)
		return nil, false
	}
	const maxTries = 4
	for i, booth := range candidates {
		if i >= maxTries {
			break
		}
		// Onto a tile the booth answers from: the server ignores a click from a diagonal one in
		// silence (freshstartrunner.WalkToUse).
		if !freshstartrunner.WalkToUse(fresh(d), tile.Tile{X: booth.X, Z: booth.Z, Level: booth.Level}, walkNearTimeout) {
			continue
		}
		op := booth.Op
		if op == 0 {
			op = 1
		}
		if err := actions.OpLoc(d.Session, op, booth.X, booth.Z, booth.ID); err != nil {
			d.logf("restock: OpLoc on the bank booth: %v", err)
			continue
		}
		bank := actions.NewBank(d.Session, d.Components)
		w := waiter{d}
		if w.DelayUntil(bank.IsOpen, timeout) {
			return bank, true
		}
	}
	return nil, false
}

// OpenBankNear walks to within radius of stand and opens the nearest bank booth.
func OpenBankNear(d *Deps, stand tile.Tile, radius int) (*actions.Bank, bool) {
	if !walkNear(d, stand, radius) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	return openNearestBankBooth(d, x, z, level, 20*time.Second)
}

func packItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// PocketCoins is how many coins are in this account's own pack right now, counted by NAME so a
// server that hands back a different coin object id for a large stack is still counted.
func PocketCoins(d *Deps, name string) int {
	if d.Config == nil {
		return 0
	}
	return int(actions.CountByName(packItems(d), d.Config, name))
}

// BankCoins is how many coins an ALREADY-OPEN bank holds. It must be called against a bank that
// was really opened: State().Inventory keeps the last reading after a bank closes, so a count
// taken without an open is the previous one again — the exact trap
// cmd/lootchestmuletest's own readMuleBank comment records.
func BankCoins(d *Deps, bank *actions.Bank, name string) int {
	if bank == nil || d.Config == nil {
		return 0
	}
	return int(actions.CountByName(bank.Items(), d.Config, name))
}

// heldObjID resolves name to the un-noted object id that `items` ACTUALLY holds, falling back
// to the first usable id when none of them is there.
//
// This is not defensive tidiness; a live run on 2026-09-21 failed on exactly it. "Coins" is not
// one object id — content defines a separate id per stack graphic (one coin, two coins, a pile,
// and so on) — so taking the first un-noted id off cfg.ObjIDs and calling Bank.WithdrawAll with
// it produced `withdraw all Coins: actions: item not found in inventory`, and a donor that had
// just been sent for walked away with nothing. mulerunner.bankedID solves the identical problem
// for the hand-OUT direction and this is its twin, duplicated per this project's established
// per-package convention.
func heldObjID(d *Deps, items []gamestate.SlotItem, name string) int32 {
	if d.Config == nil {
		return 0
	}
	var first int32
	for _, id := range d.Config.ObjIDs(name) {
		obj, ok := d.Config.Obj(id)
		if !ok || obj.Noted() {
			continue
		}
		if first == 0 {
			first = int32(id)
		}
		if countOfID(items, int32(id)) > 0 {
			return int32(id)
		}
	}
	return first
}

func countOfID(items []gamestate.SlotItem, id int32) int {
	n := 0
	for _, it := range items {
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

// Donate runs one whole trip and reports what actually crossed.
//
// It is bounded at every stage and never leaves the account worse off than it found it: a
// failure at the bank returns before anything is drawn, a failure at the trade walks the money
// back to the bank, and a stop request ends it at the next checkpoint. A donation is never
// urgent enough to justify stranding a working account.
func Donate(d *Deps, o DonateOptions) DonateResult {
	name := o.CoinsName
	if strings.TrimSpace(name) == "" {
		name = restockplan.CoinsItem
	}
	reserve := o.Reserve
	if reserve <= 0 {
		reserve = restockplan.DonorReserveCoins
	}
	if d.Delay == nil {
		d.Delay = RealDelay
	}
	if strings.TrimSpace(o.Mule) == "" {
		return DonateResult{Reason: "no mule named"}
	}

	// ---- draw the surplus ----

	bank, ok := OpenBankNear(d, o.BankStand, 4)
	if !ok {
		return DonateResult{Reason: "could not reach or open my own bank"}
	}
	banked := BankCoins(d, bank, name)
	pocket := PocketCoins(d, name)
	spare, canGive, why := restockplan.SpareCoins(banked, pocket, 0)
	if !canGive {
		_ = bank.Close()
		return DonateResult{Reason: "nothing to donate after all: " + why}
	}
	d.logf("restock: %d banked + %d in pocket, %d spare above the %d reserve — drawing for %q",
		banked, pocket, spare, reserve, o.Mule)

	if banked > 0 {
		if id := heldObjID(d, bank.Items(), name); id > 0 {
			before := PocketCoins(d, name)
			if err := bank.WithdrawAll(id); err != nil {
				d.logf("restock: withdraw all %s: %v", name, err)
			} else {
				w := waiter{d}
				w.DelayUntil(func() bool { return PocketCoins(d, name) > before }, 4*time.Second)
			}
		} else {
			d.logf("restock: could not resolve an un-noted object id for %q — donating only what is in my pocket", name)
		}
	}
	drawn := PocketCoins(d, name) - pocket
	if drawn < 0 {
		drawn = 0
	}
	_ = bank.Close()

	held := PocketCoins(d, name)
	if held-reserve <= 0 {
		// The bank read and the withdrawal disagreed — the coins were not there, or the
		// withdrawal did not land. Say so rather than walking to the mule with nothing.
		return DonateResult{Drawn: drawn, Reason: fmt.Sprintf("holding only %d %s after the withdrawal, not above the %d reserve", held, name, reserve)}
	}

	// ---- the hand-over: donorrunner's own proven path ----

	// Resolved from the PACK now that the coins are drawn, for the same reason the bank read
	// above resolves from the bank: the id a stack carries is the id the trade screen wants.
	coinsID := heldObjID(d, packItems(d), name)
	before := held
	rd := &donorrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Log: d.Log,
		Delay: func(t time.Duration) bool { return d.Delay(t) },
		Bus:   d.Bus, Me: d.Me,
	}
	res := donorrunner.Run(rd, donorrunner.Options{
		Mule: o.Mule, Stand: o.Post,
		RowsInBank: 1, Drawn: 1,
		Load: func() []donorplan.LoadGroup {
			if PocketCoins(d, name)-reserve <= 0 {
				return nil
			}
			return []donorplan.LoadGroup{{Name: name, ID: int(coinsID), Slots: 1}}
		},
		HaulLeft: func() int {
			if PocketCoins(d, name)-reserve > 0 {
				return 1
			}
			return 0
		},
		CoinsName: name, CoinsID: coinsID,
		CoinsHeld: func() int { return PocketCoins(d, name) },
		Reserve:   reserve,
	})

	given := before - PocketCoins(d, name)
	if given < 0 {
		given = 0
	}
	out := DonateResult{Given: given, Drawn: drawn, Stopped: res.Stopped}
	switch {
	case res.Stopped:
		out.Reason = "stopped mid-donation"
	case given > 0:
		out.Reason = fmt.Sprintf("%d %s left my pack for %q", given, name, o.Mule)
	case res.GaveUp:
		out.Reason = "the mule never showed up: " + res.Reason
	default:
		out.Reason = "nothing crossed: " + res.Reason
	}

	// ---- put the remainder back, whatever happened ----
	//
	// Always, not only on failure. A partial hand-over leaves the rest in the pocket too, and
	// an account carrying its whole bank balance around is one death away from losing it.
	if left := PocketCoins(d, name) - reserve; left > 0 {
		out.Returned = returnToBank(d, o.BankStand, name, reserve)
	}
	return out
}

// returnToBank walks back and deposits everything above the reserve, reporting what actually
// went in (counted from the pocket, which is the only side this account can observe). A failure
// here is logged and not fatal: the coins are still the donor's, and its own script banks on
// its next ordinary cycle.
func returnToBank(d *Deps, stand tile.Tile, name string, reserve int) int {
	before := PocketCoins(d, name)
	if before-reserve <= 0 {
		return 0
	}
	bank, ok := OpenBankNear(d, stand, 4)
	if !ok {
		d.logf("restock: could not get back to the bank holding %d %s — my own script will bank them on its next cycle", before, name)
		return 0
	}
	// DepositAll, not a partial deposit of "everything above the reserve": the reserve is
	// TRAVEL money and this account is standing at its own bank, which is precisely where it
	// does not need any. fisherplan.DeathFloatCoins — the float that actually matters, the one
	// a death cannot take — is a BANKED float, so banking the lot is what puts the money where
	// that rule wants it. The pocket reserve reappears the next time the script draws for a
	// trip.
	id := heldObjID(d, packItems(d), name)
	if id > 0 {
		if err := bank.DepositAll(id); err != nil {
			d.logf("restock: depositing the remainder: %v", err)
		}
		w := waiter{d}
		w.DelayUntil(func() bool { return PocketCoins(d, name) < before }, 4*time.Second)
	}
	_ = bank.Close()
	deposited := before - PocketCoins(d, name)
	if deposited < 0 {
		deposited = 0
	}
	if deposited > 0 {
		d.logf("restock: put %d %s back in the bank", deposited, name)
	}
	return deposited
}
