// Package vialrunner is the I/O half of the VialFiller port: bank, an optional Jatix shop
// restock, and filling vials at the Falador fountain, driven by the pure decisions in
// internal/scripts/vialplan. Follows the exact split every other runner package already proved
// — see docs/script_porting_framework.md.
//
// Filling a vial on the fountain uses actions.Equipment.UseOnLoc (OPLOCU) rather than a loc-menu
// click: this content pack's own Fountain locs carry no "Fill"-labelled op (confirmed live
// before porting, unlike a mineable rock or a choppable tree, which do have a named op this
// codebase's other runners click directly).
//
// Deliberately NOT ported here: the paint/status overlay — no headless equivalent.
package vialrunner

import (
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
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/vialplan"
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

// Deps is everything one bank/shop/fill trip needs from the outside world.
type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
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

// WaitForGuardian pauses while a random event is detected at all — duplicated per this
// project's established per-package convention.
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

// fresh adapts d to freshstartrunner.Deps — the same field-for-field copy every sibling runner
// that calls into freshstartrunner already does (see e.g. woodcutrunner.fresh).
func fresh(d *Deps) *freshstartrunner.Deps {
	return &freshstartrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat, Delay: freshstartrunner.Delayer(d.Delay), Log: d.Log,
	}
}

// WalkNear paths to dest and walks there, stopping once within radius tiles — a thin wrapper
// around freshstartrunner.WalkNear, NOT one of this project's usual small hand-duplicated
// WalkNears: found live (this port's own first live run) that the Falador fountain-to-bank leg
// crosses a closed door/gate the static collision pack's own FindPath graph reports as
// traversable but the real server refuses to walk through — a plain recompute-and-rewalk loop
// spun in place for 90s straight. This is the identical class of gap woodcutrunner.WalkNear/
// thieverrunner.WalkNear already hit and fixed the same way: delegate to
// freshstartrunner.WalkNear, which already has the generic door-opening/level-changing recovery
// (tryOpenDoor/tryChangeLevel/tryClimbOver) rather than hand-porting a third bespoke door hack.
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	return freshstartrunner.WalkNear(fresh(d), dest, radius, walkNearTimeout)
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

// FreeSlots is the pack's empty slots.
func FreeSlots(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return vialplan.PackSlots
	}
	free := vialplan.PackSlots - len(inv.NonEmpty())
	if free < 0 {
		return 0
	}
	return free
}

// EmptyVials counts empty Vials in the pack.
func EmptyVials(d *Deps) int { return PackCountByName(d, vialplan.Vial) }

// Coins is the pack's own coins.
func Coins(d *Deps) int { return PackCountByName(d, vialplan.Coins) }

// packItemID finds the first pack slot's item id named name.
func packItemID(d *Deps, name string) (int32, bool) {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return 0, false
	}
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), name) {
			return it.ID, true
		}
	}
	return 0, false
}

func findNpcByName(d *Deps, name string, near tile.Tile, radius int, requireOp *string) (int, int, int, int, bool) {
	bestIdx, bestType := -1, -1
	bestDist := radius + 1
	found := false
	bestX, bestZ := 0, 0
	for _, n := range d.Session.State().NPCs() {
		npcType, ok := d.Config.Npc(n.Type)
		if !ok || !strings.EqualFold(npcType.Name, name) {
			continue
		}
		if requireOp != nil && cachecfg.OpIndex(npcType.Ops, *requireOp) == 0 {
			continue
		}
		dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if dist <= radius && dist < bestDist {
			bestIdx, bestType, bestDist, found = n.Index, n.Type, dist, true
			bestX, bestZ = n.X, n.Z
		}
	}
	return bestIdx, bestType, bestX, bestZ, found
}

// ---- fill ----

const fillTimeout = 4 * time.Second

