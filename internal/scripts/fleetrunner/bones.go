package fleetrunner

import (
	"strings"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/loot"
	"rsb0t-go/internal/scripts/boneburierrunner"
	"rsb0t-go/internal/scripts/boneplan"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/tile"
)

// LOOTING AND BURYING WHAT THE TRAINING KILLS LEAVE BEHIND.
//
// The fleet owner's instruction, 2026-09-22: "scarab swarm should loot and bury bones from
// cows or anything it trains against also."
//
// Stages 3 and 5 both kill Cows for hours — Stage 3 for Magic, Stage 5 for Hitpoints — and
// every one of them drops Bones, which were left on the floor. Two things were being thrown
// away: the Prayer experience (burying is the only way this script gets any, and Stage 6's
// own prayer work has to come from somewhere) and the loot itself, which the panel never saw
// because nothing picked it up.
//
// This reuses BoneBurier's own primitives rather than copying them — FindBone, Collect,
// HeldBoneToBury and BuryOne are that script's, already live-proven — so there is one
// implementation of "what is a bone and how is it buried" in the repo, not two.

// BonesDeps adapts a scarabrunner Deps to BoneBurier's. The same core fields; only the
// Delayer needs converting.
func BonesDeps(d *Deps) *boneburierrunner.Deps {
	return &boneburierrunner.Deps{
		Session: d.Session, Pack: d.Pack, Locs: d.Locs, Config: d.Config,
		Components: d.Components, Chat: d.Chat,
		Delay: boneburierrunner.Delayer(d.Delay), Log: d.Log,
	}
}

// BoneLootRadius is how far from the account a training drop is worth walking to. Small on
// purpose: the cow field is leashed and a kill happens within a tile or two, so anything
// further away is somebody else's kill in a cohort of a hundred.
const BoneLootRadius = 4

// MaxBonesPerPass bounds one call, so a field littered by a hundred accounts cannot turn a
// training step into a scavenging run.
const MaxBonesPerPass = 2

// BoneSkipPrayer is the base Prayer level at which LootAndBuryBones leaves bones on the ground.
const BoneSkipPrayer = 43

// boneSkipPrayerStat is UPDATE_STAT's Prayer index.
const boneSkipPrayerStat = 5

// LootAndBuryBones picks up bones lying near the account and buries them, returning how many
// were buried.
//
// Called after a kill, it is what turns a training cow into Prayer experience instead of
// litter. It is bounded and non-blocking by design: at most MaxBonesPerPass, within
// BoneLootRadius, and it gives up rather than chasing a drop it cannot reach — training is
// the job, and this is the tidy-up.
//
// Every take is recorded through d.Loot, on the kill that left it, so the panel's Cow card shows
// what the training actually yielded. The burial is recorded by internal/bot's bury watch.
func LootAndBuryBones(d *Deps, names []string) int {
	if d == nil || d.Session == nil {
		return 0
	}
	// a user request: Prayer 43 (Protect prayers) is what combat needs, so an account there or above stops slowing its
	// training for bones.
	if BaseLevel(d, boneSkipPrayerStat) >= BoneSkipPrayer {
		return 0
	}
	if len(names) == 0 {
		names = boneplan.DefaultNames
	}
	bd := BonesDeps(d)
	here := CurrentTile(d)
	if here == nil {
		return 0
	}
	anchor := tile.Tile{X: here.X, Z: here.Z, Level: here.Level}

	buried := 0
	skip := map[string]bool{}
	for i := 0; i < MaxBonesPerPass; i++ {
		drop, ok := boneburierrunner.FindBone(bd, anchor, BoneLootRadius, names, skip)
		if !ok {
			break
		}
		packBefore := LootBefore(d)
		res := boneburierrunner.Collect(bd, drop)
		if !res.Picked {
			// Somebody else's now, or unreachable. Do not try it again this pass.
			skip[boneburierrunner.DropKey(drop, "")] = true
			continue
		}
		RecordGroundTake(d, packBefore, drop)

		it, op, found := boneburierrunner.HeldBoneToBury(bd, names, boneplan.DefaultBankNames, boneplan.DisposalBury)
		if !found {
			continue
		}
		if boneburierrunner.BuryOne(bd, it, op) {
			buried++
		}
		d.Delay(300 * time.Millisecond)
	}
	return buried
}

// MaxLootPerPass bounds LootValuables the way MaxBonesPerPass bounds the bones.
const MaxLootPerPass = 3

