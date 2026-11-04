// Package flinchrunner is the I/O half of the MeleeFlincher port: it drives the pure
// settle/reset/stage decisions in internal/scripts/flinchplan (plus target picking, shared
// from internal/scripts/fighterplan) against a live session, following the same split every
// sibling runner package uses (see docs/script_porting_framework.md).
//
// Every blocking wait goes through a caller-supplied Delayer instead of time.Sleep directly,
// matching every sibling runner package, so internal/bot's Context.Delay can actually cancel
// a running flincher mid-wait.
//
// Deliberately NOT ported here (see flinchplan's own doc comment for the full list and why):
// the HUD, prayer flicking.
package flinchrunner

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
	"rsb0t-go/internal/scripts/fighterplan"
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

// Deps is everything one flinch cycle needs from the outside world.
type Deps struct {
	Session actions.Session
	Pack    *pathfind.Pack
	Locs    *locindex.Index
	Config  *cachecfg.Config
	Chat    *chatlog.Log
	Delay   Delayer
	// Log receives one already-formatted line per notable event; nil drops them.
	Log func(string)
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// PlayerPack is the persistent inventory-tab component id — see fighterrunner.PlayerPack's
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
		if !d.Delay(50 * time.Millisecond) {
			return false
		}
	}
	d.logf("timed out waiting for %s", what)
	return false
}

// emergencyFleeDriftTiles is 0 — the flincher's safe tile is a configured setting, not a
// fixed reference this package tracks separately (the bot adapter owns it); matching every
// sibling runner's own choice when it has no single fixed "home" of its own.
const emergencyFleeDriftTiles = 0

var emergencyFleeHome pathfind.NavPoint