// FillOnce uses one Vial on the fountain (OPLOCU) and waits for it to become a Vial of water.
func FillOnce(d *Deps) bool {
	here := currentTile(d)
	m, ok := d.Locs.Nearest(d.Config, locindex.Query{X: here.X, Z: here.Z, Level: here.Level, Radius: vialplan.FountainRadius, Name: vialplan.FountainName}, locindex.ChangesFrom(d.Session.State().LocChanges()))
	if !ok {
		d.logf("no %s in sight", vialplan.FountainName)
		return false
	}
	if tile.Chebyshev(here, tile.Tile{X: m.X, Z: m.Z, Level: m.Level}) > 1 {
		if !WalkNear(d, tile.Tile{X: m.X, Z: m.Z, Level: m.Level}, 1) {
			return false
		}
	}
	vialID, ok := packItemID(d, vialplan.Vial)
	if !ok {
		return false
	}
	before := PackCountByName(d, vialplan.VialOfWater)
	if err := useVial(d, vialID, m); err != nil {
		d.logf("UseOnLoc(vial on fountain): %v", err)
		return false
	}
	return waitFor(d, "a filled vial after using it on the fountain", fillTimeout, func() bool {
		return PackCountByName(d, vialplan.VialOfWater) > before
	})
}

// useVial uses the vial on the fountain through the click gate (actions.GatedClick): the same fountain is not clicked more than once
// per actions.ClickGapTicks game ticks, whatever calls this and however fast. FillLeg's own pace (a result wait and a tick between
// uses) never comes near that, and a chat in front of it ends the leg before a click is sent; the gate is what keeps a future
// caller that skips both from queueing clicks behind everything it sends next (docs/click_flood_audit_2026-10-15.md).
func useVial(d *Deps, vialID int32, m locindex.Match) error {
	eq := actions.NewEquipment(d.Session, d.Components)
	return actions.GatedClick(d.Session, actions.DelayWaiter(d.Delay), actions.LocKey(m.X, m.Z, m.ID), nil, func() error {
		return eq.UseOnLoc(vialID, m.X, m.Z, m.ID)
	})
}

// FillResult is one fill trip's outcome.
type FillResult struct {
	Filled int
	Reason string // "empty", "full", "dialog", "stopped"
}

const fillGuardMax = vialplan.PackSlots + 2

// FillLeg fills empty vials until none remain, the pack is full, or a dialog interrupts —
// ported from VialFiller.ts's own fillLeg, whose guard counter (inventory size + 2) prevents an
// infinite loop if a fill silently never lands.
func FillLeg(d *Deps) FillResult {
	dlg := actions.NewChatDialog(d.Session, d.Components)
	filled := 0
	for i := 0; i < fillGuardMax; i++ {
		if dlg.CanContinue() {
			return FillResult{Filled: filled, Reason: "dialog"}
		}
		if EmptyVials(d) == 0 {
			return FillResult{Filled: filled, Reason: "empty"}
		}
		if FreeSlots(d) == 0 && EmptyVials(d) == 0 {
			return FillResult{Filled: filled, Reason: "full"}
		}
		if !FillOnce(d) {
			return FillResult{Filled: filled, Reason: "stalled"}
		}
		filled++
		// engine protection: one tick between uses, matching VialFiller.ts's own pacing.
		if !d.Delay(600 * time.Millisecond) {
			return FillResult{Filled: filled, Reason: "stopped"}
		}
	}
	return FillResult{Filled: filled, Reason: "empty"}
}

// ---- the bank ----

func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: vialplan.BoothName, Op: vialplan.BoothOp}, live)
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

// OpenBank walks to bankTile and opens its booth.
func OpenBank(d *Deps, bankTile tile.Tile) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, bankTile, 3) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	bank, ok := openNearestBankBooth(d, x, z, level, 20*time.Second)
	if !ok {
		return nil, false
	}
	return bank, true
}