// RunePileRadius is how far a pile of runes at a rune pen (scarabplan.TrainPen.RunePen) is worth walking to:
// a cast kills from up to a spell's reach away (scarabplan.CastRange) and the pile lies where the NPC fell, so
// BoneLootRadius left most of a walk-up Magic pen's runes on the ground ("runepens", 13 Oct 2026: the Dwarven
// Mine dwarves drop their Chaos and Nature runes in one kill in eight, and the nearest dwarf is rarely
// within four tiles). Only piles of the runes a rune pen pays (scarabplan.RunePayRunes), only at the pen
// and off its hazard, and the pile is the killer's alone for its first hundred ticks.
const RunePileRadius = fleetplan.CastRange

// LootValuables picks up, after a kill, what combatplan.WorthTrainLoot takes within BoneLootRadius:
// every stackable drop the pack has room for (runes from the giants, feathers from the chickens,
// coins) and a valuable non-stackable only into a free slot, never dropping food for it. Returns
// how many piles it took; each take is recorded on the kill that left it.
func LootValuables(d *Deps) int {
	if d == nil || d.Session == nil || d.Config == nil {
		return 0
	}
	here := CurrentTile(d)
	if here == nil {
		return 0
	}
	bd := BonesDeps(d)
	anchor := tile.Tile{X: here.X, Z: here.Z, Level: here.Level}
	taken := 0
	skip := map[string]bool{}
	for i := 0; i < MaxLootPerPass; i++ {
		drop, ok := nearestTrainLoot(d, anchor, skip)
		if !ok {
			break
		}
		packBefore := LootBefore(d)
		res := boneburierrunner.Collect(bd, drop)
		if !res.Picked {
			skip[boneburierrunner.DropKey(drop, "")] = true
			continue
		}
		RecordGroundTake(d, packBefore, drop)
		NoteRunePay(d, drop)
		taken++
	}
	return taken
}

// NoteRunePay says what a rune pen paid, in the log the live hour is judged by: the pen and the runes taken. A
// pile that is not a rune of scarabplan.RunePayRunes, or a take away from a rune pen, says nothing.
func NoteRunePay(d *Deps, drop gamestate.GroundItem) {
	pen := d.Pen()
	if !pen.RunePen || d.Config == nil {
		return
	}
	obj, ok := d.Config.Obj(drop.ID)
	if !ok || !fleetplan.IsRunePayName(obj.Name) {
		return
	}
	d.Logf("runepen: %s paid %d %s (taken at %d,%d)", pen.Name, max(drop.Count, 1), obj.Name, drop.X, drop.Z)
}

// nearestTrainLoot is the closest ground item within BoneLootRadius that WorthTrainLoot takes now; at a rune pen
// a pile of a rune it pays is worth RunePileRadius (see there).
func nearestTrainLoot(d *Deps, anchor tile.Tile, skip map[string]bool) (gamestate.GroundItem, bool) {
	free := FreeSlots(d)
	held := map[int]bool{}
	for _, it := range PackSlotItems(d) {
		held[int(it.ID)] = true
	}
	pen := d.Pen()
	best, bestDist, found := gamestate.GroundItem{}, -1, false
	for _, g := range d.Session.State().GroundItems() {
		gt := tile.Tile{X: g.X, Z: g.Z, Level: g.Level}
		if gt.Level != anchor.Level || tile.Chebyshev(anchor, gt) > RunePileRadius || skip[boneburierrunner.DropKey(g, "")] {
			continue
		}
		obj, ok := d.Config.Obj(g.ID)
		if !ok {
			continue
		}
		if tile.Chebyshev(anchor, gt) > BoneLootRadius && !(pen.RunePen && fleetplan.IsRunePayName(obj.Name) && pen.Near(gt) && pen.SafeTarget(gt)) {
			continue
		}
		drop := combatplan.TrainLootDrop{Name: obj.Name, Stackable: obj.Stackable, Cost: int(obj.Cost), Stacks: obj.Stackable && held[g.ID], ID: g.ID}
		if !combatplan.WorthTrainLoot(drop, free) {
			continue
		}
		if dist := tile.Chebyshev(anchor, gt); !found || dist < bestDist {
			best, bestDist, found = g, dist, true
		}
	}
	return best, found
}