// WaitForGuardian pauses while a random event is detected at all. Uses
// EmergencyFleeCombatAware, not EmergencyFlee — this script's own InCombat is its ordinary
// operating state (see randomevents.Guardian.CombatExpected's own doc comment for the live
// evidence this matters: an unmodified EmergencyFlee fled a real fighter away from every
// single fight within a couple of poll ticks).
func WaitForGuardian(d *Deps) bool {
	randomevents.EmergencyFleeCombatAware(d.Session, d.Pack, emergencyFleeHome, emergencyFleeDriftTiles, d.Log)
	if !randomevents.Busy(d.Session) {
		return true
	}
	d.logf("[guardian] a random event is present; waiting")
	// The periodic line every other runner's WaitForGuardian already writes. Without it this
	// wait was the one silent one: a trapped event the Guardian keeps retrying held MeleeFlincher
	// with nothing written after the first line (stallwatch's sweep, 2026-09-23).
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

// guardianWaitLogEvery matches every sibling runner's own cadence for the line above.
const guardianWaitLogEvery = 10 * time.Second

// walkNearTimeout bounds WalkNear's entire operation.
const walkNearTimeout = 120 * time.Second

// WalkNear paths to dest and walks there, stopping once within radius tiles of dest —
// duplicated per this project's established per-package convention (see
// docs/script_porting_framework.md's "why duplicated" section).
func WalkNear(d *Deps, dest tile.Tile, radius int) bool {
	deadline := time.Now().Add(walkNearTimeout)
	for time.Now().Before(deadline) {
		here, ok := currentTile(d)
		if ok && tile.Chebyshev(here, dest) <= radius {
			return true
		}
		if !ok || d.Pack == nil {
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
	return false
}

// DirectWalk clicks a single destination tile without pathfinding validation — ported from
// DirectNavigator.walk: the safe tile and the strike target are always within the same
// scene, a step or two apart, so a raw walk click is what the TS source uses too rather
// than a validated multi-corner path.
func DirectWalk(d *Deps, dest tile.Tile) error {
	return actions.Walk(d.Session, []pathfind.NavPoint{{X: dest.X, Z: dest.Z, Level: dest.Level}}, false)
}

// ---- health / food ----

// HPFraction is Skills.hpFraction().
func HPFraction(d *Deps) float64 {
	hp, hpOK := d.Session.State().HP()
	maxHP, maxOK := d.Session.State().MaxHP()
	if !hpOK || !maxOK || maxHP <= 0 {
		return 0
	}
	return float64(hp) / float64(maxHP)
}

// AnyFoodCount counts pack slots offering an "Eat" option, whatever the item — ported from
// MeleeFlincher.ts's own foodItems(): a working flinch trap never eats, so this is a floor
// under a broken one rather than a strategy, and no particular food name is configured.
func AnyFoodCount(d *Deps) int {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return 0
	}
	n := 0
	for _, it := range inv.NonEmpty() {
		if obj, ok := d.Config.Obj(int(it.ID)); ok && hasEatOption(obj) {
			n++
		}
	}
	return n
}

func hasEatOption(obj cachecfg.Obj) bool {
	for _, op := range obj.IOps {
		if strings.EqualFold(op, "Eat") {
			return true
		}
	}
	return false
}

// EatOnce eats the first pack item offering "Eat", confirmed against a real HP rise.
func EatOnce(d *Deps) bool {
	inv := d.Session.State().Inventory(PlayerPack)
	if inv == nil || d.Config == nil {
		return false
	}
	slot := -1
	var objID int32
	name := ""
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if ok && hasEatOption(obj) {
			slot, objID, name = it.Slot, it.ID, obj.Name
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
	ok = waitFor(d, "hp to rise after eating", 3*time.Second, func() bool {
		hp, ok := d.Session.State().HP()
		return ok && hp > before
	})
	if ok {
		after, _ := d.Session.State().HP()
		d.logf("ate %s (%d -> %d hp)", name, before, after)
	}
	return ok
}

// ---- targets ----

// SelfSlot is the local player's own slot.
func SelfSlot(d *Deps) int { return d.Session.State().Self() }

// TargetNPCs returns every currently-visible NPC offering "Attack", optionally filtered to
// name (case-insensitive, empty means anything attackable).
func TargetNPCs(d *Deps, name string) []gamestate.NPC {
	var out []gamestate.NPC
	for _, n := range d.Session.State().NPCs() {
		t, ok := d.Config.Npc(n.Type)
		if !ok || cachecfg.OpIndex(t.Ops, "Attack") == 0 {
			continue
		}
		if name != "" && !strings.EqualFold(t.Name, name) {
			continue
		}
		out = append(out, n)
	}
	return out
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

func npcName(d *Deps, n gamestate.NPC) string {
	if t, ok := d.Config.Npc(n.Type); ok {
		return t.Name
	}
	return ""
}

// ToTargetRecord converts a live NPC into fighterplan's pure TargetRecord — duplicated from
// fighterrunner's own twin per this project's established per-package convention.
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

// ApproachRadius bounds how far the reachability check below looks for a route. A flinch trap
// is fought from arm's length by construction, so this is generous against the 8-tile leash Pick
// already applies and still cheap: the search never leaves a 24-tile box around the safe tile.
const ApproachRadius = 24

// Reachable answers whether there is a walkable route from where we are standing to target.
//
// A flinch target must be reachable — the whole technique is to walk out, land one hit and walk
// back — so a target with no route is one this script can never swing at. Before this check it
// still picked them: the pick ranks by Chebyshev, the attack click is accepted by the server
// through any wall, and the strike then burned its nine-tick swing wait and a twenty-second skip
// on a target it was never going to touch, over and over. Same rule, same reason, as
// fighterrunner.Reachable and combatrunner.Reachable.
//
// Fails OPEN for the same asymmetry those two document: no pack or no known tile answers yes.
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

// Pick is the best npc to flinch that we can actually walk out and hit, or ok=false when nothing
// within flinchplan.LEASH of safe qualifies — ported from MeleeFlincher.ts's own pick(), plus
// the reachability gate the TS source never had (see Reachable).
func Pick(d *Deps, name string, safe tile.Tile, ownIndex int, haveOwn bool, skipUntil map[int]int64, nowMs int64) (gamestate.NPC, bool) {
	live := TargetNPCs(d, name)
	recs := make([]fighterplan.TargetRecord, 0, len(live))
	for _, n := range live {
		recs = append(recs, ToTargetRecord(d, n, safe))
	}
	// One line per pick with a count, rather than one per refused candidate: a safe tile whose
	// whole side of a wall is spawns answers this on every pass, and the useful reading is how
	// many were passed over, not the same tile repeated.
	passedOver, lastWhy := 0, ""
	best, ok := fighterplan.PickTarget(recs, fighterplan.TargetContext{
		Anchor: safe, Leash: 8, OwnIndex: ownIndex, HaveOwn: haveOwn,
		AttackOthersTargets: false, PreferAttackers: true,
		SkipUntil: skipUntil, NowMs: nowMs,
		Reachable: func(t fighterplan.TargetRecord) bool {
			reach, why := Reachable(d, t.Tile)
			if !reach {
				passedOver++
				lastWhy = why
			}
			return reach
		},
	})
	if passedOver > 0 {
		d.logf("no route to %d nearer target(s) from the safe tile (%s) — not clicking through the wall", passedOver, lastWhy)
	}
	if !ok {
		return gamestate.NPC{}, false
	}
	for _, n := range live {
		if n.Index == best.Index {
			return n, true
		}
	}
	return gamestate.NPC{}, false
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

// ---- the swing watch ----

// SwingWatch watches the local player's animation for the start of a swing — ported from
// flinchPlan.ts's SwingWatch class, adapted to gamestate.State.SelfAnim/Tick as the
// animation/tick source instead of the TS scripting API's readerX.selfAnim()/Game.tick().
type SwingWatch struct {
	lastAnim    int
	startedTick int
}

// NewSwingWatch is a SwingWatch with no swing observed yet.
func NewSwingWatch() *SwingWatch { return &SwingWatch{lastAnim: -1, startedTick: -1} }

// Observe feeds the current primary animation (-1 when idle) and the current tick.
func (w *SwingWatch) Observe(anim, tick int) {
	if anim == w.lastAnim {
		return
	}
	w.lastAnim = anim
	if anim != -1 {
		w.startedTick = tick
	}
}

// SwungSince reports whether a swing began on tick or later.
func (w *SwingWatch) SwungSince(tick int) bool { return w.startedTick >= tick }

// Reset clears the watch.
func (w *SwingWatch) Reset() { w.lastAnim, w.startedTick = -1, -1 }

// ObserveSelf feeds a SwingWatch from live state.
func ObserveSelf(d *Deps, w *SwingWatch) {
	anim, tick := d.Session.State().SelfAnim()
	w.Observe(anim, tick)
}

// Tick is the current server tick this session has observed.
func Tick(d *Deps) int { return d.Session.State().Tick() }

// ---- dialogue ----

// CanContinueDialogue reports whether a chat dialogue is blocking (needs a Components
// archive to read interface state).
func CanContinueDialogue(d *Deps, components archive.Components) bool {
	if components == nil {
		return false
	}
	return actions.NewChatDialog(d.Session, components).CanContinue()
}