// DepositAllExceptCoins deposits every pack item but Coins — ported from VialFiller.ts's own
// bankLeg deposit step.
func DepositAllExceptCoins(d *Deps, bank *actions.Bank) int {
	var ids []int32
	seen := map[int32]bool{}
	for _, it := range bank.Inventory() {
		if seen[it.ID] {
			continue
		}
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || strings.EqualFold(strings.TrimSpace(obj.Name), vialplan.Coins) {
			continue
		}
		seen[it.ID] = true
		ids = append(ids, it.ID)
	}
	// One burst, five Deposit-Alls a tick, confirmed once (twopertick-1012).
	deposited, _, r, retried := bank.DepositKinds(ids, actions.DelayWaiter(d.Delay))
	if len(ids) > 0 {
		d.logf("%s", actions.VisitLog("vial deposit", len(ids), r, retried))
	}
	return len(deposited)
}

// BankCoins is the bank's own coin total at the last read.
func BankCoins(d *Deps, bank *actions.Bank) int {
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(strings.TrimSpace(obj.Name), vialplan.Coins) {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			return c
		}
	}
	return 0
}

// WithdrawEmptyVials withdraws n empty Vials (never notes) from an already-open bank.
func WithdrawEmptyVials(d *Deps, bank *actions.Bank, n int) bool {
	if n <= 0 {
		return true
	}
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !strings.EqualFold(strings.TrimSpace(obj.Name), vialplan.Vial) {
			continue
		}
		before := PackCountByName(d, vialplan.Vial)
		if err := bank.WithdrawX(it.ID, int32(n), waiter{d}); err != nil {
			d.logf("withdraw %d vials: %v", n, err)
			return false
		}
		return waitFor(d, "vials in pack after withdrawing", 4*time.Second, func() bool {
			return PackCountByName(d, vialplan.Vial) > before
		})
	}
	return false
}

// WithdrawCoins withdraws n coins from an already-open bank.
func WithdrawCoins(d *Deps, bank *actions.Bank, n int) bool {
	if n <= 0 {
		return true
	}
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(strings.TrimSpace(obj.Name), vialplan.Coins) {
			continue
		}
		before := Coins(d)
		if err := bank.WithdrawX(it.ID, int32(n), waiter{d}); err != nil {
			d.logf("withdraw %d coins: %v", n, err)
			return false
		}
		return waitFor(d, "coins in pack after withdrawing", 4*time.Second, func() bool {
			return Coins(d) > before
		})
	}
	return false
}

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

// ---- Jatix's shop ----

// BuyVialsFromJatix walks to Jatix, opens his shop, and buys n empty Vials.
func BuyVialsFromJatix(d *Deps, n int) bool {
	if n <= 0 {
		return true
	}
	if !WalkNear(d, vialplan.JatixStand, 4) {
		d.logf("could not reach Jatix's shop")
		return false
	}
	idx, typ, _, _, ok := findNpcByName(d, vialplan.Jatix, vialplan.JatixStand, 15, nil)
	if !ok {
		d.logf("could not find Jatix near his stand")
		return false
	}
	npcType, _ := d.Config.Npc(typ)
	tradeOp := cachecfg.OpIndex(npcType.Ops, "Trade")
	if tradeOp == 0 {
		tradeOp = 3
	}
	shop := actions.NewShop(d.Session)
	if err := actions.OpNpc(d.Session, tradeOp, idx); err != nil {
		d.logf("OpNpc(Trade) on Jatix: %v", err)
		return false
	}
	if !waitFor(d, "Jatix's shop to open", 5*time.Second, shop.IsOpen) {
		return false
	}
	defer shop.Close()
	var itemID int32 = -1
	for _, it := range shop.Stock() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && strings.EqualFold(obj.Name, vialplan.Vial) {
			itemID = it.ID
			break
		}
	}
	if itemID == -1 {
		d.logf("%q not found in Jatix's stock", vialplan.Vial)
		return false
	}
	before := PackCountByName(d, vialplan.Vial)
	if err := shop.Buy(itemID, int32(n)); err != nil {
		d.logf("Buy: %v", err)
		return false
	}
	return waitFor(d, "vials in pack after buying", 5*time.Second, func() bool {
		return PackCountByName(d, vialplan.Vial) > before
	})
}
