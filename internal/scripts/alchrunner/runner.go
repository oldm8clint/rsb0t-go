// Package alchrunner is the I/O half of the Alcher port: it drives the pure spell/restock
// decisions in internal/scripts/alchplan against a live session, following the same split
// every sibling runner package uses. See docs/script_porting_framework.md for the recipe.
//
// The one genuinely new primitive this port needed: casting a spell on a backpack item
// (High/Low Level Alchemy). actions.CastOnItem (OPHELDT) and the alch spell components
// (1162/1178) were added to internal/actions/magic.go alongside this package — see that
// file's own doc comment on itemSpells for how the component ids were confirmed against
// content/scripts/skill_magic/interfaces/magic.if.
//
// Deliberately NOT ported here: the HUD/bank feed/tally/update-checker — no headless
// equivalent, same boundary drawn around every other ported script.
package alchrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/locindex"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/alchplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// MagicStat is UPDATE_STAT's index for Magic, per the same PlayerStat enum
// boneburierrunner.PrayerStat/essencerunner.MiningStat cite: attack,defence,strength,
// hitpoints,ranged,prayer,magic,... — magic is index 6.
const MagicStat = 6

const PlayerPack = 3214
const PackSlots = 28

const (
	BoothName = "Bank booth"
	BoothOp   = "Use-quickly"
)

type Delayer func(d time.Duration) bool

func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

type Deps struct {
	Session    actions.Session
	Pack       *pathfind.Pack
	Locs       *locindex.Index
	Config     *cachecfg.Config
	Components archive.Components
	Chat       *chatlog.Log
	Delay      Delayer
	Log        func(string)
	// Loot receives every landed cast for the Loot panel's Magic card (the item alched, the coins
	// it paid, and whether a fire staff paid the Fire runes: loot.Alchemy); nil records nothing.
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

func MagicLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(MagicStat)
	if !ok {
		return 0
	}
	return skill.Level
}

func MagicXP(d *Deps) int {
	skill, ok := d.Session.State().Skill(MagicStat)
	if !ok {
		return 0
	}
	return skill.XP
}

func packItems(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

func PackCountByName(d *Deps, name string) int {
	n := 0
	for _, it := range packItems(d) {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name == name {
			c := int(it.Count)
			if c < 1 {
				c = 1
			}
			n += c
		}
	}
	return n
}

// CastsAffordableFor is how many casts of spell the pack's own nature/fire runes fund right
// now, live.
func CastsAffordableFor(d *Deps, spell alchplan.AlchSpell, hasStaff bool) int {
	return alchplan.CastsAffordable(spell, PackCountByName(d, alchplan.NatureRune), PackCountByName(d, alchplan.FireRune), hasStaff)
}

// HasFireStaff reports whether a known fire staff is currently worn.
func HasFireStaff(d *Deps) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && alchplan.IsFireStaff(obj.Name) {
			return true
		}
	}
	return false
}

// ---- the bank ----

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

// FetchFireStaff looks in an already-open bank for the best-listed fire staff and wears it —
// ported from fetchStaff(). ok is false only on a real failure (withdraw/equip refused); a
// bank with no staff at all reports (found=false, ok=true) so the caller knows to fall back
// to fire runes without retrying forever.
func FetchFireStaff(d *Deps, bank *actions.Bank) (staffName string, found bool, ok bool) {
	for _, name := range alchplan.FireStaffs {
		if bankCount(bank, d, name) > 0 {
			staffName = name
			found = true
			break
		}
	}
	if !found {
		return "", false, true
	}
	if PackCountByName(d, staffName) == 0 {
		if len(packItems(d)) >= PackSlots {
			if err := bank.BatchDepositInventory(Waiter(d)); err != nil {
				d.logf("deposit inventory: %v", err)
				return staffName, true, false
			}
		}
		id, idOK := bankItemID(bank, d, staffName)
		if !idOK {
			return staffName, true, false
		}
		if err := bank.WithdrawOne(id); err != nil {
			d.logf("withdraw %s: %v", staffName, err)
			return staffName, true, false
		}
		if !waitFor(d, staffName+" in pack", 3*time.Second, func() bool { return PackCountByName(d, staffName) > 0 }) {
			return staffName, true, false
		}
	}
	_ = bank.Close()
	eq := actions.NewEquipment(d.Session, d.Components)
	id, idOK := packItemID(d, staffName)
	if !idOK {
		return staffName, true, false
	}
	if err := eq.EquipItemAndWait(d.Config, id, Waiter(d)); err != nil {
		d.logf("equip %s: %v", staffName, err)
		return staffName, true, false
	}
	return staffName, true, waitFor(d, staffName+" worn", 3*time.Second, func() bool { return HasFireStaff(d) })
}

