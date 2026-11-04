// Package fighterrunner is the I/O half of the AdvancedFighter port: it drives the pure
// stage/target/loot/bank decisions in internal/scripts/fighterplan against a live session,
// following the exact split internal/scripts/essencerunner and its siblings already proved
// (see docs/script_porting_framework.md). A pure plan package decides, this package does the
// walking/clicking/banking/fighting, and internal/bot/advancedfighter.go wires the two
// together the way internal/bot/essence.go wires essenceplan+essencerunner.
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// matching every sibling runner package, so internal/bot's Context.Delay can actually cancel
// a running fighter mid-wait.
//
// Deliberately NOT ported here (see fighterplan's own doc comment for the full list and why):
// the HUD/bank feed/tally, prayer, potions, ranged/mage, treasure trails, safespotting,
// teleport-flee.
package fighterrunner

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
	"rsb0t-go/internal/pathfind"
	"rsb0t-go/internal/randomevents"
	"rsb0t-go/internal/scripts/donorplan"
	"rsb0t-go/internal/scripts/fighterfood"
	"rsb0t-go/internal/scripts/fighterplan"
	"rsb0t-go/internal/scripts/freshstartrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Delayer sleeps for d, returning false early if the caller should stop instead of true once
// d has fully elapsed — the same shape every sibling runner package uses.
type Delayer func(d time.Duration) bool

// RealDelay always sleeps the full duration — for a standalone tool with nothing to cancel.
func RealDelay(d time.Duration) bool {
	time.Sleep(d)
	return true
}

// Deps is everything one fight/bank/loot cycle needs from the outside world.
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

// PlayerPack is the persistent inventory-tab component id — see essencerunner.PlayerPack's
// own doc comment for why this fixed, always-current component is read instead of scanning
// every known inventory.
const PlayerPack = 3214

func currentTile(d *Deps) (tile.Tile, bool) {
	x, z, level, ok := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}, ok
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

// emergencyFleeDriftTiles is 0, the same choice essencerunner/donorrunner make — a fighter
// has no single fixed "home" the way fisherrunner's Catherby camp does; the anchor moves with
// wherever the run was started or configured. EmergencyFleeCombatAware's drift check is
// disabled by driftTiles<=0; its ordinary "InCombat -> flee" branch is also disabled
// (unconditionally, by using the *CombatAware variant) since this script's own InCombat is
// its normal, desired operating state — see randomevents.EmergencyFleeCombatAware's own doc
// comment for the live evidence this was added for.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

const guardianWaitLogEvery = 10 * time.Second