// RecordGroundTake reports what a pickup actually put in the pack, on the kill that dropped it:
// the newest kill within loot.KillNearTiles of the drop, else the newest kill (loot.KillLog).
//
// THE COW CARD'S MISSING DROPS. This used to record every take as an openable named "Ground",
// id -1 — its comment said "attributed to the kill that dropped it", and it was not. The live
// panel had 97 cow kills with no items and their 106 Bones on a separate "Ground" card
// (2026-09-23). internal/controlpanel's restore folds those old records onto the cow.
//
// A take no recent kill of this account or the fleet can claim is a pile somebody else left. At the
// pen the account trains at it is the pen's own monster's drop (penSource: a kill nobody noted, or one
// that aged out), recorded on that monster's card with no kill added; anywhere else it is recorded as a
// pickup of the item itself, never as Ground.
//
// THE PANEL, 5 Oct 2026 on 0741287b: Black sq shield, Nature rune, Law rune, Earth rune and Coins
// grabbed at the North Ardougne Moss giants were "take:<item>" cards of their own, and a
// "take: Big bones" card held 1,147 takes, while kill:ogre:114 held 2,388 kills and 287 Big bones.
func RecordGroundTake(d *Deps, packBefore *loot.Reading, drop gamestate.GroundItem) {
	if d.Loot == nil {
		return
	}
	src, ok := d.kills().SourceFor(drop.X, drop.Z, drop.Level, time.Now())
	events := 0
	if !ok {
		if ps, found := penSource(d, drop); found {
			src = ps
		} else {
			// A rune or a bone nobody's kill claimed goes under this account's newest kill, with no
			// event of its own; a rune with no kill at all is not recorded (loot.PickupSource).
			var picked bool
			src, events, picked = loot.PickupSource(d.Config, d.kills(), drop.ID, "")
			if !picked {
				d.Logf("loot: not recording a rune taken off the ground: this account has no kill to put it under")
				return
			}
		}
	}
	RecordTake(d, src, packBefore, events, 1500*time.Millisecond)
}

// penSource is the kill source of the pen's monster for a pile at the account's pen that no kill could
// claim: the pile lies within the pen's leash of its stand, on its level, and the pen has a monster whose
// npc type is known (the nearest one in view named for the pen, else the first type of that name that
// offers Attack). false for an account that has chosen no pen, for the zero config, and for a pile
// away from the pen.
func penSource(d *Deps, drop gamestate.GroundItem) (loot.Source, bool) {
	if d.TrainPen.Name == "" || d.Config == nil {
		return loot.Source{}, false
	}
	pen := d.Pen()
	at := tile.Tile{X: drop.X, Z: drop.Z, Level: drop.Level}
	if pen.Monster == "" || at.Level != pen.Stand.Level || tile.Chebyshev(at, pen.Stand) > pen.Leash {
		return loot.Source{}, false
	}
	if !penDrops(d, pen, drop.ID) {
		return loot.Source{}, false
	}
	typ, ok := PenMonsterType(d, pen.Herd(), at)
	if !ok {
		return loot.Source{}, false
	}
	return loot.NPC(d.Config, loot.ActionKill, typ, pen.Monster), true
}

// penDrops reports whether the pen's monster can drop the object: always, for a pen whose drops are
// not listed (scarabplan.TrainPen.Drops).
func penDrops(d *Deps, pen fleetplan.TrainPen, objID int) bool {
	if len(pen.Drops) == 0 {
		return true
	}
	obj, ok := d.Config.Obj(objID)
	if !ok {
		return false
	}
	for _, name := range pen.Drops {
		if strings.EqualFold(name, obj.Name) {
			return true
		}
	}
	return false
}

// PenMonsterType is the npc type of the monster named `name`: the nearest one in view to `at`, else the
// first type of that name in the config that offers Attack (the Ogres are two types, 114 and 115, and
// the Combat Training Camp's pen has a third that does not offer it), else the first of that name.
func PenMonsterType(d *Deps, name string, at tile.Tile) (int, bool) {
	best, bestDist, found := 0, 0, false
	for _, n := range d.Session.State().NPCs() {
		if !NpcNamed(d, n.Type, name) {
			continue
		}
		dist := tile.Chebyshev(at, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		if !found || dist < bestDist {
			best, bestDist, found = n.Type, dist, true
		}
	}
	if found {
		return best, true
	}
	ids := d.Config.NpcIDs(name)
	for _, id := range ids {
		if t, ok := d.Config.Npc(id); ok && HasAttackOp(t.Ops) {
			return id, true
		}
	}
	if len(ids) > 0 {
		return ids[0], true
	}
	return 0, false
}