func bankCount(bank *actions.Bank, d *Deps, name string) int {
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name == name {
			return int(it.Count)
		}
	}
	return 0
}

func bankItemID(bank *actions.Bank, d *Deps, name string) (int32, bool) {
	for _, it := range bank.Items() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name == name {
			return it.ID, true
		}
	}
	return 0, false
}

func packItemID(d *Deps, name string) (int32, bool) {
	for _, it := range packItems(d) {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name == name {
			return it.ID, true
		}
	}
	return 0, false
}

// Restock deposits everything not in keep, then draws one trip's worth of the current alch
// item (as notes) and the runes it needs — ported from restock(). Returns the item name drawn
// (or the one already held) and whether the trip produced anything to alch with.
func Restock(d *Deps, bank *actions.Bank, items []string, spell alchplan.AlchSpell, castsPerTrip int, hasStaff bool) (itemName string, ok bool) {
	// The items only against Nature runes (alchplan.ItemsForNatures): with none in the pack or the bank they go into the
	// bank with the rest and none is drawn. That is reported as a restock that completed with nothing to alch with
	// (ok, no item name), so the caller's own runes check ends the run ("no runes left to cast with").
	stock := alchplan.ItemsForNatures(items, PackCountByName(d, alchplan.NatureRune), bankCount(bank, d, alchplan.NatureRune))
	keep := map[string]bool{alchplan.NatureRune: true, alchplan.FireRune: true}
	for _, it := range stock {
		keep[it] = true
	}
	if err := bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool {
		obj, cok := d.Config.Obj(int(it.ID))
		return !cok || !keep[obj.Name]
	}, Waiter(d)); err != nil {
		d.logf("deposit: %v", err)
	}
	if len(stock) == 0 && len(items) > 0 {
		d.logf("restock: no Nature runes in the pack or the bank, so no item is drawn to alch")
		return "", true
	}
	if err := SetNoteMode(d, bank, true); err != nil {
		d.logf("set note mode: %v", err)
	}

	current, haveItem := alchplan.NextItem(items, func(n string) int { return PackCountByName(d, n) })
	if !haveItem {
		pick, count, pok := alchplan.ItemToWithdraw(items, func(n string) int { return bankCount(bank, d, n) }, castsPerTrip)
		if !pok {
			return "", false
		}
		id, idOK := bankItemID(bank, d, pick)
		if !idOK {
			return "", false
		}
		before := PackCountByName(d, pick)
		if err := bank.WithdrawX(id, int32(count), Waiter(d)); err != nil {
			d.logf("withdraw %s: %v", pick, err)
			return "", false
		}
		if !waitFor(d, pick+" in pack", 3*time.Second, func() bool { return PackCountByName(d, pick) > before }) {
			return "", false
		}
		current = pick
	}

	heldNature, heldFire := PackCountByName(d, alchplan.NatureRune), PackCountByName(d, alchplan.FireRune)
	needNature, needFire := alchplan.RunesToWithdraw(spell, castsPerTrip, heldNature, heldFire, hasStaff)
	if needNature > 0 {
		if !withdrawRunes(d, bank, alchplan.NatureRune, needNature) {
			return current, false
		}
	}
	if needFire > 0 {
		if !withdrawRunes(d, bank, alchplan.FireRune, needFire) {
			return current, false
		}
	}
	return current, true
}

func withdrawRunes(d *Deps, bank *actions.Bank, name string, count int) bool {
	banked := bankCount(bank, d, name)
	if banked <= 0 {
		return true
	}
	if count > banked {
		count = banked
	}
	id, ok := bankItemID(bank, d, name)
	if !ok {
		return true
	}
	before := PackCountByName(d, name)
	if err := bank.WithdrawX(id, int32(count), Waiter(d)); err != nil {
		d.logf("withdraw %ss: %v", name, err)
		return false
	}
	return waitFor(d, name+"s in pack", 4*time.Second, func() bool { return PackCountByName(d, name) > before })
}