// WaitForGuardian pauses while a random event is detected at all — mirrors every sibling
// runner's own WaitForGuardian exactly.
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFleeCombatAware(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	lastLog := time.Now()
	for randomevents.Busy(d.Session) {
		randomevents.EmergencyFleeCombatAware(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
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

// walkNearTimeout bounds WalkNear's entire operation — see fisherrunner.walkNearTimeout's own
// doc comment for the live evidence behind 120s rather than a shorter cap.
const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there, re-clicking from wherever the player stops,
// stopping once within radius tiles of dest. Duplicated per this project's established
// per-package convention rather than shared (see docs/script_porting_framework.md's "why
// duplicated" section).
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
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
			randomevents.EmergencyFleeCombatAware(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
			if !d.Delay(200 * time.Millisecond) {
				return false
			}
			cur, ok := currentTile(d)
			if !ok {
				continue
			}
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
	here, _ := currentTile(d)
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

// ---- skills ----
//
// Stat indices per gamestate/skills.go's own SkillNames table (the engine's PlayerStat
// enum, cross-checked live elsewhere in this repo — see that table's own doc comment):
// Attack=0, Defence=1, Strength=2, Hitpoints=3 (gamestate.HitpointsStat).
const (
	AttackStat   = 0
	DefenceStat  = 1
	StrengthStat = 2
)

// CombatSkillStats is every skill AdvancedFighter's xp counter totals — ported from
// fighterData.ts's COMBAT_SKILLS, minus Ranged/Magic/Prayer (not trained by this melee-only
// port).
var CombatSkillStats = []int{AttackStat, DefenceStat, StrengthStat, gamestate.HitpointsStat}

// SkillLevel reads a stat's current level, 0 if never seen.
func SkillLevel(d *Deps, stat int) int {
	sk, ok := d.Session.State().Skill(stat)
	if !ok {
		return 0
	}
	return sk.Level
}

// SkillXP reads a stat's current xp, 0 if never seen.
func SkillXP(d *Deps, stat int) int {
	sk, ok := d.Session.State().Skill(stat)
	if !ok {
		return 0
	}
	return sk.XP
}

// MeleeLevels reads the three progressive-ladder skills at once.
func MeleeLevels(d *Deps) fighterplan.MeleeLevels {
	return fighterplan.MeleeLevels{
		Attack:   SkillLevel(d, AttackStat),
		Strength: SkillLevel(d, StrengthStat),
		Defence:  SkillLevel(d, DefenceStat),
	}
}

// ---- health / status ----

// HPFraction is Skills.hpFraction() — current HP over max, 0 when unknown.
func HPFraction(d *Deps) float64 {
	hp, hpOK := d.Session.State().HP()
	maxHP, maxOK := d.Session.State().MaxHP()
	if !hpOK || !maxOK || maxHP <= 0 {
		return 0
	}
	return float64(hp) / float64(maxHP)
}

// ---- food ----

// DetectFood is the first edible thing in the pack, for a run given no food setting —
// ported from bot.detectFood(): the first item whose config offers an "Eat" option.
func DetectFood(d *Deps) string {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return ""
	}
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		for _, op := range obj.IOps {
			if strings.EqualFold(op, "Eat") {
				return obj.Name
			}
		}
	}
	return ""
}

// FoodCount counts every pack slot holding any form of foodName.
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

// EatOnce eats one form of foodName, confirmed against a real HP rise — ported from
// AdvancedFighter.ts's own eatOnce().
func EatOnce(d *Deps, foodName string) bool {
	slot := -1
	var objID int32
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return false
	}
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
	ate := waitFor(d, "hp to rise after eating", 3*time.Second, func() bool {
		hp, ok := d.Session.State().HP()
		return ok && (hp > before || FoodCount(d, foodName) == 0)
	})
	hp, ok := d.Session.State().HP()
	if ate && ok && hp > before {
		d.logf("ate a %s (%d -> %d hp)", foodName, before, hp)
		return true
	}
	return false
}

// ---- bones ----

// BuryOne buries the first bone-family item in the pack — ported from bot.buryOne().
func BuryOne(d *Deps, bones []string) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return false
	}
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !fighterplan.IsBone(obj.Name, bones) {
			continue
		}
		before := len(inv.NonEmpty())
		if err := actions.Bury(d.Session, it.ID, it.Slot, PlayerPack); err != nil {
			d.logf("bury: %v", err)
			return false
		}
		return waitFor(d, "bones buried", 3*time.Second, func() bool {
			cur := d.Session.State().Inventory(PlayerPack)
			return cur == nil || len(cur.NonEmpty()) < before
		})
	}
	return false
}

// ---- targets ----

// SelfSlot is the local player's own slot — needed to read TargetsPlayer off a live NPC.
func SelfSlot(d *Deps) int { return d.Session.State().Self() }