// ---- casting ----

// CastOutcome mirrors alchCaster.ts's CastOutcome.
type CastOutcome string

const (
	Cast        CastOutcome = "cast"
	Refused     CastOutcome = "refused"
	NoXP        CastOutcome = "no_xp"
	NotEnough   CastOutcome = "not_enough"
	LevelTooLow CastOutcome = "level_too_low"
	NoItem      CastOutcome = "no_item"
)

// CastOnce casts spell on the first pack slot named itemName, waits for the xp (or a
// disqualifying chat line), then sits out the spell's lock — ported from AlchCaster.castOnce().
//
// alchCaster.ts's own openTab() (Game.openSideTab(MAGIC_TAB)) is deliberately not called
// first here: its own doc comment already says casting "does not need the magic tab shown,
// only makes a dropped click rarer" (a client-side nicety, not a server requirement), and the
// server-side handler itself confirms this — content/scripts/skill_magic/scripts/spells/
// alchemy.rs2's magic_spell_high_alch/magic_spell_low_alch both open with
// `if_settabactive(^tab_magic); if_close;` as their own first action, unconditionally
// switching the active tab themselves rather than checking it was already switched. This
// differs from magic.go's own CastOnNpc, whose doc comment notes the engine does require the
// spell component visible for a *targeted* (TGT_BUTTON-then-TGT_NPC) cast; alchemy's own
// script shows no such gate. No Go primitive to switch the client's own displayed sidebar tab
// exists in this codebase yet regardless (no prior port has needed one) — if a future script
// needs the visual nicety (or a real server-side visibility gate is found live on some other
// spell), that is real new-primitive work, not something this port silently assumed away.
func CastOnce(d *Deps, spell alchplan.AlchSpell, itemName string) CastOutcome {
	var item gamestate.SlotItem
	found := false
	for _, it := range packItems(d) {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name == itemName {
			item, found = it, true
			break
		}
	}
	if !found {
		return NoItem
	}
	xpBefore := MagicXP(d)
	coinsBefore := packCoins(d)
	staff := HasFireStaff(d)
	clickAt := time.Now()
	if err := actions.CastOnItem(d.Session, spell.Name, item.ID, item.Slot, PlayerPack); err != nil {
		d.logf("cast %s on %s: %v", spell.Name, itemName, err)
		return Refused
	}
	timeout := time.Duration(alchplan.CastTimeoutTicks)*600*time.Millisecond + 500*time.Millisecond
	dlg := actions.NewChatDialog(d.Session, d.Components)
	landed := waitFor(d, "the cast to land", timeout, func() bool {
		if MagicXP(d) > xpBefore || dlg.CanContinue() {
			return true
		}
		text := d.Chat.Since(clickAt)
		return alchplan.IsNotEnoughRunesMessage(text) || alchplan.IsLevelTooLowMessage(text)
	})
	text := d.Chat.Since(clickAt)
	if alchplan.IsLevelTooLowMessage(text) {
		return LevelTooLow
	}
	if alchplan.IsNotEnoughRunesMessage(text) {
		return NotEnough
	}
	if !landed || MagicXP(d) <= xpBefore {
		return NoXP
	}
	remaining := time.Duration(spell.LockTicks)*600*time.Millisecond - time.Since(clickAt)
	if remaining > 0 {
		d.Delay(remaining)
	}
	// The cast landed: report it, the coins measured from the pack (the remaining lock above has let
	// them arrive).
	loot.Made(d.Loot, d.Config, loot.Alchemy(int(item.ID), itemName, spell.Name == alchplan.HighAlch.Name, staff), 1,
		loot.Product(alchCoinsID, "Coins", packCoins(d)-coinsBefore))
	return Cast
}

// alchCoinsID is the coin obj an alchemy pays (995; 617 is a members obj that shares the name).
const alchCoinsID = 995

// packCoins is the coins in the pack, by id.
func packCoins(d *Deps) int {
	n := 0
	for _, it := range packItems(d) {
		if it.ID == alchCoinsID {
			n += int(it.Count)
		}
	}
	return n
}