// TargetNPCs returns every currently-visible NPC offering "Attack", optionally filtered to
// names (case-insensitive, empty means anything attackable) — ported from targetNpcs().
func TargetNPCs(d *Deps, names []string) []gamestate.NPC {
	var out []gamestate.NPC
	for _, n := range d.Session.State().NPCs() {
		t, ok := d.Config.Npc(n.Type)
		if !ok || cachecfg.OpIndex(t.Ops, "Attack") == 0 {
			continue
		}
		if len(names) > 0 && !nameMatches(t.Name, names) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func nameMatches(name string, names []string) bool {
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// TrackNPC finds a still-visible NPC by index.
func TrackNPC(d *Deps, index int) (gamestate.NPC, bool) {
	for _, n := range d.Session.State().NPCs() {
		if n.Index == index {
			return n, true
		}
	}
	return gamestate.NPC{}, false
}

// ToTargetRecord converts a live NPC into fighterplan's pure TargetRecord — ported from
// bot.targetRecord().
func ToTargetRecord(d *Deps, n gamestate.NPC, here tile.Tile) fighterplan.TargetRecord {
	selfSlot := SelfSlot(d)
	return fighterplan.TargetRecord{
		Index:        n.Index,
		Name:         npcName(d, n),
		Tile:         tile.Tile{X: n.X, Z: n.Z, Level: n.Level},
		Distance:     tile.Chebyshev(here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}),
		InCombat:     n.InCombat,
		Health:       n.Health,
		TotalHealth:  n.TotalHealth,
		TargetsMe:    n.TargetsPlayer(selfSlot),
		TargetsOther: n.TargetsAnotherPlayer(selfSlot),
	}
}

func npcName(d *Deps, n gamestate.NPC) string {
	if t, ok := d.Config.Npc(n.Type); ok {
		return t.Name
	}
	return ""
}

// ApproachRadius bounds how far out of its way a fight will walk to reach a target, and so how
// far the reachability check below looks for a route.
//
// It is the same 24 combatrunner.ApproachRadius uses, for the same reason: generous against a
// fighter's own leash (2 to 40, default 10) and mean against the map, since a monster only
// chases as far as its own maxrange from its spawn tile and a route that goes twenty tiles round
// a building to reach something four tiles away loses the fight it walked to.
const ApproachRadius = 24

// Reachable answers the question a distance comparison cannot: whether there is a route from
// where the account is standing to target, rather than only a short straight line to it.
//
// It fails OPEN — no pack, or no known tile yet, answers "reachable" with the reason — because
// the two mistakes do not cost the same. A false "unreachable" makes a run refuse a monster it
// could have killed; the check exists to stop a run grinding forever on one it can never reach,
// and a run with no collision data has no business overruling the server.
func Reachable(d *Deps, target tile.Tile) (bool, string) {
	if d == nil || d.Pack == nil {
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

// ReachableRecord is Reachable as fighterplan.TargetContext wants it, so a caller wires the
// pathfinder into the pure picker with one field rather than repeating this closure.
func ReachableRecord(d *Deps) func(fighterplan.TargetRecord) bool {
	return func(t fighterplan.TargetRecord) bool {
		ok, _ := Reachable(d, t.Tile)
		return ok
	}
}

// AttackNPC clicks Attack on npc.
func AttackNPC(d *Deps, npc gamestate.NPC) bool {
	t, ok := d.Config.Npc(npc.Type)
	if !ok {
		return false
	}
	op := cachecfg.OpIndex(t.Ops, "Attack")
	if op == 0 {
		return false
	}
	if err := actions.OpNpc(d.Session, op, npc.Index); err != nil {
		d.logf("OpNpc(Attack): %v", err)
		return false
	}
	return true
}

// PlayerThreat reports whether a real player is attacking us — ported from playerThreat(),
// simplified to AttackedByPlayer (auto-retaliate turning our own face toward an attacker),
// which is the case that matters for a bot that never starts PvP itself; a would-be
// attacker who hasn't yet landed a hit (and so hasn't turned our face) is not detected here,
// unlike the TS source's extra Players.query().where(playerTargetsMe) scan.
func PlayerThreat(d *Deps) bool {
	return d.Session.State().AttackedByPlayer()
}

// ---- loot ----

// FindLoot returns the nearest ground item matching the loot filter within radius of anchor,
// skipping known kill-drop bones outside their window — ported from findLoot(), minus the
// per-drop retry-skip map (a caller-owned concern; see fighterplan.KillSite/BoneFromKill for
// the pure decision this still uses).
func FindLoot(d *Deps, filter fighterplan.LootFilter, anchor tile.Tile, radius int, kills []fighterplan.KillSite, nowMs int64, windowMs int64) (gamestate.GroundItem, bool) {
	best := gamestate.GroundItem{}
	bestDist := radius + 1
	found := false
	here, hereOK := currentTile(d)
	if !hereOK {
		here = anchor
	}
	for _, g := range d.Session.State().GroundItems() {
		obj, ok := d.Config.Obj(g.ID)
		if !ok {
			continue
		}
		if !fighterplan.WantsLoot(obj.Name, filter) {
			continue
		}
		gt := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if gt.Level != anchor.Level || tile.Chebyshev(gt, anchor) > radius {
			continue
		}
		if fighterplan.IsBone(obj.Name, filter.Bones) && !fighterplan.BoneFromKill(gt, kills, nowMs, windowMs) {
			continue
		}
		dist := tile.Chebyshev(here, gt)
		if dist < bestDist {
			best, bestDist, found = g, dist, true
		}
	}
	return best, found
}

// LootOnce picks up drop with its Take option, confirmed against a pack change.
func LootOnce(d *Deps, drop gamestate.GroundItem) bool {
	obj, ok := d.Config.Obj(drop.ID)
	name := ""
	if ok {
		name = obj.Name
	}
	before := len(inventoryOrEmpty(d))
	beforeCount := countByID(d, int32(drop.ID))
	if err := actions.TakeGroundItem(d.Session, d.Config, drop); err != nil {
		d.logf("take %s: %v", name, err)
		return false
	}
	got := waitFor(d, "loot picked up", 3*time.Second, func() bool {
		return len(inventoryOrEmpty(d)) > before || countByID(d, int32(drop.ID)) > beforeCount
	})
	if got {
		d.logf("looted %s", name)
	}
	return got
}

func inventoryOrEmpty(d *Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

func countByID(d *Deps, id int32) int32 {
	return actions.CountByID(inventoryOrEmpty(d), id)
}

// PackFull reports whether the 28-slot pack is full.
func PackFull(d *Deps) bool { return actions.IsFull(inventoryOrEmpty(d)) }

// LootSlots counts pack items that would be deposited as loot (not kept for the fight) —
// ported from lootSlots().
func LootSlots(d *Deps, filter fighterplan.LootFilter, keep fighterplan.KeepInput) int {
	n := 0
	for _, it := range inventoryOrEmpty(d) {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok {
			continue
		}
		if fighterplan.WantsLoot(obj.Name, filter) && !fighterplan.ShouldKeep(obj.Name, keep, fighterfood.IsFoodItem) {
			n++
		}
	}
	return n
}

// ---- gear / style ----

// ContainersReadable reports whether the backpack and worn panels resolve to real components
// right now, so a caller can tell "this account is holding nothing" apart from "this client
// cannot see what it is holding". Same shape and same reason as combatrunner.ContainersReadable.
//
// Content takes those two sidebar tabs away in several ordinary situations, not just one exotic
// event: ~macro_events_set_tabs (macro_events.rs2:218) is if_settab(null, ...) for tabs 0-6, and
// so are ~set_sail (interface_boat/scripts/sail.rs2) and ~set_sail_cairn/captain_shanks. How long
// they stay away differs by a lot: a mime holds them until its mapzoneexit, while a crossing
// restores them at ~initalltabs before the arrival telejump, so that window is the route's own
// p_delay and not the voyage. Short is not the same as absorbed, and that is the part worth
// keeping: a WaitForGuardian return was measured with each window still open. Ask this before
// treating an empty HeldNames as evidence about the account rather than about the connection.
func ContainersReadable(d *Deps) bool {
	return actions.NewEquipment(d.Session, d.Components).Readable()
}

// HeldNames is every named item the account currently wears or carries.
//
// The pack half reads PlayerPack rather than Equipment.Inventory(), and that is not a style
// preference: Equipment resolves the backpack through sidebar tab 3, which content nulls during
// a macro event or a ship crossing (see ContainersReadable for how far apart the two windows
// are), and a nulled tab makes
// Inventory() return nil - not "the pack is empty" but "this client cannot see the pack". The
// fixed component keeps working throughout because Player.setTab (engine's
// src/engine/entity/Player.ts:2121) only reassigns this.tabs[tab] and writes IF_SETTAB; it never
// calls invStopListenOnCom, so the inv_transmit(inv, inventory:inv) listener ~initalltabs
// registered stays registered and UPDATE_INV keeps flowing to 3214.
//
// Equipment.Inventory() used to return nil in that state and now falls back to the same
// component (see actions.PlayerPack), so the two agree.
//
// The WORN half is no longer the weak one: Equipment.Worn() reads the fixed PlayerWorn component
// when tab 4 is nulled, so this function does not report "wearing nothing" for an account it
// merely cannot see through its own sidebar.
//
// ContainersReadable is still worth asking first, and internal/bot/advancedfighter.go's
// trackedGear guard deliberately stays — not because either read lies any more, but because the
// seed it protects is taken ONCE at OnStart and never re-read, and at that moment the account may
// simply not have been set up yet. Before ~initalltabs has run and the first UPDATE_INV has
// arrived, every read is empty whichever component it names, and an empty reading there is still
// indistinguishable from an empty account. Readable() is the tell that the sidebar exists at all,
// so it is the right thing to wait on in the one place where a bad reading is permanent.
func HeldNames(d *Deps) []string {
	eq := actions.NewEquipment(d.Session, d.Components)
	var out []string
	for _, it := range eq.Worn() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	for _, it := range inventoryOrEmpty(d) {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && obj.Name != "" {
			out = append(out, obj.Name)
		}
	}
	return out
}

// EnsureGear equips any of gear currently in the pack but not worn — ported from EnsureGear.
// Returns how many items were equipped.
func EnsureGear(d *Deps, gear []string) int {
	eq := actions.NewEquipment(d.Session, d.Components)
	// Every piece goes on in one burst, five clicks a tick, confirmed once (twopertick-1012).
	var ids []int32
	named := map[int32]string{}
	for _, name := range gear {
		if eq.ContainsWorn(d.Config, name) {
			continue
		}
		item, ok := actions.FindByName(eq.Inventory(), d.Config, name)
		if !ok {
			continue
		}
		ids = append(ids, item.ID)
		named[item.ID] = name
	}
	if len(ids) == 0 {
		return 0
	}
	r := eq.EquipMany(d.Config, ids, Waiter(d))
	d.logf("%s", r.Log("equipped"))
	equipped := 0
	for _, id := range ids {
		if eq.IsWorn(id) {
			d.logf("equipped %s", named[id])
			equipped++
		}
	}
	return equipped
}

// GearMissing reports whether any of gear is held in the pack but not worn — the check
// EnsureGear's own caller (DecideStage) runs before deciding to spend a Loop call on it.
func GearMissing(d *Deps, gear []string) bool {
	eq := actions.NewEquipment(d.Session, d.Components)
	for _, name := range gear {
		if eq.ContainsWorn(d.Config, name) {
			continue
		}
		if _, ok := actions.FindByName(eq.Inventory(), d.Config, name); ok {
			return true
		}
	}
	return false
}

// EnsureRetaliate sets auto retaliate to want, if it isn't already — ported from
// EnsureRetaliate.
func EnsureRetaliate(d *Deps, want bool) bool {
	if d.Session.State().AutoRetaliateOn() == want {
		return true
	}
	if err := actions.SetAutoRetaliate(d.Session, want); err != nil {
		d.logf("set auto retaliate: %v", err)
		return false
	}
	if waitFor(d, "auto retaliate set", 2*time.Second, func() bool { return d.Session.State().AutoRetaliateOn() == want }) {
		d.logf("auto retaliate %v", want)
		return true
	}
	return false
}

// EnsureMeleeStyle sets the weapon's combat style to want, if it isn't already — ported from
// SetAttackStyle (melee-only; ranged/mage combat-mode is not ported, see the package doc
// comment).
func EnsureMeleeStyle(d *Deps, want fighterplan.MeleeStyle) bool {
	style, ok := actions.ParseCombatStyle(string(want))
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
	if waitFor(d, "combat style set", 2*time.Second, func() bool { return actions.HasCombatStyle(d.Session, style) }) {
		d.logf("combat style: %s", want)
		return true
	}
	return false
}

// StyleWrong reports whether either auto retaliate or the melee style disagrees with wanted
// — the read DecideStage's snapshot uses before spending a Loop call fixing it.
func StyleWrong(d *Deps, wantRetaliate bool, wantStyle fighterplan.MeleeStyle) bool {
	if d.Session.State().AutoRetaliateOn() != wantRetaliate {
		return true
	}
	style, ok := actions.ParseCombatStyle(string(wantStyle))
	if !ok {
		return false
	}
	return !actions.HasCombatStyle(d.Session, style)
}

// ---- the bank ----

// openNearestBankBooth finds every "Bank booth" candidate near (x,z,level) and tries them
// nearest-first — duplicated per this project's established per-package convention (see
// essencerunner.openNearestBankBooth's own doc comment for the live bake-artifact bug this
// nearest-first retry recovers from).
func openNearestBankBooth(d *Deps, x, z, level int, timeout time.Duration) (*actions.Bank, bool) {
	live := locindex.ChangesFrom(d.Session.State().LocChanges())
	candidates := d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth", Op: "Use-quickly"}, live)
	if len(candidates) == 0 {
		candidates = d.Locs.Find(d.Config, locindex.Query{X: x, Z: z, Level: level, Radius: 15, Name: "Bank booth"}, live)
	}
	if len(candidates) == 0 {
		d.logf("could not find a bank booth nearby")
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
		op := booth.Op
		if op == 0 {
			op = 1
		}
		mark := actions.MarkBankOpen(d.Session)
		if err := actions.OpLoc(d.Session, op, booth.X, booth.Z, booth.ID); err != nil {
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

// OpenBankNear walks to within radius of stand and opens the nearest bank booth.
func OpenBankNear(d *Deps, stand tile.Tile, radius int) (*actions.Bank, bool) {
	if d.Components == nil {
		d.logf("no interface archive Components configured — cannot open the bank")
		return nil, false
	}
	if !WalkNear(d, stand, radius) {
		return nil, false
	}
	x, z, level, _ := d.Session.State().Tile()
	return openNearestBankBooth(d, x, z, level, 20*time.Second)
}

// DepositExceptKept deposits every pack item ShouldKeep says no to.
func DepositExceptKept(d *Deps, bank *actions.Bank, keep fighterplan.KeepInput) {
	_ = bank.BatchDepositAllMatching(func(it gamestate.SlotItem) bool {
		obj, ok := d.Config.Obj(int(it.ID))
		return ok && !fighterplan.ShouldKeep(obj.Name, keep, fighterfood.IsFoodItem)
	}, fighterWaiter{d})
}

type fighterWaiter struct{ d *Deps }

func (w fighterWaiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w fighterWaiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	return waitFor(w.d, "bank confirmation", timeout, cond)
}
func (w fighterWaiter) DelayTicks(n int) bool {
	return w.d.Delay(time.Duration(n) * 600 * time.Millisecond)
}

// Waiter is d as an actions.Waiter.
func Waiter(d *Deps) actions.Waiter { return fighterWaiter{d} }

// ---- mule hand-off ----

// itemRows totals items by (name, id), preserving first-seen order — shared shape every
// donor script's own BankItemRows uses (see essencerunner.itemRows's own twin).
func itemRows(d *Deps, items []gamestate.SlotItem) []donorplan.ItemRow {
	type key struct {
		name string
		id   int
	}
	totals := map[key]int{}
	var order []key
	for _, it := range items {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		k := key{name: obj.Name, id: int(it.ID)}
		if _, seen := totals[k]; !seen {
			order = append(order, k)
		}
		c := it.Count
		if c < 1 {
			c = 1
		}
		totals[k] += int(c)
	}
	out := make([]donorplan.ItemRow, 0, len(order))
	for _, k := range order {
		out = append(out, donorplan.ItemRow{Name: k.name, ID: k.id, Count: totals[k]})
	}
	return out
}

// BankItemRows reads an already-open bank's contents as donorplan.ItemRow.
func BankItemRows(d *Deps, bank *actions.Bank) []donorplan.ItemRow {
	return itemRows(d, bank.Items())
}

// PackSlotRows reads the player's own pack one row per slot (not merged by name) — what
// MuleHaul/MuleLoad need, matching essencerunner.PackSlotRows' own doc comment on why a
// merged stack of unstackable items must not be counted as a single mule slot.
func PackSlotRows(d *Deps) []donorplan.ItemRow {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil {
		return nil
	}
	var out []donorplan.ItemRow
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Name == "" {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out = append(out, donorplan.ItemRow{Name: obj.Name, ID: int(it.ID), Count: c})
	}
	return out
}

// SetNoteMode sets the open bank's withdraw mode.
func SetNoteMode(d *Deps, bank *actions.Bank, on bool) error {
	return bank.SetNoteMode(on, Waiter(d))
}

// FreeSlots is the pack's empty slots.
func FreeSlotsInPack(d *Deps) int {
	return actions.FreeSlots(inventoryOrEmpty(d))
}

// WithdrawAllByName withdraws every one of name with Withdraw-All and waits until the pack
// holds more of that name — counts by name (not id) so it confirms a withdrawal that lands
// as a note (a different id) in note mode.
func WithdrawAllByName(d *Deps, bank *actions.Bank, name string) bool {
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || !strings.EqualFold(obj.Name, name) {
			continue
		}
		before := HeldCount(d, name)
		used := len(inventoryOrEmpty(d))
		if err := bank.WithdrawAll(it.ID); err != nil {
			d.logf("withdraw all %q: %v", name, err)
			return false
		}
		return waitFor(d, "withdrew "+name, 3*time.Second, func() bool {
			return HeldCount(d, name) > before || len(inventoryOrEmpty(d)) > used
		})
	}
	return false
}

// HeldCount is how many of name are already in the pack or worn — the `held` function
// RestockPlan needs.
func HeldCount(d *Deps, name string) int {
	n := int(actions.CountByName(inventoryOrEmpty(d), d.Config, name))
	eq := actions.NewEquipment(d.Session, d.Components)
	n += int(actions.CountByName(eq.Worn(), d.Config, name))
	return n
}

// BankCount is how many of name an already-open bank holds.
func BankCount(d *Deps, bank *actions.Bank, name string) int {
	return int(actions.CountByName(bank.Items(), d.Config, name))
}

// WithdrawTo withdraws name up to target held count (pack+worn), matching
// AdvancedFighter.ts's own withdrawTo(): repeated Withdraw-X/Withdraw-5 calls, confirmed each
// time, bounded so a bank that never obliges can't loop forever.
func WithdrawTo(d *Deps, bank *actions.Bank, name string, target int) int {
	start := HeldCount(d, name)
	for guard := 0; guard < 40 && HeldCount(d, name) < target && !PackFull(d); guard++ {
		before := HeldCount(d, name)
		need := target - before
		item, ok := actions.FindByName(bank.Items(), d.Config, name)
		if !ok {
			break
		}
		var err error
		switch {
		case need > 5:
			err = bank.WithdrawX(item.ID, int32(need), Waiter(d))
		case need > 1:
			err = bank.WithdrawFive(item.ID)
		default:
			err = bank.WithdrawOne(item.ID)
		}
		if err != nil {
			break
		}
		if !waitFor(d, "withdrew "+name, 2500*time.Millisecond, func() bool { return HeldCount(d, name) > before }) {
			break
		}
	}
	return HeldCount(d, name) - start
}
